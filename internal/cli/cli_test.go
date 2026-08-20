package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
	mu     sync.Mutex
	closed bool
	docs   []store.Document
}

func (f *fakeStore) AddDocument(_ context.Context, doc store.Document) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs = append(f.docs, doc)
	return nil
}

func (f *fakeStore) DeleteByPath(_ context.Context, _ string) error { return nil }

func (f *fakeStore) Count(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, d := range f.docs {
		n += int64(len(d.Chunks))
	}
	return n, nil
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

func TestRunIndexPropagatesParseError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.bad"), "whatever")

	var got *config.Config
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithParser(failParser{}),
		WithFakeEmbedder(),
		withFakeStore(&got, &fakeStore{}),
	)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want parse error", err)
	}
}

func TestRunSearchNotImplemented(t *testing.T) {
	err := Run([]string{"qazyna", "search", "query"})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("err = %v, want not implemented error", err)
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
