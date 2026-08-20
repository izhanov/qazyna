package store

import (
	"context"

	"qazyna/internal/parser"
)

// Document is one indexed file: its chunks plus a vector per chunk.
type Document struct {
	Path    string
	MTime   int64
	Chunks  []parser.Chunk
	Vectors [][]float32
}

// Store is a storage backend for indexed chunks.
type Store interface {
	// AddDocument writes all chunks of one file. It replaces any previously
	// stored chunks for the same path, so re-indexing does not duplicate.
	AddDocument(ctx context.Context, doc Document) error
	DeleteByPath(ctx context.Context, path string) error
	Count(ctx context.Context) (int64, error)
	Close() error
}
