package embed

import (
	"context"
	"math"
)

// Embedder turns texts into vectors of a fixed dimension.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// ID identifies the vector space, e.g. "ollama/bge-m3". Vectors from
	// different IDs are incomparable and must never share a table.
	ID() string
}

// Normalize scales every vector to unit length in place. With unit vectors
// the L2 distance the store returns maps directly to cosine similarity,
// no matter what the model emits.
func Normalize(vectors [][]float32) {
	for _, v := range vectors {
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			continue
		}
		n := float32(math.Sqrt(norm))
		for i := range v {
			v[i] /= n
		}
	}
}
