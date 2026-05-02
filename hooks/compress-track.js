'use strict';

// UserPromptSubmit hook — per-turn compression reinforcement.
// Also handles /pakka:compress <level> and /pakka:compress off commands.

const fs = require('fs');
const path = require('path');

try {
  const { VALID_LEVELS, safeWriteFlag, readFlag } = require('./compress-config');

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
    path.join(require('os').homedir(), '.claude');
  const flagPath = path.join(claudeConfigDir, '.pakka-level');

  // --- Command: /pakka:compress <level> or /pakka compress <level> ---
  // Matches both slash variants: /pakka:compress and /pakka compress
  const compressCmd = /^\/pakka(?::compress| compress)\s+(\S+)/i.exec(prompt);
  if (compressCmd) {
    const arg = compressCmd[1].toLowerCase();
    if (arg === 'off') {
      // Turn off compression: remove the flag file
      try { fs.unlinkSync(flagPath); } catch (_) { /* silent */ }
    } else if (VALID_LEVELS.includes(arg) && arg !== 'off') {
      // Set the new level
      safeWriteFlag(flagPath, arg);
    }
    // After command handling, exit without emitting reinforcement context
    process.exit(0);
  }

  // --- Prose commands: "pakka verbose" or "normal mode" ---
  if (/\b(pakka\s+verbose|normal\s+mode)\b/i.test(prompt)) {
    try { fs.unlinkSync(flagPath); } catch (_) { /* silent */ }
    // Fall through to read the flag (which is now gone, so no reinforcement)
  }

  // --- Reinforcement ---
  const activeLevel = readFlag(flagPath);
  if (activeLevel !== null && activeLevel !== 'off') {
    const out = {
      hookSpecificOutput: {
        hookEventName: 'UserPromptSubmit',
        additionalContext:
          'PAKKA COMPRESSION ACTIVE (' +
          activeLevel +
          '). Drop articles/filler/pleasantries/hedging. Fragments OK. Code/commits/security: write normal.',
      },
    };
    process.stdout.write(JSON.stringify(out));
  }
} catch (_) {
  // Silent-fail all errors — never block the user's prompt
}
