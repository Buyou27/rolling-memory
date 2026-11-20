# Contributing

**[English](CONTRIBUTING.md) · [简体中文](docs/zh-CN/CONTRIBUTING.md)**

Thanks for considering a contribution.

## Project status and development model

Rolling Memory is under **active, high-velocity development** and is still pre-1.0. This repository is the **public release face** of the project: it receives curated source snapshots and tagged releases derived from the internal development tree, and every snapshot is scanned for private material before it is published. The commit history here therefore shows release/snapshot commits rather than the full per-feature development history; every user-facing change is documented in [CHANGELOG.md](https://github.com/rocareer/rolling-memory/blob/main/CHANGELOG.md) and on the [releases page](https://github.com/rocareer/rolling-memory/releases).

Because the engine evolves quickly, APIs, file formats and behaviors may change between releases — each change is recorded in the changelog, and we do our best to keep every release reproducible and benchmarkable. We are grateful for your understanding and support, and we warmly welcome participation: bug reports, benchmark reproductions, documentation fixes and pull requests are all appreciated (see below). Thank you for helping the project grow.

## Ground rules

- Keep the runtime dependency-free: the shipped binary is self-contained, and its module set is the fixed
  whitelist below (no CLI framework, ORM, logging or diff library). Optional extras are fine if they stay optional.
- Local mode must never make a non-loopback request.
- Protect sensitive data in local storage, backups, and any data shared with external services.
- Every memory unit must have a verbatim anchor. Do not add a “fill in the anchor” path.
- Writes are append-only. Do not add a code path that edits the content of an existing unit.
- Read paths must stay deterministic for the same DB/scope/cue/asof/caps.
- `absent` must stay a first-class state, distinct from errors.

## Development

```bash
go build ./...
go vet ./...
gofmt -l .                                  # must print nothing
go test ./... -count=1
CGO_ENABLED=0 go build -trimpath ./cmd/rolling-memory   # portability smoke (pure Go, no CGO)
```

Dependencies are a fixed whitelist pinned in `go.mod` / `go.sum`: `modernc.org/sqlite` (pure-Go SQLite),
`golang.org/x/text` (NFKC / casefold), `golang.org/x/crypto` (blake2b) and
`github.com/pelletier/go-toml/v2`. The CLI, the JSON-RPC server and the token similarity matcher
are hand-written on purpose.

Tests use synthetic data only and isolate themselves: they run against temporary HOME / XDG / database
roots and never open a real `~/.local/share/rolling-memory/memory.db`.

Format vectors: `testdata/golden/vectors/` holds the small JSON expectations pinned by the id and time helpers
(`internal/ids`). They are reviewed inputs, not generated output: never refresh them from an ad-hoc test run.
A semantic contract change requires an explicit, reviewed update to the vectors in the same PR.

## Versioning & deprecation

- Releases follow semantic versioning (`MAJOR.MINOR.PATCH`) and are tagged `vX.Y.Z`.
- Every release has a non-empty `CHANGELOG.md` section; the GitHub/Gitee release body is taken from that
  section, not written by hand.
- Deprecations are announced in `CHANGELOG.md` and the matching `docs/` page, kept for at least one minor
  release, and removed in the next major release. Removed aliases fail closed; do not add silent fallbacks.
- While the project is in `0.y.z`, a minor release may contain a documented breaking change. The changelog
  must state the migration step.
- Public contracts (tool schema, receipt shape, sync contract, mode names) are versioned with the release;
  changing one requires a docs update and a changelog entry in the same PR.

## PR checklist (run these before asking for review)

All commands run from the repo root and must be clean — this is the exact gate set the branch is held to:

```bash
go build ./...                                # 1. compiles
go vet ./...                                  # 2. static checks
gofmt -l .                                    # 3. must print nothing
go test ./... -count=1                        # 4. unit + internal/e2e (process level)
go test -race ./internal/... -count=1         # 5. race detector over the engine packages
go test ./internal/... -shuffle=on -count=2   # 6. order independence (no inter-test coupling)
# 7. cross-compile smoke: all five release targets build from pure Go
for t in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do
  CGO_ENABLED=0 GOOS="${t%/*}" GOARCH="${t#*/}" go build -trimpath -o /dev/null ./cmd/rolling-memory || exit 1
done
```

- Tests use synthetic data only, and isolate HOME / XDG / DB roots by default — never point a test at a real
  memory database, and never let a new test write outside its temporary root.
- Numbers quoted in docs (scenario counts, file counts) come from the fixtures themselves
  (`testdata/golden/vectors/`); update them there first.

## Pull requests

- One concern per PR; include tests for new behavior and privacy boundaries.
- If you change a public contract (tool schema, receipt shape, sync contract), update the matching doc in
  `docs/` and `CHANGELOG.md`.
- If you change the schema, add a migration strategy (the current schema is v1; treat existing DBs carefully).
- Do not include real personal data, API keys, transcripts, or private URLs in tests or examples.

## License & sign-off

The repository's first-party code — including the client SDK under `sdk/` ([license copy](sdk/LICENSE)) — is licensed to users under the [PolyForm Noncommercial License 1.0.0](LICENSE), and the project also sells separate commercial licenses. To keep that dual-licensing possible without a CLA form, **inbound contributions are accepted under the Apache License 2.0**: by submitting a contribution you agree that it is provided under Apache-2.0, which allows the maintainers to redistribute it under the project's noncommercial and commercial license terms. Sign off every commit with `git commit -s` (Developer Certificate of Origin 1.1); no separate CLA is required. Preserve third-party attribution in [NOTICE](NOTICE).

## Security

Please report security issues privately to the maintainer rather than opening a public issue with exploit
details. See [SECURITY.md](SECURITY.md).
