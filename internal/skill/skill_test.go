package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentHasFrontmatter(t *testing.T) {
	s := string(Content)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("SKILL.md must start with YAML frontmatter")
	}
	for _, want := range []string{"name: " + Name, "description:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("SKILL.md frontmatter missing %q", want)
		}
	}
}

func TestInstallAndUninstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := filepath.Join(os.Getenv("HOME"), ".claude", "skills", Name, "SKILL.md")
	if path != want {
		t.Fatalf("Install path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(got) != string(Content) {
		t.Fatal("installed skill differs from embedded content")
	}

	// Install is idempotent.
	if _, err := Install(); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	dir, err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("skill dir still exists after Uninstall: %v", err)
	}

	// Uninstalling twice is fine.
	if _, err := Uninstall(); err != nil {
		t.Fatalf("second Uninstall: %v", err)
	}
}
