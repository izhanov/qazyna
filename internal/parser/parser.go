package parser

import (
	"context"
	"path/filepath"
	"strings"
)

// Chunk is a fragment of a parsed file, the unit of indexing and search.
type Chunk struct {
	Section string // heading trail, e.g. "Install > macOS"
	Text    string
	Ordinal int // position within the file, keeps chunk IDs stable
}

type Parser interface {
	Parse(ctx context.Context, path string) ([]Chunk, error)
	Extensions() []string // ".md", ".markdown"
}

// Registry maps file extensions to parsers.
type Registry struct {
	byExt map[string]Parser
}

func NewRegistry() *Registry {
	return &Registry{byExt: map[string]Parser{}}
}

func (r *Registry) Register(p Parser) {
	for _, ext := range p.Extensions() {
		r.byExt[strings.ToLower(ext)] = p
	}
}

func (r *Registry) For(path string) (Parser, bool) {
	p, ok := r.byExt[strings.ToLower(filepath.Ext(path))]
	return p, ok
}
