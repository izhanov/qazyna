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
		for _, p := range []parser.Parser{parser.Markdown{}, parser.PDF{}, parser.XML{}, parser.CSV{}} {
			a.parsers.Register(p)
		}
		return nil
	}
}

// WithDefaultStore registers the LanceDB store backend.
func WithDefaultStore() Option {
	return WithStore("lance", func(ctx context.Context, cfg *config.Config) (store.Store, error) {
		return store.OpenLance(ctx, cfg.DBPath, store.WithFreshReads(cfg.FreshReads))
	})
}

func flags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "verbose", Usage: "debug logging: per-file timings, retries"},
		&cli.BoolFlag{Name: "quiet", Usage: "errors and the final summary only"},
		&cli.StringFlag{Name: "store", Value: "lance", Usage: "storage backend",
			Sources: cli.EnvVars("QAZYNA_STORE")},
		&cli.StringFlag{Name: "db", Value: config.DefaultDBPath(), Usage: "path to the database",
			Sources: cli.EnvVars("QAZYNA_DB")},
		&cli.StringFlag{Name: "embedder", Value: "ollama", Usage: "embedding backend",
			Sources: cli.EnvVars("QAZYNA_EMBEDDER")},
		&cli.StringFlag{Name: "ollama-url", Value: "http://localhost:11434", Usage: "Ollama server URL",
			Sources: cli.EnvVars("QAZYNA_OLLAMA_URL")},
		&cli.StringFlag{Name: "embed-model", Value: "bge-m3", Usage: "Ollama embedding model",
			Sources: cli.EnvVars("QAZYNA_EMBED_MODEL")},
		&cli.BoolFlag{Name: "fresh-reads", Value: true,
			Usage:   "re-open the table on every read to see writes from other processes",
			Sources: cli.EnvVars("QAZYNA_FRESH_READS")},
	}
}

func newConfig(cmd *cli.Command) *config.Config {
	return &config.Config{
		StoreName:    cmd.String("store"),
		DBPath:       cmd.String("db"),
		EmbedderName: cmd.String("embedder"),
		OllamaURL:    cmd.String("ollama-url"),
		EmbedModel:   cmd.String("embed-model"),
		FreshReads:   cmd.Bool("fresh-reads"),
	}
}
