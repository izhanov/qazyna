package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	StoreName    string
	DBPath       string
	EmbedderName string
	OllamaURL    string
	EmbedModel   string
	EvalsDir     string
	FreshReads   bool
}

// dataDir returns the XDG data location for qazyna (~/.local/share/qazyna),
// so an installed binary finds the same data from any working directory.
// $XDG_DATA_HOME is honored. Empty when no home directory can be determined.
func dataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "qazyna")
}

// DefaultDBPath is where the database lives: ~/.local/share/qazyna/db.lance.
func DefaultDBPath() string {
	dir := dataDir()
	if dir == "" {
		return "data/qazyna.lance" // last resort: relative path
	}
	return filepath.Join(dir, "db.lance")
}

// DefaultEvalsDir is where golden sets for `qazyna eval` live, next to the
// database: ~/.local/share/qazyna/evals.
func DefaultEvalsDir() string {
	dir := dataDir()
	if dir == "" {
		return "data/evals" // last resort: relative path
	}
	return filepath.Join(dir, "evals")
}
