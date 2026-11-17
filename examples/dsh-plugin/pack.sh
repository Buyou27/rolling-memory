#!/usr/bin/env bash
# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>

set -euo pipefail

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
    printf 'usage: %s EXISTING_OUTPUT_DIRECTORY\n' "$0" >&2
    exit 2
fi

PLUGIN="$(cd "$(dirname "$0")" && pwd -P)"
ROOT="$(cd "${PLUGIN}/../.." && pwd -P)"
OUT="$(cd "$1" && pwd -P)"
BUILD="$(mktemp -d "${OUT}/.rolling-memory-dsh-pack.XXXXXX")"
cleanup() { rm -rf "${BUILD}"; }
trap cleanup EXIT

mkdir -p "${BUILD}/package/skills/rolling-memory-dsh" "${BUILD}/archive" "${BUILD}/cache"
cp "${PLUGIN}/package.json" "${PLUGIN}/check-pack.sh" "${PLUGIN}/cordis.patch.yml" "${PLUGIN}/README.md" "${BUILD}/package/"
cp "${PLUGIN}/skills/rolling-memory-dsh/SKILL.md" "${BUILD}/package/skills/rolling-memory-dsh/"
cp "${PLUGIN}/LICENSE" "${BUILD}/package/LICENSE"
cmp -s "${ROOT}/LICENSE" "${BUILD}/package/LICENSE"

ARCHIVE="$(npm_config_cache="${BUILD}/cache" npm_config_offline=true npm_config_update_notifier=false \
    npm pack "${BUILD}/package" --ignore-scripts --pack-destination "${BUILD}/archive")"
if ! ln "${BUILD}/archive/${ARCHIVE}" "${OUT}/${ARCHIVE}"; then
    printf 'cannot create package without overwriting: %s\n' "${OUT}/${ARCHIVE}" >&2
    exit 1
fi
printf '%s\n' "${OUT}/${ARCHIVE}"
