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
  ChevronRight,
  RotateCw,
  CheckCircle2,
  AlertTriangle,
} from 'lucide-react';
import { toast } from 'sonner';
import { OwnerBadge } from '../../components/ui/OwnerBadge';
import { DockerVolumeItem } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';

interface VolumesViewProps {
  volumes: DockerVolumeItem[];
  onDeleteVolume?: (name: string) => void | Promise<void>;
}

export const VolumesView: React.FC<VolumesViewProps> = ({ volumes, onDeleteVolume }) => {
  const [selectedVolume, setSelectedVolume] = useState<DockerVolumeItem | null>(null);
  const [browsePath, setBrowsePath] = useState('');
  const [parentPath, setParentPath] = useState('');
  const [isBrowsing, setIsBrowsing] = useState(false);
  const [browseError, setBrowseError] = useState<string | null>(null);
  const [liveBrowseEntries, setLiveBrowseEntries] = useState<
    Array<{ name: string; is_dir: boolean; rel_path: string }> | null
  >(null);

  const [viewMode, setViewMode] = useState<'table' | 'grid'>('table');
  const [search, setSearch] = useState('');

  // Multipart Volume Restore Modal & Live Progress State (POST /volumes/restore + GET /volumes/restore/status)
  const [restoreModalOpen, setRestoreModalOpen] = useState(false);
  const [restoreVolName, setRestoreVolName] = useState('');
  const [restoreFileObj, setRestoreFileObj] = useState<File | null>(null);
  const [restoreStatus, setRestoreStatus] = useState<'idle' | 'running' | 'done' | 'error'>('idle');
  const [restorePercent, setRestorePercent] = useState(0);
  const [restoreError, setRestoreError] = useState<string | null>(null);

  const loadVolumeBrowse = async (vol: DockerVolumeItem, rel = '') => {
    setSelectedVolume(vol);
    setBrowsePath(rel);
    setBrowseError(null);
    setIsBrowsing(true);
    const res = await backendApi.volumeBrowse(vol.name, rel);
    setIsBrowsing(false);

    if (res.ok && res.data) {
      if (res.data.error) {
        setBrowseError(res.data.error);
      }
      setBrowsePath(res.data.path || rel);
      setParentPath(res.data.parent_path || '');
      if (Array.isArray(res.data.entries)) {
        setLiveBrowseEntries(res.data.entries);
        return;
      }
    }
    setLiveBrowseEntries(null);
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

  const openRestoreModal = (volName = '') => {
    setRestoreVolName(volName || volumes[0]?.name || '');
    setRestoreFileObj(null);
    setRestoreStatus('idle');
    setRestorePercent(0);
    setRestoreError(null);
    setRestoreModalOpen(true);
  };

  const handleRestoreSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const targetName = restoreVolName.trim();
    if (!targetName) {
      toast.error('Enter a target Docker volume name');
      return;
    }
    if (!restoreFileObj) {
      toast.error('Select a .tar.gz, .tgz, or .tar archive to upload');
      return;
    }

    setRestoreStatus('running');
    setRestorePercent(12);
    setRestoreError(null);

    const uploadRes = await backendApi.volumeRestoreUpload(targetName, restoreFileObj);
    if (uploadRes.ok && uploadRes.job_id) {
      const jobId = uploadRes.job_id;
      const pollInterval = window.setInterval(async () => {
        const st = await backendApi.volumeRestoreStatus(jobId);
        if (!st.ok || !st.data) {
          window.clearInterval(pollInterval);
          setRestoreStatus('error');
          setRestoreError(st.error || 'Lost contact with the server while extracting');
          toast.error('Lost contact with the server while extracting');
          return;
        }
        const pct = st.data.extract_percent ?? 50;
        setRestorePercent(Math.max(15, Math.min(100, pct)));
        if (st.data.status === 'done') {
          window.clearInterval(pollInterval);
          setRestoreStatus('done');
          setRestorePercent(100);
          toast.success(`Restored archive into volume "${targetName}"`);
        } else if (st.data.status === 'error') {
          window.clearInterval(pollInterval);
          setRestoreStatus('error');
          setRestoreError(st.data.error || 'Archive extraction failed');
          toast.error(st.data.error || 'Archive extraction failed');
        }
      }, 900);
      return;
    }

    setRestoreStatus('error');
    setRestoreError(uploadRes.error || 'The upload did not complete');
    toast.error(uploadRes.error || 'The upload did not complete');
  };

  const filteredVolumes = volumes.filter(
    (v) =>
      !search ||
      v.name.toLowerCase().includes(search.toLowerCase()) ||
      v.appSlug.toLowerCase().includes(search.toLowerCase())
  );

  const getVolumeActions = (v: DockerVolumeItem) => [
    {
      label: 'Browse Volume Files',
      icon: <FolderOpen className="h-3.5 w-3.5" />,
      onClick: () => void loadVolumeBrowse(v, ''),
    },
    {
      label: 'Download Backup (.tar.gz)',
      icon: <Download className="h-3.5 w-3.5" />,
      onClick: () => downloadVolumeTar(v.name),
    },
    {
      label: 'Restore Archive (.tar.gz)',
      icon: <Upload className="h-3.5 w-3.5" />,
      onClick: () => openRestoreModal(v.name),
    },
    {
      label: 'Copy Mount Path',
      icon: <Copy className="h-3.5 w-3.5" />,
      onClick: async () => {
        const ok = await copyToClipboard(v.mountPoint);
        if (ok) toast.success('Copied volume mount path');
      },
    },
    {
      label: 'Delete Volume',
      icon: <Trash2 className="h-3.5 w-3.5" />,
      variant: 'danger' as const,
      onClick: () => {
        if (window.confirm(`Delete volume ${v.name}? Its data is lost.`)) onDeleteVolume?.(v.name);
      },
    },
  ];

  const breadcrumbSegments = browsePath ? browsePath.split('/').filter(Boolean) : [];

  return (
    <div className="space-y-4">
      {/* Header Row: Title & Count on Left, Layout & Action on Right */}
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 shrink-0">
            <HardDrive className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2 min-w-0">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100 truncate">Volumes</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-xs tabular-nums text-zinc-400 shrink-0">
              {filteredVolumes.length}
            </span>
          </div>
        </div>

        {/* View Toggle + Restore Button */}
        <div className="flex items-center gap-2 shrink-0">
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            <button
              type="button"
              onClick={() => setViewMode('table')}
              title="Table View"
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-colors whitespace-nowrap ${
                viewMode === 'table'
                  ? 'bg-zinc-800 text-zinc-100 font-semibold shadow-2xs'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <List className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">Table</span>
            </button>
            <button
              type="button"
              onClick={() => setViewMode('grid')}
              title="Grid View"
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-colors whitespace-nowrap ${
                viewMode === 'grid'
                  ? 'bg-zinc-800 text-zinc-100 font-semibold shadow-2xs'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <LayoutGrid className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">Grid</span>
            </button>
          </div>

          <button
            type="button"
            onClick={() => openRestoreModal('')}
            className="inline-flex h-9 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 transition-colors cursor-pointer whitespace-nowrap"
          >
            <Upload className="h-3.5 w-3.5 text-orange-400" />
            <span className="hidden sm:inline">Restore Archive</span>
            <span className="sm:hidden">Restore</span>
          </button>
        </div>
      </div>

      {/* Toolbar Row: Full-width Search on Mobile, w-72 on Desktop */}
      <div className="flex flex-col gap-2.5 md:flex-row md:items-center md:justify-between">
        <div className="relative w-full md:w-72">
          <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search volumes..."
            className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-8 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none transition-colors"
          />
          {search && (
            <button
              type="button"
              onClick={() => setSearch('')}
              className="absolute right-2.5 top-2.5 rounded text-zinc-500 hover:text-zinc-200 cursor-pointer"
              title="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
      </div>

      {viewMode === 'table' ? (
        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] table-fixed text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                  <th className="w-[38%] px-4 py-2.5 font-medium">Volume</th>
                  <th className="w-[22%] px-4 py-2.5 font-medium">Project</th>
                  <th className="w-[14%] px-4 py-2.5 font-medium">Driver</th>
                  <th className="w-[10%] px-4 py-2.5 font-medium text-right">Files</th>
                  <th className="w-[12%] px-4 py-2.5 font-medium text-right">Size</th>
                  <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {filteredVolumes.map((v) => (
                  <tr
                    key={v.name}
                    onClick={() => void loadVolumeBrowse(v, '')}
                    className={`hover:bg-white/[0.03] transition-colors cursor-pointer ${
                      selectedVolume?.name === v.name ? 'bg-orange-500/[0.05]' : ''
                    }`}
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
                    <td className="px-4 py-3">
                      <div className="flex min-w-0 items-center gap-1.5">
                        <span className="truncate font-mono text-orange-400">{v.appSlug}</span>
                        <OwnerBadge id={v.owner?.id} name={v.owner?.name} role={v.owner?.role} system={v.owner?.system} />
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge status="active" label={v.driver || 'local'} />
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-400">
                      {v.filesCount}
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                      {v.sizeMB >= 1024
                        ? `${(v.sizeMB / 1024).toFixed(2)} GB`
                        : `${v.sizeMB.toFixed(1)} MB`}
                    </td>
                    <td className="px-3 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                      <ActionMenu items={getVolumeActions(v)} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        <div className="grid gap-3.5 sm:grid-cols-2 lg:grid-cols-3">
          {filteredVolumes.map((v) => (
            <div
              key={v.name}
              onClick={() => void loadVolumeBrowse(v, '')}
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
                    <div className="mt-0.5 flex items-center gap-1.5">
                      <span className="font-mono text-[11px] text-orange-400">{v.appSlug}</span>
                      <OwnerBadge id={v.owner?.id} name={v.owner?.name} role={v.owner?.role} system={v.owner?.system} />
                    </div>
                  </div>
                </div>

                <div onClick={(e) => e.stopPropagation()}>
                  <ActionMenu items={getVolumeActions(v)} />
                </div>
              </div>

              <div className="flex items-center justify-between border-t border-white/[0.05] pt-2.5 text-[11px] font-mono tabular-nums text-zinc-400">
                <StatusBadge status="active" label={v.driver || 'local'} />
                <span>
                  {v.filesCount} files ·{' '}
                  {v.sizeMB >= 1024
                    ? `${(v.sizeMB / 1024).toFixed(2)} GB`
                    : `${v.sizeMB.toFixed(1)} MB`}
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Interactive Volume File Browser Drawer / Panel (/volumes/browse) */}
      {selectedVolume && (
        <div className="rounded-xl border border-white/[0.1] bg-[#111115] overflow-hidden shadow-xl">
          <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between border-b border-white/[0.07] bg-[#09090b]/60 px-4 py-3">
            <div className="flex flex-wrap items-center gap-1.5 min-w-0 font-mono text-xs">
              <FolderOpen className="h-4 w-4 text-orange-400 shrink-0" />
              <button
                type="button"
                onClick={() => void loadVolumeBrowse(selectedVolume, '')}
                className="font-semibold text-zinc-100 hover:text-orange-400 cursor-pointer"
              >
                {selectedVolume.name}
              </button>
              <span className="text-zinc-600">/</span>
              {breadcrumbSegments.map((seg, i) => {
                const rel = breadcrumbSegments.slice(0, i + 1).join('/');
                return (
                  <React.Fragment key={rel}>
                    <button
                      type="button"
                      onClick={() => void loadVolumeBrowse(selectedVolume, rel)}
                      className="text-zinc-300 hover:text-orange-400 cursor-pointer"
                    >
                      {seg}
                    </button>
                    <ChevronRight className="h-3 w-3 text-zinc-600" />
                  </React.Fragment>
                );
              })}
              {browsePath && (
                <button
                  type="button"
                  onClick={() =>
                    void loadVolumeBrowse(
                      selectedVolume,
                      parentPath ||
                        (browsePath.includes('/')
                          ? browsePath.slice(0, browsePath.lastIndexOf('/'))
                          : '')
                    )
                  }
                  className="ml-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 font-mono text-[10px] text-orange-400 hover:bg-zinc-800 cursor-pointer"
                >
                  .. Up
                </button>
              )}
              <span className="hidden md:inline-block rounded-md border border-white/[0.07] bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">
                {selectedVolume.mountPoint}
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={() => void loadVolumeBrowse(selectedVolume, browsePath)}
                className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
              >
                <RotateCw className={`h-3 w-3 ${isBrowsing ? 'animate-spin' : ''}`} />
                Refresh
              </button>
              <button
                type="button"
                onClick={() => openRestoreModal(selectedVolume.name)}
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
                Download .tar.gz
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

          {browseError && (
            <div className="border-b border-rose-500/20 bg-rose-500/10 px-4 py-2 text-xs font-mono text-rose-300">
              {browseError}
            </div>
          )}

          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs font-mono">
              <thead>
                <tr className="border-b border-white/[0.06] bg-[#09090b]/30 text-zinc-500">
                  <th className="px-4 py-2 font-medium">Entry Name</th>
                  <th className="px-4 py-2 font-medium">Type</th>
                  <th className="px-4 py-2 font-medium text-right">Size</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {liveBrowseEntries ? (
                  liveBrowseEntries.length === 0 ? (
                    <tr>
                      <td colSpan={3} className="px-4 py-6 text-center text-zinc-500">
                        Directory is empty.
                      </td>
                    </tr>
                  ) : (
                    liveBrowseEntries.map((entry) => (
                      <tr
                        key={entry.rel_path}
                        onClick={() => {
                          if (entry.is_dir) {
                            void loadVolumeBrowse(selectedVolume, entry.rel_path);
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
                  )
                ) : (
                  <tr>
                    <td colSpan={3} className="px-4 py-8 text-center text-xs text-zinc-500">
                      No files or directories found in this volume path.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Create Volume Dialog */}

      {/* Multipart Volume Archive Restore Dialog (POST /volumes/restore + GET /volumes/restore/status) */}
      <ShadcnDialog
        open={restoreModalOpen}
        onClose={() => setRestoreModalOpen(false)}
        title="Restore Archive into Docker Volume"
        icon={<Upload className="h-4 w-4 text-orange-400" />}
      >
        <form onSubmit={handleRestoreSubmit} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Target Volume Name (existing or new volume)
            </label>
            <input
              type="text"
              required
              list="existing-docker-volumes"
              value={restoreVolName}
              onChange={(e) => setRestoreVolName(e.target.value)}
              placeholder="e.g. postgres_data"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
            <datalist id="existing-docker-volumes">
              {volumes.map((v) => (
                <option key={v.name} value={v.name} />
              ))}
            </datalist>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Backup Archive (.tar.gz, .tgz, .tar)
            </label>
            <input
              type="file"
              required
              accept=".tar.gz,.tgz,.tar,application/gzip,application/x-tar"
              onChange={(e) => {
                const f = e.target.files?.[0] || null;
                setRestoreFileObj(f);
                setRestoreStatus('idle');
                setRestorePercent(0);
              }}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-300"
            />
          </div>

          {restoreStatus !== 'idle' && (
            <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 space-y-2">
              <div className="flex items-center justify-between font-mono text-xs">
                <span className="flex items-center gap-1.5 text-zinc-200">
                  {restoreStatus === 'running' && (
                    <RotateCw className="h-3.5 w-3.5 animate-spin text-orange-400" />
                  )}
                  {restoreStatus === 'done' && (
                    <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
                  )}
                  {restoreStatus === 'error' && (
                    <AlertTriangle className="h-3.5 w-3.5 text-rose-400" />
                  )}
                  <span>
                    {restoreStatus === 'running'
                      ? 'Extracting archive into volume...'
                      : restoreStatus === 'done'
                        ? 'Volume restore completed'
                        : 'Restore failed'}
                  </span>
                </span>
                <span className="tabular-nums text-zinc-300">{restorePercent}%</span>
              </div>
              <div className="h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
                <div
                  className={`h-full transition-all duration-300 ${
                    restoreStatus === 'error'
                      ? 'bg-rose-500'
                      : restoreStatus === 'done'
                        ? 'bg-emerald-500'
                        : 'bg-orange-500'
                  }`}
                  style={{ width: `${restorePercent}%` }}
                />
              </div>
              {restoreError && (
                <div className="font-mono text-[11px] text-rose-300">{restoreError}</div>
              )}
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setRestoreModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Close
            </button>
            <button
              type="submit"
              disabled={restoreStatus === 'running'}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white disabled:opacity-50 cursor-pointer"
            >
              {restoreStatus === 'running' ? 'Restoring...' : 'Upload & Restore'}
            </button>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};
