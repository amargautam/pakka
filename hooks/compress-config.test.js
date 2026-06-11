'use strict';

// Tests for compress-config.js — run with: node --test compress-config.test.js
// Uses Node 18+ built-in test runner; no external deps.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');

// ---------------------------------------------------------------------------
// Helper: create a temp directory and return its path + a cleanup fn
// ---------------------------------------------------------------------------
function makeTmpDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pakka-test-'));
  return {
    dir,
    cleanup() {
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

// ---------------------------------------------------------------------------
// Helper: save/restore env vars
// ---------------------------------------------------------------------------
function saveEnv(...keys) {
  const saved = {};
  for (const k of keys) saved[k] = process.env[k];
  return function restore() {
    for (const k of keys) {
      if (saved[k] === undefined) delete process.env[k];
      else process.env[k] = saved[k];
    }
  };
}

// ---------------------------------------------------------------------------
// Load the module under test — must require AFTER env is prepared per-test
// since getDefaultLevel() reads env at call time, not module load time.
// ---------------------------------------------------------------------------
const {
  VALID_LEVELS,
  getDefaultLevel,
  safeWriteFlag,
  readFlag,
  filterRuleset,
  getSemanticEnabled,
  stripQuotedSegments,
  isDirectiveUse,
} = require('./compress-config');

// ===========================================================================
// getSemanticEnabled
// ===========================================================================
test('getSemanticEnabled — ultra, undefined → true (on by default)', () => {
  assert.equal(getSemanticEnabled('ultra', undefined), true);
});

test('getSemanticEnabled — ultra, false → false (user opt-out respected)', () => {
  assert.equal(getSemanticEnabled('ultra', false), false);
});

test('getSemanticEnabled — super-ultra, undefined → true', () => {
  assert.equal(getSemanticEnabled('super-ultra', undefined), true);
});

test('getSemanticEnabled — super-ultra, false → true (enforced, cannot be disabled)', () => {
  assert.equal(getSemanticEnabled('super-ultra', false), true);
});

test('getSemanticEnabled — lite, undefined → false', () => {
  assert.equal(getSemanticEnabled('lite', undefined), false);
});

test('getSemanticEnabled — lite, true → true (explicit opt-in respected)', () => {
  assert.equal(getSemanticEnabled('lite', true), true);
});

// ===========================================================================
// getDefaultLevel
// ===========================================================================
test('getDefaultLevel — no env/config/settings → super-ultra', () => {
  const tmp = makeTmpDir();
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL', 'CLAUDE_PLUGIN_ROOT', 'HOME');
  try {
    delete process.env.PAKKA_DEFAULT_LEVEL;
    delete process.env.CLAUDE_PLUGIN_ROOT;
    // Point HOME at empty tmp dir so ~/.config/pakka/config.json doesn't exist
    process.env.HOME = tmp.dir;
    assert.equal(getDefaultLevel(), 'super-ultra');
  } finally {
    restore();
    tmp.cleanup();
  }
});

test('getDefaultLevel — no env/config/settings → super-ultra (new default)', () => {
  const tmp = makeTmpDir();
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL', 'CLAUDE_PLUGIN_ROOT', 'HOME');
  try {
    delete process.env.PAKKA_DEFAULT_LEVEL;
    delete process.env.CLAUDE_PLUGIN_ROOT;
    process.env.HOME = tmp.dir;
    assert.equal(getDefaultLevel(), 'super-ultra');
  } finally {
    restore();
    tmp.cleanup();
  }
});

test('getDefaultLevel — PAKKA_DEFAULT_LEVEL=lite → lite', () => {
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL');
  try {
    process.env.PAKKA_DEFAULT_LEVEL = 'lite';
    assert.equal(getDefaultLevel(), 'lite');
  } finally {
    restore();
  }
});

test('getDefaultLevel — PAKKA_DEFAULT_LEVEL=invalid → falls through to super-ultra', () => {
  const tmp = makeTmpDir();
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL', 'CLAUDE_PLUGIN_ROOT', 'HOME');
  try {
    process.env.PAKKA_DEFAULT_LEVEL = 'invalid';
    delete process.env.CLAUDE_PLUGIN_ROOT;
    process.env.HOME = tmp.dir;
    assert.equal(getDefaultLevel(), 'super-ultra');
  } finally {
    restore();
    tmp.cleanup();
  }
});

test('getDefaultLevel — config.json defaultLevel:strict → strict', () => {
  const tmp = makeTmpDir();
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL', 'CLAUDE_PLUGIN_ROOT', 'HOME');
  try {
    delete process.env.PAKKA_DEFAULT_LEVEL;
    delete process.env.CLAUDE_PLUGIN_ROOT;
    process.env.HOME = tmp.dir;
    // Write ~/.config/pakka/config.json
    const cfgDir = path.join(tmp.dir, '.config', 'pakka');
    fs.mkdirSync(cfgDir, { recursive: true });
    fs.writeFileSync(path.join(cfgDir, 'config.json'), JSON.stringify({ defaultLevel: 'strict' }));
    assert.equal(getDefaultLevel(), 'strict');
  } finally {
    restore();
    tmp.cleanup();
  }
});

test('getDefaultLevel — config.json with invalid defaultLevel → falls through to super-ultra', () => {
  const tmp = makeTmpDir();
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL', 'CLAUDE_PLUGIN_ROOT', 'HOME');
  try {
    delete process.env.PAKKA_DEFAULT_LEVEL;
    delete process.env.CLAUDE_PLUGIN_ROOT;
    process.env.HOME = tmp.dir;
    const cfgDir = path.join(tmp.dir, '.config', 'pakka');
    fs.mkdirSync(cfgDir, { recursive: true });
    fs.writeFileSync(path.join(cfgDir, 'config.json'), JSON.stringify({ defaultLevel: 'bogus' }));
    assert.equal(getDefaultLevel(), 'super-ultra');
  } finally {
    restore();
    tmp.cleanup();
  }
});

test('getDefaultLevel — CLAUDE_PLUGIN_ROOT settings.json outputLevel:lite → lite', () => {
  const tmp = makeTmpDir();
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL', 'CLAUDE_PLUGIN_ROOT', 'HOME');
  try {
    delete process.env.PAKKA_DEFAULT_LEVEL;
    process.env.HOME = tmp.dir; // no ~/.config/pakka/config.json here
    process.env.CLAUDE_PLUGIN_ROOT = tmp.dir;
    const settings = { pakka: { compress: { outputLevel: 'lite' } } };
    fs.writeFileSync(path.join(tmp.dir, 'settings.json'), JSON.stringify(settings));
    assert.equal(getDefaultLevel(), 'lite');
  } finally {
    restore();
    tmp.cleanup();
  }
});

test('getDefaultLevel — settings.json outputLevel invalid → falls through to super-ultra', () => {
  const tmp = makeTmpDir();
  const restore = saveEnv('PAKKA_DEFAULT_LEVEL', 'CLAUDE_PLUGIN_ROOT', 'HOME');
  try {
    delete process.env.PAKKA_DEFAULT_LEVEL;
    process.env.HOME = tmp.dir;
    process.env.CLAUDE_PLUGIN_ROOT = tmp.dir;
    const settings = { pakka: { compress: { outputLevel: 'nope' } } };
    fs.writeFileSync(path.join(tmp.dir, 'settings.json'), JSON.stringify(settings));
    assert.equal(getDefaultLevel(), 'super-ultra');
  } finally {
    restore();
    tmp.cleanup();
  }
});

// ===========================================================================
// safeWriteFlag
// ===========================================================================
test('safeWriteFlag — writes file with correct content', () => {
  const tmp = makeTmpDir();
  try {
    const flagPath = path.join(tmp.dir, '.pakka-level');
    safeWriteFlag(flagPath, 'ultra');
    assert.equal(fs.readFileSync(flagPath, 'utf8'), 'ultra');
  } finally {
    tmp.cleanup();
  }
});

test('safeWriteFlag — written file has mode 0600', () => {
  const tmp = makeTmpDir();
  try {
    const flagPath = path.join(tmp.dir, '.pakka-level');
    safeWriteFlag(flagPath, 'strict');
    const st = fs.statSync(flagPath);
    // Lower 9 permission bits: 0o600 = owner rw, group none, other none
    assert.equal(st.mode & 0o777, 0o600);
  } finally {
    tmp.cleanup();
  }
});

test('safeWriteFlag — overwrites existing file', () => {
  const tmp = makeTmpDir();
  try {
    const flagPath = path.join(tmp.dir, '.pakka-level');
    safeWriteFlag(flagPath, 'lite');
    safeWriteFlag(flagPath, 'strict');
    assert.equal(fs.readFileSync(flagPath, 'utf8'), 'strict');
  } finally {
    tmp.cleanup();
  }
});

test('safeWriteFlag — refuses to write if flagPath is a symlink', () => {
  const tmp = makeTmpDir();
  try {
    const target = path.join(tmp.dir, 'real-file');
    const link = path.join(tmp.dir, '.pakka-level');
    fs.writeFileSync(target, 'original');
    fs.symlinkSync(target, link);
    safeWriteFlag(link, 'ultra');
    // The target must remain unchanged; safeWriteFlag should not follow the link
    assert.equal(fs.readFileSync(target, 'utf8'), 'original');
    // The symlink itself must still point at the target (not be replaced by a regular file)
    assert.ok(fs.lstatSync(link).isSymbolicLink(), 'link must still be a symlink');
  } finally {
    tmp.cleanup();
  }
});

test('safeWriteFlag — silent-fail if parent dir cannot be created (/nonexistent path)', () => {
  const flagPath = '/nonexistent/deep/nested/.pakka-level';
  // Must not throw
  assert.doesNotThrow(() => safeWriteFlag(flagPath, 'ultra'));
  // No file should have been created
  assert.throws(() => fs.statSync(flagPath), { code: 'ENOENT' });
});

// ===========================================================================
// readFlag
// ===========================================================================
test('readFlag — returns valid level from file containing ultra', () => {
  const tmp = makeTmpDir();
  try {
    const flagPath = path.join(tmp.dir, '.pakka-level');
    fs.writeFileSync(flagPath, 'ultra', { mode: 0o600 });
    assert.equal(readFlag(flagPath), 'ultra');
  } finally {
    tmp.cleanup();
  }
});

test('readFlag — returns super-ultra (hyphenated level)', () => {
  const tmp = makeTmpDir();
  try {
    const flagPath = path.join(tmp.dir, '.pakka-level');
    fs.writeFileSync(flagPath, 'super-ultra', { mode: 0o600 });
    assert.equal(readFlag(flagPath), 'super-ultra');
  } finally {
    tmp.cleanup();
  }
});

test('readFlag — returns null for non-existent file', () => {
  const tmp = makeTmpDir();
  try {
    assert.equal(readFlag(path.join(tmp.dir, 'no-such-file')), null);
  } finally {
    tmp.cleanup();
  }
});

test('readFlag — returns null for content > 64 bytes', () => {
  const tmp = makeTmpDir();
  try {
    const flagPath = path.join(tmp.dir, '.pakka-level');
    // Write 65 bytes of valid-looking but too-long content
    fs.writeFileSync(flagPath, 'u'.repeat(65), { mode: 0o600 });
    assert.equal(readFlag(flagPath), null);
  } finally {
    tmp.cleanup();
  }
});

test('readFlag — returns null for invalid level string', () => {
  const tmp = makeTmpDir();
  try {
    const flagPath = path.join(tmp.dir, '.pakka-level');
    fs.writeFileSync(flagPath, 'invalid', { mode: 0o600 });
    assert.equal(readFlag(flagPath), null);
  } finally {
    tmp.cleanup();
  }
});

test('readFlag — returns null if flagPath is a symlink pointing to valid file', () => {
  const tmp = makeTmpDir();
  try {
    const target = path.join(tmp.dir, 'real-flag');
    const link = path.join(tmp.dir, '.pakka-level');
    fs.writeFileSync(target, 'ultra', { mode: 0o600 });
    fs.symlinkSync(target, link);
    // O_NOFOLLOW should cause open to fail → null
    assert.equal(readFlag(link), null);
  } finally {
    tmp.cleanup();
  }
});

// ===========================================================================
// filterRuleset
// ===========================================================================

// We need some realistic content to test against.
// Use a minimal representative subset of output-compress.md structure.
const SAMPLE_CONTENT = `PAKKA COMPRESSION ACTIVE — level: ultra

## Persistence
Always active.

## Intensity
| Level | Rules |
|-------|-------|
| lite | Lite rules. |
| strict | Strict rules. |
| ultra | Ultra rules. |
| super-ultra | Super-ultra rules. |

## Examples

Question — "What?"
- lite: "Lite answer."
- strict: "Strict answer."
- ultra: "Ultra answer."
- super-ultra: "Super-ultra answer."

## Boundaries
Code unchanged.
`;

const LEVELS = ['lite', 'strict', 'ultra', 'super-ultra'];

for (const level of LEVELS) {
  test(`filterRuleset — level=${level}: own table row kept, others stripped`, () => {
    const out = filterRuleset(SAMPLE_CONTENT, level);
    const lines = out.split('\n');
    const tableRowRe = /^\|\s*(lite|strict|ultra|super-ultra)\s*\|/;
    for (const line of lines) {
      const m = tableRowRe.exec(line);
      if (m !== null) {
        assert.equal(m[1], level, `Table row for ${m[1]} should have been stripped`);
      }
    }
    // The active level's row must be present
    const hasOwn = lines.some(l => {
      const m = tableRowRe.exec(l);
      return m !== null && m[1] === level;
    });
    assert.ok(hasOwn, `Table row for ${level} should be present`);
  });

  test(`filterRuleset — level=${level}: own example line kept, others stripped`, () => {
    const out = filterRuleset(SAMPLE_CONTENT, level);
    const lines = out.split('\n');
    const exampleLineRe = /^- (lite|strict|ultra|super-ultra): /;
    for (const line of lines) {
      const m = exampleLineRe.exec(line);
      if (m !== null) {
        assert.equal(m[1], level, `Example line for ${m[1]} should have been stripped`);
      }
    }
    const hasOwn = lines.some(l => {
      const m = exampleLineRe.exec(l);
      return m !== null && m[1] === level;
    });
    assert.ok(hasOwn, `Example line for ${level} should be present`);
  });
}

test('filterRuleset — header/separator/prose lines always kept', () => {
  const out = filterRuleset(SAMPLE_CONTENT, 'strict');
  assert.ok(out.includes('## Persistence'), 'prose heading missing');
  assert.ok(out.includes('Always active.'), 'prose line missing');
  assert.ok(out.includes('|-------|-------|'), 'table separator missing');
  assert.ok(out.includes('## Boundaries'), 'boundary heading missing');
  assert.ok(out.includes('Code unchanged.'), 'boundary prose missing');
});

test('filterRuleset — level substitution in header', () => {
  for (const level of LEVELS) {
    const out = filterRuleset(SAMPLE_CONTENT, level);
    assert.ok(
      out.startsWith('PAKKA COMPRESSION ACTIVE — level: ' + level),
      `Header not substituted for level=${level}`
    );
  }
});

test('filterRuleset — header substitution replaces first occurrence only', () => {
  // Add a second "level: ultra" elsewhere in the content
  const content = 'PAKKA COMPRESSION ACTIVE — level: ultra\nSome note: level: ultra still here.\n';
  const out = filterRuleset(content, 'lite');
  const lines = out.split('\n');
  assert.equal(lines[0], 'PAKKA COMPRESSION ACTIVE — level: lite');
  assert.equal(lines[1], 'Some note: level: ultra still here.');
});

test('filterRuleset — empty string input → returns empty string', () => {
  assert.doesNotThrow(() => {
    const out = filterRuleset('', 'ultra');
    assert.equal(out, '');
  });
});

// ===========================================================================
// stripQuotedSegments (issue #11)
// ===========================================================================

test('stripQuotedSegments — removes fenced blocks, backticks, double quotes', () => {
  assert.ok(!stripQuotedSegments('see ```fix the gate``` above').includes('fix'));
  assert.ok(!stripQuotedSegments('the `fix` keyword').includes('fix'));
  assert.ok(!stripQuotedSegments('docs say "fix the gate" here').includes('fix'));
});

test('stripQuotedSegments — keeps unquoted text intact', () => {
  const out = stripQuotedSegments('fix the gate, see "the error" log');
  assert.ok(out.includes('fix the gate'));
  assert.ok(!out.includes('the error'));
});

// ===========================================================================
// isDirectiveUse (issue #11) — table-driven: result varies with phrasing
// ===========================================================================

const DIRECTIVE_TABLE = [
  // [text (lowercased, pre-stripped), keyword, expected]
  ['fix the gate so the meter resets', 'fix', true],
  ['the gate is broken when the flag is missing', 'broken', false],
  ['debug why the hook exits early', 'debug', true],
  ["there's an error message saying enoent", 'error', false],
  ['error: enoent: no such file or directory', 'error', false],
  ['the fix is coming in the next release', 'fix', false],
  ['did you fix the parser yesterday', 'fix', false],
  ['can you fix the status line', 'fix', true],
  ['i want you to fix the meter', 'fix', true],
  // strong lead-in with keyword as final token — no object word required
  ['please fix', 'fix', true],
  ["let's review", 'review', true],
  // newline acts as clause boundary after pasted context
  ['here is some pasted context\nfix the gate now', 'fix', true],
  // bare keyword with no directive shape
  ['coupling', 'coupling', false],
];

for (const [text, keyword, expected] of DIRECTIVE_TABLE) {
  test('isDirectiveUse("' + text.replace(/\n/g, '\\n') + '", "' + keyword + '") → ' + expected, () => {
    assert.equal(isDirectiveUse(text, keyword), expected);
  });
}

test('isDirectiveUse — adversarial repeated-keyword input stays linear (no quadratic prefix re-scan)', () => {
  // ~700KB of repeated non-directive keyword occurrences. Pre-fix this took
  // tens of seconds (full prefix slice+match per occurrence); bounded windows
  // must keep it well under a second.
  const huge = 'the broken '.repeat(64000);
  const start = Date.now();
  const result = isDirectiveUse(huge, 'broken');
  const elapsed = Date.now() - start;
  assert.equal(result, false, 'repeated report-position keyword must not become a directive');
  assert.ok(elapsed < 2000, 'scan must stay linear; took ' + elapsed + 'ms');
});

test('isDirectiveUse — directive found late in long text still triggers', () => {
  const text = 'context line. '.repeat(2000) + 'fix the gate now';
  assert.equal(isDirectiveUse(text, 'fix'), true);
});
