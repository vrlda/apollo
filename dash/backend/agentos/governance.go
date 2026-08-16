package agentos

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PolicyDecision struct {
	Allowed      bool   `json:"allowed"`
	MatchedRule  string `json:"matched_rule"`
	ApprovalTier string `json:"approval_tier"`
	Reason       string `json:"reason"`
}

func normalizeTier(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	switch t {
	case "none", "tier1", "tier2", "tier3":
		return t
	default:
		return "none"
	}
}

func maxTier(a, b string) string {
	order := map[string]int{"none": 0, "tier1": 1, "tier2": 2, "tier3": 3}
	na := normalizeTier(a)
	nb := normalizeTier(b)
	if order[na] >= order[nb] {
		return na
	}
	return nb
}

func wildcardMatch(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" || pattern == "*" {
		return true
	}
	if ok, err := filepath.Match(pattern, value); err == nil && ok {
		return true
	}
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(value, pattern)
	}
	needle := strings.Trim(pattern, "*")
	if needle == "" {
		return true
	}
	return strings.Contains(value, needle)
}

func (s *Service) UpsertPolicy(rule PolicyRule) (PolicyRule, error) {
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = uuid.NewString()
	}
	if strings.TrimSpace(rule.Action) == "" {
		return PolicyRule{}, fmt.Errorf("action is required")
	}
	rule.Effect = strings.ToLower(strings.TrimSpace(rule.Effect))
	if rule.Effect == "" {
		rule.Effect = "allow"
	}
	if rule.Effect != "allow" && rule.Effect != "deny" {
		return PolicyRule{}, fmt.Errorf("effect must be allow or deny")
	}
	if strings.TrimSpace(rule.ScopePattern) == "" {
		rule.ScopePattern = "*"
	}
	rule.ApprovalTier = normalizeTier(rule.ApprovalTier)

	_, err := s.db.Exec(`
		INSERT INTO capability_policies (id, company_id, department_id, agent_id, action, effect, scope_pattern, approval_tier, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			company_id=excluded.company_id,
			department_id=excluded.department_id,
			agent_id=excluded.agent_id,
			action=excluded.action,
			effect=excluded.effect,
			scope_pattern=excluded.scope_pattern,
			approval_tier=excluded.approval_tier,
			updated_at=CURRENT_TIMESTAMP
	`, rule.ID, rule.CompanyID, rule.DepartmentID, rule.AgentID, rule.Action, rule.Effect, rule.ScopePattern, rule.ApprovalTier)
	if err != nil {
		return PolicyRule{}, err
	}
	s.appendAudit("policy_upsert", "policy", rule.ID, "owner", rule)
	return s.GetPolicy(rule.ID)
}

func (s *Service) GetPolicy(id string) (PolicyRule, error) {
	var p PolicyRule
	var createdAt, updatedAt string
	err := s.db.QueryRow(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(action,''), IFNULL(effect,''), IFNULL(scope_pattern,'*'), IFNULL(approval_tier,'none'), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM capability_policies WHERE id = ?
	`, id).Scan(&p.ID, &p.CompanyID, &p.DepartmentID, &p.AgentID, &p.Action, &p.Effect, &p.ScopePattern, &p.ApprovalTier, &createdAt, &updatedAt)
	if err != nil {
		return PolicyRule{}, err
	}
	p.CreatedAt = parseDBTime(createdAt)
	p.UpdatedAt = parseDBTime(updatedAt)
	return p, nil
}

func (s *Service) ListPolicies(companyID, departmentID, agentID string) ([]PolicyRule, error) {
	query := `
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(action,''), IFNULL(effect,''), IFNULL(scope_pattern,'*'), IFNULL(approval_tier,'none'), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM capability_policies WHERE 1=1`
	args := []interface{}{}
	if strings.TrimSpace(companyID) != "" {
		query += " AND company_id = ?"
		args = append(args, companyID)
	}
	if strings.TrimSpace(departmentID) != "" {
		query += " AND department_id = ?"
		args = append(args, departmentID)
	}
	if strings.TrimSpace(agentID) != "" {
		query += " AND agent_id = ?"
		args = append(args, agentID)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PolicyRule{}
	for rows.Next() {
		var p PolicyRule
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.CompanyID, &p.DepartmentID, &p.AgentID, &p.Action, &p.Effect, &p.ScopePattern, &p.ApprovalTier, &createdAt, &updatedAt); err != nil {
			continue
		}
		p.CreatedAt = parseDBTime(createdAt)
		p.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, p)
	}
	return out, nil
}

func (s *Service) EvaluatePolicy(companyID, departmentID, agentID, action, scope string) PolicyDecision {
	action = strings.ToLower(strings.TrimSpace(action))
	rules, err := s.ListPolicies(companyID, "", "")
	if err != nil {
		return PolicyDecision{Allowed: false, ApprovalTier: "tier3", Reason: "policy query failed"}
	}

	matched := []PolicyRule{}
	for _, r := range rules {
		if strings.TrimSpace(r.CompanyID) != "" && r.CompanyID != companyID {
			continue
		}
		if strings.TrimSpace(r.DepartmentID) != "" && r.DepartmentID != departmentID {
			continue
		}
		if strings.TrimSpace(r.AgentID) != "" && r.AgentID != agentID {
			continue
		}
		ra := strings.ToLower(strings.TrimSpace(r.Action))
		if ra != "*" && ra != action {
			continue
		}
		if !wildcardMatch(r.ScopePattern, scope) {
			continue
		}
		matched = append(matched, r)
	}

	for _, r := range matched {
		if strings.EqualFold(r.Effect, "deny") {
			return PolicyDecision{Allowed: false, MatchedRule: r.ID, ApprovalTier: maxTier(r.ApprovalTier, "tier3"), Reason: "explicit deny"}
		}
	}
	for _, r := range matched {
		if strings.EqualFold(r.Effect, "allow") {
			return PolicyDecision{Allowed: true, MatchedRule: r.ID, ApprovalTier: normalizeTier(r.ApprovalTier), Reason: "explicit allow"}
		}
	}

	enforcement := strings.ToLower(strings.TrimSpace(s.getSettingString("agentos_policy_enforcement", "deny_default")))
	if enforcement == "allow_default" {
		return PolicyDecision{Allowed: true, ApprovalTier: "none", Reason: "allow-default"}
	}
	return PolicyDecision{Allowed: false, ApprovalTier: "tier2", Reason: "deny-by-default"}
}

func (s *Service) TestPolicy(companyID, departmentID, agentID, action, scope string) map[string]interface{} {
	dec := s.EvaluatePolicy(companyID, departmentID, agentID, action, scope)
	return map[string]interface{}{
		"input": map[string]string{
			"company_id":    companyID,
			"department_id": departmentID,
			"agent_id":      agentID,
			"action":        action,
			"scope":         scope,
		},
		"decision": dec,
	}
}

func (s *Service) RequestApproval(companyID, taskID, action, tier, reason string, payload interface{}) error {
	if strings.TrimSpace(tier) == "" {
		tier = "tier2"
	}
	payloadJSON := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = string(b)
		}
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`
		INSERT INTO approval_requests (id, company_id, task_id, action, tier, reason, payload_json, status, requested_at, resolved_at, resolved_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', CURRENT_TIMESTAMP, NULL, '')
	`, id, companyID, taskID, action, normalizeTier(tier), reason, payloadJSON)
	if err != nil {
		return err
	}
	s.appendAudit("approval_requested", "approval", id, "system", map[string]interface{}{"task_id": taskID, "action": action, "tier": tier})
	var departmentID, agentID, threadID string
	_ = s.db.QueryRow("SELECT IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(thread_id,'') FROM agent_tasks WHERE id = ?", taskID).Scan(&departmentID, &agentID, &threadID)
	s.emitEvent(companyID, departmentID, agentID, threadID, taskID, "approval_requested", "warn", map[string]interface{}{"approval_id": id, "tier": tier, "reason": reason})
	return nil
}

func (s *Service) ListApprovals(companyID, status string) ([]ApprovalRequest, error) {
	query := `SELECT id, IFNULL(company_id,''), IFNULL(task_id,''), IFNULL(action,''), IFNULL(tier,''), IFNULL(reason,''), IFNULL(payload_json,'{}'), IFNULL(status,''), IFNULL(requested_at,''), IFNULL(resolved_at,''), IFNULL(resolved_by,'') FROM approval_requests WHERE 1=1`
	args := []interface{}{}
	if strings.TrimSpace(companyID) != "" {
		query += " AND company_id = ?"
		args = append(args, companyID)
	}
	if strings.TrimSpace(status) != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY requested_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ApprovalRequest{}
	for rows.Next() {
		var a ApprovalRequest
		if err := rows.Scan(&a.ID, &a.CompanyID, &a.TaskID, &a.Action, &a.Tier, &a.Reason, &a.PayloadJSON, &a.Status, &a.RequestedAt, &a.ResolvedAt, &a.ResolvedBy); err != nil {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Service) GetApproval(approvalID string) (ApprovalRequest, error) {
	var a ApprovalRequest
	err := s.db.QueryRow(`SELECT id, IFNULL(company_id,''), IFNULL(task_id,''), IFNULL(action,''), IFNULL(tier,''), IFNULL(reason,''), IFNULL(payload_json,'{}'), IFNULL(status,''), IFNULL(requested_at,''), IFNULL(resolved_at,''), IFNULL(resolved_by,'') FROM approval_requests WHERE id = ?`, approvalID).
		Scan(&a.ID, &a.CompanyID, &a.TaskID, &a.Action, &a.Tier, &a.Reason, &a.PayloadJSON, &a.Status, &a.RequestedAt, &a.ResolvedAt, &a.ResolvedBy)
	if err != nil {
		return ApprovalRequest{}, err
	}
	return a, nil
}

func (s *Service) ResolveApproval(approvalID string, approve bool, actor string) error {
	if strings.TrimSpace(approvalID) == "" {
		return fmt.Errorf("approval_id is required")
	}
	status := "rejected"
	if approve {
		status = "approved"
	}
	if strings.TrimSpace(actor) == "" {
		actor = "owner"
	}

	var companyID, taskID, action string
	err := s.db.QueryRow(`SELECT IFNULL(company_id,''), IFNULL(task_id,''), IFNULL(action,'') FROM approval_requests WHERE id = ?`, approvalID).Scan(&companyID, &taskID, &action)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("approval not found")
		}
		return err
	}

	_, err = s.db.Exec("UPDATE approval_requests SET status = ?, resolved_at = CURRENT_TIMESTAMP, resolved_by = ? WHERE id = ?", status, actor, approvalID)
	if err != nil {
		return err
	}

	if status == "approved" && strings.TrimSpace(taskID) != "" {
		_, _ = s.db.Exec("UPDATE agent_tasks SET status='queued', blocked_reason='', updated_at=CURRENT_TIMESTAMP WHERE id = ? AND status='blocked'", taskID)
	}

	s.appendAudit("approval_resolved", "approval", approvalID, actor, map[string]interface{}{"status": status, "task_id": taskID, "action": action})
	var departmentID, agentID, threadID string
	_ = s.db.QueryRow("SELECT IFNULL(department_id,''), IFNULL(agent_id,''), IFNULL(thread_id,'') FROM agent_tasks WHERE id = ?", taskID).Scan(&departmentID, &agentID, &threadID)
	s.emitEvent(companyID, departmentID, agentID, threadID, taskID, "approval_resolved", "info", map[string]interface{}{"approval_id": approvalID, "status": status})
	return nil
}

func canonicalJSON(raw []byte) string {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func hashAudit(eventType, entityType, entityID, actor, prevHash, payloadJSON, ts string) string {
	raw := strings.Join([]string{eventType, entityType, entityID, actor, prevHash, payloadJSON, ts}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *Service) appendAudit(eventType, entityType, entityID, actor string, payload interface{}) {
	payloadJSON := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadJSON = canonicalJSON(b)
		}
	}
	prevHash := ""
	_ = s.db.QueryRow("SELECT IFNULL(event_hash,'') FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&prevHash)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	eventHash := hashAudit(eventType, entityType, entityID, actor, prevHash, payloadJSON, now)
	sig, pubID := s.signAudit(eventHash)
	_, _ = s.db.Exec(`
		INSERT INTO audit_log (event_type, entity_type, entity_id, actor_type, actor_id, payload_json, prev_hash, event_hash, signature, pubkey_id, created_at)
		VALUES (?, ?, ?, 'agent', ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, eventType, entityType, entityID, actor, payloadJSON, prevHash, eventHash, sig, pubID)
}

func (s *Service) signAudit(eventHash string) (string, string) {
	priv, _, pubID := s.loadOrInitAuditKeypair()
	if len(priv) == 0 {
		return "", pubID
	}
	sig := ed25519.Sign(priv, []byte(eventHash))
	return base64.StdEncoding.EncodeToString(sig), pubID
}

func (s *Service) loadOrInitAuditKeypair() (ed25519.PrivateKey, ed25519.PublicKey, string) {
	keyPath := strings.TrimSpace(s.getSettingString("agentos_audit_signing_key_path", "./agentos_ed25519.key"))
	pubID := strings.TrimSpace(s.getSettingString("agentos_audit_pubkey_id", "local-ed25519"))

	if raw, err := os.ReadFile(keyPath); err == nil {
		decoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if derr == nil && len(decoded) == ed25519.PrivateKeySize {
			priv := ed25519.PrivateKey(decoded)
			pub := priv.Public().(ed25519.PublicKey)
			return priv, pub, pubID
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, pubID
	}
	_ = os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(priv)), 0600)
	return priv, pub, pubID
}

func (s *Service) AuditVerify() map[string]interface{} {
	rows, err := s.db.Query(`
		SELECT id, IFNULL(event_hash,''), IFNULL(prev_hash,''), IFNULL(signature,''), IFNULL(created_at,'')
		FROM audit_log ORDER BY id ASC
	`)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	defer rows.Close()

	_, pub, _ := s.loadOrInitAuditKeypair()
	if len(pub) == 0 {
		return map[string]interface{}{"ok": false, "error": "audit public key unavailable"}
	}

	lastHash := ""
	count := 0
	issues := []string{}
	for rows.Next() {
		count++
		var id int64
		var eventHash, prevHash, sig, createdAt string
		if err := rows.Scan(&id, &eventHash, &prevHash, &sig, &createdAt); err != nil {
			issues = append(issues, fmt.Sprintf("row %d scan failed", id))
			continue
		}
		if prevHash != lastHash {
			issues = append(issues, fmt.Sprintf("row %d chain mismatch", id))
		}
		sigBytes, err := base64.StdEncoding.DecodeString(sig)
		if err != nil || !ed25519.Verify(pub, []byte(eventHash), sigBytes) {
			issues = append(issues, fmt.Sprintf("row %d bad signature", id))
		}
		lastHash = eventHash
	}

	return map[string]interface{}{
		"ok":          len(issues) == 0,
		"entries":     count,
		"issues":      issues,
		"last_hash":   lastHash,
		"verified_at": time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *Service) ListAudit(sinceID int64, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.Query(`
		SELECT id, IFNULL(event_type,''), IFNULL(entity_type,''), IFNULL(entity_id,''), IFNULL(actor_type,''), IFNULL(actor_id,''), IFNULL(payload_json,'{}'), IFNULL(prev_hash,''), IFNULL(event_hash,''), IFNULL(signature,''), IFNULL(pubkey_id,''), IFNULL(created_at,'')
		FROM audit_log WHERE id > ? ORDER BY id ASC LIMIT ?
	`, sinceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var eventType, entityType, entityID, actorType, actorID, payloadJSON, prevHash, eventHash, signature, pubkeyID, createdAt string
		if err := rows.Scan(&id, &eventType, &entityType, &entityID, &actorType, &actorID, &payloadJSON, &prevHash, &eventHash, &signature, &pubkeyID, &createdAt); err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":           id,
			"event_type":   eventType,
			"entity_type":  entityType,
			"entity_id":    entityID,
			"actor_type":   actorType,
			"actor_id":     actorID,
			"payload_json": payloadJSON,
			"prev_hash":    prevHash,
			"event_hash":   eventHash,
			"signature":    signature,
			"pubkey_id":    pubkeyID,
			"created_at":   createdAt,
		})
	}
	return out, nil
}
