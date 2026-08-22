# qazyna search quality eval

How to measure search quality so that every ranking change becomes a
before/after comparison in numbers, not a "feels better" guess.

Russian version: [eval.md](eval.md).

## Golden set

A file of 15–20 "query → expected file" cases. Golden sets live next to
the index (`~/.local/share/qazyna/evals`); another directory is set with
`--evals` or `QAZYNA_EVALS`. They are the user's personal data and are
not kept in the qazyna repo:

```yaml
- query: "что мы должны сделать чтобы подготовить тикет для деплоя?"
  expect: ["kit-help/resources/help-text.md"]
- query: "how do I deploy to preprod"
  expect: ["skills/deploy/SKILL.md", "infra-deploy-preprod/SKILL.md"]
```

Rules:

- Expectations are at the **file** level (path suffix match), not the chunk
  level: chunk ordinals shift whenever the chunking changes, while "the right
  file was found" is a stable criterion.
- Take cases from real queries (CLI history). Every time search performs
  poorly in daily use, the query goes into golden.yaml — the set grows into
  a regression suite.

## Metrics

- **recall@5** — the share of cases where an expected file made the top 5.
  The headline "search finds things" number.
- **MRR** (mean reciprocal rank) — the average of 1/position of the first
  correct result. Sensitive to ordering: moving the answer from 5th place to
  1st leaves recall unchanged but lifts MRR from 0.2 to 1.0. This is the
  metric that will show the effect of query decomposition and a reranker.

## Harness: `qazyna eval [golden.yaml ...]`

With no arguments it runs every `*.yaml` in the evals directory; with
paths, only the given files.

A subcommand, not a Go test: it needs a live index and Ollama — semantic
quality cannot be measured with the fake embedder. The command walks the
cases, calls the same `search.Search`, prints one line per case (the position
of the expected file, or "miss") and an aggregate at the bottom.

A `--mode` flag runs vector/text/hybrid separately, to see whether hybrid
adds anything over pure vector on Russian queries against English docs
(the lexical half barely matches there).

## Comparison discipline

- Do not touch the index between "before" and "after" — otherwise you are
  measuring a new corpus, not your change.
- Exception: chunking changes, where re-indexing *is* the change.
  A/B them via two different `--db` paths.
- Look at the per-case diff, not just the aggregate: +2 cases and −2 cases
  give the same recall, but that is a regression the aggregate hides.

## Context: the search improvement queue

The eval comes first. Then, in descending order of impact:

1. Decompose conversational queries into sentences and merge the result
   lists with the existing RRF.
2. RRF candidate pool: currently `limit*3` = 15, raise to 30–50.
3. Per-file dedup: at most 2 chunks of the same file in the results.
4. Chunking: merge sections shorter than ~200 chars (kills noisy
   mini-chunks), overlap of 1–2 sentences, hard-split on sentence
   boundaries. Any chunking change requires a full re-index.
5. Reranker (bge-reranker-v2-m3) — the biggest quality gain, but Ollama has
   no rerank API: it conflicts with the "Ollama only" principle.

A note on hybrid scoring: flat ~0.5 scores on cross-lingual queries are not
a model weakness but the ceiling of the RRF normalization (1.0 = first in
both lists, 0.5 = first in only one; the lexical list is empty for a Russian
query over English docs).
