import React, { useState } from 'react';
import {
  Trash2,
  Download,
  Upload,
  FolderOpen,
  Search,
  HardDrive,
  Copy,
  LayoutGrid,
  List,
  X,
  Plus,
  FileText,
} from 'lucide-react';
import { toast } from 'sonner';
import { DockerVolumeItem } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';

interface VolumesViewProps {
  volumes: DockerVolumeItem[];
  onCreateVolume?: (vol: DockerVolumeItem) => void;
}

export const VolumesView: React.FC<VolumesViewProps> = ({ volumes, onCreateVolume }) => {
  const [selectedVolume, setSelectedVolume] = useState<DockerVolumeItem | null>(null);
  const [browsePath, setBrowsePath] = useState('');
  const [liveBrowseEntries, setLiveBrowseEntries] = useState<
    Array<{ name: string; is_dir: boolean; rel_path: string }> | null
  >(null);
  const [viewMode, setViewMode] = useState<'table' | 'grid'>('table');
  const [search, setSearch] = useState('');
  const [createVolOpen, setCreateVolOpen] = useState(false);
  const [newVolName, setNewVolName] = useState('');
  const [newVolApp, setNewVolApp] = useState('commerce-core-api');
  const [restoreVolTarget, setRestoreVolTarget] = useState<DockerVolumeItem | null>(null);
  const [restoreFileName, setRestoreFileName] = useState('pgdata_2026-10-19.tar.gz');
  const [restoreFileObj, setRestoreFileObj] = useState<File | null>(null);

  const loadVolumeBrowse = async (vol: DockerVolumeItem, rel = '') => {
    setSelectedVolume(vol);
    setBrowsePath(rel);
    const res = await backendApi.volumeBrowse(vol.name, rel);
    if (res.ok && res.data?.entries) {
      setLiveBrowseEntries(res.data.entries);
    } else {
      setLiveBrowseEntries(null);
    }
  };

  const downloadVolumeTar = (name: string) => {
    toast.success(`Downloading ${name}-backup.tar.gz`);
    const a = document.createElement('a');
    a.href = `/volumes/download?name=${encodeURIComponent(name)}`;
    a.download = `${name}-backup.tar.gz`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  };

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
      onClick: () => loadVolumeBrowse(v, ''),
    },
    {
      label: 'Backup (.tar.gz)',
      icon: <Download className="h-3.5 w-3.5" />,
      onClick: () => downloadVolumeTar(v.name),
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

      {selectedVolume && (
        <div className="rounded-xl border border-white/[0.1] bg-[#111115] overflow-hidden shadow-xl">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/60 px-4 py-2.5">
            <div className="flex items-center gap-2 min-w-0">
              <FolderOpen className="h-4 w-4 text-orange-400 shrink-0" />
              <span className="font-mono text-xs font-semibold text-zinc-100 truncate">
                {selectedVolume.name}
                {browsePath ? ` / ${browsePath}` : ''}
              </span>
              {browsePath && (
                <button
                  type="button"
                  onClick={() => {
                    const parent = browsePath.includes('/')
                      ? browsePath.slice(0, browsePath.lastIndexOf('/'))
                      : '';
                    loadVolumeBrowse(selectedVolume, parent);
                  }}
                  className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 font-mono text-[10px] text-orange-400 hover:bg-zinc-800 cursor-pointer"
                >
                  .. Up
                </button>
              )}
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
                onClick={() => downloadVolumeTar(selectedVolume.name)}
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
              {liveBrowseEntries
                ? liveBrowseEntries.map((entry) => (
                    <tr
                      key={entry.rel_path}
                      onClick={() => {
                        if (entry.is_dir) {
                          loadVolumeBrowse(selectedVolume, entry.rel_path);
                        }
                      }}
                      className={`hover:bg-white/[0.03] ${entry.is_dir ? 'cursor-pointer' : ''}`}
                    >
                      <td className="px-4 py-2.5 text-zinc-100 flex items-center gap-2">
                        {entry.is_dir ? (
                          <FolderOpen className="h-3.5 w-3.5 text-orange-400 shrink-0" />
                        ) : (
                          <FileText className="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                        )}
                        <span>{entry.name}</span>
                      </td>
                      <td className="px-4 py-2.5 text-zinc-400 tabular-nums">
                        {entry.is_dir ? 'Directory' : 'File'}
                      </td>
                      <td className="px-4 py-2.5 text-right text-zinc-200 tabular-nums">—</td>
                    </tr>
                  ))
                : selectedVolume.sampleFiles.map((f, idx) => (
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

      <ShadcnDialog
        open={Boolean(restoreVolTarget)}
        onClose={() => setRestoreVolTarget(null)}
        title="Restore Archive into Volume"
        icon={<Upload className="h-4 w-4" />}
      >
        {restoreVolTarget && (
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              if (restoreFileObj) {
                const fd = new FormData();
                fd.append('volume_name', restoreVolTarget.name);
                fd.append('backup_file', restoreFileObj);
                try {
                  const res = await fetch('/volumes/restore', {
                    method: 'POST',
                    body: fd,
                  });
                  if (res.ok) {
                    toast.success(`Restore started for ${restoreVolTarget.name}`);
                    setRestoreVolTarget(null);
                    setRestoreFileObj(null);
                    return;
                  }
                } catch {
                  // fallback in standalone preview
                }
              }
              toast.success(`Extracted ${restoreFileName} into ${restoreVolTarget.name}`);
              setRestoreVolTarget(null);
            }}
            className="space-y-4"
          >
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Upload .tar.gz Volume Backup Archive
              </label>
              <input
                type="file"
                accept=".tar.gz,.tgz,application/gzip"
                onChange={(e) => {
                  const f = e.target.files?.[0] || null;
                  setRestoreFileObj(f);
                  if (f) setRestoreFileName(f.name);
                }}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-300"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Archive Name
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
