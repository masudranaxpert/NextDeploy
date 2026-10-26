import React, { useState } from 'react';
import {
  Box,
  Play,
  Square,
  RotateCw,
  Trash2,
  Copy,
  Globe,
  Terminal as TerminalIcon,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { backendApi } from '../../../lib/backendApi';
import { copyToClipboard } from '../../../lib/clipboard';
import { ActionMenu, StatusBadge } from '../../../components/ui/Primitives';
import {
  formatCompactImage,
  formatCompactPort,
  formatCompactUptime,
  extractContainerPort,
} from '../../../lib/dockerFormatters';

interface ProjectContainersTabProps {
  app: AppStack;
  onRefresh?: () => Promise<void> | void;
  onOpenLogs?: (service: string) => void;
  onOpenTerminal?: (service: string) => void;
  onOpenDomainModal?: (service: string, port: string) => void;
}

export const ProjectContainersTab: React.FC<ProjectContainersTabProps> = ({
  app,
  onRefresh,
  onOpenLogs,
  onOpenTerminal,
  onOpenDomainModal,
}) => {
  const [selectedContainers, setSelectedContainers] = useState<string[]>([]);
  const [isDeletingContainers, setIsDeletingContainers] = useState(false);
  const [containerFilter, setContainerFilter] = useState<'all' | 'running' | 'stopped'>('all');
  const [restartAllBusy, setRestartAllBusy] = useState(false);

  const activeSlotLower = (app.activeSlot || 'blue').toLowerCase() as 'blue' | 'green';
  const containerRunningCount = app.services.filter((s) => s.state === 'running').length;

  const filteredServices =
    containerFilter === 'all'
      ? app.services
      : app.services.filter((s) =>
          containerFilter === 'running' ? s.state === 'running' : s.state !== 'running'
        );

  const restartAllContainers = async () => {
    const running = app.services.filter((s) => s.state === 'running');
    if (running.length === 0) {
      toast.info('No running containers to restart');
      return;
    }
    setRestartAllBusy(true);
    try {
      await Promise.all(
        running.map((s) => backendApi.appContainerAction(app.id, s.name, 'restart'))
      );
      toast.success(`Restarted ${running.length} container(s)`);
      await onRefresh?.();
    } catch {
      toast.error('Failed to restart containers');
    } finally {
      setRestartAllBusy(false);
    }
  };

  const toggleSelectContainer = (containerName: string) => {
    setSelectedContainers((prev) =>
      prev.includes(containerName)
        ? prev.filter((name) => name !== containerName)
        : [...prev, containerName]
    );
  };

  const toggleSelectAllContainers = () => {
    const names = filteredServices.map((s) => s.name || s.service);
    const allSelected = names.length > 0 && names.every((n) => selectedContainers.includes(n));
    setSelectedContainers(
      allSelected
        ? selectedContainers.filter((n) => !names.includes(n))
        : [...new Set([...selectedContainers, ...names])]
    );
  };

  const handleRemoveSelectedContainers = async (targets?: string[]) => {
    const toRemove = targets || selectedContainers;
    if (toRemove.length === 0) return;
    setIsDeletingContainers(true);
    try {
      const res = await backendApi.appContainersRemoveSelected(app.id, toRemove);
      if (res.ok) {
        toast.success(`Removed ${toRemove.length} container(s)`);
        setSelectedContainers((prev) => prev.filter((name) => !toRemove.includes(name)));
        await onRefresh?.();
      } else {
        toast.error(res.error || 'Failed to remove containers');
      }
    } catch (err: any) {
      toast.error(err?.message || 'Failed to remove containers');
    } finally {
      setIsDeletingContainers(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* Top Controls Header */}
      <div className="flex items-center gap-2.5 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-3">
        <input
          type="checkbox"
          checked={
            filteredServices.length > 0 && selectedContainers.length === filteredServices.length
          }
          onChange={toggleSelectAllContainers}
          className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 accent-orange-500 cursor-pointer"
          title="Select all containers"
        />
        <span className="text-sm font-semibold text-zinc-100">Containers</span>
        <span className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2 py-0.5 font-mono text-[11px] text-emerald-300">
          {containerRunningCount}/{app.services.length} running
        </span>
        <button
          type="button"
          onClick={restartAllContainers}
          disabled={restartAllBusy}
          className="ml-auto inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
        >
          <RotateCw
            className={`h-3.5 w-3.5 text-orange-400 ${restartAllBusy ? 'animate-spin' : ''}`}
          />
          Restart All
        </button>
      </div>

      {/* Filter Tabs */}
      <div className="flex items-center gap-0.5 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-1.5">
        {(['all', 'running', 'stopped'] as const).map((f) => {
          const count =
            f === 'all'
              ? app.services.length
              : app.services.filter((s) =>
                  f === 'running' ? s.state === 'running' : s.state !== 'running'
                ).length;
          const isActive = containerFilter === f;
          return (
            <button
              key={f}
              type="button"
              onClick={() => setContainerFilter(f)}
              className={`flex-1 rounded-lg px-3 py-1.5 text-center font-mono text-xs transition-colors cursor-pointer ${
                isActive
                  ? 'border border-white/[0.1] bg-zinc-800 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {f === 'all' ? 'All' : f === 'running' ? 'Running' : 'Stopped'} ({count})
            </button>
          );
        })}
      </div>

      {/* Selection action bar */}
      {selectedContainers.length > 0 && (
        <div className="flex items-center justify-between gap-2 rounded-xl border border-white/[0.07] bg-[#0c0c0f] px-4 py-2.5">
          <span className="text-xs font-medium text-zinc-300">{selectedContainers.length} selected</span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={restartAllBusy}
              onClick={async () => {
                for (const name of selectedContainers) {
                  await backendApi.appContainerAction(app.id, name, 'restart');
                }
                toast.success(`Restarted ${selectedContainers.length} container(s)`);
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
            >
              <RotateCw className="h-3.5 w-3.5 text-orange-400" /> Restart ({selectedContainers.length})
            </button>
            <button
              type="button"
              disabled={isDeletingContainers}
              onClick={() => handleRemoveSelectedContainers()}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 text-xs font-medium text-rose-300 transition-colors hover:bg-rose-500/20 cursor-pointer disabled:opacity-50"
            >
              <Trash2 className="h-3.5 w-3.5" /> Delete ({selectedContainers.length})
            </button>
          </div>
        </div>
      )}

      {/* Desktop Table View */}
      <div className="hidden md:block">
        <div className="overflow-x-auto rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <table className="w-full min-w-[820px] text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/80 font-mono text-[11px] uppercase tracking-wider text-zinc-400">
                <th className="w-9 px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={
                      filteredServices.length > 0 &&
                      selectedContainers.length === filteredServices.length
                    }
                    onChange={toggleSelectAllContainers}
                    className="rounded border-zinc-700 bg-zinc-900 text-orange-500 focus:ring-0 focus:ring-offset-0 cursor-pointer"
                  />
                </th>
                <th className="px-3 py-2.5">Container / Service</th>
                <th className="px-3 py-2.5">Image & Tag</th>
                <th className="px-3 py-2.5">Status & Uptime</th>
                <th className="px-3 py-2.5 text-right">CPU / Mem</th>
                <th className="px-3 py-2.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {filteredServices.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center">
                    <Box className="mx-auto h-8 w-8 text-zinc-600 mb-2" />
                    <p className="text-xs font-medium text-zinc-300">No containers running</p>
                    <p className="text-[11px] text-zinc-500 mt-1">
                      Deploy this project to launch your compose services.
                    </p>
                  </td>
                </tr>
              ) : (
                filteredServices.map((svc) => {
                  const imgInfo = formatCompactImage(svc.image);
                  const shortPort = formatCompactPort(svc.ports);
                  const isSelected = selectedContainers.includes(svc.name || svc.service);

                  return (
                    <tr
                      key={svc.id}
                      className={`transition-colors hover:bg-white/[0.02] ${
                        isSelected ? 'bg-orange-500/[0.04]' : ''
                      }`}
                    >
                      <td className="w-9 px-3 py-2.5 text-center">
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => toggleSelectContainer(svc.name || svc.service)}
                          className="rounded border-zinc-700 bg-zinc-900 text-orange-500 focus:ring-0 focus:ring-offset-0 cursor-pointer"
                        />
                      </td>

                      <td className="px-3 py-2.5 min-w-0">
                        <div className="flex items-center gap-2 min-w-0">
                          <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded border border-white/[0.08] bg-zinc-900">
                            <Box className="h-3 w-3 text-orange-400" />
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-1.5 min-w-0">
                              <span
                                className="font-mono text-xs font-semibold text-zinc-100 truncate"
                                title={svc.name || svc.service}
                              >
                                {svc.name || svc.service}
                              </span>
                              {(() => {
                                const n = (svc.name || '').toLowerCase();
                                const containerSlot = n.includes('-blue-')
                                  ? 'blue'
                                  : n.includes('-green-')
                                  ? 'green'
                                  : null;
                                if (!containerSlot) return null;
                                const isProdSlot = containerSlot === activeSlotLower;
                                return (
                                  <span
                                    className={`shrink-0 rounded border px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase ${
                                      isProdSlot
                                        ? 'border-emerald-500/25 bg-emerald-500/15 text-emerald-400'
                                        : 'border-violet-500/25 bg-violet-500/15 text-violet-300'
                                    }`}
                                  >
                                    {isProdSlot ? 'PROD' : 'PREVIEW'}
                                  </span>
                                );
                              })()}
                            </div>
                            <div className="font-mono text-[10px] text-zinc-500">
                              id: {svc.id ? svc.id.slice(0, 12) : '—'}
                              {svc.service && svc.service !== svc.name && (
                                <span className="text-zinc-600"> · svc: {svc.service}</span>
                              )}
                            </div>
                          </div>
                        </div>
                      </td>

                      <td className="px-3 py-2.5 min-w-0 font-mono text-[11px]">
                        <div className="flex items-center gap-1.5 truncate" title={svc.image}>
                          <span className="truncate text-zinc-300">{imgInfo.shortName}</span>
                          <span className="text-orange-400">:{imgInfo.tagPart}</span>
                        </div>
                        {shortPort && (
                          <div className="mt-0.5 text-[10px] text-zinc-500 truncate">
                            {shortPort}
                          </div>
                        )}
                      </td>

                      <td className="px-3 py-2.5 min-w-0">
                        <div className="flex flex-col items-start gap-1">
                          <StatusBadge status={svc.state} />
                          <span
                            className="max-w-full truncate font-mono text-[11px] text-zinc-400"
                            title={svc.uptime}
                          >
                            {formatCompactUptime(svc.uptime)}
                          </span>
                        </div>
                      </td>

                      <td className="px-3 py-2.5 text-right font-mono tabular-nums text-[11px] whitespace-nowrap">
                        <span className="text-zinc-200">{svc.cpuPct}%</span>
                        <span className="mx-1 text-zinc-600">·</span>
                        <span className="text-zinc-400">{svc.memUsedMB}M</span>
                      </td>

                      <td className="px-3 py-2.5 text-right whitespace-nowrap">
                        <div className="flex items-center justify-end gap-1">
                          <button
                            type="button"
                            title="View logs"
                            onClick={() => onOpenLogs?.(svc.service)}
                            className="rounded border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
                          >
                            Logs
                          </button>
                          <ActionMenu
                            items={[
                              {
                                label: 'Open Shell',
                                icon: <TerminalIcon className="h-3.5 w-3.5" />,
                                onClick: () => onOpenTerminal?.(svc.service),
                              },
                              {
                                label: 'Restart Container',
                                icon: <RotateCw className="h-3.5 w-3.5" />,
                                onClick: async () => {
                                  await backendApi.appContainerAction(app.id, svc.name, 'restart');
                                  toast.success(`Restarted ${svc.name}`);
                                  await onRefresh?.();
                                },
                              },
                              {
                                label: 'Route Domain to Service',
                                icon: <Globe className="h-3.5 w-3.5" />,
                                onClick: () =>
                                  onOpenDomainModal?.(
                                    svc.service,
                                    extractContainerPort(svc.ports)
                                  ),
                              },
                              {
                                label: 'Copy Container ID',
                                icon: <Copy className="h-3.5 w-3.5" />,
                                onClick: () => {
                                  void copyToClipboard(svc.id || svc.name);
                                  toast.success(`Copied ID`);
                                },
                              },
                              {
                                label: 'Delete Container',
                                icon: <Trash2 className="h-3.5 w-3.5" />,
                                variant: 'danger',
                                onClick: () =>
                                  handleRemoveSelectedContainers([svc.name || svc.service]),
                              },
                            ]}
                          />
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Mobile Card View */}
      <div className="space-y-2.5 md:hidden">
        {filteredServices.length === 0 ? (
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-8 text-center">
            <Box className="mx-auto h-8 w-8 text-zinc-600" />
            <p className="mt-2 text-xs font-medium text-zinc-300">No containers to show</p>
          </div>
        ) : (
          filteredServices.map((svc) => {
            const name = svc.name || svc.service;
            const isSelected = selectedContainers.includes(name);
            const n = (svc.name || '').toLowerCase();
            const containerSlot = n.includes('-blue-')
              ? 'blue'
              : n.includes('-green-')
              ? 'green'
              : null;
            const isProdSlot = containerSlot === activeSlotLower;
            const imgInfo = formatCompactImage(svc.image);
            const shortPort = formatCompactPort(svc.ports);

            return (
              <div
                key={svc.id}
                className={`rounded-xl border p-3 space-y-2.5 transition-colors ${
                  isSelected
                    ? 'border-orange-500/40 bg-orange-500/[0.04]'
                    : 'border-white/[0.07] bg-[#111115]/90'
                }`}
              >
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => toggleSelectContainer(name)}
                    className="h-4 w-4 shrink-0 rounded border-zinc-700 bg-zinc-900 accent-orange-500 cursor-pointer"
                    title="Select container"
                  />
                  <StatusBadge status={svc.state} />
                  {containerSlot && (
                    <span
                      className={`shrink-0 rounded border px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase ${
                        isProdSlot
                          ? 'border-emerald-500/25 bg-emerald-500/15 text-emerald-400'
                          : 'border-violet-500/25 bg-violet-500/15 text-violet-300'
                      }`}
                    >
                      {isProdSlot ? 'PROD' : 'PREVIEW'}
                    </span>
                  )}
                  <span className="ml-auto flex shrink-0 items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => onOpenLogs?.(svc.service)}
                      title="View logs"
                      className="inline-flex h-9 items-center rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800 cursor-pointer"
                    >
                      Logs
                    </button>
                    <button
                      type="button"
                      onClick={async () => {
                        await backendApi.appContainerAction(app.id, name, 'restart');
                        toast.success(`Restarted ${name}`);
                        await onRefresh?.();
                      }}
                      title="Restart container"
                      className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-300 transition-colors hover:bg-zinc-800 cursor-pointer"
                    >
                      <RotateCw className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={async () => {
                        await backendApi.appContainerAction(
                          app.id,
                          name,
                          svc.state === 'running' ? 'stop' : 'start'
                        );
                        toast.success(
                          svc.state === 'running' ? `Stopped ${name}` : `Started ${name}`
                        );
                        await onRefresh?.();
                      }}
                      title={svc.state === 'running' ? 'Stop container' : 'Start container'}
                      className={`inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border transition-colors cursor-pointer ${
                        svc.state === 'running'
                          ? 'border-amber-500/30 bg-amber-500/10 text-amber-400 hover:bg-amber-500/20'
                          : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20'
                      }`}
                    >
                      {svc.state === 'running' ? (
                        <Square className="h-3.5 w-3.5" />
                      ) : (
                        <Play className="h-3.5 w-3.5" />
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => handleRemoveSelectedContainers([name])}
                      title="Delete container"
                      className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-rose-500/30 bg-rose-500/10 text-rose-400 transition-colors hover:bg-rose-500/20 cursor-pointer"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </span>
                </div>

                <div className="flex items-center gap-2.5 min-w-0">
                  <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-orange-500/25 bg-orange-500/10">
                    <Box className="h-4 w-4 text-orange-400" />
                  </span>
                  <span
                    className="min-w-0 flex-1 truncate font-mono text-xs font-semibold text-zinc-100"
                    title={name}
                  >
                    {name}
                  </span>
                  {svc.service && svc.service !== svc.name && (
                    <span className="shrink-0 rounded border border-white/[0.08] bg-[#09090b] px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">
                      svc: {svc.service}
                    </span>
                  )}
                </div>

                <div className="flex items-center justify-between gap-2 min-w-0">
                  <span
                    className="min-w-0 truncate font-mono text-[11px] text-zinc-400"
                    title={svc.image}
                  >
                    {imgInfo.shortName}
                    <span className="text-orange-400">:{imgInfo.tagPart}</span>
                  </span>
                  {shortPort && (
                    <span className="shrink-0 rounded border border-emerald-500/30 bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] text-emerald-300">
                      {shortPort}
                    </span>
                  )}
                </div>

                <div className="flex items-center justify-between gap-2 font-mono text-[11px]">
                  <span className="min-w-0 truncate text-zinc-500">
                    {svc.id ? `#${svc.id.slice(0, 8)}` : '—'}
                    {svc.uptime ? ` · ${formatCompactUptime(svc.uptime)}` : ''}
                  </span>
                  <span className="shrink-0 whitespace-nowrap">
                    <span className="text-zinc-200">{svc.cpuPct}%</span>
                    <span className="mx-1 text-zinc-600">·</span>
                    <span className="text-zinc-400">{svc.memUsedMB}M</span>
                  </span>
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};
