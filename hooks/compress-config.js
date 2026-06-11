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

// --- Skill-check intent-context filter (issue #11) ---
// A word keyword only triggers in directive position (imperative verb
// targeting implementation), not on bare presence inside reports, quoted
// text, or pasted error messages. Deterministic, no LLM. Ambiguous → no
// trigger (over-triggering is the reported bug).

// Keywords that can act as imperative verbs. Noun/adjective keywords
// (broken, error, architecture, feedback, ...) only trigger via a strong
// lead-in ("please X", "let's X").
const VERB_KEYWORDS = new Set([
  'fix', 'debug', 'implement', 'add', 'refactor', 'test',
  'design', 'spec', 'plan', 'probe', 'decompose', 'slice', 'challenge', 'structure',
  'verify', 'review', 'approve', 'ship', 'receive', 'finalize',
]);
// Lead-ins that mark a directive regardless of verb-ness ("let's tdd this").
const STRONG_LEADINS = new Set(['please', 'lets', 'kindly']);
// Weak connectives — directive only when the keyword is verb-capable.
const WEAK_LEADINS = new Set(['and', 'then', 'also', 'now', 'just', 'first', 'go']);
// Next-word shapes that mark noun usage ("the fix is coming") — not a directive.
const NOUN_FOLLOWERS = new Set([
  'is', 'are', 'was', 'were', 'isn', 'aren', 'wasn', 'weren',
  'seems', 'looks', 'appears', 'has', 'have', 'had',
  'will', 'would', 'should', 'could', 'can', 'may', 'might', 'must', 'won',
  'does', 'did', 'doesn', 'didn', 'broke', 'breaks', 'failed', 'fails', 'works', 'worked',
]);

// stripQuotedSegments removes fenced code blocks, backticked spans, and
// double-quoted segments — keywords inside them are quoted material, never
// directives.
function stripQuotedSegments(text) {
  return text
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`[^`\n]*`/g, ' ')
    .replace(/"[^"\n]*"/g, ' ')
    .replace(/“[^”\n]*”/g, ' ');
}

// isDirectiveUse returns true iff any occurrence of `keyword` in `text`
// (lowercased, quoted segments already stripped) is in directive position:
//   - preceded by a strong lead-in ("please", "let's", "can/could/would/will
//     you", "you to") — no object word required; or
//   - clause start (prompt start or after . ! ? ; : , newline, em/en dash)
//     for verb-capable keywords, or after a weak connective ("and", "then"),
//     with a following object word that is not a copula/aux.
function isDirectiveUse(text, keyword) {
  const re = new RegExp('\\b' + keyword + '\\b', 'g');
  let m;
  while ((m = re.exec(text)) !== null) {
    // Bounded context windows: only the immediately surrounding words matter,
    // so never slice/scan the whole prefix per occurrence — keeps the scan
    // linear on adversarially long prompts (hook runs on every prompt).
    const windowStart = Math.max(0, m.index - 80);
    const before = text.slice(windowStart, m.index);
    const words = before.match(/[a-z'’]+/g) || [];
    const prev = words.length ? words[words.length - 1].replace(/['’]/g, '') : null;
    const prev2 = words.length > 1 ? words[words.length - 2].replace(/['’]/g, '') : null;

    // Strong lead-ins are unambiguous directives even with no object word
    // ("please fix", "can you review").
    if (prev && STRONG_LEADINS.has(prev)) return true;
    if (prev === 'you' && (prev2 === 'can' || prev2 === 'could' || prev2 === 'would' || prev2 === 'will')) return true;
    if (prev === 'to' && prev2 === 'you') return true;

    // Remaining paths require verb-object shape: a word must follow, and not
    // a copula/aux ("the fix is coming").
    const afterStart = m.index + keyword.length;
    const after = text.slice(afterStart, afterStart + 40);
    const nextMatch = /^\s*([a-z'’]+)/.exec(after);
    if (!nextMatch) continue;
    const next = nextMatch[1].replace(/['’]/g, '');
    if (NOUN_FOLLOWERS.has(next)) continue;

    // Clause start: prompt start, or boundary punctuation (incl. newline)
    // followed only by whitespace. Tested on untrimmed text so newlines count.
    // '^' only counts as prompt start when the window is not truncated.
    const clauseStart = windowStart === 0
      ? /(^|[.!?;:,\n—–])\s*$/.test(before)
      : /[.!?;:,\n—–]\s*$/.test(before);
    if (clauseStart && VERB_KEYWORDS.has(keyword)) return true;
    if (prev && WEAK_LEADINS.has(prev) && VERB_KEYWORDS.has(keyword)) return true;
  }
  return false;
}

module.exports = { VALID_LEVELS, getDefaultLevel, safeWriteFlag, readFlag, filterRuleset, getSemanticEnabled, stripQuotedSegments, isDirectiveUse };
