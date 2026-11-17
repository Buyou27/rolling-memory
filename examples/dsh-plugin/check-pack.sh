#!/usr/bin/env bash
# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>

set -euo pipefail

PACKAGE="$(cd "$(dirname "$0")" && pwd -P)"
if [ ! -s "${PACKAGE}/LICENSE" ] || ! grep -Fq 'Required Notice: Copyright (c) 2026 Rocareer & the Rolling-Memory' "${PACKAGE}/LICENSE"; then
    printf 'Build the distributable tarball with bash examples/dsh-plugin/pack.sh OUTPUT_DIRECTORY.\n' >&2
    exit 1
fi
