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

// SearchResult is one found chunk with its similarity score.
type SearchResult struct {
	Path    string  `json:"path"`
	Section string  `json:"section,omitempty"`
	Text    string  `json:"text"`
	Ordinal int     `json:"ordinal"`
	Score   float64 `json:"score"` // cosine similarity, 1 = identical
}

// Store is a storage backend for indexed chunks.
type Store interface {
	// AddDocument writes all chunks of one file. It replaces any previously
	// stored chunks for the same path, so re-indexing does not duplicate.
	AddDocument(ctx context.Context, doc Document) error
	DeleteByPath(ctx context.Context, path string) error
	Count(ctx context.Context) (int64, error)

	// Search returns the limit nearest chunks to the query vector,
	// best match first. Vectors are expected to be unit-normalized.
	Search(ctx context.Context, vector []float32, limit int) ([]SearchResult, error)

	// Meta returns database-level metadata, e.g. which embedder built the
	// index. Empty map for a fresh database.
	Meta(ctx context.Context) (map[string]string, error)
	SetMeta(ctx context.Context, meta map[string]string) error

	// Reset wipes all indexed data and metadata.
	Reset(ctx context.Context) error

	Close() error
}
