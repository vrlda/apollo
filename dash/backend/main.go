package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/danilrybalkin/apollo-dash/agentos"
	"github.com/danilrybalkin/apollo-dash/db"
	"github.com/danilrybalkin/apollo-dash/handlers"
	"github.com/danilrybalkin/apollo-dash/tools"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found or error loading it. Using environment variables.")
	}

	db.InitDB()
	handlers.SeedDefaultModelProfiles()
	handlers.InitEmailer()
	tools.InitWorkspace()
	tools.SyncBundledSkills()
	tools.SetSubagentFuncs(handlers.SpawnSubagent, handlers.GetSubagentStatus)

	workspaceRoot := os.Getenv("AGENTHQ_WORKSPACE_ROOT")
	if workspaceRoot == "" {
		workspaceRoot = tools.WorkspaceDir
	}
	agentSvc := agentos.NewService(db.DB, workspaceRoot)
	handlers.SetAgentOSService(agentSvc)
	go agentSvc.Start(context.Background())
	handlers.StartEmailNotifier(context.Background())

	// Initialize RAG Memory Engine
	handlers.LoadMemoryBank()
	go handlers.StartMemoryIndexer()

	// Initialize MCP servers (reconnect persisted servers)
	go handlers.InitMCPServers()

	port := os.Getenv("PORT")
	if port == "" {
		port = "4000"
	}

	mux := http.NewServeMux()

	// Auth routes (public — no auth required)
	mux.HandleFunc("/api/auth/register", handlers.RegisterHandler)
	mux.HandleFunc("/api/auth/login", handlers.LoginHandler)
	mux.HandleFunc("/api/auth/me", handlers.MeHandler)
	mux.HandleFunc("/api/auth/change-password", handlers.ChangePasswordHandler)
	mux.HandleFunc("/api/auth/delete-account", handlers.DeleteAccountHandler)
	mux.HandleFunc("/api/auth/logout", handlers.LogoutHandler)

	// Admin routes (require DASHBOARD_PASSWORD bearer token)
	mux.HandleFunc("/api/admin/stats", handlers.AdminStatsHandler)
	mux.HandleFunc("/api/admin/users", handlers.AdminUsersHandler)
	mux.HandleFunc("/api/admin/users/", handlers.AdminUserByIDHandler)

	// Billing (Vulta payments + webhook)
	mux.HandleFunc("/api/billing/checkout", handlers.BillingCheckoutHandler)
	mux.HandleFunc("/api/billing/webhook", handlers.BillingWebhookHandler)

	// API Routes under /api/*
	mux.HandleFunc("/api/sys", handlers.SysStatsHandler)
	mux.HandleFunc("/api/ping", handlers.PingHandler)
	mux.HandleFunc("/api/vps", handlers.VpsManagerHandler)
	mux.HandleFunc("/api/chat", handlers.AiChatHandler)
	mux.HandleFunc("/api/models", handlers.AiModelsHandler)
	mux.HandleFunc("/api/sessions", handlers.AiSessionsHandler)
	mux.HandleFunc("/api/messages", handlers.AiMessagesHandler)
	mux.HandleFunc("/api/settings", handlers.SettingsHandler)
	mux.HandleFunc("/api/sandbox/restore", handlers.SandboxRestoreHandler)

	// Workspace IDE routes
	mux.HandleFunc("/api/workspace/projects", handlers.WorkspaceProjectsHandler)
	mux.HandleFunc("/api/workspace/tree", handlers.WorkspaceTreeHandler)
	mux.HandleFunc("/api/workspace/file", handlers.WorkspaceFileHandler)
	mux.HandleFunc("/api/workspace/deploy", handlers.WorkspaceDeployHandler)

	// Background Subagents
	mux.HandleFunc("/api/subagents", handlers.SubagentsHandler)
	mux.HandleFunc("/api/companies", handlers.AgentOSCompaniesHandler)
	mux.HandleFunc("/api/companies/", handlers.AgentOSCompanyByIDHandler)
	// Compatibility aliases for reverse proxies that strip the /api prefix.
	mux.HandleFunc("/companies", handlers.AgentOSCompaniesHandler)
	mux.HandleFunc("/companies/", handlers.AgentOSCompanyByIDHandler)
	mux.HandleFunc("/api/departments", handlers.AgentOSDepartmentsHandler)
	mux.HandleFunc("/api/departments/", handlers.AgentOSDepartmentByIDHandler)
	mux.HandleFunc("/api/agents", handlers.AgentOSAgentsHandler)
	mux.HandleFunc("/api/agents/", handlers.AgentOSAgentByIDHandler)
	mux.HandleFunc("/api/model-profiles", handlers.AgentOSModelProfilesHandler)
	mux.HandleFunc("/api/threads", handlers.AgentOSThreadsHandler)
	mux.HandleFunc("/api/threads/", handlers.AgentOSThreadMessagesHandler)
	mux.HandleFunc("/api/tasks", handlers.AgentOSTasksHandler)
	mux.HandleFunc("/api/tasks/", handlers.AgentOSTaskByIDHandler)
	mux.HandleFunc("/api/consensus/rounds", handlers.AgentOSConsensusRoundsHandler)
	mux.HandleFunc("/api/consensus/rounds/", handlers.AgentOSConsensusRoundByIDHandler)
	mux.HandleFunc("/api/memory/query", handlers.AgentOSMemoryQueryHandler)
	mux.HandleFunc("/api/memory/write", handlers.AgentOSMemoryWriteHandler)
	mux.HandleFunc("/api/memory/timeline", handlers.AgentOSMemoryTimelineHandler)
	mux.HandleFunc("/api/schedules", handlers.AgentOSSchedulesHandler)
	mux.HandleFunc("/api/schedules/", handlers.AgentOSScheduleByIDHandler)
	mux.HandleFunc("/api/events", handlers.AgentOSEventsHandler)
	mux.HandleFunc("/api/events/stream", handlers.AgentOSEventsStreamHandler)
	mux.HandleFunc("/api/policies", handlers.AgentOSPoliciesHandler)
	mux.HandleFunc("/api/policies/test", handlers.AgentOSPolicyTestHandler)
	mux.HandleFunc("/api/approvals", handlers.AgentOSApprovalsHandler)
	mux.HandleFunc("/api/approvals/resolve", handlers.AgentOSApprovalsResolveHandler)
	mux.HandleFunc("/api/audit", handlers.AgentOSAuditHandler)
	mux.HandleFunc("/api/audit/verify", handlers.AgentOSAuditVerifyHandler)
	mux.HandleFunc("/api/health/agentos", handlers.AgentOSHealthHandler)
	mux.HandleFunc("/api/topology", handlers.AgentOSTopologyHandler)
	mux.HandleFunc("/api/inter-agent", handlers.AgentInboxHandler)

	// Execution Mode Confirmations
	mux.HandleFunc("/api/confirm", handlers.ConfirmHandler)

	// MCP (Model Context Protocol) server management
	mux.HandleFunc("/api/mcp", handlers.MCPHandler)

	// WebSocket Terminal (PTY)
	mux.HandleFunc("/ws/terminal", handlers.TerminalHandler)

	// Third-party API Endpoints (OpenAI format wrapper)
	mux.HandleFunc("/api/ext/chat/completions", handlers.AiExternalChatHandler)
	mux.HandleFunc("/api/ext/models", handlers.AiModelsHandler)

	// Combine API routes and wrap them in auth (if desired to protect API)
	// Or we can protect everything (Static + API)
	// Let's protect EVERYTHING so the dashboard is a true Auth Wall.

	// Serve React Frontend
	// The frontend/dist folder will be created by Vite build
	frontendDir := filepath.Join("..", "frontend", "dist")
	fs := http.FileServer(http.Dir(frontendDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
		requestedPath := filepath.Join(frontendDir, cleanPath)
		if info, err := os.Stat(requestedPath); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(frontendDir, "index.html"))
	})

	// Wrap entire mux with Auth
	protectedMux := handlers.BasicAuthMiddleware(mux)

	fmt.Printf("AgentHQ Backend running on port %s\n", port)
	fmt.Printf("Serving static files from %s\n", frontendDir)
	log.Fatal(http.ListenAndServe(":"+port, protectedMux))
}
