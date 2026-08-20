package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ollama embeds texts via a local Ollama server (POST /api/embed).
// Safe for concurrent use. Transient failures (network errors, 5xx) are
// retried with backoff; client errors like an unknown model are not.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client

	timeout  time.Duration // per-request deadline
	attempts int
	backoff  time.Duration // first retry delay, doubles per attempt
}

// NewOllama creates a client for the given server URL and embedding model.
// The generous per-request timeout covers the first call loading the model
// into memory; cancellation still comes from the caller's context.
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		baseURL:  strings.TrimRight(baseURL, "/"),
		model:    model,
		client:   &http.Client{},
		timeout:  5 * time.Minute,
		attempts: 3,
		backoff:  time.Second,
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
	var lastErr error
	for attempt := range o.attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(o.backoff << (attempt - 1)):
			}
		}

		vectors, err, retryable := o.embedOnce(ctx, texts)
		if err == nil {
			return vectors, nil
		}
		if ctx.Err() != nil {
			return nil, err // the caller is shutting down, stop trying
		}
		if !retryable {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("after %d attempts: %w", o.attempts, lastErr)
}

func (o *Ollama) embedOnce(ctx context.Context, texts []string) (_ [][]float32, err error, retryable bool) {
	reqCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	body, err := json.Marshal(embedRequest{Model: o.model, Input: texts})
	if err != nil {
		return nil, err, false
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		// Network errors and our own timeout are worth retrying.
		return nil, fmt.Errorf("ollama at %s: %w (is `ollama serve` running?)", o.baseURL, err), true
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err, true
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ollama: unexpected response (HTTP %d): %.200s", resp.StatusCode, raw), resp.StatusCode >= 500
	}
	if resp.StatusCode != http.StatusOK || parsed.Error != "" {
		// 5xx is the server hiccuping; 4xx (unknown model etc.) will not
		// get better on retry.
		return nil, fmt.Errorf("ollama (HTTP %d): %s", resp.StatusCode, parsed.Error), resp.StatusCode >= 500
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d texts", len(parsed.Embeddings), len(texts)), false
	}
	for i, v := range parsed.Embeddings {
		if len(v) == 0 {
			return nil, fmt.Errorf("ollama returned an empty embedding for text %d", i), false
		}
	}
	return parsed.Embeddings, nil, false
}
