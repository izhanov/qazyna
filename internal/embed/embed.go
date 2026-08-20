package embed

import "context"

// Embedder turns texts into vectors of a fixed dimension.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// ID identifies the vector space, e.g. "ollama/bge-m3". Vectors from
	// different IDs are incomparable and must never share a table.
	ID() string
}
