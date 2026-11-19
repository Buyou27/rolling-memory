# R for researchers

**[English](README.md) · [简体中文](README.zh-CN.md)**

A minimal workbench for people who do statistics in R: **no R SDK**. R shells out to the local binary, parses JSON, and does everything else itself. Retrieval and storage stay on the machine; the memory plane makes no network requests.

## Requirements

- R >= 4.1 (base R plus one package, `jsonlite`)
- The `rolling-memory` local build (prebuilt binary; no Go toolchain)

```bash
# Install: lands in ~/.local/share/rolling-memory/bin, linked from ~/.local/bin
curl -fsSL https://github.com/rocareer/rolling-memory/raw/main/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"

# The only R dependency
Rscript -e 'install.packages("jsonlite")'
```

## Configuration

Environment variables win over defaults:

| Variable | Default | Meaning |
| :--- | :--- | :--- |
| `ROLLING_MEMORY_BIN` | auto-detected on `PATH` and in the install dir | binary path |
| `ROLLING_MEMORY_DB` | `~/.local/share/rolling-memory/memory.db` | vault (one SQLite file) |
| `ROLLING_MEMORY_SCOPE` | `default` | partition name |

**The scope is bound to the vault at `init` time.** A vault only answers for the scope it was created with; anything else fails with `vault scope does not match its binding`. Use one vault per topic rather than renaming scopes.

To persist settings instead of exporting variables:

```bash
rolling-memory config set db /path/to/memory.db
rolling-memory config set scope my-research
rolling-memory config show
```

## Quick start

```r
source("rmemory.R")

rm_paths()                       # resolved bin / db / scope
bands <- rm_bands("why did we drop the vector index")
bands[, c("score", "title", "content")]
```

`score` is measured in **Nats** (negative log collision probability under the null), an **additive evidence quantity**, not a similarity in $[0,1]$. Scores are comparable inside one query, not across queries.

## Files

| File | Purpose |
| :--- | :--- |
| [`rmemory.R`](rmemory.R) | Thin `source()`-able layer: `rm_init` / `rm_import` / `rm_status` / `rm_doctor` / `rm_recall` / `rm_bands` |
| [`demo.R`](demo.R) | End to end on a throwaway vault: init, import four turns, two queries, print hits (never touches your real vault) |
| [`occupancy-null.R`](occupancy-null.R) | Base-R reproduction: occupancy null distribution, the $\le 1/M$ tail bound, a Monte Carlo check, and a figure |

```bash
Rscript demo.R            # cleans up its temp vault when done
Rscript demo.R --keep     # keeps the temp vault for inspection
Rscript occupancy-null.R  # prints the bound check and writes a figure
```

## Importing your own transcripts

`import` reads JSONL, one message per line:

```jsonl
{"type":"user","message":{"role":"user","content":"We decided against a vector index; everything stays local and statistical."}}
{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"Why?"}}
```

**Assistant rows must carry `"stop_reason":"end_turn"`.** Turns without that completion marker count as an unfinished tail and are skipped. This is the usual cause of "no error, but nothing was imported".

```bash
rolling-memory import ~/notes/session.jsonl --db ~/vault/research.db --scope research
```

## Troubleshooting

| Symptom | Cause | Fix |
| :--- | :--- | :--- |
| `rolling-memory binary not found` | not installed, or not on `PATH` | set `ROLLING_MEMORY_BIN` to the absolute path |
| `import: 0 messages · 0 new units` | assistant rows lack `stop_reason` | add the completion marker |
| `vault scope does not match its binding` | vault created with scope A, queried with B | keep the scope consistent, or create another vault |
| `recall` returns `absent: true` | the vault has no matching record | `import` first, or use wording closer to the source |
| mojibake in output | terminal or file is not UTF-8 | check with `file -I`, export as UTF-8 |

## Boundaries

- The local build is **plaintext, on-device**: the vault is a single SQLite file, and the memory plane makes no network requests, uses no vectors, and runs no background LLM.
- Text returned by `recall` enters your context; if your downstream model is remote, that text leaves the machine as part of the prompt.
- The system answers "is this in the archive", not "is this true". Truth adjudication stays with you and the downstream model.

## License

See [LICENSE](../../LICENSE) at the repository root.
