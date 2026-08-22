package store

import (
	"context"
	"path/filepath"
	"strings"
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

// The MCP-server scenario: a long-lived reader opened before another
// process wrote must still see the new data (a table handle is pinned to
// the version current at open time, so reads re-open it).
func TestLanceFreshReadsSeeOtherWriters(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.lance")

	reader, err := OpenLance(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })

	writer, err := OpenLance(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.AddDocument(ctx, doc("a.md", "written after reader opened")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	n, err := reader.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("Count = %d, want 1 (stale table handle?)", n)
	}
	res, err := reader.SearchText(ctx, "written", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Errorf("SearchText found %d results, want 1", len(res))
	}
}

// With fresh reads disabled the old behavior is kept: the handle stays
// pinned and concurrent writers are invisible until reopen.
func TestLanceStaleReadsWithFreshReadsOff(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.lance")

	// The table must exist before the reader opens, otherwise the reader
	// has no handle at all and sees nothing either way.
	writer, err := OpenLance(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.AddDocument(ctx, doc("a.md", "first")); err != nil {
		t.Fatal(err)
	}

	reader, err := OpenLance(ctx, path, WithFreshReads(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })

	if err := writer.AddDocument(ctx, doc("b.md", "second")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	n, err := reader.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("Count = %d, want 1 (pinned handle must not see b.md)", n)
	}
}

func TestLanceSearch(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	add := func(path, text string, vec []float32) {
		t.Helper()
		err := st.AddDocument(ctx, Document{
			Path:    path,
			Chunks:  []parser.Chunk{{Text: text, Section: "s", Ordinal: 7}},
			Vectors: [][]float32{vec},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("a.md", "alpha", []float32{1, 0, 0, 0})
	add("b.md", "beta", []float32{0, 1, 0, 0}) // orthogonal to the query

	results, err := st.Search(ctx, []float32{1, 0, 0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}

	best := results[0]
	if best.Path != "a.md" || best.Text != "alpha" || best.Section != "s" || best.Ordinal != 7 {
		t.Errorf("best result = %+v", best)
	}
	// Identical unit vectors → cosine 1; orthogonal → cosine 0. This pins
	// down the _distance→cosine conversion, not just the ordering.
	if best.Score < 0.99 || best.Score > 1.01 {
		t.Errorf("best score = %f, want ~1", best.Score)
	}
	if s := results[1].Score; s < -0.05 || s > 0.05 {
		t.Errorf("orthogonal score = %f, want ~0", s)
	}
}

func TestLanceFullTextSearch(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.AddDocument(ctx, doc("a.md", "Doorkeeper отзывает его лениво", "просто текст про хлеб")); err != nil {
		t.Fatal(err)
	}

	results, err := st.SearchText(ctx, "Лениво", 5) // case must not matter
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want exactly the chunk with the word: %+v", len(results), results)
	}
	if results[0].Path != "a.md" || !strings.Contains(results[0].Text, "лениво") {
		t.Errorf("result = %+v", results[0])
	}
}

func TestLanceSearchTextWholeWords(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.AddDocument(ctx, doc("a.md",
		"prepare the approach for printing", // "pr" only as a substring
		"push the branch and open draft PRs")); err != nil {
		t.Fatal(err)
	}

	results, err := st.SearchText(ctx, "PR", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !strings.Contains(results[0].Text, "PRs") {
		t.Fatalf("got %+v, want only the chunk with the whole word (PRs)", results)
	}
}

func TestWordSet(t *testing.T) {
	set := wordSet("Push the branch, open draft PRs (dry_run).")
	for _, want := range []string{"push", "branch", "draft", "pr", "dry_run"} {
		if !set[singular(want)] {
			t.Errorf("wordSet lacks %q: %v", want, set)
		}
	}
	if set["pr"] && set["prepare"] {
		t.Error("unexpected words in set")
	}

	if singular("prs") != "pr" || singular("is") != "is" || singular("pass") != "pas" {
		t.Errorf("singular: prs=%q is=%q pass=%q", singular("prs"), singular("is"), singular("pass"))
	}
}

func TestQueryWords(t *testing.T) {
	got := queryWords("Как горутины общаются между собой?")
	want := []string{"горутины", "общаются"}
	if len(got) != len(want) {
		t.Fatalf("queryWords = %v, want %v (stopwords must be dropped)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("queryWords[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := queryWords("и как между"); len(got) != 0 {
		t.Errorf("stopword-only query gave %v, want empty", got)
	}
	if got := queryWords("%dry_run%"); len(got) != 1 || got[0] != "dry_run" {
		// "%" stripped; "_" kept — it matches itself in LIKE patterns
		t.Errorf("wildcard query gave %v", got)
	}
}

func TestLanceSearchTextEmptyStore(t *testing.T) {
	st := openTestStore(t)

	results, err := st.SearchText(context.Background(), "anything", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results from empty store, want 0", len(results))
	}
}

func TestLanceSearchEmptyStore(t *testing.T) {
	st := openTestStore(t)

	results, err := st.Search(context.Background(), []float32{1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results from empty store, want 0", len(results))
	}
}

func TestLancePaths(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	paths, err := st.Paths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("fresh store paths = %v, want empty", paths)
	}

	if err := st.AddDocument(ctx, doc("a.md", "one", "two")); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDocument(ctx, doc("b.md", "three")); err != nil {
		t.Fatal(err)
	}

	paths, err = st.Paths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want 2 entries (deduplicated per file)", paths)
	}
	if paths["a.md"] != 1700000000 || paths["b.md"] != 1700000000 {
		t.Errorf("paths = %v, want stored mtimes", paths)
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
