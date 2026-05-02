'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');

const VALID_LEVELS = ['off', 'lite', 'strict', 'ultra', 'super-ultra'];

/**
 * getDefaultLevel — resolution order:
 *   1. PAKKA_DEFAULT_LEVEL env var
 *   2. ~/.config/pakka/config.json defaultLevel field
 *   3. ${CLAUDE_PLUGIN_ROOT}/settings.json pakka.compress.outputLevel
 *      (written by /pakka:compress skill — keeps JS hooks in sync with Go binary)
 *   4. 'ultra'
 *
 * Invalid values (not in VALID_LEVELS) fall through to next source.
 */
function getDefaultLevel() {
  // 1. Env var (highest priority — explicit user override)
  const envVal = process.env.PAKKA_DEFAULT_LEVEL;
  if (envVal && VALID_LEVELS.includes(envVal)) {
    return envVal;
  }

  // 2. Pakka config file
  try {
    const cfgPath = path.join(os.homedir(), '.config', 'pakka', 'config.json');
    const raw = fs.readFileSync(cfgPath, 'utf8');
    const cfg = JSON.parse(raw);
    if (cfg && VALID_LEVELS.includes(cfg.defaultLevel)) {
      return cfg.defaultLevel;
    }
  } catch (_) {
    // missing or malformed — fall through
  }

  // 3. Plugin settings.json (written by /pakka:compress skill)
  try {
    const pluginRoot = process.env.CLAUDE_PLUGIN_ROOT;
    if (pluginRoot) {
      const settingsPath = path.join(pluginRoot, 'settings.json');
      const raw = fs.readFileSync(settingsPath, 'utf8');
      const s = JSON.parse(raw);
      const lvl = s && s.pakka && s.pakka.compress && s.pakka.compress.outputLevel;
      if (lvl && VALID_LEVELS.includes(lvl)) {
        return lvl;
      }
    }
  } catch (_) {
    // missing or malformed — fall through
  }

  // 4. Brand default
  return 'super-ultra';
}

// safeWriteFlag writes content to flagPath atomically with 0600 permissions.
// Refuses if flagPath is already a symlink. Silent-fails on all errors.
function safeWriteFlag(flagPath, content) {
  try {
    try {
      if (fs.lstatSync(flagPath).isSymbolicLink()) return;
    } catch (_) { /* doesn't exist yet — fine */ }

    fs.mkdirSync(path.dirname(flagPath), { recursive: true });

    const tmp = flagPath + '.' + process.pid + '.tmp';
    fs.writeFileSync(tmp, String(content), { mode: 0o600 });
    try {
      fs.renameSync(tmp, flagPath);
    } catch (e) {
      try { fs.unlinkSync(tmp); } catch (_) {}
      throw e;
    }
  } catch (_) { /* silent-fail */ }
}

// readFlag reads the flag file. Returns the level string or null if the file
// is missing, is a symlink, exceeds 64 bytes, or contains an unknown value.
function readFlag(flagPath) {
  try {
    const lst = fs.lstatSync(flagPath);
    if (lst.isSymbolicLink() || !lst.isFile()) return null;
    if (lst.size > 64) return null;
    const val = fs.readFileSync(flagPath, 'utf8').trim().toLowerCase();
    return VALID_LEVELS.includes(val) ? val : null;
  } catch (_) {
    return null;
  }
}

/**
 * filterRuleset — filters output-compress.md content to the active level.
 *
 * Replaces the header level marker ("level: ultra") with the active level
 * (first occurrence only), then strips table rows and example lines that
 * belong to other levels. All other lines are kept unchanged.
 */
function filterRuleset(content, level) {
  // Replace "level: ultra" in header with active level (first occurrence only)
  let out = content.replace('level: ultra', 'level: ' + level);

  const tableRowRe = /^\|\s*(lite|strict|ultra|super-ultra)\s*\|/;
  const exampleLineRe = /^- (lite|strict|ultra|super-ultra): /;

  return out
    .split('\n')
    .filter(line => {
      const trMatch = tableRowRe.exec(line);
      if (trMatch !== null) return trMatch[1] === level;
      const exMatch = exampleLineRe.exec(line);
      if (exMatch !== null) return exMatch[1] === level;
      return true;
    })
    .join('\n');
}

// getSemanticEnabled returns whether semantic compression is enabled.
// 'super-ultra' always enforces it; 'ultra' defaults on but respects opt-out;
// other levels default off but respect explicit opt-in.
function getSemanticEnabled(level, explicitSetting) {
  if (level === 'super-ultra') return true;
  if (explicitSetting === true) return true;
  if (level === 'ultra' && explicitSetting === undefined) return true;
  return false;
}

module.exports = { VALID_LEVELS, getDefaultLevel, safeWriteFlag, readFlag, filterRuleset, getSemanticEnabled };
