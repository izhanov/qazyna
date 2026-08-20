package search

import (
	"testing"

	"qazyna/internal/store"
)

func TestRRFMerge(t *testing.T) {
	a := []store.SearchResult{
		{Path: "a.md", Ordinal: 1},
		{Path: "b.md", Ordinal: 2},
	}
	b := []store.SearchResult{
		{Path: "b.md", Ordinal: 2},
		{Path: "c.md", Ordinal: 3},
	}

	merged := rrfMerge(a, b, 5)

	if len(merged) != 3 {
		t.Fatalf("merged %d results, want 3 (b deduplicated): %+v", len(merged), merged)
	}
	if merged[0].Path != "b.md" {
		t.Errorf("first = %s, want b.md (present in both lists)", merged[0].Path)
	}
	// b.md is #2 semantically and #1 lexically, so its fused score is close
	// to but below the perfect 1.0 of a result ranked #1 in both lists.
	if merged[0].Score < 0.95 || merged[0].Score >= 1 {
		t.Errorf("fused score = %f, want just below 1", merged[0].Score)
	}
	if both := rrfMerge(a[:1], a[:1], 1); both[0].Score != 1 {
		t.Errorf("both-#1 score = %f, want exactly 1", both[0].Score)
	}
	if merged[1].Path != "a.md" || merged[2].Path != "c.md" {
		t.Errorf("tail order = %s, %s; want a.md, c.md", merged[1].Path, merged[2].Path)
	}

	if got := rrfMerge(a, b, 2); len(got) != 2 {
		t.Errorf("limit ignored: got %d results", len(got))
	}
}

func TestSearchUnknownMode(t *testing.T) {
	if _, err := Search(t.Context(), nil, nil, "q", Options{Mode: "telepathy"}); err == nil {
		t.Fatal("expected unknown mode error")
	}
}
