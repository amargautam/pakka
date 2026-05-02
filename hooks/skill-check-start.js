'use strict';

// SessionStart hook — injects skill-check.md as a dedicated additionalContext
// entry, separate from the compression rules emitted by compress-start.js.
// Separation ensures the model sees the skill-check directive before the
// 200+ line compression ruleset, not buried after it.

const fs = require('fs');
const path = require('path');

const FALLBACK =
  '<EXTREMELY_IMPORTANT>\n' +
  'YOU MUST invoke the relevant pakka command before ANY response when the signal matches. This is not optional. This is not a suggestion. You cannot rationalize your way out of it.\n' +
  '\n' +
  'IF TASK SIGNAL MATCHES, YOU DO NOT HAVE A CHOICE. INVOKE THE COMMAND FIRST.\n' +
  '\n' +
  '## Trigger signals\n' +
  '\n' +
  '### /pakka:plan — design, spec, probe, challenge, slice\n' +
  'TRIGGER when user message contains ANY of:\n' +
  '- design, spec, plan, approach, architecture, structure, "how should we", "what should we", "should we", "let\'s build", "we need to", "thinking about", "considering", "what about", "how about", "proposal", challenge, probe, decompose, slice, tickets\n' +
  '\n' +
  'TRIGGER when the message is a question about direction, not execution.\n' +
  '\n' +
  '### /pakka:build — TDD, debug, map, audit\n' +
  'TRIGGER when user message contains ANY of:\n' +
  '- implement, fix, debug, add, refactor, TDD, test, "not working", broken, error, "build this", "write the code", "make it work", "how does", "walk me through", "explain this", coupling, "hard to test"\n' +
  '\n' +
  '### /pakka:review — verify, audit, finish\n' +
  'TRIGGER when user message contains ANY of:\n' +
  '- verify, check, audit, "is this right", finalize, review, done, "looks good", ship, "ready to", "approve", "sign off", receive, feedback\n' +
  '\n' +
  '## Rules\n' +
  '\n' +
  '1. If signal matches: invoke the command BEFORE writing any response. No exceptions.\n' +
  '2. If 1% chance a command applies: invoke it. Over-invoking is better than skipping.\n' +
  '3. If in doubt between plan/build: default to /pakka:plan.\n' +
  '4. Invoked command turns out to be wrong? Stop early — still better than skipping.\n' +
  '5. SUBAGENTS: skip this rule. You were dispatched for a specific task — execute it.\n' +
  '</EXTREMELY_IMPORTANT>\n';

try {
  const pluginRoot =
    process.env.CLAUDE_PLUGIN_ROOT ||
    path.join(__dirname, '..');

  let content;
  try {
    content = fs.readFileSync(path.join(pluginRoot, 'rules', 'skill-check.md'), 'utf8');
  } catch (_) {
    content = FALLBACK;
  }

  const out = {
    hookSpecificOutput: {
      hookEventName: 'SessionStart',
      additionalContext: content,
    },
  };

  process.stdout.write(JSON.stringify(out));
  process.exit(0);
} catch (_) {
  process.exit(0);
}
