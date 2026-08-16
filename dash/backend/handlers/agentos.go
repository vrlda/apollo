package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danilrybalkin/apollo-dash/agentos"
)

var agentOSService *agentos.Service

func SetAgentOSService(svc *agentos.Service) {
	agentOSService = svc
}

func ensureAgentOSAvailable(w http.ResponseWriter) bool {
	if agentOSService == nil {
		http.Error(w, "agentos is not initialized", http.StatusServiceUnavailable)
		return false
	}
	if !getSettingBool("agentos_enabled", true) {
		http.Error(w, "agentos is disabled in settings", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := strings.TrimSpace(CurrentUserID(r))
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return userID, true
}

func requireCompanyAccess(w http.ResponseWriter, ownerUserID, companyID string) bool {
	if strings.TrimSpace(companyID) == "" {
		http.Error(w, "company_id is required", http.StatusBadRequest)
		return false
	}
	if !agentOSService.UserOwnsCompany(ownerUserID, companyID) {
		http.Error(w, "not found", http.StatusNotFound)
		return false
	}
	return true
}

func requireDepartmentAccess(w http.ResponseWriter, ownerUserID, departmentID string) (agentos.Department, bool) {
	item, err := agentOSService.GetDepartment(departmentID)
	if err != nil || !agentOSService.UserOwnsCompany(ownerUserID, item.CompanyID) {
		http.Error(w, "not found", http.StatusNotFound)
		return agentos.Department{}, false
	}
	return item, true
}

func requireAgentAccess(w http.ResponseWriter, ownerUserID, agentID string) (agentos.Agent, bool) {
	item, err := agentOSService.GetAgent(agentID)
	if err != nil || !agentOSService.UserOwnsCompany(ownerUserID, item.CompanyID) {
		http.Error(w, "not found", http.StatusNotFound)
		return agentos.Agent{}, false
	}
	return item, true
}

func requireThreadAccess(w http.ResponseWriter, ownerUserID, threadID string) (agentos.AgentThread, bool) {
	item, err := agentOSService.GetThread(threadID)
	if err != nil || !agentOSService.UserOwnsCompany(ownerUserID, item.CompanyID) {
		http.Error(w, "not found", http.StatusNotFound)
		return agentos.AgentThread{}, false
	}
	return item, true
}

func requireTaskAccess(w http.ResponseWriter, ownerUserID, taskID string) (agentos.AgentTask, bool) {
	item, err := agentOSService.GetTask(taskID)
	if err != nil || !agentOSService.UserOwnsCompany(ownerUserID, item.CompanyID) {
		http.Error(w, "not found", http.StatusNotFound)
		return agentos.AgentTask{}, false
	}
	return item, true
}

func requireScheduleAccess(w http.ResponseWriter, ownerUserID, scheduleID string) (agentos.Schedule, bool) {
	item, err := agentOSService.GetSchedule(scheduleID)
	if err != nil || !agentOSService.UserOwnsCompany(ownerUserID, item.CompanyID) {
		http.Error(w, "not found", http.StatusNotFound)
		return agentos.Schedule{}, false
	}
	return item, true
}

func AgentOSCompaniesHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := agentOSService.ListCompanies(ownerUserID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			Name          string `json:"name"`
			Description   string `json:"description"`
			Timezone      string `json:"timezone"`
			WorkspacePath string `json:"workspace_path"`
			DeployCommand string `json:"deploy_command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.CreateCompany(ownerUserID, req.Name, req.Description, req.Timezone, req.WorkspacePath, req.DeployCommand)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSCompanyByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/companies/")
	id = strings.TrimSpace(strings.Trim(id, "/"))
	if id == "" {
		http.Error(w, "company id is required", http.StatusBadRequest)
		return
	}
	if !requireCompanyAccess(w, ownerUserID, id) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := agentOSService.GetCompany(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var req struct {
			Name          string `json:"name"`
			Description   string `json:"description"`
			Status        string `json:"status"`
			Timezone      string `json:"timezone"`
			WorkspacePath string `json:"workspace_path"`
			DeployCommand string `json:"deploy_command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.UpdateCompany(id, req.Name, req.Description, req.Status, req.Timezone, req.WorkspacePath, req.DeployCommand)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSDepartmentsHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
		if companyID != "" && !requireCompanyAccess(w, ownerUserID, companyID) {
			return
		}
		if companyID == "" {
			companies, err := agentOSService.ListCompanies(ownerUserID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var all []agentos.Department
			for _, company := range companies {
				items, err := agentOSService.ListDepartments(company.ID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				all = append(all, items...)
			}
			writeJSON(w, http.StatusOK, all)
			return
		}
		items, err := agentOSService.ListDepartments(companyID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			CompanyID   string `json:"company_id"`
			Name        string `json:"name"`
			Type        string `json:"type"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
			return
		}
		item, err := agentOSService.CreateDepartment(req.CompanyID, req.Name, req.Type, req.Description)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSDepartmentByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/departments/")
	id = strings.TrimSpace(strings.Trim(id, "/"))
	if id == "" {
		http.Error(w, "department id is required", http.StatusBadRequest)
		return
	}
	if _, ok := requireDepartmentAccess(w, ownerUserID, id); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := agentOSService.GetDepartment(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var req struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Description string `json:"description"`
			Status      string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.UpdateDepartment(id, req.Name, req.Type, req.Description, req.Status)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSAgentsHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
		departmentID := strings.TrimSpace(r.URL.Query().Get("department_id"))
		if companyID != "" && !requireCompanyAccess(w, ownerUserID, companyID) {
			return
		}
		if departmentID != "" {
			dept, ok := requireDepartmentAccess(w, ownerUserID, departmentID)
			if !ok {
				return
			}
			if companyID == "" {
				companyID = dept.CompanyID
			}
		}
		if companyID == "" {
			companies, err := agentOSService.ListCompanies(ownerUserID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var all []agentos.Agent
			for _, company := range companies {
				items, err := agentOSService.ListAgents(company.ID, departmentID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				all = append(all, items...)
			}
			writeJSON(w, http.StatusOK, all)
			return
		}
		items, err := agentOSService.ListAgents(companyID, departmentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			CompanyID      string `json:"company_id"`
			DepartmentID   string `json:"department_id"`
			Name           string `json:"name"`
			RoleType       string `json:"role_type"`
			ParentAgentID  string `json:"parent_agent_id"`
			IdentityPrompt string `json:"identity_prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
			return
		}
		dept, ok := requireDepartmentAccess(w, ownerUserID, req.DepartmentID)
		if !ok {
			return
		}
		if dept.CompanyID != req.CompanyID {
			http.Error(w, "department does not belong to company", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ParentAgentID) != "" {
			parent, ok := requireAgentAccess(w, ownerUserID, req.ParentAgentID)
			if !ok {
				return
			}
			if parent.CompanyID != req.CompanyID {
				http.Error(w, "parent agent does not belong to company", http.StatusBadRequest)
				return
			}
		}
		item, err := agentOSService.CreateAgent(req.CompanyID, req.DepartmentID, req.Name, req.RoleType, req.ParentAgentID, req.IdentityPrompt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSAgentByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.Error(w, "agent id is required", http.StatusBadRequest)
		return
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	if _, ok := requireAgentAccess(w, ownerUserID, id); !ok {
		return
	}

	if len(parts) == 3 && parts[1] == "hierarchy" && parts[2] == "assign" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ManagerID string `json:"manager_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ManagerID == "" {
			http.Error(w, "manager_id is required", http.StatusBadRequest)
			return
		}
		if _, ok := requireAgentAccess(w, ownerUserID, req.ManagerID); !ok {
			return
		}
		if err := agentOSService.AssignManager(id, req.ManagerID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if len(parts) == 2 && parts[1] == "model-bind" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			PrimaryProfileID string  `json:"primary_profile_id"`
			Temperature      float64 `json:"temperature"`
			MaxTokens        int     `json:"max_tokens"`
			ReasoningEffort  string  `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.PrimaryProfileID) == "" {
			http.Error(w, "primary_profile_id is required", http.StatusBadRequest)
			return
		}
		profile, err := agentOSService.GetModelProfile(req.PrimaryProfileID)
		if err != nil || profile.OwnerUserID != ownerUserID {
			http.Error(w, "model profile not found", http.StatusNotFound)
			return
		}
		if err := agentOSService.BindAgentModel(id, req.PrimaryProfileID, req.Temperature, req.MaxTokens, req.ReasoningEffort); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := agentOSService.GetAgent(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var req struct {
			Name           string `json:"name"`
			RoleType       string `json:"role_type"`
			ParentAgentID  string `json:"parent_agent_id"`
			IdentityPrompt string `json:"identity_prompt"`
			Status         string `json:"status"`
			IsActive       *bool  `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ParentAgentID) != "" {
			if _, ok := requireAgentAccess(w, ownerUserID, req.ParentAgentID); !ok {
				return
			}
		}
		item, err := agentOSService.UpdateAgent(id, req.Name, req.RoleType, req.ParentAgentID, req.IdentityPrompt, req.Status, req.IsActive)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSModelProfilesHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := agentOSService.ListModelProfiles(ownerUserID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			Provider          string `json:"provider"`
			Model             string `json:"model"`
			SettingsJSON      string `json:"settings_json"`
			FallbackChainJSON string `json:"fallback_chain_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.CreateModelProfile(ownerUserID, req.Provider, req.Model, req.SettingsJSON, req.FallbackChainJSON)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSThreadsHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
		if agentID == "" {
			writeJSON(w, http.StatusOK, []agentos.AgentThread{})
			return
		}
		if _, ok := requireAgentAccess(w, ownerUserID, agentID); !ok {
			return
		}
		items, err := agentOSService.ListThreads(agentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			CompanyID    string `json:"company_id"`
			DepartmentID string `json:"department_id"`
			AgentID      string `json:"agent_id"`
			Title        string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
			return
		}
		if strings.TrimSpace(req.DepartmentID) != "" {
			dept, ok := requireDepartmentAccess(w, ownerUserID, req.DepartmentID)
			if !ok {
				return
			}
			if dept.CompanyID != req.CompanyID {
				http.Error(w, "department does not belong to company", http.StatusBadRequest)
				return
			}
		}
		agent, ok := requireAgentAccess(w, ownerUserID, req.AgentID)
		if !ok {
			return
		}
		if agent.CompanyID != req.CompanyID {
			http.Error(w, "agent does not belong to company", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.CreateThread(req.CompanyID, req.DepartmentID, req.AgentID, req.Title)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSThreadMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/threads/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "messages" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	threadID := parts[0]
	thread, ok := requireThreadAccess(w, ownerUserID, threadID)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		items, err := agentOSService.ListThreadMessages(threadID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			Content string `json:"content"`
			Role    string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
			http.Error(w, "content is required", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Role) == "" {
			req.Role = "user"
		}
		msg, err := agentOSService.AddThreadMessage(threadID, req.Role, req.Content, "text")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Role == "user" {
			task, err := agentOSService.CreateTask(agentos.AgentTask{
				CompanyID:    thread.CompanyID,
				DepartmentID: thread.DepartmentID,
				AgentID:      thread.AgentID,
				RequestedBy:  "user",
				ThreadID:     thread.ID,
				Type:         "conversation",
				Status:       "queued",
				Priority:     50,
				InputJSON:    agentOSService.BuildTaskPromptInput(req.Content),
			})
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{"message": msg, "task_error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"message": msg, "task": task})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"message": msg})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSTasksHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
		agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
		if companyID != "" && !requireCompanyAccess(w, ownerUserID, companyID) {
			return
		}
		if agentID != "" {
			agent, ok := requireAgentAccess(w, ownerUserID, agentID)
			if !ok {
				return
			}
			if companyID == "" {
				companyID = agent.CompanyID
			}
		}
		if companyID == "" {
			companies, err := agentOSService.ListCompanies(ownerUserID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var all []agentos.AgentTask
			for _, company := range companies {
				items, err := agentOSService.ListTasks(company.ID, agentID, r.URL.Query().Get("status"), limit)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				all = append(all, items...)
			}
			writeJSON(w, http.StatusOK, all)
			return
		}
		items, err := agentOSService.ListTasks(companyID, agentID, r.URL.Query().Get("status"), limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			CompanyID    string `json:"company_id"`
			DepartmentID string `json:"department_id"`
			AgentID      string `json:"agent_id"`
			RequestedBy  string `json:"requested_by"`
			ThreadID     string `json:"thread_id"`
			Type         string `json:"type"`
			Priority     int    `json:"priority"`
			Prompt       string `json:"prompt"`
			InputJSON    string `json:"input_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
			return
		}
		agent, ok := requireAgentAccess(w, ownerUserID, req.AgentID)
		if !ok {
			return
		}
		if agent.CompanyID != req.CompanyID {
			http.Error(w, "agent does not belong to company", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ThreadID) != "" {
			thread, ok := requireThreadAccess(w, ownerUserID, req.ThreadID)
			if !ok {
				return
			}
			if thread.CompanyID != req.CompanyID {
				http.Error(w, "thread does not belong to company", http.StatusBadRequest)
				return
			}
		}
		inputJSON := req.InputJSON
		if strings.TrimSpace(inputJSON) == "" {
			inputJSON = agentOSService.BuildTaskPromptInput(req.Prompt)
		}
		created, err := agentOSService.CreateTask(agentos.AgentTask{
			CompanyID:    req.CompanyID,
			DepartmentID: req.DepartmentID,
			AgentID:      req.AgentID,
			RequestedBy:  req.RequestedBy,
			ThreadID:     req.ThreadID,
			Type:         req.Type,
			Status:       "queued",
			Priority:     req.Priority,
			InputJSON:    inputJSON,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, created)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSTaskByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.Error(w, "task id is required", http.StatusBadRequest)
		return
	}
	parts := strings.Split(path, "/")
	taskID := parts[0]
	if _, ok := requireTaskAccess(w, ownerUserID, taskID); !ok {
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		details, err := agentOSService.GetTaskDetails(taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, details)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	action := parts[1]
	switch action {
	case "cancel":
		if err := agentOSService.CancelTask(taskID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
	case "retry":
		if err := agentOSService.RetryTask(taskID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
	case "delegate":
		var req struct {
			ToAgentID   string `json:"to_agent_id"`
			Instruction string `json:"instruction"`
			RequestedBy string `json:"requested_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ToAgentID) == "" {
			http.Error(w, "to_agent_id is required", http.StatusBadRequest)
			return
		}
		if _, ok := requireAgentAccess(w, ownerUserID, req.ToAgentID); !ok {
			return
		}
		child, err := agentOSService.DelegateTask(taskID, req.ToAgentID, req.Instruction, req.RequestedBy)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, child)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func AgentOSConsensusRoundsHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := agentOSService.ListConsensusRounds(r.URL.Query().Get("company_id"), r.URL.Query().Get("department_id"), limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			CompanyID    string `json:"company_id"`
			DepartmentID string `json:"department_id"`
			Topic        string `json:"topic"`
			CreatedBy    string `json:"created_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.CreateConsensusRound(req.CompanyID, req.DepartmentID, req.Topic, req.CreatedBy)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSConsensusRoundByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/consensus/rounds/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		http.Error(w, "round id is required", http.StatusBadRequest)
		return
	}
	roundID := parts[0]

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		decision, err := agentOSService.ConsensusDecision(roundID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, decision)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch parts[1] {
	case "vote":
		var req struct {
			AgentID    string  `json:"agent_id"`
			Option     string  `json:"option"`
			Confidence float64 `json:"confidence"`
			Rationale  string  `json:"rationale"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		vote, err := agentOSService.VoteConsensus(roundID, req.AgentID, req.Option, req.Confidence, req.Rationale)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, vote)
	case "close":
		round, err := agentOSService.CloseConsensusRound(roundID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, round)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func AgentOSMemoryQueryHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
	if !requireCompanyAccess(w, ownerUserID, companyID) {
		return
	}
	if departmentID := strings.TrimSpace(r.URL.Query().Get("department_id")); departmentID != "" {
		if _, ok := requireDepartmentAccess(w, ownerUserID, departmentID); !ok {
			return
		}
	}
	if agentID := strings.TrimSpace(r.URL.Query().Get("agent_id")); agentID != "" {
		if _, ok := requireAgentAccess(w, ownerUserID, agentID); !ok {
			return
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := agentOSService.QueryMemory(
		companyID,
		r.URL.Query().Get("department_id"),
		r.URL.Query().Get("agent_id"),
		r.URL.Query().Get("query"),
		limit,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func AgentOSMemoryWriteHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var entry agentos.MemoryEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if entry.ScopeType == "company" && !requireCompanyAccess(w, ownerUserID, entry.ScopeID) {
		return
	}
	if entry.ScopeType == "department" {
		if _, ok := requireDepartmentAccess(w, ownerUserID, entry.ScopeID); !ok {
			return
		}
	}
	if strings.HasPrefix(entry.ScopeType, "agent_") {
		if _, ok := requireAgentAccess(w, ownerUserID, entry.ScopeID); !ok {
			return
		}
	}
	if err := agentOSService.WriteMemoryEntry(entry); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func AgentOSMemoryTimelineHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
	if !requireCompanyAccess(w, ownerUserID, companyID) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := agentOSService.ListMemoryTimeline(companyID, r.URL.Query().Get("department_id"), r.URL.Query().Get("agent_id"), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func AgentOSSchedulesHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
		if !requireCompanyAccess(w, ownerUserID, companyID) {
			return
		}
		items, err := agentOSService.ListSchedules(companyID, r.URL.Query().Get("department_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req agentos.Schedule
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
			return
		}
		agent, ok := requireAgentAccess(w, ownerUserID, req.TargetAgentID)
		if !ok {
			return
		}
		if agent.CompanyID != req.CompanyID {
			http.Error(w, "agent does not belong to company", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.CreateSchedule(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSScheduleByIDHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/schedules/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		http.Error(w, "schedule id is required", http.StatusBadRequest)
		return
	}
	id := parts[0]
	if _, ok := requireScheduleAccess(w, ownerUserID, id); !ok {
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req agentos.Schedule
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		item, err := agentOSService.UpdateSchedule(id, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}

	if len(parts) == 2 && parts[1] == "toggle" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := agentOSService.ToggleSchedule(id, req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "invalid path", http.StatusBadRequest)
}

func AgentOSEventsHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
	if !requireCompanyAccess(w, ownerUserID, companyID) {
		return
	}
	sinceID, _ := strconv.ParseInt(r.URL.Query().Get("since_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := agentOSService.ListEvents(companyID, r.URL.Query().Get("task_id"), sinceID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func AgentOSEventsStreamHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unsupported", http.StatusInternalServerError)
		return
	}

	companyID := r.URL.Query().Get("company_id")
	if !requireCompanyAccess(w, ownerUserID, companyID) {
		return
	}
	ch, cancel := agentOSService.Subscribe(companyID)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeSSE := func(event string, data interface{}) {
		b, _ := json.Marshal(data)
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}

	writeSSE("ready", map[string]interface{}{"company_id": companyID, "ts": time.Now().UTC().Format(time.RFC3339)})
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			writeSSE("event", ev)
		case <-keepAlive.C:
			writeSSE("ping", map[string]interface{}{"ts": time.Now().UTC().Format(time.RFC3339)})
		}
	}
}

func AgentOSPoliciesHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
		if !requireCompanyAccess(w, ownerUserID, companyID) {
			return
		}
		items, err := agentOSService.ListPolicies(companyID, r.URL.Query().Get("department_id"), r.URL.Query().Get("agent_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req agentos.PolicyRule
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
			return
		}
		item, err := agentOSService.UpsertPolicy(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func AgentOSPolicyTestHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CompanyID    string `json:"company_id"`
		DepartmentID string `json:"department_id"`
		AgentID      string `json:"agent_id"`
		Action       string `json:"action"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
		return
	}
	writeJSON(w, http.StatusOK, agentOSService.TestPolicy(req.CompanyID, req.DepartmentID, req.AgentID, req.Action, req.Scope))
}

func AgentOSApprovalsHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
	if !requireCompanyAccess(w, ownerUserID, companyID) {
		return
	}
	items, err := agentOSService.ListApprovals(companyID, r.URL.Query().Get("status"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func AgentOSApprovalsResolveHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ApprovalID string `json:"approval_id"`
		Decision   string `json:"decision"`
		Actor      string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ApprovalID) == "" {
		http.Error(w, "approval_id is required", http.StatusBadRequest)
		return
	}
	approval, err := agentOSService.GetApproval(req.ApprovalID)
	if err != nil || !agentOSService.UserOwnsCompany(ownerUserID, approval.CompanyID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	approve := strings.EqualFold(strings.TrimSpace(req.Decision), "approve")
	if err := agentOSService.ResolveApproval(req.ApprovalID, approve, req.Actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func AgentOSAuditHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sinceID, _ := strconv.ParseInt(r.URL.Query().Get("since_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := agentOSService.ListAudit(sinceID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func AgentOSAuditVerifyHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, agentOSService.AuditVerify())
}

func AgentOSHealthHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, agentOSService.HealthStatus())
}

func AgentOSTopologyHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	companyID := strings.TrimSpace(r.URL.Query().Get("company_id"))
	if companyID == "" {
		items, _ := agentOSService.ListCompanies(ownerUserID)
		if len(items) > 0 {
			companyID = items[0].ID
		}
	}
	if !requireCompanyAccess(w, ownerUserID, companyID) {
		return
	}
	writeJSON(w, http.StatusOK, agentOSService.Topology(companyID))
}

// AgentInboxHandler handles GET/POST for inter-agent messages.
func AgentInboxHandler(w http.ResponseWriter, r *http.Request) {
	if !ensureAgentOSAvailable(w) {
		return
	}
	ownerUserID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	agentID := r.URL.Query().Get("agent_id")
	companyID := r.URL.Query().Get("company_id")
	if r.Method == http.MethodGet && agentID == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !requireCompanyAccess(w, ownerUserID, companyID) {
			return
		}
		agent, ok := requireAgentAccess(w, ownerUserID, agentID)
		if !ok {
			return
		}
		if agent.CompanyID != companyID {
			http.Error(w, "agent does not belong to company", http.StatusBadRequest)
			return
		}
		msgs, err := agentOSService.GetAgentInbox(companyID, agentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, msgs)
	case http.MethodPost:
		var req struct {
			FromAgentID string `json:"from_agent_id"`
			ToAgentID   string `json:"to_agent_id"`
			Content     string `json:"content"`
			CompanyID   string `json:"company_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.Content == "" || req.FromAgentID == "" || req.ToAgentID == "" {
			http.Error(w, "from_agent_id, to_agent_id, content required", http.StatusBadRequest)
			return
		}
		if !requireCompanyAccess(w, ownerUserID, req.CompanyID) {
			return
		}
		fromAgent, ok := requireAgentAccess(w, ownerUserID, req.FromAgentID)
		if !ok {
			return
		}
		toAgent, ok := requireAgentAccess(w, ownerUserID, req.ToAgentID)
		if !ok {
			return
		}
		if fromAgent.CompanyID != req.CompanyID || toAgent.CompanyID != req.CompanyID {
			http.Error(w, "agents do not belong to company", http.StatusBadRequest)
			return
		}
		if err := agentOSService.PostInterAgentMessage(req.CompanyID, req.FromAgentID, req.ToAgentID, req.Content); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
