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
}

// DefaultDBPath returns the XDG data location for the database
// (~/.local/share/qazyna/db.lance), so an installed binary finds the same
// database from any working directory. $XDG_DATA_HOME is honored.
func DefaultDBPath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "data/qazyna.lance" // last resort: relative path
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "qazyna", "db.lance")
}
