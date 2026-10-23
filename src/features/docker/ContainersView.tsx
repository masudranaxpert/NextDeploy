import React, { useState } from 'react';
import {
  Play,
  Square,
  Repeat,
  RotateCw,
  Search,
  ExternalLink,
  Copy,
  Box,
  Network,
  FileCode2,
  Info,
  Trash2,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, ContainerService } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';
import { formatCompactImage, formatCompactPort, formatCompactUptime } from '../../lib/dockerFormatters';
import { backendApi } from '../../lib/backendApi';

interface ContainersViewProps {
  apps: AppStack[];
  onOpenAppTab: (appId: string) => void;
  onUpdateApp: (app: AppStack) => void;
}

export const ContainersView: React.FC<ContainersViewProps> = ({
  apps,
  onOpenAppTab,
  onUpdateApp,
}) => {
  const [search, setSearch] = useState('');
  const [stateFilter, setStateFilter] = useState<'all' | 'running' | 'exited'>('all');
  const [inspectedContainer, setInspectedContainer] = useState<
    (ContainerService & { appId: string; appName: string; owner: string }) | null
  >(null);

  const allContainers = apps.flatMap((app) =>
    app.services.map((svc) => ({
      ...svc,
      appId: app.id,
      appName: app.name,
      owner: app.owner,
    }))
  );

  const filtered = allContainers.filter((c) => {
    if (stateFilter !== 'all' && c.state !== stateFilter) return false;
    if (
      search &&
      !c.name.toLowerCase().includes(search.toLowerCase()) &&
      !c.appName.toLowerCase().includes(search.toLowerCase()) &&
      !c.image.toLowerCase().includes(search.toLowerCase())
    ) {
      return false;
    }
    return true;
  });

  const toggleContainerState = async (appId: string, containerId: string) => {
    const targetApp = apps.find((a) => a.id === appId);
    if (!targetApp) return;
    const svc = targetApp.services.find((s) => s.id === containerId);
    if (!svc) return;
    const action = svc.state === 'running' ? 'stop' : 'start';
    const res = await backendApi.appContainerAction(appId, svc.name, action);
    if (!res.ok) {
      toast.error(res.error || `Failed to ${action} ${svc.name}`);
      return;
    }
    const nextState = svc.state === 'running' ? ('exited' as const) : ('running' as const);
    toast.success(`${nextState === 'running' ? 'Started' : 'Stopped'} ${svc.name}`);
    const updatedServices = targetApp.services.map((s) =>
      s.id !== containerId
        ? s
        : {
            ...s,
            state: nextState,
            uptime: nextState === 'running' ? 'Just started' : 'Exited (0)',
            cpuPct: nextState === 'running' ? 4.2 : 0,
          }
    );
    onUpdateApp({ ...targetApp, services: updatedServices });
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Box className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Containers</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {filtered.length}
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Filter containers..."
              className="h-8 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-3 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none"
            />
          </div>
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            {(['all', 'running', 'exited'] as const).map((st) => (
              <button
                key={st}
                type="button"
                onClick={() => setStateFilter(st)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium capitalize transition-colors cursor-pointer whitespace-nowrap ${
                  stateFilter === st
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {st}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[680px] table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[34%] px-4 py-2.5 font-medium">Container / Project</th>
                <th className="w-[28%] px-4 py-2.5 font-medium">Image & Port</th>
                <th className="w-[18%] px-4 py-2.5 font-medium">Status</th>
                <th className="w-[13%] px-4 py-2.5 font-medium text-right">Usage</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {filtered.map((c) => {
                const hasAppPrefix = c.name.startsWith(c.appName);
                const suffix = hasAppPrefix ? c.name.slice(c.appName.length) : `-${c.service}`;
                const imgInfo = formatCompactImage(c.image);
                const shortPort = formatCompactPort(c.ports);

                return (
                  <tr
                    key={c.id}
                    onClick={() => setInspectedContainer(c)}
                    className="hover:bg-white/[0.03] transition-colors cursor-pointer"
                  >
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
                          <Box className="h-3.5 w-3.5 text-orange-400" />
                        </div>
                        <div className="min-w-0 truncate font-mono text-xs">
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation();
                              onOpenAppTab(c.appId);
                            }}
                            className="font-semibold text-orange-400 hover:text-orange-300 hover:underline cursor-pointer"
                            title={`Open ${c.appName}`}
                          >
                            {c.appName}
                          </button>
                          <span className="font-semibold text-zinc-100">{suffix}</span>
                        </div>
                      </div>
                    </td>

                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2 min-w-0" title={c.image}>
                        <span className="truncate font-mono text-xs text-zinc-200">
                          {imgInfo.shortName}
                          <span className="text-orange-400">:{imgInfo.tagPart}</span>
                        </span>
                        <span className="inline-flex shrink-0 items-center gap-1 rounded-md border border-white/[0.07] bg-[#09090b] px-1.5 py-0.5 font-mono text-[10px] text-zinc-300">
                          <Network className="h-2.5 w-2.5 text-emerald-400" />
                          {shortPort}
                        </span>
                      </div>
                    </td>

                    <td className="px-4 py-3 min-w-0">
                      <div className="flex flex-col gap-1 min-w-0">
                        <div className="flex items-center">
                          <StatusBadge status={c.state} />
                        </div>
                        <span
                          className="font-mono text-[11px] text-zinc-400 truncate"
                          title={c.uptime}
                        >
                          {formatCompactUptime(c.uptime)}
                        </span>
                      </div>
                    </td>

                    <td className="px-4 py-3 text-right font-mono tabular-nums text-[11px]">
                      <span className="text-zinc-200">{c.cpuPct}%</span>
                      <span className="mx-1 text-zinc-600">·</span>
                      <span className="text-zinc-400">{c.memUsedMB}M</span>
                    </td>

                    <td className="px-3 py-3 text-right">
                      <ActionMenu
                        items={[
                          {
                            label: 'Inspect Container',
                            icon: <Info className="h-3.5 w-3.5" />,
                            onClick: () => setInspectedContainer(c),
                          },
                          {
                            label: 'Open Project',
                            icon: <ExternalLink className="h-3.5 w-3.5" />,
                            onClick: () => onOpenAppTab(c.appId),
                          },
                          {
                            label: 'Copy Container ID',
                            icon: <Copy className="h-3.5 w-3.5" />,
                            onClick: () => {
                              navigator.clipboard?.writeText(c.id);
                              toast.success(`Copied ID: ${c.id}`);
                            },
                          },
                          {
                            label: 'Copy Image Tag',
                            icon: <FileCode2 className="h-3.5 w-3.5" />,
                            onClick: () => {
                              navigator.clipboard?.writeText(c.image);
                              toast.success(`Copied ${c.image}`);
                            },
                          },
                          {
                            label: c.state === 'running' ? 'Stop Container' : 'Start Container',
                            icon:
                              c.state === 'running' ? (
                                <Square className="h-3.5 w-3.5" />
                              ) : (
                                <Play className="h-3.5 w-3.5" />
                              ),
                            onClick: () => toggleContainerState(c.appId, c.id),
                          },
                          {
                            label: 'Restart Container',
                            icon: <RotateCw className="h-3.5 w-3.5" />,
                            onClick: async () => {
                              const res = await backendApi.appContainerAction(
                                c.appId,
                                c.name,
                                'restart'
                              );
                              if (!res.ok) {
                                toast.error(res.error || `Failed to restart ${c.name}`);
                                return;
                              }
                              toast.success(`Restarted ${c.name}`);
                            },
                          },
                          {
                            label: 'Delete Container',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger' as const,
                            onClick: async () => {
                              const res = await backendApi.globalContainerAction(c.name, 'remove');
                              if (!res.ok) {
                                toast.error(res.error || `Failed to remove ${c.name}`);
                                return;
                              }
                              const targetApp = apps.find((a) => a.id === c.appId);
                              if (targetApp) {
                                onUpdateApp({
                                  ...targetApp,
                                  services: targetApp.services.filter((s) => s.id !== c.id),
                                });
                              }
                              toast.success(`Removed container ${c.name}`);
                            },
                          },
                        ]}
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      <ShadcnDialog
        open={Boolean(inspectedContainer)}
        onClose={() => setInspectedContainer(null)}
        title={inspectedContainer?.name || 'Container Inspect'}
        icon={<Box className="h-4 w-4" />}
      >
        {inspectedContainer && (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                  Container ID
                </span>
                <div className="mt-1 flex items-center justify-between font-mono text-zinc-100">
                  <span>{inspectedContainer.id}</span>
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard?.writeText(inspectedContainer.id);
                      toast.success('Copied ID');
                    }}
                    className="text-orange-400 hover:underline cursor-pointer"
                  >
                    Copy
                  </button>
                </div>
              </div>
              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                  Status
                </span>
                <div className="mt-1">
                  <StatusBadge status={inspectedContainer.state} />
                </div>
              </div>
            </div>

            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 space-y-2 font-mono">
              <div className="flex items-center justify-between">
                <span className="text-zinc-500">Image</span>
                <span className="text-zinc-100 truncate max-w-[260px]">
                  {inspectedContainer.image}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-500">Port Mapping</span>
                <span className="text-orange-400">{inspectedContainer.ports}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-500">Bridge Network</span>
                <span className="text-emerald-400">nextdeploy_net (172.18.0.14)</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-500">Restart Policy</span>
                <span className="text-zinc-300">unless-stopped</span>
              </div>
            </div>

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => {
                  const id = inspectedContainer.appId;
                  setInspectedContainer(null);
                  onOpenAppTab(id);
                }}
                className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                <ExternalLink className="h-3.5 w-3.5" />
                Open Project Workspace
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>
    </div>
  );
};
