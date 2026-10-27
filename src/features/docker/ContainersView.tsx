import React, { useState } from 'react';
import {
  Play,
  Square,
  RotateCw,
  Search,
  ExternalLink,
  Copy,
  Box,
  Server,
  Network,
  FileCode2,
  Info,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import { OwnerBadge } from '../../components/ui/OwnerBadge';
import { backendApi } from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';
import { AppStack, ContainerService } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';
import { formatCompactImage, formatCompactPort } from '../../lib/dockerFormatters';

interface ContainersViewProps {
  apps: AppStack[];
  liveContainers?: any[];
  onOpenAppTab: (appId: string) => void;
  /** Reloads the project and container lists from the server. */
  onRefresh: () => Promise<void> | void;
}

export const ContainersView: React.FC<ContainersViewProps> = ({
  apps,
  liveContainers = [],
  onOpenAppTab,
  onRefresh,
}) => {
  const [search, setSearch] = useState('');
  const [stateFilter, setStateFilter] = useState<'all' | 'running' | 'exited'>('all');
  const [inspectedContainer, setInspectedContainer] = useState<
    (ContainerService & { appId: string; appName: string; owner: string; ownerId?: number; ownerRole?: string }) | null
  >(null);

  // 1. Containers mapped to projects/compose services
  const appContainers = apps.flatMap((app) =>
    app.services.map((svc) => ({
      ...svc,
      appId: app.id,
      appName: app.name,
      owner: app.owner,
      ownerId: app.ownerId,
      ownerRole: app.ownerRole,
    }))
  );

  const seenIds = new Set(appContainers.map((c) => c.id));
  const seenNames = new Set(appContainers.map((c) => c.name.replace(/^\//, '')));

  // 2. Extra containers from Docker engine (PHP runtimes, standalone, system containers)
  const extraContainers = (liveContainers || [])
    .filter((lc: any) => {
      const shortId = (lc.ID || '').slice(0, 12);
      const name = String(lc.Name || '').replace(/^\//, '');
      return !seenIds.has(shortId) && !seenNames.has(name);
    })
    .map((lc: any) => {
      const name = String(lc.Name || '').replace(/^\//, '');
      const matchedApp = apps.find(
        (a) =>
          name === `nd-phpr-${a.id}` ||
          name === `nd-phpr-${a.name}` ||
          name.startsWith(`nd-phpr-${a.id}`) ||
          name.startsWith(a.id) ||
          name.startsWith(a.name)
      );

      const appName = matchedApp?.name || (name.startsWith('nd-php') ? 'PHP Runtime' : 'System');
      const isRunning = (lc.State || '').toLowerCase() === 'running';

      return {
        id: (lc.ID || '').slice(0, 12),
        name: name,
        service: lc.ComposeService || (name.startsWith('nd-phpr-') ? 'php-route' : name.startsWith('nd-php') ? 'php-fpm' : 'container'),
        image: lc.Image || 'unknown',
        state: (isRunning ? 'running' : 'exited') as 'running' | 'exited',
        uptime: lc.Status || (isRunning ? 'Active' : 'Exited'),
        cpuPct: Math.round((lc.cpu_percent || 0) * 100) / 100,
        memUsedMB: lc.memory_mb || 0,
        memLimitMB: 1024,
        ports: lc.Ports || '',
        appId: matchedApp?.id || '',
        appName: appName,
        owner: matchedApp?.owner || 'system',
        ownerId: matchedApp?.ownerId,
        ownerRole: matchedApp?.ownerRole || (matchedApp ? undefined : 'system'),
        system: !matchedApp,
      };
    });

  const allContainers = [...appContainers, ...extraContainers];

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

  // Start, stop and restart go to the server; the list is reloaded from it afterwards.
  const runContainerAction = async (
    c: { appId: string; name: string },
    action: 'start' | 'stop' | 'restart',
  ) => {
    const verb = { start: 'Started', stop: 'Stopped', restart: 'Restarted' }[action];
    let res;
    if (c.appId) {
      res = await backendApi.appContainerAction(c.appId, c.name, action);
    } else {
      res = await backendApi.globalContainerRestart(c.name);
    }
    if (!res.ok) {
      toast.error(res.error || `Could not ${action} ${c.name}`);
      return;
    }
    toast.success(`${verb} ${c.name}`);
    await onRefresh();
  };

  return (
    <div className="space-y-4">
      {/* Header Row */}
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 shrink-0">
            <Box className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2 min-w-0">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100 truncate">Containers</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-xs text-zinc-400 shrink-0">
              {filtered.length}
            </span>
          </div>
        </div>
      </div>

      {/* Toolbar Row */}
      <div className="flex flex-col gap-2.5 md:flex-row md:items-center md:justify-between">
        <div className="relative w-full md:w-72">
          <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter containers..."
            className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-8 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none transition-colors"
          />
          {search && (
            <button
              type="button"
              onClick={() => setSearch('')}
              className="absolute right-2.5 top-2.5 rounded text-zinc-500 hover:text-zinc-200 cursor-pointer"
              title="Clear filter"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        <div className="grid grid-cols-3 w-full md:w-auto md:flex md:items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
          {(['all', 'running', 'exited'] as const).map((st) => (
            <button
              key={st}
              type="button"
              onClick={() => setStateFilter(st)}
              className={`flex items-center justify-center rounded-md px-3 py-1.5 md:py-1 text-xs font-medium capitalize transition-colors cursor-pointer ${
                stateFilter === st
                  ? 'bg-zinc-800 text-zinc-100 font-semibold shadow-2xs'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {st}
            </button>
          ))}
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
                const hasAppPrefix = c.appName && c.name.startsWith(c.appName);
                const suffix = hasAppPrefix ? c.name.slice(c.appName.length) : (c.service ? `-${c.service}` : '');
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
                          {c.appId ? (
                            <Box className="h-3.5 w-3.5 text-orange-400" />
                          ) : (
                            <Server className="h-3.5 w-3.5 text-purple-400" />
                          )}
                        </div>
                        <div className="min-w-0 truncate font-mono text-xs">
                          {c.appId ? (
                            <>
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
                            </>
                          ) : (
                            <span className="font-semibold text-zinc-100" title={c.name}>
                              {c.name}
                            </span>
                          )}
                        </div>
                        <OwnerBadge
                          id={c.ownerId}
                          name={c.owner}
                          role={c.ownerRole}
                          system={!c.appId || c.owner === 'system'}
                        />
                      </div>
                    </td>

                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2 min-w-0" title={c.image}>
                        <span className="truncate font-mono text-xs text-zinc-200">
                          {imgInfo.shortName}
                          <span className="text-orange-400">:{imgInfo.tagPart}</span>
                        </span>
                        {shortPort ? (
                          <span
                            className="inline-flex shrink-0 items-center gap-1 rounded-md border border-white/[0.07] bg-[#09090b] px-1.5 py-0.5 font-mono text-[10px] text-zinc-300 max-w-[140px]"
                            title={c.ports || shortPort}
                          >
                            <Network className="h-2.5 w-2.5 text-emerald-400 shrink-0" />
                            <span className="truncate">{shortPort}</span>
                          </span>
                        ) : null}
                      </div>
                    </td>

                    <td className="px-4 py-3">
                      <div className="flex flex-col items-start gap-0.5 min-w-0">
                        <StatusBadge status={c.state} />
                        {c.uptime && (
                          <span
                            className="font-mono text-[10px] text-zinc-400 truncate max-w-full"
                            title={c.uptime}
                          >
                            {c.uptime.replace(/^Up\s+/i, '')}
                          </span>
                        )}
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
                          ...(c.appId
                            ? [
                                {
                                  label: 'Open Project',
                                  icon: <ExternalLink className="h-3.5 w-3.5" />,
                                  onClick: () => onOpenAppTab(c.appId),
                                },
                              ]
                            : []),
                          {
                            label: 'Copy Container ID',
                            icon: <Copy className="h-3.5 w-3.5" />,
                            onClick: async () => {
                              const ok = await copyToClipboard(c.id);
                              if (ok) toast.success(`Copied ID: ${c.id}`);
                            },
                          },
                          {
                            label: 'Copy Image Tag',
                            icon: <FileCode2 className="h-3.5 w-3.5" />,
                            onClick: async () => {
                              const ok = await copyToClipboard(c.image);
                              if (ok) toast.success(`Copied ${c.image}`);
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
                            onClick: () => void runContainerAction(c, c.state === 'running' ? 'stop' : 'start'),
                          },
                          {
                            label: 'Restart Container',
                            icon: <RotateCw className="h-3.5 w-3.5" />,
                            onClick: () => void runContainerAction(c, 'restart'),
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
                    onClick={async () => {
                      const ok = await copyToClipboard(inspectedContainer.id);
                      if (ok) toast.success('Copied ID');
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
                <span className="text-orange-400 truncate max-w-[260px]" title={inspectedContainer.ports}>
                  {inspectedContainer.ports || '—'}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-500">Bridge Network</span>
                <span className="text-emerald-400">nextdeploy_net</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-zinc-500">Restart Policy</span>
                <span className="text-zinc-300">unless-stopped</span>
              </div>
            </div>

            {inspectedContainer.appId ? (
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
            ) : null}
          </div>
        )}
      </ShadcnDialog>
    </div>
  );
};
