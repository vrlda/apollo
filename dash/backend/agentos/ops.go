package agentos

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

func (s *Service) Topology(companyID string) map[string]interface{} {
	departments, _ := s.ListDepartments(companyID)
	agents, _ := s.ListAgents(companyID, "")
	tasks, _ := s.ListTasks(companyID, "", "", 400)

	agentsByDepartment := map[string][]Agent{}
	for _, ag := range agents {
		agentsByDepartment[ag.DepartmentID] = append(agentsByDepartment[ag.DepartmentID], ag)
	}
	for depID := range agentsByDepartment {
		sort.Slice(agentsByDepartment[depID], func(i, j int) bool {
			return agentsByDepartment[depID][i].CreatedAt.Before(agentsByDepartment[depID][j].CreatedAt)
		})
	}

	taskCounts := map[string]int{}
	for _, t := range tasks {
		taskCounts[t.Status]++
	}

	return map[string]interface{}{
		"departments": departments,
		"agents": map[string]interface{}{
			"items":         agents,
			"by_department": agentsByDepartment,
		},
		"tasks": map[string]interface{}{
			"items":         tasks,
			"status_counts": taskCounts,
		},
	}
}

func (s *Service) GetTaskDetails(taskID string) (map[string]interface{}, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	runs, _ := s.ListRuns(taskID)
	children, _ := s.db.Query(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(requested_by,''), IFNULL(parent_task_id,''), IFNULL(thread_id,''), IFNULL(type,''), IFNULL(status,''), IFNULL(priority,50), IFNULL(input_json,''), IFNULL(result_json,''), IFNULL(blocked_reason,''), IFNULL(created_at,''), IFNULL(updated_at,''), IFNULL(completed_at,'')
		FROM agent_tasks WHERE parent_task_id = ? ORDER BY created_at ASC
	`, taskID)
	childTasks := []AgentTask{}
	if children != nil {
		defer children.Close()
		for children.Next() {
			t, err := scanTask(children)
			if err == nil {
				childTasks = append(childTasks, t)
			}
		}
	}

	dRows, _ := s.db.Query(`SELECT id, IFNULL(parent_task_id,''), IFNULL(from_agent_id,''), IFNULL(to_agent_id,''), IFNULL(instruction,''), IFNULL(status,''), IFNULL(created_at,''), IFNULL(updated_at,'') FROM agent_delegations WHERE parent_task_id = ? ORDER BY created_at ASC`, taskID)
	delegations := []AgentDelegation{}
	if dRows != nil {
		defer dRows.Close()
		for dRows.Next() {
			var d AgentDelegation
			var createdAt, updatedAt string
			if err := dRows.Scan(&d.ID, &d.ParentTaskID, &d.FromAgentID, &d.ToAgentID, &d.Instruction, &d.Status, &createdAt, &updatedAt); err == nil {
				d.CreatedAt = parseDBTime(createdAt)
				d.UpdatedAt = parseDBTime(updatedAt)
				delegations = append(delegations, d)
			}
		}
	}

	events, _ := s.ListEvents(task.CompanyID, taskID, 0, 200)
	return map[string]interface{}{
		"task":        task,
		"runs":        runs,
		"children":    childTasks,
		"delegations": delegations,
		"events":      events,
	}, nil
}

func (s *Service) DelegateTask(parentTaskID, toAgentID, instruction, requestedBy string) (AgentTask, error) {
	parent, err := s.GetTask(parentTaskID)
	if err != nil {
		return AgentTask{}, err
	}
	if strings.TrimSpace(toAgentID) == "" {
		return AgentTask{}, fmt.Errorf("to_agent_id is required")
	}
	target, err := s.GetAgent(toAgentID)
	if err != nil {
		return AgentTask{}, err
	}
	if target.CompanyID != parent.CompanyID {
		return AgentTask{}, fmt.Errorf("target agent belongs to another company")
	}
	if target.DepartmentID != parent.DepartmentID {
		allowed := s.EvaluatePolicy(parent.CompanyID, parent.DepartmentID, parent.AgentID, "delegate_cross_department", fmt.Sprintf("to_agent:%s", target.ID))
		if !allowed.Allowed {
			return AgentTask{}, fmt.Errorf("cross-department delegation denied")
		}
	}
	if strings.TrimSpace(instruction) == "" {
		instruction = parent.InputJSON
		if strings.TrimSpace(instruction) == "" {
			instruction = `{"prompt":"Delegated task"}`
		}
	}
	if strings.TrimSpace(requestedBy) == "" {
		requestedBy = parent.AgentID
	}

	child := AgentTask{
		ID:           uuid.NewString(),
		CompanyID:    parent.CompanyID,
		DepartmentID: target.DepartmentID,
		AgentID:      target.ID,
		RequestedBy:  requestedBy,
		ParentTaskID: parent.ID,
		ThreadID:     parent.ThreadID,
		Type:         "delegated",
		Status:       "queued",
		Priority:     parent.Priority + 5,
		InputJSON:    instruction,
	}
	created, err := s.CreateTask(child)
	if err != nil {
		return AgentTask{}, err
	}
	_, _ = s.db.Exec(`INSERT INTO agent_delegations (id, parent_task_id, from_agent_id, to_agent_id, instruction, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'queued', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, uuid.NewString(), parent.ID, parent.AgentID, target.ID, instruction)
	s.appendAudit("task_delegated", "task", parent.ID, requestedBy, map[string]interface{}{"to_agent_id": target.ID, "child_task_id": created.ID})
	s.emitEvent(parent.CompanyID, parent.DepartmentID, parent.AgentID, parent.ThreadID, parent.ID, "task_delegated", "info", map[string]interface{}{"to_agent_id": target.ID, "child_task_id": created.ID})
	return created, nil
}
