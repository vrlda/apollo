package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
)

// MCPServerConfig defines a single MCP server connection
type MCPServerConfig struct {
	Name      string `json:"name"`
	Command   string `json:"command"`   // e.g. "npx @modelcontextprotocol/server-postgres postgresql://..."
	Transport string `json:"transport"` // "stdio" (default) or "http"
	URL       string `json:"url"`       // for HTTP transport
}

// MCPTool represents a tool discovered from an MCP server
type MCPTool struct {
	Server      string                 `json:"server"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
}

var (
	mcpMu    sync.RWMutex
	mcpTools []MCPTool
	mcpProcs = map[string]*mcpProcess{}
)

type mcpProcess struct {
	cfg       MCPServerConfig
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	mu        sync.Mutex
	status    string // "running", "crashed", "starting", "retrying"
	lastError string
	stopped   bool
}

// mcpRPCRequest sends a JSON-RPC message to the MCP server process (stdio or http)
func (p *mcpProcess) call(method string, params interface{}) (map[string]interface{}, error) {
	if p.cfg.Transport == "http" {
		return p.callHttp(method, params)
	}
	return p.callStdio(method, params)
}

func (p *mcpProcess) callHttp(method string, params interface{}) (map[string]interface{}, error) {
	reqID := time.Now().UnixNano()
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  method,
		"params":  params,
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", p.cfg.URL, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var r map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if errObj, ok := r["error"]; ok {
		return nil, fmt.Errorf("MCP HTTP error: %v", errObj)
	}
	if result, ok := r["result"].(map[string]interface{}); ok {
		return result, nil
	}
	return nil, fmt.Errorf("invalid MCP HTTP response")
}

func (p *mcpProcess) callStdio(method string, params interface{}) (map[string]interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	reqID := time.Now().UnixNano()
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  method,
		"params":  params,
	}
	b, _ := json.Marshal(req)
	_, err := fmt.Fprintf(p.stdin, "%s\n", string(b))
	if err != nil {
		return nil, fmt.Errorf("write to MCP process failed: %w", err)
	}

	// Read next line as response (MCP uses newline-delimited JSON-RPC)
	if p.stdout.Scan() {
		var resp map[string]interface{}
		if err := json.Unmarshal([]byte(p.stdout.Text()), &resp); err != nil {
			return nil, fmt.Errorf("invalid MCP response: %w", err)
		}
		if errObj, ok := resp["error"]; ok {
			return nil, fmt.Errorf("MCP error: %v", errObj)
		}
		if result, ok := resp["result"].(map[string]interface{}); ok {
			return result, nil
		}
	}
	return nil, fmt.Errorf("no response from MCP server")
}

// StartMCPServer launches an MCP server process and discovers its tools
func StartMCPServer(cfg MCPServerConfig) {
	if cfg.Transport == "http" {
		startHttpMCPServer(cfg)
		return
	}
	startStdioMCPServer(cfg)
}

func startHttpMCPServer(cfg MCPServerConfig) {
	proc := &mcpProcess{
		cfg:    cfg,
		status: "running",
	}
	mcpMu.Lock()
	mcpProcs[cfg.Name] = proc
	mcpMu.Unlock()

	// Initialize
	_, err := proc.call("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "AgentHQ", "version": "1.0"},
	})
	if err != nil {
		proc.status = "crashed"
		proc.lastError = err.Error()
		return
	}
	discoverTools(cfg.Name, proc)
}

func startStdioMCPServer(cfg MCPServerConfig) {
	go func() {
		backoff := 1 * time.Second
		for {
			mcpMu.RLock()
			p, ok := mcpProcs[cfg.Name]
			mcpMu.RUnlock()
			if ok && p.stopped {
				return
			}

			log.Printf("MCP: Starting stdio server '%s'...", cfg.Name)
			proc, err := launchStdioProcess(cfg)
			if err != nil {
				log.Printf("MCP: Failed to launch '%s': %v", cfg.Name, err)
				mcpMu.Lock()
				mcpProcs[cfg.Name] = &mcpProcess{cfg: cfg, status: "crashed", lastError: err.Error()}
				mcpMu.Unlock()
				time.Sleep(backoff)
				if backoff < 60*time.Second {
					backoff *= 2
				}
				continue
			}

			mcpMu.Lock()
			mcpProcs[cfg.Name] = proc
			mcpMu.Unlock()

			// Initialize
			_, err = proc.call("initialize", map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]string{"name": "AgentHQ", "version": "1.0"},
			})
			if err != nil {
				log.Printf("MCP: Initialize failed for '%s': %v", cfg.Name, err)
				proc.status = "crashed"
				proc.lastError = err.Error()
				proc.cmd.Process.Kill()
				time.Sleep(backoff)
				continue
			}

			discoverTools(cfg.Name, proc)
			backoff = 1 * time.Second // Reset backoff on success

			// Watchdog: wait for process to exit
			err = proc.cmd.Wait()
			log.Printf("MCP: Server '%s' exited: %v", cfg.Name, err)

			mcpMu.RLock()
			isStopped := mcpProcs[cfg.Name].stopped
			mcpMu.RUnlock()
			if isStopped {
				return
			}

			mcpMu.Lock()
			proc.status = "retrying"
			if err != nil {
				proc.lastError = err.Error()
			}
			mcpMu.Unlock()

			time.Sleep(backoff)
			if backoff < 60*time.Second {
				backoff *= 2
			}
		}
	}()
}

func launchStdioProcess(cfg MCPServerConfig) (*mcpProcess, error) {
	parts := strings.Fields(cfg.Command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &mcpProcess{
		cfg:    cfg,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdoutPipe),
		status: "running",
	}, nil
}

func discoverTools(serverName string, proc *mcpProcess) {
	toolsResult, err := proc.call("tools/list", map[string]interface{}{})
	if err != nil {
		log.Printf("MCP: tools/list failed for '%s': %v", serverName, err)
		return
	}

	toolsList, _ := toolsResult["tools"].([]interface{})
	mcpMu.Lock()
	// Clear old tools for this server
	var filtered []MCPTool
	for _, t := range mcpTools {
		if t.Server != serverName {
			filtered = append(filtered, t)
		}
	}
	mcpTools = filtered

	for _, t := range toolsList {
		toolMap, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := toolMap["name"].(string)
		desc, _ := toolMap["description"].(string)
		schema, _ := toolMap["inputSchema"].(map[string]interface{})
		mcpTools = append(mcpTools, MCPTool{
			Server:      serverName,
			Name:        fmt.Sprintf("mcp_%s_%s", serverName, name),
			Description: fmt.Sprintf("[MCP:%s] %s", serverName, desc),
			Schema:      schema,
		})
	}
	mcpMu.Unlock()
}

// GetMCPTools returns discovered MCP tools (used by tools/registry.go via MCP integration)
func GetMCPTools() []MCPTool {
	mcpMu.RLock()
	defer mcpMu.RUnlock()
	result := make([]MCPTool, len(mcpTools))
	copy(result, mcpTools)
	return result
}

// CallMCPTool executes an MCP tool by its registry name.
func CallMCPTool(toolName, rawArgs string) string {
	// toolName format: mcp_{server}_{toolName}
	parts := strings.SplitN(strings.TrimPrefix(toolName, "mcp_"), "_", 2)
	if len(parts) != 2 {
		return fmt.Sprintf("Error: invalid MCP tool name '%s'", toolName)
	}
	serverName, toolName := parts[0], parts[1]

	mcpMu.RLock()
	proc, ok := mcpProcs[serverName]
	mcpMu.RUnlock()
	if !ok {
		return fmt.Sprintf("Error: MCP server '%s' is not running.", serverName)
	}

	var argsMap map[string]interface{}
	json.Unmarshal([]byte(rawArgs), &argsMap)

	result, err := proc.call("tools/call", map[string]interface{}{
		"name":      toolName,
		"arguments": argsMap,
	})
	if err != nil {
		return fmt.Sprintf("MCP tool error: %v", err)
	}

	// Extract text content from MCP response
	if content, ok := result["content"].([]interface{}); ok {
		var parts []string
		for _, c := range content {
			if m, ok := c.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	b, _ := json.Marshal(result)
	return string(b)
}

// MCPHandler: GET /api/mcp → list MCP tools, POST → configure new servers
func MCPHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		mcpMu.RLock()
		tools := make([]MCPTool, len(mcpTools))
		copy(tools, mcpTools)

		serverSummaries := []map[string]interface{}{}
		configs := getMCPServerConfigs()
		for _, cfg := range configs {
			summary := map[string]interface{}{
				"name":      cfg.Name,
				"transport": cfg.Transport,
				"command":   cfg.Command,
				"url":       cfg.URL,
				"status":    "offline",
				"lastError": "",
			}
			if p, ok := mcpProcs[cfg.Name]; ok {
				summary["status"] = p.status
				summary["lastError"] = p.lastError
			}
			serverSummaries = append(serverSummaries, summary)
		}
		mcpMu.RUnlock()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"tools":   tools,
			"servers": serverSummaries,
		})

	case http.MethodPost:
		var cfg MCPServerConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		// Persist to DB
		b, _ := json.Marshal(cfg)
		db.DB.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
			fmt.Sprintf("mcp_server_%s", cfg.Name), string(b))
		// Start the server process
		go StartMCPServer(cfg)
		json.NewEncoder(w).Encode(map[string]string{"status": "starting", "name": cfg.Name})

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		db.DB.Exec("DELETE FROM settings WHERE key = ?", fmt.Sprintf("mcp_server_%s", name))
		mcpMu.Lock()
		if proc, ok := mcpProcs[name]; ok {
			proc.stopped = true
			if proc.cmd != nil && proc.cmd.Process != nil {
				proc.cmd.Process.Kill()
			}
			delete(mcpProcs, name)
		}
		// Remove tools from this server
		var filtered []MCPTool
		for _, t := range mcpTools {
			if t.Server != name {
				filtered = append(filtered, t)
			}
		}
		mcpTools = filtered
		mcpMu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"status": "removed"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getMCPServerConfigs() []MCPServerConfig {
	rows, err := db.DB.Query("SELECT value FROM settings WHERE key LIKE 'mcp_server_%'")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var cfgs []MCPServerConfig
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err == nil {
			var cfg MCPServerConfig
			if json.Unmarshal([]byte(val), &cfg) == nil {
				cfgs = append(cfgs, cfg)
			}
		}
	}
	return cfgs
}

// InitMCPServers loads and starts all persisted MCP servers at startup
func InitMCPServers() {
	cfgs := getMCPServerConfigs()
	for _, cfg := range cfgs {
		go StartMCPServer(cfg)
	}
}
