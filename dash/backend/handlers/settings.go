package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/danilrybalkin/apollo-dash/db"
)

// ProjectConfig represents a managed project workspace
type ProjectConfig struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	DeployCommand string `json:"deploy_command"`
}

type SettingsPayload struct {
	BearerToken              string          `json:"bearer_token"`
	FeaturedModels           []string        `json:"featured_models"`
	DefaultModel             string          `json:"default_model"`
	EmbeddingApiUrl          string          `json:"embedding_api_url"`
	EmbeddingModel           string          `json:"embedding_model"`
	SystemPrompt             string          `json:"-"`
	ManagedProjects          []ProjectConfig `json:"-"`
	AutoCompactTokens        int             `json:"auto_compact_tokens"`
	AgentOSEnabled           bool            `json:"agentos_enabled"`
	AgentOSPolicyEnforcement string          `json:"agentos_policy_enforcement"`
	AgentOSKillSwitch        bool            `json:"agentos_kill_switch"`
}

// ── Global settings helpers ───────────────────────────────────────────────────

func getSettingString(key string, fallback string) string {
	var val string
	err := db.DB.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return fallback
	}
	return val
}

func setSettingString(key string, value string) {
	db.DB.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, value)
}

func getSettingInt(key string, fallback int) int {
	strVal := getSettingString(key, "")
	if strVal == "" {
		return fallback
	}
	var intVal int
	_, err := fmt.Sscanf(strVal, "%d", &intVal)
	if err != nil {
		return fallback
	}
	return intVal
}

func setSettingInt(key string, value int) {
	setSettingString(key, fmt.Sprintf("%d", value))
}

func getSettingBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(getSettingString(key, "")))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// ── Per-user settings helpers ─────────────────────────────────────────────────

// getUserSettingString reads from user_settings; falls back to global settings.
func getUserSettingString(userID, key, fallback string) string {
	if userID != "" && userID != "admin" {
		var val string
		err := db.DB.QueryRow("SELECT value FROM user_settings WHERE user_id = ? AND key = ?", userID, key).Scan(&val)
		if err == nil && strings.TrimSpace(val) != "" {
			return val
		}
	}
	return getSettingString(key, fallback)
}

func setUserSettingString(userID, key, value string) {
	if userID == "" || userID == "admin" {
		setSettingString(key, value)
		return
	}
	db.DB.Exec("INSERT OR REPLACE INTO user_settings (user_id, key, value) VALUES (?, ?, ?)", userID, key, value)
}

func getUserSettingInt(userID, key string, fallback int) int {
	strVal := getUserSettingString(userID, key, "")
	if strVal == "" {
		return fallback
	}
	var intVal int
	_, err := fmt.Sscanf(strVal, "%d", &intVal)
	if err != nil {
		return fallback
	}
	return intVal
}

func setUserSettingInt(userID, key string, value int) {
	setUserSettingString(userID, key, fmt.Sprintf("%d", value))
}

// ResolveOpenRouterKey returns the user's personal OpenRouter key if set,
// otherwise falls back to the server-wide OPENROUTER_API_KEY env var.
func ResolveOpenRouterKey(userID string) string {
	if userID != "" && userID != "admin" {
		if key := db.GetUserOpenrouterKey(userID); strings.TrimSpace(key) != "" {
			return strings.TrimSpace(key)
		}
	}
	return os.Getenv("OPENROUTER_API_KEY")
}

// ── Settings payload ──────────────────────────────────────────────────────────

// GetCurrentSettings returns settings for a given user. Pass "" or "admin" for global/admin view.
// User-specific keys (model, prompt, tokens) come from user_settings with global fallback.
// Platform keys (embedding, agentos) always come from global settings.
func GetCurrentSettings(userID string) SettingsPayload {
	// Per-user keys
	var featuredModels []string
	featuredBytes := getUserSettingString(userID, "featured_models", "[]")
	json.Unmarshal([]byte(featuredBytes), &featuredModels)

	defaultModel := getUserSettingString(userID, "default_model", "")
	systemPrompt := getUserSettingString(userID, "system_prompt", "You are Apollo, a core intelligent system. You are helpful, direct, and concise.")
	autoCompactTokens := getUserSettingInt(userID, "auto_compact_tokens", 80000)

	// Global/platform keys
	embeddingApiUrl := getSettingString("embedding_api_url", "http://localhost:11434")
	embeddingModel := getSettingString("embedding_model", "nomic-embed-text")

	var managedProjects []ProjectConfig
	managedProjectsBytes := getSettingString("managed_projects", "[]")
	json.Unmarshal([]byte(managedProjectsBytes), &managedProjects)

	agentOSEnabled := getSettingBool("agentos_enabled", true)
	agentOSPolicyEnforcement := getSettingString("agentos_policy_enforcement", "deny_default")
	agentOSKillSwitch := getSettingBool("agentos_kill_switch", false)

	return SettingsPayload{
		BearerToken:              "",
		FeaturedModels:           featuredModels,
		DefaultModel:             defaultModel,
		EmbeddingApiUrl:          embeddingApiUrl,
		EmbeddingModel:           embeddingModel,
		SystemPrompt:             systemPrompt,
		ManagedProjects:          managedProjects,
		AutoCompactTokens:        autoCompactTokens,
		AgentOSEnabled:           agentOSEnabled,
		AgentOSPolicyEnforcement: agentOSPolicyEnforcement,
		AgentOSKillSwitch:        agentOSKillSwitch,
	}
}

// Handler for fetching and updating app settings
func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	userID := CurrentUserID(r)

	if r.Method == http.MethodGet {
		payload := GetCurrentSettings(userID)
		json.NewEncoder(w).Encode(payload)
		return
	}

	if r.Method == http.MethodPost {
		payload := GetCurrentSettings(userID)
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Per-user settings — any authenticated user can set these
		if payload.FeaturedModels != nil {
			bytes, _ := json.Marshal(payload.FeaturedModels)
			setUserSettingString(userID, "featured_models", string(bytes))
		}
		setUserSettingString(userID, "default_model", payload.DefaultModel)
		setUserSettingString(userID, "system_prompt", payload.SystemPrompt)
		setUserSettingInt(userID, "auto_compact_tokens", payload.AutoCompactTokens)

		// Platform settings — admin only
		if userID == "admin" {
			setSettingString("embedding_api_url", payload.EmbeddingApiUrl)
			setSettingString("embedding_model", payload.EmbeddingModel)
			setSettingString("agentos_enabled", fmt.Sprintf("%t", payload.AgentOSEnabled))
			setSettingString("agentos_policy_enforcement", payload.AgentOSPolicyEnforcement)
			setSettingString("agentos_kill_switch", fmt.Sprintf("%t", payload.AgentOSKillSwitch))
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
