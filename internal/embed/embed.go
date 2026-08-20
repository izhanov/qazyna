package embed

import "context"

// Embedder turns texts into vectors of a fixed dimension.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}
