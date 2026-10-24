import React, { useCallback, useEffect, useState } from 'react';
import { Database, Download, Power, RotateCw, Server, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import type { PhpInstanceData } from '../../lib/api/phpApi';
import { ShadcnDialog, ShadcnSwitch } from '../../components/ui/Primitives';
import { Card, ErrorBlock, GhostButton, LoadingBlock, Pill, PrimaryButton, inputCls } from './ui';

export const PhpInstanceView: React.FC<{ onChanged: () => void | Promise<void> }> = ({ onChanged }) => {
  const [data, setData] = useState<PhpInstanceData | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [memory, setMemory] = useState(512);
  const [minCron, setMinCron] = useState(1);
  const [logVersion, setLogVersion] = useState<string | null>(null);
  const [limits, setLimits] = useState<Record<number, { d: number; c: number }>>({});

  const load = useCallback(async () => {
    const res = await backendApi.phpInstance();
    if (res.ok && res.data) {
      const d = res.data as PhpInstanceData;
      setData(d);
      setError('');
    } else setError(res.error || 'Failed to load');
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const working =
    !!data && (data.mysql_task.running || (data.versions ?? []).some((v) => v.build?.running));
  useEffect(() => {
    const t = setInterval(() => void load(), working ? 2500 : 15000);
    return () => clearInterval(t);
  }, [working, load]);

  const seeded = data !== null;
  useEffect(() => {
    if (data) {
      setMemory(data.mysql.memory_mb);
      setMinCron(data.min_cron_interval_minutes);
    }
  }, [seeded]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!data) return error ? <ErrorBlock message={error} onRetry={load} /> : <LoadingBlock />;

  const act = async (key: string, fn: () => Promise<{ ok: boolean; error?: string }>, okMsg: string) => {
    setBusy(key);
    const res = await fn();
    setBusy('');
    if (res.ok) toast.success(okMsg);
    else toast.error(res.error || 'Action failed');
    await load();
    await onChanged();
  };

  const m = data.mysql;
  const mysqlTone = m.ready ? 'green' : m.running ? 'amber' : data.mysql_task.running ? 'amber' : 'zinc';
  const mysqlLabel = m.ready ? 'running' : data.mysql_task.running ? 'starting…' : m.running ? 'starting' : m.exists ? 'stopped' : 'not set up';
  const logBuild = (data.versions ?? []).find((v) => v.version === logVersion)?.build;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-zinc-100">PHP Hosting · Instance</h1>
          <p className="mt-0.5 text-xs text-zinc-400">Choose which PHP versions and database server are available to your users.</p>
        </div>
        <label className="inline-flex items-center gap-2 text-xs text-zinc-300">
          <ShadcnSwitch
            checked={data.enabled}
            onChange={(v) => void act('module', () => backendApi.phpSetModule(v), v ? 'PHP hosting enabled' : 'PHP hosting disabled')}
          />
          {data.enabled ? 'Enabled' : 'Disabled'}
        </label>
      </div>

      <Card
        title="Database server"
        description="One shared MariaDB container. Every site gets its own databases and users."
        actions={<Pill tone={mysqlTone}>{mysqlLabel}</Pill>}
      >
        {data.mysql_task.error && (
          <div className="mb-3 rounded-lg border border-red-500/25 bg-red-500/10 px-3 py-2 text-xs text-red-200">{data.mysql_task.error}</div>
        )}
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <div className="mb-1 text-[11px] text-zinc-400">Image</div>
            <code className="rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 py-1.5 font-mono text-[11px] text-zinc-300">{m.image}</code>
          </div>
          <div>
            <label className="mb-1 block text-[11px] text-zinc-400">Memory limit (MB)</label>
            <input type="number" min={256} step={128} className={`${inputCls} !w-28`} value={memory} onChange={(e) => setMemory(Number(e.target.value))} />
          </div>
          {!m.running ? (
            <PrimaryButton busy={busy === 'my-on' || data.mysql_task.running} onClick={() => act('my-on', () => backendApi.phpMysql('enable', memory), 'Starting database server…')}>
              <Power className="h-3.5 w-3.5" /> {m.exists ? 'Start' : 'Set up'} database
            </PrimaryButton>
          ) : (
            <>
              <GhostButton busy={busy === 'my-re' || data.mysql_task.running} onClick={() => act('my-re', () => backendApi.phpMysql('restart', memory), 'Restarting database server…')}>
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Apply / restart
              </GhostButton>
              <GhostButton tone="danger" busy={busy === 'my-off'} onClick={() => { if (window.confirm('Stop the database server? Sites that use databases will stop working until it is started again.')) void act('my-off', () => backendApi.phpMysql('disable'), 'Database server stopped'); }}>
                Stop
              </GhostButton>
            </>
          )}
        </div>
        <p className="mt-3 text-[11px] text-zinc-500">Data is kept in the Docker volume “nd-mysql-data” and survives restarts and upgrades.</p>
      </Card>

      <Card
        title="phpMyAdmin"
        description="Official phpMyAdmin image. Users open it from their site with one click — no separate login. Each user only sees the databases of that site; you can open every database."
        actions={<Pill tone={data.pma.running ? 'green' : data.pma_task.running ? 'amber' : 'zinc'}>{data.pma.running ? 'running' : data.pma_task.running ? 'starting…' : 'off'}</Pill>}
      >
        {data.pma_task.error && (
          <div className="mb-3 rounded-lg border border-red-500/25 bg-red-500/10 px-3 py-2 text-xs text-red-200">{data.pma_task.error}</div>
        )}
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <div className="mb-1 text-[11px] text-zinc-400">Image</div>
            <code className="rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 py-1.5 font-mono text-[11px] text-zinc-300">{data.pma.image}</code>
          </div>
          {!data.pma.enabled ? (
            <PrimaryButton disabled={!m.ready} busy={busy === 'pma-on' || data.pma_task.running} onClick={() => act('pma-on', () => backendApi.phpPma('enable'), 'Starting phpMyAdmin…')}>
              <Power className="h-3.5 w-3.5" /> Enable phpMyAdmin
            </PrimaryButton>
          ) : (
            <>
              <GhostButton
                busy={busy === 'pma-open'}
                onClick={async () => {
                  setBusy('pma-open');
                  const r = await backendApi.phpPmaOpenAll();
                  setBusy('');
                  if (r.ok && r.data?.url) window.open(r.data.url, '_blank', 'noopener');
                  else toast.error(r.error || 'Could not open phpMyAdmin');
                }}
              >
                Open (all databases)
              </GhostButton>
              <GhostButton busy={busy === 'pma-re' || data.pma_task.running} onClick={() => act('pma-re', () => backendApi.phpPma('restart'), 'Restarting phpMyAdmin…')}>
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Restart
              </GhostButton>
              <GhostButton tone="danger" busy={busy === 'pma-off'} onClick={() => void act('pma-off', () => backendApi.phpPma('disable'), 'phpMyAdmin stopped')}>
                Disable
              </GhostButton>
            </>
          )}
        </div>
        {!m.ready && <p className="mt-3 text-[11px] text-zinc-500">Set up the database server first.</p>}
      </Card>

      <Card title="PHP versions" description="Each version is one Docker image shared by all sites using it. Install only what you need.">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="text-[11px] text-zinc-500">
              <tr><th className="py-2 pr-4 font-medium">Version</th><th className="py-2 pr-4 font-medium">Status</th><th className="py-2 pr-4 font-medium">Sites</th><th className="py-2 text-right font-medium">Actions</th></tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {(data.versions ?? []).map((v) => {
                const building = v.build?.running;
                return (
                  <tr key={v.version}>
                    <td className="py-2.5 pr-4 font-mono text-zinc-100">PHP {v.version}</td>
                    <td className="py-2.5 pr-4">
                      {building ? <Pill tone="amber">building…</Pill> : v.installed ? <Pill tone="green">installed</Pill> : v.build?.error ? <Pill tone="red">build failed</Pill> : <Pill>not installed</Pill>}
                    </td>
                    <td className="py-2.5 pr-4 font-mono text-zinc-400">{v.sites}</td>
                    <td className="py-2.5">
                      <div className="flex items-center justify-end gap-1.5">
                        {v.build && <GhostButton onClick={() => setLogVersion(v.version)}>Build log</GhostButton>}
                        {v.installed ? (
                          <GhostButton tone="danger" disabled={v.sites > 0} title={v.sites > 0 ? 'In use by sites' : 'Remove image'} busy={busy === 'rm' + v.version}
                            onClick={() => { if (window.confirm(`Remove the PHP ${v.version} image?`)) void act('rm' + v.version, () => backendApi.phpVersion(v.version, 'remove'), `PHP ${v.version} removed`); }}>
                            <Trash2 className="h-3.5 w-3.5" /> Remove
                          </GhostButton>
                        ) : (
                          <PrimaryButton busy={busy === 'in' + v.version || !!building} onClick={() => act('in' + v.version, () => backendApi.phpVersion(v.version, 'install'), `Building PHP ${v.version}. This takes a few minutes`)}>
                            <Download className="h-3.5 w-3.5" /> Install
                          </PrimaryButton>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Card>

      <Card title="Running services" description="Containers managed by PHP hosting.">
        {(data.containers ?? []).length === 0 ? (
          <p className="text-xs text-zinc-400">Nothing is running yet.</p>
        ) : (
          <ul className="divide-y divide-white/[0.05]">
            {(data.containers ?? []).filter((c) => c.kind !== 'route').map((c) => (
              <li key={c.name} className="flex flex-wrap items-center justify-between gap-3 py-2.5">
                <div className="flex min-w-0 items-center gap-2.5">
                  {c.kind === 'mysql' ? <Database className="h-4 w-4 text-sky-400" /> : <Server className="h-4 w-4 text-violet-400" />}
                  <div className="min-w-0">
                    <div className="truncate font-mono text-xs text-zinc-100">{c.name}</div>
                    <div className="truncate text-[11px] text-zinc-500">{c.image} · {c.status}</div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Pill tone={c.state === 'running' ? 'green' : 'red'}>{c.state}</Pill>
                  <GhostButton busy={busy === 'rs' + c.name} onClick={() => act('rs' + c.name, () => backendApi.phpRestartContainer(c.name), `${c.name} restarted`)}>
                    <RotateCw className="h-3.5 w-3.5" />
                  </GhostButton>
                </div>
              </li>
            ))}
          </ul>
        )}
        <p className="mt-3 text-[11px] text-zinc-500">{(data.containers ?? []).filter((c) => c.kind === 'route').length} lightweight route holder container(s) carry the domain routes of your sites.</p>
      </Card>

      <Card title="Limits" description="Cron jobs and per-user quotas. Memory, CPU, storage and site count come from Team & Quotas.">
        <div className="mb-4 flex flex-wrap items-end gap-3">
          <div>
            <label className="mb-1 block text-[11px] text-zinc-400">Shortest cron interval for non-admin users (minutes)</label>
            <input type="number" min={1} max={1440} className={`${inputCls} !w-28`} value={minCron} onChange={(e) => setMinCron(Number(e.target.value))} />
          </div>
          <GhostButton busy={busy === 'cron'} onClick={() => act('cron', () => backendApi.phpInstanceSettings(minCron), 'Saved')}>Save</GhostButton>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="text-[11px] text-zinc-500">
              <tr><th className="py-2 pr-4 font-medium">User</th><th className="py-2 pr-4 font-medium">Max databases</th><th className="py-2 pr-4 font-medium">Max cron jobs</th><th /></tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {(data.user_limits ?? []).map((u) => {
                const cur = limits[u.user_id] ?? { d: u.max_databases, c: u.max_cron_jobs };
                const dirty = cur.d !== u.max_databases || cur.c !== u.max_cron_jobs;
                return (
                  <tr key={u.user_id}>
                    <td className="py-2 pr-4 text-zinc-100">{u.username} {u.role === 'admin' && <Pill tone="orange">admin · unlimited</Pill>}</td>
                    <td className="py-2 pr-4"><input type="number" min={0} className={`${inputCls} !w-24`} value={cur.d} onChange={(e) => setLimits({ ...limits, [u.user_id]: { ...cur, d: Number(e.target.value) } })} /></td>
                    <td className="py-2 pr-4"><input type="number" min={0} className={`${inputCls} !w-24`} value={cur.c} onChange={(e) => setLimits({ ...limits, [u.user_id]: { ...cur, c: Number(e.target.value) } })} /></td>
                    <td className="py-2 text-right">
                      {dirty && <PrimaryButton busy={busy === 'ul' + u.user_id} onClick={async () => { await act('ul' + u.user_id, () => backendApi.phpUserLimits(u.user_id, cur.d, cur.c), 'Limits saved'); setLimits((l) => { const n = { ...l }; delete n[u.user_id]; return n; }); }}>Save</PrimaryButton>}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <p className="mt-2 text-[11px] text-zinc-500">0 means unlimited.</p>
      </Card>

      <ShadcnDialog open={logVersion !== null} onClose={() => setLogVersion(null)} title={`PHP ${logVersion} build log`} maxWidth="2xl" footer={<GhostButton onClick={() => setLogVersion(null)}>Close</GhostButton>}>
        {logBuild?.error && <div className="mb-2 text-xs text-red-300">{logBuild.error}</div>}
        <pre className="max-h-[28rem] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[#09090b] p-3 font-mono text-[11px] leading-relaxed text-zinc-300">{logBuild?.log || 'No output yet.'}</pre>
      </ShadcnDialog>
    </div>
  );
};
