package tools

import (
	"encoding/json"
	"fmt"
)

// SubagentSpawner is a function variable injected from handlers to avoid circular imports
// Set via tools.SetSubagentFuncs() in main.go
var SubagentSpawner func(sessionID, name, task string) string
var SubagentStatusGetter func(id string) (status, output string)

// SetSubagentFuncs injects the handler-level subagent functions into the tools package
func SetSubagentFuncs(spawner func(string, string, string) string, getter func(string) (string, string)) {
	SubagentSpawner = spawner
	SubagentStatusGetter = getter
}

func executeSpawnSubagent(rawArgs string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: Invalid arguments for spawn_subagent"
	}
	name, _ := args["name"].(string)
	task, _ := args["task"].(string)
	sessionID, _ := args["session_id"].(string)

	if name == "" || task == "" {
		return "Error: 'name' and 'task' are required for spawn_subagent"
	}
	if SubagentSpawner == nil {
		return "Error: Subagent system is not initialized"
	}
	id := SubagentSpawner(sessionID, name, task)
	return fmt.Sprintf(`{"id": "%s", "status": "running", "message": "Subagent '%s' spawned successfully. Use get_subagent_status('%s') to check progress."}`, id, name, id)
}

func executeGetSubagentStatus(rawArgs string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: Invalid arguments for get_subagent_status"
	}
	id, _ := args["id"].(string)
	if id == "" {
		return "Error: 'id' is required for get_subagent_status"
	}
	if SubagentStatusGetter == nil {
		return "Error: Subagent system is not initialized"
	}
	status, output := SubagentStatusGetter(id)
	if status == "" {
		return fmt.Sprintf(`{"error": "Subagent '%s' not found"}`, id)
	}
	result, _ := json.Marshal(map[string]string{"id": id, "status": status, "output": output})
	return string(result)
}
