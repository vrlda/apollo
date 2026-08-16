package agentos

import "time"

type Company struct {
	ID            string    `json:"id"`
	OwnerUserID   string    `json:"owner_user_id,omitempty"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Timezone      string    `json:"timezone"`
	WorkspacePath string    `json:"workspace_path"`
	DeployCommand string    `json:"deploy_command"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Department struct {
	ID          string    `json:"id"`
	CompanyID   string    `json:"company_id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Agent struct {
	ID             string    `json:"id"`
	CompanyID      string    `json:"company_id"`
	DepartmentID   string    `json:"department_id"`
	Name           string    `json:"name"`
	RoleType       string    `json:"role_type"`
	ParentAgentID  string    `json:"parent_agent_id"`
	IdentityPrompt string    `json:"identity_prompt"`
	Status         string    `json:"status"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AgentModelProfile struct {
	ID                string    `json:"id"`
	OwnerUserID       string    `json:"owner_user_id,omitempty"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	SettingsJSON      string    `json:"settings_json"`
	FallbackChainJSON string    `json:"fallback_chain_json"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type AgentModelBinding struct {
	AgentID          string  `json:"agent_id"`
	PrimaryProfileID string  `json:"primary_profile_id"`
	Temperature      float64 `json:"temperature"`
	MaxTokens        int     `json:"max_tokens"`
	ReasoningEffort  string  `json:"reasoning_effort"`
}

type AgentThread struct {
	ID           string    `json:"id"`
	CompanyID    string    `json:"company_id"`
	DepartmentID string    `json:"department_id"`
	AgentID      string    `json:"agent_id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AgentMessage struct {
	ID          int64     `json:"id"`
	ThreadID    string    `json:"thread_id"`
	Role        string    `json:"role"`
	Content     string    `json:"content"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
}

type AgentTask struct {
	ID            string     `json:"id"`
	CompanyID     string     `json:"company_id"`
	DepartmentID  string     `json:"department_id"`
	AgentID       string     `json:"agent_id"`
	RequestedBy   string     `json:"requested_by"`
	ParentTaskID  string     `json:"parent_task_id"`
	ThreadID      string     `json:"thread_id"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	Priority      int        `json:"priority"`
	InputJSON     string     `json:"input_json"`
	ResultJSON    string     `json:"result_json"`
	BlockedReason string     `json:"blocked_reason"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

type AgentRun struct {
	ID        string     `json:"id"`
	TaskID    string     `json:"task_id"`
	Attempt   int        `json:"attempt"`
	Status    string     `json:"status"`
	Provider  string     `json:"provider"`
	Model     string     `json:"model"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Summary   string     `json:"summary"`
	Error     string     `json:"error"`
}

type AgentDelegation struct {
	ID           string    `json:"id"`
	ParentTaskID string    `json:"parent_task_id"`
	FromAgentID  string    `json:"from_agent_id"`
	ToAgentID    string    `json:"to_agent_id"`
	Instruction  string    `json:"instruction"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Schedule struct {
	ID            string    `json:"id"`
	CompanyID     string    `json:"company_id"`
	DepartmentID  string    `json:"department_id"`
	TargetAgentID string    `json:"target_agent_id"`
	ScheduleType  string    `json:"schedule_type"`
	CronExpr      string    `json:"cron_expr"`
	RRule         string    `json:"rrule"`
	StartAt       string    `json:"start_at"`
	NextRunAt     string    `json:"next_run_at"`
	Timezone      string    `json:"timezone"`
	PayloadJSON   string    `json:"payload_json"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type MemoryEntry struct {
	ID            string    `json:"id"`
	ScopeType     string    `json:"scope_type"`
	ScopeID       string    `json:"scope_id"`
	SourceType    string    `json:"source_type"`
	AuthorAgentID string    `json:"author_agent_id"`
	Content       string    `json:"content"`
	Embedding     string    `json:"embedding"`
	TagsJSON      string    `json:"tags_json"`
	Importance    float64   `json:"importance"`
	TTLAt         string    `json:"ttl_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ConsensusRound struct {
	ID           string    `json:"id"`
	CompanyID    string    `json:"company_id"`
	DepartmentID string    `json:"department_id"`
	Topic        string    `json:"topic"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	ClosedAt     string    `json:"closed_at"`
}

type ConsensusVote struct {
	ID         string    `json:"id"`
	RoundID    string    `json:"round_id"`
	AgentID    string    `json:"agent_id"`
	Option     string    `json:"option"`
	Confidence float64   `json:"confidence"`
	Rationale  string    `json:"rationale"`
	CreatedAt  time.Time `json:"created_at"`
}

type PolicyRule struct {
	ID           string    `json:"id"`
	CompanyID    string    `json:"company_id"`
	DepartmentID string    `json:"department_id"`
	AgentID      string    `json:"agent_id"`
	Action       string    `json:"action"`
	Effect       string    `json:"effect"`
	ScopePattern string    `json:"scope_pattern"`
	ApprovalTier string    `json:"approval_tier"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ApprovalRequest struct {
	ID          string `json:"id"`
	CompanyID   string `json:"company_id"`
	TaskID      string `json:"task_id"`
	Action      string `json:"action"`
	Tier        string `json:"tier"`
	Reason      string `json:"reason"`
	PayloadJSON string `json:"payload_json"`
	Status      string `json:"status"`
	RequestedAt string `json:"requested_at"`
	ResolvedAt  string `json:"resolved_at"`
	ResolvedBy  string `json:"resolved_by"`
}

type AgentEvent struct {
	ID           int64     `json:"id"`
	CompanyID    string    `json:"company_id"`
	DepartmentID string    `json:"department_id"`
	AgentID      string    `json:"agent_id"`
	ThreadID     string    `json:"thread_id"`
	TaskID       string    `json:"task_id"`
	EventType    string    `json:"event_type"`
	Severity     string    `json:"severity"`
	PayloadJSON  string    `json:"payload_json"`
	CreatedAt    time.Time `json:"created_at"`
}
