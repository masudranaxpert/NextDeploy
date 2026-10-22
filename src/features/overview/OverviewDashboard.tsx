import React, { useState } from 'react';
import { GitBranch, Server } from 'lucide-react';
import { NavigationSection, TelemetryPoint } from '../../types/nextdeploy';
import { EvilAreaChart, StatusBadge } from '../../components/ui/Primitives';

interface OverviewDashboardProps {
  telemetry: TelemetryPoint[];
  onSelectApp: (appId: string) => void;
  onSelectNav: (nav: NavigationSection) => void;
}

const RECENT_DEPLOYMENTS_FEED = [
  {
    id: 'dep-4920',
    appId: 'app-9f81c2a0',
    appName: 'commerce-core-api',
    branch: 'main',
    sha: '8c4f19a',
    message: 'perf(checkout): batch inventory reservation lock in redis',
    trigger: 'GitHub Push',
    duration: '28s',
    time: '4m ago',
    status: 'ready' as const,
  },
  {
    id: 'dep-4917',
    appId: 'app-6b12f0e9',
    appName: 'temporal-worker-queue',
    branch: 'main',
    sha: '19d04e2',
    message: 'chore(ffmpeg): bump worker concurrency limit to 8 threads',
    trigger: 'GitLab CI',
    duration: '19s',
    time: '12m ago',
    status: 'building' as const,
  },
  {
    id: 'dep-4915',
    appId: 'app-7d24e8b1',
    appName: 'studio-web-dashboard',
    branch: 'production',
    sha: 'f42a90c',
    message: 'feat(billing): integrate instant SEPA & invoice PDF stream',
    trigger: 'GitHub Push',
    duration: '42s',
    time: '38m ago',
    status: 'ready' as const,
  },
  {
    id: 'dep-4912',
    appId: 'app-9f81c2a0',
    appName: 'commerce-core-api',
    branch: 'main',
    sha: '3b10e4c',
    message: 'feat(orders): add idempotency key header middleware',
    trigger: 'nd CLI',
    duration: '31s',
    time: '5h ago',
    status: 'ready' as const,
  },
  {
    id: 'dep-4908',
    appId: 'app-3c90a1f4',
    appName: 'plausible-analytics',
    branch: 'v2.1.4',
    sha: 'compose',
    message: 'ClickHouse 24.3-alpine + Plausible v2.1 rollout',
    trigger: 'Compose',
    duration: '16s',
    time: '2d ago',
    status: 'ready' as const,
  },
];

export const OverviewDashboard: React.FC<OverviewDashboardProps> = ({
  telemetry,
  onSelectApp,
  onSelectNav,
}) => {
  const [chartMetric, setChartMetric] = useState<'requests' | 'cpu-mem' | 'network'>('requests');

  return (
    <div className="space-y-5 xl:space-y-6">
      {/* Row 1: 4 Host Telemetry Cards */}
      <div className="grid grid-cols-1 gap-3.5 xl:gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">Host Memory</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                15.4 <span className="text-xs font-normal text-zinc-500">/ 32 GB</span>
              </p>
            </div>
            <span className="rounded-md bg-sky-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-sky-400 ring-1 ring-sky-500/20">
              48%
            </span>
          </div>
          <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-sky-500 to-sky-400 rounded-full"
              style={{ width: '48%' }}
            />
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">NVMe Storage (/)</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                84.2 <span className="text-xs font-normal text-zinc-500">/ 320 GB</span>
              </p>
            </div>
            <span className="rounded-md bg-orange-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-orange-400 ring-1 ring-orange-500/20">
              27%
            </span>
          </div>
          <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-orange-500 to-amber-400 rounded-full"
              style={{ width: '27%' }}
            />
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">CPU Load (16 vCPU)</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                {telemetry[telemetry.length - 1]?.cpu || 23}
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
              style={{ width: `${telemetry[telemetry.length - 1]?.cpu || 23}%` }}
            />
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-[11px] font-medium text-zinc-400">Containers & TLS</p>
              <p className="mt-1.5 font-mono text-xl xl:text-2xl font-bold tabular-nums text-zinc-100 leading-none">
                7 <span className="text-xs font-normal text-zinc-500">running</span>
              </p>
            </div>
            <StatusBadge status="active" label="TLS 1.3" />
          </div>
          <div className="mt-3 flex items-center justify-between font-mono text-[11px] text-zinc-500">
            <span>4 projects</span>
            <span>4 SSL routes</span>
          </div>
        </div>
      </div>

      {/* Row 2: Telemetry Area Chart Card */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 xl:p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
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
        <div className="lg:col-span-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden flex flex-col justify-between">
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
              {RECENT_DEPLOYMENTS_FEED.map((dep) => (
                <div
                  key={dep.id}
                  onClick={() => onSelectApp(dep.appId)}
                  className="flex items-center justify-between gap-3 px-4 py-2.5 hover:bg-white/[0.03] transition-colors cursor-pointer text-xs"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <StatusBadge status={dep.status} />
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-mono font-semibold text-zinc-100">
                          {dep.appName}
                        </span>
                        <span className="rounded bg-white/[0.05] px-1.5 py-0.2 font-mono text-[10px] text-orange-400">
                          {dep.branch}@{dep.sha}
                        </span>
                      </div>
                      <div className="truncate text-[11px] text-zinc-500 mt-0.5">{dep.message}</div>
                    </div>
                  </div>

                  <div className="flex items-center gap-3 shrink-0 font-mono text-[11px] text-zinc-400">
                    <span className="hidden sm:inline rounded border border-white/[0.06] bg-[#09090b] px-1.5 py-0.5 text-[10px] text-zinc-400">
                      {dep.trigger}
                    </span>
                    <span>{dep.duration}</span>
                    <span className="text-zinc-500 w-12 text-right">{dep.time}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between border-b border-white/[0.06] px-4 py-3">
              <div className="flex items-center gap-2">
                <Server className="h-3.5 w-3.5 text-emerald-400" />
                <span className="text-xs font-semibold text-zinc-100">System Daemons</span>
              </div>
              <span className="font-mono text-[10px] text-emerald-400">5/5 Online</span>
            </div>

            <div className="divide-y divide-white/[0.05] font-mono text-xs">
              {[
                { name: 'dockerd', ver: 'v27.3.1', meta: '7 containers', status: 'running' as const },
                { name: 'caddy-proxy', ver: 'v2.8.4', meta: 'TLS 1.3 · HTTP/3', status: 'active' as const },
                { name: 'nextdeployd', ver: 'v2.4.0', meta: '24 MB RAM', status: 'running' as const },
                { name: 'mcp-server', ver: 'SSE/JSON-RPC', meta: '16 tools', status: 'active' as const },
                { name: 'rclone-cron', ver: '0 */6 * * *', meta: 'R2 + GDrive', status: 'connected' as const },
              ].map((d) => (
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
                  <span className="inline-flex items-center gap-1.5 rounded-md bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-400 ring-1 ring-emerald-500/20">
                    <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                    ok
                  </span>
                </div>
              ))}
            </div>
          </div>

          <div className="flex items-center justify-between border-t border-white/[0.06] bg-[#09090b]/50 px-4 py-2.5 font-mono text-[10px] text-zinc-500">
            <span>Ubuntu 24.04 LTS</span>
            <span className="text-zinc-400">Uptime 42d 18h</span>
          </div>
        </div>
      </div>
    </div>
  );
};
