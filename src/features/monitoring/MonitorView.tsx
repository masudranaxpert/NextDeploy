import React, { useState } from 'react';
import {
  Pin,
  RefreshCw,
  Activity,
  Search,
  Eye,
  EyeOff,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import { OwnerBadge } from '../../components/ui/OwnerBadge';
import { AppStack, TelemetryPoint } from '../../types/nextdeploy';
import { EvilAreaChart, StatusBadge } from '../../components/ui/Primitives';

interface MonitorViewProps {
  telemetry: TelemetryPoint[];
  apps: AppStack[];
  onStreamTick: () => void;
}

export const MonitorView: React.FC<MonitorViewProps> = ({ telemetry, apps, onStreamTick }) => {
  const [pinnedApps, setPinnedApps] = useState<string[]>([]);
  const [monitorSearch, setMonitorSearch] = useState('');
  const [monitorStatusFilter, setMonitorStatusFilter] = useState<
    'all' | 'running' | 'building' | 'stopped'
  >('all');
  const [showCpuChart, setShowCpuChart] = useState(true);
  const [showMemChart, setShowMemChart] = useState(true);
  const [showNetChart, setShowNetChart] = useState(true);

  const togglePin = (slug: string) => {
    setPinnedApps((prev) =>
      prev.includes(slug) ? prev.filter((s) => s !== slug) : [...prev, slug]
    );
  };

  const filteredMonitorApps = apps.filter((app) => {
    if (monitorStatusFilter !== 'all' && app.status !== monitorStatusFilter) return false;
    const q = monitorSearch.trim().toLowerCase();
    if (!q) return true;
    return (
      app.name.toLowerCase().includes(q) ||
      app.slug.toLowerCase().includes(q) ||
      (app.primaryDomain && app.primaryDomain.toLowerCase().includes(q)) ||
      app.services.some((s) => s.service.toLowerCase().includes(q) || s.name.toLowerCase().includes(q))
    );
  });

  const sortedApps = [...filteredMonitorApps].sort((a, b) => {
    const aPin = pinnedApps.includes(a.slug) ? 1 : 0;
    const bPin = pinnedApps.includes(b.slug) ? 1 : 0;
    return bPin - aPin;
  });

  const latest = telemetry[telemetry.length - 1] || {
    cpu: 23,
    memory: 48,
    netInMbps: 44,
    netOutMbps: 33,
  };

  const anyChartVisible = showCpuChart || showMemChart || showNetChart;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Activity className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Monitoring</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {sortedApps.length}
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={monitorSearch}
              onChange={(e) => setMonitorSearch(e.target.value)}
              placeholder="Search projects or services..."
              className="h-8 w-52 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-7 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none transition-colors"
            />
            {monitorSearch && (
              <button
                type="button"
                onClick={() => setMonitorSearch('')}
                className="absolute right-2 top-2 text-zinc-500 hover:text-zinc-200 cursor-pointer"
                title="Clear search"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-1">
            <span className="px-2 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
              Charts
            </span>
            <button
              type="button"
              onClick={() => setShowCpuChart((p) => !p)}
              className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors cursor-pointer ${
                showCpuChart
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {showCpuChart ? <Eye className="h-3 w-3 text-emerald-400" /> : <EyeOff className="h-3 w-3" />}
              CPU
            </button>
            <button
              type="button"
              onClick={() => setShowMemChart((p) => !p)}
              className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors cursor-pointer ${
                showMemChart
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {showMemChart ? <Eye className="h-3 w-3 text-sky-400" /> : <EyeOff className="h-3 w-3" />}
              RAM
            </button>
            <button
              type="button"
              onClick={() => setShowNetChart((p) => !p)}
              className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors cursor-pointer ${
                showNetChart
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {showNetChart ? <Eye className="h-3 w-3 text-orange-400" /> : <EyeOff className="h-3 w-3" />}
              Network
            </button>
          </div>

          <button
            type="button"
            onClick={() => {
              onStreamTick();
              toast.success('Updated telemetry');
            }}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <RefreshCw className="h-3.5 w-3.5 text-orange-400" />
            Refresh
          </button>
        </div>
      </div>

      {anyChartVisible && (
        <div
          className={`grid grid-cols-1 gap-3.5 ${
            [showCpuChart, showMemChart, showNetChart].filter(Boolean).length === 3
              ? 'lg:grid-cols-3'
              : [showCpuChart, showMemChart, showNetChart].filter(Boolean).length === 2
                ? 'lg:grid-cols-2'
                : 'lg:grid-cols-1'
          }`}
        >
          {showCpuChart && (
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-300">CPU Load</span>
                  <span className="font-mono text-xs font-bold text-emerald-400">
                    {latest.cpu}%
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => setShowCpuChart(false)}
                  className="rounded p-1 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                  title="Hide CPU chart"
                >
                  <EyeOff className="h-3.5 w-3.5" />
                </button>
              </div>
              <EvilAreaChart
                data={telemetry}
                dataKey="cpu"
                color="#10b981"
                unit="%"
                height={145}
                label="CPU"
              />
            </div>
          )}

          {showMemChart && (
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-300">Memory</span>
                  <span className="font-mono text-xs font-bold text-sky-400">
                    {latest.memory}%
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => setShowMemChart(false)}
                  className="rounded p-1 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                  title="Hide RAM chart"
                >
                  <EyeOff className="h-3.5 w-3.5" />
                </button>
              </div>
              <EvilAreaChart
                data={telemetry}
                dataKey="memory"
                color="#38bdf8"
                unit="%"
                height={145}
                label="Memory"
              />
            </div>
          )}

          {showNetChart && (
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-300">Network I/O</span>
                  <span className="font-mono text-xs font-bold text-orange-400">
                    {latest.netInMbps} Mbps
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => setShowNetChart(false)}
                  className="rounded p-1 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                  title="Hide Network chart"
                >
                  <EyeOff className="h-3.5 w-3.5" />
                </button>
              </div>
              <EvilAreaChart
                data={telemetry}
                dataKey="netInMbps"
                secondaryKey="netOutMbps"
                color="#f97316"
                secondaryColor="#eab308"
                unit=" Mbps"
                height={145}
                label="Inbound"
                secondaryLabel="Outbound"
              />
            </div>
          )}
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/40 px-4 py-2.5">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-zinc-200">Project Resource Usage</span>
            <span className="font-mono text-[11px] text-zinc-500">
              ({sortedApps.length} of {apps.length})
            </span>
          </div>
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            {(['all', 'running', 'building', 'stopped'] as const).map((st) => (
              <button
                key={st}
                type="button"
                onClick={() => setMonitorStatusFilter(st)}
                className={`rounded-md px-2 py-0.5 text-[11px] font-medium capitalize transition-colors cursor-pointer ${
                  monitorStatusFilter === st
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {st}
              </button>
            ))}
          </div>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[34%] px-4 py-2.5 font-medium">Project</th>
                <th className="w-[18%] px-4 py-2.5 font-medium">Status</th>
                <th className="w-[16%] px-4 py-2.5 font-medium text-right">CPU</th>
                <th className="w-[16%] px-4 py-2.5 font-medium text-right">Memory</th>
                <th className="w-[16%] px-4 py-2.5 font-medium text-right">Disk</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {sortedApps.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-xs text-zinc-500">
                    No projects match &ldquo;{monitorSearch}&rdquo;
                  </td>
                </tr>
              ) : (
                sortedApps.map((app) => {
                  const isPinned = pinnedApps.includes(app.slug);
                  return (
                    <tr key={app.id} className="hover:bg-white/[0.03] transition-colors">
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2.5 min-w-0">
                          <button
                            type="button"
                            onClick={() => togglePin(app.slug)}
                            className={`cursor-pointer ${
                              isPinned ? 'text-orange-400' : 'text-zinc-600 hover:text-zinc-300'
                            }`}
                          >
                            <Pin className="h-3.5 w-3.5" />
                          </button>
                          <div className="min-w-0">
                            <span className="flex items-center gap-1.5">
                              <span className="truncate font-mono font-semibold text-zinc-100">{app.name}</span>
                              <OwnerBadge id={app.ownerId} name={app.owner} role={app.ownerRole} />
                            </span>
                            {app.primaryDomain && (
                              <span className="block truncate font-mono text-[10px] text-zinc-500">
                                {app.primaryDomain}
                              </span>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <StatusBadge status={app.status} />
                      </td>
                      <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                        {app.cpuPct}%
                      </td>
                      <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                        {app.memMB} MB
                      </td>
                      <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-400">
                        {(app.diskMB / 1024).toFixed(2)} GB
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
