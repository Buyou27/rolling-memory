# Built-in Console Command Line (Terminal)

**[English](console-terminal.md) · [简体中文](zh-CN/console-terminal.md)**

Rolling Memory's admin console ships a lightweight built-in command line (the panel's "Terminal"): a single window that shows the live stream of what the memory engine is doing *and* runs the common commands by hand. Watch `recall` hits and latency in real time, see the most recently ingested memory units, and run `rmem` (`rolling-memory`) diagnostic and maintenance commands with one click. Everything runs against your local plaintext vault on this machine — zero cloud requests.

---

## 1. How to open it

| Entry point | Location |
|---|---|
| Header terminal icon (Rolling Memory CLI Terminal) | Top bar of the admin console, expand/collapse anytime |
| Desktop widget | Terminal entry inside the floating widget at the bottom-right corner |
| Overview page | Both the runtime status card and the page footer offer an "Open Terminal" button |

The terminal is a floating window; its expanded state survives page switches, and the entry shows an unread dot when a task finishes.

## 2. What is on screen

1. **Live stream**: engine `recall` events (hit/absent, matched blocks, injected chars, milliseconds) and host hook events scroll line by line; lines tagged `RAME:INGEST` echo the most recently ingested memory units — rendered as hydrated text, never as raw internal pointers.
2. **Task cards**: every executed command gets a card with the command line, its full output, and a success/failure status; cards support **re-run** and one-click **clean completed**.
3. **Quick command buttons**: `rmem status`, `rmem doctor`, `rmem recent`, plus `rolling-memory check` and `rolling-memory sync prune` (read-only preview) under "More CLI Commands".
4. **Stream controls**: pause/resume the live stream, toggle auto-scroll, and filter event sources by `ALL / HOOK / RAME`.

## 3. Common command reference

| Command | What it does | Where to run it |
|---|---|---|
| `rmem status --json` | Engine status: version, mode, vault path and size | Terminal (button) |
| `rmem doctor --json` | Self-check: environment, harness wiring, vault health | Terminal (button) |
| `rmem recent --limit 10 --json` | Most recently ingested memory units (hydrated text) | Terminal (button) |
| `rmem check --json` | Consistency check: indexes and protocol contracts | Terminal (dropdown) |
| `rmem sync prune --json` | **Retire & self-heal preview** (read-only): verified / re-anchorable / provably-stale / unreadable-source counts | Terminal (dropdown) |
| `rmem recall --cue "keyword" --json` | Run one manual recall to debug "why didn't it hit" | System terminal |
| `rmem sync watch --pause / --resume / --step` | Background scanning and historical pointer index backfill controls | Buttons on the L2 dialogue page |
| `rmem sync prune --apply --yes` | **Apply maintenance**: retire provably-stale units, re-anchor drifted pointers, write an auditable receipt | Overview → "Retire & Self-heal" entry |
| `rmem eval --suite atm …` | Frozen benchmark evaluation | Evaluation lab |

> The terminal only exposes supported read-only diagnostics; every operation that deletes or rewrites data (applying maintenance, deleting memory units, unmounting directories, system updates) goes through the explicit confirmation entry on its own page and is never a one-click terminal action.

## 4. Safety boundaries

- **Allow-listed execution**: the terminal only runs supported command keys; unknown input is never handed to a system shell, and the child process environment strips cloud credentials so local mode (`local_plain`) stays zero-egress.
- **Display discipline**: every echo and output renders hydrated text; rows whose source cannot be resolved show an honest "source unavailable" placeholder, never a raw internal pointer.
- **Traceable results**: command output stays in the task card and can be re-run anytime; destructive maintenance commands return their receipt file path (archived under `~/.local/state/rolling-memory/`).
- **Shared with the pages**: the terminal and the console pages share one local vault and one engine implementation — a change made on either side shows up faithfully on the other (e.g. the environment health counters refresh right after maintenance runs).
