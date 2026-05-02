'use strict';

// SessionStart hook — injects output-compression rules into context.
// Writes the active level flag file, then emits filtered ruleset to stdout.

const fs = require('fs');
const path = require('path');

const { getDefaultLevel, safeWriteFlag, filterRuleset } = require('./compress-config');

// Resolve flag path: prefer $CLAUDE_CONFIG_DIR, fall back to ~/.claude
const claudeConfigDir =
  process.env.CLAUDE_CONFIG_DIR ||
  path.join(require('os').homedir(), '.claude');
const flagPath = path.join(claudeConfigDir, '.pakka-level');

const level = getDefaultLevel();

// If compression is off, exit cleanly — no rules injected
if (level === 'off') {
  process.stdout.write('OK');
  process.exit(0);
}

// Persist the active level for per-turn reinforcement
safeWriteFlag(flagPath, level);

// Load and filter ruleset
const rulesFile = path.join(__dirname, '..', 'rules', 'output-compress.md');

let content = null;
try {
  content = fs.readFileSync(rulesFile, 'utf8');
} catch (_) {
  content = null;
}

// Ambient behaviors appended to every non-off ruleset output.
// Skill-check discipline is injected separately via skill-check-start.js.
const ambientBehaviors =
  '\n## Verification discipline\n' +
  'Before outputting "done", "working", "fixed", "passing", or any completion claim:\n' +
  'run the relevant command and show the actual exit code. Exit 0 = evidence. "Should work" is not evidence.\n';

if (content !== null) {
  // filterRuleset: replaces header level marker and strips other-level rows/examples.
  // output-compress.md line 1 IS "PAKKA COMPRESSION ACTIVE — level: <level>"
  // (after the replace inside filterRuleset). Emit directly — no prepend.
  const filtered = filterRuleset(content, level);
  process.stdout.write(filtered + ambientBehaviors);
} else {
  // Fallback: hardcoded minimal ruleset
  const fallback =
    'PAKKA COMPRESSION ACTIVE — level: ' + level + '\n\n' +
    '## Rules\n' +
    'Drop: articles (a/an/the), filler (just/really/basically/actually/simply),\n' +
    'pleasantries (sure/certainly/of course/happy to), hedging (I think/maybe/perhaps).\n' +
    'Fragments OK. Short synonyms. Technical terms exact. Code blocks unchanged.\n' +
    'Pattern: [thing] [action] [reason]. [next step].\n\n' +
    '## Boundaries\n' +
    'Code/commits/PRs/error messages: write normal. Never compress code output.\n';
  process.stdout.write(fallback + ambientBehaviors);
}
