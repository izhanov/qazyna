package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"

	"qazyna/internal/config"
	"qazyna/internal/embed"
	"qazyna/internal/mcpserver"
	"qazyna/internal/mcpserver/tools"
	"qazyna/internal/parser"
	"qazyna/internal/search"
	"qazyna/internal/skill"
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

// version is stamped at build time via -ldflags "-X qazyna/internal/cli.version=...".
var version = "dev"

func Run(args []string, opts ...Option) error {
	// Ctrl-C / SIGTERM cancel the context, which reaches every worker,
	// Ollama request and pdftotext subprocess instead of killing mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		Name:    "qazyna",
		Usage:   "semantic search over local files",
		Version: version,
		Flags:   flags(),
		Commands: []*cli.Command{
			{
				Name:      "index",
				Usage:     "index files and directories",
				ArgsUsage: "<path> [<path>...]",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "force", Aliases: []string{"f"},
						Usage: "re-index even unchanged files (skip the mtime check)"},
				},
				Action: app.indexAction,
			},
			{
				Name:      "reindex",
				Usage:     "wipe the database and index from scratch (required after changing the embedding model)",
				ArgsUsage: "<path> [<path>...]",
				Action:    app.reindexAction,
			},
			{
				Name:   "mcp",
				Usage:  "serve the index to AI agents over MCP (stdio)",
				Action: app.mcpAction,
			},
			{
				Name:  "flush",
				Usage: "remove all indexed data from the database",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "skip confirmation"},
				},
				Action: app.flushAction,
			},
			{
				Name:  "list",
				Usage: "list indexed files",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "json", Usage: "machine-readable output"},
				},
				Action: app.listAction,
			},
			{
				Name:      "search",
				Usage:     "search the index by meaning; -word and -\"a phrase\" inside the query exclude results containing them",
				ArgsUsage: "<query>",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Value: 5, Usage: "maximum number of results"},
					&cli.BoolFlag{Name: "json", Usage: "machine-readable output"},
					&cli.StringFlag{Name: "mode", Value: "hybrid", Usage: "search mode: hybrid, vector (by meaning) or text (by words)"},
					&cli.StringSliceFlag{Name: "exclude-path", Usage: "drop results whose file path contains this fragment (repeatable)"},
				},
				Action: app.searchAction,
			},
			{
				Name:      "eval",
				Usage:     "measure search quality against golden sets of query → expected-file cases; no argument runs every set in the evals directory",
				ArgsUsage: "[golden.yaml ...]",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Value: 5, Usage: "results per query; recall is measured at this depth"},
					&cli.StringFlag{Name: "mode", Value: "hybrid", Usage: "search mode to evaluate: hybrid, vector or text"},
				},
				Action: app.evalAction,
			},
			{
				Name:   "setup",
				Usage:  "one-command setup: check Ollama, pull the model, register MCP in Claude Code, install the skill",
				Action: app.setupAction,
			},
			{
				Name:  "skill",
				Usage: "manage the Claude Code skill that teaches AI agents to search with qazyna",
				Commands: []*cli.Command{
					{
						Name:   "install",
						Usage:  "install the skill into ~/.claude/skills (overwrites previous version)",
						Action: skillInstallAction,
					},
					{
						Name:   "uninstall",
						Usage:  "remove the installed skill",
						Action: skillUninstallAction,
					},
					{
						Name:   "show",
						Usage:  "print the skill to stdout",
						Action: skillShowAction,
					},
				},
			},
		},
	}
	return cmd.Run(ctx, args)
}

func skillInstallAction(_ context.Context, _ *cli.Command) error {
	path, err := skill.Install()
	if err != nil {
		return err
	}
	fmt.Printf("skill installed: %s\n", path)
	fmt.Println("\nIt loads automatically in new Claude Code sessions. To make agents")
	fmt.Println("reach for it proactively, add a line to your ~/.claude/CLAUDE.md:")
	fmt.Println("\n  Before tasks involving my notes, docs or style guides, use the qazyna-search skill.")
	return nil
}

func skillUninstallAction(_ context.Context, _ *cli.Command) error {
	dir, err := skill.Uninstall()
	if err != nil {
		return err
	}
	fmt.Printf("skill removed: %s\n", dir)
	return nil
}

func skillShowAction(_ context.Context, _ *cli.Command) error {
	_, err := os.Stdout.Write(skill.Content)
	return err
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
	err    error
}

// setupLogging routes diagnostics to stderr; stdout stays clean for the
// actual output (search results, progress lines) so piping keeps working.
func setupLogging(cmd *cli.Command) {
	level := logLevel(cmd.Bool("quiet"), cmd.Bool("verbose"))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func logLevel(quiet, verbose bool) slog.Level {
	switch {
	case quiet:
		return slog.LevelError // --quiet wins over --verbose
	case verbose:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func (a *App) indexAction(ctx context.Context, cmd *cli.Command) error {
	return a.runIndex(ctx, cmd, false, cmd.Bool("force"))
}

func (a *App) reindexAction(ctx context.Context, cmd *cli.Command) error {
	return a.runIndex(ctx, cmd, true, false)
}

func (a *App) runIndex(ctx context.Context, cmd *cli.Command, reset, force bool) error {
	setupLogging(cmd)
	quiet := cmd.Bool("quiet")
	start := time.Now()

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

	known, err := st.Paths(ctx)
	if err != nil {
		return err
	}

	// Incremental indexing: an unchanged mtime means the file is skipped
	// entirely — no parsing and, above all, no embedding. --force bypasses
	// the check for files whose content changed without touching mtime.
	var work []string
	skipped := 0
	for _, path := range files {
		if !force {
			if info, err := os.Stat(path); err == nil && known[path] == info.ModTime().Unix() {
				skipped++
				continue
			}
		}
		work = append(work, path)
	}

	// Indexed files that vanished from the indexed directories are stale:
	// drop their chunks so search stops returning ghosts. Paths outside the
	// given roots are left alone — this run knows nothing about them.
	removed := 0
	onDisk := make(map[string]bool, len(files))
	for _, f := range files {
		onDisk[f] = true
	}
	for dbPath := range known {
		if onDisk[dbPath] || !underAnyRoot(dbPath, roots) {
			continue
		}
		if err := st.DeleteByPath(ctx, dbPath); err != nil {
			return err
		}
		slog.Info("removed vanished file from index", "file", dbPath)
		removed++
	}

	files = work
	if len(files) == 0 {
		fmt.Printf("nothing to do: %d files unchanged, %d stale removed\n", skipped, removed)
		return nil
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
				if err != nil && ctx.Err() != nil {
					return ctx.Err() // shutting down, not a file problem
				}
				select {
				case results <- fileResult{path: path, chunks: chunks, err: err}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}
		g.Wait() //nolint:errcheck // the error is returned by the g.Wait below
		close(results)
	}()

	// One broken file must not abort the run: failures are reported and
	// counted, the rest of the corpus still gets indexed.
	total, failed := 0, 0
	for r := range results {
		if r.err != nil {
			failed++
			slog.Error("failed to index", "file", r.path, "error", r.err)
			continue
		}
		if !quiet {
			fmt.Printf("  %s — %d chunks indexed\n", r.path, len(r.chunks))
		}
		total += len(r.chunks)
	}
	if err := g.Wait(); err != nil {
		return err
	}

	stored, err := st.Count(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%d chunks indexed, %d in the database", total, stored)
	if skipped > 0 || removed > 0 {
		fmt.Printf(" (%d unchanged skipped, %d stale removed)", skipped, removed)
	}
	fmt.Println()
	slog.Info("done",
		"files", len(files)-failed, "failed", failed, "chunks", total,
		"skipped", skipped, "removed", removed,
		"took", time.Since(start).Round(time.Millisecond))
	if failed > 0 {
		return fmt.Errorf("%d of %d files failed to index", failed, len(files))
	}
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
	switch stored := meta[search.MetaEmbedderKey]; stored {
	case embedderID:
		return nil
	case "":
		meta[search.MetaEmbedderKey] = embedderID
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
	t := time.Now()
	chunks, err := p.Parse(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	slog.Debug("parsed", "file", path, "chunks", len(chunks), "took", time.Since(t).Round(time.Millisecond))
	if len(chunks) == 0 {
		// Nothing to store, but drop stale chunks of a now-empty file.
		return nil, st.DeleteByPath(ctx, path)
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = embeddingText(path, c)
	}
	t = time.Now()
	vectors, err := emb.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed %s: %w", path, err)
	}
	embed.Normalize(vectors)
	slog.Debug("embedded", "file", path, "vectors", len(vectors), "took", time.Since(t).Round(time.Millisecond))

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
	t = time.Now()
	if err := st.AddDocument(ctx, doc); err != nil {
		return nil, err
	}
	slog.Debug("stored", "file", path, "took", time.Since(t).Round(time.Millisecond))
	return chunks, nil
}

// embeddingText builds what the embedder sees for one chunk: the file name
// and the heading trail woven in above the text, so a query mentioning
// either still lands on the right vectors. The stored chunk text stays
// untouched — this context never shows up in search output.
func embeddingText(path string, c parser.Chunk) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)

	parts := make([]string, 0, 3)
	parts = append(parts, name)
	if c.Section != "" {
		parts = append(parts, c.Section)
	}
	parts = append(parts, c.Text)
	return strings.Join(parts, "\n")
}

// underAnyRoot reports whether path lies under (or is) one of the roots.
func underAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		root = filepath.Clean(root)
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
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

func (a *App) mcpAction(ctx context.Context, cmd *cli.Command) error {
	setupLogging(cmd)

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

	return mcpserver.Run(ctx,
		tools.SearchNotes(st, emb),
		tools.IndexStatus(st),
		tools.ListFiles(st),
	)
}

func (a *App) listAction(ctx context.Context, cmd *cli.Command) error {
	setupLogging(cmd)

	st, err := a.openStore(ctx, newConfig(cmd))
	if err != nil {
		return err
	}
	defer st.Close()

	known, err := st.Paths(ctx)
	if err != nil {
		return err
	}
	paths := slices.Sorted(maps.Keys(known))

	if cmd.Bool("json") {
		type indexedFile struct {
			Path     string `json:"path"`
			Modified string `json:"modified"`
		}
		files := make([]indexedFile, 0, len(paths))
		for _, p := range paths {
			files = append(files, indexedFile{
				Path:     p,
				Modified: time.Unix(known[p], 0).UTC().Format(time.RFC3339),
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(files)
	}

	if len(paths) == 0 {
		fmt.Println("index is empty")
		return nil
	}
	for _, p := range paths {
		fmt.Printf("%s  %s\n", time.Unix(known[p], 0).Format("2006-01-02 15:04"), prettyPath(p))
	}
	fmt.Printf("%d files\n", len(paths))
	return nil
}

func (a *App) flushAction(ctx context.Context, cmd *cli.Command) error {
	setupLogging(cmd)

	st, err := a.openStore(ctx, newConfig(cmd))
	if err != nil {
		return err
	}
	defer st.Close()

	n, err := st.Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Println("database is already empty")
		return nil
	}

	if !cmd.Bool("force") {
		ok, err := confirm(fmt.Sprintf("flush removes ALL indexed data (%d chunks). Continue?", n))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("aborted")
			return nil
		}
	}

	if err := st.Reset(ctx); err != nil {
		return err
	}
	fmt.Println("database cleared")
	return nil
}

// confirm asks a y/N question on the terminal. A non-interactive stdin
// (pipe, /dev/null, cron) refuses instead of hanging on a read nobody
// will answer.
func confirm(question string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("stdin is not a terminal; pass --force to confirm")
	}
	fmt.Printf("%s [y/N]: ", question)
	var answer string
	fmt.Scanln(&answer) //nolint:errcheck // empty answer means "no"
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func (a *App) searchAction(ctx context.Context, cmd *cli.Command) error {
	setupLogging(cmd)
	start := time.Now()

	query := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
	if query == "" {
		return fmt.Errorf("usage: qazyna search <query>")
	}
	mode := cmd.String("mode")

	cfg := newConfig(cmd)
	st, err := a.openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	// ModeText works without an embedder (and on a foreign-model database),
	// so only resolve one when the mode actually needs vectors.
	var emb embed.Embedder
	if mode != search.ModeText {
		if emb, err = a.newEmbedder(cfg); err != nil {
			return err
		}
	}

	results, err := search.Search(ctx, st, emb, query, search.Options{
		Mode:        mode,
		Limit:       cmd.Int("limit"),
		ExcludePath: cmd.StringSlice("exclude-path"),
	})
	if err != nil {
		return err
	}
	slog.Debug("search done", "mode", mode, "results", len(results),
		"took", time.Since(start).Round(time.Millisecond))

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
