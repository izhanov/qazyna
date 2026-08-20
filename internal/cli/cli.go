package cli

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/urfave/cli/v3"

	"qazyna/internal/config"
	"qazyna/internal/store"
)

type storeFactory func(ctx context.Context, cfg *config.Config) (store.Store, error)

// App holds the registered backends; which one to use is picked
// by the user via --store / --embedder flags.
type App struct {
	stores map[string]storeFactory
}

type Option func(*App) error

func Run(args []string, opts ...Option) error {
	app := &App{stores: map[string]storeFactory{}}
	for _, o := range opts {
		if err := o(app); err != nil {
			return err
		}
	}

	cmd := &cli.Command{
		Name:  "qazyna",
		Usage: "semantic search over local files",
		Flags: flags(),
		Commands: []*cli.Command{
			{
				Name:      "index",
				Usage:     "index a file or directory",
				ArgsUsage: "<path>",
				Action:    app.indexAction,
			},
			{
				Name:      "search",
				Usage:     "search the index",
				ArgsUsage: "<query>",
				Action:    app.searchAction,
			},
		},
	}
	return cmd.Run(context.Background(), args)
}

func (a *App) openStore(ctx context.Context, cfg *config.Config) (store.Store, error) {
	f, ok := a.stores[cfg.StoreName]
	if !ok {
		return nil, fmt.Errorf("unknown store %q (registered: %v)", cfg.StoreName, slices.Sorted(maps.Keys(a.stores)))
	}
	return f(ctx, cfg)
}

func (a *App) indexAction(ctx context.Context, cmd *cli.Command) error {
	cfg := newConfig(cmd)
	st, err := a.openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	fmt.Println("db opened, indexing not implemented yet")
	return nil
}

func (a *App) searchAction(ctx context.Context, cmd *cli.Command) error {
	return fmt.Errorf("not implemented yet")
}
