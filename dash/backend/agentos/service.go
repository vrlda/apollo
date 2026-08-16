package agentos

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	db            *sql.DB
	workspaceRoot string

	mu          sync.Mutex
	running     map[string]struct{}
	subscribers map[string]map[chan AgentEvent]struct{}

	queueTick     time.Duration
	schedulerTick time.Duration
}

func NewService(db *sql.DB, workspaceRoot string) *Service {
	return &Service{
		db:            db,
		workspaceRoot: workspaceRoot,
		running:       map[string]struct{}{},
		subscribers:   map[string]map[chan AgentEvent]struct{}{},
		queueTick:     2 * time.Second,
		schedulerTick: 1 * time.Minute,
	}
}

func (s *Service) Start(ctx context.Context) {
	s.migrateLegacyChats()

	qTicker := time.NewTicker(s.queueTick)
	sTicker := time.NewTicker(s.schedulerTick)
	defer qTicker.Stop()
	defer sTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-qTicker.C:
			s.processQueue()
			s.resumeBlockedManagers()
		case <-sTicker.C:
			s.runSchedules()
		}
	}
}

func (s *Service) Subscribe(companyID string) (<-chan AgentEvent, func()) {
	ch := make(chan AgentEvent, 128)

	s.mu.Lock()
	if s.subscribers[companyID] == nil {
		s.subscribers[companyID] = map[chan AgentEvent]struct{}{}
	}
	s.subscribers[companyID][ch] = struct{}{}
	s.mu.Unlock()

	cancel := func() {
		s.mu.Lock()
		if subs := s.subscribers[companyID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(s.subscribers, companyID)
			}
		}
		s.mu.Unlock()
		close(ch)
	}

	return ch, cancel
}

func (s *Service) emitEvent(companyID, departmentID, agentID, threadID, taskID, eventType, severity string, payload interface{}) {
	if strings.TrimSpace(severity) == "" {
		severity = "info"
	}
	payloadJSON := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = string(b)
		}
	}

	res, err := s.db.Exec(`
		INSERT INTO agent_events (company_id, department_id, agent_id, thread_id, task_id, event_type, severity, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, companyID, departmentID, agentID, threadID, taskID, eventType, severity, payloadJSON)
	if err != nil {
		log.Printf("agentos event insert failed: %v", err)
		return
	}
	id, _ := res.LastInsertId()
	ev := AgentEvent{
		ID:           id,
		CompanyID:    companyID,
		DepartmentID: departmentID,
		AgentID:      agentID,
		ThreadID:     threadID,
		TaskID:       taskID,
		EventType:    eventType,
		Severity:     severity,
		PayloadJSON:  payloadJSON,
		CreatedAt:    time.Now(),
	}

	s.mu.Lock()
	for cid, subs := range s.subscribers {
		if cid != "" && cid != companyID {
			continue
		}
		for ch := range subs {
			select {
			case ch <- ev:
			default:
			}
		}
	}
	s.mu.Unlock()
}

func (s *Service) ListEvents(companyID, taskID string, sinceID int64, limit int) ([]AgentEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := `
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(thread_id,''), IFNULL(task_id,''), IFNULL(event_type,''), IFNULL(severity,'info'), IFNULL(payload_json,'{}'), IFNULL(created_at,'')
		FROM agent_events WHERE id > ?`
	args := []interface{}{sinceID}
	if strings.TrimSpace(companyID) != "" {
		query += " AND company_id = ?"
		args = append(args, companyID)
	}
	if strings.TrimSpace(taskID) != "" {
		query += " AND task_id = ?"
		args = append(args, taskID)
	}
	query += " ORDER BY id ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AgentEvent{}
	for rows.Next() {
		var ev AgentEvent
		var createdAt string
		if err := rows.Scan(&ev.ID, &ev.CompanyID, &ev.DepartmentID, &ev.AgentID, &ev.ThreadID, &ev.TaskID, &ev.EventType, &ev.Severity, &ev.PayloadJSON, &createdAt); err != nil {
			continue
		}
		ev.CreatedAt = parseDBTime(createdAt)
		out = append(out, ev)
	}
	return out, nil
}

func (s *Service) HealthStatus() map[string]interface{} {
	var queued, running, blocked, waiting int
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status='queued'").Scan(&queued)
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status='running'").Scan(&running)
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status='blocked'").Scan(&blocked)
	_ = s.db.QueryRow("SELECT COUNT(1) FROM agent_tasks WHERE status='waiting_input'").Scan(&waiting)

	return map[string]interface{}{
		"queue": map[string]interface{}{
			"queued":        queued,
			"running":       running,
			"blocked":       blocked,
			"waiting_input": waiting,
		},
		"scheduler":  s.SchedulerState(),
		"audit":      s.AuditVerify(),
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *Service) cleanWorkspacePath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && strings.TrimSpace(s.workspaceRoot) != "" {
		path = filepath.Join(s.workspaceRoot, path)
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func (s *Service) CreateCompany(ownerUserID, name, description, timezone, workspacePath, deployCommand string) (Company, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return Company{}, fmt.Errorf("owner_user_id is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Company{}, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(timezone) == "" {
		timezone = "UTC"
	}
	workspacePath = s.cleanWorkspacePath(workspacePath)
	deployCommand = strings.TrimSpace(deployCommand)
	id := uuid.NewString()
	slug := slugify(name + "-" + ownerUserID)
	_, err := s.db.Exec(`
		INSERT INTO companies (id, owner_user_id, name, slug, description, status, timezone, workspace_path, deploy_command, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, ownerUserID, name, slug, description, timezone, workspacePath, deployCommand)
	if err != nil {
		return Company{}, err
	}

	dept, err := s.CreateDepartment(id, "General", "general", "Default department")
	if err == nil {
		_, _ = s.CreateAgent(id, dept.ID, "Core Agent", "manager", "", "Primary coordinator agent")
	}
	// Bootstrap allow rules so a newly created company is operational without manual policy seeding.
	for _, action := range []string{"task_run", "delegate", "memory_write", "schedule_mutate", "hierarchy_mutate", "model_bind", "thread_write"} {
		_, _ = s.UpsertPolicy(PolicyRule{
			CompanyID:    id,
			DepartmentID: "",
			AgentID:      "",
			Action:       action,
			Effect:       "allow",
			ScopePattern: "*",
			ApprovalTier: "none",
		})
	}

	s.appendAudit("company_created", "company", id, "owner", map[string]interface{}{"name": name, "workspace_path": workspacePath})
	s.emitEvent(id, "", "", "", "", "company_created", "info", map[string]interface{}{"company_id": id, "name": name, "workspace_path": workspacePath})
	return s.GetCompany(id)
}

func (s *Service) ListCompanies(ownerUserID string) ([]Company, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	rows, err := s.db.Query(`
		SELECT id, IFNULL(owner_user_id,''), IFNULL(name,''), IFNULL(slug,''), IFNULL(description,''), IFNULL(status,'active'), IFNULL(timezone,'UTC'), IFNULL(workspace_path,''), IFNULL(deploy_command,''), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM companies WHERE owner_user_id = ? ORDER BY created_at DESC`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Company{}
	for rows.Next() {
		var c Company
		var createdAt, updatedAt string
		if err := rows.Scan(&c.ID, &c.OwnerUserID, &c.Name, &c.Slug, &c.Description, &c.Status, &c.Timezone, &c.WorkspacePath, &c.DeployCommand, &createdAt, &updatedAt); err != nil {
			continue
		}
		c.CreatedAt = parseDBTime(createdAt)
		c.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) GetCompany(id string) (Company, error) {
	var c Company
	var createdAt, updatedAt string
	err := s.db.QueryRow(`
		SELECT id, IFNULL(owner_user_id,''), IFNULL(name,''), IFNULL(slug,''), IFNULL(description,''), IFNULL(status,'active'), IFNULL(timezone,'UTC'), IFNULL(workspace_path,''), IFNULL(deploy_command,''), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM companies WHERE id = ?
	`, id).Scan(&c.ID, &c.OwnerUserID, &c.Name, &c.Slug, &c.Description, &c.Status, &c.Timezone, &c.WorkspacePath, &c.DeployCommand, &createdAt, &updatedAt)
	if err != nil {
		return Company{}, err
	}
	c.CreatedAt = parseDBTime(createdAt)
	c.UpdatedAt = parseDBTime(updatedAt)
	return c, nil
}

func (s *Service) UserOwnsCompany(ownerUserID, companyID string) bool {
	ownerUserID = strings.TrimSpace(ownerUserID)
	companyID = strings.TrimSpace(companyID)
	if ownerUserID == "" || companyID == "" {
		return false
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM companies WHERE id = ? AND owner_user_id = ?`, companyID, ownerUserID).Scan(&count)
	return count > 0
}

func (s *Service) UpdateCompany(id, name, description, status, timezone, workspacePath, deployCommand string) (Company, error) {
	current, err := s.GetCompany(id)
	if err != nil {
		return Company{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = current.Name
	}
	if strings.TrimSpace(status) == "" {
		status = current.Status
	}
	if strings.TrimSpace(timezone) == "" {
		timezone = current.Timezone
	}
	if strings.TrimSpace(workspacePath) == "" {
		workspacePath = current.WorkspacePath
	} else {
		workspacePath = s.cleanWorkspacePath(workspacePath)
	}
	if strings.TrimSpace(deployCommand) == "" {
		deployCommand = current.DeployCommand
	} else {
		deployCommand = strings.TrimSpace(deployCommand)
	}
	_, err = s.db.Exec(`UPDATE companies SET name = ?, slug = ?, description = ?, status = ?, timezone = ?, workspace_path = ?, deploy_command = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, slugify(name+"-"+current.OwnerUserID), description, status, timezone, workspacePath, deployCommand, id)
	if err != nil {
		return Company{}, err
	}
	s.appendAudit("company_updated", "company", id, "owner", map[string]interface{}{"name": name, "status": status, "workspace_path": workspacePath})
	return s.GetCompany(id)
}

func (s *Service) CreateDepartment(companyID, name, depType, description string) (Department, error) {
	if strings.TrimSpace(companyID) == "" || strings.TrimSpace(name) == "" {
		return Department{}, fmt.Errorf("company_id and name are required")
	}
	if strings.TrimSpace(depType) == "" {
		depType = "general"
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`
		INSERT INTO departments (id, company_id, name, type, description, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, companyID, name, depType, description)
	if err != nil {
		return Department{}, err
	}
	s.appendAudit("department_created", "department", id, "owner", map[string]interface{}{"company_id": companyID, "name": name})
	s.emitEvent(companyID, id, "", "", "", "department_created", "info", map[string]interface{}{"department_id": id, "name": name})
	return s.GetDepartment(id)
}

func (s *Service) ListDepartments(companyID string) ([]Department, error) {
	query := `SELECT id, IFNULL(company_id,''), IFNULL(name,''), IFNULL(type,''), IFNULL(description,''), IFNULL(status,'active'), IFNULL(created_at,''), IFNULL(updated_at,'') FROM departments`
	args := []interface{}{}
	if strings.TrimSpace(companyID) != "" {
		query += " WHERE company_id = ?"
		args = append(args, companyID)
	}
	query += " ORDER BY created_at ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Department{}
	for rows.Next() {
		var d Department
		var createdAt, updatedAt string
		if err := rows.Scan(&d.ID, &d.CompanyID, &d.Name, &d.Type, &d.Description, &d.Status, &createdAt, &updatedAt); err != nil {
			continue
		}
		d.CreatedAt = parseDBTime(createdAt)
		d.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, d)
	}
	return out, nil
}

func (s *Service) GetDepartment(id string) (Department, error) {
	var d Department
	var createdAt, updatedAt string
	err := s.db.QueryRow(`SELECT id, IFNULL(company_id,''), IFNULL(name,''), IFNULL(type,''), IFNULL(description,''), IFNULL(status,'active'), IFNULL(created_at,''), IFNULL(updated_at,'') FROM departments WHERE id = ?`, id).
		Scan(&d.ID, &d.CompanyID, &d.Name, &d.Type, &d.Description, &d.Status, &createdAt, &updatedAt)
	if err != nil {
		return Department{}, err
	}
	d.CreatedAt = parseDBTime(createdAt)
	d.UpdatedAt = parseDBTime(updatedAt)
	return d, nil
}

func (s *Service) UpdateDepartment(id, name, depType, description, status string) (Department, error) {
	cur, err := s.GetDepartment(id)
	if err != nil {
		return Department{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = cur.Name
	}
	if strings.TrimSpace(depType) == "" {
		depType = cur.Type
	}
	if strings.TrimSpace(description) == "" {
		description = cur.Description
	}
	if strings.TrimSpace(status) == "" {
		status = cur.Status
	}
	_, err = s.db.Exec(`UPDATE departments SET name = ?, type = ?, description = ?, status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, depType, description, status, id)
	if err != nil {
		return Department{}, err
	}
	s.appendAudit("department_updated", "department", id, "owner", map[string]interface{}{"name": name, "status": status})
	return s.GetDepartment(id)
}

func (s *Service) CreateAgent(companyID, departmentID, name, roleType, parentAgentID, identityPrompt string) (Agent, error) {
	if strings.TrimSpace(companyID) == "" || strings.TrimSpace(departmentID) == "" || strings.TrimSpace(name) == "" {
		return Agent{}, fmt.Errorf("company_id, department_id and name are required")
	}
	roleType = strings.ToLower(strings.TrimSpace(roleType))
	if roleType == "" {
		roleType = "worker"
	}
	if roleType != "manager" && roleType != "worker" {
		return Agent{}, fmt.Errorf("role_type must be manager or worker")
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`
		INSERT INTO agents (id, company_id, department_id, name, role_type, parent_agent_id, identity_prompt, status, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'idle', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, companyID, departmentID, name, roleType, parentAgentID, identityPrompt)
	if err != nil {
		return Agent{}, err
	}

	profileID := uuid.NewString()
	ownerUserID := "admin"
	_ = s.db.QueryRow(`SELECT IFNULL(owner_user_id,'admin') FROM companies WHERE id = ?`, companyID).Scan(&ownerUserID)
	defaultProvider := "openrouter"
	defaultModel := s.getUserSettingString(ownerUserID, "default_model", "")
	if strings.TrimSpace(defaultModel) == "" {
		defaultProvider = "local"
		defaultModel = "llama3.1:8b"
	}
	_, _ = s.db.Exec(`INSERT INTO agent_model_profiles (id, owner_user_id, provider, model, settings_json, fallback_chain_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, '{}', '["openrouter","local","cli_codex","cli_claude"]', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, profileID, ownerUserID, defaultProvider, defaultModel)
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO agent_model_bindings (agent_id, primary_profile_id, temperature, max_tokens, reasoning_effort) VALUES (?, ?, ?, ?, ?)`, id, profileID, 0.2, 1200, "standard")

	s.appendAudit("agent_created", "agent", id, "owner", map[string]interface{}{"name": name, "role_type": roleType})
	s.emitEvent(companyID, departmentID, id, "", "", "agent_created", "info", map[string]interface{}{"agent_id": id, "name": name})
	return s.GetAgent(id)
}

func (s *Service) ListAgents(companyID, departmentID string) ([]Agent, error) {
	query := `SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(name,''), IFNULL(role_type,'worker'), IFNULL(parent_agent_id,''), IFNULL(identity_prompt,''), IFNULL(status,'idle'), IFNULL(is_active,1), IFNULL(created_at,''), IFNULL(updated_at,'') FROM agents WHERE 1=1`
	args := []interface{}{}
	if strings.TrimSpace(companyID) != "" {
		query += " AND company_id = ?"
		args = append(args, companyID)
	}
	if strings.TrimSpace(departmentID) != "" {
		query += " AND department_id = ?"
		args = append(args, departmentID)
	}
	query += " ORDER BY created_at ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		ag, err := scanAgent(rows)
		if err != nil {
			continue
		}
		out = append(out, ag)
	}
	return out, nil
}

func (s *Service) GetAgent(id string) (Agent, error) {
	row := s.db.QueryRow(`SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(name,''), IFNULL(role_type,'worker'), IFNULL(parent_agent_id,''), IFNULL(identity_prompt,''), IFNULL(status,'idle'), IFNULL(is_active,1), IFNULL(created_at,''), IFNULL(updated_at,'') FROM agents WHERE id = ?`, id)
	return scanAgent(row)
}

func (s *Service) UpdateAgent(id, name, roleType, parentAgentID, identityPrompt, status string, isActive *bool) (Agent, error) {
	ag, err := s.GetAgent(id)
	if err != nil {
		return Agent{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = ag.Name
	}
	if strings.TrimSpace(roleType) == "" {
		roleType = ag.RoleType
	}
	if strings.TrimSpace(identityPrompt) == "" {
		identityPrompt = ag.IdentityPrompt
	}
	if strings.TrimSpace(status) == "" {
		status = ag.Status
	}
	if parentAgentID == "" {
		parentAgentID = ag.ParentAgentID
	}
	active := ag.IsActive
	if isActive != nil {
		active = *isActive
	}
	activeInt := 0
	if active {
		activeInt = 1
	}
	_, err = s.db.Exec(`UPDATE agents SET name = ?, role_type = ?, parent_agent_id = ?, identity_prompt = ?, status = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, roleType, parentAgentID, identityPrompt, status, activeInt, id)
	if err != nil {
		return Agent{}, err
	}
	s.appendAudit("agent_updated", "agent", id, "owner", map[string]interface{}{"name": name, "status": status})
	return s.GetAgent(id)
}

func (s *Service) AssignManager(agentID, managerID string) error {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(managerID) == "" {
		return fmt.Errorf("agent_id and manager_id are required")
	}
	agent, err := s.GetAgent(agentID)
	if err != nil {
		return err
	}
	manager, err := s.GetAgent(managerID)
	if err != nil {
		return err
	}
	if manager.RoleType != "manager" {
		return fmt.Errorf("parent must be a manager")
	}
	if agent.DepartmentID != manager.DepartmentID {
		return fmt.Errorf("department-scoped hierarchy: manager and agent must belong to same department")
	}
	if agent.ID == manager.ID {
		return fmt.Errorf("agent cannot manage itself")
	}
	if s.pathWouldCycle(agentID, managerID) {
		return fmt.Errorf("hierarchy cycle detected")
	}
	_, err = s.db.Exec("UPDATE agents SET parent_agent_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", managerID, agentID)
	if err == nil {
		s.appendAudit("hierarchy_assigned", "agent", agentID, "owner", map[string]interface{}{"manager_id": managerID})
		s.emitEvent(agent.CompanyID, agent.DepartmentID, agentID, "", "", "hierarchy_assigned", "info", map[string]interface{}{"manager_id": managerID})
	}
	return err
}

func (s *Service) pathWouldCycle(agentID, managerID string) bool {
	current := managerID
	for i := 0; i < 256; i++ {
		if current == "" {
			return false
		}
		if current == agentID {
			return true
		}
		var next string
		err := s.db.QueryRow("SELECT IFNULL(parent_agent_id,'') FROM agents WHERE id = ?", current).Scan(&next)
		if err != nil {
			return false
		}
		current = next
	}
	return true
}

func (s *Service) CreateThread(companyID, departmentID, agentID, title string) (AgentThread, error) {
	if strings.TrimSpace(companyID) == "" || strings.TrimSpace(agentID) == "" {
		return AgentThread{}, fmt.Errorf("company_id and agent_id are required")
	}
	if strings.TrimSpace(title) == "" {
		title = "New Thread"
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`INSERT INTO agent_threads (id, company_id, department_id, agent_id, title, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, id, companyID, departmentID, agentID, title)
	if err != nil {
		return AgentThread{}, err
	}
	s.emitEvent(companyID, departmentID, agentID, id, "", "thread_created", "info", map[string]interface{}{"thread_id": id})
	return s.GetThread(id)
}

func (s *Service) ListThreads(agentID string) ([]AgentThread, error) {
	query := `SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(title,''), IFNULL(status,'active'), IFNULL(created_at,''), IFNULL(updated_at,'') FROM agent_threads`
	args := []interface{}{}
	if strings.TrimSpace(agentID) != "" {
		query += " WHERE agent_id = ?"
		args = append(args, agentID)
	}
	query += " ORDER BY updated_at DESC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentThread{}
	for rows.Next() {
		th, err := scanThread(rows)
		if err != nil {
			continue
		}
		out = append(out, th)
	}
	return out, nil
}

func (s *Service) GetThread(id string) (AgentThread, error) {
	row := s.db.QueryRow(`SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(title,''), IFNULL(status,'active'), IFNULL(created_at,''), IFNULL(updated_at,'') FROM agent_threads WHERE id = ?`, id)
	return scanThread(row)
}

func (s *Service) ListThreadMessages(threadID string) ([]AgentMessage, error) {
	rows, err := s.db.Query(`SELECT id, IFNULL(thread_id,''), IFNULL(role,''), IFNULL(content,''), IFNULL(content_type,'text'), IFNULL(created_at,'') FROM agent_messages WHERE thread_id = ? ORDER BY id ASC`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentMessage{}
	for rows.Next() {
		var m AgentMessage
		var createdAt string
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.ContentType, &createdAt); err != nil {
			continue
		}
		m.CreatedAt = parseDBTime(createdAt)
		out = append(out, m)
	}
	return out, nil
}

func (s *Service) AddThreadMessage(threadID, role, content, contentType string) (AgentMessage, error) {
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(role) == "" {
		return AgentMessage{}, fmt.Errorf("thread_id and role are required")
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "text"
	}
	res, err := s.db.Exec(`INSERT INTO agent_messages (thread_id, role, content, content_type, text_embedding, created_at) VALUES (?, ?, ?, ?, '', CURRENT_TIMESTAMP)`, threadID, role, content, contentType)
	if err != nil {
		return AgentMessage{}, err
	}
	_, _ = s.db.Exec("UPDATE agent_threads SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", threadID)
	id, _ := res.LastInsertId()
	row := s.db.QueryRow(`SELECT id, IFNULL(thread_id,''), IFNULL(role,''), IFNULL(content,''), IFNULL(content_type,'text'), IFNULL(created_at,'') FROM agent_messages WHERE id = ?`, id)
	var m AgentMessage
	var createdAt string
	if err := row.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.ContentType, &createdAt); err != nil {
		return AgentMessage{}, err
	}
	m.CreatedAt = parseDBTime(createdAt)
	return m, nil
}

func (s *Service) CreateTask(input AgentTask) (AgentTask, error) {
	if strings.TrimSpace(input.CompanyID) == "" || strings.TrimSpace(input.AgentID) == "" {
		return AgentTask{}, fmt.Errorf("company_id and agent_id are required")
	}
	if strings.TrimSpace(input.Type) == "" {
		input.Type = "conversation"
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "queued"
	}
	if input.Priority <= 0 {
		input.Priority = 50
	}
	if strings.TrimSpace(input.ID) == "" {
		input.ID = uuid.NewString()
	}
	_, err := s.db.Exec(`
		INSERT INTO agent_tasks (id, company_id, department_id, agent_id, requested_by, parent_task_id, thread_id, type, status, priority, input_json, result_json, blocked_reason, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, IFNULL(?,''), '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL)
	`, input.ID, input.CompanyID, input.DepartmentID, input.AgentID, input.RequestedBy, input.ParentTaskID, input.ThreadID, input.Type, input.Status, input.Priority, input.InputJSON, input.ResultJSON)
	if err != nil {
		return AgentTask{}, err
	}
	s.emitEvent(input.CompanyID, input.DepartmentID, input.AgentID, input.ThreadID, input.ID, "task_created", "info", map[string]interface{}{"task_id": input.ID, "type": input.Type})
	return s.GetTask(input.ID)
}

func (s *Service) GetTask(taskID string) (AgentTask, error) {
	row := s.db.QueryRow(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(requested_by,''), IFNULL(parent_task_id,''), IFNULL(thread_id,''), IFNULL(type,''), IFNULL(status,''), IFNULL(priority,50), IFNULL(input_json,''), IFNULL(result_json,''), IFNULL(blocked_reason,''), IFNULL(created_at,''), IFNULL(updated_at,''), IFNULL(completed_at,'')
		FROM agent_tasks WHERE id = ?
	`, taskID)
	return scanTask(row)
}

func (s *Service) ListTasks(companyID, agentID, status string, limit int) ([]AgentTask, error) {
	if limit <= 0 || limit > 400 {
		limit = 120
	}
	query := `SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(requested_by,''), IFNULL(parent_task_id,''), IFNULL(thread_id,''), IFNULL(type,''), IFNULL(status,''), IFNULL(priority,50), IFNULL(input_json,''), IFNULL(result_json,''), IFNULL(blocked_reason,''), IFNULL(created_at,''), IFNULL(updated_at,''), IFNULL(completed_at,'') FROM agent_tasks WHERE 1=1`
	args := []interface{}{}
	if strings.TrimSpace(companyID) != "" {
		query += " AND company_id = ?"
		args = append(args, companyID)
	}
	if strings.TrimSpace(agentID) != "" {
		query += " AND agent_id = ?"
		args = append(args, agentID)
	}
	if strings.TrimSpace(status) != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY priority ASC, created_at ASC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentTask{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Service) CancelTask(taskID string) error {
	t, err := s.GetTask(taskID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE agent_tasks SET status='canceled', completed_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id = ?", taskID)
	if err == nil {
		s.emitEvent(t.CompanyID, t.DepartmentID, t.AgentID, t.ThreadID, t.ID, "task_canceled", "warn", map[string]interface{}{"task_id": taskID})
		s.appendAudit("task_canceled", "task", taskID, t.RequestedBy, nil)
	}
	return err
}

func (s *Service) RetryTask(taskID string) error {
	t, err := s.GetTask(taskID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE agent_tasks SET status='queued', blocked_reason='', updated_at=CURRENT_TIMESTAMP, completed_at=NULL WHERE id = ?", taskID)
	if err == nil {
		s.emitEvent(t.CompanyID, t.DepartmentID, t.AgentID, t.ThreadID, t.ID, "task_retried", "info", map[string]interface{}{"task_id": taskID})
		s.appendAudit("task_retried", "task", taskID, t.RequestedBy, nil)
	}
	return err
}

func (s *Service) ListRuns(taskID string) ([]AgentRun, error) {
	rows, err := s.db.Query(`SELECT id, IFNULL(task_id,''), IFNULL(attempt,1), IFNULL(status,''), IFNULL(provider,''), IFNULL(model,''), IFNULL(started_at,''), IFNULL(ended_at,''), IFNULL(summary,''), IFNULL(error,'') FROM agent_runs WHERE task_id = ? ORDER BY started_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRun{}
	for rows.Next() {
		var r AgentRun
		var startedAt, endedAt string
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Attempt, &r.Status, &r.Provider, &r.Model, &startedAt, &endedAt, &r.Summary, &r.Error); err != nil {
			continue
		}
		r.StartedAt = parseDBTime(startedAt)
		if strings.TrimSpace(endedAt) != "" {
			t := parseDBTime(endedAt)
			r.EndedAt = &t
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) CreateModelProfile(ownerUserID, provider, model, settingsJSON, fallbackChainJSON string) (AgentModelProfile, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return AgentModelProfile{}, fmt.Errorf("owner_user_id is required")
	}
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
		return AgentModelProfile{}, fmt.Errorf("provider and model are required")
	}
	if strings.TrimSpace(settingsJSON) == "" {
		settingsJSON = "{}"
	}
	if strings.TrimSpace(fallbackChainJSON) == "" {
		fallbackChainJSON = "[]"
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`INSERT INTO agent_model_profiles (id, owner_user_id, provider, model, settings_json, fallback_chain_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, id, ownerUserID, provider, model, settingsJSON, fallbackChainJSON)
	if err != nil {
		return AgentModelProfile{}, err
	}
	return s.GetModelProfile(id)
}

func (s *Service) ListModelProfiles(ownerUserID string) ([]AgentModelProfile, error) {
	rows, err := s.db.Query(`SELECT id, IFNULL(owner_user_id,''), IFNULL(provider,''), IFNULL(model,''), IFNULL(settings_json,'{}'), IFNULL(fallback_chain_json,'[]'), IFNULL(created_at,''), IFNULL(updated_at,'') FROM agent_model_profiles WHERE owner_user_id = ? ORDER BY updated_at DESC`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentModelProfile{}
	for rows.Next() {
		var p AgentModelProfile
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.OwnerUserID, &p.Provider, &p.Model, &p.SettingsJSON, &p.FallbackChainJSON, &createdAt, &updatedAt); err != nil {
			continue
		}
		p.CreatedAt = parseDBTime(createdAt)
		p.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, p)
	}
	return out, nil
}

func (s *Service) GetModelProfile(id string) (AgentModelProfile, error) {
	var p AgentModelProfile
	var createdAt, updatedAt string
	err := s.db.QueryRow(`SELECT id, IFNULL(owner_user_id,''), IFNULL(provider,''), IFNULL(model,''), IFNULL(settings_json,'{}'), IFNULL(fallback_chain_json,'[]'), IFNULL(created_at,''), IFNULL(updated_at,'') FROM agent_model_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.OwnerUserID, &p.Provider, &p.Model, &p.SettingsJSON, &p.FallbackChainJSON, &createdAt, &updatedAt)
	if err != nil {
		return AgentModelProfile{}, err
	}
	p.CreatedAt = parseDBTime(createdAt)
	p.UpdatedAt = parseDBTime(updatedAt)
	return p, nil
}

func (s *Service) BindAgentModel(agentID, profileID string, temperature float64, maxTokens int, reasoningEffort string) error {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(profileID) == "" {
		return fmt.Errorf("agent_id and primary_profile_id are required")
	}
	if maxTokens <= 0 {
		maxTokens = 1200
	}
	if strings.TrimSpace(reasoningEffort) == "" {
		reasoningEffort = "standard"
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO agent_model_bindings (agent_id, primary_profile_id, temperature, max_tokens, reasoning_effort) VALUES (?, ?, ?, ?, ?)`, agentID, profileID, temperature, maxTokens, reasoningEffort)
	if err == nil {
		s.appendAudit("agent_model_bound", "agent", agentID, "owner", map[string]interface{}{"profile_id": profileID, "temperature": temperature})
	}
	return err
}

// GetAgentBinding is the public version of getBinding for use by handlers.
func (s *Service) GetAgentBinding(agentID string) (AgentModelBinding, AgentModelProfile, error) {
	return s.getBinding(agentID)
}

func (s *Service) getBinding(agentID string) (AgentModelBinding, AgentModelProfile, error) {
	var b AgentModelBinding
	err := s.db.QueryRow(`SELECT IFNULL(agent_id,''), IFNULL(primary_profile_id,''), IFNULL(temperature,0.2), IFNULL(max_tokens,1200), IFNULL(reasoning_effort,'standard') FROM agent_model_bindings WHERE agent_id = ?`, agentID).
		Scan(&b.AgentID, &b.PrimaryProfileID, &b.Temperature, &b.MaxTokens, &b.ReasoningEffort)
	if err != nil {
		return AgentModelBinding{}, AgentModelProfile{}, err
	}
	p, err := s.GetModelProfile(b.PrimaryProfileID)
	if err != nil {
		return AgentModelBinding{}, AgentModelProfile{}, err
	}
	return b, p, nil
}

func (s *Service) getSettingString(key, fallback string) string {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// getUserSettingString reads from user_settings for a specific user; falls back to global settings.
func (s *Service) getUserSettingString(userID, key, fallback string) string {
	if userID != "" && userID != "admin" {
		var value string
		err := s.db.QueryRow("SELECT value FROM user_settings WHERE user_id = ? AND key = ?", userID, key).Scan(&value)
		if err == nil && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return s.getSettingString(key, fallback)
}

func parseDBTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	formats := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	for _, f := range formats {
		if t, err := time.Parse(f, value); err == nil {
			return t
		}
	}
	return time.Now()
}

func slugify(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return "item"
	}
	v = strings.ReplaceAll(v, " ", "-")
	out := make([]rune, 0, len(v))
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out = append(out, r)
		} else {
			out = append(out, '-')
		}
	}
	res := strings.Trim(strings.ReplaceAll(string(out), "--", "-"), "-")
	if res == "" {
		return "item"
	}
	if len(res) > 64 {
		return res[:64]
	}
	return res
}

func scanAgent(scanner interface {
	Scan(dest ...interface{}) error
}) (Agent, error) {
	var ag Agent
	var createdAt, updatedAt string
	var isActive int
	err := scanner.Scan(&ag.ID, &ag.CompanyID, &ag.DepartmentID, &ag.Name, &ag.RoleType, &ag.ParentAgentID, &ag.IdentityPrompt, &ag.Status, &isActive, &createdAt, &updatedAt)
	if err != nil {
		return Agent{}, err
	}
	ag.IsActive = isActive == 1
	ag.CreatedAt = parseDBTime(createdAt)
	ag.UpdatedAt = parseDBTime(updatedAt)
	return ag, nil
}

func scanThread(scanner interface {
	Scan(dest ...interface{}) error
}) (AgentThread, error) {
	var t AgentThread
	var createdAt, updatedAt string
	err := scanner.Scan(&t.ID, &t.CompanyID, &t.DepartmentID, &t.AgentID, &t.Title, &t.Status, &createdAt, &updatedAt)
	if err != nil {
		return AgentThread{}, err
	}
	t.CreatedAt = parseDBTime(createdAt)
	t.UpdatedAt = parseDBTime(updatedAt)
	return t, nil
}

func scanTask(scanner interface {
	Scan(dest ...interface{}) error
}) (AgentTask, error) {
	var t AgentTask
	var createdAt, updatedAt, completedAt string
	err := scanner.Scan(&t.ID, &t.CompanyID, &t.DepartmentID, &t.AgentID, &t.RequestedBy, &t.ParentTaskID, &t.ThreadID, &t.Type, &t.Status, &t.Priority, &t.InputJSON, &t.ResultJSON, &t.BlockedReason, &createdAt, &updatedAt, &completedAt)
	if err != nil {
		return AgentTask{}, err
	}
	t.CreatedAt = parseDBTime(createdAt)
	t.UpdatedAt = parseDBTime(updatedAt)
	if strings.TrimSpace(completedAt) != "" {
		tm := parseDBTime(completedAt)
		t.CompletedAt = &tm
	}
	return t, nil
}

func (s *Service) migrateLegacyChats() {
	var companiesCount int
	_ = s.db.QueryRow("SELECT COUNT(1) FROM companies").Scan(&companiesCount)
	if companiesCount > 0 {
		return
	}

	var legacyCount int
	_ = s.db.QueryRow("SELECT COUNT(1) FROM chat_sessions").Scan(&legacyCount)
	if legacyCount == 0 {
		return
	}

	log.Printf("agentos migration: migrating %d legacy chat sessions", legacyCount)

	settingsRaw := s.getSettingString("managed_projects", "[]")
	var projects []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(settingsRaw), &projects)

	companyByPath := map[string]string{}
	if len(projects) == 0 {
		comp, err := s.CreateCompany("admin", "Migrated Workspace", "Auto-migrated from legacy chats", "UTC", "", "")
		if err == nil {
			companyByPath[""] = comp.ID
		}
	} else {
		for _, p := range projects {
			comp, err := s.CreateCompany("admin", p.Name, "Auto-migrated from managed project", "UTC", p.Path, "")
			if err == nil {
				companyByPath[strings.TrimSpace(p.Path)] = comp.ID
			}
		}
	}

	rows, err := s.db.Query("SELECT id, IFNULL(title,''), IFNULL(project_path,'') FROM chat_sessions ORDER BY created_at ASC")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var sid, title, projectPath string
		if err := rows.Scan(&sid, &title, &projectPath); err != nil {
			continue
		}
		companyID := companyByPath[strings.TrimSpace(projectPath)]
		if companyID == "" {
			for _, cid := range companyByPath {
				companyID = cid
				break
			}
		}
		if companyID == "" {
			continue
		}
		depts, _ := s.ListDepartments(companyID)
		if len(depts) == 0 {
			continue
		}
		agents, _ := s.ListAgents(companyID, depts[0].ID)
		if len(agents) == 0 {
			continue
		}
		thread, err := s.CreateThread(companyID, depts[0].ID, agents[0].ID, title)
		if err != nil {
			continue
		}

		msgRows, err := s.db.Query("SELECT role, content FROM chat_messages WHERE session_id = ? ORDER BY id ASC", sid)
		if err == nil {
			for msgRows.Next() {
				var role, content string
				if err := msgRows.Scan(&role, &content); err != nil {
					continue
				}
				mappedRole := role
				if role == "assistant" {
					mappedRole = "agent"
				}
				_, _ = s.AddThreadMessage(thread.ID, mappedRole, content, "text")
			}
			msgRows.Close()
		}

		_, _ = s.db.Exec("INSERT INTO legacy_migration_map (legacy_type, legacy_id, new_type, new_id, created_at) VALUES ('chat_session', ?, 'agent_thread', ?, CURRENT_TIMESTAMP)", sid, thread.ID)
	}
}

// GetOrCreateInterAgentThread returns the shared thread between two agents (or creates one).
func (s *Service) GetOrCreateInterAgentThread(companyID, agentA, agentB string) (AgentThread, error) {
	var thread AgentThread
	var createdAt, updatedAt string
	err := s.db.QueryRow(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(title,''), IFNULL(status,''), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM agent_threads
		WHERE company_id = ? AND title = ? AND status != 'archived'
		LIMIT 1
	`, companyID, "inter:"+agentA+":"+agentB).Scan(
		&thread.ID, &thread.CompanyID, &thread.DepartmentID, &thread.AgentID,
		&thread.Title, &thread.Status, &createdAt, &updatedAt)
	if err == nil {
		thread.CreatedAt = parseDBTime(createdAt)
		thread.UpdatedAt = parseDBTime(updatedAt)
		return thread, nil
	}
	// Try reverse order too
	err = s.db.QueryRow(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(title,''), IFNULL(status,''), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM agent_threads
		WHERE company_id = ? AND title = ? AND status != 'archived'
		LIMIT 1
	`, companyID, "inter:"+agentB+":"+agentA).Scan(
		&thread.ID, &thread.CompanyID, &thread.DepartmentID, &thread.AgentID,
		&thread.Title, &thread.Status, &createdAt, &updatedAt)
	if err == nil {
		thread.CreatedAt = parseDBTime(createdAt)
		thread.UpdatedAt = parseDBTime(updatedAt)
		return thread, nil
	}
	// Create new thread
	id := uuid.NewString()
	_, err = s.db.Exec(`
		INSERT INTO agent_threads (id, company_id, department_id, agent_id, title, status, created_at, updated_at)
		VALUES (?, ?, '', ?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, companyID, agentA, "inter:"+agentA+":"+agentB)
	if err != nil {
		return AgentThread{}, err
	}
	return s.GetThread(id)
}

// PostInterAgentMessage sends a direct message from one agent to another.
func (s *Service) PostInterAgentMessage(companyID, fromAgentID, toAgentID, content string) error {
	thread, err := s.GetOrCreateInterAgentThread(companyID, fromAgentID, toAgentID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO agent_messages (thread_id, role, content, content_type, text_embedding, created_at)
		VALUES (?, ?, ?, 'text', '', CURRENT_TIMESTAMP)
	`, thread.ID, "agent:"+fromAgentID, content)
	return err
}

// GetAgentInbox returns all inter-agent threads for an agent (as sender or receiver).
func (s *Service) GetAgentInbox(companyID, agentID string) ([]map[string]interface{}, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.title, t.status, t.created_at,
		       m.role, m.content, m.created_at as msg_at
		FROM agent_threads t
		LEFT JOIN agent_messages m ON m.thread_id = t.id
		WHERE t.company_id = ? AND (t.title LIKE ? OR t.title LIKE ?)
		  AND t.title LIKE 'inter:%'
		ORDER BY m.created_at DESC
		LIMIT 200
	`, companyID, "inter:"+agentID+":%", "inter:%:"+agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var threadID, title, status, createdAt, role, content, msgAt string
		if err := rows.Scan(&threadID, &title, &status, &createdAt, &role, &content, &msgAt); err != nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"thread_id":  threadID,
			"title":      title,
			"role":       role,
			"content":    content,
			"created_at": msgAt,
		})
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	return result, nil
}
