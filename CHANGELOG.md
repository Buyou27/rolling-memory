# Changelog

All notable user-facing changes to **Rolling Memory** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- **Reproducible evaluation runner (`rmem eval`) & benchmark readings**: corpus-pinned evaluation over ACL 2024 LoCoMo and the University of Cambridge ATM-Bench with isolated evaluation scopes and per-question provenance. On the full official LoCoMo Cat 1–4 set (1,540 questions), the local plaintext arm reaches **85.26%** end-to-end judge accuracy (1313/1540; single-hop 90.96 / temporal 87.23 / multi-hop 70.57 / open-domain 71.88; retrieval 100% offline). ATM-Bench readings: Main-1013 R@10 **82.56%**, Hard-31 R@10 **50.35%** (official gold-set-mean, pinned corpus and protocol). One-command corpus fetch (`rmem eval --fetch atm|locomo`) downloads the pinned upstream data, rebuilds the corpus and question sets locally, and verifies the digests. The benchmark corpora are never bundled with the product or its release assets (upstream CC BY-NC 4.0 terms).
- **Cross-platform packaging & self-update**: six-platform CLI binaries, macOS `.dmg` and Windows desktop packages (`.zip`), Windows drive-letter/UNC path hardening, and a built-in `rolling-memory update` path with SHA-256-verified release assets and dual GitHub/Gitee sources.
- **Client SDK (`sdk/`)**: a zero-dependency Go SDK (`v1.11.3`) for local memory encoding and integration, with complete third-party license notices.
- **Licensing**: source-available under **PolyForm Noncommercial 1.0.0** — free to use, modify, and share for noncommercial purposes; commercial use or distribution requires a separate license.

[Unreleased]: https://github.com/rocareer/rolling-memory/compare/v0.0.5...HEAD
[0.0.5]: https://github.com/rocareer/rolling-memory/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/rocareer/rolling-memory/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/rocareer/rolling-memory/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/rocareer/rolling-memory/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/rocareer/rolling-memory/releases/tag/v0.0.1
