# Configuration Reference

**[English](configuration.md) · [简体中文](zh-CN/configuration.md)**

Rolling Memory keeps one human-editable configuration file and a small set of environment overrides. This page collects the settings most users touch — storage paths, the recall delivery window, the background engine's memory cap, console ports, and portable mode — and shows the equivalent controls in the web console's **System → Memory Service** page.

---

## 1. Where configuration lives

| Item | Default location | Notes |
|---|---|---|
| Configuration file | `~/.config/rolling-memory/config.toml` | TOML; created on first run with commented defaults (permissions `0600`) |
| Vault database | `~/.local/share/rolling-memory/memory.db` | Local plaintext SQLite vault |
| Log file | `~/.local/state/rolling-memory/rolling-memory.log` | |
| Engine socket | `~/.local/state/rolling-memory/engine.sock` | Local Unix socket for the background engine daemon |

Portable (USB) payloads automatically use relative paths and the `usb` storage profile; see [USB Portable Mode](usb-portable.md).

**Precedence:** command-line flags → environment variables → `config.toml` → built-in defaults.

Inspect the file and the resolved values:

```bash
rmem config path          # print the active config file path
rmem config show          # dump resolved configuration as JSON
rmem config set <section.key> <value>
```

---

## 2. Common settings

### 2.1 Storage and identity — `[core]`

| Key | Default | Meaning |
|---|---|---|
| `core.db` | `~/.local/share/rolling-memory/memory.db` | Vault database path |
| `core.scope` | `default` | Memory scope; isolated evaluation runs use their own scope |
| `core.reader` | `deliverable` | Default audience view (`owner` / `deliverable`) |
| `core.log` | `~/.local/state/rolling-memory/rolling-memory.log` | Log file path |

### 2.2 Recall delivery — `[recall]`

| Key | Default | Meaning |
|---|---|---|
| `recall.delivery_window` | `auto` (resolves to `0-16`) | Adaptive delivery window: `auto`, `0-16`, `0-32`, or `0-64`. Larger windows give more evidence in one recall |
| `recall.raw_limit` | `5` | Raw-evidence delivery cap within the character budget |
| `recall.chars` | `16000` | Character budget per recall |
| `recall.budget_ms` | `500` | Latency budget hint |

The delivery window is also switchable from the console header of the overview page.

### 2.3 Background engine memory cap — `[daemon]`

| Key | Default | Meaning |
|---|---|---|
| `daemon.memory_limit` | `auto` | Soft heap limit for the background engine daemon |

Accepted values:

| Value | Behaviour |
|---|---|
| `auto` | `1/8` of physical RAM, clamped to `1GiB–4GiB` (a 16 GB machine resolves to 2 GiB) |
| `off` | No explicit cap |
| An explicit size | `B` / `KiB` / `MiB` / `GiB` / `TiB`, e.g. `512MiB`, `2GiB`; accepted range `64MiB–1TiB` |

- The cap takes effect when the engine daemon starts; after saving a new value, restart the daemon (console button, or `rmem daemon restart`).
- `rmem daemon status --json` reports the effective cap and where it came from (`auto` / `explicit` / `env` / `off` / `unavailable`).
- If the `GOMEMLIMIT` environment variable is set, it wins over the configuration file (standard Go runtime semantics).
- If the physical-memory probe fails, no cap is applied — the daemon keeps the runtime default rather than guessing.

### 2.4 Storage profile — `[storage]`

| Key | Default | Meaning |
|---|---|---|
| `storage.profile` | `local` | `local` for normal installs; `usb` is selected automatically for portable payloads |

---

## 3. Environment variables (common subset)

Every `config.toml` key has an environment counterpart that wins over the file; flags win over both.

| Variable | Overrides |
|---|---|
| `ROLLING_MEMORY_CONFIG` | Configuration file path itself |
| `ROLLING_MEMORY_DB` | `core.db` |
| `ROLLING_MEMORY_LOG` | `core.log` |
| `ROLLING_MEMORY_SCOPE` | `core.scope` |
| `ROLLING_MEMORY_READER` | `core.reader` |
| `ROLLING_MEMORY_DELIVERY_WINDOW` | `recall.delivery_window` |
| `ROLLING_MEMORY_DAEMON_MEMORY_LIMIT` | `daemon.memory_limit` |
| `ROLLING_MEMORY_DAEMON_SOCKET` | Engine socket path |
| `ROLLING_MEMORY_PORTABLE` | Force portable mode with an explicit payload root |
| `GOMEMLIMIT` | Go runtime soft memory limit (wins over `daemon.memory_limit` when set) |
| `RM_PANEL_API_PORT` / `RM_PANEL_WEB_PORT` | Console backend / web ports (defaults `9006` / `8008`) |

---

## 4. Equivalent controls in the web console

Open the console and choose **System → Memory Service** in the sidebar:

| Card | Does | CLI equivalent |
|---|---|---|
| Engine Daemon | Status (PID, uptime, heap, effective cap and its source); start / stop / restart | `rmem daemon status\|start\|stop\|restart` |
| Memory Cap | Read and write `daemon.memory_limit`; prompts for a restart to apply | `rmem config set daemon.memory_limit 2GiB` |
| Cache | Drop in-memory index snapshots and read caches (regenerable; stored memory is untouched) | `rmem engine invalidate` |
| Disk Maintenance | Read-only scan, backup/cache cleanup, vault compaction | `rmem maintain status\|clean\|compact` |

Stopping the daemon blocks nothing: the next recall or background sync starts it again automatically.

---

## 5. Verifying a change

```bash
rmem config show            # resolved configuration, including daemon.memory_limit
rmem daemon status --json   # live daemon: pid, uptime, heap, effective memory cap and source
rmem doctor                 # environment and integrity diagnostics
```

If a value is rejected, the write fails with a message naming the accepted values; the file is left unchanged.

---

## 6. Related documents

- [MCP contract](mcp.md) — per-call parameters (including the `delivery_window` override)
- [Multi-harness guide](harness.md) — wiring Rolling Memory into each coding agent
- [USB portable mode](usb-portable.md) — relative paths and the `usb` profile
- [Web console guide](plugin-console.md) — the rest of the console
- [Privacy and local data](privacy.md) — what stays on your machine
