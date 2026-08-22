package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"qazyna/internal/embed"
	"qazyna/internal/search"
	"qazyna/internal/store"
)

// evalCase is one golden-set entry: a query and the files that count as a
// correct answer. Expectations are file-level path suffixes — chunk ordinals
// shift whenever the chunking changes, "the right file was found" does not.
type evalCase struct {
	Query  string   `yaml:"query"`
	Expect []string `yaml:"expect"`
}

// evalAction runs golden sets against the live index and reports
// recall@limit (an expected file made the top results) and MRR (mean of
// 1/rank of the first correct file, 0 for a miss). With no argument it runs
// every set in the evals directory (--evals, next to the database by default).
func (a *App) evalAction(ctx context.Context, cmd *cli.Command) error {
	setupLogging(cmd)

	cfg := newConfig(cmd)
	files := cmd.Args().Slice()
	if len(files) == 0 {
		var err error
		if files, err = goldenFiles(cfg.EvalsDir); err != nil {
			return err
		}
	}

	mode := cmd.String("mode")
	limit := cmd.Int("limit")

	st, err := a.openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	var emb embed.Embedder
	if mode != search.ModeText {
		if emb, err = a.newEmbedder(cfg); err != nil {
			return err
		}
	}

	color := colorsEnabled()
	totalHits, totalCases := 0, 0
	totalMRR := 0.0
	for _, path := range files {
		cases, err := loadGolden(path)
		if err != nil {
			return err
		}
		if len(files) > 1 {
			fmt.Printf("%s\n", paint(color, "1", prettyPath(path)))
		}

		hits := 0
		mrr := 0.0
		for i, c := range cases {
			results, err := search.Search(ctx, st, emb, c.Query, search.Options{Mode: mode, Limit: limit})
			if err != nil {
				return fmt.Errorf("%s case %d (%q): %w", path, i+1, c.Query, err)
			}

			rank, found := firstExpectedRank(results, c.Expect)
			if rank > 0 {
				hits++
				mrr += 1.0 / float64(rank)
				fmt.Printf("%-2d %s  %s\n", i+1, paint(color, "32", fmt.Sprintf("hit@%d", rank)), snippet(c.Query, 60))
				fmt.Printf("          %s\n", paint(color, "2", prettyPath(found)))
			} else {
				fmt.Printf("%-2d %s   %s\n", i+1, paint(color, "31", "miss"), snippet(c.Query, 60))
				fmt.Printf("          %s\n", paint(color, "2", "want "+strings.Join(c.Expect, ", ")))
			}
		}

		n := len(cases)
		fmt.Printf("\nrecall@%d %.2f (%d/%d)   MRR %.3f   mode=%s\n\n",
			limit, float64(hits)/float64(n), hits, n, mrr/float64(n), mode)
		totalHits += hits
		totalCases += n
		totalMRR += mrr
	}

	if len(files) > 1 {
		fmt.Printf("overall: recall@%d %.2f (%d/%d)   MRR %.3f\n",
			limit, float64(totalHits)/float64(totalCases), totalHits, totalCases,
			totalMRR/float64(totalCases))
	}
	return nil
}

// evalsAction shows the golden sets: their files and cases. With --edit it
// opens them in the user's editor instead, seeding a starter file when the
// directory is still empty.
func (a *App) evalsAction(_ context.Context, cmd *cli.Command) error {
	setupLogging(cmd)
	dir := newConfig(cmd).EvalsDir

	if cmd.Bool("edit") {
		return editEvals(dir)
	}

	files, err := goldenFiles(dir)
	if err != nil {
		return err
	}

	color := colorsEnabled()
	for _, path := range files {
		cases, err := loadGolden(path)
		if err != nil {
			// Show the broken file instead of aborting the listing: the whole
			// point of this command is seeing what is there.
			fmt.Printf("%s — %v\n\n", paint(color, "1", prettyPath(path)), err)
			continue
		}
		fmt.Printf("%s (%d cases)\n", paint(color, "1", prettyPath(path)), len(cases))
		for i, c := range cases {
			fmt.Printf("%-2d %s\n   %s\n", i+1, snippet(c.Query, 70),
				paint(color, "2", "→ "+strings.Join(c.Expect, ", ")))
		}
		fmt.Println()
	}
	return nil
}

// editEvals opens the golden sets in $VISUAL/$EDITOR: the single file when
// there is one, the directory when there are several. An empty directory is
// seeded with a commented starter file first.
func editEvals(dir string) error {
	files, err := goldenFiles(dir)
	if err != nil {
		seed := filepath.Join(dir, "golden.yaml")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(seed, []byte(goldenSeed), 0o644); err != nil {
			return err
		}
		fmt.Printf("created starter golden set: %s\n", seed)
		files = []string{seed}
	}

	target := dir
	if len(files) == 1 {
		target = files[0]
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set — open %s yourself", target)
	}

	args := append(strings.Fields(editor), target)
	ed := exec.Command(args[0], args[1:]...)
	ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stdout, os.Stderr
	return ed.Run()
}

const goldenSeed = `# Golden sets for ` + "`qazyna eval`" + `: query → expected-file cases.
# expect entries are file-level path suffixes: chunking changes don't
# invalidate them. Add a case every time search performs poorly.
#
# - query: "how do I deploy to preprod"
#   expect: ["skills/deploy/SKILL.md"]
`

// goldenFiles lists the golden sets in the evals directory, sorted.
func goldenFiles(dir string) ([]string, error) {
	var files []string
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no golden sets (*.yaml) in %s — put one there, "+
			"point --evals (or QAZYNA_EVALS) at your directory, or pass a file: qazyna eval <golden.yaml>", dir)
	}
	slices.Sort(files)
	return files, nil
}

func loadGolden(path string) ([]evalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []evalCase
	if err := yaml.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s: no cases", path)
	}
	for i, c := range cases {
		if c.Query == "" || len(c.Expect) == 0 {
			return nil, fmt.Errorf("%s: case %d needs both query and expect", path, i+1)
		}
	}
	return cases, nil
}

// firstExpectedRank returns the 1-based rank of the first result whose path
// ends with one of the expected suffixes, and that path. Results are chunks,
// so the first chunk of an expected file sets the file's rank.
func firstExpectedRank(results []store.SearchResult, expect []string) (int, string) {
	for i, r := range results {
		for _, want := range expect {
			if strings.HasSuffix(r.Path, want) {
				return i + 1, r.Path
			}
		}
	}
	return 0, ""
}
