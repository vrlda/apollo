package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var SkillsDir = filepath.Join(WorkspaceDir, ".skills")

func resolveBundledSkillsDir() string {
	candidates := []string{
		"skills",              // run from backend/
		"backend/skills",      // run from repo root
		"dash/backend/skills", // run from workspace root containing dash/
	}

	if exePath, err := os.Executable(); err == nil && exePath != "" {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "skills"),
			filepath.Join(exeDir, "..", "skills"),
			filepath.Join(exeDir, "..", "backend", "skills"),
		)
	}

	for _, d := range candidates {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return ""
}

func copySkillFile(srcPath string, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// SyncBundledSkills ensures bundled skills are present in the runtime workspace library.
// Existing runtime skill files are preserved and not overwritten.
func SyncBundledSkills() {
	_ = os.MkdirAll(SkillsDir, 0755)

	srcDir := resolveBundledSkillsDir()
	if srcDir == "" {
		return
	}
	if filepath.Clean(srcDir) == filepath.Clean(SkillsDir) {
		return
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		dstPath := filepath.Join(SkillsDir, e.Name())
		if info, err := os.Stat(dstPath); err == nil && !info.IsDir() {
			continue // keep user-updated runtime copy
		}
		_ = copySkillFile(filepath.Join(srcDir, e.Name()), dstPath)
	}
}

// resolveSkillsDir returns the first skills directory that actually exists.
// Priority: runtime workspace dir (auto-seeded from bundled skills) → local fallback.
func resolveSkillsDir() string {
	SyncBundledSkills()

	candidates := []string{
		SkillsDir,
		"skills", // relative to cwd — used when binary runs from backend/
	}
	for _, d := range candidates {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return SkillsDir // fallback — will return empty manifest
}

// GetSkillsManifest returns a compact list of skill names and descriptions for the system prompt
func GetSkillsManifest() string {
	dir := resolveSkillsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "" // Skills dir doesn't exist yet — silently skip
	}
	var lines []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		// Extract description from frontmatter (line starting with "description:")
		desc := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "description:") {
				desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
				break
			}
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		if desc != "" {
			lines = append(lines, fmt.Sprintf("- %s: %s", name, desc))
		} else {
			lines = append(lines, fmt.Sprintf("- %s", name))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "AVAILABLE SKILLS (call load_skill(name) to get full content when needed):\n" + strings.Join(lines, "\n")
}

// executeLoadSkill reads the full content of a skill file by name
func executeLoadSkill(rawArgs string) string {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Name == "" {
		return "Error: 'name' is required for load_skill."
	}

	name := strings.TrimSuffix(strings.TrimSpace(args.Name), ".md")
	dir := resolveSkillsDir()
	path := filepath.Join(dir, name+".md")

	data, err := os.ReadFile(path)
	if err != nil {
		entries, _ := os.ReadDir(dir)
		var available []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				available = append(available, strings.TrimSuffix(e.Name(), ".md"))
			}
		}
		if len(available) > 0 {
			return fmt.Sprintf("Skill '%s' not found. Available skills: %s", name, strings.Join(available, ", "))
		}
		return fmt.Sprintf("Skill '%s' not found. No skills are installed yet.", name)
	}

	return fmt.Sprintf("=== SKILL: %s ===\n\n%s", name, string(data))
}
