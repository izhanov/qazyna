# qazyna

**Qazyna** (Kazakh «қазына» — treasury, trove) is a semantic search service for local files.
Files are split into chunks, each chunk gets an embedding computed locally (Ollama),
and vectors are stored in [LanceDB](https://lancedb.com). Search is vector-based —
by meaning, not by substring.

Supported formats: Markdown, PDF (text layer), XML, CSV/TSV, DOCX (Word documents). Planned: JPEG, PNG, video; .doc (legacy binary format).

**Embeddings: Ollama only, for now.** Qazyna computes embeddings exclusively
through a local [Ollama](https://ollama.com) server — no OpenAI, no cloud
APIs, nothing leaves your machine. The default and tested model is
[`bge-m3`](https://ollama.com/library/bge-m3) (multilingual, good for
Cyrillic). Other Ollama embedding models can be selected with
`--embed-model`, but `bge-m3` is what the project is developed against.
Switching models requires a full `qazyna reindex`.

## Requirements

- Go 1.26+
- macOS, Linux, or Windows (MinGW/Git Bash) — the `Makefile` detects the platform
  automatically, check it with `make platform-info`
- Poppler for PDF parsing (provides `pdftotext`)
- [Ollama](https://ollama.com) with an embedding model (default: `bge-m3` —
  multilingual, works well for Cyrillic)

### macOS

```sh
brew install poppler ollama
ollama pull bge-m3
ollama serve  # run in background
```

### Linux (Ubuntu/Debian)

```sh
# Poppler
sudo apt-get update && sudo apt-get install -y poppler-utils

# Ollama
curl -fsSL https://ollama.ai/install.sh | sh
ollama pull bge-m3
ollama serve  # run in background
```

### Windows

Install [Ollama for Windows](https://ollama.com/download/windows) and [Poppler](https://github.com/oschwartz10612/poppler-windows/releases/) manually, or use WSL2 with the Linux instructions above.

## Installation

### Quick install (prebuilt binary)

```sh
curl -fsSL https://raw.githubusercontent.com/izhanov/qazyna/main/install.sh | sh
qazyna setup
```

`setup` finishes the job in one command: checks Ollama and pulls the
embedding model, checks poppler, registers the MCP server in Claude Code and
installs the agent skill. Anything it cannot do itself (like installing
Ollama) becomes a printed hint — fix and re-run.

### From source

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
`QAZYNA_OLLAMA_URL`, `QAZYNA_EMBED_MODEL`, `QAZYNA_FRESH_READS`.
Reads re-open the table by default so long-lived processes (the MCP server)
see new indexing without a restart; `--fresh-reads=false` pins the version
that was current at startup. The CGO flags for linking the native library
are declared in `internal/store/cgo_flags.go` (via `#cgo` directives with
`${SRCDIR}`-relative paths), so plain `go build ./...` and `go test ./...` work
without any environment variables — as long as the native libraries are
downloaded (`make deps`).

## Usage

Index files and directories (any mix, overlaps are deduplicated):

```sh
./bin/qazyna index ./notes ./docs README.md
```

Indexing is incremental: unchanged files (by mtime) are skipped, files that
vanished from the given directories are dropped from the index. `--force`
re-indexes the given paths even when mtimes are unchanged.

See what is in the index (`--json` for machine-readable output):

```sh
./bin/qazyna list
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
vector` or `--mode text` selects a single half. Each chunk is embedded
together with its file name and heading trail, so queries mentioning either
rank the right files higher.

Exclusions: embeddings cannot express "not about X" — mentioning X in a
query *attracts* it. Use minus-operators or a path filter instead:

```sh
./bin/qazyna search 'improve search -"evil martians" -preprod'
./bin/qazyna search --exclude-path uscreen "deploy process"
```

Measure search quality against golden sets of `query → expected file`
cases. Golden sets live next to the database
(`~/.local/share/qazyna/evals`); `--evals` or `QAZYNA_EVALS` points
elsewhere. With no argument every set in that directory runs:

```sh
./bin/qazyna eval                             # recall@5 and MRR, all sets
./bin/qazyna eval --mode vector golden.yaml   # one file, one search half
```

A golden set is a YAML list of cases; `expect` entries are file-level
path suffixes, so chunking changes don't invalidate the set:

```yaml
- query: "how do I deploy to preprod"
  expect: ["skills/deploy/SKILL.md"]
```

### MCP server (search from AI agents)

`qazyna mcp` serves the index over the Model Context Protocol (stdio), so
agents like Claude Code can search your files themselves. Read-only by
design: three tools — `search_notes`, `index_status` and `list_files`
(what is indexed, with mtimes); indexing stays with you in the terminal. To register in Claude Code:

```sh
claude mcp add qazyna -- qazyna mcp
```

### Claude Code skill (teach agents the search loop)

The MCP server gives agents the tools; the bundled skill teaches them the
workflow — search the index first, read only what search surfaced, verify
freshness, suggest reindexing after edits. Install it once:

```sh
qazyna skill install
```

The skill lands in `~/.claude/skills/qazyna-search/` and loads in every
Claude Code session, in any repository. `qazyna skill show` prints it,
`qazyna skill uninstall` removes it. To make agents reach for it proactively,
add one line to your `~/.claude/CLAUDE.md`:

> Before tasks involving my notes, docs or style guides, use the qazyna-search skill.

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
- [x] DOCX parser (Word documents with heading hierarchy)
- [x] MCP server (`qazyna mcp`): read-only search for AI agents over stdio
- [ ] Images (OCR/captioning), video; .doc (legacy binary format)
