package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

type ToolDefinition struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  struct {
			Type       string                 `json:"type"`
			Properties map[string]interface{} `json:"properties"`
			Required   []string               `json:"required,omitempty"`
		} `json:"parameters"`
	} `json:"function"`
}

// Generate the array of JSON schemas attached to the stream payload
func GetAvailableTools() []ToolDefinition {
	var registry []ToolDefinition

	// ======================================
	// 1. Sandboxed FS Tools
	// ======================================
	fsList := ToolDefinition{Type: "function"}
	fsList.Function.Name = "list_files"
	fsList.Function.Description = "Lists files and directories in the AI Sandbox."
	fsList.Function.Parameters.Type = "object"
	fsList.Function.Parameters.Properties = map[string]interface{}{
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Relative or absolute path. If empty, lists the Sandbox root.",
		},
	}
	registry = append(registry, fsList)

	fsRead := ToolDefinition{Type: "function"}
	fsRead.Function.Name = "read_file"
	fsRead.Function.Description = "Reads the textual contents of a file inside the AI Sandbox."
	fsRead.Function.Parameters.Type = "object"
	fsRead.Function.Parameters.Properties = map[string]interface{}{
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Target file path.",
		},
	}
	fsRead.Function.Parameters.Required = []string{"path"}
	registry = append(registry, fsRead)

	fsWrite := ToolDefinition{Type: "function"}
	fsWrite.Function.Name = "write_file"
	fsWrite.Function.Description = "Writes or overwrites a text file inside the AI Sandbox."
	fsWrite.Function.Parameters.Type = "object"
	fsWrite.Function.Parameters.Properties = map[string]interface{}{
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Target file path.",
		},
		"content": map[string]interface{}{
			"type":        "string",
			"description": "Full file content to write.",
		},
	}
	fsWrite.Function.Parameters.Required = []string{"path", "content"}
	registry = append(registry, fsWrite)

	fsEdit := ToolDefinition{Type: "function"}
	fsEdit.Function.Name = "edit_file"
	fsEdit.Function.Description = "Modifies an existing file by replacing a unique search block with new content inside the Sandbox."
	fsEdit.Function.Parameters.Type = "object"
	fsEdit.Function.Parameters.Properties = map[string]interface{}{
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Target file path.",
		},
		"search": map[string]interface{}{
			"type":        "string",
			"description": "The exact block of code or text to find. It must be completely unique within the file.",
		},
		"replace": map[string]interface{}{
			"type":        "string",
			"description": "The new content to replace the search block with.",
		},
	}
	fsEdit.Function.Parameters.Required = []string{"path", "search", "replace"}
	registry = append(registry, fsEdit)

	fsGrep := ToolDefinition{Type: "function"}
	fsGrep.Function.Name = "grep_search"
	fsGrep.Function.Description = "Recursively searches all files in the Sandbox for a specific Regex pattern."
	fsGrep.Function.Parameters.Type = "object"
	fsGrep.Function.Parameters.Properties = map[string]interface{}{
		"pattern": map[string]interface{}{
			"type":        "string",
			"description": "Regex or string pattern to search for.",
		},
		"path": map[string]interface{}{
			"type":        "string",
			"description": "Optional subdirectory to restrict search. Defaults to Sandbox root.",
		},
	}
	fsGrep.Function.Parameters.Required = []string{"pattern"}
	registry = append(registry, fsGrep)

	addProject := ToolDefinition{Type: "function"}
	addProject.Function.Name = "add_workspace_project"
	addProject.Function.Description = "Creates a new project directory and permanently mounts it into the UI Workspace File Explorer."
	addProject.Function.Parameters.Type = "object"
	addProject.Function.Parameters.Properties = map[string]interface{}{
		"name": map[string]interface{}{
			"type":        "string",
			"description": "The user-facing display name of the project.",
		},
		"path": map[string]interface{}{
			"type":        "string",
			"description": "The folder path to create and mount.",
		},
		"deploy_command": map[string]interface{}{
			"type":        "string",
			"description": "Optional terminal command to run when 'Deploy' is clicked.",
		},
	}
	addProject.Function.Parameters.Required = []string{"name", "path"}
	registry = append(registry, addProject)

	// ======================================
	// User Interaction Toolkit
	// ======================================
	askQ := ToolDefinition{Type: "function"}
	askQ.Function.Name = "ask_user_question"
	askQ.Function.Description = "Pauses AI execution to ask the user a specific question. Use this to gather architectural preferences, clarify requirements, or request a decision before drafting plans or writing code."
	askQ.Function.Parameters.Type = "object"
	askQ.Function.Parameters.Properties = map[string]interface{}{
		"question": map[string]interface{}{
			"type":        "string",
			"description": "The precise question to ask the user.",
		},
		"options": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "An array of 2 to 5 distinct options for the user to choose from (e.g. ['React', 'Vue', 'Vanilla JS']).",
		},
	}
	askQ.Function.Parameters.Required = []string{"question", "options"}
	registry = append(registry, askQ)

	// ======================================
	// 3. Sandboxed Terminal Execution
	// ======================================
	termExec := ToolDefinition{Type: "function"}
	termExec.Function.Name = "execute_command"
	termExec.Function.Description = "Executes a raw bash terminal command strictly jailed within the AI Sandbox directory."
	termExec.Function.Parameters.Type = "object"
	termExec.Function.Parameters.Properties = map[string]interface{}{
		"command": map[string]interface{}{
			"type":        "string",
			"description": "The exact shell command string (e.g. 'npm install react'). Avoid interactive commands like 'nano' or 'vim'.",
		},
	}
	termExec.Function.Parameters.Required = []string{"command"}
	registry = append(registry, termExec)

	// ======================================
	// 4. Web Toolkit
	// ======================================
	webScrape := ToolDefinition{Type: "function"}
	webScrape.Function.Name = "web_scrape"
	webScrape.Function.Description = "Downloads a specific webpage and extracts all plaintext paragraphs, stripping HTML junk."
	webScrape.Function.Parameters.Type = "object"
	webScrape.Function.Parameters.Properties = map[string]interface{}{
		"url": map[string]interface{}{
			"type":        "string",
			"description": "The exact 'https://' URL to scrape.",
		},
	}
	webScrape.Function.Parameters.Required = []string{"url"}
	registry = append(registry, webScrape)

	// ======================================
	// 5. Version Control / Checkpointing
	// ======================================
	undoOp := ToolDefinition{Type: "function"}
	undoOp.Function.Name = "undo_checkpoint"
	undoOp.Function.Description = "Physically reverts the entire Sandbox workspace back to its state prior to the last file modification or terminal command execution."
	undoOp.Function.Parameters.Type = "object"
	undoOp.Function.Parameters.Properties = map[string]interface{}{}
	registry = append(registry, undoOp)

	// ======================================
	// 5b. Extended File Operations
	// ======================================
	findFiles := ToolDefinition{Type: "function"}
	findFiles.Function.Name = "find_files"
	findFiles.Function.Description = "Finds files by name glob pattern within the workspace (e.g. '*.go', 'test_*', 'README.md'). Skips hidden dirs, node_modules, and vendor."
	findFiles.Function.Parameters.Type = "object"
	findFiles.Function.Parameters.Properties = map[string]interface{}{
		"pattern": map[string]interface{}{"type": "string", "description": "Glob pattern for file names, e.g. '*.go', 'test_*'."},
		"path":    map[string]interface{}{"type": "string", "description": "Optional subdirectory to search within."},
	}
	findFiles.Function.Parameters.Required = []string{"pattern"}
	registry = append(registry, findFiles)

	renameFile := ToolDefinition{Type: "function"}
	renameFile.Function.Name = "rename_file"
	renameFile.Function.Description = "Renames or moves a file or directory within the workspace."
	renameFile.Function.Parameters.Type = "object"
	renameFile.Function.Parameters.Properties = map[string]interface{}{
		"from": map[string]interface{}{"type": "string", "description": "Source path (relative to workspace)."},
		"to":   map[string]interface{}{"type": "string", "description": "Destination path (relative to workspace)."},
	}
	renameFile.Function.Parameters.Required = []string{"from", "to"}
	registry = append(registry, renameFile)

	checkCode := ToolDefinition{Type: "function"}
	checkCode.Function.Name = "check_code"
	checkCode.Function.Description = "Runs language-appropriate static analysis on a file or directory: go vet (Go), tsc --noEmit (TypeScript), eslint (JavaScript), flake8 (Python), cargo check (Rust). Auto-detects language from file extension."
	checkCode.Function.Parameters.Type = "object"
	checkCode.Function.Parameters.Properties = map[string]interface{}{
		"path":     map[string]interface{}{"type": "string", "description": "File or directory path to analyze."},
		"language": map[string]interface{}{"type": "string", "description": "Optional: 'go'|'typescript'|'javascript'|'python'|'rust'. Auto-detected if omitted."},
	}
	checkCode.Function.Parameters.Required = []string{"path"}
	registry = append(registry, checkCode)

	contextTree := ToolDefinition{Type: "function"}
	contextTree.Function.Name = "get_context_tree"
	contextTree.Function.Description = "Native structural context tree of the current project. Returns folders/files, file headers, and optional symbols with line ranges."
	contextTree.Function.Parameters.Type = "object"
	contextTree.Function.Parameters.Properties = map[string]interface{}{
		"target_path":     map[string]interface{}{"type": "string", "description": "Optional subpath to analyze (relative to project root)."},
		"depth_limit":     map[string]interface{}{"type": "number", "description": "Optional directory depth limit."},
		"include_symbols": map[string]interface{}{"type": "boolean", "description": "Include symbol-level details (default true)."},
		"max_tokens":      map[string]interface{}{"type": "number", "description": "Approximate output token cap. Tool auto-prunes if exceeded."},
	}
	registry = append(registry, contextTree)

	fileSkeleton := ToolDefinition{Type: "function"}
	fileSkeleton.Function.Name = "get_file_skeleton"
	fileSkeleton.Function.Description = "Native file skeleton view. Returns signatures for functions/classes/types and line ranges without dumping full file bodies."
	fileSkeleton.Function.Parameters.Type = "object"
	fileSkeleton.Function.Parameters.Properties = map[string]interface{}{
		"file_path": map[string]interface{}{"type": "string", "description": "Target file path (relative to project root)."},
	}
	fileSkeleton.Function.Parameters.Required = []string{"file_path"}
	registry = append(registry, fileSkeleton)

	semanticSearch := ToolDefinition{Type: "function"}
	semanticSearch.Function.Name = "semantic_code_search"
	semanticSearch.Function.Description = "Native semantic code search with hybrid embedding + keyword ranking over project files."
	semanticSearch.Function.Parameters.Type = "object"
	semanticSearch.Function.Parameters.Properties = map[string]interface{}{
		"query":                  map[string]interface{}{"type": "string", "description": "Natural language search query."},
		"top_k":                  map[string]interface{}{"type": "number", "description": "Max number of matches to return."},
		"semantic_weight":        map[string]interface{}{"type": "number", "description": "Weight for embedding similarity."},
		"keyword_weight":         map[string]interface{}{"type": "number", "description": "Weight for keyword overlap score."},
		"min_semantic_score":     map[string]interface{}{"type": "number", "description": "Minimum semantic score threshold (0-1 or 0-100)."},
		"min_keyword_score":      map[string]interface{}{"type": "number", "description": "Minimum keyword score threshold (0-1 or 0-100)."},
		"min_combined_score":     map[string]interface{}{"type": "number", "description": "Minimum combined score threshold (0-1 or 0-100)."},
		"require_keyword_match":  map[string]interface{}{"type": "boolean", "description": "When true, discard results with no keyword overlap."},
		"require_semantic_match": map[string]interface{}{"type": "boolean", "description": "When true, discard results with no semantic match."},
	}
	semanticSearch.Function.Parameters.Required = []string{"query"}
	registry = append(registry, semanticSearch)

	semanticIdentifiers := ToolDefinition{Type: "function"}
	semanticIdentifiers.Function.Name = "semantic_identifier_search"
	semanticIdentifiers.Function.Description = "Native identifier-level semantic search for functions/classes/variables with ranked call sites."
	semanticIdentifiers.Function.Parameters.Type = "object"
	semanticIdentifiers.Function.Parameters.Properties = map[string]interface{}{
		"query":                    map[string]interface{}{"type": "string", "description": "Natural language query for identifier intent."},
		"top_k":                    map[string]interface{}{"type": "number", "description": "Max identifiers to return."},
		"top_calls_per_identifier": map[string]interface{}{"type": "number", "description": "Max call sites per identifier."},
		"include_kinds":            map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional kind filter list (e.g. function,class,variable)."},
		"semantic_weight":          map[string]interface{}{"type": "number", "description": "Weight for embedding similarity."},
		"keyword_weight":           map[string]interface{}{"type": "number", "description": "Weight for keyword score."},
	}
	semanticIdentifiers.Function.Parameters.Required = []string{"query"}
	registry = append(registry, semanticIdentifiers)

	blastRadius := ToolDefinition{Type: "function"}
	blastRadius.Function.Name = "get_blast_radius"
	blastRadius.Function.Description = "Native blast-radius analysis. Finds references/import-usage lines for a symbol across the project."
	blastRadius.Function.Parameters.Type = "object"
	blastRadius.Function.Parameters.Properties = map[string]interface{}{
		"symbol_name":  map[string]interface{}{"type": "string", "description": "Symbol name to trace."},
		"file_context": map[string]interface{}{"type": "string", "description": "Optional defining file path to avoid counting definition line."},
	}
	blastRadius.Function.Parameters.Required = []string{"symbol_name"}
	registry = append(registry, blastRadius)

	staticAnalysis := ToolDefinition{Type: "function"}
	staticAnalysis.Function.Name = "run_static_analysis"
	staticAnalysis.Function.Description = "Native multi-language static analysis runner (go vet, tsc/eslint, py_compile, cargo check where applicable)."
	staticAnalysis.Function.Parameters.Type = "object"
	staticAnalysis.Function.Parameters.Properties = map[string]interface{}{
		"target_path": map[string]interface{}{"type": "string", "description": "Optional file/directory to scope analysis."},
	}
	registry = append(registry, staticAnalysis)

	semanticNavigate := ToolDefinition{Type: "function"}
	semanticNavigate.Function.Name = "semantic_navigate"
	semanticNavigate.Function.Description = "Native semantic navigator that clusters project files into topic groups for high-level exploration."
	semanticNavigate.Function.Parameters.Type = "object"
	semanticNavigate.Function.Parameters.Properties = map[string]interface{}{
		"max_depth":    map[string]interface{}{"type": "number", "description": "Optional cluster depth hint."},
		"max_clusters": map[string]interface{}{"type": "number", "description": "Maximum clusters to return."},
	}
	registry = append(registry, semanticNavigate)

	featureHub := ToolDefinition{Type: "function"}
	featureHub.Function.Name = "get_feature_hub"
	featureHub.Function.Description = "Native feature-hub graph over markdown wikilinks. List hubs, inspect a hub, or detect orphaned code files."
	featureHub.Function.Parameters.Type = "object"
	featureHub.Function.Parameters.Properties = map[string]interface{}{
		"hub_path":     map[string]interface{}{"type": "string", "description": "Optional explicit hub markdown path."},
		"feature_name": map[string]interface{}{"type": "string", "description": "Optional feature name to resolve to a hub."},
		"show_orphans": map[string]interface{}{"type": "boolean", "description": "If true, list source files not linked from hubs."},
	}
	registry = append(registry, featureHub)

	proposeCommit := ToolDefinition{Type: "function"}
	proposeCommit.Function.Name = "propose_commit"
	proposeCommit.Function.Description = "Native guarded write operation. Creates a restore point, writes file content, and returns validation warnings."
	proposeCommit.Function.Parameters.Type = "object"
	proposeCommit.Function.Parameters.Properties = map[string]interface{}{
		"file_path":   map[string]interface{}{"type": "string", "description": "File path to write (relative to project root)."},
		"new_content": map[string]interface{}{"type": "string", "description": "Full new file content."},
	}
	proposeCommit.Function.Parameters.Required = []string{"file_path", "new_content"}
	registry = append(registry, proposeCommit)

	listRestorePoints := ToolDefinition{Type: "function"}
	listRestorePoints.Function.Name = "list_restore_points"
	listRestorePoints.Function.Description = "List native restore points created before propose_commit writes."
	listRestorePoints.Function.Parameters.Type = "object"
	listRestorePoints.Function.Parameters.Properties = map[string]interface{}{}
	registry = append(registry, listRestorePoints)

	undoChange := ToolDefinition{Type: "function"}
	undoChange.Function.Name = "undo_change"
	undoChange.Function.Description = "Restore files to the state captured by a native restore point ID."
	undoChange.Function.Parameters.Type = "object"
	undoChange.Function.Parameters.Properties = map[string]interface{}{
		"point_id": map[string]interface{}{"type": "string", "description": "Restore point ID from list_restore_points."},
	}
	undoChange.Function.Parameters.Required = []string{"point_id"}
	registry = append(registry, undoChange)

	webSearch := ToolDefinition{Type: "function"}
	webSearch.Function.Name = "web_search"
	webSearch.Function.Description = "Searches the internet using DuckDuckGo. Use to find documentation, look up error messages, research libraries, or find solutions to problems."
	webSearch.Function.Parameters.Type = "object"
	webSearch.Function.Parameters.Properties = map[string]interface{}{
		"query": map[string]interface{}{"type": "string", "description": "The search query string."},
	}
	webSearch.Function.Parameters.Required = []string{"query"}
	registry = append(registry, webSearch)

	// ======================================
	// 5c. Skills System
	// ======================================
	loadSkill := ToolDefinition{Type: "function"}
	loadSkill.Function.Name = "load_skill"
	loadSkill.Function.Description = "Loads the full content of a named skill file. Skills provide deep specialized knowledge (debugging patterns, architectural recipes, language idioms) loaded on demand."
	loadSkill.Function.Parameters.Type = "object"
	loadSkill.Function.Parameters.Properties = map[string]interface{}{
		"name": map[string]interface{}{"type": "string", "description": "The skill name (e.g. 'go_debugging', 'react_patterns', 'git_workflow')."},
	}
	loadSkill.Function.Parameters.Required = []string{"name"}
	registry = append(registry, loadSkill)

	// ======================================
	// 6. Background Subagents
	// ======================================
	spawnAgent := ToolDefinition{Type: "function"}
	spawnAgent.Function.Name = "spawn_subagent"
	spawnAgent.Function.Description = "Spawns a background AI subagent to handle a long-running task independently (research, code review, testing, etc.) without blocking the main conversation. Returns a subagent ID."
	spawnAgent.Function.Parameters.Type = "object"
	spawnAgent.Function.Parameters.Properties = map[string]interface{}{
		"name":       map[string]interface{}{"type": "string", "description": "A short, descriptive name for this subagent (e.g., 'Researcher-1', 'CodeReviewer')."},
		"task":       map[string]interface{}{"type": "string", "description": "A precise, self-contained task description for the subagent."},
		"session_id": map[string]interface{}{"type": "string", "description": "The current session ID so the subagent can notify the parent conversation when done."},
	}
	spawnAgent.Function.Parameters.Required = []string{"name", "task"}
	registry = append(registry, spawnAgent)

	getAgentStatus := ToolDefinition{Type: "function"}
	getAgentStatus.Function.Name = "get_subagent_status"
	getAgentStatus.Function.Description = "Checks the status and output of a spawned background subagent by its ID."
	getAgentStatus.Function.Parameters.Type = "object"
	getAgentStatus.Function.Parameters.Properties = map[string]interface{}{
		"id": map[string]interface{}{"type": "string", "description": "The subagent ID returned by spawn_subagent."},
	}
	getAgentStatus.Function.Parameters.Required = []string{"id"}
	registry = append(registry, getAgentStatus)

	// ======================================
	// 8. Dynamic MCP Tools
	// ======================================
	mcpTools := GetMCPTools()
	for _, mt := range mcpTools {
		mTool := ToolDefinition{Type: "function"}
		mTool.Function.Name = mt.Name
		mTool.Function.Description = mt.Description
		mTool.Function.Parameters.Type = "object"
		mTool.Function.Parameters.Properties = map[string]interface{}{}
		if propsRaw, ok := mt.Schema["properties"]; ok {
			if props, ok := propsRaw.(map[string]interface{}); ok {
				mTool.Function.Parameters.Properties = props
			}
		}
		if req, ok := mt.Schema["required"].([]interface{}); ok {
			for _, r := range req {
				if rs, ok := r.(string); ok {
					mTool.Function.Parameters.Required = append(mTool.Function.Parameters.Required, rs)
				}
			}
		}
		registry = append(registry, mTool)
	}

	return registry
}

func executeUndoCheckpoint(rawArgs string) string {
	cmd := exec.Command("git", "reset", "--hard", "HEAD~1")
	cmd.Dir = WorkspaceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error reverting checkpoint: %v\nOutput: %s", err, string(out))
	}
	return fmt.Sprintf("Success: Sandbox reverted to previous checkpoint.\n%s", string(out))
}

// Routes tool call arguments to the strict Go function implementations
func ExecuteTool(name string, rawArgs string, projectRoot string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return fmt.Sprintf("Error: Invalid JSON arguments passed to %s", name)
	}

	log.Printf("Tool Dispatcher: Executing '%s'", name)

	switch name {
	case "list_files":
		return executeListFiles(rawArgs, projectRoot)
	case "read_file":
		return executeReadFile(rawArgs, projectRoot)
	case "write_file":
		return executeWriteFile(rawArgs, projectRoot)
	case "edit_file":
		return executeEditFile(rawArgs, projectRoot)
	case "add_workspace_project":
		return executeAddWorkspaceProject(rawArgs, projectRoot)
	case "grep_search":
		return executeGrepSearch(rawArgs, projectRoot)
	case "execute_command":
		return executeCommand(rawArgs, projectRoot)
	case "undo_checkpoint":
		return executeUndoCheckpoint(rawArgs)
	case "web_scrape":
		return executeWebScrape(rawArgs)
	case "web_search":
		return executeWebSearch(rawArgs)
	case "find_files":
		return executeFindFiles(rawArgs, projectRoot)
	case "rename_file":
		return executeRenameFile(rawArgs, projectRoot)
	case "check_code":
		return executeCheckCode(rawArgs, projectRoot)
	case "get_context_tree":
		return executeGetContextTree(rawArgs, projectRoot)
	case "get_file_skeleton":
		return executeGetFileSkeleton(rawArgs, projectRoot)
	case "semantic_code_search":
		return executeSemanticCodeSearch(rawArgs, projectRoot)
	case "semantic_identifier_search":
		return executeSemanticIdentifierSearch(rawArgs, projectRoot)
	case "get_blast_radius":
		return executeGetBlastRadius(rawArgs, projectRoot)
	case "run_static_analysis":
		return executeRunStaticAnalysisNative(rawArgs, projectRoot)
	case "semantic_navigate":
		return executeSemanticNavigate(rawArgs, projectRoot)
	case "get_feature_hub":
		return executeGetFeatureHub(rawArgs, projectRoot)
	case "propose_commit":
		return executeProposeCommitNative(rawArgs, projectRoot)
	case "list_restore_points":
		return executeListRestorePointsNative(rawArgs, projectRoot)
	case "undo_change":
		return executeUndoChangeNative(rawArgs, projectRoot)
	case "load_skill":
		return executeLoadSkill(rawArgs)
	case "spawn_subagent":
		return executeSpawnSubagent(rawArgs)
	case "get_subagent_status":
		return executeGetSubagentStatus(rawArgs)
	default:
		if strings.HasPrefix(name, "mcp_") {
			return CallMCPTool(name, rawArgs)
		}
		return fmt.Sprintf("Error: Tool '%s' is not registered.", name)
	}
}
