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
function spawnStart(pluginRoot, env = {}, stdin = '') {
  const result = spawnSync(process.execPath, [SCRIPT], {
    encoding: 'utf8',
    input: stdin,
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
  return result;
}

// stdoutOf — convenience wrapper returning just stdout (most tests only need it).
function stdoutOf(pluginRoot, env = {}, stdin = '') {
  return spawnStart(pluginRoot, env, stdin).stdout || '';
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

    const stdout = stdoutOf(tmp.dir);
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

    const stdout = stdoutOf(tmp.dir);
    assert.ok(
      stdout.includes('Exit 0 = evidence'),
      'stdout should contain "Exit 0 = evidence" from the verification rule',
    );
  } finally {
    tmp.cleanup();
  }
});

// ---------------------------------------------------------------------------
// Cycle 2 — Skill-check discipline lives in skill-check-start.js, NOT here.
// compress-start.js emits compression rules + Verification discipline only;
// the skill-check directive is a separate SessionStart hook so the model
// sees it before the long ruleset (see skill-check-start.js header).
// ---------------------------------------------------------------------------

test('Cycle2: compress-start does NOT inject skill-check (moved to skill-check-start.js)', () => {
  const tmp = makeTmpDir();
  try {
    const rulesDir = path.join(tmp.dir, 'rules');
    fs.mkdirSync(rulesDir, { recursive: true });
    fs.writeFileSync(
      path.join(rulesDir, 'output-compress.md'),
      'PAKKA COMPRESSION ACTIVE — level: ultra\n\n## Rules\nDrop filler.\n',
    );

    const stdout = stdoutOf(tmp.dir);
    assert.ok(
      !stdout.includes('Skill-check discipline'),
      'skill-check injection belongs to skill-check-start.js, not compress-start.js',
    );
    assert.ok(stdout.includes('Verification discipline'), 'ambient behaviors still appended');
  } finally {
    tmp.cleanup();
  }
});

test('Cycle2: skill-check-start.js mentions /pakka:plan, /pakka:build, /pakka:review', () => {
  const result = spawnSync(
    process.execPath,
    [path.join(__dirname, 'skill-check-start.js')],
    {
      encoding: 'utf8',
      env: { ...process.env, CLAUDE_PLUGIN_ROOT: path.join(__dirname, '..') },
      timeout: 5000,
    },
  );
  if (result.error) throw result.error;
  const stdout = result.stdout || '';
  assert.ok(stdout.includes('/pakka:plan'), 'stdout should mention /pakka:plan');
  assert.ok(stdout.includes('/pakka:build'), 'stdout should mention /pakka:build');
  assert.ok(stdout.includes('/pakka:review'), 'stdout should mention /pakka:review');
});

// ---------------------------------------------------------------------------
// Cycle 3 — level=off exits cleanly (no behaviors injected)
// ---------------------------------------------------------------------------

test('Cycle3: level=off — stdout is exactly "OK" (no behaviors injected)', () => {
  const tmp = makeTmpDir();
  try {
    const stdout = stdoutOf(tmp.dir, { PAKKA_DEFAULT_LEVEL: 'off' });
    assert.equal(stdout, 'OK', 'level=off should output exactly "OK"');
  } finally {
    tmp.cleanup();
  }
});

test('Cycle3: level=off — stdout does NOT contain "Verification discipline"', () => {
  const tmp = makeTmpDir();
  try {
    const stdout = stdoutOf(tmp.dir, { PAKKA_DEFAULT_LEVEL: 'off' });
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
    const stdout = stdoutOf(tmp.dir, { PAKKA_DEFAULT_LEVEL: 'off' });
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
    const stdout = stdoutOf(tmp.dir);
    assert.ok(
      stdout.includes('Verification discipline'),
      'fallback path should also inject Verification discipline',
    );
  } finally {
    tmp.cleanup();
  }
});

test('Fallback: when rules file absent, skill-check is still NOT injected here', () => {
  const tmp = makeTmpDir();
  try {
    const stdout = stdoutOf(tmp.dir);
    assert.ok(
      !stdout.includes('Skill-check discipline'),
      'fallback path must not inject skill-check (moved to skill-check-start.js)',
    );
  } finally {
    tmp.cleanup();
  }
});

// ---------------------------------------------------------------------------
// Compaction re-injection guard (spec 2026-07-22-compaction-survival)
//
// After a compaction, Claude Code 2.1 fires SessionStart with source:"compact"
// (PostCompact itself is side-effects only and CANNOT inject context — verified
// against code.claude.com/docs/en/hooks.md). So the disciplines survive a
// compaction ONLY because compress-start.js is a SessionStart hook whose matcher
// also matches the "compact" source. This test pins that: narrowing the
// SessionStart matcher (e.g. to "startup") — which would silently drop
// re-injection after every compaction — turns this red.
// ---------------------------------------------------------------------------

test('SessionStart matcher covers compaction source — compaction re-injection depends on this', () => {
  const hooks = JSON.parse(
    fs.readFileSync(path.join(__dirname, 'hooks.json'), 'utf8'),
  );
  const entries = hooks.hooks && hooks.hooks.SessionStart;
  assert.ok(Array.isArray(entries) && entries.length > 0, 'SessionStart must be registered');

  // The entry that runs compress-start.js is the one that re-injects the ruleset.
  const injector = entries.find(
    (e) =>
      Array.isArray(e.hooks) &&
      e.hooks.some((h) => typeof h.command === 'string' && h.command.includes('compress-start.js')),
  );
  assert.ok(injector, 'a SessionStart entry must run compress-start.js');

  // Its matcher regex must match both a normal startup and a post-compaction
  // start, or compaction re-injection breaks.
  const re = new RegExp(injector.matcher);
  assert.ok(re.test('compact'), `SessionStart matcher ${injector.matcher} must match source "compact"`);
  assert.ok(re.test('startup'), `SessionStart matcher ${injector.matcher} must match source "startup"`);
});
