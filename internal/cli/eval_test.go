package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"qazyna/internal/store"
)

func TestFirstExpectedRank(t *testing.T) {
	results := []store.SearchResult{
		{Path: "/home/notes/other.md"},
		{Path: "/home/skills/deploy/SKILL.md"},
		{Path: "/home/skills/deploy/SKILL.md"}, // second chunk, same file
	}

	rank, path := firstExpectedRank(results, []string{"deploy/SKILL.md"})
	if rank != 2 || path != "/home/skills/deploy/SKILL.md" {
		t.Errorf("rank = %d, path = %q; want 2, deploy/SKILL.md", rank, path)
	}
	if rank, _ := firstExpectedRank(results, []string{"missing.md"}); rank != 0 {
		t.Errorf("miss rank = %d, want 0", rank)
	}
	// eval.md must not match eval.en.md: suffix match is exact.
	if rank, _ := firstExpectedRank([]store.SearchResult{{Path: "/d/eval.en.md"}}, []string{"eval.md"}); rank != 0 {
		t.Error("eval.md matched eval.en.md")
	}
}

func TestGoldenFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.yaml", "a.yml", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := goldenFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.yml"), filepath.Join(dir, "b.yaml")}
	if !slices.Equal(files, want) {
		t.Errorf("goldenFiles = %v, want %v", files, want)
	}

	if _, err := goldenFiles(t.TempDir()); err == nil {
		t.Error("empty evals dir: expected error")
	}
}

func TestEditEvalsSeedsEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "evals")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true") // /usr/bin/true: exits immediately

	if err := editEvals(dir); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(dir, "golden.yaml")
	if _, err := os.Stat(seed); err != nil {
		t.Fatalf("starter file not created: %v", err)
	}
}

func TestEditEvalsNoEditor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "g.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	if err := editEvals(dir); err == nil {
		t.Fatal("expected error when $EDITOR is not set")
	}
}

func TestLoadGolden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden.yaml")
	good := "- query: \"q\"\n  expect: [\"a.md\"]\n"
	if err := os.WriteFile(path, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	cases, err := loadGolden(path)
	if err != nil || len(cases) != 1 || cases[0].Query != "q" {
		t.Fatalf("loadGolden = %+v, %v", cases, err)
	}

	for name, bad := range map[string]string{
		"empty":      "",
		"no expect":  "- query: \"q\"\n",
		"not a list": "query: q\n",
	} {
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadGolden(path); err == nil {
			t.Errorf("%s golden file: expected error", name)
		}
	}
}
