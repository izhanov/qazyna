package cli

import (
	"context"

	"github.com/urfave/cli/v3"

	"qazyna/internal/config"
	"qazyna/internal/store"
)

func WithStore(name string, f storeFactory) Option {
	return func(a *App) error {
		a.stores[name] = f
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
		&cli.StringFlag{Name: "embed-model", Value: "nomic-embed-text", Usage: "Ollama embedding model"},
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
