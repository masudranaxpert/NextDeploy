import React, { useCallback, useEffect, useState } from 'react';
import { BookOpen, Check, Clock, Database, Download, ExternalLink, Gauge, Power, RotateCw, Server, Shield, Trash2, User, Zap } from 'lucide-react';
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
  const [restartAllBusy, setRestartAllBusy] = useState(false);

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
  const serviceList = (data.containers ?? []).filter((c) => c.kind !== 'route');
  const runningCount = serviceList.filter((c) => c.state === 'running').length;

  const restartAllServices = async () => {
    if (runningCount === 0) return;
    setRestartAllBusy(true);
    try {
      await Promise.all(serviceList.filter((c) => c.state === 'running').map((c) => backendApi.phpRestartContainer(c.name)));
      toast.success(`Restarted ${runningCount} service(s)`);
      await load();
      await onChanged();
    } finally {
      setRestartAllBusy(false);
    }
  };
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
        title={
          <span className="flex items-center gap-2.5">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-sky-500/30 bg-sky-500/10">
              <Database className="h-4 w-4 text-sky-400" />
            </span>
            <span className="text-sm font-semibold text-zinc-100">Database server</span>
          </span>
        }
        actions={
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setShowDocs(true)}
              className="hidden sm:inline-flex h-7 items-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 px-2.5 text-[11px] font-medium text-orange-400 hover:bg-orange-500/20 transition-colors cursor-pointer"
              title="View MariaDB Architecture & Tuning Documentation"
            >
              <BookOpen className="h-3 w-3" />
              <span>Tuning Docs</span>
            </button>
            <Pill tone={mysqlTone}>
              <span aria-hidden="true" className={`h-1.5 w-1.5 rounded-full ${mysqlTone === 'green' ? 'bg-emerald-400' : mysqlTone === 'amber' ? 'bg-amber-400' : 'bg-zinc-500'}`} />
              {mysqlLabel}
            </Pill>
          </div>
        }
      >
        {data.mysql_task.error && (
          <div className="mb-3 rounded-lg border border-red-500/25 bg-red-500/10 px-3 py-2 text-xs text-red-200">{data.mysql_task.error}</div>
        )}
        <div className="grid grid-cols-2 gap-2.5">
          <Field label="Image"><ValueBox>{m.image}</ValueBox></Field>
          <Field label="Memory (MB)">
            <input
              type="number"
              min={256}
              max={m.host_memory_mb || undefined}
              step={1}
              className={inputCls}
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
        </div>

        <div className="mt-3 grid grid-cols-2 gap-2.5 sm:flex sm:items-center">
          {!m.running ? (
            <PrimaryButton className="col-span-2 sm:w-auto" busy={busy === 'my-on' || data.mysql_task.running} onClick={() => act('my-on', () => backendApi.phpMysql('enable', snapToBinaryRAM(memory, m.host_memory_mb)), 'Starting database server…')}>
              <Power className="h-3.5 w-3.5" /> {m.exists ? 'Start' : 'Set up'} database
            </PrimaryButton>
          ) : (
            <>
              <GhostButton className="sm:w-auto" busy={busy === 'my-re' || data.mysql_task.running} onClick={() => act('my-re', () => backendApi.phpMysql('restart', snapToBinaryRAM(memory, m.host_memory_mb)), 'Restarting database server…')}>
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Apply / restart
              </GhostButton>
              <GhostButton className="sm:w-auto" tone="danger" busy={busy === 'my-off'} onClick={() => { if (window.confirm('Stop the database server? Sites that use databases will stop working until it is started again.')) void act('my-off', () => backendApi.phpMysql('disable'), 'Database server stopped'); }}>
                Stop
              </GhostButton>
            </>
          )}
        </div>

        {/* Quick Memory Presets */}
        <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[11px]">
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

        {/* Live Tuning Stats + Documentation */}
        <div className="mt-3.5 rounded-xl border border-white/[0.07] bg-zinc-900/40 p-2.5">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
            <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
              <div className="flex items-center gap-1.5 text-[11px] text-zinc-400">
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                Buffer Pool
              </div>
              <div className="mt-1 font-mono text-xs font-semibold text-emerald-400">
                {m.buffer_pool_mb} MB <span className="font-normal text-zinc-500">(50%)</span>
              </div>
            </div>
            <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
              <div className="text-[11px] text-zinc-400">Max Conns</div>
              <div className="mt-1 font-mono text-xs font-semibold text-zinc-100">{m.max_connections}</div>
            </div>
            <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
              <div className="text-[11px] text-zinc-400">Idle Timeout</div>
              <div className="mt-1 font-mono text-xs font-semibold text-zinc-100">{m.idle_seconds}s</div>
            </div>
            <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
              <div className="text-[11px] text-zinc-400">Statement Limit</div>
              <div className="mt-1 font-mono text-xs font-semibold text-amber-400">
                {m.query_seconds > 0 ? `${m.query_seconds}s` : 'Off'}
              </div>
            </div>
          </div>

          <button
            type="button"
            onClick={() => setShowDocs(true)}
            className="sm:hidden mt-2.5 inline-flex h-9 w-full items-center justify-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 text-xs font-medium text-orange-400 transition hover:bg-orange-500/20 hover:text-orange-300 cursor-pointer"
          >
            <BookOpen className="h-3.5 w-3.5" />
            <span>Architecture &amp; Tuning Docs</span>
          </button>
        </div>

        {m.exists && (
          <div className="mt-4 border-t border-white/[0.06] pt-4">
            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-[13px] font-medium text-zinc-200">Query time limit (seconds)</span>
                  <span className="text-[11px] text-zinc-500 font-mono">
                    {m.query_limit_supported ? '(0 = off)' : 'N/A'}
                  </span>
                </div>
                <p className="mt-1 text-[11px] leading-snug text-zinc-500">
                  {m.query_limit_supported ? `Site query abort threshold (${querySeconds}s active). ` : ''}
                  phpMyAdmin gets 5× ({querySeconds * 5}s); panel root and backups are unlimited.
                </p>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <input
                  type="number"
                  min={0}
                  max={3600}
                  step={10}
                  disabled={!m.query_limit_supported}
                  className={`${inputCls} !w-24 text-right font-mono`}
                  value={querySeconds}
                  onChange={(e) => setQuerySeconds(Number(e.target.value))}
                />
                <GhostButton
                  busy={busy === 'my-q'}
                  disabled={!m.query_limit_supported || querySeconds === m.query_seconds}
                  onClick={() => act('my-q', () => backendApi.phpMysqlQueryLimit(querySeconds), 'Query limit saved')}
                  className="!border-emerald-500/30 !bg-emerald-500/10 !text-emerald-300 hover:!bg-emerald-500/20 shrink-0"
                >
                  Save limit
                </GhostButton>
              </div>
            </div>
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
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2.5 min-w-0">
            <span className="text-xs font-medium text-zinc-400 shrink-0">Image:</span>
            <code className="rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 py-1.5 font-mono text-xs text-zinc-300 truncate">
              {data.pma.image}
            </code>
          </div>

          {!data.pma.enabled ? (
            <PrimaryButton
              disabled={!m.ready}
              busy={busy === 'pma-on' || data.pma_task.running}
              onClick={() => act('pma-on', () => backendApi.phpPma('enable'), 'Starting phpMyAdmin…')}
            >
              <Power className="h-3.5 w-3.5" /> Enable phpMyAdmin
            </PrimaryButton>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                disabled={busy === 'pma-open'}
                onClick={async () => {
                  setBusy('pma-open');
                  const r = await backendApi.phpPmaOpenAll();
                  setBusy('');
                  if (r.ok && r.data?.url) window.open(r.data.url, '_blank', 'noopener');
                  else toast.error(r.error || 'Could not open phpMyAdmin');
                }}
                className="inline-flex h-8.5 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer flex-1 sm:flex-initial"
              >
                <ExternalLink className="h-3.5 w-3.5 text-orange-400" />
                <span>Open (all databases)</span>
              </button>

              <button
                type="button"
                disabled={busy === 'pma-re' || data.pma_task.running}
                onClick={() => act('pma-re', () => backendApi.phpPma('restart'), 'Restarting phpMyAdmin…')}
                className="inline-flex h-8.5 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer shrink-0"
              >
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
                <span>Restart</span>
              </button>

              <button
                type="button"
                disabled={busy === 'pma-off'}
                onClick={() => void act('pma-off', () => backendApi.phpPma('disable'), 'phpMyAdmin stopped')}
                className="inline-flex h-8.5 items-center justify-center rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 text-xs font-medium text-rose-300 hover:bg-rose-500/20 transition-colors cursor-pointer shrink-0"
              >
                Disable
              </button>
            </div>
          )}
        </div>
        {!m.ready && <p className="mt-3 text-[11px] text-zinc-500">Start the database server first.</p>}
      </Card>

      {/* Redis In-Memory Cache Card */}
      <Card
        title={
          <span className="inline-flex items-center gap-2">
            <Zap className="h-4 w-4 text-rose-500" />
            Redis In-Memory Cache
          </span>
        }
        actions={
          <Pill tone={data.redis?.running ? 'green' : 'zinc'}>
            <span
              aria-hidden="true"
              className={`h-1.5 w-1.5 rounded-full ${
                data.redis?.running ? 'bg-emerald-400' : 'bg-zinc-500'
              }`}
            />
            {data.redis?.running ? 'running' : data.redis?.exists ? 'stopped' : 'not setup'}
          </Pill>
        }
      >
        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
            <Field label="Host / Container">
              <ValueBox>{data.redis?.host || 'nd-redis'}</ValueBox>
            </Field>
            <Field label="Port">
              <ValueBox>{String(data.redis?.port || 6379)}</ValueBox>
            </Field>
            <Field label="Image">
              <ValueBox>{data.redis?.image || 'redis:7-alpine'}</ValueBox>
            </Field>
          </div>

          {data.redis?.running && (
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 pt-1">
              <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
                <div className="text-[11px] text-zinc-400">Used Memory</div>
                <div className="mt-1 font-mono text-xs font-semibold text-emerald-400">
                  {data.redis.used_memory_human || '0B'}
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
                <div className="text-[11px] text-zinc-400">Connected Clients</div>
                <div className="mt-1 font-mono text-xs font-semibold text-zinc-100">
                  {data.redis.connected_clients ?? 0}
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
                <div className="text-[11px] text-zinc-400">Version</div>
                <div className="mt-1 font-mono text-xs font-semibold text-zinc-300">
                  {data.redis.version || '7.x'}
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.05] bg-[#09090b] p-2.5">
                <div className="text-[11px] text-zinc-400">Uptime</div>
                <div className="mt-1 font-mono text-xs font-semibold text-zinc-300">
                  {data.redis.uptime_seconds ? `${Math.floor(data.redis.uptime_seconds / 60)}m` : '0m'}
                </div>
              </div>
            </div>
          )}

          {/* PHP / WordPress Connection Snippet */}
          <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3 text-xs">
            <div className="text-zinc-300 font-medium">PHP &amp; WordPress Connection</div>
            <p className="mt-0.5 text-[11px] text-zinc-500">
              Every PHP-FPM container is linked on the internal network. Connect with host{' '}
              <code className="text-orange-300 font-mono">nd-redis</code> and port{' '}
              <code className="text-orange-300 font-mono">6379</code>. Compatible with WordPress Redis Object Cache plugin.
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2 pt-1">
            {!data.redis?.running ? (
              <PrimaryButton
                busy={busy === 'redis-on'}
                onClick={() => act('redis-on', () => backendApi.phpRedis('enable'), 'Starting Redis cache…')}
              >
                <Power className="h-3.5 w-3.5" /> {data.redis?.exists ? 'Start Redis' : 'Enable Redis Server'}
              </PrimaryButton>
            ) : (
              <>
                <GhostButton
                  busy={busy === 'redis-re'}
                  onClick={() => act('redis-re', () => backendApi.phpRedis('restart'), 'Restarting Redis cache…')}
                >
                  <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Restart
                </GhostButton>
                <GhostButton
                  tone="danger"
                  busy={busy === 'redis-off'}
                  onClick={() => {
                    if (window.confirm('Stop Redis? Sites utilizing Redis cache or sessions will fail back to disk/database.')) {
                      void act('redis-off', () => backendApi.phpRedis('disable'), 'Redis stopped');
                    }
                  }}
                >
                  Stop Redis
                </GhostButton>
              </>
            )}
          </div>
        </div>
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

      <Card
        title={<span className="text-sm font-semibold text-zinc-100">Running services</span>}
        actions={
          <span className="flex items-center gap-2">
            <Pill tone="green">
              <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
              {runningCount}/{(data.containers ?? []).length}
            </Pill>
            <GhostButton busy={restartAllBusy} onClick={restartAllServices} disabled={runningCount === 0}>
              <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Restart All
            </GhostButton>
          </span>
        }
      >
        {(data.containers ?? []).length === 0 ? (
          <EmptyState icon={<Server className="h-6 w-6" />} text="Nothing is running yet." />
        ) : (
          <ul className="space-y-2.5">
            {(data.containers ?? []).filter((c) => c.kind !== 'route').map((c) => (
              <li key={c.name} className="space-y-2">
                <div className="flex min-w-0 items-center gap-2.5">
                  <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border ${c.kind === 'mysql' ? 'border-sky-500/30 bg-sky-500/10' : 'border-violet-500/30 bg-violet-500/10'}`}>
                    {c.kind === 'mysql' ? <Database className="h-4 w-4 text-sky-400" /> : <Server className="h-4 w-4 text-violet-400" />}
                  </span>
                  <span className="min-w-0 flex-1 truncate font-mono text-xs font-semibold text-zinc-100">{c.name}</span>
                  <Pill tone={c.state === 'running' ? 'green' : 'red'}>
                    <span aria-hidden="true" className={`h-1.5 w-1.5 rounded-full ${c.state === 'running' ? 'bg-emerald-400' : 'bg-rose-400'}`} />
                    {c.state}
                  </Pill>
                  <GhostButton busy={busy === 'rs' + c.name} onClick={() => act('rs' + c.name, () => backendApi.phpRestartContainer(c.name), `${c.name} restarted`)} title={`Restart ${c.name}`} className="!h-9 !w-9 !px-0">
                    <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
                  </GhostButton>
                </div>
                <div className="flex items-center justify-between gap-2 text-[11px]">
                  <span className="min-w-0 truncate font-mono text-zinc-500">{c.image}</span>
                  <span className="shrink-0 font-mono text-zinc-500">{c.status}</span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <Card
        title={
          <div className="flex items-center gap-2">
            <span className="flex h-6 w-6 items-center justify-center rounded-md border border-orange-500/25 bg-orange-500/10 text-orange-400">
              <Gauge className="h-3.5 w-3.5" />
            </span>
            <span>Instance Limits & Quotas</span>
          </div>
        }
        description="Global execution thresholds and per-user database and cron job allocation."
      >
        <div className="space-y-4">
          {/* Minimum Cron Frequency Setting */}
          <div className="flex flex-col gap-3 rounded-xl border border-white/[0.08] bg-zinc-950/60 p-3 sm:flex-row sm:items-center sm:justify-between sm:p-3.5">
            <div className="flex items-start gap-3">
              <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-amber-500/25 bg-amber-500/10 text-amber-400">
                <Clock className="h-4 w-4" />
              </div>
              <div>
                <div className="text-xs font-semibold text-zinc-100">Minimum Cron Interval</div>
                <p className="mt-0.5 text-[11px] leading-relaxed text-zinc-500">
                  Global minimum execution frequency across all user cron jobs to protect CPU stability.
                </p>
              </div>
            </div>

            <div className="flex items-center gap-2 self-end sm:self-auto shrink-0">
              <div className="flex h-8 items-center rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 focus-within:border-amber-500/50">
                <input
                  type="number"
                  min={1}
                  max={1440}
                  className="w-14 bg-transparent font-mono text-xs font-medium text-zinc-100 text-right focus:outline-none [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
                  value={minCron}
                  onChange={(e) => setMinCron(Number(e.target.value))}
                />
                <span className="ml-1.5 font-mono text-[11px] text-zinc-500">min</span>
              </div>
              <button
                type="button"
                disabled={minCron === data.min_cron_interval_minutes || busy === 'cron'}
                onClick={() => act('cron', () => backendApi.phpInstanceSettings(minCron), 'Saved cron interval')}
                className={`inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-xs font-semibold transition-all cursor-pointer disabled:cursor-not-allowed disabled:opacity-40 ${
                  minCron !== data.min_cron_interval_minutes
                    ? 'bg-orange-500 text-white shadow-lg shadow-orange-500/20 hover:bg-orange-600'
                    : 'border border-white/[0.08] bg-zinc-900 text-zinc-400'
                }`}
              >
                {busy === 'cron' ? (
                  <span className="h-3 w-3 animate-spin rounded-full border border-white border-t-transparent" />
                ) : minCron !== data.min_cron_interval_minutes ? (
                  <span>Save</span>
                ) : (
                  <>
                    <Check className="h-3.5 w-3.5 text-emerald-400" />
                    <span>Saved</span>
                  </>
                )}
              </button>
            </div>
          </div>

          {/* User Resource Quotas */}
          <div className="space-y-2.5">
            <div className="flex items-center justify-between px-1">
              <div className="text-xs font-medium text-zinc-300">User Resource Quotas</div>
              <div className="text-[11px] text-zinc-500 font-mono">
                Set to <span className="text-zinc-300 font-semibold">0</span> for unlimited (<span className="text-cyan-400 font-bold">∞</span>)
              </div>
            </div>

            <div className="space-y-2">
              {(data.user_limits ?? []).map((u) => {
                const cur = limits[u.user_id] ?? { d: u.max_databases, c: u.max_cron_jobs };
                const dirty = cur.d !== u.max_databases || cur.c !== u.max_cron_jobs;

                return (
                  <div
                    key={u.user_id}
                    className={`rounded-xl border p-3 transition-colors ${
                      dirty
                        ? 'border-orange-500/40 bg-orange-500/[0.04]'
                        : 'border-white/[0.07] bg-zinc-950/60 hover:border-white/[0.12]'
                    }`}
                  >
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                      {/* User Identity */}
                      <div className="flex items-center gap-2.5 min-w-0">
                        <div
                          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border ${
                            u.role === 'admin'
                              ? 'border-orange-500/30 bg-orange-500/10 text-orange-400'
                              : 'border-indigo-500/30 bg-indigo-500/10 text-indigo-400'
                          }`}
                        >
                          {u.role === 'admin' ? <Shield className="h-4 w-4" /> : <User className="h-4 w-4" />}
                        </div>
                        <div className="min-w-0">
                          <div className="flex items-center gap-2">
                            <span className="truncate font-semibold text-xs text-zinc-100">{u.username}</span>
                            {u.role === 'admin' && (
                              <span className="inline-flex items-center gap-1 rounded-md border border-orange-500/30 bg-orange-500/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-orange-400">
                                Admin · Unlimited
                              </span>
                            )}
                          </div>
                          <span className="text-[11px] text-zinc-500 font-mono">ID #{u.user_id}</span>
                        </div>
                      </div>

                      {/* Quotas & Action */}
                      <div className="flex flex-wrap items-center justify-between gap-2.5 sm:justify-end">
                        {/* Databases Input Group */}
                        <div
                          className="flex h-8 items-center gap-2 rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 transition-colors focus-within:border-cyan-500/50"
                          title="Max databases (0 = unlimited)"
                        >
                          <Database className="h-3.5 w-3.5 text-cyan-400 shrink-0" />
                          <span className="text-[11px] text-zinc-400">DBs:</span>
                          <input
                            type="number"
                            min={0}
                            value={cur.d}
                            onChange={(e) =>
                              setLimits({
                                ...limits,
                                [u.user_id]: { ...cur, d: Math.max(0, Number(e.target.value)) },
                              })
                            }
                            className="w-12 bg-transparent font-mono text-xs font-semibold text-zinc-100 text-right focus:outline-none [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
                          />
                          <span className="text-[11px] font-mono text-zinc-500 w-4 text-center">
                            {cur.d === 0 ? <span className="font-bold text-cyan-400">∞</span> : 'max'}
                          </span>
                        </div>

                        {/* Cron Jobs Input Group */}
                        <div
                          className="flex h-8 items-center gap-2 rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 transition-colors focus-within:border-amber-500/50"
                          title="Max cron jobs (0 = unlimited)"
                        >
                          <Clock className="h-3.5 w-3.5 text-amber-400 shrink-0" />
                          <span className="text-[11px] text-zinc-400">Crons:</span>
                          <input
                            type="number"
                            min={0}
                            value={cur.c}
                            onChange={(e) =>
                              setLimits({
                                ...limits,
                                [u.user_id]: { ...cur, c: Math.max(0, Number(e.target.value)) },
                              })
                            }
                            className="w-12 bg-transparent font-mono text-xs font-semibold text-zinc-100 text-right focus:outline-none [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
                          />
                          <span className="text-[11px] font-mono text-zinc-500 w-4 text-center">
                            {cur.c === 0 ? <span className="font-bold text-amber-400">∞</span> : 'max'}
                          </span>
                        </div>

                        {/* Save Action */}
                        <button
                          type="button"
                          disabled={!dirty || busy === 'ul' + u.user_id}
                          onClick={async () => {
                            await act(
                              'ul' + u.user_id,
                              () => backendApi.phpUserLimits(u.user_id, cur.d, cur.c),
                              `Limits saved for ${u.username}`
                            );
                            setLimits((l) => {
                              const n = { ...l };
                              delete n[u.user_id];
                              return n;
                            });
                          }}
                          className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-3 text-xs font-semibold transition-all cursor-pointer disabled:cursor-not-allowed ${
                            dirty
                              ? 'bg-orange-500 text-white shadow-lg shadow-orange-500/20 hover:bg-orange-600'
                              : 'border border-white/[0.06] bg-zinc-900/40 text-zinc-600 opacity-60'
                          }`}
                        >
                          {busy === 'ul' + u.user_id ? (
                            <span className="h-3 w-3 animate-spin rounded-full border border-white border-t-transparent" />
                          ) : dirty ? (
                            <span>Save</span>
                          ) : (
                            <span className="text-[11px]">Saved</span>
                          )}
                        </button>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
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
