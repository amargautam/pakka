'use strict';

// Tests for bin/run hot-path routing (hot-path startup floor spec, AC3).
// Run with: node --test hooks/hot-routing.test.js
//
// Contract: bin/run routes guard, commit-gate, and status-line to the lean
// pakka-hot-<os>-<arch> binary WHEN it exists beside pakka-core; every other
// subcommand — and all subcommands when no pakka-hot binary is present (older
// caches) — go to pakka-core-<os>-<arch>. The wrapper must be both forward- and
// backward-compatible.
//
// The test copies bin/run into a throwaway dir, drops in stub "binaries" that
// print their own identity, and asserts which stub actually ran.

const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const BIN_RUN = path.join(__dirname, '..', 'bin', 'run');

// Compute the exact os/arch suffix bin/run will derive, by running the same
// commands it does. This keeps the stub filenames in lockstep with the wrapper.
function osArch() {
  const s = spawnSync('uname', ['-s'], { encoding: 'utf8' }).stdout.trim().toLowerCase();
  let m = spawnSync('uname', ['-m'], { encoding: 'utf8' }).stdout.trim();
  if (m === 'x86_64') m = 'amd64';
  else if (m === 'aarch64') m = 'arm64';
  return `${s}-${m}`;
}

// stubBin writes an executable shell stub that prints `label` and exits 0.
function stubBin(dir, name, label) {
  const p = path.join(dir, name);
  fs.writeFileSync(p, `#!/bin/sh\necho "${label}"\n`, { mode: 0o755 });
  fs.chmodSync(p, 0o755);
}

function makeEnv(dir, { withHot }) {
  const suffix = osArch();
  fs.copyFileSync(BIN_RUN, path.join(dir, 'run'));
  fs.chmodSync(path.join(dir, 'run'), 0o755);
  stubBin(dir, `pakka-core-${suffix}`, 'CORE');
  if (withHot) stubBin(dir, `pakka-hot-${suffix}`, 'HOT');
  return path.join(dir, 'run');
}

// runSub invokes the copied wrapper with a subcommand and returns trimmed stdout.
function runSub(runPath, sub) {
  const env = { ...process.env };
  delete env.PAKKA_DISABLED; // don't let an outer kill-switch skew routing
  const res = spawnSync(runPath, [sub], { encoding: 'utf8', env });
  return (res.stdout || '').trim();
}

function withTmp(fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pakka-route-'));
  try {
    fn(dir);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

test('hot subcommands route to pakka-hot when present', () => {
  withTmp((dir) => {
    const run = makeEnv(dir, { withHot: true });
    for (const sub of ['guard', 'commit-gate', 'status-line']) {
      assert.equal(runSub(run, sub), 'HOT', `${sub} should route to pakka-hot`);
    }
  });
});

test('non-hot subcommands route to pakka-core even when pakka-hot present', () => {
  withTmp((dir) => {
    const run = makeEnv(dir, { withHot: true });
    for (const sub of ['meter', 'audit', 'compress', 'index']) {
      assert.equal(runSub(run, sub), 'CORE', `${sub} should route to pakka-core`);
    }
  });
});

test('hot subcommands fall back to pakka-core when no pakka-hot binary (old cache)', () => {
  withTmp((dir) => {
    const run = makeEnv(dir, { withHot: false });
    for (const sub of ['guard', 'commit-gate', 'status-line']) {
      assert.equal(runSub(run, sub), 'CORE', `${sub} should fall back to pakka-core`);
    }
  });
});
