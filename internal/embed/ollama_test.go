package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeOllama spins up an httptest server pretending to be Ollama.
func fakeOllama(t *testing.T, handler http.HandlerFunc) *Ollama {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewOllama(srv.URL, "bge-m3")
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

func TestOllamaEmbedServerError(t *testing.T) {
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": `model "bge-m3" not found`})
	})

	_, err := o.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want server error message surfaced", err)
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

	_, err := o.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "ollama serve") {
		t.Fatalf("err = %v, want hint about ollama serve", err)
	}
}

func TestOllamaID(t *testing.T) {
	if got := NewOllama("http://x", "bge-m3").ID(); got != "ollama/bge-m3" {
		t.Errorf("ID() = %q, want ollama/bge-m3", got)
	}
}
