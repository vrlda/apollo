package handlers

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
	"github.com/danilrybalkin/apollo-dash/tools"
	"github.com/google/uuid"
)

type ChatSession struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Model           string `json:"model"`
	ProjectPath     string `json:"project_path"`
	ReasoningEffort string `json:"reasoning_effort"`
	ExecutionMode   string `json:"execution_mode"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Attachment struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Content string `json:"content,omitempty"`
}

type AiChatRequest struct {
	SessionID   string       `json:"sessionId"`
	Model       string       `json:"model"`
	Message     string       `json:"message"`
	Mode        string       `json:"mode"`
	Attachments []Attachment `json:"attachments"`
}

const (
	reasoningTierNone     = "none"
	reasoningTierStandard = "standard"
	reasoningTierHigh     = "high"
)

func normalizeReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case reasoningTierHigh:
		return reasoningTierHigh
	case reasoningTierStandard:
		return reasoningTierStandard
	case "", reasoningTierNone:
		return reasoningTierNone
	default:
		return reasoningTierNone
	}
}

func displayReasoningEffort(value string) string {
	switch normalizeReasoningEffort(value) {
	case reasoningTierHigh:
		return "High"
	case reasoningTierStandard:
		return "Standard"
	default:
		return "None"
	}
}

func isHighReasoningEffort(value string) bool {
	return normalizeReasoningEffort(value) == reasoningTierHigh
}

func usesReasoningDirective(value string) bool {
	tier := normalizeReasoningEffort(value)
	return tier == reasoningTierStandard || tier == reasoningTierHigh
}

// Handler for fetching existing sessions
func AiSessionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		projectPath := r.URL.Query().Get("project_path")
		query := "SELECT id, title, IFNULL(model, ''), IFNULL(project_path, ''), IFNULL(reasoning_effort, 'None'), IFNULL(execution_mode, 'Plan'), created_at, updated_at FROM chat_sessions ORDER BY updated_at DESC"
		var rows *sql.Rows
		var err error

		if projectPath != "" {
			query = "SELECT id, title, IFNULL(model, ''), IFNULL(project_path, ''), IFNULL(reasoning_effort, 'None'), IFNULL(execution_mode, 'Plan'), created_at, updated_at FROM chat_sessions WHERE project_path = ? ORDER BY updated_at DESC"
			rows, err = db.DB.Query(query, projectPath)
		} else {
			query = "SELECT id, title, IFNULL(model, ''), IFNULL(project_path, ''), IFNULL(reasoning_effort, 'None'), IFNULL(execution_mode, 'Plan'), created_at, updated_at FROM chat_sessions WHERE project_path = '' OR project_path IS NULL ORDER BY updated_at DESC"
			rows, err = db.DB.Query(query)
		}

		if err != nil {
			http.Error(w, "Failed to fetch sessions", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var sessions []ChatSession
		for rows.Next() {
			var s ChatSession
			if err := rows.Scan(&s.ID, &s.Title, &s.Model, &s.ProjectPath, &s.ReasoningEffort, &s.ExecutionMode, &s.CreatedAt, &s.UpdatedAt); err != nil {
				continue
			}
			s.ReasoningEffort = displayReasoningEffort(s.ReasoningEffort)
			sessions = append(sessions, s)
		}
		if sessions == nil {
			sessions = []ChatSession{}
		}
		json.NewEncoder(w).Encode(sessions)
		return
	}

	if r.Method == http.MethodPost {
		var reqBody struct {
			ProjectPath     string `json:"project_path"`
			ReasoningEffort string `json:"reasoning_effort"`
			ExecutionMode   string `json:"execution_mode"`
		}
		json.NewDecoder(r.Body).Decode(&reqBody)

		reqBody.ReasoningEffort = displayReasoningEffort(reqBody.ReasoningEffort)
		if reqBody.ExecutionMode == "" {
			reqBody.ExecutionMode = "Plan"
		}

		id := uuid.New().String()
		title := "New Chat"
		_, err := db.DB.Exec("INSERT INTO chat_sessions (id, title, project_path, reasoning_effort, execution_mode) VALUES (?, ?, ?, ?, ?)", id, title, reqBody.ProjectPath, reqBody.ReasoningEffort, reqBody.ExecutionMode)
		if err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(ChatSession{
			ID:              id,
			Title:           title,
			ProjectPath:     reqBody.ProjectPath,
			ReasoningEffort: reqBody.ReasoningEffort,
			ExecutionMode:   reqBody.ExecutionMode,
		})
		return
	}

	if r.Method == http.MethodPut {
		var reqBody struct {
			ID              string `json:"id"`
			Model           string `json:"model"`
			ReasoningEffort string `json:"reasoning_effort"`
			ExecutionMode   string `json:"execution_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		reqBody.ReasoningEffort = displayReasoningEffort(reqBody.ReasoningEffort)

		// Update only the provided fields.
		// For simplicity, we assume frontend sends the full new state.
		_, err := db.DB.Exec("UPDATE chat_sessions SET model = ?, reasoning_effort = ?, execution_mode = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
			reqBody.Model, reqBody.ReasoningEffort, reqBody.ExecutionMode, reqBody.ID)
		if err != nil {
			http.Error(w, "Failed to update session", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
		return
	}

	if r.Method == http.MethodDelete {
		sid := r.URL.Query().Get("id")
		if sid == "" {
			http.Error(w, "Session ID required", http.StatusBadRequest)
			return
		}
		_, err := db.DB.Exec("DELETE FROM chat_sessions WHERE id = ?", sid) // cascades to messages
		if err != nil {
			http.Error(w, "Failed to delete session", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Fetch messages for a specific session
func AiMessagesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sid := r.URL.Query().Get("sessionId")
	if sid == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query("SELECT role, content FROM chat_messages WHERE session_id = ? ORDER BY id ASC", sid)
	if err != nil {
		http.Error(w, "Failed to fetch messages", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			continue
		}
		if m.Role == "assistant" {
			systemLogRe := regexp.MustCompile("(?is)````system_log[\\s\\S]*?````")
			m.Content = strings.TrimSpace(systemLogRe.ReplaceAllString(m.Content, ""))
			if strings.Contains(m.Content, "[Asked User:") && !strings.Contains(m.Content, "````question") {
				askedRe := regexp.MustCompile(`(?is)\[Asked User:\s*(.*?)\]`)
				qtxt := ""
				if match := askedRe.FindStringSubmatch(m.Content); len(match) > 1 {
					qtxt = strings.TrimSpace(match[1])
				} else {
					lowered := strings.ToLower(m.Content)
					if idx := strings.Index(lowered, "[asked user:"); idx >= 0 {
						qtxt = strings.TrimSpace(m.Content[idx+len("[Asked User:"):])
						qtxt = strings.TrimSpace(strings.TrimSuffix(qtxt, "]"))
					}
				}
				if qtxt != "" {
					qPayload := map[string]interface{}{
						"question": qtxt,
						"options":  []string{"Answer in chat"},
					}
					qJSON, _ := json.Marshal(qPayload)
					if askedRe.MatchString(m.Content) {
						m.Content = askedRe.ReplaceAllString(m.Content, fmt.Sprintf("````question\n%s\n````", string(qJSON)))
					} else {
						m.Content = fmt.Sprintf("````question\n%s\n````", string(qJSON))
					}
				}
			}
		}
		msgs = append(msgs, m)
	}

	if msgs == nil {
		msgs = []ChatMessage{}
	}
	json.NewEncoder(w).Encode(msgs)
}

// Fetches available OpenRouter Models and Local Ollama Models
func AiModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	apiKey := ResolveOpenRouterKey(CurrentUserID(r))

	var orResp map[string]interface{}

	if apiKey != "" && apiKey != "your_openrouter_api_key_here" {
		req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			json.NewDecoder(resp.Body).Decode(&orResp)
		}
	}

	if orResp == nil {
		orResp = map[string]interface{}{"data": []interface{}{}}
	}

	dataList, ok := orResp["data"].([]interface{})
	if !ok {
		dataList = []interface{}{}
	}

	ollamaUrl := os.Getenv("OLLAMA_API_URL")
	if ollamaUrl != "" {
		req, _ := http.NewRequest("GET", ollamaUrl+"/api/tags", nil)
		client := &http.Client{Timeout: 2 * time.Second}
		if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			var ollamaResp struct {
				Models []struct {
					Name string `json:"name"`
				} `json:"models"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err == nil {
				for _, m := range ollamaResp.Models {
					localModel := map[string]interface{}{
						"id":   "local/" + m.Name,
						"name": "(Local) " + m.Name,
						"architecture": map[string]interface{}{
							// We can assume text, maybe add image if the ID matches llava
							"input_modalities": []string{"text"},
						},
					}
					// If the user's running a vision model like llava
					if strings.Contains(strings.ToLower(m.Name), "llava") {
						localModel["architecture"].(map[string]interface{})["input_modalities"] = []string{"text", "image"}
					}
					dataList = append([]interface{}{localModel}, dataList...)
				}
			}
		}
	}

	// Filter by Featured Models unless ?all=1 is passed
	settings := GetCurrentSettings(CurrentUserID(r))
	if r.URL.Query().Get("all") != "1" && len(settings.FeaturedModels) > 0 {
		featuredMap := make(map[string]bool)
		for _, fm := range settings.FeaturedModels {
			featuredMap[fm] = true
		}

		var filteredList []interface{}
		for _, m := range dataList {
			mMap, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			idStr, _ := mMap["id"].(string)
			if featuredMap[idStr] {
				filteredList = append(filteredList, m)
			}
		}
		dataList = filteredList
	}

	orResp["data"] = dataList
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orResp)
}

// Constructs the master personality block from Settings and SQLite Facts
func ConstructPersonalityPrompt(userID string, mode string, reasoningEffort string, executionMode string, projectPath string) string {
	settings := GetCurrentSettings(userID)
	var builder strings.Builder

	builder.WriteString(settings.SystemPrompt)
	builder.WriteString("\n\n")

	rows, err := db.DB.Query("SELECT fact FROM personality_facts ORDER BY id ASC")
	if err == nil {
		var facts []string
		for rows.Next() {
			var fact string
			if err := rows.Scan(&fact); err == nil {
				facts = append(facts, fact)
			}
		}
		rows.Close()

		if len(facts) > 0 {
			builder.WriteString("<core_memories>\n")
			builder.WriteString("You have permanently memorized the following facts across all sessions:\n")
			for _, f := range facts {
				builder.WriteString("- " + f + "\n")
			}
			builder.WriteString("</core_memories>\n\n")
		}
	}

	builder.WriteString("CORE DIRECTIVE: If the user explicitly tells you something you should remember permanently, or you establish a permanent detail about your persona, output it anywhere in your response wrapped in `<remember>fact here</remember>` tags so it can be committed to your core memory bank.\n\n")
	builder.WriteString(fmt.Sprintf("SYSTEM CONTEXT: The current local server time is %s.\n\n", time.Now().Format("Monday, 02 Jan 2006 15:04:05 MST")))

	if skillsManifest := tools.GetSkillsManifest(); skillsManifest != "" {
		builder.WriteString(skillsManifest)
		builder.WriteString("\n\n")
	}

	if usesReasoningDirective(reasoningEffort) {
		builder.WriteString(`
=========================================
MANDATORY REASONING DIRECTIVE
=========================================
Before answering any request, you MUST engage in deep, step-by-step Chain-of-Thought reasoning.
You must break down the problem, analyze constraints, and explore potential solutions. 
If during your reasoning you realize a step is flawed, you must explicitly write out your Self-Correction and pivot your approach.
This ensures highly accurate, logical, and thoroughly vetted intelligent responses.

You MUST enclose your internal thought process inside a markdown code block with the language "reasoning".
Example:
` + "````" + `reasoning
1. First I need to analyze the user's request...
2. The user wants X. Let's break this down...
Wait, if I do X, it might break Y. Let me self-correct and approach this via Z instead...
` + "````" + `

After closing the reasoning block, output your final, perfectly formatted direct response to the user.
`)
	}
	if mode == "coding" {
		builder.WriteString("\n\nMODE: CODING (ACTIVE)\n")
		builder.WriteString(fmt.Sprintf("\nWORKING DIRECTORY: %s\n", projectPath))
		builder.WriteString("You are currently in autonomous coding mode. Your primary objective is to execute tasks using your tools. Do not just talk—ACT!\n")
		builder.WriteString(`
AGENTIC LOOP:
Work through every task in three blended phases — chain as many tool calls as needed:
1. GATHER: Understand the codebase first. Read files, list directories, grep for patterns, find files, check types. Never skip this phase on unfamiliar code.
2. ACT: Make changes. Edit files, write new files, rename/move things, run commands, run tests. One action per tool call — be surgical and precise.
3. VERIFY: After every change, confirm it worked. Run the relevant check_code, execute tests, read the modified file back, diff it. If verification fails, loop back to ACT with the corrected approach.

SKILL-BASED LEARNING:
You have a dynamic skill library. When a skill is needed, call load_skill(name) to load its full content. Skills provide deep, specialized knowledge on demand without bloating every conversation.

TOOL DISCIPLINE:
- After ANY file edit, immediately call check_code on the modified file to catch errors.
- After ANY execute_command that should produce output, verify the output matches expectations.
- If a test fails, re-read the relevant source files before attempting a fix — never guess.
- Chain dozens of tool calls confidently. Stop only when the task is fully verified complete.
- If you receive a user message mid-task, STOP immediately and address it before continuing.
`)
		builder.WriteString(`
PLANNING DIRECTIVE:
If a user request requires a complex or multi-component build (like "create a web app"), DO NOT guess the architecture, tech stack, or design preferences. 
Instead, FIRST use the 'ask_user_question' tool to prompt the user for their preferences (e.g. asking them to choose between React/Vue, or asking about a specific visual style).
You can ask as many questions as you need using the 'ask_user_question' tool. Wait for their answers.

Once you have gathered enough context, you must draft a concrete, self-directed architecture spec.
Then, ALWAYS propose an execution plan.
Before the plan block, include a short architecture summary (3-6 bullets).
The FINAL content of your message MUST be exactly one markdown code block with language "plan" containing ONLY valid JSON. Nothing should appear after that plan block.
` + "````" + `plan
{"title":"<concise plan title>","summary":"<1-2 sentence summary>","steps":[{"index":0,"label":"<step title>","details":"<what/why>","validation":"<how we verify>","status":"pending"}]}
` + "````" + `
After outputting the plan block, stop and wait for the user to respond.
If the user responds with approval (for example "yes proceed"), treat it as PLAN_APPROVED and execute each step in sequence.
After completing each step, output a plan step update:
` + "````" + `step_update
{"planId": "<same plan title>", "index": <step_index>, "status": "done"}
` + "````" + `
If the user suggests changes instead, revise the plan summary + JSON and output a full new plan block (same format).

SUBAGENT DIRECTIVE:
You have access to a spawn_subagent tool. You should PROACTIVELY decide to use it — you do NOT need to be asked.
Spawn a background subagent when any of the following is true:
- The task involves deep research (e.g. "find the best X", "compare Y options", "analyze Z codebase")
- The task can be parallelized safely (e.g. "review all these files" → spawn one reviewer per major file/module)
- The task is expected to take many tool calls and would block the main conversation for a long time
- The user's main task benefits from concurrent validation, testing, or fact-checking
When spawning: use a descriptive name (e.g. "Researcher-1", "CodeReviewer", "Tester"), write a precise self-contained task description, and ALWAYS pass the current session_id so the user gets notified when it finishes.
After spawning, immediately tell the user you've spawned an agent and continue the main thread without waiting.
`)
		if !isGitWorkspace(projectPath) {
			builder.WriteString(`
GITLESS WORKSPACE OVERRIDE:
- This workspace is not a git repository.
- Do NOT ask the user to create branches, commits, pull requests, or run git commands.
- Do NOT rely on worktrees or PR-based orchestration.
- Execute tasks through direct file edits + validation commands only.
`)
		}
	} else if mode == "talking" {
		builder.WriteString("\n\nMODE: TALKING (ACTIVE)\n")
		builder.WriteString("You are currently in conversational talking mode. Your objective is communication, brainstorming, or clarification. Only use tools if strictly necessary for answering a question. If asked to write or edit code, provide the code snippets directly in your response chat without triggering actual file system edits.\n")
	}

	return builder.String()
}

func isGitWorkspace(projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return false
	}
	gitPath := filepath.Join(projectPath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	// .git can be a directory or a file (worktree/submodule pointer)
	return info.IsDir() || info.Mode().IsRegular()
}

func extractQuestionFromAssistantContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	questionBlockRe := regexp.MustCompile("(?is)````question\\s*([\\s\\S]*?)\\s*````")
	if blocks := questionBlockRe.FindAllStringSubmatch(content, -1); len(blocks) > 0 {
		for i := len(blocks) - 1; i >= 0; i-- {
			raw := strings.TrimSpace(blocks[i][1])
			var payload struct {
				Question string `json:"question"`
			}
			if json.Unmarshal([]byte(raw), &payload) == nil {
				if q := strings.TrimSpace(payload.Question); q != "" {
					return q
				}
			}
		}
	}

	askedRe := regexp.MustCompile(`(?is)\[Asked User:\s*(.*?)\]`)
	if m := askedRe.FindStringSubmatch(content); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func hasPlanBlock(content string) bool {
	return regexp.MustCompile("(?is)````plan\\s*[\\s\\S]*?````").MatchString(content)
}

func looksLikePlanApproval(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(msg), "PLAN_APPROVED") {
		return true
	}
	disqualifiers := []string{"but", "except", "instead", "change", "edit", "revise", "?"}
	for _, bad := range disqualifiers {
		if strings.Contains(m, bad) {
			return false
		}
	}
	approvals := []string{
		"yes", "yep", "yeah", "proceed", "go ahead", "approved", "approve",
		"looks good", "ship it", "continue", "do it", "ok proceed", "you can proceed",
	}
	for _, token := range approvals {
		if strings.Contains(m, token) {
			return true
		}
	}
	return false
}

// Process Chat Generation and DB storage
func AiChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	apiKey := ResolveOpenRouterKey(CurrentUserID(r))
	ollamaUrl := os.Getenv("OLLAMA_API_URL")

	var chatReq AiChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if chatReq.SessionID == "" || chatReq.Message == "" {
		http.Error(w, "SessionID and Message are required", http.StatusBadRequest)
		return
	}

	sendSSEChunk := func(w http.ResponseWriter, content string) {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		chunk.Choices = append(chunk.Choices, struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		}{})
		chunk.Choices[0].Delta.Content = content
		b, _ := json.Marshal(chunk)
		w.Write([]byte("data: "))
		w.Write(b)
		w.Write([]byte("\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	extractAskedUserQuestion := func(input string) (string, bool) {
		re := regexp.MustCompile(`(?is)\[Asked User:\s*(.*?)\]`)
		m := re.FindStringSubmatch(input)
		if len(m) < 2 {
			return "", false
		}
		return strings.TrimSpace(m[1]), true
	}

	var reasoningEffort, executionMode, projectPath string

	// 1. Process documents first
	for _, att := range chatReq.Attachments {
		if att.Type == "file" {
			chatReq.Message += "\n\n<document name=\"" + att.Name + "\">\n" + att.Content + "\n</document>"
		}
	}

	// 2. Fetch past messages for this session
	questionBlockRe := regexp.MustCompile("(?is)````question\\s*([\\s\\S]*?)\\s*````")
	uiBlockRe := regexp.MustCompile("(?is)````(?:reasoning|tool_action|compacting|step_update|plan|checkpoint|terminal|system_log)[\\s\\S]*?````")
	rows, err := db.DB.Query("SELECT role, content FROM chat_messages WHERE session_id = ? ORDER BY id ASC", chatReq.SessionID)
	var historyRows []ChatMessage
	var openRouterMsgs []map[string]interface{}
	if err == nil {
		for rows.Next() {
			var role, content string
			rows.Scan(&role, &content)
			historyRows = append(historyRows, ChatMessage{Role: role, Content: content})
		}
		rows.Close()
	}

	var lastAssistantRaw string
	for i := len(historyRows) - 1; i >= 0; i-- {
		if historyRows[i].Role == "assistant" {
			lastAssistantRaw = historyRows[i].Content
			break
		}
	}
	lastAssistantQuestion := extractQuestionFromAssistantContent(lastAssistantRaw)
	lastAssistantHasPlan := hasPlanBlock(lastAssistantRaw)

	for _, hr := range historyRows {
		role := hr.Role
		content := hr.Content

		// Context Hygiene: Strip UI-only blocks from assistant history before sending to LLM
		if role == "assistant" {
			content = questionBlockRe.ReplaceAllString(content, "")
			content = uiBlockRe.ReplaceAllString(content, "")
			askedRe := regexp.MustCompile(`(?is)\[Asked User:\s*(.*?)\]`)
			content = askedRe.ReplaceAllString(content, "")
			re := regexp.MustCompile(`(?is)<remember>.*?</remember>`)
			content = re.ReplaceAllString(content, "")
			content = strings.TrimSpace(content)
		}

		msgObj := map[string]interface{}{"role": role}
		if len(content) > 0 && content[0] == '[' {
			var contentArr []map[string]interface{}
			if err := json.Unmarshal([]byte(content), &contentArr); err == nil {
				msgObj["content"] = contentArr
			} else {
				msgObj["content"] = content
			}
		} else {
			msgObj["content"] = content
		}
		openRouterMsgs = append(openRouterMsgs, msgObj)
	}

	effectiveUserMessage := chatReq.Message
	var orchestrationHints []string
	if lastAssistantQuestion != "" && strings.TrimSpace(chatReq.Message) != "" {
		orchestrationHints = append(orchestrationHints, fmt.Sprintf("The assistant previously asked the user: %q. The user's latest message is the answer: %q. Treat it as answered and continue. Do NOT repeat the same question unless the user's answer is empty/ambiguous.", lastAssistantQuestion, chatReq.Message))
	}
	if lastAssistantHasPlan {
		if looksLikePlanApproval(chatReq.Message) {
			effectiveUserMessage = "PLAN_APPROVED"
			orchestrationHints = append(orchestrationHints, "The user approved the latest plan. Start execution now. Do not ask a new clarification question unless a hard blocker appears.")
		} else if strings.TrimSpace(chatReq.Message) != "" {
			orchestrationHints = append(orchestrationHints, "The user requested plan changes. Produce a fully revised plan with updated summary and detailed steps, and place the plan block at the end of the message.")
		}
	}

	// 3. Construct new user message content
	var imageAttachments []map[string]interface{}
	for _, att := range chatReq.Attachments {
		if att.Type == "image" {
			imageAttachments = append(imageAttachments, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]string{
					"url": att.Data,
				},
			})
		}
	}

	var dbContentStr string
	if len(imageAttachments) > 0 {
		var contentArr []map[string]interface{}
		contentArr = append(contentArr, map[string]interface{}{
			"type": "text",
			"text": effectiveUserMessage,
		})
		for _, img := range imageAttachments {
			contentArr = append(contentArr, img)
		}

		// For OpenRouter
		openRouterMsgs = append(openRouterMsgs, map[string]interface{}{"role": "user", "content": contentArr})

		// For SQLite
		b, _ := json.Marshal(contentArr)
		dbContentStr = string(b)
	} else {
		// Normal text
		openRouterMsgs = append(openRouterMsgs, map[string]interface{}{"role": "user", "content": effectiveUserMessage})
		dbContentStr = chatReq.Message
	}

	// 4. Save new user message to DB
	db.DB.Exec("INSERT INTO chat_messages (session_id, role, content) VALUES (?, 'user', ?)", chatReq.SessionID, dbContentStr)
	db.DB.Exec("UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", chatReq.SessionID)

	// If this is the FIRST message in a session (len == 1), update the title and model automatically
	if len(openRouterMsgs) == 1 {
		title := chatReq.Message
		if len(title) > 30 {
			title = title[:27] + "..."
		}
		db.DB.Exec("UPDATE chat_sessions SET title = ?, model = ? WHERE id = ?", title, chatReq.Model, chatReq.SessionID)
	}

	// Load session runtime mode/config once and reuse below.
	err = db.DB.QueryRow("SELECT IFNULL(reasoning_effort, 'None'), IFNULL(execution_mode, 'Plan'), IFNULL(project_path, '') FROM chat_sessions WHERE id = ?", chatReq.SessionID).Scan(&reasoningEffort, &executionMode, &projectPath)
	if err != nil {
		reasoningEffort = "None"
		executionMode = "Plan"
		projectPath = ""
	}
	reasoningEffort = displayReasoningEffort(reasoningEffort)
	gitWorkspace := isGitWorkspace(projectPath)
	if !gitWorkspace && strings.EqualFold(chatReq.Mode, "coding") {
		orchestrationHints = append(orchestrationHints, "Environment note: this project is gitless. Do not ask for git actions or PR flow; continue with direct code edits and local validation only.")
	}

	if len(orchestrationHints) > 0 {
		for i := len(orchestrationHints) - 1; i >= 0; i-- {
			openRouterMsgs = append([]map[string]interface{}{
				{"role": "system", "content": orchestrationHints[i]},
			}, openRouterMsgs...)
		}
	}

	if (apiKey == "" || apiKey == "your_openrouter_api_key_here") && ollamaUrl == "" {
		http.Error(w, `{"error": "No AI Providers configured (Missing OpenRouter API Key and OLLAMA_API_URL)"}`, http.StatusServiceUnavailable)
		return
	}

	// Persist assistant output in DB incrementally so reloads can recover ongoing work.
	var assistantRowID int64
	if res, err := db.DB.Exec("INSERT INTO chat_messages (session_id, role, content) VALUES (?, 'assistant', '')", chatReq.SessionID); err == nil {
		if id, idErr := res.LastInsertId(); idErr == nil {
			assistantRowID = id
		}
	}
	lastPersistedAssistant := ""
	persistAssistant := func(content string) {
		if content == lastPersistedAssistant {
			return
		}
		lastPersistedAssistant = content
		if assistantRowID > 0 {
			db.DB.Exec("UPDATE chat_messages SET content = ? WHERE id = ?", content, assistantRowID)
		} else {
			db.DB.Exec("INSERT INTO chat_messages (session_id, role, content) VALUES (?, 'assistant', ?)", chatReq.SessionID, content)
		}
		db.DB.Exec("UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", chatReq.SessionID)
	}

	// 4. Send request to either Local Ollama or OpenRouter
	model := chatReq.Model
	var targetUrl string
	var reqApiKey string

	if strings.HasPrefix(model, "local/") {
		if ollamaUrl == "" {
			http.Error(w, "OLLAMA_API_URL is not configured", http.StatusBadRequest)
			return
		}
		model = strings.TrimPrefix(model, "local/")
		targetUrl = ollamaUrl + "/v1/chat/completions"
		reqApiKey = "Bearer local"
	} else {
		if model == "" {
			settings := GetCurrentSettings(CurrentUserID(r))
			if settings.DefaultModel != "" {
				model = settings.DefaultModel
			} else {
				model = "meta-llama/llama-3-8b-instruct:free" // fallback
			}
		}
		targetUrl = "https://openrouter.ai/api/v1/chat/completions"
		reqApiKey = "Bearer " + apiKey
	}

	// 5. Inject RAG Episodes
	episodes := SearchEpisodes(chatReq.Message, chatReq.SessionID)
	if episodes != "" {
		sysMsg := map[string]interface{}{"role": "system", "content": episodes}
		openRouterMsgs = append([]map[string]interface{}{sysMsg}, openRouterMsgs...)
		log.Println("Memory System: Injected chronological memory RAG block into context stream.")
	}

	// 5. Inject Global Personality Prompt
	personaPrompt := ConstructPersonalityPrompt(CurrentUserID(r), chatReq.Mode, reasoningEffort, executionMode, projectPath)
	if personaPrompt != "" {
		sysMsg := map[string]interface{}{"role": "system", "content": personaPrompt}
		openRouterMsgs = append([]map[string]interface{}{sysMsg}, openRouterMsgs...)
	}

	// --- SMART CONTEXT COMPRESSION ---
	// Estimate total token usage. If over threshold, compress the middle of the conversation.
	{
		totalChars := 0
		for _, msg := range openRouterMsgs {
			if c, ok := msg["content"].(string); ok {
				totalChars += len(c)
			}
		}
		estimatedTokens := totalChars / 4

		settings := GetCurrentSettings(CurrentUserID(r))
		compressionThreshold := settings.AutoCompactTokens
		if compressionThreshold <= 0 {
			compressionThreshold = 80000
		}

		if estimatedTokens > compressionThreshold && len(openRouterMsgs) > 6 {
			log.Printf("Context Compression: Estimated %d tokens — compressing history.", estimatedTokens)

			// Stream compacting indicator to UI
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			var compactChunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			compactChunk.Choices = append(compactChunk.Choices, struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{})
			compactChunk.Choices[0].Delta.Content = "````compacting\nCompacting context...\n````\n\n"
			compactB, _ := json.Marshal(compactChunk)
			w.Write([]byte("data: "))
			w.Write(compactB)
			w.Write([]byte("\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			// Build a compression prompt from the middle of the conversation
			sysMessages := []map[string]interface{}{}
			var histBuilder strings.Builder
			for _, msg := range openRouterMsgs {
				if role, ok := msg["role"].(string); ok && role == "system" {
					sysMessages = append(sysMessages, msg)
					continue
				}
				if c, ok := msg["content"].(string); ok {
					role, _ := msg["role"].(string)
					histBuilder.WriteString(fmt.Sprintf("%s: %s\n\n", role, c))
				}
			}
			compressionMsgs := []map[string]interface{}{
				{"role": "system", "content": "You are a context compression engine. Summarize the following conversation history into a dense, factual memory block. Preserve all key decisions, code changes, files edited, commands run, and important context. Output plain text only."},
				{"role": "user", "content": histBuilder.String()},
			}
			compressPayload, _ := json.Marshal(map[string]interface{}{
				"model":    model,
				"messages": compressionMsgs,
				"stream":   false,
			})
			cReq, _ := http.NewRequest("POST", targetUrl, bytes.NewBuffer(compressPayload))
			cReq.Header.Set("Authorization", reqApiKey)
			cReq.Header.Set("Content-Type", "application/json")
			cClient := &http.Client{Timeout: 60 * time.Second}
			cResp, cErr := cClient.Do(cReq)
			if cErr == nil && cResp.StatusCode == 200 {
				var cResult struct {
					Choices []struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					} `json:"choices"`
				}
				json.NewDecoder(cResp.Body).Decode(&cResult)
				cResp.Body.Close()
				if len(cResult.Choices) > 0 {
					summaryContent := cResult.Choices[0].Message.Content
					summaryMsg := map[string]interface{}{
						"role":    "system",
						"content": "<compressed_history>\n" + summaryContent + "\n</compressed_history>",
					}
					// Rebuild: system messages + compressed summary + last 4 messages
					lastN := openRouterMsgs
					if len(lastN) > 4 {
						lastN = lastN[len(lastN)-4:]
					}
					openRouterMsgs = append(sysMessages, summaryMsg)
					openRouterMsgs = append(openRouterMsgs, lastN...)
					log.Println("Context Compression: History compressed successfully.")
				}
			}
		}
	}

	// --- TEST-TIME COMPUTE ORCHESTRATOR (HIGH EFFORT) ---
	// --- TEST-TIME COMPUTE ORCHESTRATOR (HIGH EFFORT) ---
	if isHighReasoningEffort(reasoningEffort) {
		log.Println("Test-Time Compute: High Effort Orchestrator Initiated.")

		orchestratorStartTime := time.Now()

		// Phase 1: Decomposition
		decompMsgs := []map[string]interface{}{
			{"role": "system", "content": "You are an analytical Engine. The user has submitted a prompt. Your ONLY objective is to break their request down into a sequential array of abstract reasoning steps required to perfectly solve it. If the request requires any factual lookup, explicitly include steps to search for and double-check those facts. Output ONLY a valid JSON array of strings, nothing else. Example: [\"Analyze constraints\", \"Search for facts\", \"Critique logic\", \"Finalize\"]"},
			{"role": "user", "content": chatReq.Message},
		}

		decompPayload, _ := json.Marshal(map[string]interface{}{
			"model":    model,
			"messages": decompMsgs,
			"stream":   false,
		})

		dReq, _ := http.NewRequest("POST", targetUrl, bytes.NewBuffer(decompPayload))
		dReq.Header.Set("Authorization", reqApiKey)
		dReq.Header.Set("Content-Type", "application/json")
		dClient := &http.Client{Timeout: 60 * time.Second}
		dResp, dErr := dClient.Do(dReq)

		var steps []string
		if dErr == nil && dResp.StatusCode == 200 {
			var dResult struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			json.NewDecoder(dResp.Body).Decode(&dResult)
			dResp.Body.Close()

			if len(dResult.Choices) > 0 {
				rawJson := strings.TrimPrefix(dResult.Choices[0].Message.Content, "```json")
				rawJson = strings.TrimPrefix(rawJson, "```")
				rawJson = strings.TrimSuffix(rawJson, "```")
				json.Unmarshal([]byte(strings.TrimSpace(rawJson)), &steps)
			}
		}

		if len(steps) == 0 {
			steps = []string{"Analyze the request logically", "Determine the correct response carefully"}
		}

		// Phase 2: Orchestration
		var fullThoughtChain strings.Builder
		fullThoughtChain.WriteString("<thought_process>\n")

		sendSSEChunk(w, "````reasoning\n")

		orchestratorContext := append([]map[string]interface{}(nil), openRouterMsgs...)

		for i, step := range steps {
			fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf("\n> **Step %d:** %s\n", i+1, step))
			w.(http.Flusher).Flush()

			stepPrompt := fmt.Sprintf("You are currently executing Step %d of your reasoning plan: '%s'.\n\nPlease output your internal thoughts processing *only* this step. You must gather facts and double-check logic. Do not execute the final answer yet. Just think aloud.", i+1, step)
			orchestratorContext = append(orchestratorContext, map[string]interface{}{"role": "user", "content": stepPrompt})

			stepPayload, _ := json.Marshal(map[string]interface{}{
				"model":    model,
				"messages": orchestratorContext,
				"stream":   false,
				"tools":    tools.GetAvailableTools(), // Give reasoning mid-thought access
			})
			sReq, _ := http.NewRequest("POST", targetUrl, bytes.NewBuffer(stepPayload))
			sReq.Header.Set("Authorization", reqApiKey)
			sReq.Header.Set("Content-Type", "application/json")
			sResp, sErr := dClient.Do(sReq)

			if sErr == nil && sResp.StatusCode == 200 {
				var sResult struct {
					Choices []struct {
						Message struct {
							Content   string `json:"content"`
							ToolCalls []struct {
								Id       string `json:"id"`
								Type     string `json:"type"`
								Function struct {
									Name      string `json:"name"`
									Arguments string `json:"arguments"`
								} `json:"function"`
							} `json:"tool_calls"`
						} `json:"message"`
					} `json:"choices"`
				}
				json.NewDecoder(sResp.Body).Decode(&sResult)
				sResp.Body.Close()

				if len(sResult.Choices) > 0 {
					msg := sResult.Choices[0].Message

					// Handle Mid-Reasoning Tool Use!
					if len(msg.ToolCalls) > 0 {
						for _, tc := range msg.ToolCalls {
							var args map[string]interface{}
							json.Unmarshal([]byte(tc.Function.Arguments), &args)

							sendSSEChunk(w, fmt.Sprintf("*(Calling tool %s to gather facts...)*\n", tc.Function.Name))

							toolRes := tools.ExecuteTool(tc.Function.Name, tc.Function.Arguments, projectPath)

							// Inject back into step
							orchestratorContext = append(orchestratorContext, map[string]interface{}{
								"role": "assistant", "content": "", "tool_calls": msg.ToolCalls,
							})
							orchestratorContext = append(orchestratorContext, map[string]interface{}{
								"role": "tool", "content": toolRes, "tool_call_id": tc.Id,
							})
						}
						// We don't formally re-loop here to keep it simple; we just inject the tools so it has them in Phase 4 Synthesis!
					}

					thought := msg.Content
					if thought != "" {
						orchestratorContext = append(orchestratorContext, map[string]interface{}{"role": "assistant", "content": thought})
						fullThoughtChain.WriteString(fmt.Sprintf("-- Step %d: %s --\n%s\n\n", i+1, step, thought))

						sendSSEChunk(w, thought)
					}
				}
			}
		}

		elapsedSecs := time.Since(orchestratorStartTime).Seconds()
		fullThoughtChain.WriteString("</thought_process>")

		sendSSEChunk(w, fmt.Sprintf("\n\n[Completed in %.1fs]\n````\n\n", elapsedSecs))

		// Phase 4: Synthesis
		// Prepend the entire massively generated thought process to the user's final message in the Tool Loop
		lastUserIdx := len(openRouterMsgs) - 1
		if openRouterMsgs[lastUserIdx]["role"] == "user" {
			openRouterMsgs[lastUserIdx]["content"] = fmt.Sprintf("%s\n\nUser Request: %s", fullThoughtChain.String(), chatReq.Message)
		}
	}

	// --- MAIN TOOL LOOP ---
	// Models can call multiple tools in sequence. We loop until the model ceases calling tools.
	debugSystemLogs := strings.EqualFold(strings.TrimSpace(os.Getenv("APOLLO_DEBUG_SYSTEM_LOG")), "1") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("APOLLO_DEBUG_SYSTEM_LOG")), "true")
	timelineResponse := ""
	for attempt := 0; attempt < 20; attempt++ {
		// Log the prompt to the system log panel
		if chatReq.Mode == "coding" && debugSystemLogs {
			promptLog, _ := json.MarshalIndent(openRouterMsgs, "", "  ")
			sendSSEChunk(w, fmt.Sprintf("````system_log\n[LLM PROMPT - Turn %d]\n%s\n````\n", attempt+1, string(promptLog)))
		}

		payload, _ := json.Marshal(map[string]interface{}{
			"model":    model,
			"messages": openRouterMsgs,
			"stream":   true,
			"tools":    tools.GetAvailableTools(),
		})

		req, _ := http.NewRequest("POST", targetUrl, bytes.NewBuffer(payload))
		req.Header.Set("Authorization", reqApiKey)
		req.Header.Set("Content-Type", "application/json")
		if !strings.HasPrefix(chatReq.Model, "local/") {
			req.Header.Set("X-Title", "Apollo Dashboard")
		}

		client := &http.Client{Timeout: 300 * time.Second}
		resp, err := client.Do(req)

		if err != nil {
			if attempt == 0 {
				http.Error(w, "Upstream AI error", http.StatusBadGateway)
			}
			return
		}

		if resp.StatusCode != 200 {
			if attempt == 0 {
				w.WriteHeader(resp.StatusCode)
				io.Copy(w, resp.Body)
			}
			resp.Body.Close()
			return
		}

		if attempt == 0 {
			// Only send headers on the very first loop before stream begins
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
		}

		flusher, ok := w.(http.Flusher)
		if !ok && attempt == 0 {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			resp.Body.Close()
			return
		}

		reader := bufio.NewReader(resp.Body)
		var fullResponse string
		var toolCalls []map[string]interface{}
		var hiddenBuffer string
		askedLeakBuffer := ""
		inAskedLeak := false

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				break
			}

			// Parse strictly to see if this is a tool payload or a text payload
			cleanLine := bytes.TrimSpace(line)
			isToolChunk := false

			if bytes.HasPrefix(cleanLine, []byte("data: ")) && string(cleanLine) != "data: [DONE]" {
				data := bytes.TrimPrefix(cleanLine, []byte("data: "))
				var chunk struct {
					Choices []struct {
						Delta struct {
							Content          string                   `json:"content"`
							ReasoningContent string                   `json:"reasoning_content"`
							Thought          string                   `json:"thought"`
							ToolCalls        []map[string]interface{} `json:"tool_calls"`
						} `json:"delta"`
					} `json:"choices"`
				}

				if err := json.Unmarshal(data, &chunk); err == nil && len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta
					if len(delta.ToolCalls) > 0 {
						isToolChunk = true

						// Buffer tool calls (SSE models stream JSON args in chunks)
						for _, tc := range delta.ToolCalls {
							idxFloat, ok := tc["index"].(float64)
							if !ok {
								continue
							}
							idx := int(idxFloat)

							for len(toolCalls) <= idx {
								toolCalls = append(toolCalls, make(map[string]interface{}))
							}

							if id, exists := tc["id"].(string); exists {
								toolCalls[idx]["id"] = id
							}
							if function, exists := tc["function"].(map[string]interface{}); exists {
								if _, fExists := toolCalls[idx]["function"]; !fExists {
									toolCalls[idx]["function"] = make(map[string]interface{})
								}
								funcPtr := toolCalls[idx]["function"].(map[string]interface{})

								if n, nExists := function["name"].(string); nExists {
									funcPtr["name"] = n
								}
								if argChunk, argExists := function["arguments"].(string); argExists {
									if existingArgs, ex := funcPtr["arguments"].(string); ex {
										funcPtr["arguments"] = existingArgs + argChunk
									} else {
										funcPtr["arguments"] = argChunk
									}
								}
							}
						}
					} else if delta.Content != "" || delta.ReasoningContent != "" || delta.Thought != "" {
						combinedText := delta.Content
						if delta.ReasoningContent != "" {
							combinedText = "````reasoning\n" + delta.ReasoningContent + "\n````\n" + combinedText
						} else if delta.Thought != "" {
							combinedText = "````reasoning\n" + delta.Thought + "\n````\n" + combinedText
						}

						// Safety filters to prevent internal/debug leakage in user-visible chat.
						if strings.Contains(strings.ToLower(combinedText), "[llm prompt - turn") {
							combinedText = ""
						}
						sysLogInlineRe := regexp.MustCompile("(?is)````system_log[\\s\\S]*?````")
						combinedText = sysLogInlineRe.ReplaceAllString(combinedText, "")

						// Convert legacy "[Asked User: ...]" leakage into first-class question block.
						if inAskedLeak || strings.Contains(combinedText, "[Asked User:") {
							inAskedLeak = true
							askedLeakBuffer += combinedText
							if strings.Contains(askedLeakBuffer, "]") {
								if qtxt, ok := extractAskedUserQuestion(askedLeakBuffer); ok && qtxt != "" {
									qPayload := map[string]interface{}{
										"question": qtxt,
										"options":  []string{"Answer in chat"},
									}
									qJSON, _ := json.Marshal(qPayload)
									combinedText = fmt.Sprintf("````question\n%s\n````\n", string(qJSON))
								} else {
									combinedText = ""
								}
								askedLeakBuffer = ""
								inAskedLeak = false
							} else {
								combinedText = ""
							}
						}

						displayChunk := combinedText
						fullResponse += displayChunk
						persistAssistant(timelineResponse + fullResponse)

						if !isToolChunk && len(toolCalls) == 0 {
							lowerResp := strings.ToLower(fullResponse)
							startIdx := strings.LastIndex(lowerResp, "<remember>")
							endIdx := strings.LastIndex(lowerResp, "</remember>")

							inTag := false
							if startIdx != -1 && (endIdx == -1 || endIdx < startIdx) {
								inTag = true
							}

							inPartial := false
							if !inTag {
								last10 := lowerResp
								if len(last10) > 10 {
									last10 = last10[len(last10)-10:]
								}
								for i := 1; i < len("<remember>"); i++ {
									if strings.HasSuffix(last10, "<remember>"[:i]) {
										inPartial = true
										break
									}
								}
							}

							if inTag || (endIdx != -1 && strings.HasSuffix(lowerResp, "</remember>")) {
								// Completely drop the token chunk from the UI
							} else if inPartial {
								hiddenBuffer += displayChunk
							} else {
								// Fully render chunk + any accumulated false-positive buffer
								contentToStream := hiddenBuffer + displayChunk
								hiddenBuffer = ""

								chunk.Choices[0].Delta.Content = contentToStream
								b, _ := json.Marshal(chunk)
								w.Write([]byte("data: "))
								w.Write(b)
								w.Write([]byte("\n\n"))
								if ok {
									flusher.Flush()
								}
							}
						}
					}
				}
			}

			if string(cleanLine) == "data: [DONE]" {
				if hiddenBuffer != "" {
					var finalChunk struct {
						Choices []struct {
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
						} `json:"choices"`
					}
					finalChunk.Choices = append(finalChunk.Choices, struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					}{})
					finalChunk.Choices[0].Delta.Content = hiddenBuffer
					b, _ := json.Marshal(finalChunk)
					w.Write([]byte("data: "))
					w.Write(b)
					w.Write([]byte("\n\n"))
					if ok {
						flusher.Flush()
					}
				}
				w.Write([]byte("data: [DONE]\n\n"))
				if ok {
					flusher.Flush()
				}
				break
			}
		}

		resp.Body.Close()

		// If the AI invoked tools, we must NOT exit. We execute them, append to memory, and loop!
		if len(toolCalls) > 0 {

			// 1. Append the AI's tool request to openRouterMsgs
			assistantToolMsg := map[string]interface{}{
				"role":       "assistant",
				"content":    fullResponse, // Can be null if it just straight up called a tool
				"tool_calls": toolCalls,
			}
			for _, tc := range toolCalls {
				tc["type"] = "function" // OpenRouter spec enforcement
			}
			openRouterMsgs = append(openRouterMsgs, assistantToolMsg)

			// 2. Execute Go Tools
			for _, tc := range toolCalls {
				id, _ := tc["id"].(string)
				funcObj, _ := tc["function"].(map[string]interface{})
				name, _ := funcObj["name"].(string)
				args, _ := funcObj["arguments"].(string)

				var parsedArgs map[string]interface{}
				json.Unmarshal([]byte(args), &parsedArgs)

				// -- ASK USER QUESTION INTERCEPT --
				if name == "ask_user_question" {
					var qChunk struct {
						Choices []struct {
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
						} `json:"choices"`
					}
					qChunk.Choices = append(qChunk.Choices, struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					}{})

					// Re-standardize for frontend
					qPayload := map[string]interface{}{
						"question": parsedArgs["question"],
					}
					if opt, ok := parsedArgs["options"]; ok {
						qPayload["options"] = opt
					} else if ch, ok := parsedArgs["choices"]; ok {
						qPayload["options"] = ch
					} else {
						qPayload["options"] = []string{"Yes", "No"}
					}
					qPayloadB, _ := json.Marshal(qPayload)

					qBlock := fmt.Sprintf("\n\n````question\n%s\n````\n", string(qPayloadB))
					qChunk.Choices[0].Delta.Content = qBlock
					qB, _ := json.Marshal(qChunk)
					w.Write([]byte("data: "))
					w.Write(qB)
					w.Write([]byte("\n\n"))
					if ok {
						flusher.Flush()
					}

					// Persist question blocks and tool actions across reloads.
					askActionJSON, _ := json.Marshal(map[string]string{"action": "Asked Question", "target": "user_input"})
					askActionBlock := fmt.Sprintf("````tool_action\n%s\n````\n", string(askActionJSON))
					fullResponse += askActionBlock + qBlock

					persistAssistant(timelineResponse + fullResponse)
					resp.Body.Close()
					return
				}

				// -- TIMELINE: Stream a tool_action block BEFORE execution --
				type toolActionPayload struct {
					Action string `json:"action"`
					Target string `json:"target"`
				}
				tap := toolActionPayload{Action: name}
				switch name {
				case "execute_command":
					tap.Action = "Ran Command"
					if c, ok := parsedArgs["command"].(string); ok {
						tap.Target = c
					}
				case "edit_file":
					tap.Action = "Edited"
					if p, ok := parsedArgs["path"].(string); ok {
						tap.Target = p
					}
				case "write_file":
					tap.Action = "Created"
					if p, ok := parsedArgs["path"].(string); ok {
						tap.Target = p
					}
				case "read_file":
					tap.Action = "Read"
					if p, ok := parsedArgs["path"].(string); ok {
						tap.Target = p
					}
				case "list_files":
					tap.Action = "Listed Files"
					if p, ok := parsedArgs["path"].(string); ok {
						tap.Target = p
					}
				case "grep_search":
					tap.Action = "Searched Code"
					if q, ok := parsedArgs["query"].(string); ok {
						tap.Target = q
					}
				case "web_search":
					tap.Action = "Searched Web"
					if q, ok := parsedArgs["query"].(string); ok {
						tap.Target = q
					}
				case "web_scrape":
					tap.Action = "Scraped URL"
					if u, ok := parsedArgs["url"].(string); ok {
						tap.Target = u
					}
				case "undo_checkpoint":
					tap.Action = "Reverted Checkpoint"
				case "propose_commit":
					tap.Action = "Proposed Commit"
					if p, ok := parsedArgs["file_path"].(string); ok {
						tap.Target = p
					}
				case "undo_change":
					tap.Action = "Undid Change"
					if p, ok := parsedArgs["point_id"].(string); ok {
						tap.Target = p
					}
				case "get_context_tree":
					tap.Action = "Mapped Context Tree"
				case "get_file_skeleton":
					tap.Action = "Generated File Skeleton"
					if p, ok := parsedArgs["file_path"].(string); ok {
						tap.Target = p
					}
				case "semantic_code_search":
					tap.Action = "Semantic Search"
					if q, ok := parsedArgs["query"].(string); ok {
						tap.Target = q
					}
				case "semantic_identifier_search":
					tap.Action = "Identifier Search"
					if q, ok := parsedArgs["query"].(string); ok {
						tap.Target = q
					}
				case "get_blast_radius":
					tap.Action = "Computed Blast Radius"
					if s, ok := parsedArgs["symbol_name"].(string); ok {
						tap.Target = s
					}
				case "run_static_analysis":
					tap.Action = "Ran Static Analysis"
				case "semantic_navigate":
					tap.Action = "Semantic Navigation"
				case "get_feature_hub":
					tap.Action = "Feature Hub Lookup"
				}
				tActionJSON, _ := json.Marshal(tap)
				tActionBlock := fmt.Sprintf("````tool_action\n%s\n````\n", string(tActionJSON))
				var taChunk struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				taChunk.Choices = append(taChunk.Choices, struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				}{})
				taChunk.Choices[0].Delta.Content = tActionBlock
				taB, _ := json.Marshal(taChunk)
				w.Write([]byte("data: "))
				w.Write(taB)
				w.Write([]byte("\n\n"))
				if ok {
					flusher.Flush()
				}
				fullResponse += tActionBlock
				timelineResponse += tActionBlock
				persistAssistant(timelineResponse + fullResponse)

				// -- Checkpoint: create git snapshot before destructive actions --
				var hash string
				if name == "execute_command" {
					cmdName := "command"
					if c, ok := parsedArgs["command"].(string); ok {
						cmdName = c
					}
					hash = tools.CreateCheckpoint("Before running: " + cmdName)
				} else if name == "write_file" || name == "edit_file" || name == "propose_commit" {
					pathName := "file"
					if p, ok := parsedArgs["path"].(string); ok {
						pathName = p
					} else if p, ok := parsedArgs["file_path"].(string); ok {
						pathName = p
					}
					hash = tools.CreateCheckpoint("Before modifying: " + pathName)
				}

				// --- EXECUTION MODE SAFETY GATE ---
				execMode := strings.ToLower(executionMode)
				if execMode == "" {
					execMode = "default"
				}
				destructiveWrite := name == "write_file" || name == "edit_file" || name == "rename_file" || name == "propose_commit" || name == "undo_change"
				destructiveExec := name == "execute_command"
				if execMode == "plan" && (destructiveWrite || destructiveExec) {
					openRouterMsgs = append(openRouterMsgs, map[string]interface{}{
						"role": "tool", "tool_call_id": id, "name": name,
						"content": fmt.Sprintf("[PLAN MODE] Tool '%s' blocked. Read-only mode active.", name),
					})
					continue
				}
				needsConfirm := (execMode == "default" && (destructiveWrite || destructiveExec)) ||
					(execMode == "auto_accept" && destructiveExec)
				if needsConfirm {
					confirmID := fmt.Sprintf("conf-%d-%s", time.Now().UnixNano(), name)
					confirmPayload, _ := json.Marshal(map[string]interface{}{"id": confirmID, "tool": name, "args": parsedArgs})
					var cfChunk struct {
						Choices []struct {
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
						} `json:"choices"`
					}
					cfChunk.Choices = append(cfChunk.Choices, struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					}{})
					cfChunk.Choices[0].Delta.Content = fmt.Sprintf("````confirm\n%s\n````\n", string(confirmPayload))
					cfB, _ := json.Marshal(cfChunk)
					w.Write([]byte("data: "))
					w.Write(cfB)
					w.Write([]byte("\n\n"))
					if ok {
						flusher.Flush()
					}
					ch := RegisterConfirmation(confirmID)
					if approved := <-ch; !approved {
						openRouterMsgs = append(openRouterMsgs, map[string]interface{}{
							"role": "tool", "tool_call_id": id, "name": name,
							"content": fmt.Sprintf("[Rejected] User did not approve '%s'.", name),
						})
						continue
					}
				}
				resultStr := tools.ExecuteTool(name, args, projectPath)

				// -- Post-execution visual feedback for terminal output and checkpoints --
				var visualMsg string
				switch name {
				case "execute_command":
					cmdName := "command"
					if c, ok := parsedArgs["command"].(string); ok {
						cmdName = c
					}
					visualMsg = fmt.Sprintf("\n\n```terminal\n$ %s\n%s\n```\n", cmdName, resultStr)
				case "undo_checkpoint":
					visualMsg = "\n\n> ⏪ **Reverted sandbox to previous checkpoint.**\n"
				}

				if hash != "" {
					visualMsg = fmt.Sprintf("\n\n````checkpoint\n%s\n````\n", hash) + visualMsg
				}

				if visualMsg != "" {
					var streamChunk struct {
						Choices []struct {
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
						} `json:"choices"`
					}
					streamChunk.Choices = append(streamChunk.Choices, struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					}{})
					streamChunk.Choices[0].Delta.Content = visualMsg
					b, _ := json.Marshal(streamChunk)
					w.Write([]byte("data: "))
					w.Write(b)
					w.Write([]byte("\n\n"))
					if ok {
						flusher.Flush()
					}
					fullResponse += visualMsg
					timelineResponse += visualMsg
					persistAssistant(timelineResponse + fullResponse)
				}

				// 3. Append the physical tool output
				toolResultMsg := map[string]interface{}{
					"role":         "tool",
					"tool_call_id": id,
					"name":         name,
					"content":      resultStr,
				}
				openRouterMsgs = append(openRouterMsgs, toolResultMsg)

				if chatReq.Mode == "coding" && debugSystemLogs {
					sendSSEChunk(w, fmt.Sprintf("````system_log\n[TOOL RESULT: %s]\n%s\n````\n", name, resultStr))
				}
			}

			// DO NOT BREAK. Let the `for` loop spin again and send the complete array back to OpenRouter!
			continue
		}

		// --- END OF TOOL LOOP ---
		// If the AI has reached here, it didn't pick any more tools.
		// BUT: if the fullResponse is empty or purely technical, the user sees nothing!
		// We detect if there's no "human" text and force one final synthesis.
		cleanText := uiBlockRe.ReplaceAllString(fullResponse, "")
		cleanText = strings.TrimSpace(cleanText)
		if cleanText == "" && attempt > 0 {
			// Nudge the model to synthesize
			openRouterMsgs = append(openRouterMsgs, map[string]interface{}{"role": "user", "content": "The tools have finished. Please provide a concise, final summary of what was accomplished for the user."})
			continue // One more spin!
		}

		// --- END OF TOOL LOOP ---

		// If we reach here, the AI is done picking tools and has sent us a final text answer.

		// 5. Personality Engine Extraction
		if fullResponse != "" {
			re := regexp.MustCompile(`(?is)<remember>(.*?)</remember>`)
			matches := re.FindAllStringSubmatch(fullResponse, -1)
			for _, match := range matches {
				if len(match) > 1 {
					fact := strings.TrimSpace(match[1])
					if fact != "" {
						db.DB.Exec("INSERT OR IGNORE INTO personality_facts (fact) VALUES (?)", fact)
						log.Println("Personality Engine: Immortalized new fact:", fact)
					}
				}
			}
		}

		storedResponse := timelineResponse + fullResponse
		persistAssistant(storedResponse)
		return
	}
}

// Proxies raw OpenAI-format requests from external tools (VSCode, Mobile) out to the correct provider
func AiExternalChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	apiKey := ResolveOpenRouterKey(CurrentUserID(r))
	ollamaUrl := os.Getenv("OLLAMA_API_URL")

	if (apiKey == "" || apiKey == "your_openrouter_api_key_here") && ollamaUrl == "" {
		http.Error(w, `{"error": "No AI Providers configured"}`, http.StatusServiceUnavailable)
		return
	}

	// Read verbatim incoming JSON payload
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}

	var payloadMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payloadMap); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	model, _ := payloadMap["model"].(string)

	// Agent routing: check X-Agent-ID header, ?agent_id= param, or default setting
	routingAgentID := r.Header.Get("X-Agent-ID")
	if routingAgentID == "" {
		routingAgentID = r.URL.Query().Get("agent_id")
	}
	if routingAgentID == "" {
		routingAgentID = getSettingString("external_default_agent_id", "")
	}
	if routingAgentID != "" && agentOSService != nil {
		if routingAgent, err := agentOSService.GetAgent(routingAgentID); err == nil {
			if strings.TrimSpace(routingAgent.IdentityPrompt) != "" {
				agentSysMsg := map[string]interface{}{"role": "system", "content": routingAgent.IdentityPrompt}
				if messages, ok := payloadMap["messages"].([]interface{}); ok {
					payloadMap["messages"] = append([]interface{}{agentSysMsg}, messages...)
				}
			}
			// Use agent's model if set
			if binding, profile, err2 := agentOSService.GetAgentBinding(routingAgent.ID); err2 == nil {
				if profile.Model != "" && model == "" {
					payloadMap["model"] = profile.Model
					model = profile.Model
					_ = binding
				}
			}
		}
	}

	// Inject RAG episodes for external tooling
	if messages, ok := payloadMap["messages"].([]interface{}); ok && len(messages) > 0 {
		lastMsg, _ := messages[len(messages)-1].(map[string]interface{})
		if contentStr, ok := lastMsg["content"].(string); ok {
			episodes := SearchEpisodes(contentStr, "external_api")
			if episodes != "" {
				sysMsg := map[string]interface{}{"role": "system", "content": episodes}
				payloadMap["messages"] = append([]interface{}{sysMsg}, messages...)
				log.Println("Memory System: Injected chronological memory RAG block into EXTERNAL context stream.")
			}
		}
	}

	// Inject Global Personality Prompt for external tooling
	personaPrompt := ConstructPersonalityPrompt(CurrentUserID(r), "talking", "None", "Plan", "")
	if personaPrompt != "" {
		sysMsg := map[string]interface{}{"role": "system", "content": personaPrompt}
		if messages, ok := payloadMap["messages"].([]interface{}); ok {
			payloadMap["messages"] = append([]interface{}{sysMsg}, messages...)
		}
	}

	payloadMap["tools"] = tools.GetAvailableTools()

	var targetUrl string
	var reqApiKey string

	if strings.HasPrefix(model, "local/") {
		if ollamaUrl == "" {
			http.Error(w, "OLLAMA_API_URL is not configured", http.StatusBadRequest)
			return
		}
		// Rewrite model internally
		payloadMap["model"] = strings.TrimPrefix(model, "local/")
		targetUrl = ollamaUrl + "/v1/chat/completions"
		reqApiKey = "Bearer local"
	} else {
		if model == "" {
			settings := GetCurrentSettings(CurrentUserID(r))
			if settings.DefaultModel != "" {
				payloadMap["model"] = settings.DefaultModel
			} else {
				payloadMap["model"] = "meta-llama/llama-3-8b-instruct:free"
			}
		}
		targetUrl = "https://openrouter.ai/api/v1/chat/completions"
		reqApiKey = "Bearer " + apiKey
	}

	for attempt := 0; attempt < 5; attempt++ {
		// Re-marshal to send upstream
		upstreamPayload, _ := json.Marshal(payloadMap)

		req, _ := http.NewRequestWithContext(r.Context(), "POST", targetUrl, bytes.NewBuffer(upstreamPayload))
		req.Header.Set("Authorization", reqApiKey)
		req.Header.Set("Content-Type", "application/json")
		if !strings.HasPrefix(model, "local/") {
			req.Header.Set("X-Title", "AgentHQ API Gateway")
		}

		client := &http.Client{Timeout: 300 * time.Second}
		resp, err := client.Do(req)

		if err != nil {
			if attempt == 0 {
				http.Error(w, "Upstream AI error", http.StatusBadGateway)
			}
			return
		}

		if attempt == 0 {
			// Only send headers on the very first loop before stream begins
			w.WriteHeader(resp.StatusCode)
		}

		if resp.StatusCode != 200 {
			if attempt == 0 {
				// We already wrote the header above, just pipe body
				io.Copy(w, resp.Body)
			}
			resp.Body.Close()
			return
		}

		reader := bufio.NewReader(resp.Body)
		var fullResponse string
		var toolCalls []map[string]interface{}
		var hiddenBuffer string
		flusher, ok := w.(http.Flusher)

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				break
			}

			cleanLine := bytes.TrimSpace(line)
			isToolChunk := false

			if bytes.HasPrefix(cleanLine, []byte("data: ")) && string(cleanLine) != "data: [DONE]" {
				data := bytes.TrimPrefix(cleanLine, []byte("data: "))
				var chunk struct {
					Choices []struct {
						Delta struct {
							Content   string                   `json:"content"`
							ToolCalls []map[string]interface{} `json:"tool_calls"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if json.Unmarshal(data, &chunk) == nil && len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta
					if len(delta.ToolCalls) > 0 {
						isToolChunk = true
						for _, tc := range delta.ToolCalls {
							idxFloat, ok := tc["index"].(float64)
							if !ok {
								continue
							}
							idx := int(idxFloat)
							for len(toolCalls) <= idx {
								toolCalls = append(toolCalls, make(map[string]interface{}))
							}
							if id, exists := tc["id"].(string); exists {
								toolCalls[idx]["id"] = id
							}
							if function, exists := tc["function"].(map[string]interface{}); exists {
								if _, fExists := toolCalls[idx]["function"]; !fExists {
									toolCalls[idx]["function"] = make(map[string]interface{})
								}
								funcPtr := toolCalls[idx]["function"].(map[string]interface{})

								if n, nExists := function["name"].(string); nExists {
									funcPtr["name"] = n
								}
								if argChunk, argExists := function["arguments"].(string); argExists {
									if existingArgs, ex := funcPtr["arguments"].(string); ex {
										funcPtr["arguments"] = existingArgs + argChunk
									} else {
										funcPtr["arguments"] = argChunk
									}
								}
							}
						}
					} else if delta.Content != "" {
						fullResponse += delta.Content

						if !isToolChunk && len(toolCalls) == 0 {
							lowerResp := strings.ToLower(fullResponse)
							startIdx := strings.LastIndex(lowerResp, "<remember>")
							endIdx := strings.LastIndex(lowerResp, "</remember>")

							inTag := false
							if startIdx != -1 && (endIdx == -1 || endIdx < startIdx) {
								inTag = true
							}

							inPartial := false
							if !inTag {
								last10 := lowerResp
								if len(last10) > 10 {
									last10 = last10[len(last10)-10:]
								}
								for i := 1; i < len("<remember>"); i++ {
									if strings.HasSuffix(last10, "<remember>"[:i]) {
										inPartial = true
										break
									}
								}
							}

							if inTag || (endIdx != -1 && strings.HasSuffix(lowerResp, "</remember>")) {
								// Drop token
							} else if inPartial {
								hiddenBuffer += delta.Content
							} else {
								// Render
								contentToStream := hiddenBuffer + delta.Content
								hiddenBuffer = ""

								chunk.Choices[0].Delta.Content = contentToStream
								b, _ := json.Marshal(chunk)
								w.Write([]byte("data: "))
								w.Write(b)
								w.Write([]byte("\n\n"))
								if ok {
									flusher.Flush()
								}
							}
						}
					}
				}
			}

			if string(cleanLine) == "data: [DONE]" {
				if hiddenBuffer != "" {
					var finalChunk struct {
						Choices []struct {
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
						} `json:"choices"`
					}
					finalChunk.Choices = append(finalChunk.Choices, struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					}{})
					finalChunk.Choices[0].Delta.Content = hiddenBuffer
					b, _ := json.Marshal(finalChunk)
					w.Write([]byte("data: "))
					w.Write(b)
					w.Write([]byte("\n\n"))
					if ok {
						flusher.Flush()
					}
				}
				break
			}
		}

		resp.Body.Close()

		if len(toolCalls) > 0 {
			assistantToolMsg := map[string]interface{}{
				"role":       "assistant",
				"content":    fullResponse,
				"tool_calls": toolCalls,
			}
			for _, tc := range toolCalls {
				tc["type"] = "function"
			}

			if messagesArray, ok := payloadMap["messages"].([]interface{}); ok {
				messagesArray = append(messagesArray, assistantToolMsg)

				for _, tc := range toolCalls {
					id, _ := tc["id"].(string)
					funcObj, _ := tc["function"].(map[string]interface{})
					name, _ := funcObj["name"].(string)
					args, _ := funcObj["arguments"].(string)

					var parsedArgs map[string]interface{}
					json.Unmarshal([]byte(args), &parsedArgs)

					var hash string
					if name == "execute_command" {
						cmdName := "command"
						if c, ok := parsedArgs["command"].(string); ok {
							cmdName = c
						}
						hash = tools.CreateCheckpoint("Before running: " + cmdName)
					} else if name == "write_file" || name == "edit_file" || name == "propose_commit" {
						pathName := "file"
						if p, ok := parsedArgs["path"].(string); ok {
							pathName = p
						} else if p, ok := parsedArgs["file_path"].(string); ok {
							pathName = p
						}
						hash = tools.CreateCheckpoint("Before modifying: " + pathName)
					}

					resultStr := tools.ExecuteTool(name, args, "") // External API doesn't have project root yet

					var visualMsg string

					switch name {
					case "execute_command":
						cmdName := "command"
						if c, ok := parsedArgs["command"].(string); ok {
							cmdName = c
						}
						visualMsg = fmt.Sprintf("\n\n> 🛠️ **Ran Command:** `%s`\n```terminal\n%s\n```\n", cmdName, resultStr)
					case "undo_checkpoint":
						visualMsg = "\n\n> ⏪ **Reverted sandbox to previous checkpoint.**\n"
					case "edit_file":
						pathName := "file"
						if p, ok := parsedArgs["path"].(string); ok {
							pathName = p
						}
						visualMsg = fmt.Sprintf("\n\n> 📝 **Edited `%s`**\n", pathName)
					case "write_file":
						pathName := "file"
						if p, ok := parsedArgs["path"].(string); ok {
							pathName = p
						}
						visualMsg = fmt.Sprintf("\n\n> 📝 **Created `%s`**\n", pathName)
					case "propose_commit":
						pathName := "file"
						if p, ok := parsedArgs["file_path"].(string); ok {
							pathName = p
						}
						visualMsg = fmt.Sprintf("\n\n> 📝 **Proposed Commit `%s`**\n", pathName)
					case "undo_change":
						pointID := "restore point"
						if p, ok := parsedArgs["point_id"].(string); ok {
							pointID = p
						}
						visualMsg = fmt.Sprintf("\n\n> ⏪ **Restored `%s`**\n", pointID)
					case "read_file":
						pathName := "file"
						if p, ok := parsedArgs["path"].(string); ok {
							pathName = p
						}
						visualMsg = fmt.Sprintf("\n\n> 📖 **Read `%s`**\n", pathName)
					case "list_files":
						visualMsg = "\n\n> 📂 **Listed Files**\n"
					case "grep_search":
						visualMsg = "\n\n> 🔍 **Searched Files**\n"
					case "web_scrape":
						visualMsg = "\n\n> 🌐 **Scraped Webpage**\n"
					}

					if visualMsg != "" && hash != "" {
						visualMsg = fmt.Sprintf("\n\n````checkpoint\n%s\n````\n", hash) + visualMsg
					}

					if visualMsg != "" {
						var streamChunk struct {
							Choices []struct {
								Delta struct {
									Content string `json:"content"`
								} `json:"delta"`
							} `json:"choices"`
						}
						streamChunk.Choices = append(streamChunk.Choices, struct {
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
						}{})
						streamChunk.Choices[0].Delta.Content = visualMsg
						b, _ := json.Marshal(streamChunk)
						w.Write([]byte("data: "))
						w.Write(b)
						w.Write([]byte("\n\n"))
						if ok {
							flusher.Flush()
						}
						fullResponse += visualMsg
					}

					toolResultMsg := map[string]interface{}{
						"role":         "tool",
						"tool_call_id": id,
						"name":         name,
						"content":      resultStr,
					}
					messagesArray = append(messagesArray, toolResultMsg)
				}
				payloadMap["messages"] = messagesArray
			}

			continue
		}

		// 5. Personality Engine Extraction against proxied stream
		if fullResponse != "" {
			re := regexp.MustCompile(`(?is)<remember>(.*?)</remember>`)
			matches := re.FindAllStringSubmatch(fullResponse, -1)
			for _, match := range matches {
				if len(match) > 1 {
					fact := strings.TrimSpace(match[1])
					if fact != "" {
						db.DB.Exec("INSERT OR IGNORE INTO personality_facts (fact) VALUES (?)", fact)
						log.Println("Personality Engine: Immortalized EXTERNAL proxied fact:", fact)
					}
				}
			}
		}

		w.Write([]byte("data: [DONE]\n\n"))
		if ok {
			flusher.Flush()
		}
	}
}
