import React, { useCallback, useEffect, useState } from 'react';
import { Database, Download, Power, Server, ShieldCheck, Wrench } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import type { PhpInstanceData } from '../../lib/api/phpApi';
import { ShadcnDialog, ShadcnSwitch } from '../../components/ui/Primitives';
import { ErrorBlock, GhostButton, LoadingBlock, MetricStat } from './ui';
import { DatabaseServerCard } from './components/DatabaseServerCard';
import { PhpMyAdminCard } from './components/PhpMyAdminCard';
import { PhpVersionsCard } from './components/PhpVersionsCard';
import { PhpContainersCard } from './components/PhpContainersCard';
import { PhpLimitsCard } from './components/PhpLimitsCard';

interface PhpInstanceViewProps {
  onChanged: () => void | Promise<void>;
}

export const PhpInstanceView: React.FC<PhpInstanceViewProps> = ({ onChanged }) => {
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
    } else {
      setError(res.error || 'Failed to load instance configurations');
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const working =
    !!data && (data.mysql_task?.running || (data.versions ?? []).some((v) => v.build?.running));

  useEffect(() => {
    const t = setInterval(() => void load(), working ? 2500 : 15000);
    return () => clearInterval(t);
  }, [working, load]);

  const seeded = data !== null;
  useEffect(() => {
    if (data) {
      setMemory(data.mysql.memory_mb || 512);
      setMinCron(data.min_cron_interval_minutes || 1);
    }
  }, [seeded]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!data) {
    return error ? <ErrorBlock message={error} onRetry={load} /> : <LoadingBlock label="Loading PHP instance data..." />;
  }

  const act = async (
    key: string,
    fn: () => Promise<{ ok: boolean; error?: string }>,
    okMsg: string
  ) => {
    setBusy(key);
    const res = await fn();
    setBusy('');
    if (res.ok) {
      toast.success(okMsg);
    } else {
      toast.error(res.error || 'Operation failed');
    }
    await load();
    await onChanged();
  };

  const installedCount = (data.versions ?? []).filter((v) => v.installed).length;
  const runningContainers = (data.containers ?? []).filter((c) => c.state === 'running').length;
  const logBuild = (data.versions ?? []).find((v) => v.version === logVersion)?.build;

  return (
    <div className="space-y-6">
      {/* Top Page Header & Main Engine Toggle */}
      <div className="flex flex-col gap-4 rounded-xl border border-white/[0.08] bg-[#111115]/90 p-4 sm:p-5 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-2.5">
            <h1 className="text-xl font-bold tracking-tight text-zinc-100">
              PHP Hosting & Infrastructure
            </h1>
            <span
              className={`rounded-full px-2 py-0.5 font-mono text-[10px] font-semibold uppercase ${
                data.enabled
                  ? 'border border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                  : 'border border-zinc-700 bg-zinc-800 text-zinc-400'
              }`}
            >
              {data.enabled ? 'Active' : 'Disabled'}
            </span>
          </div>
          <p className="mt-1 text-xs text-zinc-400">
            Provision MariaDB database containers, phpMyAdmin access, and manage PHP-FPM runtimes.
          </p>
        </div>

        <div className="flex items-center gap-3 self-start sm:self-auto rounded-lg border border-white/[0.08] bg-black/40 px-3.5 py-2">
          <span className="text-xs font-medium text-zinc-300">
            {data.enabled ? 'Hosting Engine Enabled' : 'Hosting Engine Disabled'}
          </span>
          <ShadcnSwitch
            checked={data.enabled}
            onChange={(v) =>
              void act(
                'module',
                () => backendApi.phpSetModule(v),
                v ? 'PHP hosting enabled' : 'PHP hosting disabled'
              )
            }
          />
        </div>
      </div>

      {/* Top Overview KPI Metrics */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MetricStat
          label="Engine State"
          value={data.enabled ? 'Online' : 'Offline'}
          subtext={data.enabled ? 'Accepting site traffic' : 'Engine turned off'}
          icon={<Server className="h-4 w-4" />}
          tone={data.enabled ? 'emerald' : 'zinc'}
        />
        <MetricStat
          label="MariaDB Database"
          value={data.mysql.ready ? 'Running' : data.mysql.running ? 'Starting' : 'Stopped'}
          subtext={`Port 3306 · ${memory}MB limit`}
          icon={<Database className="h-4 w-4" />}
          tone={data.mysql.ready ? 'emerald' : data.mysql.running ? 'amber' : 'zinc'}
        />
        <MetricStat
          label="phpMyAdmin"
          value={data.pma.running ? 'Ready' : data.pma.enabled ? 'Starting' : 'Disabled'}
          subtext="Single Sign-On Web GUI"
          icon={<Wrench className="h-4 w-4" />}
          tone={data.pma.running ? 'emerald' : data.pma.enabled ? 'amber' : 'zinc'}
        />
        <MetricStat
          label="PHP Runtimes"
          value={`${installedCount} / ${(data.versions ?? []).length}`}
          subtext={`${runningContainers} active containers`}
          icon={<Download className="h-4 w-4" />}
          tone="violet"
        />
      </div>

      {/* Database Server Card */}
      <DatabaseServerCard
        data={data}
        memory={memory}
        busy={busy}
        onMemoryChange={setMemory}
        onAct={act}
        backendApi={backendApi}
      />

      {/* phpMyAdmin Card */}
      <PhpMyAdminCard
        data={data}
        busy={busy}
        onAct={act}
        backendApi={backendApi}
        setBusy={setBusy}
      />

      {/* PHP Versions Management Card */}
      <PhpVersionsCard
        data={data}
        busy={busy}
        onOpenLog={(ver) => setLogVersion(ver)}
        onAct={act}
        backendApi={backendApi}
      />

      {/* Running Containers Card */}
      <PhpContainersCard
        data={data}
        busy={busy}
        onAct={act}
        backendApi={backendApi}
      />

      {/* Limits & Quotas Card */}
      <PhpLimitsCard
        data={data}
        minCron={minCron}
        limits={limits}
        busy={busy}
        onMinCronChange={setMinCron}
        onLimitsChange={setLimits}
        onSaveMinCron={() =>
          act('cron', () => backendApi.phpInstanceSettings(minCron), 'Minimum cron interval updated')
        }
        onSaveUserLimit={async (userId, d, c) => {
          await act(
            'ul' + userId,
            () => backendApi.phpUserLimits(userId, d, c),
            'User quota updated'
          );
          setLimits((l) => {
            const next = { ...l };
            delete next[userId];
            return next;
          });
        }}
      />

      {/* Build Log Dialog */}
      <ShadcnDialog
        open={logVersion !== null}
        onClose={() => setLogVersion(null)}
        title={`PHP ${logVersion} Container Build Log`}
        description="Live compiler and Docker build output for this runtime."
        icon={<Download className="h-4 w-4 text-orange-400" />}
        maxWidth="2xl"
        footer={
          <div className="flex justify-end">
            <GhostButton onClick={() => setLogVersion(null)}>Close</GhostButton>
          </div>
        }
      >
        {logBuild?.error && (
          <div className="mb-3 rounded-lg border border-red-500/25 bg-red-500/10 p-3 text-xs text-red-300">
            {logBuild.error}
          </div>
        )}
        <pre className="max-h-[30rem] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[#09090b] p-3.5 font-mono text-[11px] leading-relaxed text-zinc-300">
          {logBuild?.log || 'No compiler output captured yet.'}
        </pre>
      </ShadcnDialog>
    </div>
  );
};
