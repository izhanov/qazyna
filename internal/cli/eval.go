package cli

import (
	"context"
	"fmt"
	"os"
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

// evalAction runs the golden set against the live index and reports
// recall@limit (an expected file made the top results) and MRR (mean of
// 1/rank of the first correct file, 0 for a miss).
func (a *App) evalAction(ctx context.Context, cmd *cli.Command) error {
	setupLogging(cmd)

	path := cmd.Args().First()
	if path == "" {
		return fmt.Errorf("usage: qazyna eval <golden.yaml>")
	}
	cases, err := loadGolden(path)
	if err != nil {
		return err
	}

	mode := cmd.String("mode")
	limit := cmd.Int("limit")

	cfg := newConfig(cmd)
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
	hits := 0
	mrr := 0.0
	for i, c := range cases {
		results, err := search.Search(ctx, st, emb, c.Query, search.Options{Mode: mode, Limit: limit})
		if err != nil {
			return fmt.Errorf("case %d (%q): %w", i+1, c.Query, err)
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
	fmt.Printf("\nrecall@%d %.2f (%d/%d)   MRR %.3f   mode=%s\n",
		limit, float64(hits)/float64(n), hits, n, mrr/float64(n), mode)
	return nil
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
