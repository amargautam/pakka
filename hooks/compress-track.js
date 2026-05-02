'use strict';

// UserPromptSubmit hook — per-turn compression reinforcement.
// Also handles /pakka:compress <level>, /pakka:compress status, and /pakka:help commands.

const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawnSync } = require('child_process');

try {
  const { VALID_LEVELS, safeWriteFlag, readFlag, getSemanticEnabled, filterRuleset } = require('./compress-config');

  // Parse hook event from stdin
  let raw = '';
  try {
    raw = fs.readFileSync('/dev/stdin', 'utf8');
  } catch (_) {
    raw = '';
  }

  let prompt = '';
  try {
    const event = JSON.parse(raw);
    prompt = (event.prompt || event.message || event.userMessage || '').trim();
  } catch (_) {
    prompt = raw.trim();
  }

  const promptLower = prompt.toLowerCase();

  // Flag path: prefer $CLAUDE_CONFIG_DIR, fall back to ~/.claude
  const claudeConfigDir =
    process.env.CLAUDE_CONFIG_DIR ||
    path.join(os.homedir(), '.claude');
  const flagPath = path.join(claudeConfigDir, '.pakka-level');

  // Config.json path: ~/.config/pakka/config.json (HOME respects env var)
  const configJsonPath = path.join(os.homedir(), '.config', 'pakka', 'config.json');

  // Helper: read config.json, merge key, write back
  function writeConfigJson(key, value) {
    let cfg = {};
    try {
      cfg = JSON.parse(fs.readFileSync(configJsonPath, 'utf8'));
    } catch (_) { /* file missing or malformed — start fresh */ }
    cfg[key] = value;
    fs.mkdirSync(path.dirname(configJsonPath), { recursive: true });
    fs.writeFileSync(configJsonPath, JSON.stringify(cfg), { mode: 0o600 });
  }

  // Helper: read config.json defaultLevel (or fallback)
  function readConfigLevel() {
    try {
      const cfg = JSON.parse(fs.readFileSync(configJsonPath, 'utf8'));
      if (cfg && VALID_LEVELS.includes(cfg.defaultLevel)) return cfg.defaultLevel;
    } catch (_) { /* missing or malformed */ }
    return 'super-ultra';
  }

  // Helper: read meter totals from ~/.pakka/meter/*.jsonl
  function readMeterTotals() {
    let bytesSaved = 0;
    let tokensSaved = 0;
    try {
      const meterDir = path.join(os.homedir(), '.pakka', 'meter');
      const files = fs.readdirSync(meterDir).filter(f => f.endsWith('.jsonl'));
      for (const file of files) {
        try {
          const lines = fs.readFileSync(path.join(meterDir, file), 'utf8').split('\n');
          for (const line of lines) {
            if (!line.trim()) continue;
            try {
              const entry = JSON.parse(line);
              bytesSaved += (entry.bytes_saved || 0);
              tokensSaved += (entry.tokens_saved_est || 0);
            } catch (_) { /* skip malformed lines */ }
          }
        } catch (_) { /* skip unreadable files */ }
      }
    } catch (_) { /* no meter dir — 0/0 */ }
    return { bytesSaved, tokensSaved };
  }

  // Helper: load and filter the ruleset for a given level.
  // Returns filtered content string, or null if CLAUDE_PLUGIN_ROOT is unset
  // or the rules file is unreadable.
  function loadFilteredRuleset(level) {
    const pluginRoot = process.env.CLAUDE_PLUGIN_ROOT;
    if (!pluginRoot) return null;
    try {
      const rulesPath = path.join(pluginRoot, 'rules', 'output-compress.md');
      const content = fs.readFileSync(rulesPath, 'utf8');
      return filterRuleset(content, level);
    } catch (_) { return null; }
  }

  // Helper: emit hookSpecificOutput with additionalContext and exit
  function emitAndExit(context) {
    const out = {
      hookSpecificOutput: {
        hookEventName: 'UserPromptSubmit',
        additionalContext: context,
      },
    };
    process.stdout.write(JSON.stringify(out));
    process.exit(0);
  }

  // --- Command: /pakka:help ---
  if (/^\/pakka:help\s*$/i.test(prompt)) {
    const pluginRoot = process.env.CLAUDE_PLUGIN_ROOT;
    let helpOutput;
    if (!pluginRoot) {
      helpOutput = '(help unavailable — CLAUDE_PLUGIN_ROOT not set)';
    } else {
      const binPath = path.join(pluginRoot, 'bin', 'run');
      const result = spawnSync(binPath, ['help'], { encoding: 'utf8', timeout: 5000 });
      helpOutput = (result.stdout || '') + (result.stderr ? '\n' + result.stderr : '');
      if (result.error) {
        helpOutput = '(help unavailable — ' + result.error.message + ')';
      }
    }
    emitAndExit('PAKKA HOOK HANDLED: help\n' + helpOutput + '\n\nOutput this verbatim, no tool calls needed.');
  }

  // --- Command: /pakka:compress (no arg) or /pakka:compress status ---
  // Must be checked BEFORE the level-switch block so "status" isn't caught as an invalid level.
  if (/^\/pakka(?::compress| compress)(?:\s+status)?\s*$/i.test(prompt)) {
    const level = readConfigLevel();
    const { bytesSaved, tokensSaved } = readMeterTotals();
    const activeLevel = readFlag(flagPath) || level;
    const semanticOn = getSemanticEnabled(activeLevel, undefined);
    const statusText =
      'PAKKA HOOK HANDLED: compress status\n' +
      'Output level: ' + level + '\n' +
      'Semantic: ' + (semanticOn ? 'on' : 'off') + '\n' +
      'Input bytes saved: ' + bytesSaved + '\n' +
      'Tokens saved (est.): ' + tokensSaved + '\n\n' +
      'Output this table verbatim, no tool calls needed.';
    emitAndExit(statusText);
  }

  // --- Command: /pakka:compress <level> or /pakka compress <level> ---
  // Matches both slash variants: /pakka:compress and /pakka compress
  const compressCmd = /^\/pakka(?::compress| compress)\s+(\S+)/i.exec(prompt);
  if (compressCmd) {
    const arg = compressCmd[1].toLowerCase();
    if (arg === 'off') {
      // Turn off compression: remove the flag file, write config.json
      try { fs.unlinkSync(flagPath); } catch (_) { /* silent */ }
      try { writeConfigJson('defaultLevel', 'off'); } catch (_) { /* silent */ }
      emitAndExit('PAKKA HOOK HANDLED: compress off. Output compression disabled. Output to user exactly: "Output compression turned off." — no tool calls needed.');
    } else if (VALID_LEVELS.includes(arg)) {
      // Set the new level: write config.json AND flag file
      try { writeConfigJson('defaultLevel', arg); } catch (_) { /* silent */ }
      safeWriteFlag(flagPath, arg);
      const ruleset = loadFilteredRuleset(arg);
      const trailer = '\n\nLevel switched to ' + arg + '. Config and flag updated. Output to user exactly: "Output compression set to ' + arg + '. Active now." — no tool calls needed.';
      emitAndExit(
        ruleset
          ? ruleset + trailer
          : 'PAKKA HOOK HANDLED: compress level set to ' + arg + '. Config written to ~/.config/pakka/config.json and flag file updated. Output to user exactly: "Output compression set to ' + arg + '. Active now." — no tool calls needed.'
      );
    }
    // Invalid level — fall through, exit without reinforcement or additionalContext
    process.exit(0);
  }

  // --- Prose commands: "pakka verbose" or "normal mode" ---
  if (/\b(pakka\s+verbose|normal\s+mode)\b/i.test(prompt)) {
    try { fs.unlinkSync(flagPath); } catch (_) { /* silent */ }
    // Fall through to read the flag (which is now gone, so no reinforcement)
  }

  // --- Keyword-based skill-check detection ---
  // Skip scan for /pakka: commands (already handled or self-referential)
  const isSlashPakka = /^\/pakka:/i.test(prompt);

  // Skill groups in priority order: BUILD > PLAN > REVIEW
  const SKILL_KEYWORDS = [
    {
      skill: '/pakka:build',
      words: ['fix', 'debug', 'implement', 'add', 'refactor', 'tdd', 'test', 'broken', 'error', 'coupling'],
      phrases: ['not working', 'build this', 'write the code', 'make it work', 'how does', 'walk me through', 'explain this', 'hard to test'],
    },
    {
      skill: '/pakka:plan',
      words: ['design', 'spec', 'plan', 'approach', 'architecture', 'structure', 'proposal', 'challenge', 'probe', 'decompose', 'slice', 'tickets'],
      phrases: ['how should we', 'what should we', 'should we', "let's build", 'we need to', 'thinking about', 'considering', 'what about', 'how about'],
    },
    {
      skill: '/pakka:review',
      words: ['verify', 'review', 'done', 'ship', 'approve', 'receive', 'feedback', 'finalize'],
      phrases: ['is this right', 'looks good', 'ready to', 'sign off'],
    },
  ];

  let skillMatch = null;
  if (!isSlashPakka) {
    outer: for (const group of SKILL_KEYWORDS) {
      for (const w of group.words) {
        if (new RegExp('\\b' + w + '\\b').test(promptLower)) {
          skillMatch = { skill: group.skill, keyword: w };
          break outer;
        }
      }
      for (const p of group.phrases) {
        if (promptLower.includes(p)) {
          skillMatch = { skill: group.skill, keyword: p };
          break outer;
        }
      }
    }
  }

  // --- Reinforcement ---
  const activeLevel = readFlag(flagPath);
  if (activeLevel !== null && activeLevel !== 'off') {
    const compressionLine =
      'PAKKA COMPRESSION ACTIVE (' +
      activeLevel +
      '). Drop articles/filler/pleasantries/hedging. Fragments OK. Code/commits/security: write normal.';
    const genericSkillCheck =
      'SKILL-CHECK: design/spec/plan/approach → YOU MUST invoke /pakka:plan FIRST. implement/fix/debug/add → YOU MUST invoke /pakka:build FIRST. verify/review/done → YOU MUST invoke /pakka:review FIRST. No exceptions.';

    let additionalContext;
    if (skillMatch) {
      additionalContext =
        "SKILL-CHECK: Your message contains '" + skillMatch.keyword + "' → " + skillMatch.skill + ' MUST be invoked BEFORE your response. No exceptions.\n\n' +
        compressionLine;
    } else {
      additionalContext = compressionLine + '\n\n' + genericSkillCheck;
    }

    const out = {
      hookSpecificOutput: {
        hookEventName: 'UserPromptSubmit',
        additionalContext: additionalContext,
      },
    };
    process.stdout.write(JSON.stringify(out));
  }
} catch (_) {
  // Silent-fail all errors — never block the user's prompt
}
