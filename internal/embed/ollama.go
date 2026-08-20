package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Ollama embeds texts via a local Ollama server (POST /api/embed).
// Safe for concurrent use.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama creates a client for the given server URL and embedding model.
// The client has no own timeout: cancellation comes from the context, and the
// first request may legitimately take long while the model loads into memory.
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

func (o *Ollama) ID() string { return "ollama/" + o.model }

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error"`
}

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: o.model, Input: texts})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama at %s: %w (is `ollama serve` running?)", o.baseURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ollama: unexpected response (HTTP %d): %.200s", resp.StatusCode, raw)
	}
	if resp.StatusCode != http.StatusOK || parsed.Error != "" {
		return nil, fmt.Errorf("ollama (HTTP %d): %s", resp.StatusCode, parsed.Error)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d texts", len(parsed.Embeddings), len(texts))
	}
	for i, v := range parsed.Embeddings {
		if len(v) == 0 {
			return nil, fmt.Errorf("ollama returned an empty embedding for text %d", i)
		}
	}
	return parsed.Embeddings, nil
}
