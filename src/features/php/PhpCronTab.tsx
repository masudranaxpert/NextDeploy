import React, { useState } from 'react';
import { BookOpen, Clock, Pause, Play, Plus, Terminal, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { ShadcnDialog } from '../../components/ui/Primitives';
import type { PhpCronRow } from '../../lib/api/phpApi';
import {
  Card,
  ErrorBlock,
  Field,
  GhostButton,
  LoadingBlock,
  Pill,
  PrimaryButton,
  formatTime,
  inputCls,
  selectCls,
  usePhpSite,
} from './ui';

const TEMPLATES: Array<{ label: string; expr: string; minutes: number }> = [
  { label: 'Every minute', expr: '* * * * *', minutes: 1 },
  { label: 'Every 5 minutes', expr: '*/5 * * * *', minutes: 5 },
  { label: 'Every 15 minutes', expr: '*/15 * * * *', minutes: 15 },
  { label: 'Once an hour', expr: '0 * * * *', minutes: 60 },
  { label: 'Every 3 hours', expr: '0 */3 * * *', minutes: 180 },
  { label: 'Once a day (00:00)', expr: '0 0 * * *', minutes: 1440 },
  { label: 'Every day at 04:00', expr: '0 4 * * *', minutes: 1440 },
  { label: 'Once a week (Sunday 00:00)', expr: '0 0 * * 0', minutes: 10080 },
  { label: 'Once a month (1st, 00:00)', expr: '0 0 1 * *', minutes: 43200 },
];

type JobKind = 'custom' | 'file' | 'wordpress' | 'laravel' | 'url';

const JOB_KINDS: Array<{ id: JobKind; label: string }> = [
  { id: 'file', label: 'Run a PHP file from my site' },
  { id: 'wordpress', label: 'WordPress scheduled tasks (wp-cron)' },
  { id: 'laravel', label: 'Laravel scheduler' },
  { id: 'url', label: 'Open a web address (URL)' },
  { id: 'custom', label: 'Custom command' },
];

const statusTone = (s: string): 'green' | 'red' | 'zinc' => (s === 'ok' ? 'green' : s ? 'red' : 'zinc');

export const PhpCronTab: React.FC<{ app: AppStack }> = ({ app }) => {
  const { detail, error, loading, reload } = usePhpSite(app.id);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<PhpCronRow | null>(null);
  const [schedule, setSchedule] = useState('*/5 * * * *');
  const [command, setCommand] = useState('');
  const [kind, setKind] = useState<JobKind>('file');
  const [filePath, setFilePath] = useState('');
  const [urlValue, setUrlValue] = useState('');
  const [help, setHelp] = useState(false);
  const [busy, setBusy] = useState('');
  const [output, setOutput] = useState<{ title: string; text: string } | null>(null);

  if (loading) return <LoadingBlock />;
  if (error || !detail) return <ErrorBlock message={error || 'Site not found'} onRetry={reload} />;
  if (detail.site.site_type === 'static') {
    return <Card title="Cron jobs"><p className="text-xs text-zinc-400">Static sites have no PHP runtime, so they cannot run cron jobs.</p></Card>;
  }

  const { limits } = detail;
  // Cron jobs start in the document root, which for Laravel-style sites is a subfolder (public/).
  // The builder therefore always starts from the site folder that holds the project files.
  const rootDir = (detail.site.root_dir || '').replace(/^\/+|\/+$/g, '');
  const siteDir = rootDir && detail.doc_root.endsWith('/' + rootDir) ? detail.doc_root.slice(0, -(rootDir.length + 1)) : detail.doc_root;

  const build = (k: JobKind, file: string, url: string): string => {
    switch (k) {
      case 'file':
        return file.trim() ? `cd ${siteDir} && php ${file.trim().replace(/^\/+/, '')}` : '';
      case 'wordpress':
        return `cd ${siteDir} && wp cron event run --due-now`;
      case 'laravel':
        return `cd ${siteDir} && php artisan schedule:run`;
      case 'url':
        return url.trim() ? `curl -fsS -o /dev/null ${url.trim()}` : '';
      default:
        return '';
    }
  };
  const pickKind = (k: JobKind) => {
    setKind(k);
    if (k !== 'custom') setCommand(build(k, filePath, urlValue));
  };
  const min = limits.unlimited ? 1 : detail.min_cron_interval_minutes;
  const limitReached = !limits.unlimited && limits.max_cron_jobs > 0 && limits.used_cron_jobs >= limits.max_cron_jobs;

  const openCreate = () => {
    setEditing(null);
    setSchedule(TEMPLATES.find((t) => t.minutes >= min)?.expr || '0 * * * *');
    const k: JobKind = detail.site.site_type === 'wordpress' ? 'wordpress' : 'file';
    setKind(k);
    setFilePath('');
    setUrlValue('');
    setCommand(build(k, '', ''));
    setOpen(true);
  };
  const openEdit = (j: PhpCronRow) => {
    setEditing(j);
    setSchedule(j.schedule);
    setCommand(j.command);
    setKind('custom');
    setOpen(true);
  };

  const save = async () => {
    setBusy('save');
    const res = editing
      ? await backendApi.phpCronUpdate(app.id, editing.id, { schedule, command })
      : await backendApi.phpCronCreate(app.id, { schedule, command });
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Could not save the cron job');
      return;
    }
    setOpen(false);
    toast.success(editing ? 'Cron job updated' : 'Cron job added');
    await reload();
  };

  const toggle = async (j: PhpCronRow) => {
    setBusy('t' + j.id);
    const res = await backendApi.phpCronUpdate(app.id, j.id, { enabled: !j.enabled });
    setBusy('');
    if (!res.ok) toast.error(res.error || 'Could not update the cron job');
    await reload();
  };

  const remove = async (j: PhpCronRow) => {
    if (!window.confirm('Delete this cron job?')) return;
    setBusy('d' + j.id);
    const res = await backendApi.phpCronDelete(app.id, j.id);
    setBusy('');
    if (!res.ok) toast.error(res.error || 'Could not delete the cron job');
    await reload();
  };

  const runNow = async (j: PhpCronRow) => {
    setBusy('r' + j.id);
    const res = await backendApi.phpCronRun(app.id, j.id);
    setBusy('');
    if (!res.ok || !res.data) {
      toast.error(res.error || 'Could not run the job');
      return;
    }
    setOutput({ title: `${res.data.status}: ${j.command}`, text: res.data.output || '(no output)' });
    await reload();
  };

  return (
    <div className="space-y-5">
      <Card
        title="Cron jobs"
        description="Commands run inside this site's PHP container as its own system user, from the document root."
        actions={
          <>
            <GhostButton onClick={() => setHelp(true)} title="How to add a cron job">
              <BookOpen className="h-3.5 w-3.5 text-zinc-400" /> Docs
            </GhostButton>
            {!limits.unlimited && <Pill tone={limitReached ? 'amber' : 'zinc'}>{limits.used_cron_jobs}/{limits.max_cron_jobs || '∞'} used</Pill>}
            <PrimaryButton onClick={openCreate} disabled={limitReached}>
              <Plus className="h-3.5 w-3.5" /> Add cron job
            </PrimaryButton>
          </>
        }
      >
        {min > 1 && <p className="mb-3 text-[11px] text-zinc-500">Jobs may run at most once every {min} minutes.</p>}
        {(detail.cron_jobs ?? []).length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <Clock className="h-6 w-6 text-zinc-600" />
            <p className="text-xs text-zinc-400">No cron jobs yet.</p>
          </div>
        ) : (
          <ul className="divide-y divide-white/[0.05]">
            {(detail.cron_jobs ?? []).map((j) => (
              <li key={j.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                <button type="button" onClick={() => openEdit(j)} className="min-w-0 flex-1 cursor-pointer text-left">
                  <div className="flex flex-wrap items-center gap-2">
                    <code className="rounded bg-[#09090b] px-1.5 py-0.5 font-mono text-[11px] text-orange-300">{j.schedule}</code>
                    {!j.enabled && <Pill tone="amber">paused</Pill>}
                    {j.last_status && <Pill tone={statusTone(j.last_status)}>{j.last_status}</Pill>}
                  </div>
                  <div className="mt-1 truncate font-mono text-xs text-zinc-200">{j.command}</div>
                  <div className="mt-0.5 text-[11px] text-zinc-500">Last run: {formatTime(j.last_run_at)}</div>
                </button>
                <div className="flex items-center gap-1.5">
                  {j.last_output && (
                    <GhostButton onClick={() => setOutput({ title: `Last output: ${j.command}`, text: j.last_output })} title="Last output">
                      <Terminal className="h-3.5 w-3.5" />
                    </GhostButton>
                  )}
                  <GhostButton busy={busy === 'r' + j.id} onClick={() => runNow(j)} title="Run now">
                    <Play className="h-3.5 w-3.5" /> Run
                  </GhostButton>
                  <GhostButton busy={busy === 't' + j.id} onClick={() => toggle(j)} title={j.enabled ? 'Pause' : 'Resume'}>
                    {j.enabled ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                  </GhostButton>
                  <GhostButton tone="danger" busy={busy === 'd' + j.id} onClick={() => remove(j)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </GhostButton>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <ShadcnDialog
        open={open}
        onClose={() => setOpen(false)}
        title={editing ? 'Edit cron job' : 'Add cron job'}
        icon={<Clock className="h-4 w-4" />}
        maxWidth="lg"
        footer={
          <>
            <GhostButton onClick={() => setOpen(false)}>Cancel</GhostButton>
            <PrimaryButton busy={busy === 'save'} onClick={save} disabled={!command.trim()}>
              {editing ? 'Save' : 'Add'}
            </PrimaryButton>
          </>
        }
      >
        <div className="space-y-3">
          <Field label="What should it run?">
            <select className={selectCls} value={kind} onChange={(e) => pickKind(e.target.value as JobKind)}>
              {JOB_KINDS.map((k) => (
                <option key={k.id} value={k.id}>{k.label}</option>
              ))}
            </select>
          </Field>
          {kind === 'file' && (
            <Field label="PHP file" hint={`Path inside your site folder (${siteDir}). For example cron.php or scripts/cleanup.php.`}>
              <input
                className={`${inputCls} font-mono`}
                value={filePath}
                placeholder="cron.php"
                onChange={(e) => { setFilePath(e.target.value); setCommand(build('file', e.target.value, urlValue)); }}
              />
            </Field>
          )}
          {kind === 'url' && (
            <Field label="Web address" hint="The address is opened like a browser would, e.g. https://example.com/cron.php?key=secret.">
              <input
                className={`${inputCls} font-mono`}
                value={urlValue}
                placeholder="https://example.com/cron.php"
                onChange={(e) => { setUrlValue(e.target.value); setCommand(build('url', filePath, e.target.value)); }}
              />
            </Field>
          )}
          <Field label="Schedule" hint="Pick how often it runs, or type minute hour day-of-month month weekday (or @hourly, @daily, @weekly).">
            <select className={`${selectCls} mb-2`} value="" onChange={(e) => e.target.value && setSchedule(e.target.value)}>
              <option value="">Choose how often…</option>
              {TEMPLATES.filter((t) => t.minutes >= min).map((t) => (
                <option key={t.expr} value={t.expr}>{t.label}</option>
              ))}
            </select>
            <input className={`${inputCls} font-mono`} value={schedule} onChange={(e) => setSchedule(e.target.value)} />
          </Field>
          <Field label="Command" hint="This is what actually runs (sh -c). You can edit it.">
            <input
              className={`${inputCls} font-mono`}
              value={command}
              onChange={(e) => { setCommand(e.target.value); setKind('custom'); }}
              placeholder="php /path/to/script.php"
            />
          </Field>
          <button type="button" onClick={() => setHelp(true)} className="inline-flex cursor-pointer items-center gap-1.5 text-[11px] text-orange-300 hover:text-orange-200">
            <BookOpen className="h-3 w-3" /> How do cron jobs work?
          </button>
        </div>
      </ShadcnDialog>

      <ShadcnDialog
        open={help}
        onClose={() => setHelp(false)}
        title="How to add a cron job"
        icon={<BookOpen className="h-4 w-4" />}
        maxWidth="xl"
        footer={<GhostButton onClick={() => setHelp(false)}>Close</GhostButton>}
      >
        <div className="max-h-[60vh] space-y-4 overflow-y-auto pr-1 text-xs leading-relaxed text-zinc-300">
          <p>A cron job runs a command automatically on a schedule, for example to send emails every 5 minutes or clean old files every night.</p>
          <div>
            <div className="mb-1 font-medium text-zinc-100">1. Where are my files?</div>
            <p>Your site files live in <code className="font-mono text-orange-300">{siteDir}</code>. A file you uploaded as <code className="font-mono text-orange-300">cron.php</code> in the File Manager is <code className="font-mono text-orange-300">{siteDir}/cron.php</code>.</p>
          </div>
          <div>
            <div className="mb-1 font-medium text-zinc-100">2. The easy way</div>
            <p>Click <b>Add cron job</b>, choose <b>Run a PHP file from my site</b>, type the file's path (like <code className="font-mono text-orange-300">cron.php</code> or <code className="font-mono text-orange-300">scripts/cleanup.php</code>), then pick how often. The command is filled in for you.</p>
          </div>
          <div>
            <div className="mb-1 font-medium text-zinc-100">3. Examples</div>
            <ul className="space-y-1.5">
              <li><code className="font-mono text-zinc-100">cd {siteDir} &amp;&amp; php cron.php</code> — run your own script</li>
              <li><code className="font-mono text-zinc-100">cd {siteDir} &amp;&amp; wp cron event run --due-now</code> — WordPress scheduled posts, backups and plugin tasks</li>
              <li><code className="font-mono text-zinc-100">cd {siteDir} &amp;&amp; php artisan schedule:run</code> — Laravel (schedule it every minute)</li>
              <li><code className="font-mono text-zinc-100">curl -fsS -o /dev/null https://example.com/cron.php?key=secret</code> — open a web address</li>
            </ul>
          </div>
          <div>
            <div className="mb-1 font-medium text-zinc-100">4. Schedule format</div>
            <p>Five fields: <code className="font-mono text-orange-300">minute hour day-of-month month weekday</code>. <code className="font-mono text-orange-300">*/5 * * * *</code> means every 5 minutes, <code className="font-mono text-orange-300">0 4 * * *</code> means every day at 04:00. Use the “Choose how often” list if you are unsure.</p>
          </div>
          <div>
            <div className="mb-1 font-medium text-zinc-100">5. Good to know</div>
            <ul className="list-disc space-y-1 pl-4">
              <li>Commands run as your site's own user inside your PHP container, so they can only touch your own files.</li>
              <li>Use <b>Run</b> on a job to test it now and see its output.</li>
              {min > 1 && <li>Jobs can run at most once every {min} minutes.</li>}
              <li>The command starts in your document root{rootDir ? ` (${detail.doc_root})` : ''}. That is why the examples first <code className="font-mono">cd</code> into the site folder.</li>
            </ul>
          </div>
        </div>
      </ShadcnDialog>

      <ShadcnDialog
        open={output !== null}
        onClose={() => setOutput(null)}
        title={output?.title || ''}
        icon={<Terminal className="h-4 w-4" />}
        maxWidth="xl"
        footer={<GhostButton onClick={() => setOutput(null)}>Close</GhostButton>}
      >
        <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-lg bg-[#09090b] p-3 font-mono text-[11px] leading-relaxed text-zinc-300">
          {output?.text}
        </pre>
      </ShadcnDialog>
    </div>
  );
};
