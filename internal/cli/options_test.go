package cli

import (
	"context"
	"testing"

	"qazyna/internal/config"
	"qazyna/internal/parser"
	"qazyna/internal/store"
)

func newTestApp() *App {
	return &App{
		stores:  map[string]storeFactory{},
		parsers: parser.NewRegistry(),
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
	for _, path := range []string{"a.md", "b.markdown"} {
		if _, ok := app.parsers.For(path); !ok {
			t.Errorf("no parser registered for %s", path)
		}
	}
}

// newConfig is what turns CLI flags into config; check defaults end-to-end
// through Run with the fake store capturing the config it received.
func TestNewConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/a.md", "hello\n")

	var (
		got    *config.Config
		closed bool
	)
	err := Run(
		[]string{"qazyna", "--store", "fake", "index", dir},
		WithDefaultParsers(),
		withFakeStore(&got, &closed),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("store factory was not called")
	}

	want := config.Config{
		StoreName:    "fake", // overridden by the flag
		DBPath:       "data/qazyna.lance",
		EmbedderName: "ollama",
		OllamaURL:    "http://localhost:11434",
		EmbedModel:   "nomic-embed-text",
	}
	if *got != want {
		t.Errorf("config = %+v, want %+v", *got, want)
	}
}
