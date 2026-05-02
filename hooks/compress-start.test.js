'use strict';

// Tests for compress-start.js — run with: node --test hooks/compress-start.test.js
// Uses Node 18+ built-in test runner and child_process.spawnSync. No external deps.

const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

// Absolute path to the script under test
const SCRIPT = path.join(__dirname, 'compress-start.js');

// ---------------------------------------------------------------------------
// Helper: create a temp dir and return { dir, cleanup }
// ---------------------------------------------------------------------------
function makeTmpDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pakka-start-test-'));
  return {
    dir,
    cleanup() {
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

// ---------------------------------------------------------------------------
// spawnStart — runs compress-start.js as a child process.
//
// Sets CLAUDE_PLUGIN_ROOT and CLAUDE_CONFIG_DIR to pluginRoot so the rules
// file resolves correctly (compress-start.js uses __dirname/../rules, so the
// real project rules file is loaded). Any additional env overrides go in env.
// ---------------------------------------------------------------------------
function spawnStart(pluginRoot, env = {}) {
  const result = spawnSync(process.execPath, [SCRIPT], {
    encoding: 'utf8',
    env: {
      ...process.env,
      CLAUDE_PLUGIN_ROOT: pluginRoot,
      CLAUDE_CONFIG_DIR: pluginRoot, // reuse as config dir
      PAKKA_DEFAULT_LEVEL: 'ultra',
      ...env,
    },
    timeout: 5000,
  });
  if (result.error) throw result.error;
  return result.stdout || '';
}

// ---------------------------------------------------------------------------
// Cycle 1 — Verification rule injected
// ---------------------------------------------------------------------------

test('Cycle1: stdout contains "Verification discipline" when level is active', () => {
  const tmp = makeTmpDir();
  try {
    // Create minimal rules/output-compress.md so the main code path runs
    const rulesDir = path.join(tmp.dir, 'rules');
    fs.mkdirSync(rulesDir, { recursive: true });
    fs.writeFileSync(
      path.join(rulesDir, 'output-compress.md'),
      'PAKKA COMPRESSION ACTIVE — level: ultra\n\n## Rules\nDrop filler.\n',
    );

    const stdout = spawnStart(tmp.dir);
    assert.ok(
      stdout.includes('Verification discipline'),
      'stdout should contain "Verification discipline"',
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle1: stdout contains the full verification rule text', () => {
  const tmp = makeTmpDir();
  try {
    const rulesDir = path.join(tmp.dir, 'rules');
    fs.mkdirSync(rulesDir, { recursive: true });
    fs.writeFileSync(
      path.join(rulesDir, 'output-compress.md'),
      'PAKKA COMPRESSION ACTIVE — level: ultra\n\n## Rules\nDrop filler.\n',
    );

    const stdout = spawnStart(tmp.dir);
    assert.ok(
      stdout.includes('Exit 0 = evidence'),
      'stdout should contain "Exit 0 = evidence" from the verification rule',
    );
  } finally {
    tmp.cleanup();
  }
});

// ---------------------------------------------------------------------------
// Cycle 2 — Skill-check discipline injected
// ---------------------------------------------------------------------------

test('Cycle2: stdout contains "Skill-check discipline" when level is active', () => {
  const tmp = makeTmpDir();
  try {
    const rulesDir = path.join(tmp.dir, 'rules');
    fs.mkdirSync(rulesDir, { recursive: true });
    fs.writeFileSync(
      path.join(rulesDir, 'output-compress.md'),
      'PAKKA COMPRESSION ACTIVE — level: ultra\n\n## Rules\nDrop filler.\n',
    );

    const stdout = spawnStart(tmp.dir);
    assert.ok(
      stdout.includes('Skill-check discipline'),
      'stdout should contain "Skill-check discipline"',
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle2: stdout contains /pakka:plan and /pakka:build and /pakka:review mentions', () => {
  const tmp = makeTmpDir();
  try {
    const rulesDir = path.join(tmp.dir, 'rules');
    fs.mkdirSync(rulesDir, { recursive: true });
    fs.writeFileSync(
      path.join(rulesDir, 'output-compress.md'),
      'PAKKA COMPRESSION ACTIVE — level: ultra\n\n## Rules\nDrop filler.\n',
    );

    const stdout = spawnStart(tmp.dir);
    assert.ok(stdout.includes('/pakka:plan'), 'stdout should mention /pakka:plan');
    assert.ok(stdout.includes('/pakka:build'), 'stdout should mention /pakka:build');
    assert.ok(stdout.includes('/pakka:review'), 'stdout should mention /pakka:review');
  } finally {
    tmp.cleanup();
  }
});

// ---------------------------------------------------------------------------
// Cycle 3 — level=off exits cleanly (no behaviors injected)
// ---------------------------------------------------------------------------

test('Cycle3: level=off — stdout is exactly "OK" (no behaviors injected)', () => {
  const tmp = makeTmpDir();
  try {
    const stdout = spawnStart(tmp.dir, { PAKKA_DEFAULT_LEVEL: 'off' });
    assert.equal(stdout, 'OK', 'level=off should output exactly "OK"');
  } finally {
    tmp.cleanup();
  }
});

test('Cycle3: level=off — stdout does NOT contain "Verification discipline"', () => {
  const tmp = makeTmpDir();
  try {
    const stdout = spawnStart(tmp.dir, { PAKKA_DEFAULT_LEVEL: 'off' });
    assert.ok(
      !stdout.includes('Verification discipline'),
      'level=off should not inject Verification discipline',
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle3: level=off — stdout does NOT contain "Skill-check discipline"', () => {
  const tmp = makeTmpDir();
  try {
    const stdout = spawnStart(tmp.dir, { PAKKA_DEFAULT_LEVEL: 'off' });
    assert.ok(
      !stdout.includes('Skill-check discipline'),
      'level=off should not inject Skill-check discipline',
    );
  } finally {
    tmp.cleanup();
  }
});

// ---------------------------------------------------------------------------
// Fallback path — behaviors also injected when rules file is absent
// ---------------------------------------------------------------------------

test('Fallback: when rules file absent, stdout still contains "Verification discipline"', () => {
  const tmp = makeTmpDir();
  try {
    // No rules file created — compress-start.js falls back to hardcoded minimal ruleset
    const stdout = spawnStart(tmp.dir);
    assert.ok(
      stdout.includes('Verification discipline'),
      'fallback path should also inject Verification discipline',
    );
  } finally {
    tmp.cleanup();
  }
});

test('Fallback: when rules file absent, stdout still contains "Skill-check discipline"', () => {
  const tmp = makeTmpDir();
  try {
    const stdout = spawnStart(tmp.dir);
    assert.ok(
      stdout.includes('Skill-check discipline'),
      'fallback path should also inject Skill-check discipline',
    );
  } finally {
    tmp.cleanup();
  }
});
