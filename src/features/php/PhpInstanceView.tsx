import React, { useCallback, useEffect, useState } from 'react';
import { BookOpen, Database, Download, Power, RotateCw, Server, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import type { PhpInstanceData } from '../../lib/api/phpApi';
import { ShadcnDialog, ShadcnSwitch } from '../../components/ui/Primitives';
import { MariaDbIcon, PhpIcon, PhpMyAdminIcon } from '../../components/ui/BrandIcons';
import { Card, EmptyState, ErrorBlock, Field, GhostButton, LoadingBlock, Pill, PrimaryButton, ValueBox, inputCls } from './ui';
import { MariaDbTuningDocsModal } from './MariaDbTuningDocsModal';

function snapToBinaryRAM(val: number, maxMB?: number): number {
  if (isNaN(val) || val < 256) return 256;
  const common: Record<number, number> = {
    500: 512,
    1000: 1024,
    1500: 1536,
    2000: 2048,
    3000: 3072,
    4000: 4096,
    5000: 5120,
    6000: 6144,
    8000: 8192,
    12000: 12288,
    16000: 16384,
    24000: 24576,
    32000: 32768,
  };
  let snapped = common[val];
  if (!snapped) {
    const rem = val % 128;
    snapped = rem >= 64 ? val + (128 - rem) : val - rem;
  }
  if (maxMB && snapped > maxMB) {
    snapped = Math.floor(maxMB / 128) * 128 || maxMB;
  }
  return snapped < 256 ? 256 : snapped;
}

export const PhpInstanceView: React.FC<{ onChanged: () => void | Promise<void> }> = ({ onChanged }) => {
  const [data, setData] = useState<PhpInstanceData | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [memory, setMemory] = useState(512);
  const [querySeconds, setQuerySeconds] = useState(60);
  const [showDocs, setShowDocs] = useState(false);
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
      setQuerySeconds(data.mysql.query_seconds);
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
        title={<span className="inline-flex items-center gap-2"><MariaDbIcon className="h-4 w-4" />Database server</span>}
        actions={<Pill tone={mysqlTone}>{mysqlLabel}</Pill>}
      >
        {data.mysql_task.error && (
          <div className="mb-3 rounded-lg border border-red-500/25 bg-red-500/10 px-3 py-2 text-xs text-red-200">{data.mysql_task.error}</div>
        )}
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Image"><ValueBox>{m.image}</ValueBox></Field>
          <Field label="Memory (MB)">
            <input
              type="number"
              min={256}
              max={m.host_memory_mb || undefined}
              step={1}
              className={`${inputCls} !w-32`}
              value={memory}
              onChange={(e) => setMemory(Number(e.target.value))}
              onBlur={() => setMemory((prev) => snapToBinaryRAM(prev, m.host_memory_mb))}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  setMemory((prev) => snapToBinaryRAM(prev, m.host_memory_mb));
                }
              }}
            />
          </Field>
          {!m.running ? (
            <PrimaryButton busy={busy === 'my-on' || data.mysql_task.running} onClick={() => act('my-on', () => backendApi.phpMysql('enable', snapToBinaryRAM(memory, m.host_memory_mb)), 'Starting database server…')}>
              <Power className="h-3.5 w-3.5" /> {m.exists ? 'Start' : 'Set up'} database
            </PrimaryButton>
          ) : (
            <>
              <GhostButton busy={busy === 'my-re' || data.mysql_task.running} onClick={() => act('my-re', () => backendApi.phpMysql('restart', snapToBinaryRAM(memory, m.host_memory_mb)), 'Restarting database server…')}>
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Apply / restart
              </GhostButton>
              <GhostButton tone="danger" busy={busy === 'my-off'} onClick={() => { if (window.confirm('Stop the database server? Sites that use databases will stop working until it is started again.')) void act('my-off', () => backendApi.phpMysql('disable'), 'Database server stopped'); }}>
                Stop
              </GhostButton>
            </>
          )}
        </div>

        {/* Quick Memory Presets */}
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5 text-[11px]">
          <span className="text-[10.5px] text-zinc-500">Quick presets:</span>
          {[512, 1024, 2048, 4096, 8192]
            .filter((p) => !m.host_memory_mb || p <= m.host_memory_mb)
            .map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => setMemory(p)}
                className={`rounded px-2 py-0.5 font-mono text-[10.5px] transition cursor-pointer border ${
                  memory === p
                    ? 'border-orange-500/50 bg-orange-500/20 text-orange-300 font-semibold shadow-sm'
                    : 'border-white/[0.06] bg-zinc-900/60 text-zinc-400 hover:border-white/[0.12] hover:text-zinc-200'
                }`}
              >
                {p >= 1024 ? `${p / 1024} GB` : `${p} MB`}
              </button>
            ))}
          {m.host_memory_mb > 0 && (
            <button
              type="button"
              onClick={() => setMemory(Math.min(4096, Math.max(512, Math.floor((m.host_memory_mb / 4) / 128) * 128)))}
              className="rounded px-2 py-0.5 font-mono text-[10.5px] transition cursor-pointer border border-emerald-500/30 bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20"
              title="Recommended default: 25% of Host RAM"
            >
              Auto (25%)
            </button>
          )}
        </div>

        {/* Live Tuning Bar & Documentation Modal Trigger */}
        <div className="mt-3.5 flex flex-wrap items-center justify-between gap-2.5 rounded-xl border border-white/[0.07] bg-zinc-900/40 p-2.5 px-3">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[11px] text-zinc-400">
            <span className="inline-flex items-center gap-1.5 font-medium text-zinc-300">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
              <span>Buffer Pool: <strong className="font-mono text-emerald-400">{m.buffer_pool_mb} MB</strong> (50%)</span>
            </span>
            <span className="text-zinc-600 hidden sm:inline">·</span>
            <span>Max Conns: <strong className="font-mono text-zinc-200">{m.max_connections}</strong></span>
            <span className="text-zinc-600 hidden sm:inline">·</span>
            <span>Idle Timeout: <strong className="font-mono text-zinc-200">{m.idle_seconds}s</strong></span>
            <span className="text-zinc-600 hidden sm:inline">·</span>
            <span>Statement Limit: <strong className="font-mono text-amber-400">{m.query_seconds > 0 ? `${m.query_seconds}s` : 'Off'}</strong></span>
          </div>

          <button
            type="button"
            onClick={() => setShowDocs(true)}
            className="inline-flex items-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 px-2.5 py-1 text-[11px] font-medium text-orange-400 transition hover:bg-orange-500/20 hover:text-orange-300 cursor-pointer shadow-sm"
          >
            <BookOpen className="h-3.5 w-3.5" />
            <span>Architecture & Tuning Docs (গাইড)</span>
          </button>
        </div>

        {m.exists && (
          <div className="mt-4 flex flex-wrap items-end gap-3 border-t border-white/[0.06] pt-4">
            <Field label="Query time limit (seconds)" hint={m.query_limit_supported ? 'Site query abort threshold (0 = off)' : 'only MariaDB supports per-user limits'}>
              <input
                type="number"
                min={0}
                max={3600}
                step={10}
                disabled={!m.query_limit_supported}
                className={`${inputCls} !w-32`}
                value={querySeconds}
                onChange={(e) => setQuerySeconds(Number(e.target.value))}
              />
            </Field>
            <GhostButton
              busy={busy === 'my-q'}
              disabled={!m.query_limit_supported || querySeconds === m.query_seconds}
              onClick={() => act('my-q', () => backendApi.phpMysqlQueryLimit(querySeconds), 'Query limit saved')}
            >
              Save limit
            </GhostButton>
            <span className="pb-2 text-[11px] text-zinc-500">
              phpMyAdmin gets 5x ({querySeconds * 5}s); panel root and backups are unlimited.
            </span>
          </div>
        )}
      </Card>

      <Card
        title={<span className="inline-flex items-center gap-2"><PhpMyAdminIcon className="h-4 w-4" />phpMyAdmin</span>}
        actions={<Pill tone={data.pma.running ? 'green' : data.pma_task.running ? 'amber' : 'zinc'}>{data.pma.running ? 'running' : data.pma_task.running ? 'starting…' : 'off'}</Pill>}
      >
        {data.pma_task.error && (
          <div className="mb-3 rounded-lg border border-red-500/25 bg-red-500/10 px-3 py-2 text-xs text-red-200">{data.pma_task.error}</div>
        )}
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Image"><ValueBox>{data.pma.image}</ValueBox></Field>
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
        {!m.ready && <p className="mt-3 text-[11px] text-zinc-500">Start the database server first.</p>}
      </Card>

      <Card title={<span className="inline-flex items-center gap-2"><PhpIcon className="h-4 w-4" />PHP versions</span>}>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="text-[11px] text-zinc-500">
              <tr>
                <th className="py-2 pr-4 font-medium">Version</th>
                <th className="py-2 pr-4 font-medium">Security & CVE Status</th>
                <th className="py-2 pr-4 font-medium">Status</th>
                <th className="py-2 pr-4 font-medium">Sites</th>
                <th className="py-2 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {(data.versions ?? []).map((v) => {
                const building = v.build?.running;
                return (
                  <tr key={v.version}>
                    <td className="py-2.5 pr-4 font-mono text-zinc-100 whitespace-nowrap">
                      PHP {v.version}
                      {v.is_default && (
                        <span className="ml-1.5 rounded bg-sky-500/10 px-1 py-0.2 text-[9px] font-sans font-medium text-sky-400 ring-1 ring-sky-500/20">
                          Default
                        </span>
                      )}
                    </td>
                    <td className="py-2.5 pr-4">
                      {v.has_cve ? (
                        <div className="flex flex-col gap-0.5" title={v.cve_warning}>
                          <span className="inline-flex items-center gap-1">
                            <Pill tone="red">⚠️ {v.cves?.[0] || 'CVE Risk'} ({v.cvss || '9.2'})</Pill>
                          </span>
                          <span className="text-[10px] text-rose-400/80 font-sans truncate max-w-[220px]" title={v.cve_warning}>
                            {v.status_label || 'EOL / Critical RCE'}
                          </span>
                        </div>
                      ) : v.status === 'security-only' ? (
                        <div className="flex flex-col gap-0.5" title={v.cve_warning}>
                          <span className="inline-flex items-center gap-1">
                            <Pill tone="amber">Security Only</Pill>
                          </span>
                          <span className="text-[10px] text-zinc-500 font-sans">
                            {v.patched_in ? `Patched ≥ ${v.patched_in}` : `EOL: ${v.eol_date}`}
                          </span>
                        </div>
                      ) : (
                        <div className="flex flex-col gap-0.5">
                          <span className="inline-flex items-center gap-1">
                            <Pill tone="green">{v.status_label || 'Active Support'}</Pill>
                          </span>
                          <span className="text-[10px] text-zinc-500 font-sans">
                            {v.patched_in ? `Patched ≥ ${v.patched_in}` : 'Latest Secure'}
                          </span>
                        </div>
                      )}
                    </td>
                    <td className="py-2.5 pr-4 whitespace-nowrap">
                      {building ? (
                        <Pill tone="amber">pulling / building…</Pill>
                      ) : v.installed ? (
                        <Pill tone="green">installed</Pill>
                      ) : v.build?.error ? (
                        <Pill tone="red">build failed</Pill>
                      ) : (
                        <Pill>not installed</Pill>
                      )}
                    </td>
                    <td className="py-2.5 pr-4 font-mono text-zinc-400">{v.sites}</td>
                    <td className="py-2.5">
                      <div className="flex items-center justify-end gap-1.5 whitespace-nowrap">
                        {v.build && <GhostButton onClick={() => setLogVersion(v.version)}>Build log</GhostButton>}
                        {v.installed ? (
                          <GhostButton tone="danger" disabled={v.sites > 0} title={v.sites > 0 ? 'In use by sites' : 'Remove image'} busy={busy === 'rm' + v.version}
                            onClick={() => { if (window.confirm(`Remove the PHP ${v.version} image?`)) void act('rm' + v.version, () => backendApi.phpVersion(v.version, 'remove'), `PHP ${v.version} removed`); }}>
                            <Trash2 className="h-3.5 w-3.5" /> Remove
                          </GhostButton>
                        ) : (
                          <PrimaryButton busy={busy === 'in' + v.version || !!building} onClick={() => act('in' + v.version, () => backendApi.phpVersion(v.version, 'install'), `Installing PHP ${v.version} (pulling DockerHub image)...`)}>
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

      <Card title="Running services">
        {(data.containers ?? []).length === 0 ? (
          <EmptyState icon={<Server className="h-6 w-6" />} text="Nothing is running yet." />
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
      </Card>

      <Card title="Limits">
        <div className="mb-4 flex flex-wrap items-end gap-3">
          <div>
            <Field label="Minimum cron interval (minutes)">
              <input type="number" min={1} max={1440} className={`${inputCls} !w-32`} value={minCron} onChange={(e) => setMinCron(Number(e.target.value))} />
            </Field>
          </div>
          <GhostButton busy={busy === 'cron'} onClick={() => act('cron', () => backendApi.phpInstanceSettings(minCron), 'Saved')}>Save</GhostButton>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="text-[11px] text-zinc-500">
              <tr><th className="py-2 pr-4 font-medium">User</th><th className="py-2 pr-4 font-medium">Max databases <span className="text-zinc-600">(0 = ∞)</span></th><th className="py-2 pr-4 font-medium">Max cron jobs <span className="text-zinc-600">(0 = ∞)</span></th><th /></tr>
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
      </Card>

      <ShadcnDialog open={logVersion !== null} onClose={() => setLogVersion(null)} title={`PHP ${logVersion} build log`} maxWidth="2xl" footer={<GhostButton onClick={() => setLogVersion(null)}>Close</GhostButton>}>
        {logBuild?.error && <div className="mb-2 text-xs text-red-300">{logBuild.error}</div>}
        <pre className="max-h-[28rem] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[#09090b] p-3 font-mono text-[11px] leading-relaxed text-zinc-300">{logBuild?.log || 'No output yet.'}</pre>
      </ShadcnDialog>

      <MariaDbTuningDocsModal
        open={showDocs}
        onClose={() => setShowDocs(false)}
        mysql={m}
      />
    </div>
  );
};
