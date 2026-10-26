import React, { useState } from 'react';
import { BookOpen, Clock, Pause, Play, Plus, Terminal, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { ShadcnDialog } from '../../components/ui/Primitives';
import type { PhpCronRow } from '../../lib/api/phpApi';
import {
  Card,
  EmptyState,
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
    setSchedule(TEMPLATES.find((t) => t.minutes >= Math.max(min, 5))?.expr || '0 * * * *');
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
        title={
          <span className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-orange-500/30 bg-orange-500/10">
              <Clock className="h-4 w-4 text-orange-400" />
            </span>
            <span className="text-sm font-semibold text-zinc-100">Cronjobs</span>
          </span>
        }
        actions={!limits.unlimited ? <Pill tone={limitReached ? 'amber' : 'zinc'}>{limits.used_cron_jobs}/{limits.max_cron_jobs || '∞'} used</Pill> : undefined}
      >
        {min > 1 && <p className="mb-3 text-[11px] text-zinc-500">Minimum interval: {min} minutes.</p>}
        <div className="grid grid-cols-2 gap-2">
          <GhostButton onClick={() => setHelp(true)} title="How to add a cron job">
            <BookOpen className="h-3.5 w-3.5 text-zinc-400" /> Docs
          </GhostButton>
          <PrimaryButton onClick={openCreate} disabled={limitReached}>
            <Plus className="h-3.5 w-3.5" /> Add cron job
          </PrimaryButton>
        </div>

        <div className="mt-4">
        {(detail.cron_jobs ?? []).length === 0 ? (
          <EmptyState icon={<Clock className="h-6 w-6" />} text="No cron jobs yet." />
        ) : (
          <ul className="space-y-1">
            {(detail.cron_jobs ?? []).map((j) => {
              const phpFile = j.command.match(/([\w.-]+\.php)/);
              const label = phpFile ? phpFile[1] : (j.command.trim().split(/\s+/)[0].split('/').pop() || 'cron').slice(0, 18);
              const ran = j.last_run_at && j.last_run_at !== 'never';
              return (
                <li key={j.id} className="py-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="inline-flex max-w-[11rem] items-center gap-1.5 rounded-md border border-sky-500/25 bg-sky-500/10 px-2 py-0.5 font-mono text-[11px] text-sky-300">
                      <button
                        type="button"
                        onClick={() => openEdit(j)}
                        title="Edit cron job"
                        className="truncate cursor-pointer"
                      >
                        {label}
                      </button>
                    </span>
                    <span className="rounded-md border border-orange-500/25 bg-orange-500/[0.07] px-2 py-0.5 font-mono text-[11px] text-orange-300">{j.schedule}</span>
                    {!j.enabled && <Pill tone="amber">paused</Pill>}
                    {j.last_status && <Pill tone={statusTone(j.last_status)}>{j.last_status}</Pill>}
                    <span className="ml-auto flex shrink-0 items-center gap-1.5">
                      {j.last_output && (
                        <GhostButton onClick={() => setOutput({ title: `Last output: ${j.command}`, text: j.last_output })} title="Last output" className="!h-9 !w-9 !px-0">
                          <Terminal className="h-3.5 w-3.5" />
                        </GhostButton>
                      )}
                      <GhostButton busy={busy === 'r' + j.id} onClick={() => runNow(j)} title="Run now">
                        <Play className="h-3.5 w-3.5" /> Run
                      </GhostButton>
                      <GhostButton busy={busy === 't' + j.id} onClick={() => toggle(j)} title={j.enabled ? 'Pause' : 'Resume'} className="!h-9 !w-9 !px-0">
                        {j.enabled ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                      </GhostButton>
                      <GhostButton tone="danger" busy={busy === 'd' + j.id} onClick={() => remove(j)} title="Delete cron job" className="!h-9 !w-9 !px-0">
                        <Trash2 className="h-3.5 w-3.5" />
                      </GhostButton>
                    </span>
                  </div>
                  <button type="button" onClick={() => openEdit(j)} className="mt-2 flex w-full items-center justify-between gap-2 text-left cursor-pointer" title="Edit cron job">
                    <span className="truncate font-mono text-[11px] text-zinc-500">{j.command}</span>
                  </button>
                  <div className="mt-1.5 flex items-center justify-between gap-2 text-[11px]">
                    <span className="text-zinc-500">
                      Last run: <span className={ran ? 'text-emerald-400' : 'text-zinc-500'}>{formatTime(j.last_run_at)}</span>
                    </span>
                    {ran && (
                      <span className="font-mono text-zinc-500">
                        {new Date(j.last_run_at).toLocaleString(undefined, { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' })}
                      </span>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        </div>
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
            <Field label="PHP file">
              <input
                className={`${inputCls} font-mono`}
                value={filePath}
                placeholder="cron.php  or  scripts/cleanup.php"
                onChange={(e) => { setFilePath(e.target.value); setCommand(build('file', e.target.value, urlValue)); }}
              />
            </Field>
          )}
          {kind === 'url' && (
            <Field label="Web address">
              <input
                className={`${inputCls} font-mono`}
                value={urlValue}
                placeholder="https://example.com/cron.php"
                onChange={(e) => { setUrlValue(e.target.value); setCommand(build('url', filePath, e.target.value)); }}
              />
            </Field>
          )}
          <Field label="Schedule" hint="minute  hour  day  month  weekday">
            <select className={`${selectCls} mb-2`} value="" onChange={(e) => e.target.value && setSchedule(e.target.value)}>
              <option value="">Choose a schedule…</option>
              {TEMPLATES.filter((t) => t.minutes >= min).map((t) => (
                <option key={t.expr} value={t.expr}>{t.label}</option>
              ))}
            </select>
            <input className={`${inputCls} font-mono`} value={schedule} onChange={(e) => setSchedule(e.target.value)} />
          </Field>
          <Field label="Command">
            <input
              className={`${inputCls} font-mono`}
              value={command}
              onChange={(e) => { setCommand(e.target.value); setKind('custom'); }}
              placeholder="command to run"
            />
          </Field>
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
        <div className="max-h-[60vh] space-y-5 overflow-y-auto pr-1 text-xs leading-relaxed text-zinc-400">
          <p>A cron job runs a command on a schedule, for example to send emails every 5 minutes or clean old files every night.</p>

          <section className="space-y-1.5">
            <h3 className="text-xs font-semibold text-zinc-100">Where your files are</h3>
            <p>
              Your site folder is <code className="font-mono text-orange-300">{siteDir}</code>. A file uploaded as{' '}
              <code className="font-mono text-orange-300">cron.php</code> in the File Manager is{' '}
              <code className="font-mono text-orange-300">{siteDir}/cron.php</code>.
            </p>
          </section>

          <section className="space-y-1.5">
            <h3 className="text-xs font-semibold text-zinc-100">The easy way</h3>
            <p>
              Click <b className="text-zinc-200">Add cron job</b>, keep <b className="text-zinc-200">Run a PHP file from my site</b>, type the
              file's path (<code className="font-mono text-orange-300">cron.php</code> or{' '}
              <code className="font-mono text-orange-300">scripts/cleanup.php</code>) and choose a schedule. The command is filled in for you.
            </p>
          </section>

          <section className="space-y-1.5">
            <h3 className="text-xs font-semibold text-zinc-100">Examples</h3>
            <ul className="space-y-2">
              {[
                [`cd ${siteDir} && php cron.php`, 'your own script'],
                [`cd ${siteDir} && wp cron event run --due-now`, 'WordPress scheduled tasks'],
                [`cd ${siteDir} && php artisan schedule:run`, 'Laravel, every minute'],
                ['curl -fsS -o /dev/null https://example.com/cron.php', 'open a web address'],
              ].map(([cmd, note]) => (
                <li key={cmd}>
                  <code className="block break-all rounded-md bg-[#09090b] px-2 py-1.5 font-mono text-[11px] text-zinc-200">{cmd}</code>
                  <span className="mt-0.5 block text-[11px] text-zinc-500">{note}</span>
                </li>
              ))}
            </ul>
          </section>

          <section className="space-y-1.5">
            <h3 className="text-xs font-semibold text-zinc-100">Schedule</h3>
            <p>
              Five fields: <code className="font-mono text-orange-300">minute hour day month weekday</code>.{' '}
              <code className="font-mono text-orange-300">*/5 * * * *</code> runs every 5 minutes and{' '}
              <code className="font-mono text-orange-300">0 4 * * *</code> every day at 04:00. The schedule list fills it in for you.
            </p>
          </section>

          <section className="space-y-1.5">
            <h3 className="text-xs font-semibold text-zinc-100">Good to know</h3>
            <ul className="list-disc space-y-1 pl-4">
              <li>Jobs run as your site's own user, so they can only reach your own files.</li>
              <li>Commands start in the document root, so the examples first <code className="font-mono text-zinc-300">cd</code> into the site folder.</li>
              <li><b className="text-zinc-200">Run</b> tests a job now and shows its output.</li>
              {min > 1 && <li>Jobs can run at most once every {min} minutes.</li>}
            </ul>
          </section>
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
