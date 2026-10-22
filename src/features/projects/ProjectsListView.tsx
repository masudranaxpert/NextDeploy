import React, { useState } from 'react';
import {
  ArrowUpRight,
  Copy,
  ExternalLink,
  GitBranch,
  Globe,
  Kanban,
  Layers,
  LayoutGrid,
  List,
  Play,
  RotateCw,
  Search,
  Square,
  Trash2,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { ActionMenu, StatusBadge } from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';

interface ProjectsListViewProps {
  apps: AppStack[];
  onSelectApp: (appId: string) => void;
  onUpdateApps: React.Dispatch<React.SetStateAction<AppStack[]>>;
}

export const ProjectsListView: React.FC<ProjectsListViewProps> = ({
  apps,
  onSelectApp,
  onUpdateApps,
}) => {
  const [appsLayout, setAppsLayout] = useState<'grid' | 'board' | 'table'>('grid');
  const [appSearch, setAppSearch] = useState('');
  const [appStatusFilter, setAppStatusFilter] = useState<
    'all' | 'running' | 'building' | 'stopped'
  >('all');

  const filteredApps = apps.filter((a) => {
    if (appStatusFilter !== 'all' && a.status !== appStatusFilter) return false;
    if (
      appSearch &&
      !a.name.toLowerCase().includes(appSearch.toLowerCase()) &&
      !(a.primaryDomain && a.primaryDomain.toLowerCase().includes(appSearch.toLowerCase()))
    ) {
      return false;
    }
    return true;
  });

  const getAppActions = (app: AppStack) => [
    {
      label: 'Open Workspace',
      icon: <ExternalLink className="h-3.5 w-3.5" />,
      onClick: () => onSelectApp(app.id),
    },
    {
      label: 'Trigger Deploy',
      icon: <Play className="h-3.5 w-3.5" />,
      onClick: async () => {
        toast.info(`Deploying ${app.name}...`);
        await backendApi.composeAction(app.id, 'redeploy');
        toast.success(`Deploy initiated for ${app.name}`);
      },
    },
    {
      label: 'Copy App ID',
      icon: <Copy className="h-3.5 w-3.5" />,
      onClick: () => {
        navigator.clipboard?.writeText(app.id);
        toast.success(`Copied ${app.id}`);
      },
    },
    {
      label: 'Restart Containers',
      icon: <RotateCw className="h-3.5 w-3.5" />,
      onClick: async () => {
        toast.info(`Restarting ${app.name}...`);
        await backendApi.composeAction(app.id, 'restart');
        toast.success(`Restarted ${app.name}`);
      },
    },
    {
      label: app.status === 'stopped' ? 'Start Stack' : 'Stop Stack',
      icon:
        app.status === 'stopped' ? (
          <Play className="h-3.5 w-3.5" />
        ) : (
          <Square className="h-3.5 w-3.5" />
        ),
      onClick: async () => {
        const nextStatus = app.status === 'stopped' ? ('running' as const) : ('stopped' as const);
        await backendApi.composeAction(app.id, nextStatus === 'running' ? 'up' : 'down');
        onUpdateApps((prev) =>
          prev.map((a) => (a.id === app.id ? { ...a, status: nextStatus } : a))
        );
        toast.success(`${nextStatus === 'running' ? 'Started' : 'Stopped'} ${app.name}`);
      },
    },
    {
      label: 'Delete Project',
      icon: <Trash2 className="h-3.5 w-3.5" />,
      variant: 'danger' as const,
      onClick: async () => {
        if (!window.confirm(`Permanently delete project "${app.name}" and remove all related containers?`)) return;
        await backendApi.deleteApp(app.id, app.name);
        onUpdateApps((prev) => prev.filter((a) => a.id !== app.id));
        toast.success(`Deleted ${app.name}`);
      },
    },
  ];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Layers className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">Projects</h1>
        </div>
      </div>

      <div className="space-y-3.5">
        <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
              <input
                type="text"
                value={appSearch}
                onChange={(e) => setAppSearch(e.target.value)}
                placeholder="Filter projects..."
                className="h-8 w-52 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-3 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none transition-colors"
              />
            </div>
            <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
              {(['all', 'running', 'building', 'stopped'] as const).map((st) => (
                <button
                  key={st}
                  type="button"
                  onClick={() => setAppStatusFilter(st)}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium capitalize transition-all cursor-pointer ${
                    appStatusFilter === st
                      ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {st}
                </button>
              ))}
            </div>
          </div>

          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5 self-start sm:self-auto">
            <button
              type="button"
              onClick={() => setAppsLayout('grid')}
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-all ${
                appsLayout === 'grid'
                  ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <LayoutGrid className="h-3.5 w-3.5" />
              Cards
            </button>
            <button
              type="button"
              onClick={() => setAppsLayout('board')}
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-all ${
                appsLayout === 'board'
                  ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Kanban className="h-3.5 w-3.5" />
              Board
            </button>
            <button
              type="button"
              onClick={() => setAppsLayout('table')}
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-all ${
                appsLayout === 'table'
                  ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <List className="h-3.5 w-3.5" />
              List
            </button>
          </div>
        </div>

        {appsLayout === 'grid' && (
          <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
            {filteredApps.map((app) => (
              <div
                key={app.id}
                onClick={() => onSelectApp(app.id)}
                className="group relative overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/80 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] transition-all hover:border-white/[0.15] hover:bg-[#141419] cursor-pointer flex flex-col justify-between gap-3.5"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900 font-display text-xs font-bold text-orange-400 uppercase shadow-inner group-hover:border-orange-500/30 transition-colors">
                      {app.name.slice(0, 2)}
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <h3 className="truncate text-sm font-semibold text-zinc-100 group-hover:text-white transition-colors">
                          {app.name}
                        </h3>
                        <StatusBadge status={app.status} />
                      </div>
                      <div className="mt-0.5 flex items-center gap-2 text-[11px] text-zinc-400">
                        {app.primaryDomain ? (
                          <span className="inline-flex items-center gap-1 font-mono text-zinc-300 group-hover:text-orange-300 transition-colors truncate">
                            <Globe className="h-3 w-3 text-emerald-400 shrink-0" />
                            {app.primaryDomain}
                            <ArrowUpRight className="h-2.5 w-2.5 opacity-60 shrink-0" />
                          </span>
                        ) : (
                          <span className="font-mono text-zinc-500">internal compose network</span>
                        )}
                      </div>
                    </div>
                  </div>

                  <ActionMenu items={getAppActions(app)} />
                </div>

                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-1.5">
                    {app.services.map((svc) => (
                      <span
                        key={svc.id}
                        className="inline-flex items-center gap-1.5 rounded-md border border-white/[0.06] bg-[#09090b]/90 px-2 py-0.5 font-mono text-[10px] text-zinc-300"
                      >
                        <span
                          className={`h-1.5 w-1.5 rounded-full ${
                            svc.state === 'running' ? 'bg-emerald-400' : 'bg-zinc-500'
                          }`}
                        />
                        {svc.service}
                      </span>
                    ))}
                  </div>

                  {app.gitBranch && (
                    <span className="inline-flex items-center gap-1 rounded-md border border-white/[0.06] bg-[#09090b]/60 px-2 py-0.5 font-mono text-[10px] text-zinc-400">
                      <GitBranch className="h-2.5 w-2.5 text-orange-400" />
                      {app.gitBranch}
                      {app.gitCommitSha && (
                        <span className="text-zinc-500">@{app.gitCommitSha.slice(0, 7)}</span>
                      )}
                    </span>
                  )}
                </div>

                <div className="flex items-center justify-between border-t border-white/[0.05] pt-2.5 text-[11px] text-zinc-500 font-mono tabular-nums">
                  <div className="flex items-center gap-3">
                    <span>
                      CPU <strong className="font-medium text-zinc-300">{app.cpuPct}%</strong>
                    </span>
                    <span>
                      RAM <strong className="font-medium text-zinc-300">{app.memMB} MB</strong>
                    </span>
                  </div>
                  <span>{app.updatedAt}</span>
                </div>
              </div>
            ))}
          </div>
        )}

        {appsLayout === 'board' && (
          <div className="grid grid-cols-1 gap-3.5 lg:grid-cols-3">
            {(
              [
                { key: 'running', title: 'Running' },
                { key: 'building', title: 'Building' },
                { key: 'stopped', title: 'Stopped' },
              ] as const
            ).map((col) => {
              const colApps = filteredApps.filter((a) => a.status === col.key);
              return (
                <div
                  key={col.key}
                  className="rounded-xl border border-white/[0.07] bg-[#09090b]/60 p-3.5 space-y-2.5"
                >
                  <div className="flex items-center justify-between text-xs font-semibold text-zinc-300">
                    <StatusBadge status={col.key} label={col.title} />
                    <span className="font-mono text-zinc-500">{colApps.length}</span>
                  </div>
                  <div className="space-y-2">
                    {colApps.map((app) => (
                      <div
                        key={app.id}
                        onClick={() => onSelectApp(app.id)}
                        className="flex items-center justify-between rounded-xl border border-white/[0.07] bg-[#111115] p-3 hover:border-white/[0.15] cursor-pointer transition-all"
                      >
                        <div className="min-w-0">
                          <div className="truncate text-xs font-semibold text-zinc-100">
                            {app.name}
                          </div>
                          <div className="mt-0.5 font-mono text-[11px] text-zinc-500">
                            {app.services.length} services · {app.cpuPct}% CPU
                          </div>
                        </div>
                        <ActionMenu items={getAppActions(app)} />
                      </div>
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {appsLayout === 'table' && (
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/80">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[640px] table-fixed text-left text-xs">
                <thead>
                  <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                    <th className="w-[38%] px-4 py-2.5 font-medium">Project</th>
                    <th className="w-[20%] px-4 py-2.5 font-medium">Status</th>
                    <th className="w-[20%] px-4 py-2.5 font-medium">Containers</th>
                    <th className="w-[16%] px-4 py-2.5 font-medium text-right">Usage</th>
                    <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.05]">
                  {filteredApps.map((app) => (
                    <tr
                      key={app.id}
                      onClick={() => onSelectApp(app.id)}
                      className="hover:bg-white/[0.03] cursor-pointer transition-colors"
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2.5 min-w-0">
                          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 font-display text-[11px] font-bold text-orange-400 uppercase">
                            {app.name.slice(0, 2)}
                          </div>
                          <div className="min-w-0">
                            <div className="truncate font-semibold text-zinc-100">{app.name}</div>
                            {app.primaryDomain && (
                              <div className="truncate font-mono text-[10px] text-zinc-500">
                                {app.primaryDomain}
                              </div>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <StatusBadge status={app.status} />
                      </td>
                      <td className="px-4 py-3 font-mono text-[11px] text-zinc-400">
                        {app.services.filter((s) => s.state === 'running').length}/
                        {app.services.length} online
                      </td>
                      <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-300">
                        {app.cpuPct}% · {app.memMB} MB
                      </td>
                      <td className="px-3 py-3 text-right">
                        <ActionMenu items={getAppActions(app)} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
