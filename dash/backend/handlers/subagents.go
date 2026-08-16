package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
	"github.com/danilrybalkin/apollo-dash/tools"
	"github.com/google/uuid"
)

// SubagentRecord represents a background subagent record in the DB
type SubagentRecord struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Task      string `json:"task"`
	Status    string `json:"status"` // running | done | error
	Output    string `json:"output"`
	CreatedAt string `json:"created_at"`
}

// SubagentsHandler: GET /api/subagents    → list all
//
//	DELETE /api/subagents?id=  → cancel/remove one
func SubagentsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		rows, err := db.DB.Query("SELECT id, IFNULL(session_id,''), name, task, status, IFNULL(output,''), created_at FROM subagents ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var agents []SubagentRecord
		for rows.Next() {
			var a SubagentRecord
			rows.Scan(&a.ID, &a.SessionID, &a.Name, &a.Task, &a.Status, &a.Output, &a.CreatedAt)
			agents = append(agents, a)
		}
		if agents == nil {
			agents = []SubagentRecord{}
		}
		json.NewEncoder(w).Encode(agents)
		return
	}

	if r.Method == http.MethodDelete {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}
		db.DB.Exec("DELETE FROM subagents WHERE id = ?", id)
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// SpawnSubagent creates a DB record and launches a background goroutine that runs
// an independent LLM+tool loop for the given task.
func SpawnSubagent(sessionID, name, task string) string {
	id := uuid.New().String()
	db.DB.Exec(
		"INSERT INTO subagents (id, session_id, name, task, status, output) VALUES (?, ?, ?, ?, 'running', '')",
		id, sessionID, name, task,
	)
	log.Printf("Subagent [%s] spawned: %s", name, task)

	go runSubagent(id, name, task, sessionID)
	return id
}

// GetSubagentStatus returns the current status and output of a subagent
func GetSubagentStatus(id string) (status, output string) {
	db.DB.QueryRow("SELECT status, IFNULL(output,'') FROM subagents WHERE id = ?", id).Scan(&status, &output)
	return
}

// runSubagent is the background goroutine that executes the subagent's LLM+tool loop
func runSubagent(id, name, task, sessionID string) {
	defer func() {
		if r := recover(); r != nil {
			db.DB.Exec("UPDATE subagents SET status='error', output=? WHERE id=?", fmt.Sprintf("Panic: %v", r), id)
		}
	}()

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	ollamaUrl := os.Getenv("OLLAMA_API_URL")

	var targetUrl, reqApiKey, model string

	settings := GetCurrentSettings("")
	if settings.DefaultModel != "" {
		model = settings.DefaultModel
	} else {
		model = "meta-llama/llama-3-8b-instruct:free"
	}

	if ollamaUrl != "" {
		targetUrl = ollamaUrl + "/v1/chat/completions"
		reqApiKey = "Bearer local"
	} else {
		targetUrl = "https://openrouter.ai/api/v1/chat/completions"
		reqApiKey = "Bearer " + apiKey
	}

	sysPrompt := fmt.Sprintf(`You are a background subagent named "%s". Your sole task is:

%s

Work autonomously using your tools. When you are done, produce a comprehensive report of your findings, actions taken, or results. Be thorough.`, name, task)

	messages := []map[string]interface{}{
		{"role": "system", "content": sysPrompt},
		{"role": "user", "content": "Begin working on your assigned task."},
	}

	var outputLog string
	var projectPath string
	if sessionID != "" {
		db.DB.QueryRow("SELECT IFNULL(project_path, '') FROM chat_sessions WHERE id = ?", sessionID).Scan(&projectPath)
	}

	// Tool loop — up to 8 turns to complete the task
	client := &http.Client{Timeout: 300 * time.Second}
	for attempt := 0; attempt < 8; attempt++ {
		payload, _ := json.Marshal(map[string]interface{}{
			"model":    model,
			"messages": messages,
			"stream":   false,
			"tools":    tools.GetAvailableTools(),
		})
		req, _ := http.NewRequest("POST", targetUrl, bytes.NewBuffer(payload))
		req.Header.Set("Authorization", reqApiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			db.DB.Exec("UPDATE subagents SET status='error', output=? WHERE id=?", "LLM request failed: "+err.Error(), id)
			return
		}

		var result struct {
			Choices []struct {
				Message struct {
					Content   string                   `json:"content"`
					ToolCalls []map[string]interface{} `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if len(result.Choices) == 0 {
			break
		}

		msg := result.Choices[0].Message
		if msg.Content != "" {
			outputLog += msg.Content + "\n\n"
		}

		// Execute tool calls if any
		if len(msg.ToolCalls) > 0 {
			messages = append(messages, map[string]interface{}{
				"role":       "assistant",
				"content":    msg.Content,
				"tool_calls": msg.ToolCalls,
			})
			for _, tc := range msg.ToolCalls {
				id2, _ := tc["id"].(string)
				funcObj, _ := tc["function"].(map[string]interface{})
				tName, _ := funcObj["name"].(string)
				tArgs, _ := funcObj["arguments"].(string)
				result := tools.ExecuteTool(tName, tArgs, projectPath)
				outputLog += fmt.Sprintf("[Tool: %s] %s\n\n", tName, result)
				messages = append(messages, map[string]interface{}{
					"role": "tool", "tool_call_id": id2, "name": tName, "content": result,
				})
			}
			continue
		}

		// No tool calls → the agent is done
		if result.Choices[0].FinishReason == "stop" || msg.Content != "" {
			break
		}
	}

	// Store final output, notify session
	db.DB.Exec("UPDATE subagents SET status='done', output=? WHERE id=?", outputLog, id)
	if sessionID != "" {
		// Inject a system notification into the session's next context via a DB message
		notification := fmt.Sprintf("<subagent_complete>\nSubagent **%s** has finished its task.\n\n**Summary:**\n%s\n</subagent_complete>", name, outputLog)
		db.DB.Exec("INSERT INTO chat_messages (session_id, role, content) VALUES (?, 'system', ?)", sessionID, notification)
	}
	log.Printf("Subagent [%s] completed.", name)
}
