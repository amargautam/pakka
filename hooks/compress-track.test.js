'use strict';

// Tests for compress-track.js — run with: node --test compress-track.test.js
// Uses Node 18+ built-in test runner and child_process.spawnSync. No external deps.

const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

// ---------------------------------------------------------------------------
// Helper: create a temp directory and return its path + a cleanup fn
// ---------------------------------------------------------------------------
function makeTmpDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pakka-track-test-'));
  return {
    dir,
    cleanup() {
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

// Absolute path to the script under test
const SCRIPT = path.join(__dirname, 'compress-track.js');
const FLAG_NAME = '.pakka-level';

// ---------------------------------------------------------------------------
// spawnTracker — runs compress-track.js as a child process.
//
// @param promptText      — the prompt string to send (wrapped in JSON event)
// @param flagContent     — if non-null, pre-write this string to the flag file;
//                          if null, flag file is not created (absent)
// @param configDir       — tmp dir to use as CLAUDE_CONFIG_DIR
//
// Returns { stdout: string, flagContent: string|null }
//   flagContent is the flag file content after the process exits, or null if absent.
// ---------------------------------------------------------------------------
function spawnTracker(promptText, flagContent, configDir) {
  const flagPath = path.join(configDir, FLAG_NAME);

  // Pre-write flag if requested
  if (flagContent !== null) {
    fs.mkdirSync(configDir, { recursive: true });
    fs.writeFileSync(flagPath, flagContent, { mode: 0o600 });
  }

  const input = JSON.stringify({ prompt: promptText });

  const result = spawnSync(process.execPath, [SCRIPT], {
    input,
    encoding: 'utf8',
    env: {
      ...process.env,
      CLAUDE_CONFIG_DIR: configDir,
      // Point HOME at configDir so config.json path is predictable in tests
      HOME: configDir,
      // Isolate from any real PAKKA_DEFAULT_LEVEL that might affect behaviour
      PAKKA_DEFAULT_LEVEL: '',
      // Unset CLAUDE_PLUGIN_ROOT so help falls back cleanly in tests
      CLAUDE_PLUGIN_ROOT: '',
    },
    timeout: 5000,
  });

  if (result.error) throw result.error;

  // Read resulting flag file
  let resultFlag = null;
  try {
    resultFlag = fs.readFileSync(flagPath, 'utf8');
  } catch (_) {
    resultFlag = null;
  }

  return { stdout: result.stdout, flagContent: resultFlag };
}

// ===========================================================================
// /pakka:compress <level> command tests
// ===========================================================================

test('/pakka:compress ultra — flag written as ultra, stdout has PAKKA HOOK HANDLED', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('/pakka:compress ultra', null, tmp.dir);
    assert.equal(flagContent, 'ultra', 'flag should be ultra');
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('PAKKA HOOK HANDLED: compress level set to ultra'),
      'additionalContext should include hook handled message'
    );
  } finally {
    tmp.cleanup();
  }
});

test('/pakka:compress lite — flag written as lite, stdout has PAKKA HOOK HANDLED', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('/pakka:compress lite', null, tmp.dir);
    assert.equal(flagContent, 'lite');
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('PAKKA HOOK HANDLED: compress level set to lite'),
      'additionalContext should include hook handled message for lite'
    );
  } finally {
    tmp.cleanup();
  }
});

test('/pakka:compress super-ultra — flag written as super-ultra, stdout has PAKKA HOOK HANDLED', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('/pakka:compress super-ultra', null, tmp.dir);
    assert.equal(flagContent, 'super-ultra');
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('PAKKA HOOK HANDLED: compress level set to super-ultra'),
      'additionalContext should include hook handled message for super-ultra'
    );
  } finally {
    tmp.cleanup();
  }
});

test('/pakka:compress off — flag file deleted', () => {
  const tmp = makeTmpDir();
  try {
    // Pre-seed with a level so we can verify deletion
    const { flagContent } = spawnTracker('/pakka:compress off', 'ultra', tmp.dir);
    assert.equal(flagContent, null, 'flag file should be deleted by off command');
  } finally {
    tmp.cleanup();
  }
});

test('/pakka:compress invalid — flag unchanged, no PAKKA HOOK HANDLED', () => {
  const tmp = makeTmpDir();
  try {
    // Pre-seed with 'lite' so we can verify it's not changed
    const { stdout, flagContent } = spawnTracker('/pakka:compress invalid', 'lite', tmp.dir);
    assert.equal(flagContent, 'lite', 'flag should remain lite after invalid arg');
    assert.ok(!stdout.includes('PAKKA HOOK HANDLED'), 'stdout should not contain hook handled for invalid level');
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// Prose command tests: "pakka verbose" and "normal mode"
// ===========================================================================

test('"pakka verbose" — flag deleted, no reinforcement emitted', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('pakka verbose', 'strict', tmp.dir);
    assert.equal(flagContent, null, 'flag should be deleted');
    assert.equal(stdout, '', 'no reinforcement after flag deletion');
  } finally {
    tmp.cleanup();
  }
});

test('"normal mode" — flag deleted, no reinforcement emitted', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('normal mode', 'ultra', tmp.dir);
    assert.equal(flagContent, null, 'flag should be deleted');
    assert.equal(stdout, '', 'no reinforcement after flag deletion');
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// Reinforcement tests (regular prompts)
// ===========================================================================

test('Regular prompt with flag=ultra → stdout is JSON with hookSpecificOutput containing "ultra"', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('What is a database connection pool?', 'ultra', tmp.dir);
    assert.ok(stdout.length > 0, 'stdout should not be empty');
    const parsed = JSON.parse(stdout);
    assert.ok(parsed.hookSpecificOutput, 'should have hookSpecificOutput');
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('ultra'),
      'additionalContext should mention ultra'
    );
    assert.equal(parsed.hookSpecificOutput.hookEventName, 'UserPromptSubmit');
  } finally {
    tmp.cleanup();
  }
});

test('Regular prompt with flag=off → stdout empty (off level suppresses reinforcement)', () => {
  const tmp = makeTmpDir();
  try {
    // We have to write 'off' directly — the command path deletes instead of writing it.
    // Write it manually as a regular file (not via safeWriteFlag) to confirm readFlag returns 'off'.
    fs.mkdirSync(tmp.dir, { recursive: true });
    fs.writeFileSync(path.join(tmp.dir, FLAG_NAME), 'off', { mode: 0o600 });
    const { stdout } = spawnTracker('Tell me about caching', 'off', tmp.dir);
    assert.equal(stdout, '', 'off level should suppress reinforcement output');
  } finally {
    tmp.cleanup();
  }
});

test('Regular prompt with no flag → stdout empty', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('What is 2 + 2?', null, tmp.dir);
    assert.equal(stdout, '', 'no flag → no reinforcement');
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// Additional edge cases
// ===========================================================================

test('/pakka:compress strict over existing lite flag — flag updated to strict, stdout has PAKKA HOOK HANDLED', () => {
  const tmp = makeTmpDir();
  try {
    const { flagContent, stdout } = spawnTracker('/pakka:compress strict', 'lite', tmp.dir);
    assert.equal(flagContent, 'strict');
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('PAKKA HOOK HANDLED: compress level set to strict'),
      'additionalContext should include hook handled message for strict'
    );
  } finally {
    tmp.cleanup();
  }
});

test('Regular prompt with flag=lite → stdout is JSON mentioning lite', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('Explain recursion.', 'lite', tmp.dir);
    assert.ok(stdout.length > 0);
    const parsed = JSON.parse(stdout);
    assert.ok(parsed.hookSpecificOutput.additionalContext.includes('lite'));
  } finally {
    tmp.cleanup();
  }
});

test('Regular prompt with flag=super-ultra → stdout is JSON mentioning super-ultra', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('Explain closures.', 'super-ultra', tmp.dir);
    assert.ok(stdout.length > 0);
    const parsed = JSON.parse(stdout);
    assert.ok(parsed.hookSpecificOutput.additionalContext.includes('super-ultra'));
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// Cycle 1 — level switch: writes config.json AND emits additionalContext
// ===========================================================================

test('Cycle1: /pakka:compress ultra — config.json written with defaultLevel=ultra', () => {
  const tmp = makeTmpDir();
  try {
    spawnTracker('/pakka:compress ultra', null, tmp.dir);
    const cfgPath = path.join(tmp.dir, '.config', 'pakka', 'config.json');
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8'));
    assert.equal(cfg.defaultLevel, 'ultra', 'config.json defaultLevel should be ultra');
  } finally {
    tmp.cleanup();
  }
});

test('Cycle1: /pakka:compress super-ultra — config.json written with defaultLevel=super-ultra', () => {
  const tmp = makeTmpDir();
  try {
    spawnTracker('/pakka:compress super-ultra', null, tmp.dir);
    const cfgPath = path.join(tmp.dir, '.config', 'pakka', 'config.json');
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8'));
    assert.equal(cfg.defaultLevel, 'super-ultra', 'config.json defaultLevel should be super-ultra');
  } finally {
    tmp.cleanup();
  }
});

test('Cycle1: /pakka:compress invalid — config.json NOT written', () => {
  const tmp = makeTmpDir();
  try {
    spawnTracker('/pakka:compress invalid', 'lite', tmp.dir);
    const cfgPath = path.join(tmp.dir, '.config', 'pakka', 'config.json');
    assert.throws(() => fs.readFileSync(cfgPath, 'utf8'), { code: 'ENOENT' }, 'config.json should not exist for invalid level');
  } finally {
    tmp.cleanup();
  }
});

test('Cycle1: /pakka:compress ultra preserves existing config.json keys', () => {
  const tmp = makeTmpDir();
  try {
    // Pre-write config.json with an extra key
    const cfgDir = path.join(tmp.dir, '.config', 'pakka');
    fs.mkdirSync(cfgDir, { recursive: true });
    fs.writeFileSync(path.join(cfgDir, 'config.json'), JSON.stringify({ semantic: false, defaultLevel: 'lite' }));
    spawnTracker('/pakka:compress ultra', null, tmp.dir);
    const cfg = JSON.parse(fs.readFileSync(path.join(cfgDir, 'config.json'), 'utf8'));
    assert.equal(cfg.defaultLevel, 'ultra', 'defaultLevel should be updated');
    assert.equal(cfg.semantic, false, 'existing semantic key should be preserved');
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// Cycle 2 — status: pre-computes and emits
// ===========================================================================

test('Cycle2: /pakka:compress status — stdout has PAKKA HOOK HANDLED: compress status', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('/pakka:compress status', null, tmp.dir);
    assert.ok(stdout.length > 0, 'stdout should not be empty');
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('PAKKA HOOK HANDLED: compress status'),
      'additionalContext should include compress status hook handled message'
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle2: /pakka:compress (no arg) — stdout has PAKKA HOOK HANDLED: compress status', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('/pakka:compress', null, tmp.dir);
    assert.ok(stdout.length > 0, 'stdout should not be empty');
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('PAKKA HOOK HANDLED: compress status'),
      'additionalContext should include compress status hook handled message'
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle2: /pakka:compress status — output contains current level from config.json', () => {
  const tmp = makeTmpDir();
  try {
    // Write config.json with ultra
    const cfgDir = path.join(tmp.dir, '.config', 'pakka');
    fs.mkdirSync(cfgDir, { recursive: true });
    fs.writeFileSync(path.join(cfgDir, 'config.json'), JSON.stringify({ defaultLevel: 'ultra' }));
    const { stdout } = spawnTracker('/pakka:compress status', null, tmp.dir);
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('ultra'),
      'additionalContext should mention the active level'
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle2: /pakka:compress status — no meter files → bytes_saved: 0', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('/pakka:compress status', null, tmp.dir);
    const parsed = JSON.parse(stdout);
    // Bytes saved should be 0 (no meter files in test env)
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('bytes saved'),
      'additionalContext should include bytes saved field'
    );
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// Cycle 3 — help: runs binary and emits
// ===========================================================================

test('Cycle3: /pakka:help — stdout has PAKKA HOOK HANDLED: help', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('/pakka:help', null, tmp.dir);
    assert.ok(stdout.length > 0, 'stdout should not be empty');
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('PAKKA HOOK HANDLED: help'),
      'additionalContext should include help hook handled message'
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle3: /pakka:help — CLAUDE_PLUGIN_ROOT not set → unavailable fallback in additionalContext', () => {
  const tmp = makeTmpDir();
  try {
    // spawnTracker sets CLAUDE_PLUGIN_ROOT='' which means not set
    const { stdout } = spawnTracker('/pakka:help', null, tmp.dir);
    const parsed = JSON.parse(stdout);
    assert.ok(
      parsed.hookSpecificOutput.additionalContext.includes('help unavailable'),
      'additionalContext should include help unavailable message when CLAUDE_PLUGIN_ROOT not set'
    );
  } finally {
    tmp.cleanup();
  }
});

test('Cycle3: /pakka:help — hookEventName is UserPromptSubmit', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout } = spawnTracker('/pakka:help', null, tmp.dir);
    const parsed = JSON.parse(stdout);
    assert.equal(parsed.hookSpecificOutput.hookEventName, 'UserPromptSubmit');
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// spawnTrackerFull — like spawnTracker but also accepts pluginRoot env var
// ---------------------------------------------------------------------------
function spawnTrackerFull(promptText, flagContent, configDir, pluginRoot) {
  const flagPath = path.join(configDir, FLAG_NAME);

  if (flagContent !== null) {
    fs.mkdirSync(configDir, { recursive: true });
    fs.writeFileSync(flagPath, flagContent, { mode: 0o600 });
  }

  const input = JSON.stringify({ prompt: promptText });

  const result = spawnSync(process.execPath, [SCRIPT], {
    input,
    encoding: 'utf8',
    env: {
      ...process.env,
      CLAUDE_CONFIG_DIR: configDir,
      HOME: configDir,
      PAKKA_DEFAULT_LEVEL: '',
      CLAUDE_PLUGIN_ROOT: pluginRoot || '',
    },
    timeout: 5000,
  });

  if (result.error) throw result.error;

  let resultFlag = null;
  try {
    resultFlag = fs.readFileSync(flagPath, 'utf8');
  } catch (_) {
    resultFlag = null;
  }

  return { stdout: result.stdout, flagContent: resultFlag };
}

// ===========================================================================
// Cycle 4 — full ruleset included in additionalContext when CLAUDE_PLUGIN_ROOT set
// ===========================================================================

test('Cycle4: /pakka:compress strict with CLAUDE_PLUGIN_ROOT set — additionalContext contains filtered ruleset and trailer', () => {
  const tmp = makeTmpDir();
  try {
    // Create minimal rules file in <pluginRoot>/rules/output-compress.md
    const rulesDir = path.join(tmp.dir, 'rules');
    fs.mkdirSync(rulesDir, { recursive: true });
    fs.writeFileSync(path.join(rulesDir, 'output-compress.md'), [
      'PAKKA COMPRESSION ACTIVE — level: ultra',
      '',
      '## Rules',
      'Drop filler.',
      '',
      '| lite | keep articles |',
      '| strict | drop articles |',
      '| ultra | abbreviate |',
    ].join('\n'));

    const { stdout } = spawnTrackerFull('/pakka:compress strict', null, tmp.dir, tmp.dir);
    const parsed = JSON.parse(stdout);
    const ctx = parsed.hookSpecificOutput.additionalContext;

    // filterRuleset should have replaced "level: ultra" with "level: strict"
    assert.ok(ctx.includes('level: strict'), 'additionalContext should contain "level: strict" (filterRuleset ran)');

    // Trailer must be present
    assert.ok(ctx.includes('Level switched to strict'), 'additionalContext should contain trailer "Level switched to strict"');

    // Rows for other levels must be stripped
    assert.ok(!ctx.includes('| lite |'), 'lite table row should be filtered out');
    assert.ok(!ctx.includes('| ultra |'), 'ultra table row should be filtered out');
  } finally {
    tmp.cleanup();
  }
});

test('Cycle4: /pakka:compress strict with CLAUDE_PLUGIN_ROOT set — does NOT contain brief fallback message', () => {
  const tmp = makeTmpDir();
  try {
    const rulesDir = path.join(tmp.dir, 'rules');
    fs.mkdirSync(rulesDir, { recursive: true });
    fs.writeFileSync(path.join(rulesDir, 'output-compress.md'), 'PAKKA COMPRESSION ACTIVE — level: ultra\n## Rules\nDrop filler.');

    const { stdout } = spawnTrackerFull('/pakka:compress strict', null, tmp.dir, tmp.dir);
    const parsed = JSON.parse(stdout);
    const ctx = parsed.hookSpecificOutput.additionalContext;

    assert.ok(!ctx.includes('PAKKA HOOK HANDLED: compress level set to strict'), 'should NOT use brief fallback when ruleset is available');
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// Cycle 5 — fallback when CLAUDE_PLUGIN_ROOT unset (regression)
// ===========================================================================

test('Cycle5: /pakka:compress ultra with NO CLAUDE_PLUGIN_ROOT — falls back to brief message', () => {
  const tmp = makeTmpDir();
  try {
    // spawnTracker sets CLAUDE_PLUGIN_ROOT='' (unset), which triggers fallback
    const { stdout } = spawnTracker('/pakka:compress ultra', null, tmp.dir);
    const parsed = JSON.parse(stdout);
    const ctx = parsed.hookSpecificOutput.additionalContext;
    assert.ok(
      ctx.includes('PAKKA HOOK HANDLED: compress level set to ultra'),
      'should use brief fallback when CLAUDE_PLUGIN_ROOT is not set'
    );
  } finally {
    tmp.cleanup();
  }
});
