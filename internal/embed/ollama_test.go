package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeOllama spins up an httptest server pretending to be Ollama.
// Retries are near-instant so tests stay fast.
func fakeOllama(t *testing.T, handler http.HandlerFunc) *Ollama {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	o := NewOllama(srv.URL, "bge-m3")
	o.backoff = time.Millisecond
	return o
}

func TestOllamaEmbed(t *testing.T) {
	var gotReq embedRequest
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %s, want /api/embed", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1, 0.2}, {0.3, 0.4}},
		})
	})

	vecs, err := o.Embed(context.Background(), []string{"раз", "два"})
	if err != nil {
		t.Fatal(err)
	}

	if gotReq.Model != "bge-m3" {
		t.Errorf("request model = %q, want bge-m3", gotReq.Model)
	}
	if len(gotReq.Input) != 2 || gotReq.Input[0] != "раз" {
		t.Errorf("request input = %v", gotReq.Input)
	}
	if len(vecs) != 2 || vecs[1][1] != 0.4 {
		t.Errorf("vecs = %v", vecs)
	}
}

func TestOllamaEmbedClientErrorNotRetried(t *testing.T) {
	attempts := 0
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": `model "bge-m3" not found`})
	})

	_, err := o.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want server error message surfaced", err)
	}
	if attempts != 1 {
		t.Errorf("4xx retried %d times, unknown model will not appear on retry", attempts)
	}
}

func TestOllamaEmbedRetriesServerErrors(t *testing.T) {
	attempts := 0
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "hiccup"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1}}})
	})

	vecs, err := o.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if len(vecs) != 1 {
		t.Errorf("vecs = %v", vecs)
	}
}

func TestOllamaEmbedGivesUpAfterMaxAttempts(t *testing.T) {
	attempts := 0
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "still down"})
	})

	_, err := o.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("err = %v, want attempts exhausted error", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestOllamaEmbedRespectsCancellation(t *testing.T) {
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "down"})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := o.Embed(ctx, []string{"x"})
	if err == nil {
		t.Fatal("expected an error with a cancelled context")
	}
}

func TestOllamaEmbedCountMismatch(t *testing.T) {
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}},
		})
	})

	_, err := o.Embed(context.Background(), []string{"x", "y"})
	if err == nil || !strings.Contains(err.Error(), "1 embeddings for 2 texts") {
		t.Fatalf("err = %v, want count mismatch error", err)
	}
}

func TestOllamaEmbedConnectionRefused(t *testing.T) {
	o := NewOllama("http://127.0.0.1:1", "bge-m3") // nothing listens there
	o.backoff = time.Millisecond

	_, err := o.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "ollama serve") {
		t.Fatalf("err = %v, want hint about ollama serve", err)
	}
}

func TestOllamaEmbedBatches(t *testing.T) {
	var batchSizes []int
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		batchSizes = append(batchSizes, len(req.Input))
		// Echo the text's ordinal back as the vector so the test can
		// verify the batches are glued together in order.
		vecs := make([][]float32, len(req.Input))
		for i, text := range req.Input {
			vecs[i] = []float32{float32(text[0])}
		}
		json.NewEncoder(w).Encode(map[string]any{"embeddings": vecs})
	})
	o.batch = 3

	texts := make([]string, 7)
	for i := range texts {
		texts[i] = string(rune('a' + i))
	}
	vecs, err := o.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}

	if want := []int{3, 3, 1}; !slices.Equal(batchSizes, want) {
		t.Errorf("batch sizes = %v, want %v", batchSizes, want)
	}
	if len(vecs) != 7 {
		t.Fatalf("got %d vectors, want 7", len(vecs))
	}
	for i, v := range vecs {
		if v[0] != float32('a'+i) {
			t.Errorf("vector %d = %v, out of order", i, v)
		}
	}
}

func TestOllamaID(t *testing.T) {
	if got := NewOllama("http://x", "bge-m3").ID(); got != "ollama/bge-m3" {
		t.Errorf("ID() = %q, want ollama/bge-m3", got)
	}
}
