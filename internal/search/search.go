// Package search implements qazyna's hybrid search over a Store: a semantic
// (vector) and a lexical (exact words) lookup run concurrently and their
// rankings are fused with Reciprocal Rank Fusion. It is shared by the CLI
// and the MCP server.
package search

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"qazyna/internal/embed"
	"qazyna/internal/store"
)

const (
	ModeHybrid = "hybrid"
	ModeVector = "vector"
	ModeText   = "text"
)

type Options struct {
	Mode  string // ModeHybrid (default), ModeVector or ModeText
	Limit int    // maximum results, default 5

	// Exclude drops chunks whose text or section contains any of these
	// terms, case-insensitive. Embeddings cannot express "not about X" —
	// mentioning X in a query attracts it — so exclusion is a filter.
	// The query itself may also carry exclusions: -word or -"a phrase".
	Exclude []string
	// ExcludePath drops results whose file path contains any of these
	// fragments, case-insensitive.
	ExcludePath []string
}

// MetaEmbedderKey stores the Embedder.ID the database was built with.
const MetaEmbedderKey = "embedder"

// Search runs the query against the store. The embedder may be nil for
// ModeText. Searching with a different model than the one that built the
// index would silently return garbage, so that is refused.
func Search(ctx context.Context, st store.Store, emb embed.Embedder, query string, opts Options) ([]store.SearchResult, error) {
	if opts.Mode == "" {
		opts.Mode = ModeHybrid
	}
	switch opts.Mode {
	case ModeHybrid, ModeVector, ModeText:
	default:
		return nil, fmt.Errorf("unknown mode %q (want hybrid, vector or text)", opts.Mode)
	}
	if opts.Limit <= 0 {
		opts.Limit = 5
	}

	query, minus := ParseExclusions(query)
	exclude := append(minus, opts.Exclude...)
	if query == "" {
		return nil, fmt.Errorf("query is empty after removing exclusions")
	}

	meta, err := st.Meta(ctx)
	if err != nil {
		return nil, err
	}
	if meta[MetaEmbedderKey] == "" {
		return nil, fmt.Errorf("database is empty; run `qazyna index <path>` first")
	}

	// Both halves run concurrently: embedding the query dominates the
	// latency, the lexical lookup rides along for free.
	fetch := opts.Limit
	if opts.Mode == ModeHybrid {
		fetch = opts.Limit * 3 // give the fusion room to work with
	}
	filtering := len(exclude)+len(opts.ExcludePath) > 0
	if filtering {
		fetch *= 4 // headroom: filtering happens after the stores return
	}
	var vecResults, textResults []store.SearchResult
	g, gctx := errgroup.WithContext(ctx)
	if opts.Mode != ModeText {
		if emb == nil {
			return nil, fmt.Errorf("mode %q needs an embedder", opts.Mode)
		}
		if stored := meta[MetaEmbedderKey]; stored != emb.ID() {
			return nil, fmt.Errorf("database is indexed with embedder %q, current is %q", stored, emb.ID())
		}
		g.Go(func() error {
			vectors, err := emb.Embed(gctx, []string{query})
			if err != nil {
				return err
			}
			embed.Normalize(vectors)
			vecResults, err = st.Search(gctx, vectors[0], fetch)
			return err
		})
	}
	if opts.Mode != ModeVector {
		g.Go(func() error {
			var err error
			textResults, err = st.SearchText(gctx, query, fetch)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	if filtering {
		vecResults = filterExcluded(vecResults, exclude, opts.ExcludePath)
		textResults = filterExcluded(textResults, exclude, opts.ExcludePath)
	}

	switch opts.Mode {
	case ModeVector:
		return vecResults[:min(opts.Limit, len(vecResults))], nil
	case ModeText:
		return textResults[:min(opts.Limit, len(textResults))], nil
	default:
		return rrfMerge(vecResults, textResults, opts.Limit), nil
	}
}

var (
	excludePhraseRe = regexp.MustCompile(`(?:^|\s)-"([^"]+)"`)
	excludeWordRe   = regexp.MustCompile(`(?:^|\s)-(\S+)`)
)

// ParseExclusions splits minus-operators out of a query: -word and
// -"some phrase" become exclusion terms, the rest stays the query.
// A hyphen inside a word (re-index, state-of-the-art) is not an operator.
func ParseExclusions(query string) (positive string, exclude []string) {
	strip := func(re *regexp.Regexp, q string) string {
		return re.ReplaceAllStringFunc(q, func(m string) string {
			if term := strings.Trim(re.FindStringSubmatch(m)[1], `"`); term != "" {
				exclude = append(exclude, term)
			}
			return " "
		})
	}
	query = strip(excludePhraseRe, query)
	query = strip(excludeWordRe, query)
	return strings.Join(strings.Fields(query), " "), exclude
}

// filterExcluded drops results matching an exclusion: a term found in the
// chunk text or section, or a fragment found in the file path. All matching
// is case-insensitive substring.
func filterExcluded(results []store.SearchResult, terms, paths []string) []store.SearchResult {
	return slices.DeleteFunc(results, func(r store.SearchResult) bool {
		content := strings.ToLower(r.Text + "\n" + r.Section)
		for _, t := range terms {
			if strings.Contains(content, strings.ToLower(t)) {
				return true
			}
		}
		path := strings.ToLower(r.Path)
		for _, p := range paths {
			if strings.Contains(path, strings.ToLower(p)) {
				return true
			}
		}
		return false
	})
}

// rrfMerge fuses two ranked lists via Reciprocal Rank Fusion: each result
// earns 1/(k+rank) from every list it appears in, so agreement between the
// semantic and lexical halves beats a high position in either one alone.
// Scores are scaled so that a #1 hit in both lists gives exactly 1.
func rrfMerge(a, b []store.SearchResult, limit int) []store.SearchResult {
	const k = 60

	type entry struct {
		result store.SearchResult
		score  float64
	}
	byID := map[string]*entry{}
	add := func(list []store.SearchResult) {
		for rank, r := range list {
			id := fmt.Sprintf("%s#%d", r.Path, r.Ordinal)
			e, ok := byID[id]
			if !ok {
				e = &entry{result: r}
				byID[id] = e
			}
			e.score += 1 / float64(k+rank+1)
		}
	}
	add(a)
	add(b)

	merged := slices.Collect(maps.Values(byID))
	slices.SortFunc(merged, func(x, y *entry) int {
		if x.score != y.score {
			if x.score > y.score {
				return -1
			}
			return 1
		}
		return strings.Compare(x.result.Path, y.result.Path)
	})

	results := make([]store.SearchResult, 0, min(limit, len(merged)))
	for _, e := range merged[:min(limit, len(merged))] {
		e.result.Score = e.score / (2.0 / (k + 1)) // both-#1 → 1.0, single-#1 → 0.5
		results = append(results, e.result)
	}
	return results
}
