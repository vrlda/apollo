package agentos

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

type runtimeTaskInput struct {
	Prompt string `json:"prompt"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Service) processQueue() {
	row := s.db.QueryRow(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(requested_by,''), IFNULL(parent_task_id,''), IFNULL(thread_id,''), IFNULL(type,''), IFNULL(status,''), IFNULL(priority,50), IFNULL(input_json,''), IFNULL(result_json,''), IFNULL(blocked_reason,''), IFNULL(created_at,''), IFNULL(updated_at,''), IFNULL(completed_at,'')
		FROM agent_tasks WHERE status = 'queued'
		ORDER BY priority ASC, created_at ASC
		LIMIT 1
	`)
	task, err := scanTask(row)
	if err != nil {
		return
	}
	if ok, reason := s.schedulerAdmission(&task); !ok {
		// Resource pressure should throttle admission without mutating task state.
		if reason == "cpu_hard" || reason == "ram_hard" || reason == "max_workers" {
			return
		}
		if reason != "" && task.Status == "queued" {
			_, _ = s.db.Exec("UPDATE agent_tasks SET status='blocked', blocked_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id = ?", reason, task.ID)
			s.emitEvent(task.CompanyID, task.DepartmentID, task.AgentID, task.ThreadID, task.ID, "task_blocked", "warn", map[string]interface{}{"reason": reason})
		}
		return
	}

	s.mu.Lock()
	if _, ok := s.running[task.ID]; ok {
		s.mu.Unlock()
		return
	}
	s.running[task.ID] = struct{}{}
	s.mu.Unlock()

	go func(t AgentTask) {
		defer func() {
			s.mu.Lock()
			delete(s.running, t.ID)
			s.mu.Unlock()
		}()
		s.runTask(t)
	}(task)
}

func (s *Service) runTask(task AgentTask) {
	agent, err := s.GetAgent(task.AgentID)
	if err != nil {
		s.failTask(task, "agent_not_found", err.Error())
		return
	}
	if !agent.IsActive {
		s.failTask(task, "agent_inactive", "agent is inactive")
		return
	}

	if !s.policyAllowed(task, "task_run", fmt.Sprintf("company:%s/department:%s/agent:%s", task.CompanyID, task.DepartmentID, task.AgentID), "tier2") {
		return
	}

	_, _ = s.db.Exec("UPDATE agent_tasks SET status='running', blocked_reason='', updated_at=CURRENT_TIMESTAMP WHERE id = ?", task.ID)
	s.emitEvent(task.CompanyID, task.DepartmentID, task.AgentID, task.ThreadID, task.ID, "task_running", "info", map[string]interface{}{"task_id": task.ID})

	runID := uuid.NewString()
	_, _ = s.db.Exec(`INSERT INTO agent_runs (id, task_id, attempt, status, provider, model, started_at, ended_at, summary, error) VALUES (?, ?, 1, 'running', '', '', CURRENT_TIMESTAMP, NULL, '', '')`, runID, task.ID)

	if strings.EqualFold(agent.RoleType, "manager") {
		if done := s.processManagerTask(task, agent, runID); done {
			return
		}
	}

	output, provider, model, runErr := s.generateAgentOutput(task, agent)
	if runErr != nil {
		s.failRunAndTask(runID, task, provider, model, runErr)
		return
	}

	s.extractAndApplySelfSchedule(output, task, agent)
	s.extractAndApplyInterAgentMessages(output, task, agent)

	result := map[string]interface{}{
		"output":   output,
		"provider": provider,
		"model":    model,
	}
	resJSON, _ := json.Marshal(result)

	completedAt := time.Now().UTC()
	_, _ = s.db.Exec("UPDATE agent_tasks SET status='done', result_json=?, updated_at=CURRENT_TIMESTAMP, completed_at=CURRENT_TIMESTAMP WHERE id = ?", string(resJSON), task.ID)
	_, _ = s.db.Exec("UPDATE agent_runs SET status='done', provider=?, model=?, ended_at=CURRENT_TIMESTAMP, summary=? WHERE id = ?", provider, model, summarize(output, 500), runID)
	_, _ = s.db.Exec("UPDATE agents SET status='idle', updated_at=CURRENT_TIMESTAMP WHERE id = ?", agent.ID)

	if task.ThreadID != "" {
		_, _ = s.AddThreadMessage(task.ThreadID, "agent", output, "text")
	}

	_ = s.WriteMemoryEntry(MemoryEntry{
		ID:            uuid.NewString(),
		ScopeType:     "agent_long_term",
		ScopeID:       agent.ID,
		SourceType:    "task_result",
		AuthorAgentID: agent.ID,
		Content:       summarize(output, 1600),
		TagsJSON:      `{"origin":"task","task_id":"` + task.ID + `"}`,
		Importance:    0.7,
	})

	if task.ParentTaskID != "" {
		_, _ = s.db.Exec("UPDATE agent_delegations SET status='done', updated_at=CURRENT_TIMESTAMP WHERE parent_task_id = ? AND to_agent_id = ?", task.ParentTaskID, task.AgentID)
	}

	s.emitEvent(task.CompanyID, task.DepartmentID, task.AgentID, task.ThreadID, task.ID, "task_done", "info", map[string]interface{}{"completed_at": completedAt.Format(time.RFC3339)})
	s.appendAudit("task_done", "task", task.ID, agent.ID, map[string]interface{}{"provider": provider, "model": model})
}

func (s *Service) processManagerTask(task AgentTask, agent Agent, runID string) bool {
	// If manager has active subtasks, wait for completion and synthesize later.
	var totalChildren, pendingChildren int
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE parent_task_id = ?", task.ID).Scan(&totalChildren)
	if totalChildren > 0 {
		_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE parent_task_id = ? AND status IN ('queued','running','blocked','waiting_input')", task.ID).Scan(&pendingChildren)
		if pendingChildren > 0 {
			_, _ = s.db.Exec("UPDATE agent_tasks SET status='waiting_input', blocked_reason='awaiting_worker_tasks', updated_at=CURRENT_TIMESTAMP WHERE id = ?", task.ID)
			_, _ = s.db.Exec("UPDATE agent_runs SET status='done', provider='manager', model='manager', ended_at=CURRENT_TIMESTAMP, summary='waiting for worker tasks' WHERE id = ?", runID)
			s.emitEvent(task.CompanyID, task.DepartmentID, task.AgentID, task.ThreadID, task.ID, "task_waiting_workers", "info", map[string]interface{}{"pending_children": pendingChildren})
			return true
		}

		rows, err := s.db.Query("SELECT IFNULL(result_json,'') FROM agent_tasks WHERE parent_task_id = ? ORDER BY created_at ASC", task.ID)
		if err != nil {
			s.failRunAndTask(runID, task, "manager", "manager", err)
			return true
		}
		defer rows.Close()
		parts := []string{}
		for rows.Next() {
			var r string
			if err := rows.Scan(&r); err == nil {
				parts = append(parts, r)
			}
		}
		summary := "Manager synthesis:\n\n" + strings.Join(parts, "\n\n")
		result := map[string]interface{}{"output": summary, "provider": "manager", "model": "manager"}
		resJSON, _ := json.Marshal(result)
		_, _ = s.db.Exec("UPDATE agent_tasks SET status='done', result_json=?, updated_at=CURRENT_TIMESTAMP, completed_at=CURRENT_TIMESTAMP WHERE id = ?", string(resJSON), task.ID)
		_, _ = s.db.Exec("UPDATE agent_runs SET status='done', provider='manager', model='manager', ended_at=CURRENT_TIMESTAMP, summary=? WHERE id = ?", summarize(summary, 500), runID)
		if task.ThreadID != "" {
			_, _ = s.AddThreadMessage(task.ThreadID, "agent", summary, "text")
		}
		s.emitEvent(task.CompanyID, task.DepartmentID, task.AgentID, task.ThreadID, task.ID, "task_done", "info", map[string]interface{}{"mode": "manager_synthesis"})
		return true
	}

	workers, err := s.ListAgents(task.CompanyID, task.DepartmentID)
	if err != nil {
		s.failRunAndTask(runID, task, "manager", "manager", err)
		return true
	}
	eligible := []Agent{}
	for _, w := range workers {
		if w.RoleType != "worker" || !w.IsActive {
			continue
		}
		if strings.TrimSpace(w.ParentAgentID) != "" && w.ParentAgentID != agent.ID {
			continue
		}
		eligible = append(eligible, w)
	}
	if len(eligible) == 0 {
		// No workers; manager executes directly as regular agent.
		return false
	}

	instruction := task.InputJSON
	if strings.TrimSpace(instruction) == "" {
		instruction = `{"prompt":"Execute delegated manager task"}`
	}
	for _, w := range eligible {
		if !s.policyAllowed(task, "delegate", fmt.Sprintf("to_agent:%s", w.ID), "tier1") {
			continue
		}
		child := AgentTask{
			ID:           uuid.NewString(),
			CompanyID:    task.CompanyID,
			DepartmentID: task.DepartmentID,
			AgentID:      w.ID,
			RequestedBy:  agent.ID,
			ParentTaskID: task.ID,
			ThreadID:     task.ThreadID,
			Type:         "delegated",
			Status:       "queued",
			Priority:     task.Priority + 5,
			InputJSON:    instruction,
		}
		if _, err := s.CreateTask(child); err == nil {
			_, _ = s.db.Exec(`INSERT INTO agent_delegations (id, parent_task_id, from_agent_id, to_agent_id, instruction, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'queued', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, uuid.NewString(), task.ID, agent.ID, w.ID, summarize(instruction, 500))
			s.emitEvent(task.CompanyID, task.DepartmentID, agent.ID, task.ThreadID, task.ID, "task_delegated", "info", map[string]interface{}{"to_agent_id": w.ID, "child_task_id": child.ID})
		}
	}

	_, _ = s.db.Exec("UPDATE agent_tasks SET status='waiting_input', blocked_reason='awaiting_worker_tasks', updated_at=CURRENT_TIMESTAMP WHERE id = ?", task.ID)
	_, _ = s.db.Exec("UPDATE agent_runs SET status='done', provider='manager', model='manager', ended_at=CURRENT_TIMESTAMP, summary='delegated to worker agents' WHERE id = ?", runID)
	return true
}

func (s *Service) resumeBlockedManagers() {
	rows, err := s.db.Query(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(requested_by,''), IFNULL(parent_task_id,''), IFNULL(thread_id,''), IFNULL(type,''), IFNULL(status,''), IFNULL(priority,50), IFNULL(input_json,''), IFNULL(result_json,''), IFNULL(blocked_reason,''), IFNULL(created_at,''), IFNULL(updated_at,''), IFNULL(completed_at,'')
		FROM agent_tasks WHERE status = 'waiting_input' AND blocked_reason = 'awaiting_worker_tasks'
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			continue
		}
		var pending int
		_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE parent_task_id = ? AND status IN ('queued','running','blocked','waiting_input')", t.ID).Scan(&pending)
		if pending == 0 {
			_, _ = s.db.Exec("UPDATE agent_tasks SET status='queued', blocked_reason='', updated_at=CURRENT_TIMESTAMP WHERE id = ?", t.ID)
			s.emitEvent(t.CompanyID, t.DepartmentID, t.AgentID, t.ThreadID, t.ID, "task_resumed", "info", map[string]interface{}{"reason": "workers_completed"})
		}
	}
}

func (s *Service) failRunAndTask(runID string, task AgentTask, provider string, model string, err error) {
	if runID != "" {
		_, _ = s.db.Exec("UPDATE agent_runs SET status='error', provider=?, model=?, ended_at=CURRENT_TIMESTAMP, error=?, summary=? WHERE id = ?", provider, model, err.Error(), summarize(err.Error(), 500), runID)
	}
	s.failTask(task, "runtime_error", err.Error())
}

func (s *Service) failTask(task AgentTask, reason string, details string) {
	_, _ = s.db.Exec("UPDATE agent_tasks SET status='failed', blocked_reason=?, updated_at=CURRENT_TIMESTAMP, completed_at=CURRENT_TIMESTAMP WHERE id = ?", reason, task.ID)
	_, _ = s.db.Exec("UPDATE agents SET status='error', updated_at=CURRENT_TIMESTAMP WHERE id = ?", task.AgentID)
	s.emitEvent(task.CompanyID, task.DepartmentID, task.AgentID, task.ThreadID, task.ID, "task_failed", "error", map[string]interface{}{"reason": reason, "details": details})
	s.appendAudit("task_failed", "task", task.ID, task.AgentID, map[string]interface{}{"reason": reason, "details": details})
}

func (s *Service) generateAgentOutput(task AgentTask, agent Agent) (string, string, string, error) {
	binding, profile, err := s.getBinding(agent.ID)
	if err != nil {
		return "", "", "", err
	}

	messages := []llmMessage{}
	messages = append(messages, llmMessage{Role: "system", Content: strings.TrimSpace(agent.IdentityPrompt)})
	messages = append(messages, llmMessage{Role: "system", Content: "Mode: AgentOS v1. You can reason, write memory, and use two special output blocks:\n\nTo schedule a future task for yourself, output:\n````agenthq_schedule\n{\"type\":\"cron\",\"cron_expr\":\"0 9 * * MON\",\"message\":\"What to do\"}\n````\nor for a one-time trigger:\n````agenthq_schedule\n{\"type\":\"once\",\"start_at\":\"2026-05-28T09:00:00Z\",\"message\":\"What to do\"}\n````\n\nTo send a direct message to another agent, output:\n````agenthq_message\n{\"to_agent_id\":\"<agent-uuid>\",\"content\":\"Your message here\"}\n````"})
	messages = append(messages, llmMessage{Role: "system", Content: s.memoryContextForAgent(task.CompanyID, task.DepartmentID, agent.ID)})

	if task.ThreadID != "" {
		recent, _ := s.ListThreadMessages(task.ThreadID)
		if len(recent) > 24 {
			recent = recent[len(recent)-24:]
		}
		for _, m := range recent {
			role := "user"
			switch m.Role {
			case "agent":
				role = "assistant"
			case "system":
				role = "system"
			default:
				role = "user"
			}
			messages = append(messages, llmMessage{Role: role, Content: m.Content})
		}
	}

	if strings.TrimSpace(task.InputJSON) != "" {
		var in runtimeTaskInput
		if err := json.Unmarshal([]byte(task.InputJSON), &in); err == nil && strings.TrimSpace(in.Prompt) != "" {
			messages = append(messages, llmMessage{Role: "user", Content: in.Prompt})
		} else {
			messages = append(messages, llmMessage{Role: "user", Content: task.InputJSON})
		}
	}

	// Resolve owner user ID for per-user API key lookup
	ownerUserID := "admin"
	_ = s.db.QueryRow(`SELECT IFNULL(owner_user_id,'admin') FROM companies WHERE id = ?`, task.CompanyID).Scan(&ownerUserID)

	output, provider, model, err := s.callProvider(ownerUserID, profile, binding, messages)
	if err != nil {
		return "", provider, model, err
	}
	return output, provider, model, nil
}

func (s *Service) callProvider(ownerUserID string, profile AgentModelProfile, binding AgentModelBinding, messages []llmMessage) (string, string, string, error) {
	provider := strings.ToLower(strings.TrimSpace(profile.Provider))
	model := strings.TrimSpace(profile.Model)
	if provider == "" {
		provider = "openrouter"
	}
	if model == "" {
		model = s.getUserSettingString(ownerUserID, "default_model", "")
	}

	chain := []string{provider}
	var fallback []string
	_ = json.Unmarshal([]byte(profile.FallbackChainJSON), &fallback)
	for _, p := range fallback {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" && p != provider {
			chain = append(chain, p)
		}
	}

	var lastErr error
	for _, candidate := range unique(chain) {
		out, usedModel, err := s.callProviderOnce(ownerUserID, candidate, model, binding, messages)
		if err == nil {
			return strings.TrimSpace(out), candidate, usedModel, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no provider available")
	}
	return "", provider, model, lastErr
}

func (s *Service) callProviderOnce(ownerUserID, provider, model string, binding AgentModelBinding, messages []llmMessage) (string, string, error) {
	switch provider {
	case "openrouter":
		return s.callOpenRouter(ownerUserID, model, binding, messages)
	case "local":
		return s.callLocal(model, binding, messages)
	case "cli_codex":
		return s.callCLI("codex", model, messages)
	case "cli_claude":
		return s.callCLI("claude", model, messages)
	case "cli_gemini":
		return s.callCLI("gemini", model, messages)
	default:
		return "", model, fmt.Errorf("unsupported provider %s", provider)
	}
}

func (s *Service) callOpenRouter(ownerUserID, model string, binding AgentModelBinding, messages []llmMessage) (string, string, error) {
	// User's personal key takes priority over server-wide env key
	apiKey := ""
	if ownerUserID != "" && ownerUserID != "admin" {
		_ = s.db.QueryRow(`SELECT COALESCE(openrouter_api_key,'') FROM users WHERE id = ?`, ownerUserID).Scan(&apiKey)
		apiKey = strings.TrimSpace(apiKey)
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if apiKey == "" || apiKey == "your_openrouter_api_key_here" {
		return "", model, fmt.Errorf("OPENROUTER_API_KEY missing")
	}
	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	if binding.MaxTokens > 0 {
		payload["max_tokens"] = binding.MaxTokens
	}
	if binding.Temperature > 0 {
		payload["temperature"] = binding.Temperature
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Apollo AgentOS")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", model, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", model, fmt.Errorf("openrouter status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", model, err
	}
	if len(parsed.Choices) == 0 {
		return "", model, fmt.Errorf("empty choices")
	}
	return parsed.Choices[0].Message.Content, model, nil
}

func (s *Service) callLocal(model string, binding AgentModelBinding, messages []llmMessage) (string, string, error) {
	baseURL := strings.TrimSpace(os.Getenv("OLLAMA_API_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(s.getSettingString("embedding_api_url", "http://localhost:11434"))
	}
	if strings.HasSuffix(baseURL, "/api") {
		baseURL = strings.TrimSuffix(baseURL, "/api")
	}
	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	if binding.MaxTokens > 0 {
		payload["max_tokens"] = binding.MaxTokens
	}
	if binding.Temperature > 0 {
		payload["temperature"] = binding.Temperature
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/chat/completions", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer local")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", model, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", model, fmt.Errorf("local status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", model, err
	}
	if len(parsed.Choices) == 0 {
		return "", model, fmt.Errorf("empty choices")
	}
	return parsed.Choices[0].Message.Content, model, nil
}

func (s *Service) callCLI(cliName, model string, messages []llmMessage) (string, string, error) {
	if _, err := exec.LookPath(cliName); err != nil {
		return "", model, fmt.Errorf("%s not installed", cliName)
	}
	prompt := ""
	for _, m := range messages {
		if m.Role == "system" {
			prompt += "[SYSTEM]\n" + m.Content + "\n\n"
		} else if m.Role == "assistant" {
			prompt += "[ASSISTANT]\n" + m.Content + "\n\n"
		} else {
			prompt += "[USER]\n" + m.Content + "\n\n"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	switch cliName {
	case "codex":
		cmd = exec.CommandContext(ctx, "codex", "--model", model, "--dangerously-bypass-approvals-and-sandbox", prompt)
	case "claude":
		cmd = exec.CommandContext(ctx, "claude", "--model", model, "--dangerously-skip-permissions", "-p", prompt)
	default:
		cmd = exec.CommandContext(ctx, cliName, "-p", prompt)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", model, fmt.Errorf("%s failed: %s", cliName, strings.TrimSpace(string(out)))
	}
	return string(out), model, nil
}

func summarize(text string, max int) string {
	clean := strings.TrimSpace(text)
	if max <= 0 || len(clean) <= max {
		return clean
	}
	return clean[:max] + "..."
}

func unique(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		if _, ok := seen[it]; ok {
			continue
		}
		seen[it] = struct{}{}
		out = append(out, it)
	}
	return out
}

func (s *Service) policyAllowed(task AgentTask, action string, scope string, fallbackTier string) bool {
	dec := s.EvaluatePolicy(task.CompanyID, task.DepartmentID, task.AgentID, action, scope)
	if dec.Allowed && normalizeTier(dec.ApprovalTier) == "none" {
		return true
	}
	tier := normalizeTier(dec.ApprovalTier)
	if tier == "none" {
		tier = normalizeTier(fallbackTier)
	}
	if tier == "none" {
		tier = "tier2"
	}
	_ = s.RequestApproval(task.CompanyID, task.ID, action, tier, dec.Reason, map[string]interface{}{"scope": scope, "decision": dec})
	_, _ = s.db.Exec("UPDATE agent_tasks SET status='blocked', blocked_reason='approval_pending', updated_at=CURRENT_TIMESTAMP WHERE id = ?", task.ID)
	s.emitEvent(task.CompanyID, task.DepartmentID, task.AgentID, task.ThreadID, task.ID, "task_blocked", "warn", map[string]interface{}{"reason": "approval_pending", "action": action})
	return false
}

func (s *Service) threadForTask(taskID string) (AgentTask, AgentThread, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return AgentTask{}, AgentThread{}, err
	}
	if strings.TrimSpace(task.ThreadID) == "" {
		return task, AgentThread{}, sql.ErrNoRows
	}
	thread, err := s.GetThread(task.ThreadID)
	return task, thread, err
}

func (s *Service) BuildTaskPromptInput(prompt string) string {
	payload := runtimeTaskInput{Prompt: strings.TrimSpace(prompt)}
	b, _ := json.Marshal(payload)
	return string(b)
}

// extractAndApplySelfSchedule parses ````agenthq_schedule ... ```` blocks from agent output
// and creates schedules targeting the agent itself.
func (s *Service) extractAndApplySelfSchedule(output string, task AgentTask, agent Agent) {
	re := regexp.MustCompile("(?s)````agenthq_schedule\n(.*?)````")
	matches := re.FindAllStringSubmatch(output, -1)
	for _, m := range matches {
		var cmd struct {
			Type     string `json:"type"`
			CronExpr string `json:"cron_expr"`
			StartAt  string `json:"start_at"`
			Message  string `json:"message"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &cmd); err != nil {
			continue
		}
		schedType := cmd.Type
		if schedType == "" {
			if cmd.CronExpr != "" {
				schedType = "cron"
			} else {
				schedType = "once"
			}
		}
		payload, _ := json.Marshal(map[string]string{"prompt": cmd.Message})
		sch := Schedule{
			ID:            uuid.NewString(),
			CompanyID:     task.CompanyID,
			DepartmentID:  task.DepartmentID,
			TargetAgentID: agent.ID,
			ScheduleType:  schedType,
			CronExpr:      cmd.CronExpr,
			StartAt:       cmd.StartAt,
			Timezone:      "UTC",
			PayloadJSON:   string(payload),
			IsActive:      true,
		}
		if _, err := s.CreateSchedule(sch); err == nil {
			s.emitEvent(task.CompanyID, task.DepartmentID, agent.ID, task.ThreadID, task.ID, "agent_self_scheduled", "info", map[string]interface{}{"schedule_type": schedType})
		}
	}
}

// extractAndApplyInterAgentMessages parses ````agenthq_message ... ```` blocks and delivers them.
func (s *Service) extractAndApplyInterAgentMessages(output string, task AgentTask, agent Agent) {
	re := regexp.MustCompile("(?s)````agenthq_message\n(.*?)````")
	matches := re.FindAllStringSubmatch(output, -1)
	for _, m := range matches {
		var cmd struct {
			ToAgentID string `json:"to_agent_id"`
			Content   string `json:"content"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &cmd); err != nil {
			continue
		}
		if cmd.ToAgentID == "" || cmd.Content == "" {
			continue
		}
		if err := s.PostInterAgentMessage(task.CompanyID, agent.ID, cmd.ToAgentID, cmd.Content); err == nil {
			s.emitEvent(task.CompanyID, task.DepartmentID, agent.ID, task.ThreadID, task.ID, "inter_agent_message_sent", "info", map[string]interface{}{"to_agent_id": cmd.ToAgentID})
		}
	}
}
