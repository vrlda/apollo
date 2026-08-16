package agentos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type memoryQueryHit struct {
	Entry MemoryEntry
	Score float64
}

func (s *Service) WriteMemoryEntry(entry MemoryEntry) error {
	entry.ScopeType = strings.TrimSpace(strings.ToLower(entry.ScopeType))
	entry.ScopeID = strings.TrimSpace(entry.ScopeID)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.SourceType = strings.TrimSpace(entry.SourceType)
	entry.AuthorAgentID = strings.TrimSpace(entry.AuthorAgentID)

	if entry.ScopeType == "" || entry.ScopeID == "" {
		return fmt.Errorf("scope_type and scope_id are required")
	}
	if entry.Content == "" {
		return nil
	}
	if len(entry.Content) < 18 {
		return nil
	}
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.SourceType == "" {
		entry.SourceType = "agent_note"
	}
	if entry.TagsJSON == "" {
		entry.TagsJSON = "{}"
	}
	if entry.Importance <= 0 {
		entry.Importance = 0.5
	}

	allowedScopes := map[string]bool{
		"global_user":      true,
		"company":          true,
		"department":       true,
		"agent_short_term": true,
		"agent_long_term":  true,
	}
	if !allowedScopes[entry.ScopeType] {
		return fmt.Errorf("unsupported scope_type %q", entry.ScopeType)
	}

	// Duplicate suppression within same scope.
	var existing string
	_ = s.db.QueryRow(`
		SELECT id FROM memory_entries
		WHERE scope_type = ? AND scope_id = ? AND content = ?
		ORDER BY created_at DESC LIMIT 1
	`, entry.ScopeType, entry.ScopeID, entry.Content).Scan(&existing)
	if existing != "" {
		return nil
	}

	if entry.TTLAt != "" {
		if _, err := time.Parse(time.RFC3339, entry.TTLAt); err != nil {
			entry.TTLAt = ""
		}
	}

	emb, err := s.generateEmbedding(entry.Content)
	if err == nil && len(emb) > 0 {
		b, _ := json.Marshal(emb)
		entry.Embedding = string(b)
	} else {
		entry.Embedding = ""
	}

	_, err = s.db.Exec(`
		INSERT INTO memory_entries (id, scope_type, scope_id, source_type, author_agent_id, content, embedding, tags_json, importance, ttl_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, entry.ID, entry.ScopeType, entry.ScopeID, entry.SourceType, entry.AuthorAgentID, entry.Content, entry.Embedding, entry.TagsJSON, entry.Importance, entry.TTLAt)
	if err != nil {
		return err
	}

	companyID, departmentID := s.scopeContext(entry.ScopeType, entry.ScopeID)
	s.appendAudit("memory_write", "memory_entry", entry.ID, entry.AuthorAgentID, map[string]interface{}{
		"scope_type":  entry.ScopeType,
		"scope_id":    entry.ScopeID,
		"source_type": entry.SourceType,
	})
	s.emitEvent(companyID, departmentID, entry.AuthorAgentID, "", "", "memory_written", "info", map[string]interface{}{
		"memory_id":   entry.ID,
		"scope_type":  entry.ScopeType,
		"scope_id":    entry.ScopeID,
		"importance":  entry.Importance,
		"source_type": entry.SourceType,
	})

	return nil
}

func (s *Service) scopeContext(scopeType, scopeID string) (companyID string, departmentID string) {
	switch scopeType {
	case "company":
		return scopeID, ""
	case "department":
		_ = s.db.QueryRow("SELECT IFNULL(company_id,'') FROM departments WHERE id = ?", scopeID).Scan(&companyID)
		return companyID, scopeID
	case "agent_short_term", "agent_long_term":
		_ = s.db.QueryRow("SELECT IFNULL(company_id,''), IFNULL(department_id,'') FROM agents WHERE id = ?", scopeID).Scan(&companyID, &departmentID)
		return companyID, departmentID
	default:
		return "", ""
	}
}

func (s *Service) ListMemoryTimeline(companyID, departmentID, agentID string, limit int) ([]MemoryEntry, error) {
	if limit <= 0 || limit > 400 {
		limit = 120
	}
	query := `
		SELECT id, IFNULL(scope_type,''), IFNULL(scope_id,''), IFNULL(source_type,''), IFNULL(author_agent_id,''), IFNULL(content,''), IFNULL(embedding,''), IFNULL(tags_json,'{}'), IFNULL(importance,0.5), IFNULL(ttl_at,''), IFNULL(created_at,''), IFNULL(updated_at,'')
		FROM memory_entries WHERE 1=1`
	args := []interface{}{}

	if strings.TrimSpace(agentID) != "" {
		query += " AND ((scope_type IN ('agent_short_term','agent_long_term') AND scope_id = ?) OR author_agent_id = ?)"
		args = append(args, agentID, agentID)
	} else if strings.TrimSpace(departmentID) != "" {
		query += " AND ( (scope_type = 'department' AND scope_id = ?) OR (scope_type IN ('agent_short_term','agent_long_term') AND scope_id IN (SELECT id FROM agents WHERE department_id = ?)) )"
		args = append(args, departmentID, departmentID)
	} else if strings.TrimSpace(companyID) != "" {
		query += " AND ( (scope_type = 'company' AND scope_id = ?) OR (scope_type = 'department' AND scope_id IN (SELECT id FROM departments WHERE company_id = ?)) OR (scope_type IN ('agent_short_term','agent_long_term') AND scope_id IN (SELECT id FROM agents WHERE company_id = ?)) )"
		args = append(args, companyID, companyID, companyID)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MemoryEntry{}
	for rows.Next() {
		var e MemoryEntry
		var ttlAt, createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.ScopeType, &e.ScopeID, &e.SourceType, &e.AuthorAgentID, &e.Content, &e.Embedding, &e.TagsJSON, &e.Importance, &ttlAt, &createdAt, &updatedAt); err != nil {
			continue
		}
		e.TTLAt = strings.TrimSpace(ttlAt)
		e.CreatedAt = parseDBTime(createdAt)
		e.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, e)
	}
	return out, nil
}

func (s *Service) QueryMemory(companyID, departmentID, agentID, query string, limit int) ([]MemoryEntry, error) {
	if limit <= 0 || limit > 120 {
		limit = 24
	}
	entries, err := s.ListMemoryTimeline(companyID, departmentID, agentID, 500)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return []MemoryEntry{}, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		if len(entries) > limit {
			entries = entries[:limit]
		}
		return entries, nil
	}

	queryEmb, embErr := s.generateEmbedding(query)
	qLower := strings.ToLower(query)

	hits := []memoryQueryHit{}
	for _, e := range entries {
		score := 0.0
		contentLower := strings.ToLower(e.Content)
		if strings.Contains(contentLower, qLower) {
			score += 0.45
		}
		for _, tok := range strings.Fields(qLower) {
			if tok != "" && strings.Contains(contentLower, tok) {
				score += 0.08
			}
		}
		if embErr == nil && len(queryEmb) > 0 && strings.TrimSpace(e.Embedding) != "" {
			var emb []float32
			if err := json.Unmarshal([]byte(e.Embedding), &emb); err == nil {
				score += 0.7 * cosineSimilarity(queryEmb, emb)
			}
		}
		if score > 0 {
			hits = append(hits, memoryQueryHit{Entry: e, Score: score})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Entry.CreatedAt.After(hits[j].Entry.CreatedAt)
		}
		return hits[i].Score > hits[j].Score
	})

	out := make([]MemoryEntry, 0, limit)
	for _, h := range hits {
		out = append(out, h.Entry)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Service) memoryContextForAgent(companyID, departmentID, agentID string) string {
	sections := []struct {
		Title string
		Scope string
		ID    string
		Limit int
	}{
		{Title: "Global User Memory", Scope: "global_user", ID: "owner", Limit: 3},
		{Title: "Company Memory", Scope: "company", ID: companyID, Limit: 4},
		{Title: "Department Memory", Scope: "department", ID: departmentID, Limit: 4},
		{Title: "Agent Long-Term Memory", Scope: "agent_long_term", ID: agentID, Limit: 5},
		{Title: "Agent Short-Term Memory", Scope: "agent_short_term", ID: agentID, Limit: 5},
	}

	var b strings.Builder
	b.WriteString("Scoped memory context for this run. Use it as guidance, not absolute truth.\n\n")

	for _, sec := range sections {
		if strings.TrimSpace(sec.ID) == "" {
			continue
		}
		rows, err := s.db.Query(`
			SELECT IFNULL(content,''), IFNULL(source_type,''), IFNULL(created_at,'')
			FROM memory_entries
			WHERE scope_type = ? AND scope_id = ?
			ORDER BY created_at DESC LIMIT ?
		`, sec.Scope, sec.ID, sec.Limit)
		if err != nil {
			continue
		}

		items := []string{}
		for rows.Next() {
			var content, source, createdAt string
			if err := rows.Scan(&content, &source, &createdAt); err != nil {
				continue
			}
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if len(content) > 280 {
				content = content[:280] + "..."
			}
			items = append(items, fmt.Sprintf("- (%s) %s", source, content))
		}
		rows.Close()
		if len(items) == 0 {
			continue
		}
		b.WriteString(sec.Title)
		b.WriteString(":\n")
		b.WriteString(strings.Join(items, "\n"))
		b.WriteString("\n\n")
	}

	// Add indexed knowledge chunks from contextplus-native indexing where available.
	if strings.TrimSpace(companyID) != "" {
		rows, err := s.db.Query(`
			SELECT IFNULL(k.chunk_text,'')
			FROM knowledge_index_chunks k
			JOIN knowledge_assets a ON a.id = k.asset_id
			WHERE a.company_id = ?
			ORDER BY k.created_at DESC
			LIMIT 6
		`, companyID)
		if err == nil {
			defer rows.Close()
			chunks := []string{}
			for rows.Next() {
				var chunk string
				if err := rows.Scan(&chunk); err != nil {
					continue
				}
				chunk = strings.TrimSpace(chunk)
				if chunk == "" {
					continue
				}
				if len(chunk) > 220 {
					chunk = chunk[:220] + "..."
				}
				chunks = append(chunks, "- "+chunk)
			}
			if len(chunks) > 0 {
				b.WriteString("Company Knowledge Index Hints:\n")
				b.WriteString(strings.Join(chunks, "\n"))
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimSpace(b.String())
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func (s *Service) generateEmbedding(text string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	apiURL := strings.TrimSpace(s.getSettingString("embedding_api_url", "http://localhost:11434"))
	model := strings.TrimSpace(s.getSettingString("embedding_model", "nomic-embed-text"))
	if apiURL == "" || model == "" {
		return nil, fmt.Errorf("embedding provider is not configured")
	}
	apiURL = strings.TrimRight(apiURL, "/")

	isOpenAICompatible := strings.Contains(apiURL, "/v1") || strings.Contains(apiURL, "openrouter.ai")
	endpoint := apiURL + "/api/embeddings"
	payload := map[string]interface{}{"model": model, "prompt": text}
	if isOpenAICompatible {
		endpoint = apiURL + "/embeddings"
		payload = map[string]interface{}{"model": model, "input": text}
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	if isOpenAICompatible {
		apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
		if apiKey == "" || apiKey == "your_openrouter_api_key_here" {
			apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embedding endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var ollamaOut struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &ollamaOut); err == nil && len(ollamaOut.Embedding) > 0 {
		return ollamaOut.Embedding, nil
	}

	var openAIOut struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &openAIOut); err != nil {
		return nil, err
	}
	if len(openAIOut.Data) == 0 || len(openAIOut.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}
	return openAIOut.Data[0].Embedding, nil
}
