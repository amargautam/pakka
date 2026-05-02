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
      // Isolate from any real PAKKA_DEFAULT_LEVEL that might affect behaviour
      PAKKA_DEFAULT_LEVEL: '',
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

test('/pakka:compress ultra — flag written as ultra, stdout empty', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('/pakka:compress ultra', null, tmp.dir);
    assert.equal(flagContent, 'ultra', 'flag should be ultra');
    assert.equal(stdout, '', 'stdout should be empty (process.exit before reinforcement)');
  } finally {
    tmp.cleanup();
  }
});

test('/pakka:compress lite — flag written as lite', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('/pakka:compress lite', null, tmp.dir);
    assert.equal(flagContent, 'lite');
    assert.equal(stdout, '');
  } finally {
    tmp.cleanup();
  }
});

test('/pakka:compress super-ultra — flag written as super-ultra', () => {
  const tmp = makeTmpDir();
  try {
    const { stdout, flagContent } = spawnTracker('/pakka:compress super-ultra', null, tmp.dir);
    assert.equal(flagContent, 'super-ultra');
    assert.equal(stdout, '');
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

test('/pakka:compress invalid — flag unchanged, stdout empty', () => {
  const tmp = makeTmpDir();
  try {
    // Pre-seed with 'lite' so we can verify it's not changed
    const { stdout, flagContent } = spawnTracker('/pakka:compress invalid', 'lite', tmp.dir);
    assert.equal(flagContent, 'lite', 'flag should remain lite after invalid arg');
    assert.equal(stdout, '', 'stdout should be empty');
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

test('/pakka:compress strict over existing lite flag — flag updated to strict', () => {
  const tmp = makeTmpDir();
  try {
    const { flagContent, stdout } = spawnTracker('/pakka:compress strict', 'lite', tmp.dir);
    assert.equal(flagContent, 'strict');
    assert.equal(stdout, '');
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
