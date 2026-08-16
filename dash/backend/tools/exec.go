package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Executes a shell command strictly jailed inside the Workspace sandbox
func executeCommand(rawArgs string, projectRoot string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Command == "" {
		return "Error: Invalid arguments. 'command' is required."
	}

	// 15 Second Hard Timeout to prevent AI locking the server with frozen processes
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", args.Command)

	// Jail execution working directory to the project root if available, otherwise global sandbox
	workDir := WorkspaceDir
	if projectRoot != "" {
		workDir = projectRoot
	}
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	_ = cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("Error: Command timed out after 15 seconds.\nStdout: %s\nStderr: %s", stdout.String(), stderr.String())
	}

	result := fmt.Sprintf("Exit Code: %v\n\n--- Stdout ---\n%s\n--- Stderr ---\n%s",
		cmd.ProcessState.ExitCode(),
		stdout.String(),
		stderr.String(),
	)

	if len(result) > 15000 {
		return result[:15000] + "\n\n... [TERMINAL TRUNCATED FOR LENGTH]"
	}
	return result
}
