package store

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"
)

const (
	tableName     = "chunks"
	metaTableName = "meta"
)

type Lance struct {
	conn  contracts.IConnection
	fresh bool

	// Table writes go through CGO into the Rust core; serialize them to stay
	// on the safe side while parsers run concurrently.
	mu    sync.Mutex
	table contracts.ITable
	dim   int
}

type LanceOption func(*Lance)

// WithFreshReads controls whether every read re-opens the table handle to
// pick up versions written by other processes (on by default). Without it
// a long-lived process — the MCP server — keeps serving the table version
// that was current when it started.
func WithFreshReads(on bool) LanceOption {
	return func(l *Lance) { l.fresh = on }
}

func OpenLance(ctx context.Context, path string, opts ...LanceOption) (Store, error) {
	conn, err := lancedb.Connect(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	l := &Lance{conn: conn, fresh: true}
	for _, opt := range opts {
		opt(l)
	}

	// Open the table if it already exists; otherwise it is created lazily on
	// the first AddDocument, when the vector dimension is known.
	names, err := conn.TableNames(ctx)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if slices.Contains(names, tableName) {
		table, err := conn.OpenTable(ctx, tableName)
		if err != nil {
			conn.Close()
			return nil, err
		}
		l.table = table
	}

	return l, nil
}

func (l *Lance) AddDocument(ctx context.Context, doc Document) error {
	if len(doc.Chunks) == 0 {
		return nil
	}
	if len(doc.Chunks) != len(doc.Vectors) {
		return fmt.Errorf("chunks/vectors mismatch: %d vs %d", len(doc.Chunks), len(doc.Vectors))
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureTable(ctx, len(doc.Vectors[0])); err != nil {
		return err
	}
	if err := l.table.Delete(ctx, pathFilter(doc.Path)); err != nil {
		return fmt.Errorf("delete old chunks of %s: %w", doc.Path, err)
	}

	rec := l.buildRecord(doc)
	defer rec.Release()

	if err := l.table.AddRecords(ctx, []arrow.Record{rec}, nil); err != nil {
		return fmt.Errorf("add chunks of %s: %w", doc.Path, err)
	}
	return nil
}

func (l *Lance) DeleteByPath(ctx context.Context, path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.table == nil {
		return nil
	}
	return l.table.Delete(ctx, pathFilter(path))
}

func (l *Lance) Paths(ctx context.Context) (map[string]int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.refreshTable(ctx); err != nil {
		return nil, err
	}
	if l.table == nil {
		return map[string]int64{}, nil
	}

	rows, err := l.table.Select(ctx, contracts.QueryConfig{Columns: []string{"path", "mtime"}})
	if err != nil {
		return nil, err
	}
	paths := map[string]int64{}
	for _, row := range rows {
		if mt, ok := toInt64(row["mtime"]); ok {
			paths[fmt.Sprint(row["path"])] = mt
		}
	}
	return paths, nil
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int32:
		return int64(x), true
	case float64:
		return int64(x), true
	default:
		return 0, false
	}
}

func (l *Lance) Count(ctx context.Context) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.refreshTable(ctx); err != nil {
		return 0, err
	}
	if l.table == nil {
		return 0, nil
	}
	return l.table.Count(ctx)
}

func (l *Lance) Search(ctx context.Context, vector []float32, limit int) ([]SearchResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.refreshTable(ctx); err != nil {
		return nil, err
	}
	if l.table == nil {
		return nil, nil
	}

	rows, err := l.table.VectorSearch(ctx, "vector", vector, limit)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		r := rowToResult(row)
		// For unit vectors LanceDB's L2 _distance d relates to cosine
		// similarity as cos = 1 - d/2 (d is squared euclidean distance).
		if d, ok := toFloat(row["_distance"]); ok {
			r.Score = 1 - d/2
		}
		results = append(results, r)
	}
	return results, nil
}

// SearchText does lexical search with a SQL ILIKE scan: the SDK's native
// full-text search is not implemented yet in v0.1.2 ("Full-text search is
// not currently supported"), and a personal corpus is small enough to scan.
// Score is the fraction of query words present in the chunk.
func (l *Lance) SearchText(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.refreshTable(ctx); err != nil {
		return nil, err
	}
	if l.table == nil {
		return nil, nil
	}

	words := queryWords(query)
	if len(words) == 0 {
		return nil, nil
	}

	conds := make([]string, len(words))
	for i, w := range words {
		conds[i] = fmt.Sprintf("text ILIKE '%%%s%%'", strings.ReplaceAll(w, "'", "''"))
	}
	// The ILIKE prefilter is substring-based and over-matches wildly on short
	// terms ("pr" is inside "prepare"), so the candidate pool must be large
	// enough to not crowd out the real matches before word-level scoring.
	fetch := 4096
	rows, err := l.table.Select(ctx, contracts.QueryConfig{
		Columns: []string{"id", "path", "section", "text"},
		Where:   strings.Join(conds, " OR "),
		Limit:   &fetch,
	})
	if err != nil {
		return nil, fmt.Errorf("text search: %w", err)
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		r := rowToResult(row)
		chunk := wordSet(r.Text)
		matched := 0
		for _, w := range words {
			if chunk[singular(w)] {
				matched++
			}
		}
		if matched == 0 {
			continue // substring prefilter hit, but no whole word matches
		}
		r.Score = float64(matched) / float64(len(words))
		results = append(results, r)
	}
	slices.SortFunc(results, func(a, b SearchResult) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// queryWords lowercases and deduplicates query terms, dropping stopwords
// (they match everything and only promote noise) and LIKE wildcards so
// user input cannot turn into match-everything patterns.
func queryWords(query string) []string {
	var words []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(query)) {
		// "%" is a LIKE wildcard; "_" (any-one-char) is kept — it still
		// matches itself, so identifiers like dry_run stay searchable.
		w = strings.ReplaceAll(w, "%", "")
		w = strings.Trim(w, ".,!?:;()[]{}«»\"'`")
		if w == "" || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		words = append(words, w)
	}
	return words
}

// wordSet splits text into lowercase words (runs of letters and digits),
// normalized to singular. Scoring on whole words instead of substrings keeps
// a query term like "PR" from matching "prepare" or "approach".
func wordSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		// "_" stays a word character so identifiers like dry_run match whole.
		return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		set[singular(w)] = true
	}
	return set
}

// singular folds a trivial English plural so "PR" matches "PRs". Both sides
// of a comparison go through it, so equality is preserved.
func singular(w string) string {
	if len(w) >= 3 && strings.HasSuffix(w, "s") {
		return w[:len(w)-1]
	}
	return w
}

func rowToResult(row map[string]interface{}) SearchResult {
	r := SearchResult{
		Path:    fmt.Sprint(row["path"]),
		Section: fmt.Sprint(row["section"]),
		Text:    fmt.Sprint(row["text"]),
	}
	id := fmt.Sprint(row["id"])
	if i := strings.LastIndex(id, "#"); i >= 0 {
		fmt.Sscanf(id[i+1:], "%d", &r.Ordinal) //nolint:errcheck // 0 on failure is fine
	}
	return r
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	default:
		return 0, false
	}
}

func (l *Lance) Meta(ctx context.Context) (map[string]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ok, err := l.hasTable(ctx, metaTableName)
	if err != nil || !ok {
		return map[string]string{}, err
	}

	t, err := l.conn.OpenTable(ctx, metaTableName)
	if err != nil {
		return nil, err
	}
	defer t.Close()

	rows, err := t.Select(ctx, contracts.QueryConfig{Columns: []string{"key", "value"}})
	if err != nil {
		return nil, err
	}
	meta := make(map[string]string, len(rows))
	for _, row := range rows {
		meta[fmt.Sprint(row["key"])] = fmt.Sprint(row["value"])
	}
	return meta, nil
}

// SetMeta replaces the whole metadata table; it is tiny and written rarely.
func (l *Lance) SetMeta(ctx context.Context, meta map[string]string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.dropTable(ctx, metaTableName); err != nil {
		return err
	}

	schema, err := lancedb.NewSchemaBuilder().
		AddStringField("key", false).
		AddStringField("value", false).
		Build()
	if err != nil {
		return err
	}
	t, err := l.conn.CreateTable(ctx, metaTableName, schema)
	if err != nil {
		return err
	}
	defer t.Close()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "key", Type: arrow.BinaryTypes.String},
		{Name: "value", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	defer b.Release()
	for _, k := range slices.Sorted(maps.Keys(meta)) {
		b.Field(0).(*array.StringBuilder).Append(k)
		b.Field(1).(*array.StringBuilder).Append(meta[k])
	}
	rec := b.NewRecord()
	defer rec.Release()

	return t.AddRecords(ctx, []arrow.Record{rec}, nil)
}

func (l *Lance) Reset(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.table != nil {
		l.table.Close()
		l.table = nil
	}
	l.dim = 0

	if err := l.dropTable(ctx, tableName); err != nil {
		return err
	}
	return l.dropTable(ctx, metaTableName)
}

// refreshTable re-opens the table handle so reads see the latest table
// version: a handle stays pinned to the version that was current when it
// was opened, and the SDK has no checkout-latest yet. Also picks up a
// table created by another process after this store was opened.
// No-op unless fresh reads are enabled; must hold l.mu.
func (l *Lance) refreshTable(ctx context.Context) error {
	if !l.fresh {
		return nil
	}
	ok, err := l.hasTable(ctx, tableName)
	if err != nil {
		return err
	}
	if l.table != nil {
		l.table.Close()
		l.table = nil
	}
	if !ok {
		return nil
	}
	table, err := l.conn.OpenTable(ctx, tableName)
	if err != nil {
		return err
	}
	l.table = table
	return nil
}

// hasTable reports whether a table exists; must hold l.mu.
func (l *Lance) hasTable(ctx context.Context, name string) (bool, error) {
	names, err := l.conn.TableNames(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(names, name), nil
}

// dropTable removes a table if it exists; must hold l.mu.
func (l *Lance) dropTable(ctx context.Context, name string) error {
	ok, err := l.hasTable(ctx, name)
	if err != nil || !ok {
		return err
	}
	return l.conn.DropTable(ctx, name)
}

func (l *Lance) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.table != nil {
		l.table.Close()
	}
	return l.conn.Close()
}

// ensureTable creates the chunks table on first write; must hold l.mu.
func (l *Lance) ensureTable(ctx context.Context, dim int) error {
	if l.table != nil {
		if l.dim != 0 && l.dim != dim {
			return fmt.Errorf("vector dimension mismatch: table has %d, got %d", l.dim, dim)
		}
		l.dim = dim
		return nil
	}

	schema, err := lancedb.NewSchemaBuilder().
		AddStringField("id", false).
		AddStringField("path", false).
		AddStringField("section", true).
		AddStringField("text", false).
		AddVectorField("vector", dim, contracts.VectorDataTypeFloat32, false).
		AddInt64Field("mtime", false).
		Build()
	if err != nil {
		return err
	}

	table, err := l.conn.CreateTable(ctx, tableName, schema)
	if err != nil {
		return err
	}
	l.table = table
	l.dim = dim
	return nil
}

func (l *Lance) buildRecord(doc Document) arrow.Record {
	dim := len(doc.Vectors[0])
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.BinaryTypes.String},
		{Name: "path", Type: arrow.BinaryTypes.String},
		{Name: "section", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "text", Type: arrow.BinaryTypes.String},
		{Name: "vector", Type: arrow.FixedSizeListOf(int32(dim), arrow.PrimitiveTypes.Float32)},
		{Name: "mtime", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()

	ids := b.Field(0).(*array.StringBuilder)
	paths := b.Field(1).(*array.StringBuilder)
	sections := b.Field(2).(*array.StringBuilder)
	texts := b.Field(3).(*array.StringBuilder)
	vectors := b.Field(4).(*array.FixedSizeListBuilder)
	values := vectors.ValueBuilder().(*array.Float32Builder)
	mtimes := b.Field(5).(*array.Int64Builder)

	for i, c := range doc.Chunks {
		ids.Append(fmt.Sprintf("%s#%d", doc.Path, c.Ordinal))
		paths.Append(doc.Path)
		sections.Append(c.Section)
		texts.Append(c.Text)
		vectors.Append(true)
		values.AppendValues(doc.Vectors[i], nil)
		mtimes.Append(doc.MTime)
	}

	return b.NewRecord()
}

func pathFilter(path string) string {
	return fmt.Sprintf("path = '%s'", strings.ReplaceAll(path, "'", "''"))
}
