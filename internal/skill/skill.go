// Package skill ships the Claude Code skill that teaches AI agents to use
// qazyna search in their work loop. The skill is embedded in the binary so
// `qazyna skill install` works without a copy of the repository.
package skill

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed SKILL.md
var Content []byte

// Name is the skill directory name under ~/.claude/skills/.
const Name = "qazyna-search"

// Path returns where the skill file lives for the current user.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "skills", Name, "SKILL.md"), nil
}

// Install writes the embedded skill into the user's Claude Code skills
// directory, overwriting any previous version, and returns the path.
func Install() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, Content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Uninstall removes the installed skill directory. Missing is not an error.
func Uninstall() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return dir, nil
}
