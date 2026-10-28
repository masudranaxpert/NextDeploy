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
      break;
    case 'edit':
    case 'ast_edit':
      label = `${running ? 'Editing' : 'Edited'} ${base(args.path)}`;
      patch = (typeof details?.diff === 'string' && ompDiffToPatch(details.diff)) || patchFromArgs(args);
      break;
    case 'bash':
      label = `${running ? 'Running' : 'Ran'} ${trim(String(args.command ?? ''), 72)}`;
      detail = String(args.command ?? '');
      detailLabel = 'Command';
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
  }

  if (!detail && Object.keys(args).length) {
    const { i: _i, ...rest } = args;
    if (Object.keys(rest).length) detail = JSON.stringify(rest, null, 2);
  }

  return {
    id: tc.id,
    label,
    status: failed ? 'error' : running ? 'running' : 'done',
    detail,
    detailLabel,
    output: patch ? undefined : text || undefined,
    error: failed ? text || 'Failed' : undefined,
    patch,
  };
}
