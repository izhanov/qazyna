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

The LanceDB Go SDK works through CGO on top of a native library, so download it
before the first build (it goes into `lib/` and `include/`, not tracked by git):

```sh
curl -sSL https://raw.githubusercontent.com/lancedb/lancedb-go/main/scripts/download-artifacts.sh | bash -s v0.1.2
make build
```

The binary lands in `bin/qazyna`. All required CGO flags are set in the `Makefile`;
if you build directly with `go build`, export them yourself:

```sh
# example for macOS arm64; see `make platform-info` for your platform's values
export CGO_CFLAGS="-I$(pwd)/include"
export CGO_LDFLAGS="$(pwd)/lib/darwin_arm64/liblancedb_go.a -framework Security -framework CoreFoundation"
```

## Usage

Index a file or a directory:

```sh
./bin/qazyna index ./notes
```

Search by meaning:

```sh
./bin/qazyna search "how to set up ollama"
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
- [ ] Markdown parser (chunks by headings/paragraphs)
- [ ] Embeddings via Ollama
- [ ] `index`: parse → embed → write to LanceDB
- [ ] `search`: vector search
- [ ] HTTP API
- [ ] PDF, XML, CSV, images, video
