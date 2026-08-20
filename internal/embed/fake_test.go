package embed

import (
	"context"
	"math"
	"testing"
)

func TestFakeDeterministic(t *testing.T) {
	f := Fake{Dim: 8}

	a, err := f.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}

	for i := range a {
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatalf("vectors for the same text differ at [%d][%d]", i, j)
			}
		}
	}
}

func TestFakeDistinctTexts(t *testing.T) {
	f := Fake{Dim: 8}

	vecs, err := f.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	same := true
	for j := range vecs[0] {
		if vecs[0][j] != vecs[1][j] {
			same = false
			break
		}
	}
	if same {
		t.Error("different texts produced identical vectors")
	}
}

func TestFakeDimensionsAndNorm(t *testing.T) {
	f := Fake{Dim: 16}
	if f.Dimensions() != 16 {
		t.Fatalf("Dimensions() = %d, want 16", f.Dimensions())
	}

	vecs, err := f.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs[0]) != 16 {
		t.Fatalf("vector length = %d, want 16", len(vecs[0]))
	}

	var norm float64
	for _, v := range vecs[0] {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1) > 1e-3 {
		t.Errorf("vector norm = %f, want ~1", math.Sqrt(norm))
	}
}
