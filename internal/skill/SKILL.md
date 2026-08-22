---
name: qazyna-search
description: Use qazyna semantic search (MCP) as the first research step for any task. Search the local index for relevant notes, docs, and files before reading or grepping blindly. Works in any repo.
---

# Qazyna search in your work loop

The user runs **qazyna** — a local semantic search over their files (notes,
docs, style guides, project files). It is available in every session via MCP
tools:

- `mcp__qazyna__search_notes` — hybrid vector + full-text search
- `mcp__qazyna__list_files` — what is indexed, with mtimes
- `mcp__qazyna__index_status` — index health (chunk count, embed model)

These are deferred tools: load them with
`ToolSearch "select:mcp__qazyna__search_notes,mcp__qazyna__list_files"` first.

## The loop

For any task that might touch knowledge the user has indexed (their notes,
style guides, project docs, reference material):

1. **Search before reading.** Query the index with the task's key concepts.
   One or two searches cost almost nothing and often surface the exact file
   and section you need.

2. **Pick the mode:**
   - default (hybrid) — best for most queries
   - `vector` — conceptual questions ("how does billing work")
   - `text` — exact identifiers, names, error strings

   For "not about X" intents, never put the negation in the query text —
   embeddings ignore "not" and mentioning X attracts it. Use the `exclude`
   parameter (words/phrases to drop) or `exclude_path` (path fragments)
   instead.

3. **Map results to files.** Each hit returns a file path + Section trail
   (heading breadcrumb like "Install > macOS"). Path = what to read,
   Section = where in the file. Read only what search surfaced.

4. **Verify freshness.** The index reflects files at index time. If a result
   names a function, flag, or fact — verify against the live file before
   relying on it. `list_files` shows mtimes if in doubt.

5. **Suggest reindexing.** If you created or edited files worth searching
   later (docs, notes, guides), remind the user to run
   `qazyna index <path>` — MCP is read-only by design; indexing happens in
   the terminal.

## When to reach for it

- The user mentions "my notes", "the style guide", "that document I have"
- Writing/editing content that must follow an indexed style guide
- A question about material that isn't in the current repo
- Before answering "where did I write about X?"
- Starting work in a repo whose docs are indexed

## When NOT to use it

- Pure code navigation in the current repo — Grep/Glob are better for symbols
- The user gave you the exact file path already
- Nothing relevant could plausibly be indexed (check `list_files` if unsure)

## Repo-specific knowledge

If the current repo has its own `.claude/skills/` or `CLAUDE.md` with a
task-to-file map (e.g. qazyna's own repo has `qazyna-dev`), prefer that map
for code work and use this skill for knowledge/content search.
