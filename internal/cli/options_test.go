package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"qazyna/internal/config"
	"qazyna/internal/parser"
	"qazyna/internal/store"
)

func newTestApp() *App {
	return &App{
		stores:    map[string]storeFactory{},
		embedders: map[string]embedderFactory{},
		parsers:   parser.NewRegistry(),
	}
}

func TestWithStore(t *testing.T) {
	app := newTestApp()

	called := false
	opt := WithStore("mem", func(_ context.Context, _ *config.Config) (store.Store, error) {
		called = true
		return nil, nil
	})
	if err := opt(app); err != nil {
		t.Fatal(err)
	}

	f, ok := app.stores["mem"]
	if !ok {
		t.Fatal("factory not registered under its name")
	}
	if _, err := f(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("registered factory is not the provided one")
	}
}

func TestWithDefaultStore(t *testing.T) {
	app := newTestApp()
	if err := WithDefaultStore()(app); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.stores["lance"]; !ok {
		t.Error(`"lance" store not registered`)
	}
}

func TestWithParser(t *testing.T) {
	app := newTestApp()
	if err := WithParser(fakeParser{})(app); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.parsers.For("doc.fake"); !ok {
		t.Error("parser not registered for its extension")
	}
}

func TestWithDefaultParsers(t *testing.T) {
	app := newTestApp()
	if err := WithDefaultParsers()(app); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a.md", "b.markdown", "c.pdf", "d.xml", "e.csv", "f.tsv"} {
		if _, ok := app.parsers.For(path); !ok {
			t.Errorf("no parser registered for %s", path)
		}
	}
}

func TestWithEmbedder(t *testing.T) {
	app := newTestApp()
	if err := WithFakeEmbedder()(app); err != nil {
		t.Fatal(err)
	}
	f, ok := app.embedders["fake"]
	if !ok {
		t.Fatal(`"fake" embedder not registered`)
	}
	e, err := f(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID() == "" {
		t.Error("embedder ID is empty")
	}
}

// newConfig is what turns CLI flags into config; check defaults end-to-end
// through Run with the fake store capturing the config it received.
func TestNewConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/a.md", "hello\n")

	var got *config.Config
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, &fakeStore{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("store factory was not called")
	}

	want := config.Config{
		StoreName:    "fake", // overridden by the flag
		DBPath:       config.DefaultDBPath(),
		EmbedderName: "fake", // overridden by the flag
		OllamaURL:    "http://localhost:11434",
		EmbedModel:   "bge-m3",
		EvalsDir:     config.DefaultEvalsDir(),
		FreshReads:   true,
	}
	if *got != want {
		t.Errorf("config = %+v, want %+v", *got, want)
	}
}

func TestFlagsReadEnvVars(t *testing.T) {
	t.Setenv("QAZYNA_DB", "/tmp/custom.lance")
	t.Setenv("QAZYNA_EMBED_MODEL", "custom-model")

	dir := t.TempDir()
	writeFile(t, dir+"/a.md", "hello\n")

	var got *config.Config
	err := Run(
		[]string{"qazyna", "--store", "fake", "--embedder", "fake", "index", dir},
		WithDefaultParsers(),
		WithFakeEmbedder(),
		withFakeStore(&got, &fakeStore{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.DBPath != "/tmp/custom.lance" {
		t.Errorf("DBPath = %q, want env value", got.DBPath)
	}
	if got.EmbedModel != "custom-model" {
		t.Errorf("EmbedModel = %q, want env value", got.EmbedModel)
	}
}

func TestDefaultDBPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/share")
	if got := config.DefaultDBPath(); got != "/custom/share/qazyna/db.lance" {
		t.Errorf("DefaultDBPath = %q", got)
	}

	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "qazyna", "db.lance")
	if got := config.DefaultDBPath(); got != want {
		t.Errorf("DefaultDBPath = %q, want %q", got, want)
	}
}
