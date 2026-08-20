package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"

	"qazyna/internal/config"
	"qazyna/internal/embed"
	"qazyna/internal/parser"
	"qazyna/internal/store"
)

type storeFactory func(ctx context.Context, cfg *config.Config) (store.Store, error)
type embedderFactory func(cfg *config.Config) (embed.Embedder, error)

// App holds the registered backends; which one to use is picked
// by the user via --store / --embedder flags.
type App struct {
	stores    map[string]storeFactory
	embedders map[string]embedderFactory
	parsers   *parser.Registry
}

type Option func(*App) error

func Run(args []string, opts ...Option) error {
	app := &App{
		stores:    map[string]storeFactory{},
		embedders: map[string]embedderFactory{},
		parsers:   parser.NewRegistry(),
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
				Usage:     "index files and directories",
				ArgsUsage: "<path> [<path>...]",
				Action:    app.indexAction,
			},
			{
				Name:      "reindex",
				Usage:     "wipe the database and index from scratch (required after changing the embedding model)",
				ArgsUsage: "<path> [<path>...]",
				Action:    app.reindexAction,
			},
			{
				Name:      "search",
				Usage:     "search the index by meaning",
				ArgsUsage: "<query>",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Value: 5, Usage: "maximum number of results"},
					&cli.BoolFlag{Name: "json", Usage: "machine-readable output"},
				},
				Action: app.searchAction,
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

func (a *App) newEmbedder(cfg *config.Config) (embed.Embedder, error) {
	f, ok := a.embedders[cfg.EmbedderName]
	if !ok {
		return nil, fmt.Errorf("unknown embedder %q (registered: %v)", cfg.EmbedderName, slices.Sorted(maps.Keys(a.embedders)))
	}
	return f(cfg)
}

type fileResult struct {
	path   string
	chunks []parser.Chunk
}

// metaEmbedderKey stores the Embedder.ID the database was built with.
const metaEmbedderKey = "embedder"

func (a *App) indexAction(ctx context.Context, cmd *cli.Command) error {
	return a.runIndex(ctx, cmd, false)
}

func (a *App) reindexAction(ctx context.Context, cmd *cli.Command) error {
	return a.runIndex(ctx, cmd, true)
}

func (a *App) runIndex(ctx context.Context, cmd *cli.Command, reset bool) error {
	roots := cmd.Args().Slice()
	if len(roots) == 0 {
		return fmt.Errorf("usage: qazyna %s <path> [<path>...]", cmd.Name)
	}

	cfg := newConfig(cmd)
	st, err := a.openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	emb, err := a.newEmbedder(cfg)
	if err != nil {
		return err
	}

	if reset {
		if err := st.Reset(ctx); err != nil {
			return err
		}
	}
	if err := checkEmbedderMeta(ctx, st, emb.ID()); err != nil {
		return err
	}

	files, err := a.collectFiles(roots...)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported files found in %s", strings.Join(roots, ", "))
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))

	results := make(chan fileResult)

	// Producer runs in its own goroutine: g.Go blocks once the limit is
	// reached, and the results channel has to be drained meanwhile.
	go func() {
		for _, path := range files {
			g.Go(func() error {
				chunks, err := a.indexFile(ctx, path, emb, st)
				if err != nil {
					return err
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
		fmt.Printf("  %s — %d chunks indexed\n", r.path, len(r.chunks))
		total += len(r.chunks)
	}
	if err := g.Wait(); err != nil {
		return err
	}

	stored, err := st.Count(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%d chunks indexed, %d in the database\n", total, stored)
	return nil
}

// checkEmbedderMeta refuses to mix vectors of different embedders in one
// database: even with equal dimensions they are incomparable. A fresh
// database gets stamped with the current embedder ID.
func checkEmbedderMeta(ctx context.Context, st store.Store, embedderID string) error {
	meta, err := st.Meta(ctx)
	if err != nil {
		return err
	}
	switch stored := meta[metaEmbedderKey]; stored {
	case embedderID:
		return nil
	case "":
		meta[metaEmbedderKey] = embedderID
		return st.SetMeta(ctx, meta)
	default:
		return fmt.Errorf(
			"database is indexed with embedder %q, current is %q; run `qazyna reindex <path>` to rebuild",
			stored, embedderID,
		)
	}
}

// indexFile runs the full pipeline for one file: parse → embed → store.
func (a *App) indexFile(ctx context.Context, path string, emb embed.Embedder, st store.Store) ([]parser.Chunk, error) {
	p, _ := a.parsers.For(path)
	chunks, err := p.Parse(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(chunks) == 0 {
		// Nothing to store, but drop stale chunks of a now-empty file.
		return nil, st.DeleteByPath(ctx, path)
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	vectors, err := emb.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed %s: %w", path, err)
	}
	embed.Normalize(vectors)

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	doc := store.Document{
		Path:    path,
		MTime:   info.ModTime().Unix(),
		Chunks:  chunks,
		Vectors: vectors,
	}
	if err := st.AddDocument(ctx, doc); err != nil {
		return nil, err
	}
	return chunks, nil
}

// collectFiles returns the files under the given roots that have a registered
// parser. Overlapping roots (e.g. `notes/` and `notes/a.md`) are deduplicated,
// so every file is indexed at most once.
func (a *App) collectFiles(roots ...string) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	for _, root := range roots {
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
			path = filepath.Clean(path)
			if _, ok := a.parsers.For(path); ok && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (a *App) searchAction(ctx context.Context, cmd *cli.Command) error {
	query := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
	if query == "" {
		return fmt.Errorf("usage: qazyna search <query>")
	}

	cfg := newConfig(cmd)
	st, err := a.openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	emb, err := a.newEmbedder(cfg)
	if err != nil {
		return err
	}

	// Searching with a different model than the one that built the index
	// would silently return garbage — refuse, same as index does.
	meta, err := st.Meta(ctx)
	if err != nil {
		return err
	}
	switch stored := meta[metaEmbedderKey]; stored {
	case "":
		return fmt.Errorf("database is empty; run `qazyna index <path>` first")
	case emb.ID():
	default:
		return fmt.Errorf("database is indexed with embedder %q, current is %q", stored, emb.ID())
	}

	vectors, err := emb.Embed(ctx, []string{query})
	if err != nil {
		return err
	}
	embed.Normalize(vectors)

	results, err := st.Search(ctx, vectors[0], cmd.Int("limit"))
	if err != nil {
		return err
	}

	if cmd.Bool("json") {
		for i := range results {
			results[i].Score = math.Round(results[i].Score*10000) / 10000
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	printResults(results)
	return nil
}

func printResults(results []store.SearchResult) {
	if len(results) == 0 {
		fmt.Println("nothing found")
		return
	}

	color := colorsEnabled()
	for i, r := range results {
		score := fmt.Sprintf("%.2f", r.Score)
		switch {
		case r.Score >= 0.7:
			score = paint(color, "32", score) // green
		case r.Score >= 0.5:
			score = paint(color, "33", score) // yellow
		default:
			score = paint(color, "90", score) // gray
		}

		fmt.Printf("%-2d %s  %s\n", i+1, score, paint(color, "1", prettyPath(r.Path)))
		if r.Section != "" {
			fmt.Printf("         %s\n", paint(color, "2", r.Section))
		}
		fmt.Printf("         %s\n\n", snippet(r.Text, 200))
	}
}

// prettyPath makes a stored path easier to read: relative to the current
// directory when inside it, otherwise with the home directory as ~.
func prettyPath(path string) string {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			return "~" + string(filepath.Separator) + rest
		}
	}
	return path
}

func colorsEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func paint(enabled bool, code, s string) string {
	if !enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// snippet flattens text to a single line of at most maxRunes runes.
func snippet(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}
