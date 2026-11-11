import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const mode = process.argv[2] || 'post';
let raw = '';
try {
  raw = fs.readFileSync(0, 'utf8');
} catch {
  // empty stdin
}

let input = {};
try {
  input = raw.trim() ? JSON.parse(raw) : {};
} catch {
  input = {};
}

const okOut = (obj = {}) => {
  process.stdout.write(JSON.stringify(obj) + '\n');
  process.exit(0);
};

const isWin = process.platform === 'win32';
const script = path.join(
  os.homedir(),
  '.agents',
  'skills',
  'impeccable',
  'scripts',
  isWin ? 'impeccable.cmd' : 'impeccable'
);
if (!fs.existsSync(script)) okOut({});

const cwd = input.workspacePaths?.[0] || process.cwd();
const sessionId = input.conversationId || 'antigravity';

function runImpeccable(payload) {
  const res = isWin
    ? spawnSync('cmd.exe', ['/d', '/s', '/c', `"${script}" hook`], {
        input: JSON.stringify(payload),
        cwd,
        encoding: 'utf8',
        windowsHide: true,
      })
    : spawnSync(script, ['hook'], {
        input: JSON.stringify(payload),
        cwd,
        encoding: 'utf8',
      });
  if (!res.stdout) return null;
  try {
    return JSON.parse(res.stdout.trim());
  } catch {
    return null;
  }
}

if (mode === 'post') {
  const filePath = input.toolCall?.args?.Path || input.toolCall?.args?.path || '';
  if (!/\.(tsx|jsx|css|scss|html|vue|svelte)$/i.test(filePath)) okOut({});
  runImpeccable({
    session_id: sessionId,
    hook_event_name: 'PostToolUse',
    tool_name: 'Edit',
    tool_input: { file_path: filePath },
    cwd,
  });
  okOut({});
}

if (mode === 'stop') {
  if (input.terminationReason && input.terminationReason !== 'NO_TOOL_CALL') {
    okOut({});
  }
  const out = runImpeccable({
    session_id: sessionId,
    hook_event_name: 'Stop',
    stop_hook_active: Number(input.executionNum || 1) > 1,
    cwd,
  });
  if (out?.decision === 'block' && out.reason) {
    okOut({ decision: 'continue', reason: out.reason });
  }
  okOut({});
}

okOut({});
