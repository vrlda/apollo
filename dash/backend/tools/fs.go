package tools

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danilrybalkin/apollo-dash/db"
)

var WorkspaceDir = resolveWorkspaceDir()

func resolveWorkspaceDir() string {
	for _, key := range []string{"AGENTHQ_WORKSPACE_ROOT", "APOLLO_WORKSPACE_ROOT"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			if abs, err := filepath.Abs(value); err == nil {
				return filepath.Clean(abs)
			}
			return filepath.Clean(value)
		}
	}
	if abs, err := filepath.Abs(filepath.Join("data", "workspaces")); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(filepath.Join("data", "workspaces"))
}

func withinBase(path string, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Ensures the workspace directory exists on boot
func InitWorkspace() {
	if _, err := os.Stat(WorkspaceDir); os.IsNotExist(err) {
		err := os.MkdirAll(WorkspaceDir, 0755)
		if err != nil {
			fmt.Printf("Startup Error: Failed to create AI Workspace at %s: %v\n", WorkspaceDir, err)
		} else {
			fmt.Printf("Startup Success: Created AI Sandbox Workspace at %s\n", WorkspaceDir)
		}
	}

	// Ensure the workspace is a Git repository to enable the Time Machine / Checkpointing Checkpoint System
	cmd := exec.Command("git", "init")
	cmd.Dir = WorkspaceDir
	_ = cmd.Run()
}

// Security function to prevent path traversal (e.g. reading /etc/shadow)
func securePath(requestedPath string, projectRoot string) (string, error) {
	// If the LLM passes an absolute path inside the workspace, or a relative one like file.txt
	cleanRequested := filepath.Clean(requestedPath)

	// Determine the effective root (either the specific project or the global workspace)
	effectiveRoot := WorkspaceDir
	if projectRoot != "" {
		effectiveRoot = projectRoot
	}

	// If it doesn't already start with the effective root, prepend it
	if !strings.HasPrefix(cleanRequested, effectiveRoot) {
		cleanRequested = filepath.Join(effectiveRoot, cleanRequested)
	}

	// Final evaluation to ensure it didn't use ../../../ to escape
	cleanFinal := filepath.Clean(cleanRequested)

	// Must be within WorkspaceDir ALWAYS
	if !withinBase(cleanFinal, WorkspaceDir) {
		return "", fmt.Errorf("security violation: path traversal detected (%s)", requestedPath)
	}

	// If projectRoot is set, it MUST be within that too
	if projectRoot != "" && !withinBase(cleanFinal, projectRoot) {
		return "", fmt.Errorf("security violation: path outside assigned project root (%s)", requestedPath)
	}

	return cleanFinal, nil
}

// ----------------------------------------------------
// TIME MACHINE
// ----------------------------------------------------

func CreateCheckpoint(message string) string {
	// Add all changes to git stagings
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = WorkspaceDir
	_ = addCmd.Run()

	// Commit with the automated message
	commitCmd := exec.Command("git", "commit", "-m", "[AgentHQ Auto-Checkpoint] "+message)
	commitCmd.Dir = WorkspaceDir
	commitCmd.Run()

	revCmd := exec.Command("git", "rev-parse", "HEAD")
	revCmd.Dir = WorkspaceDir
	out, err := revCmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// ----------------------------------------------------
// IMPLEMENTATIONS
// ----------------------------------------------------

func executeListFiles(rawArgs string, projectRoot string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: Invalid arguments."
	}

	targetPath := args.Path
	if targetPath == "" {
		targetPath = projectRoot
		if targetPath == "" {
			targetPath = WorkspaceDir
		}
	}

	safePath, err := securePath(targetPath, projectRoot)
	if err != nil {
		return err.Error()
	}

	files, err := ioutil.ReadDir(safePath)
	if err != nil {
		return fmt.Sprintf("Error reading directory: %v", err)
	}

	if len(files) == 0 {
		return "Directory is empty."
	}

	var out strings.Builder
	for _, f := range files {
		kind := "FILE"
		if f.IsDir() {
			kind = "DIR "
		}
		out.WriteString(fmt.Sprintf("[%s] %s  (%d bytes)\n", kind, f.Name(), f.Size()))
	}
	return out.String()
}

func executeReadFile(rawArgs string, projectRoot string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Path == "" {
		return "Error: Invalid arguments. 'path' is required."
	}

	safePath, err := securePath(args.Path, projectRoot)
	if err != nil {
		return err.Error()
	}

	data, err := ioutil.ReadFile(safePath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	// Truncate to prevent massive tokens
	content := string(data)
	if len(content) > 15000 {
		content = content[:15000] + "\n\n... [FILE TRUNCATED FOR LENGTH LIMITS]"
	}

	return content
}

func executeWriteFile(rawArgs string, projectRoot string) string {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Path == "" {
		return "Error: Invalid arguments. 'path' and 'content' are required."
	}

	safePath, err := securePath(args.Path, projectRoot)
	if err != nil {
		return err.Error()
	}

	// Ensure parent dir exists
	parent := filepath.Dir(safePath)
	os.MkdirAll(parent, 0755)

	err = ioutil.WriteFile(safePath, []byte(args.Content), 0644)
	if err != nil {
		return fmt.Sprintf("Error writing file: %v", err)
	}

	return fmt.Sprintf("Success: Wrote %d bytes to %s", len(args.Content), args.Path)
}

func executeEditFile(rawArgs string, projectRoot string) string {
	var args struct {
		Path    string `json:"path"`
		Search  string `json:"search"`
		Replace string `json:"replace"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Path == "" || args.Search == "" {
		return "Error: Invalid arguments. 'path', 'search', and 'replace' are required."
	}

	safePath, err := securePath(args.Path, projectRoot)
	if err != nil {
		return err.Error()
	}

	data, err := ioutil.ReadFile(safePath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	content := string(data)

	occurrences := strings.Count(content, args.Search)
	if occurrences == 0 {
		return "Error: The exact search string was not found in the file."
	} else if occurrences > 1 {
		return "Error: The search string appears multiple times. Please provide a larger unique search block to ensure the correct code is replaced."
	}

	newContent := strings.Replace(content, args.Search, args.Replace, 1)

	if err := os.WriteFile(safePath, []byte(newContent), 0644); err != nil {
		return fmt.Sprintf("Error writing modified file: %v", err)
	}

	return fmt.Sprintf("Success: Replaced block in %s.", safePath)
}

func executeAddWorkspaceProject(rawArgs string, projectRoot string) string {
	var args struct {
		Name          string `json:"name"`
		Path          string `json:"path"`
		DeployCommand string `json:"deploy_command"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "Error: Invalid arguments."
	}

	if args.Name == "" || args.Path == "" {
		return "Error: name and path are required."
	}

	safePath, err := securePath(args.Path, projectRoot)
	if err != nil {
		return err.Error()
	}

	// Make sure the directory exists natively
	if _, err := os.Stat(safePath); os.IsNotExist(err) {
		if err := os.MkdirAll(safePath, 0755); err != nil {
			return fmt.Sprintf("Error creating project directory: %v", err)
		}
	}

	// Link to internal ManagedProjects settings DB so it appears in the File Explorer
	var val string
	err = db.DB.QueryRow("SELECT value FROM settings WHERE key = 'managed_projects'").Scan(&val)
	if err != nil {
		val = "[]"
	}

	var projects []map[string]interface{}
	json.Unmarshal([]byte(val), &projects)

	// Check if already mounted
	for _, p := range projects {
		if p["path"] == safePath {
			return fmt.Sprintf("Project at %s is already managed under name '%s'.", safePath, p["name"])
		}
	}

	deployCmd := args.DeployCommand
	if deployCmd == "" {
		deployCmd = "echo 'No deploy command configured'"
	}

	projects = append(projects, map[string]interface{}{
		"name":           args.Name,
		"path":           safePath,
		"deploy_command": deployCmd,
	})

	bytes, _ := json.Marshal(projects)
	db.DB.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES ('managed_projects', ?)", string(bytes))

	return fmt.Sprintf("Success: Project '%s' mapped to workspace %s and is now visible in the File Explorer.", args.Name, safePath)
}

func executeGrepSearch(rawArgs string, projectRoot string) string {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Pattern == "" {
		return "Error: Invalid arguments. 'pattern' is required."
	}

	targetPath := args.Path
	if targetPath == "" {
		targetPath = projectRoot
		if targetPath == "" {
			targetPath = WorkspaceDir // Default to workspace root if empty
		}
	}

	safePath, err := securePath(targetPath, projectRoot)
	if err != nil {
		return err.Error()
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return fmt.Sprintf("Error: Invalid regex pattern: %v", err)
	}

	var results []string
	matches := 0
	MAX_MATCHES := 100

	filepath.Walk(safePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip hidden files to save time
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}

		data, err := ioutil.ReadFile(path)
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if re.MatchString(line) {
					relPath := strings.TrimPrefix(path, WorkspaceDir)
					if !strings.HasPrefix(relPath, "/") {
						relPath = "/" + relPath
					}
					results = append(results, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(line)))
					matches++
					if matches >= MAX_MATCHES {
						return fmt.Errorf("max_matches")
					}
				}
			}
		}
		return nil
	})

	if len(results) == 0 {
		return "No matches found."
	}

	out := strings.Join(results, "\n")
	if matches >= MAX_MATCHES {
		out += "\n\n... [ADDITIONAL MATCHES TRUNCATED]"
	}

	return out
}

// executeFindFiles searches for files by glob pattern within the workspace
func executeFindFiles(rawArgs string, projectRoot string) string {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Pattern == "" {
		return "Error: 'pattern' is required (e.g. '*.go', 'test_*.py')."
	}

	searchPath := args.Path
	if searchPath == "" {
		searchPath = projectRoot
		if searchPath == "" {
			searchPath = WorkspaceDir
		}
	}
	safePath, err := securePath(searchPath, projectRoot)
	if err != nil {
		return err.Error()
	}

	cmd := exec.Command("find", safePath, "-name", args.Pattern, "-not", "-path", "*/.*", "-not", "-path", "*/node_modules/*", "-not", "-path", "*/vendor/*")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("Error running find: %v", err)
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return fmt.Sprintf("No files matching '%s' found in %s", args.Pattern, searchPath)
	}
	// Strip the workspace prefix for readability
	lines := strings.Split(result, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, WorkspaceDir+"/")
	}
	return strings.Join(lines, "\n")
}

// executeRenameFile renames or moves a file/directory within the workspace
func executeRenameFile(rawArgs string, projectRoot string) string {
	var args struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.From == "" || args.To == "" {
		return "Error: 'from' and 'to' paths are required."
	}

	fromPath, err := securePath(args.From, projectRoot)
	if err != nil {
		return fmt.Sprintf("Error (from): %v", err)
	}
	toPath, err := securePath(args.To, projectRoot)
	if err != nil {
		return fmt.Sprintf("Error (to): %v", err)
	}

	if _, err := os.Stat(fromPath); os.IsNotExist(err) {
		return fmt.Sprintf("Error: Source path '%s' does not exist.", args.From)
	}

	// Ensure destination parent dir exists
	os.MkdirAll(filepath.Dir(toPath), 0755)

	if err := os.Rename(fromPath, toPath); err != nil {
		return fmt.Sprintf("Error renaming: %v", err)
	}
	return fmt.Sprintf("Success: Renamed '%s' → '%s'", args.From, args.To)
}

// executeCheckCode runs language-appropriate linting/type-checking on a file or directory
func executeCheckCode(rawArgs string, projectRoot string) string {
	var args struct {
		Path     string `json:"path"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Path == "" {
		return "Error: 'path' is required."
	}

	safePath, err := securePath(args.Path, projectRoot)
	if err != nil {
		return err.Error()
	}

	lang := strings.ToLower(args.Language)
	// Auto-detect from extension if not specified
	if lang == "" {
		ext := strings.ToLower(filepath.Ext(args.Path))
		switch ext {
		case ".go":
			lang = "go"
		case ".ts", ".tsx":
			lang = "typescript"
		case ".js", ".jsx":
			lang = "javascript"
		case ".py":
			lang = "python"
		case ".rs":
			lang = "rust"
		}
	}

	var cmd *exec.Cmd
	switch lang {
	case "go":
		// Run go vet on the directory containing the file
		dir := safePath
		if info, _ := os.Stat(safePath); info != nil && !info.IsDir() {
			dir = filepath.Dir(safePath)
		}
		cmd = exec.Command("go", "vet", "./...")
		cmd.Dir = dir
	case "typescript":
		cmd = exec.Command("npx", "tsc", "--noEmit", "--pretty")
		cmd.Dir = WorkspaceDir
	case "javascript":
		cmd = exec.Command("npx", "eslint", safePath, "--format", "compact")
		cmd.Dir = WorkspaceDir
	case "python":
		cmd = exec.Command("python3", "-m", "flake8", safePath, "--max-line-length", "120")
	case "rust":
		cmd = exec.Command("cargo", "check", "--message-format=short")
		cmd.Dir = WorkspaceDir
	default:
		return fmt.Sprintf("Error: Cannot automatically detect language for '%s'. Pass language='go'|'typescript'|'javascript'|'python'|'rust'.", args.Path)
	}

	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err == nil {
		if result == "" {
			return fmt.Sprintf("✓ No issues found in '%s'", args.Path)
		}
		return result
	}
	if result == "" {
		return fmt.Sprintf("Check failed: %v", err)
	}
	return result
}
