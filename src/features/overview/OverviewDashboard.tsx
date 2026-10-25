import { OwnerBadge } from '../../components/ui/OwnerBadge';
import React, { useState } from 'react';
import { GitBranch, Server } from 'lucide-react';
import { NavigationSection, TelemetryPoint } from '../../types/nextdeploy';
import { EvilAreaChart, StatusBadge } from '../../components/ui/Primitives';

interface OverviewDashboardProps {
  telemetry: TelemetryPoint[];
  sys?: any;
  apps?: any[];
  containersCount?: number;
  /** Host telemetry and system daemons are shown to admins only. */
  isAdmin?: boolean;
  /** The signed-in user's own limits and live usage (shown instead of host telemetry for non-admins). */
  quota?: {
    max_apps: number;
    apps_used: number;
    max_memory_mb: number;
    memory_used_mb: number;
    max_cpus: number;
    cpu_used_cores: number;
    max_storage_mb: number;
    storage_used_mb: number;
    storage_used_bytes?: number;
    storage_breakdown?: { files: number; databases: number; volumes: number };
  } | null;
  /** Host services with their real state (admins only). */
  services?: Array<{ name: string; ver: string; meta: string; ok: boolean }>;
  onSelectApp: (appId: string) => void;
  onSelectNav: (nav: NavigationSection) => void;
}

export const OverviewDashboard: React.FC<OverviewDashboardProps> = ({
  telemetry,
  sys,
  apps = [],
  containersCount = 0,
  isAdmin = true,
  quota = null,
  services = [],
  onSelectApp,
  onSelectNav,
}) => {
  const [chartMetric, setChartMetric] = useState<'requests' | 'cpu-mem' | 'network'>('requests');

  const memUsedGB = sys?.MemUsedGB != null ? sys.MemUsedGB.toFixed(1) : '0.0';
  const memTotalGB = sys?.MemTotalGB != null ? Math.round(sys.MemTotalGB) : 0;
  const memPct = sys?.MemUsedPct != null ? Math.round(sys.MemUsedPct) : 0;

  const diskUsedGB = sys?.DiskUsedGB != null ? sys.DiskUsedGB.toFixed(1) : '0.0';
  const diskTotalGB = sys?.DiskTotalGB != null ? Math.round(sys.DiskTotalGB) : 0;
  const diskPct = sys?.DiskUsedPct != null ? Math.round(sys.DiskUsedPct) : 0;

  const cpuPct = sys?.CPUPercent != null ? Math.round(sys.CPUPercent) : (telemetry[telemetry.length - 1]?.cpu || 0);
  const numCPU = sys?.NumCPU || 1;

  const recentDeployments = apps.flatMap((a) =>
    (a.deployments || []).map((d: any) => ({
      ...d,
      appId: a.id,
      appName: a.name,
      appOwner: a.owner,
      appOwnerId: a.ownerId,
      appOwnerRole: a.ownerRole,
    }))
  ).slice(0, 5);

  const hostOnly = isAdmin ? '' : 'hidden';
  const fmtMB = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(mb % 1024 === 0 ? 0 : 1)} GB` : `${Math.round(mb)} MB`);
  const fmtBytes = (b: number) =>
    b >= 1024 ** 3 ? `${(b / 1024 ** 3).toFixed(1)} GB` : b >= 1024 ** 2 ? `${(b / 1024 ** 2).toFixed(1)} MB` : b >= 1024 ? `${Math.round(b / 1024)} KB` : `${b} B`;
  const storageNote = quota?.storage_breakdown
    ? `files ${fmtBytes(quota.storage_breakdown.files)} · databases ${fmtBytes(quota.storage_breakdown.databases)}${
        quota.storage_breakdown.volumes > 0 ? ` · volumes ${fmtBytes(quota.storage_breakdown.volumes)}` : ''
      }`
    : '';
  const fmtPct = (n: number) => `${n < 10 ? n.toFixed(2) : n.toFixed(1)}%`;
  const pct = (used: number, max: number) => (max > 0 ? Math.min(100, Math.round((used / max) * 100)) : 0);
  const myCards =
    !isAdmin && quota
      ? [
          { label: 'Your memory', value: fmtMB(quota.memory_used_mb), limit: fmtMB(quota.max_memory_mb), p: pct(quota.memory_used_mb, quota.max_memory_mb) },
          quota.max_cpus > 0
            ? {
                label: 'Your CPU',
                value: `${fmtPct((quota.cpu_used_cores / quota.max_cpus) * 100)}`,
                limit: `of ${quota.max_cpus} ${quota.max_cpus === 1 ? 'core' : 'cores'}`,
                p: pct(quota.cpu_used_cores, quota.max_cpus),
                note: `${quota.cpu_used_cores.toFixed(3)} cores · average of the last minute`,
              }
            : { label: 'Your CPU', value: `${quota.cpu_used_cores.toFixed(2)} cores`, limit: 'no limit', p: 0 },
          { label: 'Your storage', value: fmtBytes(quota.storage_used_bytes ?? quota.storage_used_mb * 1024 * 1024), limit: fmtMB(quota.max_storage_mb), p: pct(quota.storage_used_bytes ?? quota.storage_used_mb * 1024 * 1024, quota.max_storage_mb * 1024 * 1024), note: storageNote },
          { label: 'Your projects', value: String(quota.apps_used), limit: String(quota.max_apps), p: pct(quota.apps_used, quota.max_apps) },
        ]
      : [];

  return (
    <div className="space-y-5 xl:space-y-6">
      {myCards.length > 0 && (
        <div className="grid grid-cols-1 gap-3.5 xl:gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {myCards.map((c) => (
            <div key={c.label} className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
              <p className="text-[11px] font-medium text-zinc-400">{c.label}</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                {c.value} <span className="text-xs font-normal text-zinc-500">/ {c.limit}</span>
              </p>
              {'note' in c && c.note && <p className="mt-1.5 truncate text-[10px] text-zinc-500">{c.note}</p>}
              <div className="mt-3 h-1.5 w-full overflow-hidden rounded-full bg-white/[0.06]">
                <div
                  className={`h-full rounded-full ${c.p >= 90 ? 'bg-red-500' : c.p >= 70 ? 'bg-amber-500' : 'bg-emerald-500'}`}
                  style={{ width: `${c.p}%` }}
                />
              </div>
            </div>
          ))}
        </div>
      )}
      {/* Row 1: 4 Host Telemetry Cards */}
      <div className="grid grid-cols-1 gap-3.5 xl:gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className={`rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] ${hostOnly}`}>
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">Host Memory</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                {memUsedGB} <span className="text-xs font-normal text-zinc-500">/ {memTotalGB} GB</span>
              </p>
            </div>
            <span className="rounded-md bg-sky-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-sky-400 ring-1 ring-sky-500/20">
              {memPct}%
            </span>
          </div>
          <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-sky-500 to-sky-400 rounded-full"
              style={{ width: `${memPct}%` }}
            />
          </div>
        </div>

        <div className={`rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] ${hostOnly}`}>
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">Storage (/)</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                {diskUsedGB} <span className="text-xs font-normal text-zinc-500">/ {diskTotalGB} GB</span>
              </p>
            </div>
            <span className="rounded-md bg-orange-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-orange-400 ring-1 ring-orange-500/20">
              {diskPct}%
            </span>
          </div>
          <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-orange-500 to-amber-400 rounded-full"
              style={{ width: `${diskPct}%` }}
            />
          </div>
        </div>

        <div className={`rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] ${hostOnly}`}>
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">CPU Load ({numCPU} vCPU)</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                {cpuPct}
                <span className="text-xs font-normal text-zinc-500">%</span>
              </p>
            </div>
            <span className="rounded-md bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-emerald-400 ring-1 ring-emerald-500/20">
              Healthy
            </span>
          </div>
          <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-emerald-500 to-emerald-400 rounded-full"
              style={{ width: `${cpuPct}%` }}
            />
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">Containers & TLS</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                {containersCount} <span className="text-xs font-normal text-zinc-500">running</span>
              </p>
            </div>
            <StatusBadge status="active" label="TLS 1.3" />
          </div>
          <div className="mt-3 flex items-center justify-between font-mono text-[11px] text-zinc-500">
            <span>{apps.length} projects</span>
            <span>{apps.reduce((acc, a) => acc + (a.domains?.length || 0), 0)} SSL routes</span>
          </div>
        </div>
      </div>

      {/* Row 2: Telemetry Area Chart Card */}
      <div className={`rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] ${hostOnly}`}>
        <div className="flex flex-wrap items-center justify-between gap-2 mb-3.5">
          <div className="flex items-center gap-2">
            <h2 className="text-xs font-semibold text-zinc-200">Host Throughput</h2>
          </div>
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            {(
              [
                { id: 'requests', label: 'Ingress req/m' },
                { id: 'cpu-mem', label: 'CPU & RAM' },
                { id: 'network', label: 'Network I/O' },
              ] as const
            ).map((tab) => (
              <button
                key={tab.id}
                type="button"
                onClick={() => setChartMetric(tab.id)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-all cursor-pointer ${
                  chartMetric === tab.id
                    ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>
        </div>

        {chartMetric === 'requests' && (
          <EvilAreaChart
            data={telemetry}
            dataKey="requests"
            color="#f97316"
            unit=" req/m"
            height={210}
            label="Requests"
          />
        )}
        {chartMetric === 'cpu-mem' && (
          <EvilAreaChart
            data={telemetry}
            dataKey="cpu"
            secondaryKey="memory"
            color="#10b981"
            secondaryColor="#38bdf8"
            unit="%"
            height={210}
            label="CPU"
            secondaryLabel="RAM"
          />
        )}
        {chartMetric === 'network' && (
          <EvilAreaChart
            data={telemetry}
            dataKey="netInMbps"
            secondaryKey="netOutMbps"
            color="#f97316"
            secondaryColor="#eab308"
            unit=" Mbps"
            height={210}
            label="Inbound"
            secondaryLabel="Outbound"
          />
        )}
      </div>

      {/* Row 3: Recent Deployments & System Daemons */}
      <div className="grid grid-cols-1 gap-4 xl:gap-5 lg:grid-cols-3">
        <div className={`${isAdmin ? 'lg:col-span-2' : 'lg:col-span-3'} rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden flex flex-col justify-between`}>
          <div>
            <div className="flex items-center justify-between border-b border-white/[0.06] px-4 py-3">
              <div className="flex items-center gap-2">
                <GitBranch className="h-3.5 w-3.5 text-orange-400" />
                <h2 className="text-xs font-semibold text-zinc-100">Recent Deployments</h2>
              </div>
              <button
                type="button"
                onClick={() => onSelectNav('apps')}
                className="text-[11px] font-medium text-zinc-400 hover:text-zinc-100 cursor-pointer"
              >
                All Projects →
              </button>
            </div>

            <div className="divide-y divide-white/[0.05]">
              {recentDeployments.length === 0 ? (
                <div className="p-6 text-center text-xs text-zinc-500 font-mono">
                  No deployments yet. Create and deploy a project to see releases here.
                </div>
              ) : (
                recentDeployments.map((dep: any) => (
                  <div
                    key={dep.id}
                    onClick={() => onSelectApp(dep.appId)}
                    className="flex items-center justify-between gap-3 px-4 py-2.5 hover:bg-white/[0.03] transition-colors cursor-pointer text-xs"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <StatusBadge status={dep.status || 'ready'} />
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="font-mono font-semibold text-zinc-100">
                            {dep.appName}
                          </span>
                          <OwnerBadge id={dep.appOwnerId} name={dep.appOwner} role={dep.appOwnerRole} />
                          <span className="rounded bg-white/[0.05] px-1.5 py-0.2 font-mono text-[10px] text-orange-400">
                            {dep.branch || 'main'}@{dep.commitSha?.slice(0, 7) || 'latest'}
                          </span>
                        </div>
                        <div className="truncate text-[11px] text-zinc-500 mt-0.5">
                          {dep.commitMessage || 'Production deployment'}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-3 shrink-0 font-mono text-[11px] text-zinc-400">
                      <span className="hidden sm:inline rounded border border-white/[0.06] bg-[#09090b] px-1.5 py-0.5 text-[10px] text-zinc-400">
                        {dep.trigger || 'manual'}
                      </span>
                      <span>{dep.durationSec || 15}s</span>
                      <span className="text-zinc-500 w-16 text-right">
                        {dep.startedAt ? new Date(dep.startedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : 'Recently'}
                      </span>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>

        <div className={`rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden flex flex-col justify-between ${hostOnly}`}>
          <div>
            <div className="flex items-center justify-between border-b border-white/[0.06] px-4 py-3">
              <div className="flex items-center gap-2">
                <Server className="h-3.5 w-3.5 text-emerald-400" />
                <span className="text-xs font-semibold text-zinc-100">System Daemons</span>
              </div>
              <span className={`font-mono text-[10px] ${services.every((d) => d.ok) ? 'text-emerald-400' : 'text-amber-400'}`}>
                {services.filter((d) => d.ok).length}/{services.length} online
              </span>
            </div>

            <div className="divide-y divide-white/[0.05] font-mono text-xs">
              {services.map((d) => (
                <div
                  key={d.name}
                  className="flex items-center justify-between px-4 py-2.5 hover:bg-white/[0.02] transition-colors"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-zinc-100 font-semibold">{d.name}</span>
                      <span className="text-[10px] text-zinc-500">{d.ver}</span>
                    </div>
                    <div className="text-[10px] text-zinc-500 mt-0.5">{d.meta}</div>
                  </div>
                  <span
                    className={`inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-[10px] font-medium ring-1 ${
                      d.ok ? 'bg-emerald-500/10 text-emerald-400 ring-emerald-500/20' : 'bg-amber-500/10 text-amber-400 ring-amber-500/20'
                    }`}
                  >
                    <span className={`h-1.5 w-1.5 rounded-full ${d.ok ? 'bg-emerald-400' : 'bg-amber-400'}`} />
                    {d.ok ? 'ok' : 'down'}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
