# Research

**[English](README.md) · [简体中文](README.zh-CN.md)**

Material for people who study the engine rather than wire it into an agent: reproducible analysis code, statistics notebooks, and small harnesses that turn the local vault into data.

Nothing here is a runtime dependency. Each subdirectory is self-contained and names its own requirements.

| Directory | Language | What it is |
| :--- | :--- | :--- |
| [r/](r/) | R | Shell-out client (`rmemory.R`), an end-to-end demo on a throwaway vault, and a base-R reproduction of the occupancy-null tail bound |

## Ground rules

- **Read-only by default.** Analysis code should not mutate a vault unless the directory says so. `r/demo.R` builds its own temporary vault and leaves yours alone.
- **Reproducible in one command.** A directory ships a script that prints what it checked, so a reviewer can rerun it without reading the source first.
- **No internal build steps.** If a sample needs the private development tree, it does not belong here.

If you are adding a directory for another language, follow the same shape: a short README, one runnable entry point, and one statistical check that prints pass/fail.
