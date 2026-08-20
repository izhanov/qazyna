package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseXMLString(t *testing.T, content string) []Chunk {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.xml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := XML{}.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

func TestXMLSmallDocIsOneChunk(t *testing.T) {
	chunks := parseXMLString(t, `<catalog>
		<book id="42">
			<title>Домик в деревне</title>
			<author>Иванов</author>
		</book>
	</catalog>`)

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.Section != "catalog" {
		t.Errorf("section = %q, want %q", c.Section, "catalog")
	}
	for _, want := range []string{"book id: 42", "title: Домик в деревне", "author: Иванов"} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, c.Text)
		}
	}
	if strings.ContainsAny(c.Text, "<>") {
		t.Errorf("markup leaked into text:\n%s", c.Text)
	}
}

func TestXMLBigDocSplitsIntoSubtrees(t *testing.T) {
	long := strings.Repeat("слово ", 200) // ~1200 chars per book
	chunks := parseXMLString(t, `<catalog>
		<book><title>Первая</title><description>`+long+`</description></book>
		<book><title>Вторая</title><description>`+long+`</description></book>
	</catalog>`)

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (one per book): %+v", len(chunks), len(chunks))
	}
	for i, c := range chunks {
		if c.Section != "catalog > book" {
			t.Errorf("chunk %d: section = %q, want %q", i, c.Section, "catalog > book")
		}
	}
	if !strings.Contains(chunks[0].Text, "title: Первая") || !strings.Contains(chunks[1].Text, "title: Вторая") {
		t.Errorf("books mixed up: %q / %q", chunks[0].Text[:50], chunks[1].Text[:50])
	}
}

func TestXMLEmptyDoc(t *testing.T) {
	if chunks := parseXMLString(t, `<root></root>`); len(chunks) != 0 {
		t.Fatalf("got %d chunks for empty doc, want 0", len(chunks))
	}
}

// The decoder runs non-strict on purpose: one sloppy XML file must not
// abort a whole indexing run, mismatched tags are auto-closed instead.
func TestXMLMalformedTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.xml")
	if err := os.WriteFile(path, []byte("<a><b>text</a>"), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := (XML{}).Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("non-strict parser errored: %v", err)
	}
	if len(chunks) != 1 || !strings.Contains(chunks[0].Text, "text") {
		t.Errorf("chunks = %+v, want the text preserved", chunks)
	}
}
