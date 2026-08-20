package store

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

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
	conn contracts.IConnection

	// Table writes go through CGO into the Rust core; serialize them to stay
	// on the safe side while parsers run concurrently.
	mu    sync.Mutex
	table contracts.ITable
	dim   int
}

func OpenLance(ctx context.Context, path string) (Store, error) {
	conn, err := lancedb.Connect(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	l := &Lance{conn: conn}

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

func (l *Lance) Count(ctx context.Context) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.table == nil {
		return 0, nil
	}
	return l.table.Count(ctx)
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
