package cli

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"

	"qazyna/internal/config"
	"qazyna/internal/parser"
	"qazyna/internal/store"
)

type storeFactory func(ctx context.Context, cfg *config.Config) (store.Store, error)

// App holds the registered backends; which one to use is picked
// by the user via --store / --embedder flags.
type App struct {
	stores  map[string]storeFactory
	parsers *parser.Registry
}

type Option func(*App) error

func Run(args []string, opts ...Option) error {
	app := &App{
		stores:  map[string]storeFactory{},
		parsers: parser.NewRegistry(),
	}
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

type fileResult struct {
	path   string
	chunks []parser.Chunk
}

func (a *App) indexAction(ctx context.Context, cmd *cli.Command) error {
	root := cmd.Args().First()
	if root == "" {
		return fmt.Errorf("usage: qazyna index <path>")
	}

	cfg := newConfig(cmd)
	st, err := a.openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	files, err := a.collectFiles(root)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported files found in %s", root)
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))

	results := make(chan fileResult)

	// Producer runs in its own goroutine: g.Go blocks once the limit is
	// reached, and the results channel has to be drained meanwhile.
	go func() {
		for _, path := range files {
			g.Go(func() error {
				p, _ := a.parsers.For(path)
				chunks, err := p.Parse(ctx, path)
				if err != nil {
					return fmt.Errorf("parse %s: %w", path, err)
				}
				select {
				case results <- fileResult{path: path, chunks: chunks}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}
		g.Wait() //nolint:errcheck // the error is returned by the g.Wait below
		close(results)
	}()

	total := 0
	for r := range results {
		fmt.Printf("  %s — %d chunks\n", r.path, len(r.chunks))
		total += len(r.chunks)
	}
	if err := g.Wait(); err != nil {
		return err
	}

	fmt.Printf("%d chunks total (embedding not implemented yet)\n", total)
	return nil
}

// collectFiles returns the files under root that have a registered parser.
func (a *App) collectFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip hidden directories such as .git, but not root itself (".").
			if name := d.Name(); name != "." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := a.parsers.For(path); ok {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (a *App) searchAction(ctx context.Context, cmd *cli.Command) error {
	return fmt.Errorf("not implemented yet")
}
