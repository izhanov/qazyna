package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseCSVString(t *testing.T, name, content string) []Chunk {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := CSV{}.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

func TestCSVRowsWithHeaders(t *testing.T) {
	chunks := parseCSVString(t, "people.csv", "name,role,city\nАйгерим,engineer,Almaty\nBob,designer,Berlin\n")

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1 (small rows packed together): %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.Section != "rows 2-3" {
		t.Errorf("section = %q, want %q", c.Section, "rows 2-3")
	}
	for _, want := range []string{"name: Айгерим", "role: engineer", "name: Bob", "city: Berlin"} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, c.Text)
		}
	}
}

func TestCSVPacksRowsUpToLimit(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("id,text\n")
	for i := range 20 {
		sb.WriteString(fmt.Sprintf("%d,%s\n", i, strings.Repeat("слово ", 60))) // ~360 chars per row
	}
	chunks := parseCSVString(t, "big.csv", sb.String())

	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want several: rows must not all fit one chunk", len(chunks))
	}
	for i, c := range chunks {
		if n := len([]rune(c.Text)); n > maxChunkChars {
			t.Errorf("chunk %d exceeds max size: %d runes", i, n)
		}
		if !strings.HasPrefix(c.Section, "row") {
			t.Errorf("chunk %d: section = %q", i, c.Section)
		}
	}
}

func TestCSVSniffsSemicolonAndTab(t *testing.T) {
	chunks := parseCSVString(t, "semi.csv", "name;city\nАйгерим;Almaty\n")
	if len(chunks) != 1 || !strings.Contains(chunks[0].Text, "city: Almaty") {
		t.Fatalf("semicolon file parsed wrong: %+v", chunks)
	}

	chunks = parseCSVString(t, "tabs.tsv", "name\tcity\nБолат\tAstana\n")
	if len(chunks) != 1 || !strings.Contains(chunks[0].Text, "city: Astana") {
		t.Fatalf("tsv file parsed wrong: %+v", chunks)
	}
}

func TestCSVRaggedRow(t *testing.T) {
	chunks := parseCSVString(t, "ragged.csv", "a,b\n1,2,3\n")
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Text, "col 3: 3") {
		t.Errorf("extra field lost its fallback name:\n%s", chunks[0].Text)
	}
}

func TestCSVEmptyFile(t *testing.T) {
	if chunks := parseCSVString(t, "empty.csv", ""); len(chunks) != 0 {
		t.Fatalf("got %d chunks for empty file, want 0", len(chunks))
	}
}
