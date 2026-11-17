#!/usr/bin/env bash
# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>

set -euo pipefail

PLUGIN="$(cd "$(dirname "$0")" && pwd -P)"
ROOT="$(cd "${PLUGIN}/../.." && pwd -P)"
TEST="$(mktemp -d "${TMPDIR:-/tmp}/rolling-memory-dsh-test.XXXXXX")"
cleanup() { rm -rf "${TEST}"; }
trap cleanup EXIT

# The directory is distributed in self-contained form: its LICENSE copy must
# stay byte-identical to the repository root license.
if ! cmp -s "${PLUGIN}/LICENSE" "${ROOT}/LICENSE"; then
    printf 'plugin LICENSE has drifted from the repository root LICENSE\n' >&2
    exit 1
fi

mkdir -p "${TEST}/home" "${TEST}/out"
export HOME="${TEST}/home"
export XDG_CONFIG_HOME="${HOME}/config"
export XDG_DATA_HOME="${HOME}/data"
export XDG_STATE_HOME="${HOME}/state"
export npm_config_cache="${HOME}/npm-cache"
export npm_config_offline=true
export npm_config_update_notifier=false

bash "${PLUGIN}/pack.sh" "${TEST}/out" > "${TEST}/package-path"
ARCHIVE="$(<"${TEST}/package-path")"
tar -tzf "${ARCHIVE}" > "${TEST}/contents"
for path in package/package.json package/check-pack.sh package/cordis.patch.yml package/README.md package/LICENSE package/skills/rolling-memory-dsh/SKILL.md; do
    grep -Fxq "${path}" "${TEST}/contents"
done
tar -xOzf "${ARCHIVE}" package/LICENSE | cmp -s - "${ROOT}/LICENSE"
tar -xOzf "${ARCHIVE}" package/skills/rolling-memory-dsh/SKILL.md | cmp -s - "${PLUGIN}/skills/rolling-memory-dsh/SKILL.md"
mkdir -p "${TEST}/unpacked" "${TEST}/repacked"
tar -xzf "${ARCHIVE}" -C "${TEST}/unpacked"
npm pack "${TEST}/unpacked/package" --pack-destination "${TEST}/repacked" > /dev/null
tar -xOzf "${TEST}/repacked/$(basename "${ARCHIVE}")" package/LICENSE | cmp -s - "${ROOT}/LICENSE"
# Packaging directly from the plugin directory must also produce a complete,
# license-carrying package (the directory is self-contained).
mkdir -p "${TEST}/direct"
npm pack "${PLUGIN}" --pack-destination "${TEST}/direct" > /dev/null
tar -tzf "${TEST}/direct/$(basename "${ARCHIVE}")" > "${TEST}/direct-contents"
for path in package/package.json package/LICENSE package/cordis.patch.yml package/skills/rolling-memory-dsh/SKILL.md; do
    grep -Fxq "${path}" "${TEST}/direct-contents"
done
tar -xOzf "${TEST}/direct/$(basename "${ARCHIVE}")" package/LICENSE | cmp -s - "${ROOT}/LICENSE"
shasum -a 256 "${ARCHIVE}" > "${TEST}/before"
if bash "${PLUGIN}/pack.sh" "${TEST}/out" > /dev/null 2>&1; then
    printf 'pack unexpectedly overwrote the existing archive\n' >&2
    exit 1
fi
shasum -a 256 "${ARCHIVE}" > "${TEST}/after"
cmp -s "${TEST}/before" "${TEST}/after"
node -e 'const p = require(process.argv[1]); if (p.private || p.engines.dsh !== "0.2.2-rc.3" || p.dsh.bundle.patch !== "./cordis.patch.yml") process.exit(1)' "${PLUGIN}/package.json"
printf 'bundle package, license, skill, manifest, direct pack and overwrite guard: ok\n'
