#!/usr/bin/env node
// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Rolling-Memory MCP launcher (cross-platform).
//
// Resolves an installed local Rolling-Memory binary and starts it as `mcp`,
// mirroring bin/rolling-memory-lib.sh. Used by the CodeBuddy / WorkBuddy plugin
// bundle where the host manages Node and POSIX sh may be absent on Windows.
'use strict';
const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');

function dataDir(home) {
  if (process.platform === 'win32') {
    return path.join(process.env.LOCALAPPDATA || path.join(home, 'AppData', 'Local'), 'rolling-memory');
  }
  return path.join(process.env.XDG_DATA_HOME || path.join(home, '.local', 'share'), 'rolling-memory');
}

// Explicit binary overrides are authoritative, matching the POSIX launcher.
function candidateCommands(home) {
  if (process.env.ROLLING_MEMORY_BIN) {
    return [process.env.ROLLING_MEMORY_BIN];
  }
  const exe = process.platform === 'win32' ? '.exe' : '';
  const binDir = path.join(dataDir(home), 'bin');
  const candidates = [];
  if (process.platform !== 'win32') {
    candidates.push(path.join(binDir, 'rolling-memory.sh'));
  }
  candidates.push(path.join(binDir, 'rolling-memory' + exe));
  candidates.push(path.join(home, '.local', 'bin', 'rolling-memory' + exe));
  return candidates;
}

function execCommand(command, args) {
  if (process.platform === 'win32' && /\.(cmd|bat)$/i.test(command)) {
    return spawnSync('cmd', ['/c', command, ...args], { stdio: 'inherit' });
  }
  return spawnSync(command, args, { stdio: 'inherit' });
}

function main() {
  const home = process.env.HOME || process.env.USERPROFILE || os.homedir();
  for (const candidate of candidateCommands(home)) {
    try {
      if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
        const result = execCommand(candidate, ['mcp']);
        if (result.error) {
          // keep probing: a candidate that cannot be spawned (lost exec bit,
          // wrong architecture) must not stop the next one
          process.stderr.write('[rm-mcp] cannot run ' + candidate + ': ' + result.error.message + '\n');
          continue;
        }
        process.exit(typeof result.status === 'number' ? result.status : 127);
      }
    } catch (err) {
      // keep probing: an unreadable candidate must not stop the next one
    }
  }
  process.stderr.write('[rm-mcp] no usable local Rolling-Memory binary found; install or build the CLI before enabling the plugin\n');
  process.exit(127);
}

main();
