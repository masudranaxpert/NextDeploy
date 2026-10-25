import React, { useState } from 'react';
import {
  Trash2,
  Download,
  Search,
  Copy,
  Box,
  Layers,
  Clock,
  FileCode2,
  Info,
} from 'lucide-react';
import { toast } from 'sonner';
import { OwnerBadge } from '../../components/ui/OwnerBadge';
import { DockerImageItem } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';
import { copyToClipboard } from '../../lib/clipboard';

interface ImagesViewProps {
  images: DockerImageItem[];
  onDeleteImage: (id: string) => void;
  onPruneUnused: () => void;
  onPullImage?: (repo: string, tag: string) => void;
  isAdmin?: boolean;
}

export const ImagesView: React.FC<ImagesViewProps> = ({
  images,
  onDeleteImage,
  onPruneUnused,
  onPullImage,
  isAdmin = true,
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

          {isAdmin && (
            <button
              type="button"
              onClick={() => setPullModalOpen(true)}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer whitespace-nowrap"
            >
              <Download className="h-3.5 w-3.5" />
              Pull Image
            </button>
          )}

          {isAdmin && (
            <button
              type="button"
              onClick={onPruneUnused}
              disabled={unusedCount === 0}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-rose-500/25 bg-rose-500/10 px-3 text-xs font-medium text-rose-300 hover:bg-rose-500/20 disabled:opacity-40 cursor-pointer whitespace-nowrap"
              title="Remove all unused and dangling Docker images"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete Unused ({unusedCount})
            </button>
          )}
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[680px] table-fixed text-left text-xs">
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
                        <OwnerBadge id={img.owner?.id} name={img.owner?.name} role={img.owner?.role} system={img.owner?.system} />
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
                            onClick: async () => {
                              const ok = await copyToClipboard(img.id);
                              if (ok) toast.success(`Copied Image ID: ${img.id}`);
                            },
                          },
                          {
                            label: 'Copy Image Tag',
                            icon: <FileCode2 className="h-3.5 w-3.5" />,
                            onClick: async () => {
                              const tag = `${img.repository}:${img.tag}`;
                              const ok = await copyToClipboard(tag);
                              if (ok) toast.success(`Copied ${tag}`);
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
      </div>

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
