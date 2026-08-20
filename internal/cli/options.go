package cli

import (
	"context"

	"github.com/urfave/cli/v3"

	"qazyna/internal/config"
	"qazyna/internal/embed"
	"qazyna/internal/parser"
	"qazyna/internal/store"
)

func WithStore(name string, f storeFactory) Option {
	return func(a *App) error {
		a.stores[name] = f
		return nil
	}
}

func WithEmbedder(name string, f embedderFactory) Option {
	return func(a *App) error {
		a.embedders[name] = f
		return nil
	}
}

// WithDefaultEmbedder registers the Ollama embedding backend.
func WithDefaultEmbedder() Option {
	return WithEmbedder("ollama", func(cfg *config.Config) (embed.Embedder, error) {
		return embed.NewOllama(cfg.OllamaURL, cfg.EmbedModel), nil
	})
}

// WithFakeEmbedder registers a deterministic no-model embedder, useful for
// tests and for running the pipeline without Ollama: --embedder fake.
func WithFakeEmbedder() Option {
	return WithEmbedder("fake", func(_ *config.Config) (embed.Embedder, error) {
		return embed.Fake{Dim: 32}, nil
	})
}

func WithParser(p parser.Parser) Option {
	return func(a *App) error {
		a.parsers.Register(p)
		return nil
	}
}

// WithDefaultParsers registers a parser for every supported format.
func WithDefaultParsers() Option {
	return func(a *App) error {
		for _, p := range []parser.Parser{parser.Markdown{}, parser.PDF{}} {
			a.parsers.Register(p)
		}
		return nil
	}
}

// WithDefaultStore registers the LanceDB store backend.
func WithDefaultStore() Option {
	return WithStore("lance", func(ctx context.Context, cfg *config.Config) (store.Store, error) {
		return store.OpenLance(ctx, cfg.DBPath)
	})
}

func flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "store", Value: "lance", Usage: "storage backend"},
		&cli.StringFlag{Name: "db", Value: "data/qazyna.lance", Usage: "path to the database"},
		&cli.StringFlag{Name: "embedder", Value: "ollama", Usage: "embedding backend"},
		&cli.StringFlag{Name: "ollama-url", Value: "http://localhost:11434", Usage: "Ollama server URL"},
		&cli.StringFlag{Name: "embed-model", Value: "bge-m3", Usage: "Ollama embedding model"},
	}
}

func newConfig(cmd *cli.Command) *config.Config {
	return &config.Config{
		StoreName:    cmd.String("store"),
		DBPath:       cmd.String("db"),
		EmbedderName: cmd.String("embedder"),
		OllamaURL:    cmd.String("ollama-url"),
		EmbedModel:   cmd.String("embed-model"),
	}
}
