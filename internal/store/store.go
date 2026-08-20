package store

// Store is a storage backend for indexed chunks.
type Store interface {
	Close() error
}
