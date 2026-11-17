# Accessibility

**[English](ACCESSIBILITY.md) · [简体中文](docs/zh-CN/ACCESSIBILITY.md)**

Accessibility is part of quality: a barrier in an interface we control is a defect, not a
special request. This page describes the interfaces the project ships, the environments we
support today, the limits we already know about, and how to report a barrier.

## Interfaces

Rolling Memory has four user-facing surfaces:

- **CLI (`rolling-memory` / `rmem`)** — plain-text input and output; results are sectioned
  with text headings, so they work with screen readers and can be piped to other tools.
- **MCP tool (`recall`)** — returns structured text; presentation follows your host
  harness (editor, chat UI, or terminal).
- **Web console (`rolling-memory panel`)** — built on a standard, keyboard-operable
  component library; the layout is usable with browser zoom and system text scaling.
- **Desktop launcher** — a standard platform menu window that opens the web console.

Documentation is maintained in English and Simplified Chinese as a one-to-one mirror.

## Supported environments

- Desktop browsers at current versions: Chrome, Edge, Firefox and Safari.
- macOS, Windows and Linux for the CLI and the console; the desktop package targets macOS
  and Windows.
- Any terminal emulator for the CLI, including screen-reader-driven setups.

## Known limitations

- A formal third-party accessibility audit has not been performed yet.
- Some console views are visual by design — benchmark charts and the memory graph are
  pointer-oriented and do not yet have complete text alternatives; the underlying numbers
  are also shown as text in the adjacent panels and tables.
- The console targets desktop viewports; small-screen use is best-effort.
- Systematic assistive-technology testing is not yet part of every release; reports are the
  fastest path to a fix.

## Reporting a barrier

- Email `support@psychologylake.org.cn` with the subject prefix `[accessibility]`, or open
  an issue at <https://github.com/rocareer/rolling-memory/issues>.
- Include the interface (CLI / MCP / console / desktop / documentation), your operating
  system and assistive technology (screen reader, magnifier, voice control), the steps you
  took, and what you expected versus what happened.
- Remove vault contents, secrets and personal data from anything you attach.

Reports are triaged on a best-effort basis like other issues; see [SUPPORT.md](SUPPORT.md)
for response expectations.
