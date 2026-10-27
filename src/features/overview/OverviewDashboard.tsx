import { OwnerBadge } from '../../components/ui/OwnerBadge';
import React, { useState, useEffect } from 'react';
import { ArrowUpRight, Check, Copy, GitBranch, Globe, Server, Shield, User } from 'lucide-react';
import { NavigationSection, TelemetryPoint } from '../../types/nextdeploy';
import { EvilAreaChart, StatusBadge } from '../../components/ui/Primitives';
import { backendApi, getCachedShieldNodes } from '../../lib/backendApi';
import { getServerPublicIp } from '../../lib/api/http';
import { ShieldNode } from '../../types/shield';

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
  const cachedShield = getCachedShieldNodes();
  const [shieldNodes, setShieldNodes] = useState<ShieldNode[]>(() => cachedShield.nodes || []);
  const [originIp, setOriginIp] = useState<string>(
    () => cachedShield.originIp || sys?.PublicIP || getServerPublicIp() || ''
  );
  const [copiedId, setCopiedId] = useState<string | null>(null);

  useEffect(() => {
    backendApi.listShieldNodes().then((res) => {
      if (res && res.ok) {
        if (Array.isArray(res.nodes)) setShieldNodes(res.nodes);
        if (res.origin_ip) setOriginIp(res.origin_ip);
      }
    }).catch(() => {});
  }, []);

  const activeShield = shieldNodes.find((n) => n.status === 'connected') || shieldNodes[0];
  const isShieldActive = Boolean(activeShield && (activeShield.status === 'connected' || shieldNodes.length > 0));
  const targetIp = isShieldActive
    ? activeShield.ip
    : (originIp || sys?.PublicIP || getServerPublicIp() || (typeof window !== 'undefined' ? window.location.hostname : ''));

  const handleCopy = (text: string, id: string) => {
    if (!text) return;
    navigator.clipboard.writeText(text);
    setCopiedId(id);
    setTimeout(() => {
      setCopiedId((curr) => (curr === id ? null : curr));
    }, 2000);
  };

  const memUsedGB = sys?.MemUsedGB != null ? sys.MemUsedGB.toFixed(1) : '0.0';
  const memTotalGB = sys?.MemTotalGB != null ? Math.round(sys.MemTotalGB) : 0;
  const memPct = sys?.MemUsedPct != null ? Math.round(sys.MemUsedPct) : 0;

  const diskUsedGB = sys?.DiskUsedGB != null ? sys.DiskUsedGB.toFixed(1) : '0.0';
  const diskTotalGB = sys?.DiskTotalGB != null ? Math.round(sys.DiskTotalGB) : 0;
  const diskPct = sys?.DiskUsedPct != null ? Math.round(sys.DiskUsedPct) : 0;

  const cpuPct = sys?.CPUPercent != null ? Math.round(sys.CPUPercent) : (telemetry[telemetry.length - 1]?.cpu || 0);
  const numCPU = sys?.NumCPU || 1;

  const recentDeployments = apps
    .flatMap((a) =>
      (a.deployments && a.deployments.length > 0
        ? a.deployments
        : [
            {
              id: `${a.id}-latest`,
              status: a.status === 'running' ? 'ready' : a.status || 'ready',
              branch: a.gitBranch || 'main',
              commitSha: a.gitCommit || 'latest',
              startedAt: a.createdAt || new Date().toISOString(),
              durationSec: 15,
            },
          ]
      ).map((d: any) => ({
        ...d,
        appId: a.id,
        appName: a.name,
        appOwner: a.owner || 'admin',
        appOwnerId: a.ownerId,
        appOwnerRole: a.ownerRole,
      }))
    )
    .sort((a: any, b: any) => {
      const ta = a.startedAt ? new Date(a.startedAt).getTime() : 0;
      const tb = b.startedAt ? new Date(b.startedAt).getTime() : 0;
      return tb - ta;
    })
    .slice(0, 6);

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
          {
            label: 'Memory',
            value: fmtMB(quota.memory_used_mb),
            limit: fmtMB(quota.max_memory_mb),
            p: pct(quota.memory_used_mb, quota.max_memory_mb),
          },
          {
            label: 'CPU',
            value: quota.max_cpus > 0 ? fmtPct((quota.cpu_used_cores / quota.max_cpus) * 100) : `${quota.cpu_used_cores.toFixed(2)} cores`,
            limit: quota.max_cpus > 0 ? `of ${quota.max_cpus} ${quota.max_cpus === 1 ? 'core' : 'cores'}` : 'no limit',
            p: pct(quota.cpu_used_cores, quota.max_cpus),
            tooltip: quota.max_cpus > 0 ? `${quota.cpu_used_cores.toFixed(2)} cores in use` : undefined,
          },
          {
            label: 'Storage',
            value: fmtBytes(quota.storage_used_bytes ?? quota.storage_used_mb * 1024 * 1024),
            limit: fmtMB(quota.max_storage_mb),
            p: pct(quota.storage_used_bytes ?? quota.storage_used_mb * 1024 * 1024, quota.max_storage_mb * 1024 * 1024),
            tooltip: storageNote || undefined,
          },
          {
            label: 'Projects',
            value: String(quota.apps_used),
            limit: String(quota.max_apps),
            p: pct(quota.apps_used, quota.max_apps),
          },
        ]
      : [];

  return (
    <div className="space-y-5 xl:space-y-6">
      {myCards.length > 0 && (
        <div className="grid grid-cols-1 gap-3.5 xl:gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {myCards.map((c) => (
            <div
              key={c.label}
              title={c.tooltip}
              className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between"
            >
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-xs font-medium text-zinc-400">{c.label}</p>
                  <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                    {c.value} <span className="text-xs font-normal text-zinc-500">/ {c.limit}</span>
                  </p>
                </div>
                <span
                  className={`rounded-md px-1.5 py-0.5 font-mono text-xs font-medium ring-1 ${
                    c.p >= 90
                      ? 'bg-rose-500/10 text-rose-400 ring-rose-500/20'
                      : c.p >= 70
                      ? 'bg-amber-500/10 text-amber-400 ring-amber-500/20'
                      : 'bg-emerald-500/10 text-emerald-400 ring-emerald-500/20'
                  }`}
                >
                  {c.p}%
                </span>
              </div>
              <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all duration-300 ${
                    c.p >= 90
                      ? 'bg-gradient-to-r from-rose-500 to-rose-400'
                      : c.p >= 70
                      ? 'bg-gradient-to-r from-amber-500 to-amber-400'
                      : 'bg-gradient-to-r from-emerald-500 to-emerald-400'
                  }`}
                  style={{ width: `${Math.min(100, Math.max(c.p > 0 ? 2 : 0, c.p))}%` }}
                />
              </div>
            </div>
          ))}
        </div>
      )}
      {/* Row 1/2: Host Telemetry & DNS Target Cards */}
      {isAdmin ? (
        <div className="space-y-3.5 xl:space-y-4">
          <div className="grid grid-cols-1 gap-3.5 xl:gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-xs font-medium text-zinc-400">Host Memory</p>
                  <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                    {memUsedGB} <span className="text-xs font-normal text-zinc-500">/ {memTotalGB} GB</span>
                  </p>
                </div>
                <span className="rounded-md bg-sky-500/10 px-1.5 py-0.5 font-mono text-xs font-medium text-sky-400 ring-1 ring-sky-500/20">
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

            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-xs font-medium text-zinc-400">Storage (/)</p>
                  <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                    {diskUsedGB} <span className="text-xs font-normal text-zinc-500">/ {diskTotalGB} GB</span>
                  </p>
                </div>
                <span className="rounded-md bg-orange-500/10 px-1.5 py-0.5 font-mono text-xs font-medium text-orange-400 ring-1 ring-orange-500/20">
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

            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-xs font-medium text-zinc-400">CPU Load ({numCPU} vCPU)</p>
                  <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                    {cpuPct}
                    <span className="text-xs font-normal text-zinc-500">%</span>
                  </p>
                </div>
                <span className="rounded-md bg-emerald-500/10 px-1.5 py-0.5 font-mono text-xs font-medium text-emerald-400 ring-1 ring-emerald-500/20">
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
                  <p className="text-xs font-medium text-zinc-400">Containers & TLS</p>
                  <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                    {containersCount} <span className="text-xs font-normal text-zinc-500">running</span>
                  </p>
                </div>
                <StatusBadge status="active" label="TLS 1.3" />
              </div>
              <div className="mt-3 flex items-center justify-between font-mono text-xs text-zinc-500">
                <span>{apps.length} projects</span>
                <span>{apps.reduce((acc, a) => acc + (a.domains?.length || 0), 0)} SSL routes</span>
              </div>
            </div>
          </div>

          {/* Admin DNS / Shield Nodes Ingress Card */}
          {shieldNodes.length > 1 ? (
            /* Multi-node Cluster View */
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] space-y-3.5">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-white/[0.05] pb-3">
                <div className="flex items-center gap-3">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-emerald-500/25 bg-emerald-500/10 text-emerald-400">
                    <Shield className="h-4.5 w-4.5" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-400">
                        Edge Shield Cluster
                      </h2>
                      <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2 py-0.5 font-mono text-[11px] font-semibold text-emerald-400 ring-1 ring-emerald-500/20">
                        <span className="relative flex h-1.5 w-1.5">
                          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                          <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-emerald-500" />
                        </span>
                        {shieldNodes.filter((n) => n.status === 'connected').length || shieldNodes.length} Online
                      </span>
                    </div>
                    <p className="text-[11px] text-zinc-400 mt-0.5">
                      Edge proxy nodes protecting origin server
                    </p>
                  </div>
                </div>

                <div className="flex items-center gap-2 self-start sm:self-auto flex-wrap">
                  {originIp && (
                    <div className="flex items-center gap-1.5 rounded-lg border border-white/[0.06] bg-zinc-900/60 px-2.5 py-1 text-xs font-mono text-zinc-400">
                      <span className="text-zinc-400">Origin:</span>
                      <span className="text-zinc-200 font-semibold">{originIp}</span>
                      <button
                        type="button"
                        onClick={() => handleCopy(originIp, 'origin')}
                        title="Copy Origin IP"
                        className="p-1 hover:text-white transition-colors cursor-pointer"
                      >
                        {copiedId === 'origin' ? (
                          <Check className="h-3 w-3 text-emerald-400" />
                        ) : (
                          <Copy className="h-3 w-3 text-zinc-400" />
                        )}
                      </button>
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={() => onSelectNav('shield-proxies')}
                    className="inline-flex items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 hover:bg-zinc-800 hover:border-white/[0.18] px-2.5 py-1 text-xs font-medium text-zinc-300 hover:text-white transition-all cursor-pointer"
                  >
                    <span>Manage</span>
                    <ArrowUpRight className="h-3 w-3 text-zinc-400" />
                  </button>
                </div>
              </div>

              {/* Responsive Grid for all Shield Nodes */}
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                {shieldNodes.map((node) => {
                  const isConnected = node.status === 'connected';
                  const isNodeCopied = copiedId === `node-${node.id}`;
                  return (
                    <div
                      key={node.id}
                      className="group relative flex flex-col justify-between rounded-xl border border-white/[0.06] bg-[#0c0c0e]/80 p-3.5 hover:border-white/[0.14] hover:bg-[#121217] transition-all"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <div className="flex items-center gap-1.5">
                            <span className={`h-2 w-2 rounded-full shrink-0 ${isConnected ? 'bg-emerald-400' : 'bg-amber-400'}`} />
                            <h3 className="font-semibold text-xs text-zinc-200 truncate" title={node.name}>
                              {node.name}
                            </h3>
                          </div>
                          <span className="text-[11px] text-zinc-400 font-mono mt-0.5 block">
                            {node.assigned_domains || 0} {node.assigned_domains === 1 ? 'domain' : 'domains'}
                          </span>
                        </div>
                        <span
                          className={`inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-mono text-[10px] font-medium ring-1 shrink-0 ${
                            isConnected
                              ? 'bg-emerald-500/10 text-emerald-400 ring-emerald-500/20'
                              : node.status === 'pending'
                              ? 'bg-amber-500/10 text-amber-400 ring-amber-500/20'
                              : 'bg-zinc-800 text-zinc-400 ring-white/10'
                          }`}
                        >
                          {isConnected ? 'Active' : node.status}
                        </span>
                      </div>

                      <div className="mt-3 flex items-center justify-between gap-2 border-t border-white/[0.04] pt-2.5">
                        <div className="min-w-0">
                          <span className="block text-[10px] uppercase font-semibold tracking-wider text-zinc-400">DNS IP</span>
                          <span className="font-mono text-sm sm:text-base font-bold tabular-nums text-zinc-100 truncate block">
                            {node.ip}
                          </span>
                        </div>
                        <button
                          type="button"
                          onClick={() => handleCopy(node.ip, `node-${node.id}`)}
                          title={`Copy IP for ${node.name}`}
                          className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:border-white/[0.16] hover:text-white transition-all cursor-pointer active:scale-95 shrink-0"
                        >
                          {isNodeCopied ? (
                            <>
                              <Check className="h-3 w-3 text-emerald-400" />
                              <span className="text-emerald-400 font-mono text-[11px]">Copied</span>
                            </>
                          ) : (
                            <>
                              <Copy className="h-3 w-3 text-zinc-400" />
                              <span className="font-mono text-[11px]">Copy</span>
                            </>
                          )}
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          ) : shieldNodes.length === 1 ? (
            /* Single Node Sleek Banner */
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3.5">
                <div className="flex items-center gap-3.5 min-w-0">
                  <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border border-emerald-500/25 bg-emerald-500/10 text-emerald-400">
                    <Shield className="h-5 w-5" />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-xs font-semibold uppercase tracking-wider text-zinc-400">
                        Edge Shield Ingress
                      </span>
                      <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2 py-0.5 font-mono text-[11px] font-semibold text-emerald-400 ring-1 ring-emerald-500/20">
                        <span className="relative flex h-1.5 w-1.5">
                          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                          <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-emerald-500" />
                        </span>
                        Active · {shieldNodes[0].name}
                      </span>
                    </div>
                    <div className="mt-1 flex items-center gap-2.5 flex-wrap">
                      <span className="font-mono text-xl sm:text-2xl font-bold tabular-nums text-zinc-100">
                        {shieldNodes[0].ip}
                      </span>
                      <button
                        type="button"
                        onClick={() => handleCopy(shieldNodes[0].ip, `node-${shieldNodes[0].id}`)}
                        title="Copy Shield IP for DNS A-record"
                        className="inline-flex h-6.5 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:border-white/[0.16] px-2.5 text-xs font-medium text-zinc-300 hover:text-white transition-all cursor-pointer active:scale-95"
                      >
                        {copiedId === `node-${shieldNodes[0].id}` ? (
                          <>
                            <Check className="h-3 w-3 text-emerald-400" />
                            <span className="text-emerald-400 font-mono text-[11px]">Copied</span>
                          </>
                        ) : (
                          <>
                            <Copy className="h-3 w-3 text-zinc-400" />
                            <span className="font-mono text-[11px]">Copy</span>
                          </>
                        )}
                      </button>
                    </div>
                  </div>
                </div>

                <div className="flex sm:flex-col items-center sm:items-end justify-between sm:justify-center gap-2 font-mono text-xs text-zinc-400 border-t sm:border-t-0 border-white/[0.05] pt-2.5 sm:pt-0">
                  {originIp && originIp !== shieldNodes[0].ip && (
                    <div className="flex items-center gap-1.5 text-zinc-400 text-xs">
                      <span>Origin:</span>
                      <span className="text-zinc-200">{originIp}</span>
                      <button
                        type="button"
                        onClick={() => handleCopy(originIp, 'origin')}
                        title="Copy Origin IP"
                        className="hover:text-white transition-colors cursor-pointer"
                      >
                        {copiedId === 'origin' ? (
                          <Check className="h-3 w-3 text-emerald-400" />
                        ) : (
                          <Copy className="h-3 w-3 text-zinc-400" />
                        )}
                      </button>
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={() => onSelectNav('shield-proxies')}
                    className="inline-flex items-center gap-1 text-[11px] font-sans font-medium text-zinc-400 hover:text-zinc-100 transition-colors cursor-pointer"
                  >
                    <span>Manage Shields</span>
                    <ArrowUpRight className="h-3 w-3" />
                  </button>
                </div>
              </div>
            </div>
          ) : (
            /* Zero Shield Nodes: Direct Server Ingress */
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3.5">
                <div className="flex items-center gap-3.5 min-w-0">
                  <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border border-sky-500/25 bg-sky-500/10 text-sky-400">
                    <Globe className="h-5 w-5" />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-semibold uppercase tracking-wider text-zinc-400">
                        Direct Server Ingress
                      </span>
                      <span className="inline-flex items-center gap-1 rounded-md bg-sky-500/10 px-2 py-0.5 font-mono text-[11px] font-semibold text-sky-400 ring-1 ring-sky-500/20">
                        Direct Mode
                      </span>
                    </div>
                    <div className="mt-1 flex items-center gap-2.5 flex-wrap">
                      <span className="font-mono text-xl sm:text-2xl font-bold tabular-nums text-zinc-100">
                        {targetIp || 'Detecting IP...'}
                      </span>
                      {targetIp && (
                        <button
                          type="button"
                          onClick={() => handleCopy(targetIp, 'direct')}
                          title="Copy Server IP"
                          className="inline-flex h-6.5 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:border-white/[0.16] px-2.5 text-xs font-medium text-zinc-300 hover:text-white transition-all cursor-pointer active:scale-95"
                        >
                          {copiedId === 'direct' ? (
                            <>
                              <Check className="h-3 w-3 text-emerald-400" />
                              <span className="text-emerald-400 font-mono text-[11px]">Copied</span>
                            </>
                          ) : (
                            <>
                              <Copy className="h-3 w-3 text-zinc-400" />
                              <span className="font-mono text-[11px]">Copy</span>
                            </>
                          )}
                        </button>
                      )}
                    </div>
                  </div>
                </div>

                <div className="flex sm:flex-col items-center sm:items-end justify-between sm:justify-center gap-1.5 border-t sm:border-t-0 border-white/[0.05] pt-2 sm:pt-0">
                  <button
                    type="button"
                    onClick={() => onSelectNav('shield-proxies')}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-500/20 bg-emerald-500/10 hover:bg-emerald-500/15 px-3 py-1.5 text-xs font-medium text-emerald-400 transition-all cursor-pointer"
                  >
                    <Shield className="h-3.5 w-3.5" />
                    <span>Setup Edge Shield</span>
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      ) : (
        /* User Balanced Grid: 2 equal-width cards */
        <div className="grid grid-cols-1 gap-3.5 xl:gap-4 sm:grid-cols-2">
          {/* Card 1: Containers & TLS */}
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between">
            <div className="flex items-start justify-between">
              <div>
                <p className="text-xs font-medium text-zinc-400">Containers & TLS</p>
                <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                  {containersCount} <span className="text-xs font-normal text-zinc-500">running</span>
                </p>
              </div>
              <StatusBadge status="active" label="TLS 1.3" />
            </div>
            <div className="mt-3 flex items-center justify-between font-mono text-xs text-zinc-500 border-t border-white/[0.04] pt-2.5">
              <span>{apps.length} projects</span>
              <span>{apps.reduce((acc, a) => acc + (a.domains?.length || 0), 0)} SSL routes</span>
            </div>
          </div>

          {/* Card 2: DNS Target IP (Shield or Direct) */}
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="text-xs font-medium text-zinc-400">
                  {isShieldActive ? 'Shield Ingress IP' : 'Server Ingress IP'}
                </p>
                <div className="mt-1.5 flex items-center gap-2 flex-wrap">
                  <span className="font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                    {targetIp || 'Detecting IP...'}
                  </span>
                  {targetIp && (
                    <button
                      type="button"
                      onClick={() => handleCopy(targetIp, 'user-target')}
                      title="Copy IP for DNS A-record"
                      className="inline-flex h-6 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:border-white/[0.16] px-2 text-xs font-medium text-zinc-300 hover:text-white transition-all cursor-pointer active:scale-95 shrink-0"
                    >
                      {copiedId === 'user-target' ? (
                        <>
                          <Check className="h-3 w-3 text-emerald-400" />
                          <span className="text-emerald-400 font-mono text-[10px]">Copied</span>
                        </>
                      ) : (
                        <>
                          <Copy className="h-3 w-3 text-zinc-400" />
                          <span className="font-mono text-[10px]">Copy</span>
                        </>
                      )}
                    </button>
                  )}
                </div>
              </div>
              {isShieldActive ? (
                <span className="inline-flex items-center gap-1.5 rounded-md bg-emerald-500/10 px-2 py-0.5 font-mono text-xs font-medium text-emerald-400 ring-1 ring-emerald-500/20 shrink-0">
                  <Shield className="h-3.5 w-3.5" />
                  Protected
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5 rounded-md bg-sky-500/10 px-2 py-0.5 font-mono text-xs font-medium text-sky-400 ring-1 ring-sky-500/20 shrink-0">
                  <Globe className="h-3.5 w-3.5" />
                  Direct
                </span>
              )}
            </div>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-1 font-mono text-xs text-zinc-500 border-t border-white/[0.04] pt-2.5">
              <span className="truncate">
                Point domain A-records to this IP
              </span>
              <span className="text-[11px] text-zinc-500 truncate">
                {isShieldActive ? 'Edge SSL & Routing' : 'Direct Server Routing'}
              </span>
            </div>
          </div>
        </div>
      )}

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
                className={`rounded-md px-2.5 sm:px-3 py-1.5 sm:py-1 min-h-[32px] sm:min-h-[28px] text-xs font-medium transition-all cursor-pointer ${
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
        <div className={`${isAdmin ? 'lg:col-span-2' : 'lg:col-span-3'} rounded-xl border border-zinc-800/80 bg-[#0d0d11] overflow-hidden flex flex-col justify-between shadow-sm`}>
          <div>
            <div className="flex items-center justify-between border-b border-zinc-800/80 px-4 py-3.5">
              <div className="flex items-center gap-2">
                <GitBranch className="h-4 w-4 text-orange-400" />
                <h2 className="text-sm font-semibold text-white tracking-tight">Recent Deployments</h2>
              </div>
              <button
                type="button"
                onClick={() => onSelectNav('apps')}
                className="flex items-center gap-1 text-xs font-medium text-zinc-400 hover:text-white cursor-pointer transition-colors"
              >
                <span>All Projects</span>
                <span>→</span>
              </button>
            </div>

            <div className="divide-y divide-zinc-800/60">
              {recentDeployments.length === 0 ? (
                <div className="py-10 text-center text-xs text-zinc-500 font-mono">
                  No deployments yet. Create and deploy a project to see releases here.
                </div>
              ) : (
                recentDeployments.map((dep: any) => {
                  const isFailed = dep.status === 'failed' || dep.status === 'error';
                  const isBuilding = dep.status === 'building' || dep.status === 'deploying' || dep.status === 'in_progress';
                  const branchText = dep.branch || 'main';
                  const commitText = dep.commitSha ? (dep.commitSha.length > 7 ? dep.commitSha.slice(0, 7) : dep.commitSha) : 'latest';
                  const branchCommit = `${branchText}@${commitText}`;

                  const formattedTime = dep.startedAt
                    ? new Date(dep.startedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                    : '12:00 AM';

                  return (
                    <div
                      key={dep.id}
                      onClick={() => onSelectApp(dep.appId)}
                      className="px-4 py-3.5 hover:bg-zinc-900/50 transition-colors cursor-pointer space-y-2"
                    >
                      {/* Line 1: Status Pill + App Name on Left, Owner Badge on Right */}
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2.5 min-w-0">
                          {isFailed ? (
                            <span className="inline-flex items-center gap-1.5 rounded-md border border-rose-500/40 bg-[#2b0f15] px-2 py-0.5 text-xs font-medium text-rose-400 shrink-0">
                              <span className="h-1.5 w-1.5 rounded-full bg-rose-400" />
                              <span>Failed</span>
                            </span>
                          ) : isBuilding ? (
                            <span className="inline-flex items-center gap-1.5 rounded-md border border-sky-500/40 bg-[#0c1f2f] px-2 py-0.5 text-xs font-medium text-sky-400 shrink-0">
                              <span className="h-1.5 w-1.5 rounded-full bg-sky-400 animate-pulse" />
                              <span>Building</span>
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1.5 rounded-md border border-emerald-500/40 bg-[#0c2419] px-2 py-0.5 text-xs font-medium text-emerald-400 shrink-0">
                              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                              <span>Ready</span>
                            </span>
                          )}

                          <span className="font-semibold text-white text-sm tracking-tight truncate">
                            {dep.appName}
                          </span>
                        </div>

                        {(() => {
                          const matchedApp = apps.find((a: any) => a.id === dep.appId || a.name === dep.appName);
                          const ownerName = dep.appOwner || matchedApp?.owner || 'admin';
                          const isOwnerAdmin = ownerName === 'admin' || matchedApp?.ownerRole === 'admin';

                          return isOwnerAdmin ? (
                            <span className="inline-flex items-center gap-1 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-300 shrink-0">
                              <Shield className="h-3 w-3 text-amber-400" />
                              <span>{ownerName}</span>
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 rounded-md border border-sky-500/40 bg-sky-500/10 px-2 py-0.5 text-xs font-medium text-sky-300 shrink-0">
                              <User className="h-3 w-3 text-sky-400" />
                              <span>{ownerName}</span>
                            </span>
                          );
                        })()}
                      </div>

                      {/* Line 2: Deploy · main@latest on Left, Duration + Time on Right */}
                      <div className="flex items-center justify-between gap-2 text-xs">
                        <div className="flex items-center gap-1.5 text-zinc-500 min-w-0">
                          <span className="text-zinc-500">Deploy</span>
                          <span className="text-zinc-600 font-bold">·</span>
                          <span className="rounded border border-amber-800/60 bg-[#241708] px-2 py-0.5 font-mono text-xs font-medium text-amber-400 truncate">
                            {branchCommit}
                          </span>
                        </div>

                        <div className="flex items-center gap-2.5 font-mono text-xs text-zinc-400 shrink-0">
                          <span>{dep.durationSec || 15}s</span>
                          <span>{formattedTime}</span>
                        </div>
                      </div>
                    </div>
                  );
                })
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
              <span className={`font-mono text-xs ${services.every((d) => d.ok) ? 'text-emerald-400' : 'text-amber-400'}`}>
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
                      <span className="text-xs text-zinc-500">{d.ver}</span>
                    </div>
                    <div className="text-xs text-zinc-500 mt-0.5">{d.meta}</div>
                  </div>
                  <span
                    className={`inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium ring-1 ${
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
