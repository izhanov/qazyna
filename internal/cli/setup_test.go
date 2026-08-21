package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHasModel(t *testing.T) {
	models := []string{"bge-m3:latest", "llama3:8b"}

	for _, tc := range []struct {
		want string
		ok   bool
	}{
		{"bge-m3", true},
		{"bge-m3:latest", true},
		{"llama3", false}, // only the 8b tag is present, not latest
		{"llama3:8b", true},
		{"mistral", false},
	} {
		if got := hasModel(models, tc.want); got != tc.ok {
			t.Errorf("hasModel(%q) = %v, want %v", tc.want, got, tc.ok)
		}
	}
}

func TestInstalledModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"models":[{"name":"bge-m3:latest"},{"name":"llama3:8b"}]}`))
	}))
	defer srv.Close()

	models, err := installedModels(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("installedModels: %v", err)
	}
	if len(models) != 2 || models[0] != "bge-m3:latest" {
		t.Fatalf("unexpected models: %v", models)
	}
}

func TestInstalledModelsServerDown(t *testing.T) {
	if _, err := installedModels(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
