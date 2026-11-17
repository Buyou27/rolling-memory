#!/bin/sh
# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>

# Rolling-Memory 插件公共层（纯 POSIX sh · 跨操作系统兼容 macOS / Linux / Alpine / NixOS / FreeBSD / Windows MSYS2）
# 正本：github.com/rocareer/rolling-memory bin/rolling-memory-lib.sh

rm_resolve_bin() {
  # 1) 显式环境变量覆盖（探针 / 高级布点用；设置即权威，不可执行则直报错退出 127）
  if [ -n "${ROLLING_MEMORY_BIN:-}" ]; then
    if [ -f "${ROLLING_MEMORY_BIN}" ] && [ -x "${ROLLING_MEMORY_BIN}" ]; then
      printf '%s\n' "${ROLLING_MEMORY_BIN}"
      return 0
    fi
    printf '[rm-mcp] 未找到可用的 Rolling-Memory 二进制：ROLLING_MEMORY_BIN 指向的路径不可执行（%s）\n' "${ROLLING_MEMORY_BIN}" >&2
    return 127
  fi

  # 2) 标准单机核心启动器与常见安装位置（含 Windows LOCALAPPDATA 与 POSIX XDG）
  for cand in \
    "${XDG_DATA_HOME:-${HOME}/.local/share}/rolling-memory/bin/rolling-memory.sh" \
    "${XDG_DATA_HOME:-${HOME}/.local/share}/rolling-memory/bin/rolling-memory" \
    "${XDG_DATA_HOME:-${HOME}/.local/share}/rolling-memory/bin/rolling-memory.exe" \
    "${LOCALAPPDATA:-${HOME}/AppData/Local}/rolling-memory/bin/rolling-memory.exe" \
    "${HOME}/.local/bin/rolling-memory" \
    "$(command -v rolling-memory 2>/dev/null || true)"; do
    if [ -n "$cand" ] && [ -f "$cand" ] && [ -x "$cand" ]; then
      printf '%s\n' "$cand"
      return 0
    fi
  done

  printf '[rm-mcp] 未找到 Rolling-Memory 二进制。先完成一次性安装：\n' >&2
  printf '  1) 取二进制：产品发布渠道下载，或自行构建 CGO_ENABLED=0 go build ./cmd/rolling-memory\n' >&2
  printf '  2) 放到 ~/.local/bin/rolling-memory 后执行：rolling-memory install --with-hooks\n' >&2
  return 127
}

rm_dedupe_zcode() {
  bin="$(rm_resolve_bin 2>/dev/null)" || return 0
  "$bin" install zcode --with-hooks >/dev/null 2>&1 || true
}
