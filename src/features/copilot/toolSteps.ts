import type { Step } from '@/components/ui/tool-steps';
import type { ToolExecution } from './types';

/** omp tool results are `{content:[{type:'text',text}], details?}`; flatten them. */
export function parseToolResult(raw: unknown): { text: string; details?: Record<string, any> } {
  let v: any = raw;
  if (typeof raw === 'string') {
    try {
      v = JSON.parse(raw);
    } catch {
      return { text: raw };
    }
  }
  if (!v || typeof v !== 'object') return { text: v == null ? '' : String(v) };
  const content = Array.isArray(v.content) ? v.content : [];
  const text = content
    .map((c: any) => (c && c.type === 'text' ? c.text : ''))
    .filter(Boolean)
    .join('\n');
  return { text, details: v.details };
}

const base = (p: unknown) => {
  const s = String(p ?? '');
  return s.split('/').filter(Boolean).slice(-2).join('/') || s;
};

/** omp's `N|text` diff (with +/-/space markers) as a unified patch the DiffView understands. */
export function ompDiffToPatch(diff: string): string | undefined {
  const out: string[] = [];
  let del = 0;
  let add = 0;
  let ctx = 0;
  for (const line of diff.split('\n')) {
    const m = /^([ +-])\s*\d*\|(.*)$/.exec(line);
    if (!m) continue;
    out.push(m[1] + m[2]);
    if (m[1] === '-') del++;
    else if (m[1] === '+') add++;
    else ctx++;
  }
  if (!out.length || (!del && !add)) return undefined;
  return `@@ -1,${del + ctx} +1,${add + ctx} @@\n${out.join('\n')}`;
}

function patchFromArgs(args?: Record<string, any>): string | undefined {
  const o = args?.old_string ?? args?.oldString;
  const n = args?.new_string ?? args?.newString;
  if (typeof o !== 'string' || typeof n !== 'string') return undefined;
  const oldL = o === '' ? [] : o.split('\n');
  const newL = n === '' ? [] : n.split('\n');
  return `@@ -1,${oldL.length} +1,${newL.length} @@\n${oldL.map((l) => '-' + l).join('\n')}${oldL.length && newL.length ? '\n' : ''}${newL.map((l) => '+' + l).join('\n')}`;
}

const trim = (s: string, n = 64) => (s.length > n ? s.slice(0, n - 1) + '…' : s);

/** One tool execution described as what it did, from omp's own tool names, arguments and results. */
export function toolToStep(tc: ToolExecution): Step {
  const args = (tc.args || {}) as Record<string, any>;
  const intent: string | undefined = tc.intent || (typeof args.i === 'string' ? args.i : undefined);
  const { text, details } = parseToolResult(tc.result);
  const running = tc.status === 'running';
  const failed = tc.status === 'failed';
  const name = tc.name;

  // Host tools reached through omp's xd:// scheme: show the tool, not the file verb.
  const xd = typeof args.path === 'string' && args.path.startsWith('xd://') ? args.path.slice(5) : '';
  let label = intent || name.replace(/_/g, ' ');
  let detail: string | undefined;
  let detailLabel = 'Input';
  let patch: string | undefined;
  let hint: string | undefined;

  switch (xd ? 'xd' : name) {
    case 'xd':
      label = intent || `Host tool ${xd}`;
      detail = typeof args.content === 'string' && args.content !== '{}' ? args.content : undefined;
      break;
    case 'read':
      label = `${running ? 'Reading' : 'Read'} ${base(args.path)}`;
      break;
    case 'write':
      label = `${running ? 'Creating' : 'Created'} ${base(args.path)}`;
      if (typeof args.content === 'string' && args.content) {
        detail = args.content.length > 4000 ? args.content.slice(0, 4000) + '\n…' : args.content;
        detailLabel = 'Content';
      }
      break;
    case 'execute_bash':
      // NextDeploy host tool: run a command inside a site. Show the command itself, not just the intent.
      label = intent || `Ran a command in ${args.app_id ?? 'site'}`;
      hint = trim(String(args.command ?? ''), 90);
      detail = String(args.command ?? '');
      detailLabel = args.app_id ? `Command in ${args.app_id}` : 'Command';
      break;
    case 'edit':
    case 'ast_edit':
      label = `${running ? 'Editing' : 'Edited'} ${base(args.path)}`;
      patch = (typeof details?.diff === 'string' && ompDiffToPatch(details.diff)) || patchFromArgs(args);
      if (!patch && typeof args.input === 'string') {
        detail = args.input; // omp's edit tool takes one `input` text
        detailLabel = 'Edit';
      }
      break;
    case 'bash': {
      const cmd = String(args.command ?? '');
      const bg = args.async || args.name ? ' in the background' : '';
      label = `${running ? 'Running' : 'Ran'} ${trim(cmd.split('\n')[0], 72)}${bg}`;
      detail = cmd;
      detailLabel = args.cwd ? `Command in ${args.cwd}` : 'Command';
      break;
    }
    case 'eval': {
      // omp's code tool (Python / JavaScript): show the code that ran.
      const code = String(args.code ?? '');
      label = intent || `${running ? 'Running' : 'Ran'} ${args.language || 'code'}`;
      hint = trim(code.split('\n').find((l) => l.trim()) ?? '', 90);
      detail = code;
      detailLabel = args.language ? `Code (${args.language})` : 'Code';
      break;
    }
    case 'container_action':
      label = intent || 'Container action';
      hint = `${args.action ?? ''} ${args.container ?? ''}`.trim();
      break;
    case 'grep':
      label = `Searched for “${trim(String(args.pattern ?? ''), 48)}”`;
      break;
    case 'glob':
    case 'find':
      label = `${running ? 'Listing' : 'Listed'} ${trim(String(args.pattern ?? args.path ?? ''), 48)}`;
      break;
    case 'web_search':
      label = `Searched the web for “${trim(String(args.query ?? ''), 48)}”`;
      break;
    case 'fetch':
      label = `Fetched ${trim(String(args.url ?? ''), 56)}`;
      break;
    case 'todo':
      label = intent || 'Updated the plan';
      break;
    case 'ask':
      label = intent || 'Asked a question';
      break;
    case 'task':
      label = intent || trim(String(args.description ?? 'Ran a subagent'), 64);
      break;
    default:
      label = intent || name.replace(/_/g, ' ');
      // Host tools: say what they acted on (site / container / domain).
      hint = [args.app_id, args.container, args.domain].find((v) => typeof v === 'string') as string | undefined;
  }
  if (!hint && (name === 'read' || name === 'write' || name === 'edit' || name === 'ast_edit') && typeof args.path === 'string') hint = trim(args.path, 70);
  if (!hint && name === 'grep' && typeof args.path === 'string') hint = trim(args.path, 60);
  if (!hint && name === 'web_search') hint = undefined;
  if (!hint && name === 'bash' && args.cwd) hint = trim(String(args.cwd), 60);

  if (!detail && Object.keys(args).length) {
    const { i: _i, ...rest } = args;
    if (Object.keys(rest).length) detail = JSON.stringify(rest, null, 2);
  }

  return {
    id: tc.id,
    label,
    hint,
    status: failed ? 'error' : running ? 'running' : 'done',
    detail,
    detailLabel,
    output: patch ? undefined : text || undefined,
    error: failed ? text || 'Failed' : undefined,
    patch,
  };
}
