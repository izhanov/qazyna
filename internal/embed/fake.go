package embed

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
)

// Fake produces deterministic pseudo-random vectors derived from the text
// hash. Search over them is meaningless, but the vectors are valid, so the
// whole indexing pipeline can run and be tested without a real model.
type Fake struct {
	Dim int
}

func (f Fake) ID() string { return fmt.Sprintf("fake/%d", f.Dim) }

func (f Fake) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		h := fnv.New64a()
		h.Write([]byte(text))
		state := h.Sum64()

		vec := make([]float32, f.Dim)
		var norm float64
		for j := range vec {
			// xorshift64: cheap deterministic PRNG seeded by the text hash
			state ^= state << 13
			state ^= state >> 7
			state ^= state << 17
			vec[j] = float32(int64(state)) / float32(math.MaxInt64)
			norm += float64(vec[j]) * float64(vec[j])
		}
		if norm > 0 {
			n := float32(math.Sqrt(norm))
			for j := range vec {
				vec[j] /= n
			}
		}
		vectors[i] = vec
	}
	return vectors, nil
}
