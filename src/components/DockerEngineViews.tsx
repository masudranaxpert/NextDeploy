import React, { useState } from 'react';
import {
  Play,
  Square,
  RotateCw,
  Trash2,
  Download,
  Upload,
  FolderOpen,
  Search,
  ExternalLink,
  HardDrive,
  Copy,
  Box,
  Layers,
  LayoutGrid,
  List,
  Terminal,
  Network,
  Clock,
  FileCode2,
  X,
  Plus,
  Info,
  FileText,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  AppStack,
  ContainerService,
  DockerImageItem,
  DockerVolumeItem,
} from '../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from './ui/Primitives';
import { XTermConsole } from './ui/XTermConsole';

// Helper to format long image strings cleanly so tables never scroll horizontally
export const formatCompactImage = (fullImage: string) => {
  const [repoPart, tagPart = 'latest'] = fullImage.split(':');
  const segments = repoPart.split('/');
  const shortName = segments[segments.length - 1];
  const registryPrefix =
    segments.length > 2 ? segments[0] : segments.length === 2 ? segments[0] : 'docker.io';
  return { shortName, tagPart, registryPrefix, fullImage };
};

// Helper to format port mappings compactly (e.g. "127.0.0.1:3000->3000/tcp" => ":3000/tcp")
export const formatCompactPort = (rawPort: string) => {
  if (!rawPort) return 'internal';
  const arrowIdx = rawPort.indexOf('->');
  if (arrowIdx !== -1) {
    return `:${rawPort.slice(arrowIdx + 2)}`;
  }
  return rawPort.startsWith(':') ? rawPort : `:${rawPort}`;
};

// ----------------------------------------------------------------------------
// 1. Containers Page (/containers)
// - Zero horizontal scroll on desktop
// - Merged Container/App column + Merged Image & Port column
// - Container Inspect ShadcnDialog Modal + "Copy Container ID" in 3-dot menu
// ----------------------------------------------------------------------------
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

  const toggleContainerState = (appId: string, containerId: string) => {
    const targetApp = apps.find((a) => a.id === appId);
    if (!targetApp) return;
    const updatedServices = targetApp.services.map((s) => {
      if (s.id !== containerId) return s;
      const nextState = s.state === 'running' ? ('exited' as const) : ('running' as const);
      toast.success(`${nextState === 'running' ? 'Started' : 'Stopped'} ${s.name}`);
      return {
        ...s,
        state: nextState,
        uptime: nextState === 'running' ? 'Just started' : 'Exited (0)',
        cpuPct: nextState === 'running' ? 4.2 : 0,
      };
    });
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
        <table className="w-full table-fixed text-left text-xs">
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
                  {/* Column 1: Merged Container + Colored Project */}
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

                  {/* Column 2: Compact Image + Port Pill */}
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

                  {/* Column 3: Status + Uptime */}
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <StatusBadge status={c.state} />
                      <span className="font-mono text-[11px] text-zinc-500 truncate">
                        {c.uptime.replace('Up ', '')}
                      </span>
                    </div>
                  </td>

                  {/* Column 4: CPU & Memory */}
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-[11px]">
                    <span className="text-zinc-200">{c.cpuPct}%</span>
                    <span className="mx-1 text-zinc-600">·</span>
                    <span className="text-zinc-400">{c.memUsedMB}M</span>
                  </td>

                  {/* Column 5: 3-Dot Action Menu */}
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
                          onClick: () => toast.success(`Restarted ${c.name}`),
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

      {/* Container Inspect Modal (ShadcnDialog) */}
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

// ----------------------------------------------------------------------------
// 2. Images Page (/images)
// ----------------------------------------------------------------------------
interface ImagesViewProps {
  images: DockerImageItem[];
  onDeleteImage: (id: string) => void;
  onPruneUnused: () => void;
  onPullImage?: (repo: string, tag: string) => void;
}

export const ImagesView: React.FC<ImagesViewProps> = ({
  images,
  onDeleteImage,
  onPruneUnused,
  onPullImage,
}) => {
  const [search, setSearch] = useState('');
  const [usageFilter, setUsageFilter] = useState<'all' | 'in-use' | 'unused'>('all');
  const [pullModalOpen, setPullModalOpen] = useState(false);
  const [pullInput, setPullInput] = useState('redis:7-alpine');
  const [inspectedImage, setInspectedImage] = useState<DockerImageItem | null>(null);

  const unusedCount = images.filter((img) => img.inUseBy.length === 0).length;

  const filteredImages = images.filter((img) => {
    if (usageFilter === 'in-use' && img.inUseBy.length === 0) return false;
    if (usageFilter === 'unused' && img.inUseBy.length > 0) return false;
    if (
      search &&
      !img.repository.toLowerCase().includes(search.toLowerCase()) &&
      !img.tag.toLowerCase().includes(search.toLowerCase())
    ) {
      return false;
    }
    return true;
  });

  const handlePullSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const raw = pullInput.trim();
    if (!raw) return;
    const [repo, tag = 'latest'] = raw.split(':');
    if (onPullImage) {
      onPullImage(repo, tag);
    } else {
      toast.success(`Pulled ${repo}:${tag}`);
    }
    setPullModalOpen(false);
    setPullInput('');
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Layers className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Images</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {filteredImages.length}
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
              placeholder="Search images..."
              className="h-8 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-3 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none"
            />
          </div>

          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            {(
              [
                { id: 'all', label: 'All' },
                { id: 'in-use', label: 'In Use' },
                { id: 'unused', label: 'Unused' },
              ] as const
            ).map((f) => (
              <button
                key={f.id}
                type="button"
                onClick={() => setUsageFilter(f.id)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer whitespace-nowrap ${
                  usageFilter === f.id
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={() => setPullModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer whitespace-nowrap"
          >
            <Download className="h-3.5 w-3.5" />
            Pull Image
          </button>

          <button
            type="button"
            onClick={onPruneUnused}
            disabled={unusedCount === 0}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-rose-500/25 bg-rose-500/10 px-3 text-xs font-medium text-rose-300 hover:bg-rose-500/20 disabled:opacity-40 cursor-pointer whitespace-nowrap"
          >
            <Trash2 className="h-3.5 w-3.5" />
            Prune ({unusedCount})
          </button>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[42%] px-4 py-2.5 font-medium">Image & Tag</th>
              <th className="w-[26%] px-4 py-2.5 font-medium">Status & Usage</th>
              <th className="w-[14%] px-4 py-2.5 font-medium">Created</th>
              <th className="w-[12%] px-4 py-2.5 font-medium text-right">Size</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {filteredImages.map((img) => {
              const useCount = img.inUseBy.length;
              const segments = img.repository.split('/');
              const shortRepo = segments[segments.length - 1];
              const hostPrefix = segments.length > 1 ? segments.slice(0, -1).join('/') : 'docker.io';

              return (
                <tr
                  key={img.id}
                  onClick={() => setInspectedImage(img)}
                  className="hover:bg-white/[0.03] transition-colors cursor-pointer"
                >
                  <td className="px-4 py-3">
                    <div
                      className="flex items-center gap-2.5 min-w-0"
                      title={`${img.repository}:${img.tag}`}
                    >
                      <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
                        <Layers className="h-3.5 w-3.5 text-orange-400" />
                      </div>
                      <div className="min-w-0 truncate font-mono">
                        <span className="text-zinc-500 text-[11px]">{hostPrefix}/</span>
                        <span className="font-semibold text-zinc-100">{shortRepo}</span>
                        <span className="text-orange-400">:{img.tag}</span>
                      </div>
                    </div>
                  </td>

                  <td className="px-4 py-3">
                    <div
                      className="flex items-center gap-2"
                      title={
                        useCount > 0
                          ? `Used by: ${img.inUseBy.join(', ')}`
                          : 'Not used by any container'
                      }
                    >
                      <StatusBadge
                        status={useCount > 0 ? 'in-use' : 'unused'}
                        label={useCount > 0 ? 'In use' : 'Unused'}
                      />
                      <span className="inline-flex items-center gap-1 rounded-md border border-white/[0.07] bg-[#09090b] px-2 py-0.5 font-mono text-[11px] text-zinc-300">
                        <Box className="h-3 w-3 text-orange-400" />
                        {useCount} {useCount === 1 ? 'container' : 'containers'}
                      </span>
                    </div>
                  </td>

                  <td className="px-4 py-3 text-zinc-400">
                    <span className="inline-flex items-center gap-1 font-mono text-[11px]">
                      <Clock className="h-3 w-3 text-zinc-500" />
                      {img.created}
                    </span>
                  </td>

                  <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                    {img.sizeMB.toFixed(1)} MB
                  </td>

                  <td className="px-3 py-3 text-right">
                    <ActionMenu
                      items={[
                        {
                          label: 'Inspect Layers',
                          icon: <Info className="h-3.5 w-3.5" />,
                          onClick: () => setInspectedImage(img),
                        },
                        {
                          label: 'Copy Image ID',
                          icon: <Copy className="h-3.5 w-3.5" />,
                          onClick: () => {
                            navigator.clipboard?.writeText(img.id);
                            toast.success(`Copied Image ID: ${img.id}`);
                          },
                        },
                        {
                          label: 'Copy Image Tag',
                          icon: <FileCode2 className="h-3.5 w-3.5" />,
                          onClick: () => {
                            navigator.clipboard?.writeText(`${img.repository}:${img.tag}`);
                            toast.success(`Copied ${img.repository}:${img.tag}`);
                          },
                        },
                        {
                          label: 'Delete Image',
                          icon: <Trash2 className="h-3.5 w-3.5" />,
                          variant: 'danger',
                          disabled: useCount > 0,
                          onClick: () => onDeleteImage(img.id),
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

      {/* Pull Image ShadcnDialog Modal */}
      <ShadcnDialog
        open={pullModalOpen}
        onClose={() => setPullModalOpen(false)}
        title="Pull Docker Image"
        icon={<Download className="h-4 w-4" />}
      >
        <form onSubmit={handlePullSubmit} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Image Reference
            </label>
            <input
              type="text"
              required
              value={pullInput}
              onChange={(e) => setPullInput(e.target.value)}
              placeholder="e.g. postgres:16-alpine or ghcr.io/org/app:latest"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div>
            <span className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Quick Presets
            </span>
            <div className="flex flex-wrap gap-1.5">
              {[
                'postgres:16-alpine',
                'redis:7.4-alpine',
                'caddy:2.8-alpine',
                'nginx:1.27-alpine',
              ].map((preset) => (
                <button
                  key={preset}
                  type="button"
                  onClick={() => setPullInput(preset)}
                  className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-1 font-mono text-[11px] text-zinc-300 hover:border-orange-500/40 hover:text-orange-300 cursor-pointer"
                >
                  {preset}
                </button>
              ))}
            </div>
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setPullModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <Download className="h-3.5 w-3.5" />
              docker pull
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* Inspect Image ShadcnDialog Modal */}
      <ShadcnDialog
        open={Boolean(inspectedImage)}
        onClose={() => setInspectedImage(null)}
        title={inspectedImage ? `${inspectedImage.repository}:${inspectedImage.tag}` : 'Image'}
        icon={<Layers className="h-4 w-4" />}
      >
        {inspectedImage && (
          <div className="space-y-3.5 font-mono text-xs">
            <div className="grid grid-cols-3 gap-2.5">
              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-2.5">
                <span className="font-sans text-[10px] uppercase text-zinc-500">Image ID</span>
                <div className="mt-1 text-zinc-100 truncate">{inspectedImage.id}</div>
              </div>
              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-2.5">
                <span className="font-sans text-[10px] uppercase text-zinc-500">Arch</span>
                <div className="mt-1 text-emerald-400">linux/amd64</div>
              </div>
              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-2.5">
                <span className="font-sans text-[10px] uppercase text-zinc-500">Size</span>
                <div className="mt-1 text-orange-400">{inspectedImage.sizeMB.toFixed(1)} MB</div>
              </div>
            </div>

            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 space-y-1.5">
              <div className="font-sans text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                OCI Layers
              </div>
              <div className="flex justify-between text-[11px] text-zinc-300">
                <span>ADD alpine-minirootfs-3.20.3-x86_64.tar.gz</span>
                <span className="text-zinc-500">7.8 MB</span>
              </div>
              <div className="flex justify-between text-[11px] text-zinc-300">
                <span>RUN apk add --no-cache ca-certificates tzdata</span>
                <span className="text-zinc-500">4.2 MB</span>
              </div>
              <div className="flex justify-between text-[11px] text-zinc-300">
                <span>COPY --from=builder /app/dist /app</span>
                <span className="text-zinc-500">
                  {Math.max(12, inspectedImage.sizeMB - 12).toFixed(1)} MB
                </span>
              </div>
            </div>
          </div>
        )}
      </ShadcnDialog>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 3. Volumes Page (/volumes)
// ----------------------------------------------------------------------------
interface VolumesViewProps {
  volumes: DockerVolumeItem[];
  onCreateVolume?: (vol: DockerVolumeItem) => void;
}

export const VolumesView: React.FC<VolumesViewProps> = ({ volumes, onCreateVolume }) => {
  const [selectedVolume, setSelectedVolume] = useState<DockerVolumeItem | null>(null);
  const [viewMode, setViewMode] = useState<'table' | 'grid'>('table');
  const [search, setSearch] = useState('');
  const [createVolOpen, setCreateVolOpen] = useState(false);
  const [newVolName, setNewVolName] = useState('');
  const [newVolApp, setNewVolApp] = useState('commerce-core-api');
  const [restoreVolTarget, setRestoreVolTarget] = useState<DockerVolumeItem | null>(null);
  const [restoreFileName, setRestoreFileName] = useState('pgdata_2026-10-19.tar.gz');

  const filteredVolumes = volumes.filter(
    (v) =>
      !search ||
      v.name.toLowerCase().includes(search.toLowerCase()) ||
      v.appSlug.toLowerCase().includes(search.toLowerCase())
  );

  const getVolumeActions = (v: DockerVolumeItem) => [
    {
      label: 'Browse Files',
      icon: <FolderOpen className="h-3.5 w-3.5" />,
      onClick: () => setSelectedVolume(v),
    },
    {
      label: 'Backup (.tar.gz)',
      icon: <Download className="h-3.5 w-3.5" />,
      onClick: () => toast.success(`Downloading ${v.name}.tar.gz`),
    },
    {
      label: 'Restore Archive',
      icon: <Upload className="h-3.5 w-3.5" />,
      onClick: () => setRestoreVolTarget(v),
    },
    {
      label: 'Copy Mount Path',
      icon: <Copy className="h-3.5 w-3.5" />,
      onClick: () => {
        navigator.clipboard?.writeText(v.mountPoint);
        toast.success('Copied volume mount path');
      },
    },
    {
      label: 'Delete Volume',
      icon: <Trash2 className="h-3.5 w-3.5" />,
      variant: 'danger' as const,
      onClick: () => toast.error(`Volume ${v.name} is mounted by ${v.appSlug}`),
    },
  ];

  const handleCreateVolSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = newVolName.trim().toLowerCase().replace(/[^a-z0-9_-]/g, '_');
    if (!clean) return;
    const created: DockerVolumeItem = {
      name: clean,
      driver: 'local',
      mountPoint: `/var/lib/docker/volumes/${clean}/_data`,
      appSlug: newVolApp,
      sizeMB: 12.4,
      createdAt: 'Just now',
      filesCount: 2,
      sampleFiles: [
        { path: '/data/config.json', size: '1.2 KB', modified: 'Just now' },
        { path: '/data/storage.db', size: '12.4 MB', modified: 'Just now' },
      ],
    };
    if (onCreateVolume) {
      onCreateVolume(created);
    }
    setCreateVolOpen(false);
    setNewVolName('');
    toast.success(`Created volume ${clean}`);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <HardDrive className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Volumes</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {filteredVolumes.length}
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
              placeholder="Search volumes..."
              className="h-8 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-3 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none"
            />
          </div>

          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            <button
              type="button"
              onClick={() => setViewMode('table')}
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer whitespace-nowrap ${
                viewMode === 'table'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <List className="h-3.5 w-3.5" />
              Table
            </button>
            <button
              type="button"
              onClick={() => setViewMode('grid')}
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer whitespace-nowrap ${
                viewMode === 'grid'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <LayoutGrid className="h-3.5 w-3.5" />
              Grid
            </button>
          </div>

          <button
            type="button"
            onClick={() => setCreateVolOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer whitespace-nowrap"
          >
            <Plus className="h-3.5 w-3.5" />
            Create Volume
          </button>
        </div>
      </div>

      {/* Table Format (Default) */}
      {viewMode === 'table' ? (
        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <table className="w-full table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[40%] px-4 py-2.5 font-medium">Volume</th>
                <th className="w-[22%] px-4 py-2.5 font-medium">Project</th>
                <th className="w-[14%] px-4 py-2.5 font-medium">Status</th>
                <th className="w-[10%] px-4 py-2.5 font-medium text-right">Files</th>
                <th className="w-[10%] px-4 py-2.5 font-medium text-right">Size</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {filteredVolumes.map((v) => (
                <tr
                  key={v.name}
                  onClick={() => setSelectedVolume(v)}
                  className="hover:bg-white/[0.03] transition-colors cursor-pointer"
                >
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2.5 min-w-0">
                      <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
                        <HardDrive className="h-3.5 w-3.5 text-orange-400" />
                      </div>
                      <span className="truncate font-mono font-semibold text-zinc-100">
                        {v.name}
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-3 font-mono text-orange-400 truncate">{v.appSlug}</td>
                  <td className="px-4 py-3">
                    <StatusBadge status="active" label="Mounted" />
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-400">
                    {v.filesCount}
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                    {(v.sizeMB / 1024).toFixed(2)} GB
                  </td>
                  <td className="px-3 py-3 text-right">
                    <ActionMenu items={getVolumeActions(v)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid gap-3.5 sm:grid-cols-2 lg:grid-cols-3">
          {filteredVolumes.map((v) => (
            <div
              key={v.name}
              onClick={() => setSelectedVolume(v)}
              className={`flex flex-col justify-between gap-3 rounded-xl border bg-[#111115]/90 p-4 transition-all cursor-pointer ${
                selectedVolume?.name === v.name
                  ? 'border-orange-500/60 ring-1 ring-orange-500/25'
                  : 'border-white/[0.07] hover:border-white/[0.15]'
              }`}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3 min-w-0">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900">
                    <HardDrive className="h-4 w-4 text-orange-400" />
                  </div>
                  <div className="min-w-0">
                    <div className="truncate font-mono text-xs font-semibold text-zinc-100">
                      {v.name}
                    </div>
                    <div className="mt-0.5 font-mono text-[11px] text-orange-400">{v.appSlug}</div>
                  </div>
                </div>

                <ActionMenu items={getVolumeActions(v)} />
              </div>

              <div className="flex items-center justify-between border-t border-white/[0.05] pt-2.5 text-[11px] font-mono text-zinc-400">
                <StatusBadge status="active" label="Mounted" />
                <span>
                  {v.filesCount} files · {(v.sizeMB / 1024).toFixed(2)} GB
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Interactive Volume File Browser */}
      {selectedVolume && (
        <div className="rounded-xl border border-white/[0.1] bg-[#111115] overflow-hidden shadow-xl">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/60 px-4 py-2.5">
            <div className="flex items-center gap-2 min-w-0">
              <FolderOpen className="h-4 w-4 text-orange-400 shrink-0" />
              <span className="font-mono text-xs font-semibold text-zinc-100 truncate">
                {selectedVolume.name}
              </span>
              <span className="rounded-md border border-white/[0.07] bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">
                {selectedVolume.mountPoint}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setRestoreVolTarget(selectedVolume)}
                className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <Upload className="h-3 w-3 text-orange-400" />
                Restore .tar.gz
              </button>
              <button
                type="button"
                onClick={() => toast.success(`Downloading ${selectedVolume.name}.tar.gz`)}
                className="inline-flex h-7 items-center gap-1 rounded-lg bg-zinc-100 px-2.5 text-[11px] font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                <Download className="h-3 w-3" />
                Download Backup
              </button>
              <button
                type="button"
                onClick={() => setSelectedVolume(null)}
                className="rounded-lg p-1 text-zinc-400 hover:bg-white/[0.06] hover:text-white cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>
          <table className="w-full text-left text-xs font-mono">
            <thead>
              <tr className="border-b border-white/[0.06] bg-[#09090b]/30 text-zinc-500">
                <th className="px-4 py-2 font-medium">File Path</th>
                <th className="px-4 py-2 font-medium">Modified</th>
                <th className="px-4 py-2 font-medium text-right">Size</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {selectedVolume.sampleFiles.map((f, idx) => (
                <tr key={idx} className="hover:bg-white/[0.03]">
                  <td className="px-4 py-2.5 text-zinc-100 flex items-center gap-2">
                    <FileText className="h-3.5 w-3.5 text-orange-400 shrink-0" />
                    <span>{f.path}</span>
                  </td>
                  <td className="px-4 py-2.5 text-zinc-400 tabular-nums">{f.modified}</td>
                  <td className="px-4 py-2.5 text-right text-zinc-200 tabular-nums">{f.size}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Create Volume Modal */}
      <ShadcnDialog
        open={createVolOpen}
        onClose={() => setCreateVolOpen(false)}
        title="Create Docker Volume"
        icon={<HardDrive className="h-4 w-4" />}
      >
        <form onSubmit={handleCreateVolSubmit} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Volume Name
            </label>
            <input
              type="text"
              required
              value={newVolName}
              onChange={(e) => setNewVolName(e.target.value)}
              placeholder="e.g. commerce_redis_data"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Attach to Project
            </label>
            <input
              type="text"
              value={newVolApp}
              onChange={(e) => setNewVolApp(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setCreateVolOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Create Volume
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* Restore Volume Modal */}
      <ShadcnDialog
        open={Boolean(restoreVolTarget)}
        onClose={() => setRestoreVolTarget(null)}
        title="Restore Archive into Volume"
        icon={<Upload className="h-4 w-4" />}
      >
        {restoreVolTarget && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              toast.success(`Extracted ${restoreFileName} into ${restoreVolTarget.name}`);
              setRestoreVolTarget(null);
            }}
            className="space-y-4"
          >
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Archive (.tar.gz / .zip)
              </label>
              <input
                type="text"
                value={restoreFileName}
                onChange={(e) => setRestoreFileName(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setRestoreVolTarget(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Restore Archive
              </button>
            </div>
          </form>
        )}
      </ShadcnDialog>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 4. VPS Terminal Page (/terminal) — Powered by XTerm.js (@xterm/xterm)
// ----------------------------------------------------------------------------
export const VpsTerminalView: React.FC = () => {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Terminal className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">Host Terminal</h1>
          <StatusBadge status="running" label="PTY Connected" />
        </div>
        <div className="flex items-center gap-2 font-mono text-[11px] text-zinc-400">
          <span className="rounded-lg border border-white/[0.07] bg-[#111115] px-2.5 py-1">
            Ubuntu 24.04 LTS · Docker 27.3
          </span>
        </div>
      </div>

      <XTermConsole
        hostLabel="root@nextdeploy"
        promptPrefix="root@nextdeploy:~# "
        height={420}
      />
    </div>
  );
};
