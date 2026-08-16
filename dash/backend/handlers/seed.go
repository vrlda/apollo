package handlers

import (
	"log"

	"github.com/danilrybalkin/apollo-dash/db"
	"github.com/google/uuid"
)

// SeedDefaultModelProfiles creates the two starter model profiles if none exist yet.
// Called once at startup. Models can be changed later in Settings.
func SeedDefaultModelProfiles() {
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM agent_model_profiles`).Scan(&count); err != nil {
		return
	}
	if count > 0 {
		return
	}

	profiles := []struct {
		label, provider, model string
	}{
		// Ultra-cheap trial model (free tier on OpenRouter)
		{"Trial Model (Free)", "openrouter", "meta-llama/llama-3.2-1b-instruct:free"},
		// Capable low-cost model for paying users
		{"Standard Model", "openrouter", "openai/gpt-4o-mini"},
	}

	for _, p := range profiles {
		id := uuid.New().String()
		_, err := db.DB.Exec(
			`INSERT INTO agent_model_profiles (id, provider, model, settings_json, fallback_chain_json)
			 VALUES (?, ?, ?, '{}', '[]')`,
			id, p.provider, p.model,
		)
		if err != nil {
			log.Printf("Seed: failed to create model profile %q: %v", p.label, err)
		}
	}
	log.Println("Seed: created 2 default model profiles")
}

// SeedDemoCompany creates a sample company, department, and agent for a new user
// so they understand the org hierarchy immediately on first login.
func SeedDemoCompany(userID string) {
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM companies WHERE owner_user_id = ?`, userID).Scan(&count); err != nil {
		return
	}
	if count > 0 {
		return // user already has a company
	}

	companyID := uuid.New().String()
	deptID := uuid.New().String()
	agentID := uuid.New().String()

	_, err := db.DB.Exec(
		`INSERT INTO companies (id, owner_user_id, name, slug, description, status)
		 VALUES (?, ?, 'My First Company', ?, 'Your first AI company. Edit or replace this example.', 'active')`,
		companyID, userID, "company-"+companyID[:8],
	)
	if err != nil {
		log.Printf("Seed: failed to create demo company: %v", err)
		return
	}

	_, err = db.DB.Exec(
		`INSERT INTO departments (id, company_id, name, type, description, status)
		 VALUES (?, ?, 'Product', 'general', 'Product and engineering work', 'active')`,
		deptID, companyID,
	)
	if err != nil {
		log.Printf("Seed: failed to create demo department: %v", err)
		return
	}

	identityPrompt := `You are Alex, an AI product worker at My First Company.
You help with product research, writing specifications, drafting content, and planning tasks.
Be concise, practical, and always ask for clarification when a task is ambiguous.
You can be given tasks via the chat interface or scheduled to run automatically.`

	_, err = db.DB.Exec(
		`INSERT INTO agents (id, company_id, department_id, name, role_type, identity_prompt, status, is_active)
		 VALUES (?, ?, ?, 'Alex', 'worker', ?, 'idle', 1)`,
		agentID, companyID, deptID, identityPrompt,
	)
	if err != nil {
		log.Printf("Seed: failed to create demo agent: %v", err)
		return
	}

	log.Printf("Seed: created demo company/dept/agent for user %s", userID)
}
