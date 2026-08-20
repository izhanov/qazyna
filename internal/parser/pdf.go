package parser

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PDF extracts the text layer of a PDF via Poppler's pdftotext.
// Scanned pages (no text layer) yield no chunks; OCR is a future step.
type PDF struct{}

func (PDF) Extensions() []string { return []string{".pdf"} }

func (PDF) Parse(ctx context.Context, path string) ([]Chunk, error) {
	// "-" sends the text to stdout; pages come separated by form feed.
	cmd := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", path, "-")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.Error); ok {
			return nil, fmt.Errorf("pdftotext not found — install Poppler (brew install poppler): %w", err)
		}
		return nil, fmt.Errorf("pdftotext %s: %s: %w", path, strings.TrimSpace(stderr.String()), err)
	}

	var (
		chunks  []Chunk
		ordinal int
	)
	for i, page := range strings.Split(out.String(), "\f") {
		text := strings.TrimSpace(page)
		if text == "" {
			continue // blank or scanned page — nothing to index
		}
		section := fmt.Sprintf("p. %d", i+1)
		for _, part := range splitByParagraphs(text) {
			chunks = append(chunks, Chunk{Section: section, Text: part, Ordinal: ordinal})
			ordinal++
		}
	}
	return chunks, nil
}
