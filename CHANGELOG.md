# Changelog

All notable user-facing changes to **Rolling Memory** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Memory-graph phrase nodes now require two terms whose text spans do not overlap. Adjacent sliding windows of one Han run are the dual track that keeps retrieval recall, but they are one text run projected twice, so they are no longer projected as phrases; disjoint tilings of the same run are recovered instead, which keeps genuine Chinese pairs such as 推进·继续 visible. Every phrase fragment now carries the pair df, both member dfs, `lift` against the independence expectation, and how much of the rarer member the pair covers, and the detail drawer shows them next to the binding energy. Retrieval postings, word families and the tokenizer are untouched.

### Added

- An R entry point for researchers: `research/r/` ships a thin shell-out layer (`rmemory.R`: `rm_init`, `rm_import`, `rm_recall`, `rm_bands` and friends), an end-to-end demo on a throwaway vault, a base-R reproduction of the occupancy-null distribution and its tail bound, and a half-life script that separates the closed form from the estimation problem (right-censored corpora bias a naive bridge share; the censored MLE and a bootstrap interval are worked through on simulated gaps). R needs only `jsonlite`; the CLI plus JSON stays the whole interface. The notes cover the two things that bite first — `system2()` does not quote arguments, and a vault only answers for the scope it was created with — and the JSONL sample carries the `stop_reason` completion marker that `import` requires.


### Removed

- The legacy JSON export import path and the assertion-graph FTS rebuild helper are gone. JSONL and plain-text transcripts are the only supported import formats, and recall receipts no longer carry a `vector` provider block.

## [0.0.7] - 2026-11-19

### Changed

- **Live host attachment**: the console now names a coding host only while a session is actually attached. The local MCP sidecar renews a lease every thirty seconds, a prompt hook records a two-minute heartbeat, and an expired lease means no live host — a newest transcript or a leftover presence file is no longer presented as the current environment. When nothing is attached the badge reads "No live host detected", and a separate "Recent activity" badge names the host with the newest transcript. `rolling-memory sync status --json` carries the same distinction as `attachment` / `attachments` alongside `recent_harness_id` and `recent_harness_label`, and a process whose host cannot be identified is reported as unknown instead of being labelled as a generic CLI session.
- **One physical corpus**: `rm_docs`/`rm_unit_meta` are the only stored corpus. A read-only `rm_units` read model exposes the effective text and metadata without a second copy. There is no legacy table migration: earlier corpus schemas are not carried forward.
- Benchmark disclosure now uses the current product arm: ATM-Bench Main-1013 official R@10 82.85%, R@25 86.41%, R@50 90.11%; Hard-31 R@10 52.43%, R@25 60.14%, R@50 70.62%; composite score 73.72. The corpus and question pins were re-verified on 2026-11-19. The frozen v0.0.1 readings (77.21% / 46.12%) remain in the v0.0.1 changelog entry as historical values. LoCoMo full-set end-to-end judge accuracy remains 85.26% (1,313/1,540; Top-96 delivery; LLM answerer + judge).
- `rolling-memory status`, `check`, and `doctor` report one corpus only: the canonical counters (memories, episodic, background, crystallized) plus the vault's term statistics. The retired deep-metadata fields are gone, and the configuration block shows the `update.channel` / `update.source` preferences instead of the removed `[llm]` section.

### Fixed

- `rolling-memory install --check` now probes the server without running a background version check, so the check leaves no update cache behind and never reaches the network.
- OpenCode environments are now included in the turn-scanning capability set, so the console reports episodic alignment for them instead of leaving the layer marked as unknown whenever an OpenCode configuration directory is present.
- Prompt hooks and the console now agree on where runtime presence lives when `XDG_STATE_HOME` is set or the engine log is redirected: the attachment ledger is written beside the rest of the runtime state and read from the same directory, so the console no longer reports an idle environment while a session is live.
- Re-running `rolling-memory install` after the binary moved no longer stops on its own generated launcher: an owned launcher is updated in place with a backup, while a foreign script still reports a conflict.
- `rolling-memory install --check` now compares the installed skill with the current release and inspects the launcher. An older owned copy reports `outdated`, a user-edited copy reports `conflict`, and a missing launcher fails the check instead of passing with a live probe against a different executable.
- An enabled native plugin no longer ends up with a second skill, MCP entry, or prompt hook beside it: `--force` cannot duplicate a plugin-provided surface, and `--dedupe` removes only this tool's owned copy. A plugin MCP command that is not available is reported as a conflict rather than trusted.
- After an in-place upgrade, `rolling-memory daemon restart` can retire a daemon that still serves the previous binary. The command authenticates the stale socket by ownership, vault path, and protocol identity; `install` prints it when the mismatch is present.
- Plugin cache directories are ordered by semantic version, so `0.10.0` is selected over `0.9.0`.
- Preparing the evaluation workbench no longer stops with an identity-mismatch error when the evaluation store was written by an earlier engine build: the stored corpus is re-ingested under the current endpoint and tokenizer instead. A resumed ingest also re-encodes every unit whenever the tokenizer arm changes, so a warm store is never searched with the wrong vocabulary. Evaluation receipts now report the number of units actually written, so a warm-store run shows zero instead of the corpus size.

### Added

- **Complete disk-maintenance coverage**: `rmem maintain clean` now also manages vault database backups, launcher backups in `~/.local/bin`, and the console's previous `web-dist` directories, keeping the newest copies per family and writing a receipt. `rmem maintain status` reports evaluation history (`eval/`, `atmbench/`) with its size but never auto-deletes it.
- **Global update entry**: the console top bar now shows an update badge whenever a newer release is available, from any page. One click opens the same SHA-256 verified updater that refreshes the CLI, the console backend, and the web assets in place, then restarts the console.
- **Automatic version awareness**: the web console now surfaces a newer release as soon as its real-time channel connects, with a badge on the version button and a one-time notification. The cached state is refreshed in the background after six hours (thirty minutes after a failure), and the CLI `status` / `doctor` receipts carry the same cached hint without a network request. A tag without installable release artifacts is reported as a source-only update instead of offering a download that cannot succeed.
- **Update channels**: `stable` accepts published non-prerelease tags, `beta` also accepts prerelease tags, and `dev` tracks the latest `main` commit and is the default for new installs during this early stage. The preference is saved in the CLI config (`update.channel`), exposed as a selector in the console's Version & Update dialog, and honored by the optional scheduled check. `dev` reports `main@<commit>` and requires an explicit source rebuild; it never replaces a binary automatically.
- **Optional OS-level update check**: `rolling-memory update --schedule install` registers a user-level launchd agent, systemd user timer, or Windows scheduled task that refreshes the cache and sends one desktop notification per new version. It never installs an update automatically, and `--schedule remove` cleans it up. Source installs can rebuild from a tag with `ROLLING_MEMORY_SOURCE_REF=v0.0.7 bash install.sh` or `bash install.sh --ref v0.0.7`.
- **Update source selector**: the Version & Update dialog now offers GitHub (official) and Gitee (mainland mirror) alongside a custom mirror URL. The choice is saved as `update.source`, reused by the CLI and background checks, and can be set with `rmem config set update.source gitee` or overridden per run with `rmem update --source gitee`. A Gitee tag without attached binaries is reported as a source-only update with a Gitee-specific rebuild hint.

### Removed

- Retired command surfaces are removed instead of kept as stubs: `export`, `reindex`, and `recall --deep` no longer parse, `--mode` and `mode` accept only `local`, and the unused `[llm]` configuration section is gone.

## [0.0.6] - 2026-11-19

### Changed

- **Single retrieval index per memory**: each memory's retrieval terms are derived once when it is saved and stored next to its text in the vault's document store, which is what the engine serves at query time. The in-memory snapshot and the on-disk index cache are keyed by that store's revision counters, so a warm index keeps being reused as new memories arrive, and the packed-atom codec that previously had to be decoded again at read time was removed.
- The memory console, in-app help centre, and benchmark/privacy documentation now describe the shipped deterministic cluster kernel instead of the retired FTS5/BM25 lexical index, and the evaluation page no longer mentions a retired cloud endpoint.
- **Help centre is now a documentation portal**: the console no longer stores its own copy of the docs. Opening Help & Docs shows a GitHub/Gitee site chooser and links to the official repository documentation, so the text always matches the released source; the redundant "Open-Source Repos" page was removed.

### Fixed

- Opening a vault created by an earlier version now derives its existing memories from their own sources — stored text, mounted files, or the original session — before the first recall runs, so an upgrade never leaves recall pointing at an empty corpus. The one-time pass is recorded in the vault, skips units the user deleted, and later opens start immediately.
- Background episodic maintenance now runs one pass at a time per process and stops long transcript scans as soon as its lease is revoked, so several connected hosts no longer stack repeated reads of the same session file or pin a CPU core after a session goes idle.

## [0.0.5] - 2026-11-19

### Added

- **OpenCode integration**: `rolling-memory install opencode` now wires the skill and the local MCP server into OpenCode V2 with its native configuration shape (`mcp.servers` → `type: local`, array `command`, `environment`), including project-scoped installs (`--project`) and `--check` / `--uninstall` support. The adapter writes the user skill root (`~/.config/opencode/skills`, honoring `XDG_CONFIG_HOME`) or `.opencode/skills` for projects, and leaves unrelated providers, models, and servers untouched.

### Changed

- Disk maintenance keeps the engine's on-disk index cache by default. Deleting that cache forced the next recall to rebuild the whole index — minutes of work and a multi-GB memory peak — so freeing the space is now an explicit opt-in (`rmem maintain clean --purge-engine-cache --apply --yes`), and the read-only scan reports the cache separately as kept instead of counting it as reclaimable space.
- Remembering a long text now stores it as several sentence-aligned memory units of at most 8,192 tokens each (identical chunks collapse), so oversized pastes stay recallable and never hit the single-payload ceiling; a text that fits within one unit keeps its existing memory id.

### Fixed

- Long-text units and text/code background files now keep the source text verbatim, including indentation and the whitespace at a cut point, and each one's token bound is checked against the exact text it stores rather than per-sentence estimates. A unit can no longer quietly exceed the 8,192-token rung through cross-boundary re-tokenisation; a single sentence that alone exceeds it is still kept whole instead of being cut.
- Spreadsheet background files use the same exact-fit budget rule when a sheet is split into row blocks, and a row that alone exceeds the budget is kept whole instead of being cut.
- A message long enough to be stored as several memory units now attributes every delivered part to the speaker named by the message's opening line, instead of leaving the parts after the first without a speaker. The attribution is read from the stored source, never guessed from the content.

## [0.0.4] - 2026-11-18

### Changed

- **Engine-native incremental overlay**: the retrieval kernel now serves added, updated, and deleted memory units through a zero-copy on-disk baseline plus a small in-memory delta layer. Continuous ingestion no longer triggers full index rebuilds, so recall stays responsive and resident memory stays bounded as the vault grows.
- **Leaner client surface**: remaining duplicated client/kernel paths were retired — incremental serving, per-unit term lookup, and snapshot verification are now delegated to the engine kernel, and redundant storage and graph-client adapters were removed with no change in memory quality.

### Fixed

- Automatic background sync no longer re-reads and re-verifies every stored episodic payload after a single new turn is saved; the diagnostic check reuses the zero-copy snapshot's verified unit set, removing multi-core CPU spikes during continuous ingestion.
- L3 crystallized insights no longer receive a doubled citation boost when the same evidence is reachable through both the citation closure and a direct match.
- Console maintenance and evaluation wording reads consistently in plain language.

## [0.0.3] - 2026-11-18

### Added

- **Codex hooks support**: `install codex --with-hooks` writes the Claude-shaped `~/.codex/hooks.json` (and project `.codex/hooks.json`), bringing the zero-intrusion memory readiness prompt to Codex sessions; the first run requires the hook to be reviewed and trusted inside Codex.
- **Memory curation commands**: `rmem maintain` (disk-space maintenance — prune old backups and caches, compact and slim the vault; dry-run by default) and `rmem prune` re-anchoring same-source hits by default (cross-source substitution requires an explicit `--reanchor-cross-source`).
- **Cross-turn pointer re-anchoring** with a recurring same-source pointer patrol and a "Cleanup & Self-Healing" console entry, plus a dry-run-first unmount preview for `background remove` that shows the re-bind/retire/citation impact before confirming.
- **L3 verification-reuse gain & ROI gold leaderboard**: crystallized insights whose cited evidence is recalled again rank through a continuous log gain over their citation closure, surfaced as a first-class tier in the console.
- **Evaluation**: the official LoCoMo Cat-5 adversarial tier (446 questions) is scored through the tri-state evaluator, together with three adversarial probe suites (ATM absence 100 / ATM conflict 100 / LoCoMo adversarial 446) published with reproducible evidence.

### Changed

- **Memory governance for large vaults**: steady-state resident memory dropped from runaway multi-gigabyte growth to a bounded footprint — on a 34k-unit live vault, steady ≈3 GB (≈2 GB file-backed pages) where the session previously peaked above 20 GB. The engine now serves rebuilds from disk-backed zero-copy snapshots, releases per-build term maps after use, and no longer rebuilds the full index on every background write, so continuous ingestion keeps foreground recall responsive.
- **Single local-first runtime**: the client was streamlined onto its one supported local mode; the retired local lexical index (FTS5/BM25) and its comparison arm were removed so retrieval runs solely on the engine's cluster kernel — smaller vault files and faster cold scans, with the frozen ATM baseline preserved bit-for-bit (1,044 questions).
- **Smart adaptive delivery window**: recall now delivers through a natural energy window by default (`auto`, up to 16 bands), with optional wider windows (`0-32` / `0-64`) for audits; the MCP `chars` parameter was removed in favor of the window controls.
- **Vault hygiene**: opening a vault now reclaims legacy leftover tables (session cursors, retired indexes), and fresh databases bootstrap thin — no legacy assertion graph — keeping user vaults small and simple.
- **Kernel synchronization**: the retrieval kernel picked up the latest engine revisions — package-internal deduplication, ordinal-day canonicalization, an exported high-DF fraction, and curated closed-set tables — validated bit-for-bit against the frozen ATM baseline.
- Panel lifecycle management is unified: the `rolling-memory-panel` wrapper and the `panel` command share the same pid/log locations.
- Console: the ROI 14-day panel was redesigned (distillation × crystallization × adoption tracks with independent scales and per-day adoption), the real-time recall latency card reports a robust median, and the monitoring float now speaks the Local Vault vocabulary only.

### Removed

- Pre-local-first legacy layers were removed from the client — obsolete remote-client code paths, the optional model-extraction layer, and standalone duplicate helper primitives — a smaller, faster-to-audit codebase with no memory-quality change.

### Fixed

- Recall delivery budget is now enforced end to end: the legacy 200-character per-item truncation floor was removed and truncation markers count against the budget, so `caps.chars = 300` never over-delivers.
- Installer integrity checks are fail-closed: a SHA-256 mismatch for a downloaded release asset is a fatal error instead of a silent fallback to a source build; when no checksum tool or entry is available the installer reports `unverified` with an explicit reason.
- Bare installs and one-command installs now honor the persisted language preference (`--lang` is no longer reset to English).
- Harness conversation capture keeps envelope-only turns out of the user-speech stream.
- L3 crystallization citations can no longer collapse to a bare label line: citations keep the full role block (role line plus continuation lines) and reuse the delivery-side cleaning predicate.
- The real-time stream no longer replays stale memory content: frames that carry memory text are computed fresh for new subscribers.
- Episodic import, scan, and reconcile share one frozen per-source parse and a content-addressed identity per turn, fixing cross-turn drift and enabling safe same-source re-anchoring before deletion checks.
- The console's real-time recall latency card reports a robust median (latest and peak values in the tooltip) instead of a single spike-prone sample.
- LoCoMo corpus rebuilds assign globally unique line ids and dedupe on the locator, eliminating rare id collisions in rebuilt evaluation corpora.

## [0.0.2] - 2026-11-18

### Changed

- Consolidated cloud credential handling onto the single canonical pulse path: `mem2` credentials carrying a `bpep` blinded pepper use it verbatim, static secrets and legacy `mem2` tokens keep the documented derivation, and the speculative V2/legacy dual-read query arm, pepper policies, and the `RekeyUnits` migration layer were removed from the SDK and thin client (local plaintext mode was always single-arm).
- Simplified the local product assembly onto the engine-native canonical companion path: structured-date ISO beacons and the merged closed-set companion tables (SI/IEC units, ISO 4217 currencies, ISO 3166 geography, statistical notation) now load directly from the SDK, replacing the redundant standalone language-pack layer with the same default max-capability indexing behavior.
- Aligned the client SDK with the project license: the SDK under `sdk/` now carries the same **PolyForm Noncommercial 1.0.0** terms as the rest of the repository (superseding the earlier Apache-2.0 SDK terms); commercial use still requires a separate written commercial license.

### Fixed

- Eliminated duplicate inline-snapshot documents during concurrent insert/scan windows and deduplicated repeated cloud candidates, preserving stable product/direct recall ordering; deterministic regressions were added and the LoCoMo product-parity probe passes 24/24 runs.
- Preserved high-resolution email sub-unit line spans by keeping subject metadata out of the document body while retaining the subject-plus-summary header block.

### Added

- Added repository community-health files — a Contributor Covenant 2.1 Code of Conduct (English and Simplified Chinese), an accessibility statement, and a pull-request template — so the public repository covers the full GitHub community-standards checklist.

## [0.0.1] - 2026-11-16

Initial public release.

### Added

- **Local-first persistent memory (`local_plain`)**: on-device long-term memory over a single-file SQLite plaintext vault (FTS5 + BM25 retrieval with dimensionless Shannon self-information gain and event-chain coherence). Zero network egress, zero vector database, zero model calls required for daily memory storage and retrieval.
- **Three-layer memory architecture**: structured separation across L1 Background (mounted local workspace folders), L2 Episodic (streaming Harness conversation capture with zero-copy verified pointer indexing — the multi-generation union read keeps the entire history recallable across encoder upgrades; on a live 4,773-document vault, cited-evidence recall went 26.3% → **90.9%**), and L3 Noetic (consolidated evidence-backed insights, automatically crystallized from recalled evidence).
- **Single-tool MCP & multi-harness integration**: built-in stdio/HTTP MCP server exposing the unified `recall` tool (automatic consolidation on hit; default `top-k = 10`, up to `64`; default `16,000`-character delivery budget) together with one-command installation and lifecycle hooks for ZCode, Claude Code, Codex, DeepSeek Harness (`dsh`), Cursor, CodeBuddy/WorkBuddy, Windsurf, and Cline.
- **Source-available web administration console** (`rolling-memory panel`): Go + SQLite backend (`:9006`) and Vue 3 + TypeScript + Vite frontend (`:8008`) providing a read-only Recall Debugger, three-layer memory browser, Harness sync diagnostics, ROI & adoption telemetry, an interactive memory graph, and an evaluation workbench over an event-driven WebSocket bus.
- **Reproducible evaluation runner (`rmem eval`) & benchmark readings**: corpus-pinned evaluation over ACL 2024 LoCoMo and the University of Cambridge ATM-Bench with isolated evaluation scopes and per-question provenance. On the full official LoCoMo Cat 1–4 set (1,540 questions), the local plaintext arm reaches **85.26%** end-to-end judge accuracy (1313/1540; single-hop 90.96 / temporal 87.23 / multi-hop 70.57 / open-domain 71.88; retrieval 100% offline). ATM-Bench readings: Main-1013 R@10 **77.21%**, Hard-31 R@10 **46.12%** (v0.0.1 frozen build; official gold-set-mean, pinned corpus and protocol). Current product-arm readings are 82.85% / 52.43% — see the current `docs/benchmarks.md`. One-command corpus fetch (`rmem eval --fetch atm|locomo`) downloads the pinned upstream data, rebuilds the corpus and question sets locally, and verifies the digests. The benchmark corpora are never bundled with the product or its release assets (upstream CC BY-NC 4.0 terms).
- **Cross-platform packaging & self-update**: six-platform CLI binaries, macOS `.dmg` and Windows desktop packages (`.zip`), Windows drive-letter/UNC path hardening, and a built-in `rolling-memory update` path with SHA-256-verified release assets and dual GitHub/Gitee sources.
- **Client SDK (`sdk/`)**: a zero-dependency Go SDK (`v1.11.3`) for local memory encoding and integration, with complete third-party license notices.
- **Licensing**: source-available under **PolyForm Noncommercial 1.0.0** — free to use, modify, and share for noncommercial purposes; commercial use or distribution requires a separate license.

[Unreleased]: https://github.com/rocareer/rolling-memory/compare/v0.0.7...HEAD
[0.0.7]: https://github.com/rocareer/rolling-memory/compare/v0.0.6...v0.0.7
[0.0.6]: https://github.com/rocareer/rolling-memory/compare/v0.0.5...v0.0.6
[0.0.5]: https://github.com/rocareer/rolling-memory/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/rocareer/rolling-memory/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/rocareer/rolling-memory/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/rocareer/rolling-memory/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/rocareer/rolling-memory/releases/tag/v0.0.1
