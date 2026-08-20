package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseString(t *testing.T, content string) []Chunk {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := Markdown{}.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

func TestParseHeadingSections(t *testing.T) {
	chunks := parseString(t, `intro text

# Install

first steps

## macOS

brew stuff

## Linux

apt stuff
`)

	want := []struct{ section, text string }{
		{"", "intro text"},
		{"Install", "first steps"},
		{"Install > macOS", "brew stuff"},
		{"Install > Linux", "apt stuff"},
	}
	if len(chunks) != len(want) {
		t.Fatalf("got %d chunks, want %d: %+v", len(chunks), len(want), chunks)
	}
	for i, w := range want {
		if chunks[i].Section != w.section {
			t.Errorf("chunk %d: section = %q, want %q", i, chunks[i].Section, w.section)
		}
		if chunks[i].Text != w.text {
			t.Errorf("chunk %d: text = %q, want %q", i, chunks[i].Text, w.text)
		}
		if chunks[i].Ordinal != i {
			t.Errorf("chunk %d: ordinal = %d, want %d", i, chunks[i].Ordinal, i)
		}
	}
}

func TestParseIgnoresHeadingsInCodeFence(t *testing.T) {
	chunks := parseString(t, "# Title\n\n```sh\n# not a heading\necho hi\n```\n")

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %+v", len(chunks), chunks)
	}
	if chunks[0].Section != "Title" {
		t.Errorf("section = %q, want %q", chunks[0].Section, "Title")
	}
	if !strings.Contains(chunks[0].Text, "# not a heading") {
		t.Errorf("code fence content lost: %q", chunks[0].Text)
	}
}

func TestParseSplitsLongSection(t *testing.T) {
	para := strings.Repeat("word ", 100) // ~500 chars
	content := "# Long\n\n" + strings.Repeat(para+"\n\n", 10)

	chunks := parseString(t, content)

	if len(chunks) < 2 {
		t.Fatalf("long section not split: got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if len(c.Text) > maxChunkChars {
			t.Errorf("chunk %d exceeds max size: %d chars", i, len(c.Text))
		}
		if c.Section != "Long" {
			t.Errorf("chunk %d: section = %q, want %q", i, c.Section, "Long")
		}
	}
}

func TestParseHardSplitsOversizedParagraph(t *testing.T) {
	content := "# Big\n\n" + strings.Repeat("а", 4000) // multi-byte runes on purpose

	chunks := parseString(t, content)

	if len(chunks) < 2 {
		t.Fatalf("oversized paragraph not split: got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if n := len([]rune(c.Text)); n > maxChunkChars {
			t.Errorf("chunk %d exceeds max size: %d runes", i, n)
		}
	}
}

func TestParseEmptyFile(t *testing.T) {
	if chunks := parseString(t, ""); len(chunks) != 0 {
		t.Fatalf("got %d chunks for empty file, want 0", len(chunks))
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(Markdown{})

	if _, ok := r.For("notes/readme.MD"); !ok {
		t.Error("expected .MD to be matched case-insensitively")
	}
	if _, ok := r.For("photo.png"); ok {
		t.Error("did not expect a parser for .png")
	}
}
