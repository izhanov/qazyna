package store

import (
	"context"
	"path/filepath"
	"testing"

	"qazyna/internal/parser"
)

// Integration test against a real LanceDB in a temp directory.

func openTestStore(t *testing.T) Store {
	t.Helper()
	st, err := OpenLance(context.Background(), filepath.Join(t.TempDir(), "test.lance"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func doc(path string, texts ...string) Document {
	d := Document{Path: path, MTime: 1700000000}
	for i, text := range texts {
		d.Chunks = append(d.Chunks, parser.Chunk{Text: text, Section: "s", Ordinal: i})
		d.Vectors = append(d.Vectors, []float32{float32(i), 1, 2, 3})
	}
	return d
}

func TestLanceAddAndCount(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.AddDocument(ctx, doc("a.md", "one", "two")); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDocument(ctx, doc("b.md", "three")); err != nil {
		t.Fatal(err)
	}

	n, err := st.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("Count = %d, want 3", n)
	}
}

func TestLanceReindexDoesNotDuplicate(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	for range 3 {
		if err := st.AddDocument(ctx, doc("a.md", "one", "two")); err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("Count after re-indexing = %d, want 2", n)
	}
}

func TestLanceDeleteByPath(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.AddDocument(ctx, doc("a.md", "one")); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDocument(ctx, doc("b.md", "two")); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteByPath(ctx, "a.md"); err != nil {
		t.Fatal(err)
	}

	n, err := st.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("Count after delete = %d, want 1", n)
	}
}

func TestLanceEmptyStore(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	// No table exists yet: both must work, not fail.
	if err := st.DeleteByPath(ctx, "a.md"); err != nil {
		t.Fatal(err)
	}
	n, err := st.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("Count = %d, want 0", n)
	}
}

func TestLanceMetaRoundtrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	meta, err := st.Meta(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 0 {
		t.Fatalf("fresh store meta = %v, want empty", meta)
	}

	if err := st.SetMeta(ctx, map[string]string{"embedder": "ollama/bge-m3"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta(ctx, map[string]string{"embedder": "fake/32"}); err != nil {
		t.Fatal(err) // second write replaces, not appends
	}

	meta, err = st.Meta(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 1 || meta["embedder"] != "fake/32" {
		t.Errorf("meta = %v, want {embedder: fake/32}", meta)
	}
}

func TestLanceResetAllowsNewDimension(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.AddDocument(ctx, doc("a.md", "one")); err != nil { // dim 4
		t.Fatal(err)
	}
	if err := st.SetMeta(ctx, map[string]string{"embedder": "fake/4"}); err != nil {
		t.Fatal(err)
	}

	if err := st.Reset(ctx); err != nil {
		t.Fatal(err)
	}

	n, err := st.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("Count after reset = %d, want 0", n)
	}
	meta, err := st.Meta(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 0 {
		t.Errorf("meta after reset = %v, want empty", meta)
	}

	// A different vector dimension must be accepted after a reset.
	other := Document{
		Path:    "b.md",
		Chunks:  []parser.Chunk{{Text: "x"}},
		Vectors: [][]float32{{1, 2}}, // dim 2 instead of 4
	}
	if err := st.AddDocument(ctx, other); err != nil {
		t.Fatalf("AddDocument with new dimension after reset: %v", err)
	}
}

func TestLanceVectorDimensionMismatch(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.AddDocument(ctx, doc("a.md", "one")); err != nil {
		t.Fatal(err)
	}

	bad := Document{
		Path:    "b.md",
		Chunks:  []parser.Chunk{{Text: "x"}},
		Vectors: [][]float32{{1, 2}}, // dim 2 instead of 4
	}
	if err := st.AddDocument(ctx, bad); err == nil {
		t.Error("expected dimension mismatch error, got nil")
	}
}
