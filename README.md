# qazyna

**Qazyna** (Kazakh «қазына» — treasury, trove) is a semantic search service for local files.
Files are split into chunks, each chunk gets an embedding computed locally (Ollama),
and vectors are stored in [LanceDB](https://lancedb.com). Search is vector-based —
by meaning, not by substring.

Supported formats: Markdown (in progress). Planned: PDF, XML, CSV, JPEG, PNG, video.

## Requirements

- Go 1.26+
- macOS, Linux, or Windows (MinGW/Git Bash) — the `Makefile` detects the platform
  automatically, check it with `make platform-info`
- [Ollama](https://ollama.com) with an embedding model (default: `nomic-embed-text`):

```sh
brew install ollama
ollama pull nomic-embed-text
```

## Installation

The LanceDB Go SDK works through CGO on top of a native library. The first
`make build` downloads it automatically (into `lib/` and `include/`, not tracked
by git) and then compiles the binary:

```sh
make build
```

Useful extra targets: `make deps` (download the native libraries only),
`make clean-deps` (remove them, e.g. before a version bump).

The binary lands in `bin/qazyna`. The CGO flags for linking the native library
are declared in `internal/store/cgo_flags.go` (via `#cgo` directives with
`${SRCDIR}`-relative paths), so plain `go build ./...` and `go test ./...` work
without any environment variables — as long as the native libraries are
downloaded (`make deps`).

## Usage

Index files and directories (any mix, overlaps are deduplicated):

```sh
./bin/qazyna index ./notes ./docs README.md
```

Search by meaning:

```sh
./bin/qazyna search "how to set up ollama"
```

The database remembers which embedding model built it and refuses to mix
vectors from different models. After changing `--embed-model`, rebuild from
scratch:

```sh
./bin/qazyna reindex ./notes
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--store` | `lance` | storage backend |
| `--db` | `data/qazyna.lance` | path to the LanceDB database |
| `--embedder` | `ollama` | embedding backend |
| `--ollama-url` | `http://localhost:11434` | Ollama server URL |
| `--embed-model` | `nomic-embed-text` | embedding model |

## Architecture

```
file ──▶ Parser (by extension) ──▶ chunks ──▶ Embedder (Ollama) ──▶ vectors ──▶ Store (LanceDB)

query ──▶ Embedder ──▶ vector search in Store ──▶ top-N chunks + file paths
```

The core (`internal/cli`) knows nothing about concrete implementations: backends are
registered in `cmd/qazyna/main.go` via functional options (`cli.WithDefaultStore()` etc.),
and the user picks which one to use with the `--store` / `--embedder` flags.

```
cmd/qazyna/       entry point
internal/cli/     CLI wiring (urfave/cli/v3), backend registries
internal/config/  configuration
internal/store/   Store interface + LanceDB implementation
```

## Status

- [x] CLI skeleton, LanceDB connection
- [x] Markdown parser (chunks by headings/paragraphs)
- [ ] Embeddings via Ollama
- [x] `index`: parse → embed → write to LanceDB (with the fake embedder for now)
- [ ] `search`: vector search
- [ ] HTTP API
- [ ] PDF, XML, CSV, images, video
