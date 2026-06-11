'use strict';

// Tests for the PAKKA_DISABLED=1 kill-switch (issue #13) — run with:
//   node --test hooks/pakka-disabled.test.js
//
// Contract: with PAKKA_DISABLED=1 in the environment, every pakka hook
// entry point (bin/run shell wrapper + each JS hook) exits 0 and emits
// NOTHING on stdout — zero pakka injection. This is what makes the raw
// arm of `pakka-core bench` raw while keeping the same Claude Code
// session config and inherited OAuth.
//
// Uses Node 18+ built-in test runner and child_process.spawnSync. No external deps.

const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const HOOKS_DIR = __dirname;
const PLUGIN_ROOT = path.join(__dirname, '..');
const BIN_RUN = path.join(PLUGIN_ROOT, 'bin', 'run');

function makeTmpDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pakka-disabled-test-'));
  return {
    dir,
    cleanup() {
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

// spawnScript runs a JS hook with the given env overrides and stdin.
// PAKKA_DISABLED is inherited ONLY when the test sets it explicitly, so a
// stray value in the outer environment can't skew the control arms.
function spawnScript(script, env, stdin) {
  const merged = { ...process.env, ...env };
  if (!('PAKKA_DISABLED' in env)) delete merged.PAKKA_DISABLED;
  const result = spawnSync(process.execPath, [path.join(HOOKS_DIR, script)], {
    encoding: 'utf8',
    input: stdin || '',
    env: merged,
    timeout: 5000,
  });
  if (result.error) throw result.error;
  return result;
}

// ---------------------------------------------------------------------------
// bin/run (shell wrapper — routes every Go-side hook)
// ---------------------------------------------------------------------------

test('bin/run: PAKKA_DISABLED=1 exits 0 with empty stdout, even for a bogus subcommand', () => {
  const result = spawnSync(BIN_RUN, ['definitely-not-a-subcommand'], {
    encoding: 'utf8',
    env: { ...process.env, PAKKA_DISABLED: '1' },
    timeout: 5000,
  });
  assert.equal(result.status, 0, 'must exit 0');
  assert.equal(result.stdout, '', 'must emit nothing on stdout');
  assert.equal(result.stderr, '', 'must emit nothing on stderr');
});

test('bin/run: without PAKKA_DISABLED the same bogus subcommand reaches the binary and fails', () => {
  const env = { ...process.env };
  delete env.PAKKA_DISABLED;
  const result = spawnSync(BIN_RUN, ['definitely-not-a-subcommand'], {
    encoding: 'utf8',
    env,
    timeout: 5000,
  });
  // The wrapper must NOT short-circuit: pakka-core rejects the subcommand
  // with a non-zero exit. (Proves the kill-switch varies with the env var.)
  assert.notEqual(result.status, 0, 'must dispatch to pakka-core and fail');
});

test('bin/run: PAKKA_DISABLED=0 does not engage the kill-switch', () => {
  const result = spawnSync(BIN_RUN, ['definitely-not-a-subcommand'], {
    encoding: 'utf8',
    env: { ...process.env, PAKKA_DISABLED: '0' },
    timeout: 5000,
  });
  assert.notEqual(result.status, 0, 'PAKKA_DISABLED=0 must behave as enabled');
});

// ---------------------------------------------------------------------------
// compress-start.js (SessionStart — would otherwise inject the ruleset)
// ---------------------------------------------------------------------------

test('compress-start.js: PAKKA_DISABLED=1 emits nothing and exits 0', () => {
  const t = makeTmpDir();
  try {
    const result = spawnScript('compress-start.js', {
      CLAUDE_PLUGIN_ROOT: PLUGIN_ROOT,
      CLAUDE_CONFIG_DIR: t.dir,
      PAKKA_DEFAULT_LEVEL: 'ultra', // would normally force ruleset output
      PAKKA_DISABLED: '1',
    });
    assert.equal(result.status, 0);
    assert.equal(result.stdout, '');
    // And no flag file side effect either.
    assert.equal(fs.existsSync(path.join(t.dir, '.pakka-level')), false, 'no flag file may be written');
  } finally {
    t.cleanup();
  }
});

test('compress-start.js: without PAKKA_DISABLED the ruleset is emitted (control arm)', () => {
  const t = makeTmpDir();
  try {
    const env = {
      CLAUDE_PLUGIN_ROOT: PLUGIN_ROOT,
      CLAUDE_CONFIG_DIR: t.dir,
      PAKKA_DEFAULT_LEVEL: 'ultra',
    };
    const result = spawnScript('compress-start.js', env);
    assert.equal(result.status, 0);
    assert.ok(result.stdout.length > 0, 'control arm must emit the ruleset');
    assert.match(result.stdout, /PAKKA COMPRESSION ACTIVE/);
  } finally {
    t.cleanup();
  }
});

// ---------------------------------------------------------------------------
// compress-track.js (UserPromptSubmit — would otherwise emit reinforcement)
// ---------------------------------------------------------------------------

test('compress-track.js: PAKKA_DISABLED=1 emits nothing even with an active level flag', () => {
  const t = makeTmpDir();
  try {
    fs.writeFileSync(path.join(t.dir, '.pakka-level'), 'ultra', { mode: 0o600 });
    const result = spawnScript(
      'compress-track.js',
      { CLAUDE_PLUGIN_ROOT: PLUGIN_ROOT, CLAUDE_CONFIG_DIR: t.dir, PAKKA_DISABLED: '1' },
      JSON.stringify({ prompt: 'please fix the login bug' })
    );
    assert.equal(result.status, 0);
    assert.equal(result.stdout, '');
  } finally {
    t.cleanup();
  }
});

test('compress-track.js: without PAKKA_DISABLED reinforcement is emitted (control arm)', () => {
  const t = makeTmpDir();
  try {
    fs.writeFileSync(path.join(t.dir, '.pakka-level'), 'ultra', { mode: 0o600 });
    const result = spawnScript(
      'compress-track.js',
      { CLAUDE_PLUGIN_ROOT: PLUGIN_ROOT, CLAUDE_CONFIG_DIR: t.dir },
      JSON.stringify({ prompt: 'please fix the login bug' })
    );
    assert.equal(result.status, 0);
    assert.ok(result.stdout.length > 0, 'control arm must emit reinforcement');
    assert.match(result.stdout, /PAKKA COMPRESSION ACTIVE/);
  } finally {
    t.cleanup();
  }
});

// ---------------------------------------------------------------------------
// skill-check-start.js (SessionStart — would otherwise inject skill-check)
// ---------------------------------------------------------------------------

test('skill-check-start.js: PAKKA_DISABLED=1 emits nothing and exits 0', () => {
  const result = spawnScript('skill-check-start.js', {
    CLAUDE_PLUGIN_ROOT: PLUGIN_ROOT,
    PAKKA_DISABLED: '1',
  });
  assert.equal(result.status, 0);
  assert.equal(result.stdout, '');
});

test('skill-check-start.js: without PAKKA_DISABLED the directive is emitted (control arm)', () => {
  const result = spawnScript('skill-check-start.js', { CLAUDE_PLUGIN_ROOT: PLUGIN_ROOT });
  assert.equal(result.status, 0);
  assert.ok(result.stdout.length > 0, 'control arm must emit skill-check context');
  assert.match(result.stdout, /additionalContext/);
});
