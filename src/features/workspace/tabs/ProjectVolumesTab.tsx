import React, { useState, useEffect } from 'react';
import {
  HardDrive,
  Plus,
  Trash2,
  FolderOpen,
  Download,
  Upload,
  ChevronRight,
  FileText,
  Folder,
  X,
  RefreshCw,
  CheckCircle2,
  AlertCircle,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, AppVolumeMount } from '../../../types/nextdeploy';
import { backendApi } from '../../../lib/backendApi';

interface ProjectVolumesTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
}

export const ProjectVolumesTab: React.FC<ProjectVolumesTabProps> = ({
  app,
  onUpdateApp,
}) => {
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [newVolName, setNewVolName] = useState('');
  const [newVolMountPath, setNewVolMountPath] = useState('/var/lib/postgresql/data');
  const [creating, setCreating] = useState(false);

  // Volume File Browser state
  const [browsingVolume, setBrowsingVolume] = useState<string | null>(null);
  const [browsePath, setBrowsePath] = useState<string>('');
  const [browseEntries, setBrowseEntries] = useState<
    Array<{
      name: string;
      rel_path: string;
      is_dir: boolean;
    }>
  >([]);
  const [browseLoading, setBrowseLoading] = useState(false);

  // Multipart Volume Restore state
  const [restoreTargetVolume, setRestoreTargetVolume] = useState<string | null>(null);
  const [restoreFile, setRestoreFile] = useState<File | null>(null);
  const [restoreJobId, setRestoreJobId] = useState<string | null>(null);
  const [restoreStatus, setRestoreStatus] = useState<{
    status: string;
    extract_percent: number;
    done: boolean;
    error?: string;
  } | null>(null);
  const [uploadingRestore, setUploadingRestore] = useState(false);

  const appPrefix = app.slug.toLowerCase().replace(/[^a-z0-9_-]/g, '');

  const loadVolumeDirectory = async (volName: string, subPath: string) => {
    setBrowseLoading(true);
    setBrowsingVolume(volName);
    setBrowsePath(subPath);
    try {
      const res = await backendApi.volumeBrowse(volName, subPath);
      if (!res.ok) {
        toast.error(res.error || 'Failed to browse volume contents');
        setBrowseEntries([]);
      } else {
        setBrowseEntries(res.data?.entries ?? []);
      }
    } catch (err: any) {
      toast.error(err?.message || 'Failed to browse volume contents');
    } finally {
      setBrowseLoading(false);
    }
  };

  const handleCreateVolume = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newVolName.trim()) return;
    setCreating(true);
    try {
      const fullName = newVolName.trim().startsWith(appPrefix)
        ? newVolName.trim()
        : `${appPrefix}_${newVolName.trim()}`;
      const newMount: AppVolumeMount = {
        name: fullName,
        mountPath: newVolMountPath.trim() || '/data',
        driver: 'local',
        sizeMB: 64,
        lastBackupAt: 'Never',
      };
      onUpdateApp({
        ...app,
        volumes: [...app.volumes, newMount],
      });
      toast.success(`Mounted volume ${fullName}`);
      setNewVolName('');
      setShowCreateModal(false);
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteVolume = (volName: string) => {
    onUpdateApp({
      ...app,
      volumes: app.volumes.filter((v) => v.name !== volName),
    });
    if (browsingVolume === volName) setBrowsingVolume(null);
    toast.success(`Detached volume ${volName}`);
  };

  // Poll volume restore status
  useEffect(() => {
    if (!restoreJobId) return;
    const timer = setInterval(async () => {
      try {
        const res = await backendApi.volumeRestoreStatus(restoreJobId);
        const st = res.data;
        if (st) {
          const isDone = st.status === 'done' || st.status === 'error';
          setRestoreStatus({
            status: st.status || 'running',
            extract_percent:
              typeof st.extract_percent === 'number'
                ? st.extract_percent
                : isDone
                  ? 100
                  : 65,
            done: isDone,
            error: st.error,
          });
          if (isDone) {
            clearInterval(timer);
            if (!st.error) {
              toast.success(`Volume ${restoreTargetVolume} restored`);
            }
          }
        }
      } catch {
        clearInterval(timer);
      }
    }, 1200);
    return () => clearInterval(timer);
  }, [restoreJobId, restoreTargetVolume]);

  const handleStartRestore = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!restoreTargetVolume || !restoreFile) return;
    setUploadingRestore(true);
    setRestoreStatus({
      status: 'uploading',
      extract_percent: 20,
      done: false,
    });
    try {
      const res = await backendApi.volumeRestoreUpload(restoreTargetVolume, restoreFile);
      if (res?.job_id) {
        setRestoreJobId(res.job_id);
        setRestoreStatus({
          status: 'extracting',
          extract_percent: 45,
          done: false,
        });
      } else {
        setRestoreStatus({
          status: 'done',
          extract_percent: 100,
          done: true,
        });
        toast.success(`Archive extracted into ${restoreTargetVolume}`);
      }
    } catch (err: any) {
      toast.error(err?.message || 'Volume restore upload failed');
    } finally {
      setUploadingRestore(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
        <div className="flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-orange-400" />
          <h2 className="text-sm font-semibold text-zinc-100">Persistent Docker Volumes</h2>
          <span className="rounded-md bg-white/[0.06] px-2 py-0.5 text-[11px] font-medium text-zinc-400 tabular-nums">
            {app.volumes.length}
          </span>
        </div>
      </div>

      {/* Volumes List */}
      {app.volumes.length === 0 ? (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-8 text-center">
          <HardDrive className="h-8 w-8 text-zinc-600 mx-auto mb-2" />
          <p className="text-sm font-medium text-zinc-300">
            No persistent volumes mounted for {app.name}
          </p>
          <p className="text-xs text-zinc-500 mt-1">
            Mount a named Docker volume to persist databases or uploaded media across deployments.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3">
          {app.volumes.map((vol) => {
            const mountTarget = vol.mountPath || vol.mountPoint || '/data';
            return (
              <div
                key={vol.name}
                className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 flex flex-col lg:flex-row lg:items-center justify-between gap-4"
              >
                <div className="space-y-1 min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-semibold text-zinc-100 font-mono break-all">
                      {vol.name}
                    </span>
                    <span className="text-[11px] font-mono px-2 py-0.5 rounded bg-zinc-800 text-zinc-300">
                      {vol.driver || 'local'}
                    </span>
                    <span className="text-[11px] font-mono px-2 py-0.5 rounded bg-orange-500/10 text-orange-300 border border-orange-500/20 tabular-nums">
                      {vol.sizeMB} MB
                    </span>
                  </div>
                  <p className="text-xs text-zinc-400 font-mono truncate">
                    Container Mount: <span className="text-zinc-200">{mountTarget}</span>
                  </p>
                </div>

                <div className="flex flex-wrap items-center gap-2 shrink-0">
                  <button
                    type="button"
                    onClick={() => loadVolumeDirectory(vol.name, '')}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 hover:bg-zinc-800 text-zinc-200 text-xs font-medium transition-colors cursor-pointer"
                  >
                    <FolderOpen className="h-3.5 w-3.5 text-orange-400" />
                    Browse Files
                  </button>

                  <a
                    href={`/volumes/download?name=${encodeURIComponent(vol.name)}`}
                    onClick={(e) => {
                      e.preventDefault();
                      toast.success(`Downloading ${vol.name}.tar.gz stream`);
                    }}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 hover:bg-zinc-800 text-zinc-200 text-xs font-medium transition-colors cursor-pointer"
                  >
                    <Download className="h-3.5 w-3.5 text-emerald-400" />
                    Download .tar.gz
                  </a>

                  <button
                    type="button"
                    onClick={() => {
                      setRestoreTargetVolume(vol.name);
                      setRestoreFile(null);
                      setRestoreJobId(null);
                      setRestoreStatus(null);
                    }}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 hover:bg-zinc-800 text-zinc-200 text-xs font-medium transition-colors cursor-pointer"
                  >
                    <Upload className="h-3.5 w-3.5 text-sky-400" />
                    Restore Archive
                  </button>

                  <button
                    type="button"
                    onClick={() => handleDeleteVolume(vol.name)}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg bg-red-500/10 hover:bg-red-500/20 text-red-400 border border-red-500/20 text-xs font-medium transition-colors cursor-pointer"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Interactive Volume File Browser Drawer */}
      {browsingVolume && (
        <div className="rounded-xl border border-orange-500/30 bg-[#111115] overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3 bg-[#09090b] border-b border-white/[0.07]">
            <div className="flex flex-wrap items-center gap-1.5 text-xs font-mono">
              <button
                type="button"
                onClick={() => loadVolumeDirectory(browsingVolume, '')}
                className="text-orange-400 hover:underline font-semibold cursor-pointer"
              >
                {browsingVolume}
              </button>
              {browsePath
                .split('/')
                .filter(Boolean)
                .map((part, idx, arr) => {
                  const target = arr.slice(0, idx + 1).join('/');
                  return (
                    <React.Fragment key={target}>
                      <ChevronRight className="h-3.5 w-3.5 text-zinc-600" />
                      <button
                        type="button"
                        onClick={() => loadVolumeDirectory(browsingVolume, target)}
                        className="text-zinc-200 hover:text-orange-400 cursor-pointer"
                      >
                        {part}
                      </button>
                    </React.Fragment>
                  );
                })}
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => loadVolumeDirectory(browsingVolume, browsePath)}
                className="p-1.5 rounded-lg bg-zinc-900 hover:bg-zinc-800 text-zinc-300 cursor-pointer"
                title="Refresh directory"
              >
                <RefreshCw className={`h-3.5 w-3.5 ${browseLoading ? 'animate-spin' : ''}`} />
              </button>
              <button
                type="button"
                onClick={() => setBrowsingVolume(null)}
                className="p-1.5 rounded-lg bg-zinc-900 hover:bg-zinc-800 text-zinc-300 cursor-pointer"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>

          <div className="divide-y divide-white/[0.05] max-h-72 overflow-y-auto">
            {browsePath && (
              <button
                type="button"
                onClick={() => {
                  const parent = browsePath.split('/').filter(Boolean).slice(0, -1).join('/');
                  loadVolumeDirectory(browsingVolume, parent);
                }}
                className="w-full px-4 py-2 text-left text-xs font-mono text-zinc-400 hover:bg-white/[0.03] flex items-center gap-2 cursor-pointer"
              >
                <Folder className="h-3.5 w-3.5 text-zinc-500" />
                ..
              </button>
            )}

            {browseEntries.length === 0 ? (
              <div className="p-6 text-center text-xs text-zinc-500">
                {browseLoading ? 'Inspecting volume filesystem...' : 'Directory is empty'}
              </div>
            ) : (
              browseEntries.map((entry) => (
                <div
                  key={entry.rel_path || entry.name}
                  onClick={() => {
                    if (entry.is_dir) {
                      const nextPath = browsePath ? `${browsePath}/${entry.name}` : entry.name;
                      loadVolumeDirectory(browsingVolume, nextPath);
                    }
                  }}
                  className={`px-4 py-2.5 flex items-center justify-between gap-4 text-xs ${
                    entry.is_dir ? 'cursor-pointer hover:bg-white/[0.03]' : ''
                  }`}
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    {entry.is_dir ? (
                      <Folder className="h-4 w-4 text-orange-400 shrink-0" />
                    ) : (
                      <FileText className="h-4 w-4 text-zinc-500 shrink-0" />
                    )}
                    <span className="font-mono text-zinc-200 truncate">{entry.name}</span>
                  </div>
                  <span className="text-[11px] font-mono text-zinc-500 shrink-0">
                    {entry.is_dir ? 'DIR' : 'FILE'}
                  </span>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {/* Mount Volume Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4">
          <div className="bg-[#111115] border border-white/[0.08] rounded-xl w-full max-w-md p-5 space-y-4">
            <div className="flex items-center justify-between border-b border-white/[0.07] pb-3">
              <h3 className="text-sm font-semibold text-zinc-100">Mount Persistent Volume</h3>
              <button
                type="button"
                onClick={() => setShowCreateModal(false)}
                className="text-zinc-500 hover:text-zinc-300 cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <form onSubmit={handleCreateVolume} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1">
                  Volume Name Suffix
                </label>
                <input
                  type="text"
                  required
                  placeholder="postgres_data"
                  value={newVolName}
                  onChange={(e) => setNewVolName(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono"
                />
                <p className="text-[11px] text-zinc-500 mt-1">
                  Full volume:{' '}
                  <code className="font-mono text-zinc-300">
                    {appPrefix}_{newVolName || 'data'}
                  </code>
                </p>
              </div>
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1">
                  Container Mount Path
                </label>
                <input
                  type="text"
                  required
                  placeholder="/var/lib/postgresql/data"
                  value={newVolMountPath}
                  onChange={(e) => setNewVolMountPath(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono"
                />
              </div>
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setShowCreateModal(false)}
                  className="px-3 py-1.5 rounded-lg bg-zinc-800 text-zinc-300 text-xs cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={creating}
                  className="px-3.5 py-1.5 rounded-lg bg-zinc-100 hover:bg-white text-zinc-950 text-xs font-semibold cursor-pointer"
                >
                  {creating ? 'Mounting...' : 'Mount Volume'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Restore Volume Archive Modal */}
      {restoreTargetVolume && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4">
          <div className="bg-[#111115] border border-white/[0.08] rounded-xl w-full max-w-md p-5 space-y-4">
            <div className="flex items-center justify-between border-b border-white/[0.07] pb-3">
              <h3 className="text-sm font-semibold text-zinc-100">
                Restore Archive to{' '}
                <span className="font-mono text-orange-400">{restoreTargetVolume}</span>
              </h3>
              <button
                type="button"
                onClick={() => setRestoreTargetVolume(null)}
                className="text-zinc-500 hover:text-zinc-300 cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <form onSubmit={handleStartRestore} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1.5">
                  Select Archive (<code className="font-mono">.tar.gz</code>,{' '}
                  <code className="font-mono">.tgz</code>, <code className="font-mono">.tar</code>,{' '}
                  <code className="font-mono">.zip</code>)
                </label>
                <input
                  type="file"
                  accept=".tar.gz,.tgz,.tar,.zip"
                  onChange={(e) => setRestoreFile(e.target.files?.[0] || null)}
                  className="w-full text-xs text-zinc-300 file:mr-3 file:py-1.5 file:px-3 file:rounded-lg file:border-0 file:text-xs file:font-medium file:bg-zinc-800 file:text-zinc-200 hover:file:bg-zinc-700"
                />
              </div>

              {restoreStatus && (
                <div className="p-3 rounded-lg bg-[#09090b] border border-white/[0.08] space-y-2">
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-zinc-300 font-medium capitalize">
                      {restoreStatus.status}
                    </span>
                    <span className="font-mono text-zinc-400 tabular-nums">
                      {restoreStatus.extract_percent}%
                    </span>
                  </div>
                  <div className="w-full h-1.5 bg-zinc-800 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-orange-500 transition-all duration-300"
                      style={{
                        width: `${Math.min(100, Math.max(5, restoreStatus.extract_percent))}%`,
                      }}
                    />
                  </div>
                  {restoreStatus.done && !restoreStatus.error && (
                    <div className="flex items-center gap-1.5 text-xs text-emerald-400 pt-1">
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      Extraction complete
                    </div>
                  )}
                  {restoreStatus.error && (
                    <div className="flex items-center gap-1.5 text-xs text-red-400 pt-1">
                      <AlertCircle className="h-3.5 w-3.5" />
                      {restoreStatus.error}
                    </div>
                  )}
                </div>
              )}

              <div className="flex justify-end gap-2 pt-2">
                <button
                  type="button"
                  onClick={() => setRestoreTargetVolume(null)}
                  className="px-3 py-1.5 rounded-lg bg-zinc-800 text-zinc-300 text-xs cursor-pointer"
                >
                  Close
                </button>
                <button
                  type="submit"
                  disabled={!restoreFile || uploadingRestore}
                  className="px-4 py-1.5 rounded-lg bg-zinc-100 hover:bg-white text-zinc-950 text-xs font-semibold disabled:opacity-50 cursor-pointer"
                >
                  {uploadingRestore ? 'Uploading...' : 'Upload & Restore'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
