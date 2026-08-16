package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danilrybalkin/apollo-dash/db"
)

// WorkspaceTreeNode represents a file or directory in the tree
type WorkspaceTreeNode struct {
	Name     string               `json:"name"`
	Path     string               `json:"path"`
	IsDir    bool                 `json:"isDir"`
	Children []*WorkspaceTreeNode `json:"children,omitempty"`
}

func withinRoot(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// getProjectRoots returns mapped workspace roots for one user from companies.
func getProjectRoots(ownerUserID string) []ProjectConfig {
	rows, err := db.DB.Query(`
		SELECT IFNULL(name,''), IFNULL(workspace_path,''), IFNULL(deploy_command,'')
		FROM companies
		WHERE owner_user_id = ? AND TRIM(IFNULL(workspace_path,'')) <> ''
		ORDER BY created_at ASC
	`, ownerUserID)
	if err == nil {
		defer rows.Close()
		var out []ProjectConfig
		for rows.Next() {
			var p ProjectConfig
			if scanErr := rows.Scan(&p.Name, &p.Path, &p.DeployCommand); scanErr != nil {
				continue
			}
			p.Path = strings.TrimSpace(p.Path)
			if p.Path == "" {
				continue
			}
			out = append(out, p)
		}
		if len(out) > 0 {
			return out
		}
	}

	return []ProjectConfig{}
}

// securePath validates that the requested path is within one of the allowed project roots
func secureWorkspacePath(ownerUserID string, requestedPath string) (string, bool) {
	abs, err := filepath.Abs(requestedPath)
	if err != nil {
		return "", false
	}
	roots := getProjectRoots(ownerUserID)
	for _, p := range roots {
		rootAbs, err := filepath.Abs(p.Path)
		if err != nil {
			continue
		}
		if withinRoot(abs, rootAbs) {
			return abs, true
		}
	}
	return "", false
}

// buildTree recursively builds a WorkspaceTreeNode tree up to maxDepth levels
func buildTree(root string, displayPath string, depth int) *WorkspaceTreeNode {
	info, err := os.Stat(root)
	if err != nil {
		return nil
	}
	node := &WorkspaceTreeNode{
		Name:  info.Name(),
		Path:  displayPath,
		IsDir: info.IsDir(),
	}
	if !info.IsDir() || depth <= 0 {
		return node
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return node
	}
	// Sort: dirs first, then files
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files and common noise
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__" || name == "venv" || name == "dist" {
			continue
		}
		childPath := filepath.Join(root, name)
		childDisplay := filepath.Join(displayPath, name)
		child := buildTree(childPath, childDisplay, depth-1)
		if child != nil {
			node.Children = append(node.Children, child)
		}
	}
	return node
}

// WorkspaceProjectsHandler: GET /api/workspace/projects
// Returns the list of company-mapped workspace roots.
func WorkspaceProjectsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	projects := getProjectRoots(CurrentUserID(r))
	if projects == nil {
		projects = []ProjectConfig{}
	}
	json.NewEncoder(w).Encode(projects)
}

// WorkspaceTreeHandler: GET /api/workspace/tree?path=<dir>
// Returns the file tree for the given directory path (must be within a mapped company workspace).
func WorkspaceTreeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		// Return all project roots as a virtual tree
		roots := getProjectRoots(CurrentUserID(r))
		var nodes []*WorkspaceTreeNode
		for _, p := range roots {
			node := buildTree(p.Path, p.Path, 20)
			if node != nil {
				node.Name = p.Name // Use friendly name as tree root label
				nodes = append(nodes, node)
			}
		}
		if nodes == nil {
			nodes = []*WorkspaceTreeNode{}
		}
		json.NewEncoder(w).Encode(nodes)
		return
	}
	safePath, ok := secureWorkspacePath(CurrentUserID(r), requestedPath)
	if !ok {
		http.Error(w, "Path not within a managed project", http.StatusForbidden)
		return
	}
	node := buildTree(safePath, safePath, 20)
	if node == nil {
		http.Error(w, "Path not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(node)
}

// WorkspaceFileHandler handles GET/POST /api/workspace/file
func WorkspaceFileHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		// Read file contents
		requestedPath := r.URL.Query().Get("path")
		if requestedPath == "" {
			http.Error(w, "path is required", http.StatusBadRequest)
			return
		}
		safePath, ok := secureWorkspacePath(CurrentUserID(r), requestedPath)
		if !ok {
			http.Error(w, "Path not within a managed project", http.StatusForbidden)
			return
		}
		content, err := os.ReadFile(safePath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"path":    safePath,
			"content": string(content),
		})
		return
	}

	if r.Method == http.MethodPost {
		// Write file contents
		var payload struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		safePath, ok := secureWorkspacePath(CurrentUserID(r), payload.Path)
		if !ok {
			http.Error(w, "Path not within a managed project", http.StatusForbidden)
			return
		}
		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
			http.Error(w, "Failed to create directory", http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(safePath, []byte(payload.Content), 0644); err != nil {
			http.Error(w, "Failed to write file", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// WorkspaceDeployHandler: POST /api/workspace/deploy
// Runs the configured deploy command for a project and streams stdout
func WorkspaceDeployHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var target *ProjectConfig
	for _, p := range getProjectRoots(CurrentUserID(r)) {
		if p.Name == payload.Project {
			pc := p
			target = &pc
			break
		}
	}
	if target == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Stream deploy output as SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)

	cmd := exec.Command("bash", "-c", target.DeployCommand)
	cmd.Dir = target.Path

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "Failed to create stdout pipe", http.StatusInternalServerError)
		return
	}
	cmd.Stderr = cmd.Stdout // Merge stderr into stdout

	if err := cmd.Start(); err != nil {
		http.Error(w, "Failed to start deploy command: "+err.Error(), http.StatusInternalServerError)
		return
	}

	buf := make([]byte, 512)
	for {
		n, err := stdoutPipe.Read(buf)
		if n > 0 {
			line := string(buf[:n])
			data, _ := json.Marshal(map[string]string{"output": line})
			w.Write([]byte("data: "))
			w.Write(data)
			w.Write([]byte("\n\n"))
			if ok {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}

	cmd.Wait()
	w.Write([]byte("data: {\"output\":\"\\n[Deploy complete]\\n\",\"done\":true}\n\n"))
	if ok {
		flusher.Flush()
	}
}
