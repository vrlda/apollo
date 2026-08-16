package db

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB() {
	// Create database locally in backend dir
	dbPath := filepath.Join(".", "apollo.db")
	var err error

	// Create file if it doesn't exist
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		file, err := os.Create(dbPath)
		if err != nil {
			log.Fatal(err)
		}
		file.Close()
	}

	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Error opening database: %v\n", err)
	}
	// Single connection: serialises all reads+writes at the Go level.
	// Eliminates SQLITE_BUSY entirely without WAL (which causes stale reads on a single conn).
	DB.SetMaxOpenConns(1)

	createTables()
}

func createTables() {
	createVpsHostsTable := `
	CREATE TABLE IF NOT EXISTS vps_hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		ip TEXT NOT NULL UNIQUE
	);`

	createChatSessionsTable := `
	CREATE TABLE IF NOT EXISTS chat_sessions (
		id TEXT PRIMARY KEY, /* UUID */
		title TEXT NOT NULL,
		model TEXT DEFAULT '',
		project_path TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createChatMessagesTable := `
	CREATE TABLE IF NOT EXISTS chat_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL, /* 'user' or 'assistant' */
		content TEXT NOT NULL,
		vector_embedding TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
	);`

	createSettingsTable := `
	CREATE TABLE IF NOT EXISTS settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL UNIQUE,
		value TEXT NOT NULL
	);`

	createPersonalityFactsTable := `
	CREATE TABLE IF NOT EXISTS personality_facts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		fact TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Retry on write contention instead of immediately failing with SQLITE_BUSY
	DB.Exec("PRAGMA busy_timeout = 5000;")

	// Enable foreign keys
	_, err := DB.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		log.Fatal("Could not enable foreign keys: ", err)
	}

	createSubagentsTable := `
	CREATE TABLE IF NOT EXISTS subagents (
		id TEXT PRIMARY KEY,
		session_id TEXT,
		name TEXT NOT NULL,
		task TEXT NOT NULL,
		status TEXT DEFAULT 'running',
		output TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createCompaniesTable := `
	CREATE TABLE IF NOT EXISTS companies (
		id TEXT PRIMARY KEY,
		owner_user_id TEXT DEFAULT '',
		name TEXT NOT NULL,
		slug TEXT NOT NULL,
		description TEXT DEFAULT '',
		status TEXT DEFAULT 'active',
		timezone TEXT DEFAULT 'UTC',
		workspace_path TEXT DEFAULT '',
		deploy_command TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createDepartmentsTable := `
	CREATE TABLE IF NOT EXISTS departments (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT DEFAULT 'general',
		description TEXT DEFAULT '',
		status TEXT DEFAULT 'active',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(company_id) REFERENCES companies(id) ON DELETE CASCADE
	);`

	createAgentsTable := `
	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		department_id TEXT NOT NULL,
		name TEXT NOT NULL,
		role_type TEXT DEFAULT 'worker',
		parent_agent_id TEXT DEFAULT '',
		identity_prompt TEXT DEFAULT '',
		status TEXT DEFAULT 'idle',
		is_active INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(company_id) REFERENCES companies(id) ON DELETE CASCADE,
		FOREIGN KEY(department_id) REFERENCES departments(id) ON DELETE CASCADE
	);`

	createAgentModelProfilesTable := `
	CREATE TABLE IF NOT EXISTS agent_model_profiles (
		id TEXT PRIMARY KEY,
		owner_user_id TEXT DEFAULT '',
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		settings_json TEXT DEFAULT '{}',
		fallback_chain_json TEXT DEFAULT '[]',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createAgentModelBindingsTable := `
	CREATE TABLE IF NOT EXISTS agent_model_bindings (
		agent_id TEXT PRIMARY KEY,
		primary_profile_id TEXT NOT NULL,
		temperature REAL DEFAULT 0.2,
		max_tokens INTEGER DEFAULT 1200,
		reasoning_effort TEXT DEFAULT 'standard',
		FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE,
		FOREIGN KEY(primary_profile_id) REFERENCES agent_model_profiles(id) ON DELETE CASCADE
	);`

	createAgentThreadsTable := `
	CREATE TABLE IF NOT EXISTS agent_threads (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		department_id TEXT DEFAULT '',
		agent_id TEXT NOT NULL,
		title TEXT NOT NULL,
		status TEXT DEFAULT 'active',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(company_id) REFERENCES companies(id) ON DELETE CASCADE,
		FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE
	);`

	createAgentMessagesTable := `
	CREATE TABLE IF NOT EXISTS agent_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		thread_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		content_type TEXT DEFAULT 'text',
		text_embedding TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(thread_id) REFERENCES agent_threads(id) ON DELETE CASCADE
	);`

	createAgentTasksTable := `
	CREATE TABLE IF NOT EXISTS agent_tasks (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		department_id TEXT DEFAULT '',
		agent_id TEXT NOT NULL,
		requested_by TEXT DEFAULT '',
		parent_task_id TEXT DEFAULT '',
		thread_id TEXT DEFAULT '',
		type TEXT DEFAULT 'conversation',
		status TEXT NOT NULL,
		priority INTEGER DEFAULT 50,
		input_json TEXT DEFAULT '',
		result_json TEXT DEFAULT '',
		blocked_reason TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME NULL,
		FOREIGN KEY(company_id) REFERENCES companies(id) ON DELETE CASCADE,
		FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE
	);`

	createAgentRunsTable := `
	CREATE TABLE IF NOT EXISTS agent_runs (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		attempt INTEGER DEFAULT 1,
		status TEXT NOT NULL,
		provider TEXT DEFAULT '',
		model TEXT DEFAULT '',
		started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		ended_at DATETIME NULL,
		summary TEXT DEFAULT '',
		error TEXT DEFAULT '',
		FOREIGN KEY(task_id) REFERENCES agent_tasks(id) ON DELETE CASCADE
	);`

	createAgentDelegationsTable := `
	CREATE TABLE IF NOT EXISTS agent_delegations (
		id TEXT PRIMARY KEY,
		parent_task_id TEXT NOT NULL,
		from_agent_id TEXT NOT NULL,
		to_agent_id TEXT NOT NULL,
		instruction TEXT DEFAULT '',
		status TEXT DEFAULT 'queued',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(parent_task_id) REFERENCES agent_tasks(id) ON DELETE CASCADE
	);`

	createAgentConsensusRoundsTable := `
	CREATE TABLE IF NOT EXISTS agent_consensus_rounds (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		department_id TEXT DEFAULT '',
		topic TEXT NOT NULL,
		status TEXT DEFAULT 'open',
		created_by TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		closed_at TEXT DEFAULT ''
	);`

	createAgentConsensusVotesTable := `
	CREATE TABLE IF NOT EXISTS agent_consensus_votes (
		id TEXT PRIMARY KEY,
		round_id TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		option TEXT NOT NULL,
		confidence REAL DEFAULT 0.5,
		rationale TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(round_id) REFERENCES agent_consensus_rounds(id) ON DELETE CASCADE
	);`

	createAgentEventsTable := `
	CREATE TABLE IF NOT EXISTS agent_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		company_id TEXT DEFAULT '',
		department_id TEXT DEFAULT '',
		agent_id TEXT DEFAULT '',
		thread_id TEXT DEFAULT '',
		task_id TEXT DEFAULT '',
		event_type TEXT NOT NULL,
		severity TEXT DEFAULT 'info',
		payload_json TEXT DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createMemoryEntriesTable := `
	CREATE TABLE IF NOT EXISTS memory_entries (
		id TEXT PRIMARY KEY,
		scope_type TEXT NOT NULL,
		scope_id TEXT NOT NULL,
		source_type TEXT DEFAULT '',
		author_agent_id TEXT DEFAULT '',
		content TEXT NOT NULL,
		embedding TEXT DEFAULT '',
		tags_json TEXT DEFAULT '{}',
		importance REAL DEFAULT 0.5,
		ttl_at TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createMemoryLinksTable := `
	CREATE TABLE IF NOT EXISTS memory_links (
		id TEXT PRIMARY KEY,
		from_entry_id TEXT NOT NULL,
		to_entry_id TEXT NOT NULL,
		relation_type TEXT DEFAULT '',
		score REAL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createKnowledgeAssetsTable := `
	CREATE TABLE IF NOT EXISTS knowledge_assets (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		department_id TEXT DEFAULT '',
		path_or_uri TEXT NOT NULL,
		asset_type TEXT DEFAULT '',
		status TEXT DEFAULT 'indexed',
		hash TEXT DEFAULT '',
		metadata_json TEXT DEFAULT '{}',
		indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createKnowledgeIndexChunksTable := `
	CREATE TABLE IF NOT EXISTS knowledge_index_chunks (
		id TEXT PRIMARY KEY,
		asset_id TEXT NOT NULL,
		chunk_text TEXT NOT NULL,
		embedding TEXT DEFAULT '',
		symbols_json TEXT DEFAULT '{}',
		cluster_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(asset_id) REFERENCES knowledge_assets(id) ON DELETE CASCADE
	);`

	createSchedulesTable := `
	CREATE TABLE IF NOT EXISTS schedules (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		department_id TEXT DEFAULT '',
		target_agent_id TEXT NOT NULL,
		schedule_type TEXT NOT NULL,
		cron_expr TEXT DEFAULT '',
		rrule TEXT DEFAULT '',
		start_at TEXT DEFAULT '',
		next_run_at TEXT DEFAULT '',
		timezone TEXT DEFAULT 'UTC',
		payload_json TEXT DEFAULT '{}',
		is_active INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createScheduleRunsTable := `
	CREATE TABLE IF NOT EXISTS schedule_runs (
		id TEXT PRIMARY KEY,
		schedule_id TEXT NOT NULL,
		task_id TEXT DEFAULT '',
		status TEXT NOT NULL,
		triggered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		finished_at DATETIME NULL,
		error TEXT DEFAULT '',
		FOREIGN KEY(schedule_id) REFERENCES schedules(id) ON DELETE CASCADE
	);`

	createCapabilityPoliciesTable := `
	CREATE TABLE IF NOT EXISTS capability_policies (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		department_id TEXT DEFAULT '',
		agent_id TEXT DEFAULT '',
		action TEXT NOT NULL,
		effect TEXT NOT NULL,
		scope_pattern TEXT DEFAULT '*',
		approval_tier TEXT DEFAULT 'none',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createApprovalRequestsTable := `
	CREATE TABLE IF NOT EXISTS approval_requests (
		id TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		task_id TEXT DEFAULT '',
		action TEXT NOT NULL,
		tier TEXT NOT NULL,
		reason TEXT DEFAULT '',
		payload_json TEXT DEFAULT '{}',
		status TEXT DEFAULT 'pending',
		requested_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		resolved_at DATETIME NULL,
		resolved_by TEXT DEFAULT ''
	);`

	createAuditLogTable := `
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		actor_type TEXT DEFAULT '',
		actor_id TEXT DEFAULT '',
		payload_json TEXT NOT NULL,
		prev_hash TEXT DEFAULT '',
		event_hash TEXT NOT NULL,
		signature TEXT DEFAULT '',
		pubkey_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createLegacyMigrationMapTable := `
	CREATE TABLE IF NOT EXISTS legacy_migration_map (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		legacy_type TEXT NOT NULL,
		legacy_id TEXT NOT NULL,
		new_type TEXT NOT NULL,
		new_id TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createUsersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		name TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createUserTokensTable := `
	CREATE TABLE IF NOT EXISTS user_tokens (
		token TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);`

	for _, stmt := range []string{
		createVpsHostsTable,
		createChatSessionsTable,
		createChatMessagesTable,
		createSettingsTable,
		createPersonalityFactsTable,
		createSubagentsTable,
		createCompaniesTable,
		createDepartmentsTable,
		createAgentsTable,
		createAgentModelProfilesTable,
		createAgentModelBindingsTable,
		createAgentThreadsTable,
		createAgentMessagesTable,
		createAgentTasksTable,
		createAgentRunsTable,
		createAgentDelegationsTable,
		createAgentConsensusRoundsTable,
		createAgentConsensusVotesTable,
		createAgentEventsTable,
		createMemoryEntriesTable,
		createMemoryLinksTable,
		createKnowledgeAssetsTable,
		createKnowledgeIndexChunksTable,
		createSchedulesTable,
		createScheduleRunsTable,
		createCapabilityPoliciesTable,
		createApprovalRequestsTable,
		createAuditLogTable,
		createLegacyMigrationMapTable,
		createUsersTable,
		createUserTokensTable,
	} {
		if _, err := DB.Exec(stmt); err != nil {
			log.Fatalf("Error creating table: %v\n", err)
		}
	}

	// AgentOS indexes
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_companies_slug ON companies(slug)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_companies_owner_user_id ON companies(owner_user_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_departments_company_id ON departments(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agents_company_id ON agents(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agents_department_id ON agents(department_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agents_parent_agent_id ON agents(parent_agent_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_model_profiles_owner_user_id ON agent_model_profiles(owner_user_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_threads_agent_id ON agent_threads(agent_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_threads_company_id ON agent_threads(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_messages_thread_id ON agent_messages(thread_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_tasks_status ON agent_tasks(status)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_tasks_company_id ON agent_tasks(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_tasks_agent_id ON agent_tasks(agent_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_tasks_parent_task_id ON agent_tasks(parent_task_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_runs_task_id ON agent_runs(task_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_delegations_parent_task_id ON agent_delegations(parent_task_id)")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_consensus_votes_round_agent ON agent_consensus_votes(round_id, agent_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_events_company_id_id ON agent_events(company_id, id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_agent_events_task_id_id ON agent_events(task_id, id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_memory_entries_scope ON memory_entries(scope_type, scope_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_memory_entries_created_at ON memory_entries(created_at)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_knowledge_assets_company_id ON knowledge_assets(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_asset_id ON knowledge_index_chunks(asset_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_schedules_company_id ON schedules(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_schedules_next_run_at ON schedules(next_run_at)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_schedule_runs_schedule_id ON schedule_runs(schedule_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_capability_policies_company_id ON capability_policies(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_capability_policies_action ON capability_policies(action)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_approval_requests_company_id ON approval_requests(company_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_approval_requests_task_id ON approval_requests(task_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_audit_log_entity_id ON audit_log(entity_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_legacy_migration_map_legacy ON legacy_migration_map(legacy_type, legacy_id)")

	// Schema migrations for existing databases
	DB.Exec("ALTER TABLE chat_sessions ADD COLUMN model TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE chat_sessions ADD COLUMN project_path TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE chat_sessions ADD COLUMN reasoning_effort TEXT DEFAULT 'None'")
	DB.Exec("ALTER TABLE chat_sessions ADD COLUMN execution_mode TEXT DEFAULT 'Plan'")
	DB.Exec("ALTER TABLE chat_messages ADD COLUMN vector_embedding TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE companies ADD COLUMN workspace_path TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE companies ADD COLUMN deploy_command TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE companies ADD COLUMN owner_user_id TEXT DEFAULT ''")
	DB.Exec("UPDATE companies SET owner_user_id = 'admin' WHERE TRIM(IFNULL(owner_user_id,'')) = ''")
	DB.Exec("ALTER TABLE agent_model_profiles ADD COLUMN owner_user_id TEXT DEFAULT ''")
	DB.Exec("UPDATE agent_model_profiles SET owner_user_id = 'admin' WHERE TRIM(IFNULL(owner_user_id,'')) = ''")

	// AgentOS defaults
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_enabled', 'true')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_policy_enforcement', 'deny_default')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_mode', 'adaptive')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_min_workers', '1')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_max_workers', '4')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_min_reviewers', '1')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_max_reviewers', '2')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_cpu_soft', '70')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_cpu_hard', '90')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_ram_soft', '75')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_scheduler_ram_hard', '90')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_audit_signing_key_path', './agentos_ed25519.key')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_audit_pubkey_id', 'local-ed25519')")
	DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('agentos_kill_switch', 'false')")

	// User feature migrations
	DB.Exec("ALTER TABLE users ADD COLUMN openrouter_api_key TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN trial_ends_at TEXT DEFAULT ''")
	DB.Exec("UPDATE users SET trial_ends_at = datetime(created_at, '+3 days') WHERE TRIM(IFNULL(trial_ends_at,'')) = ''")
	DB.Exec("ALTER TABLE users ADD COLUMN is_blocked INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE users ADD COLUMN plan TEXT DEFAULT 'trial'")
	DB.Exec("ALTER TABLE users ADD COLUMN subscription_ends_at TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN trial_warning_sent INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE users ADD COLUMN renewal_warning_sent INTEGER DEFAULT 0")

	// Billing sessions
	DB.Exec(`CREATE TABLE IF NOT EXISTS billing_sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		payment_request_id TEXT NOT NULL UNIQUE,
		status TEXT DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_billing_sessions_user_id ON billing_sessions(user_id)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_billing_sessions_payment_request_id ON billing_sessions(payment_request_id)")

	// Per-user settings (overrides global settings per user)
	DB.Exec(`CREATE TABLE IF NOT EXISTS user_settings (
		user_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		PRIMARY KEY (user_id, key)
	)`)
}
