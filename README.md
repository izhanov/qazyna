# qazyna

**Qazyna** (Kazakh «қазына» — treasury, trove) is a semantic search service for local files.
Files are split into chunks, each chunk gets an embedding computed locally (Ollama),
and vectors are stored in [LanceDB](https://lancedb.com). Search is vector-based —
by meaning, not by substring.

Supported formats: Markdown, PDF (text layer), XML, CSV/TSV. Planned: JPEG, PNG, video.

## Requirements

- Go 1.26+
- macOS, Linux, or Windows (MinGW/Git Bash) — the `Makefile` detects the platform
  automatically, check it with `make platform-info`
- Poppler for PDF parsing: `brew install poppler` (provides `pdftotext`)
- [Ollama](https://ollama.com) with an embedding model (default: `bge-m3` —
  multilingual, works well for Cyrillic):

```sh
brew install ollama
ollama pull bge-m3
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

The binary lands in `bin/qazyna`. To install it into your PATH (via `go
install`, so it works from any directory):

```sh
make install
```

The database lives in `~/.local/share/qazyna/db.lance` by default
(`$XDG_DATA_HOME` is honored), so every directory shares one index. Every
flag has an env twin: `QAZYNA_DB`, `QAZYNA_STORE`, `QAZYNA_EMBEDDER`,
`QAZYNA_OLLAMA_URL`, `QAZYNA_EMBED_MODEL`. The CGO flags for linking the native library
are declared in `internal/store/cgo_flags.go` (via `#cgo` directives with
`${SRCDIR}`-relative paths), so plain `go build ./...` and `go test ./...` work
without any environment variables — as long as the native libraries are
downloaded (`make deps`).

## Usage

Index files and directories (any mix, overlaps are deduplicated):

```sh
./bin/qazyna index ./notes ./docs README.md
```

Search:

```sh
./bin/qazyna search "how to set up ollama"
./bin/qazyna search --limit 10 --json "vector database"
./bin/qazyna search --mode text "лениво"        # exact-word lookup
```

By default the search is hybrid: a semantic (vector) and a lexical (exact
words) lookup run concurrently and are fused with Reciprocal Rank Fusion, so
both question-like queries and grep-style keyword lookups work. `--mode
vector` or `--mode text` selects a single half.

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
| `--db` | `~/.local/share/qazyna/db.lance` | path to the LanceDB database |
| `--embedder` | `ollama` | embedding backend |
| `--ollama-url` | `http://localhost:11434` | Ollama server URL |
| `--embed-model` | `bge-m3` | embedding model |

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
- [x] Embeddings via Ollama
- [x] `index`: parse → embed → write to LanceDB
- [x] `search`: hybrid vector + lexical search with RRF (`--mode`, `--limit`, `--json`)
- [x] PDF parser (text layer via `pdftotext`; scanned PDFs need OCR, later)
- [x] XML parser (subtree chunks, tag names become sections and labels)
- [x] CSV/TSV parser (rows with headers woven in, packed up to chunk size)
- [ ] HTTP API
- [ ] Images (OCR/captioning), video
