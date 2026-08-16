package agentos

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Service) CreateConsensusRound(companyID, departmentID, topic, createdBy string) (ConsensusRound, error) {
	if strings.TrimSpace(companyID) == "" || strings.TrimSpace(topic) == "" {
		return ConsensusRound{}, fmt.Errorf("company_id and topic are required")
	}
	if strings.TrimSpace(createdBy) == "" {
		createdBy = "owner"
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`
		INSERT INTO agent_consensus_rounds (id, company_id, department_id, topic, status, created_by, created_at, closed_at)
		VALUES (?, ?, ?, ?, 'open', ?, CURRENT_TIMESTAMP, '')
	`, id, companyID, departmentID, topic, createdBy)
	if err != nil {
		return ConsensusRound{}, err
	}
	s.appendAudit("consensus_round_created", "consensus_round", id, createdBy, map[string]interface{}{"topic": topic})
	s.emitEvent(companyID, departmentID, "", "", "", "consensus_round_created", "info", map[string]interface{}{"round_id": id, "topic": topic})
	return s.GetConsensusRound(id)
}

func (s *Service) GetConsensusRound(id string) (ConsensusRound, error) {
	var r ConsensusRound
	var createdAt string
	err := s.db.QueryRow(`
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(topic,''), IFNULL(status,'open'), IFNULL(created_by,''), IFNULL(created_at,''), IFNULL(closed_at,'')
		FROM agent_consensus_rounds WHERE id = ?
	`, id).Scan(&r.ID, &r.CompanyID, &r.DepartmentID, &r.Topic, &r.Status, &r.CreatedBy, &createdAt, &r.ClosedAt)
	if err != nil {
		return ConsensusRound{}, err
	}
	r.CreatedAt = parseDBTime(createdAt)
	return r, nil
}

func (s *Service) ListConsensusRounds(companyID, departmentID string, limit int) ([]ConsensusRound, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `
		SELECT id, IFNULL(company_id,''), IFNULL(department_id,''), IFNULL(topic,''), IFNULL(status,'open'), IFNULL(created_by,''), IFNULL(created_at,''), IFNULL(closed_at,'')
		FROM agent_consensus_rounds WHERE 1=1`
	args := []interface{}{}
	if strings.TrimSpace(companyID) != "" {
		query += " AND company_id = ?"
		args = append(args, companyID)
	}
	if strings.TrimSpace(departmentID) != "" {
		query += " AND department_id = ?"
		args = append(args, departmentID)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ConsensusRound{}
	for rows.Next() {
		var r ConsensusRound
		var createdAt string
		if err := rows.Scan(&r.ID, &r.CompanyID, &r.DepartmentID, &r.Topic, &r.Status, &r.CreatedBy, &createdAt, &r.ClosedAt); err != nil {
			continue
		}
		r.CreatedAt = parseDBTime(createdAt)
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) VoteConsensus(roundID, agentID, option string, confidence float64, rationale string) (ConsensusVote, error) {
	if strings.TrimSpace(roundID) == "" || strings.TrimSpace(agentID) == "" || strings.TrimSpace(option) == "" {
		return ConsensusVote{}, fmt.Errorf("round_id, agent_id and option are required")
	}
	round, err := s.GetConsensusRound(roundID)
	if err != nil {
		return ConsensusVote{}, err
	}
	if strings.ToLower(strings.TrimSpace(round.Status)) != "open" {
		return ConsensusVote{}, fmt.Errorf("consensus round is closed")
	}
	if confidence <= 0 {
		confidence = 0.5
	}
	if confidence > 1 {
		confidence = 1
	}

	id := uuid.NewString()
	_, err = s.db.Exec(`
		INSERT INTO agent_consensus_votes (id, round_id, agent_id, option, confidence, rationale, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(round_id, agent_id) DO UPDATE SET
			option=excluded.option,
			confidence=excluded.confidence,
			rationale=excluded.rationale,
			created_at=CURRENT_TIMESTAMP
	`, id, roundID, agentID, option, confidence, rationale)
	if err != nil {
		return ConsensusVote{}, err
	}
	s.appendAudit("consensus_vote_cast", "consensus_round", roundID, agentID, map[string]interface{}{"option": option, "confidence": confidence})
	s.emitEvent(round.CompanyID, round.DepartmentID, agentID, "", "", "consensus_vote_cast", "info", map[string]interface{}{"round_id": roundID, "option": option})

	votes, _ := s.ListConsensusVotes(roundID)
	for _, v := range votes {
		if v.AgentID == agentID {
			return v, nil
		}
	}
	return ConsensusVote{}, fmt.Errorf("vote not found after write")
}

func (s *Service) ListConsensusVotes(roundID string) ([]ConsensusVote, error) {
	rows, err := s.db.Query(`
		SELECT id, IFNULL(round_id,''), IFNULL(agent_id,''), IFNULL(option,''), IFNULL(confidence,0.5), IFNULL(rationale,''), IFNULL(created_at,'')
		FROM agent_consensus_votes
		WHERE round_id = ?
		ORDER BY created_at ASC
	`, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ConsensusVote{}
	for rows.Next() {
		var v ConsensusVote
		var createdAt string
		if err := rows.Scan(&v.ID, &v.RoundID, &v.AgentID, &v.Option, &v.Confidence, &v.Rationale, &createdAt); err != nil {
			continue
		}
		v.CreatedAt = parseDBTime(createdAt)
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) CloseConsensusRound(roundID string) (ConsensusRound, error) {
	round, err := s.GetConsensusRound(roundID)
	if err != nil {
		return ConsensusRound{}, err
	}
	if strings.ToLower(strings.TrimSpace(round.Status)) == "closed" {
		return round, nil
	}
	_, err = s.db.Exec("UPDATE agent_consensus_rounds SET status='closed', closed_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339), roundID)
	if err != nil {
		return ConsensusRound{}, err
	}
	s.appendAudit("consensus_round_closed", "consensus_round", roundID, "owner", nil)
	s.emitEvent(round.CompanyID, round.DepartmentID, "", "", "", "consensus_round_closed", "info", map[string]interface{}{"round_id": roundID})
	return s.GetConsensusRound(roundID)
}

func (s *Service) ConsensusDecision(roundID string) (map[string]interface{}, error) {
	round, err := s.GetConsensusRound(roundID)
	if err != nil {
		return nil, err
	}
	votes, err := s.ListConsensusVotes(roundID)
	if err != nil {
		return nil, err
	}
	byOption := map[string]int{}
	for _, v := range votes {
		opt := strings.TrimSpace(v.Option)
		if opt == "" {
			continue
		}
		byOption[opt]++
	}
	winner := ""
	winnerCount := 0
	for option, count := range byOption {
		if count > winnerCount {
			winner = option
			winnerCount = count
		}
	}
	return map[string]interface{}{
		"round":         round,
		"votes":         votes,
		"counts":        byOption,
		"winner_option": winner,
		"winner_votes":  winnerCount,
	}, nil
}
