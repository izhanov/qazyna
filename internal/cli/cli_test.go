package cli

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"qazyna/internal/config"
	"qazyna/internal/parser"
	"qazyna/internal/store"
)

type fakeParser struct{}

func (fakeParser) Extensions() []string { return []string{".fake"} }

func (fakeParser) Parse(_ context.Context, _ string) ([]parser.Chunk, error) {
	return []parser.Chunk{{Text: "hi"}}, nil
}

type failParser struct{}

func (failParser) Extensions() []string { return []string{".bad"} }

func (failParser) Parse(_ context.Context, _ string) ([]parser.Chunk, error) {
	return nil, errors.New("boom")
}

type fakeStore struct {
	mu       sync.Mutex
	closed   bool
	docs     []store.Document
	meta     map[string]string
	resetted bool
	deleted  []string

	searchVec     []float32
	searchLimit   int
	searchResults []store.SearchResult

	textQuery   string
	textResults []store.SearchResult
}

func (f *fakeStore) Search(_ context.Context, vec []float32, limit int) ([]store.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searchVec = vec
	f.searchLimit = limit
	return f.searchResults, nil
}

func (f *fakeStore) SearchText(_ context.Context, query string, _ int) ([]store.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.textQuery = query
	return f.textResults, nil
}

func (f *fakeStore) AddDocument(_ context.Context, doc store.Document) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs = append(f.docs, doc)
	return nil
}

func (f *fakeStore) DeleteByPath(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, path)
	f.docs = slices.DeleteFunc(f.docs, func(d store.Document) bool { return d.Path == path })
	return nil
}

func (f *fakeStore) Paths(_ context.Context) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := map[string]int64{}
	for _, d := range f.docs {
		paths[d.Path] = d.MTime
	}
	return paths, nil
}

func (f *fakeStore) Count(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, d := range f.docs {
		n += int64(len(d.Chunks))
	}
	return n, nil
}

func (f *fakeStore) Meta(_ context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.meta == nil {
		return map[string]string{}, nil
	}
	return f.meta, nil
}

func (f *fakeStore) SetMeta(_ context.Context, meta map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.meta = meta
	return nil
}

func (f *fakeStore) Reset(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs = nil
	f.meta = nil
	f.resetted = true
	return nil
}

func (f *fakeStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// withFakeStore registers a "fake" store backend that captures the config
// it was built with and records everything written to it.
func withFakeStore(got **config.Config, st *fakeStore) Option {
	return WithStore("fake", func(_ context.Context, cfg *config.Config) (store.Store, error) {
		*got = cfg
		return st, nil
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunIndex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# Title\n\nhello\n")

	var got *config.Config
	st := &fakeStore{}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "--db", "custom.lance", "index", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("store factory was not called")
	}
	if got.DBPath != "custom.lance" {
		t.Errorf("DBPath = %q, want %q", got.DBPath, "custom.lance")
	}
	if !st.closed {
		t.Error("store was not closed")
	}

	if len(st.docs) != 1 {
		t.Fatalf("stored %d documents, want 1", len(st.docs))
	}
	doc := st.docs[0]
	if doc.Path != filepath.Join(dir, "a.md") {
		t.Errorf("doc path = %q", doc.Path)
	}
	if len(doc.Chunks) != 1 || len(doc.Vectors) != 1 {
		t.Fatalf("doc has %d chunks and %d vectors, want 1 and 1", len(doc.Chunks), len(doc.Vectors))
	}
	if doc.Chunks[0].Text != "hello" {
		t.Errorf("chunk text = %q, want %q", doc.Chunks[0].Text, "hello")
	}
	if doc.MTime == 0 {
		t.Error("doc mtime is not set")
	}
}

func TestRunIndexUnknownStore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "hello\n")

	err := Run([]string{"qazyna", "--store", "bogus", "index", dir}, WithDefaultParsers())
	if err == nil || !strings.Contains(err.Error(), `unknown store "bogus"`) {
		t.Fatalf("err = %v, want unknown store error", err)
	}
}

func TestRunIndexRequiresPath(t *testing.T) {
	err := Run([]string{"qazyna", "index"})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestRunIndexNoSupportedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.png"), "not really a png")

	var got *config.Config
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, &fakeStore{}),
	)
	if err == nil || !strings.Contains(err.Error(), "no supported files") {
		t.Fatalf("err = %v, want no supported files error", err)
	}
}

func TestRunIndexStampsEmbedderMeta(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "hello\n")

	var got *config.Config
	st := &fakeStore{}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.meta["embedder"] != "fake/32" {
		t.Errorf(`meta["embedder"] = %q, want "fake/32"`, st.meta["embedder"])
	}
}

func TestRunIndexRefusesForeignEmbedder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "hello\n")

	var got *config.Config
	st := &fakeStore{meta: map[string]string{"embedder": "ollama/bge-m3"}}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err == nil || !strings.Contains(err.Error(), "reindex") {
		t.Fatalf("err = %v, want embedder mismatch error suggesting reindex", err)
	}
	if len(st.docs) != 0 {
		t.Errorf("stored %d documents into a foreign-embedder database", len(st.docs))
	}
}

func TestRunReindexResetsForeignDatabase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "hello\n")

	var got *config.Config
	st := &fakeStore{
		meta: map[string]string{"embedder": "ollama/bge-m3"},
		docs: []store.Document{{Path: "stale.md"}},
	}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "reindex", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !st.resetted {
		t.Error("reindex did not reset the store")
	}
	if st.meta["embedder"] != "fake/32" {
		t.Errorf(`meta["embedder"] = %q, want "fake/32"`, st.meta["embedder"])
	}
	if len(st.docs) != 1 || st.docs[0].Path == "stale.md" {
		t.Errorf("stale documents survived reindex: %+v", st.docs)
	}
}

func TestRunIndexUnknownEmbedder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "hello\n")

	var got *config.Config
	err := Run(
		[]string{"qazyna", "--store", "fake", "index", dir}, // default --embedder ollama not registered
		WithDefaultParsers(),
		withFakeStore(&got, &fakeStore{}),
	)
	if err == nil || !strings.Contains(err.Error(), `unknown embedder "ollama"`) {
		t.Fatalf("err = %v, want unknown embedder error", err)
	}
}

func TestRunIndexReportsFailedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.bad"), "whatever")

	var got *config.Config
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithParser(failParser{}),
		WithFakeEmbedder(),
		withFakeStore(&got, &fakeStore{}),
	)
	if err == nil || !strings.Contains(err.Error(), "1 of 1 files failed") {
		t.Fatalf("err = %v, want failure summary", err)
	}
}

// One broken file must not stop the rest of the corpus from being indexed.
func TestRunIndexContinuesAfterFailedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "good.md"), "# ok\n\nhello\n")
	writeFile(t, filepath.Join(dir, "broken.bad"), "whatever")

	var got *config.Config
	st := &fakeStore{}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithDefaultParsers(),
		WithParser(failParser{}),
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 files failed") {
		t.Fatalf("err = %v, want partial failure summary", err)
	}
	if len(st.docs) != 1 || !strings.HasSuffix(st.docs[0].Path, "good.md") {
		t.Errorf("healthy file was not indexed: %+v", st.docs)
	}
}

func TestRunSearchRequiresQuery(t *testing.T) {
	err := Run([]string{"qazyna", "search"})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestRunSearchEmptyDatabase(t *testing.T) {
	var got *config.Config
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "search", "anything"},
		WithFakeEmbedder(),
		withFakeStore(&got, &fakeStore{}),
	)
	if err == nil || !strings.Contains(err.Error(), "database is empty") {
		t.Fatalf("err = %v, want empty database error", err)
	}
}

func TestRunSearchEmbedderMismatch(t *testing.T) {
	var got *config.Config
	st := &fakeStore{meta: map[string]string{"embedder": "ollama/bge-m3"}}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "search", "anything"},
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err == nil || !strings.Contains(err.Error(), `indexed with embedder "ollama/bge-m3"`) {
		t.Fatalf("err = %v, want embedder mismatch error", err)
	}
}

func TestRunSearch(t *testing.T) {
	var got *config.Config
	st := &fakeStore{
		meta: map[string]string{"embedder": "fake/32"},
		searchResults: []store.SearchResult{
			{Path: "a.md", Section: "S", Text: "hello", Score: 0.9},
		},
	}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "search", "--mode", "vector", "--limit", "3", "привет", "мир"},
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.searchLimit != 3 {
		t.Errorf("search limit = %d, want 3", st.searchLimit)
	}
	if len(st.searchVec) != 32 {
		t.Errorf("query vector length = %d, want 32", len(st.searchVec))
	}
	var norm float64
	for _, x := range st.searchVec {
		norm += float64(x) * float64(x)
	}
	if norm < 0.99 || norm > 1.01 {
		t.Errorf("query vector norm² = %f, want ~1", norm)
	}
}

func TestRunSearchTextModeNeedsNoEmbedder(t *testing.T) {
	var got *config.Config
	// Meta names a foreign embedder and no "ollama" embedder is registered
	// at all: text mode must not care about either.
	st := &fakeStore{
		meta:        map[string]string{"embedder": "ollama/bge-m3"},
		textResults: []store.SearchResult{{Path: "a.md", Text: "лениво"}},
	}
	err := Run(
		[]string{"qazyna", "--store", "fake", "search", "--mode", "text", "лениво"},
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.textQuery != "лениво" {
		t.Errorf("text query = %q, want %q", st.textQuery, "лениво")
	}
	if st.searchVec != nil {
		t.Error("vector search ran in text mode")
	}
}

func TestRunSearchHybrid(t *testing.T) {
	var got *config.Config
	st := &fakeStore{
		meta: map[string]string{"embedder": "fake/32"},
		searchResults: []store.SearchResult{
			{Path: "a.md", Ordinal: 1, Text: "semantic hit"},
			{Path: "b.md", Ordinal: 2, Text: "both"},
		},
		textResults: []store.SearchResult{
			{Path: "b.md", Ordinal: 2, Text: "both"},
			{Path: "c.md", Ordinal: 3, Text: "keyword hit"},
		},
	}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "search", "query"},
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if st.textQuery == "" || st.searchVec == nil {
		t.Fatal("hybrid mode must run both searches")
	}
	if st.searchLimit != 15 { // limit 5 × 3 for fusion room
		t.Errorf("vector fetch limit = %d, want 15", st.searchLimit)
	}
}

func TestRunSearchUnknownMode(t *testing.T) {
	var got *config.Config
	st := &fakeStore{meta: map[string]string{"embedder": "fake/32"}}
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "search", "--mode", "telepathy", "query"},
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err == nil || !strings.Contains(err.Error(), `unknown mode "telepathy"`) {
		t.Fatalf("err = %v, want unknown mode error", err)
	}
}

func TestRunFlushForce(t *testing.T) {
	var got *config.Config
	st := &fakeStore{
		docs: []store.Document{{Path: "a.md", Chunks: []parser.Chunk{{Text: "x"}}}},
		meta: map[string]string{"embedder": "fake/32"},
	}
	err := Run(
		[]string{"qazyna", "--store", "fake", "flush", "--force"},
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !st.resetted {
		t.Error("flush --force did not reset the store")
	}
}

func TestRunFlushRefusesWithoutTerminal(t *testing.T) {
	var got *config.Config
	st := &fakeStore{
		docs: []store.Document{{Path: "a.md", Chunks: []parser.Chunk{{Text: "x"}}}},
	}
	// Test stdin is not a terminal, so without --force it must refuse.
	err := Run(
		[]string{"qazyna", "--store", "fake", "flush"},
		withFakeStore(&got, st),
	)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want refusal hinting at --force", err)
	}
	if st.resetted {
		t.Error("store was reset without confirmation")
	}
}

func TestRunFlushEmptyDatabase(t *testing.T) {
	var got *config.Config
	st := &fakeStore{}
	err := Run(
		[]string{"qazyna", "--store", "fake", "flush"},
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatalf("flush on empty database must succeed, got %v", err)
	}
	if st.resetted {
		t.Error("empty database was reset needlessly")
	}
}

// runIndexArgs runs `index dir` against st with the fake backends.
func runIndexArgs(t *testing.T, st *fakeStore, dir string) error {
	t.Helper()
	var got *config.Config
	return Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
}

func TestRunIndexSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "hello\n")

	st := &fakeStore{}
	if err := runIndexArgs(t, st, dir); err != nil {
		t.Fatal(err)
	}
	if len(st.docs) != 1 {
		t.Fatalf("first run stored %d documents, want 1", len(st.docs))
	}

	// Second run: file untouched, must be skipped — no new AddDocument.
	if err := runIndexArgs(t, st, dir); err != nil {
		t.Fatal(err)
	}
	if len(st.docs) != 1 {
		t.Errorf("unchanged file was re-indexed: %d documents", len(st.docs))
	}
}

func TestRunIndexReindexesChangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	writeFile(t, path, "hello\n")

	st := &fakeStore{}
	if err := runIndexArgs(t, st, dir); err != nil {
		t.Fatal(err)
	}

	// Change content and push mtime into the future (writes within the
	// same second would otherwise be invisible to a second-granular check).
	writeFile(t, path, "changed\n")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	if err := runIndexArgs(t, st, dir); err != nil {
		t.Fatal(err)
	}
	last := st.docs[len(st.docs)-1]
	if last.Chunks[0].Text != "changed" {
		t.Errorf("changed file was not re-indexed: %q", last.Chunks[0].Text)
	}
}

func TestRunIndexRemovesVanishedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.md"), "keep\n")

	st := &fakeStore{
		docs: []store.Document{
			{Path: filepath.Join(dir, "gone.md"), MTime: 1, Chunks: []parser.Chunk{{Text: "x"}}},
			{Path: "/elsewhere/other.md", MTime: 1, Chunks: []parser.Chunk{{Text: "y"}}},
		},
		meta: map[string]string{"embedder": "fake/32"},
	}
	if err := runIndexArgs(t, st, dir); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(st.deleted, filepath.Join(dir, "gone.md")) {
		t.Error("vanished file under the root was not removed")
	}
	if slices.Contains(st.deleted, "/elsewhere/other.md") {
		t.Error("file outside the indexed roots was removed")
	}
}

func TestUnderAnyRoot(t *testing.T) {
	roots := []string{"/a/b", "/c"}
	for path, want := range map[string]bool{
		"/a/b/x.md":  true,
		"/a/b":       true,
		"/a/bc/x.md": false, // prefix of the name, not of the directory
		"/c/deep/y":  true,
		"/d/z.md":    false,
	} {
		if got := underAnyRoot(path, roots); got != want {
			t.Errorf("underAnyRoot(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestOpenStoreUnknownListsRegistered(t *testing.T) {
	app := &App{stores: map[string]storeFactory{}}
	var got *config.Config
	if err := withFakeStore(&got, &fakeStore{})(app); err != nil {
		t.Fatal(err)
	}

	_, err := app.openStore(context.Background(), &config.Config{StoreName: "nope"})
	if err == nil || !strings.Contains(err.Error(), "[fake]") {
		t.Fatalf("err = %v, want registered backends listed", err)
	}
}

func TestCollectFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "a")
	writeFile(t, filepath.Join(dir, "b.txt"), "b")
	writeFile(t, filepath.Join(dir, ".git", "c.md"), "c")
	writeFile(t, filepath.Join(dir, "sub", "d.md"), "d")

	app := &App{parsers: parser.NewRegistry()}
	app.parsers.Register(parser.Markdown{})

	files, err := app.collectFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "a.md"),
		filepath.Join(dir, "sub", "d.md"),
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, files[i], want[i])
		}
	}
}

func TestRunIndexMultiplePaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes", "a.md"), "alpha\n")
	writeFile(t, filepath.Join(dir, "docs", "b.md"), "beta\n")
	single := filepath.Join(dir, "c.md")
	writeFile(t, single, "gamma\n")

	var got *config.Config
	st := &fakeStore{}
	err := Run(
		[]string{
			"qazyna", "--store", "fake", "--embedder", "fake", "index",
			filepath.Join(dir, "notes"), filepath.Join(dir, "docs"), single,
		},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, st),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.docs) != 3 {
		t.Fatalf("stored %d documents, want 3: %+v", len(st.docs), st.docs)
	}
}

func TestCollectFilesDeduplicatesOverlappingRoots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	writeFile(t, path, "a")

	app := &App{parsers: parser.NewRegistry()}
	app.parsers.Register(parser.Markdown{})

	// The same file is reachable three ways: via the dir, directly, and
	// via an unclean path. It must be indexed exactly once.
	files, err := app.collectFiles(dir, path, dir+"//a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != path {
		t.Fatalf("files = %v, want exactly [%s]", files, path)
	}
}

func TestLogLevel(t *testing.T) {
	cases := []struct {
		quiet, verbose bool
		want           slog.Level
	}{
		{false, false, slog.LevelInfo},
		{false, true, slog.LevelDebug},
		{true, false, slog.LevelError},
		{true, true, slog.LevelError}, // quiet wins
	}
	for _, c := range cases {
		if got := logLevel(c.quiet, c.verbose); got != c.want {
			t.Errorf("logLevel(quiet=%v, verbose=%v) = %v, want %v", c.quiet, c.verbose, got, c.want)
		}
	}
}

func TestPrettyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := prettyPath(filepath.Join(home, "notes", "a.md")); got != "~/notes/a.md" {
		t.Errorf("home path = %q, want ~/notes/a.md", got)
	}
	if got := prettyPath("/somewhere/else/a.md"); got != "/somewhere/else/a.md" {
		t.Errorf("foreign path = %q, want unchanged", got)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := prettyPath(filepath.Join(cwd, "sub", "b.md")); got != filepath.Join("sub", "b.md") {
		t.Errorf("cwd path = %q, want sub/b.md", got)
	}
}

func TestSnippet(t *testing.T) {
	if got := snippet("a\nb\n\n  c", 100); got != "a b c" {
		t.Errorf("snippet = %q, want %q", got, "a b c")
	}
	long := strings.Repeat("ы", 250)
	got := snippet(long, 200)
	if n := len([]rune(got)); n != 201 { // 200 runes + ellipsis
		t.Errorf("snippet length = %d runes, want 201", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("long snippet is not ellipsized")
	}
}

func TestCollectFilesSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	writeFile(t, path, "a")

	app := &App{parsers: parser.NewRegistry()}
	app.parsers.Register(parser.Markdown{})

	files, err := app.collectFiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != path {
		t.Fatalf("files = %v, want [%s]", files, path)
	}
}
