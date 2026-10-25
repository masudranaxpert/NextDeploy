import React, { useEffect, useState } from 'react';
import {
  Plus,
  RotateCw,
  RotateCcw,
  Archive,
  Calendar,
  FileText,
  ExternalLink,
  Trash2,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock,
  AlertTriangle,
  Sliders,
  Play,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  AppStack,
  BackupArchiveRecord,
  BackupDestination,
  BackupScheduleItem,
} from '../../../types/nextdeploy';
import {
  ActionMenu,
  ShadcnDialog,
  ShadcnSwitch,
  StatusBadge,
} from '../../../components/ui/Primitives';
import { backendApi } from '../../../lib/backendApi';

interface ProjectBackupsTabProps {
  app: AppStack;
  onOpenBackupDestinations?: () => void;
}

const CRON_PRESETS: Array<{ label: string; value: string }> = [
  { label: 'Every hour (0 * * * *)', value: '0 * * * *' },
  { label: 'Every 6 hours (0 */6 * * *)', value: '0 */6 * * *' },
  { label: 'Every 12 hours (0 */12 * * *)', value: '0 */12 * * *' },
  { label: 'Daily at 02:00 (0 2 * * *)', value: '0 2 * * *' },
  { label: 'Weekly on Sunday (0 2 * * 0)', value: '0 2 * * 0' },
  { label: 'Monthly on 1st (0 2 1 * *)', value: '0 2 1 * *' },
];

function formatBytes(sizeMB: number, sizeBytes?: number): string {
  const bytes = sizeBytes && sizeBytes > 0 ? sizeBytes : sizeMB * 1024 * 1024;
  if (bytes >= 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  if (bytes >= 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${Math.max(0, Math.round(bytes))} B`;
}

// Last loaded backup data per site, so reopening the tab shows it at once and refreshes quietly.
const backupsCache = new Map<
  string,
  { destinations: BackupDestination[]; schedules: BackupScheduleItem[]; history: BackupArchiveRecord[] }
>();

export const ProjectBackupsTab: React.FC<ProjectBackupsTabProps> = ({ app, onOpenBackupDestinations }) => {
  // PHP sites have no Docker volumes or containers to pause: a backup is the site files plus its databases.
  const isPhp = !!app.php;
  const cached = backupsCache.get(app.id);
  const [destinations, setDestinations] = useState<BackupDestination[]>(cached?.destinations ?? []);
  const [loaded, setLoaded] = useState(!!cached);

  // Manual Backup Runner Form State (POST /apps/:id/backup/:destid)
  const [manualDestId, setManualDestId] = useState<string>(cached?.destinations[0]?.id ?? '');
  const [manualType, setManualType] = useState<'full' | 'volume' | 'app'>(isPhp ? 'app' : 'full');
  const [manualSelectedVols, setManualSelectedVols] = useState<string[]>(
    app.volumes.map((v) => v.name)
  );
  const [manualPauseContainers, setManualPauseContainers] = useState(!isPhp);
  const [isTriggeringBackup, setIsTriggeringBackup] = useState(false);

  // Schedules State (GET /apps/:id/backup/schedules)
  const [schedules, setSchedules] = useState<BackupScheduleItem[]>(cached?.schedules ?? []);

  // Add / Edit Schedule Modal State
  const [scheduleModalOpen, setScheduleModalOpen] = useState(false);
  const [editingScheduleId, setEditingScheduleId] = useState<string | null>(null);
  const [schedDestId, setSchedDestId] = useState<string>(cached?.destinations[0]?.id ?? '');
  const [schedType, setSchedType] = useState<'full' | 'volume' | 'app'>(isPhp ? 'app' : 'full');
  const [schedSelectedVols, setSchedSelectedVols] = useState<string[]>(
    app.volumes.map((v) => v.name)
  );
  const [schedCronPreset, setSchedCronPreset] = useState<string>('0 2 * * *');
  const [schedCustomCron, setSchedCustomCron] = useState<string>('0 2 * * *');
  const [schedRetention, setSchedRetention] = useState<string>('7');
  const [schedPauseContainers, setSchedPauseContainers] = useState<boolean>(!isPhp);

  // Paginated Backup History State (GET /apps/:id/backup/history?page=...&per_page=...)
  const [historyItems, setHistoryItems] = useState<BackupArchiveRecord[]>(cached?.history ?? []);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(5);
  const [totalHistoryCount, setTotalHistoryCount] = useState<number | null>(null);
  const [totalHistoryPages, setTotalHistoryPages] = useState<number | null>(null);

  // Rclone Log Modal State
  const [logModalData, setLogModalData] = useState<{
    title: string;
    status: string;
    errorMessage?: string;
    log: string;
  } | null>(null);

  // Point-in-Time Restore Confirmation & Live Status Polling (/apps/:id/backup/restore/:historyid & /restore-status)
  const [restoreTarget, setRestoreTarget] = useState<BackupArchiveRecord | null>(null);
  const [restoreJobState, setRestoreJobState] = useState<{
    status: 'idle' | 'running' | 'completed' | 'failed';
    log: string;
    error?: string;
  }>({ status: 'idle', log: '' });

  // Hydrate destinations, schedules, and history from Go backend when running on server
  const fetchBackupsData = async (targetPage = page, targetPerPage = perPage) => {
    const [destRes, schedRes, histRes] = await Promise.all([
      backendApi.backupDestinationsList(),
      backendApi.backupSchedulesList(app.id),
      backendApi.backupHistoryList(app.id, targetPage, targetPerPage),
    ]);

    const rawDests = destRes.data?.destinations;
    let mappedDests = destinations;
    if (destRes.ok && !Array.isArray(rawDests)) {
      mappedDests = [];
      setDestinations([]);
    }
    if (Array.isArray(rawDests)) {
      mappedDests = rawDests.map((d) => {
        const providerRaw = (d.Provider || d.provider || 'r2').toLowerCase();
        const isGDrive = providerRaw === 'gdrive';
        return {
          id: String(d.ID ?? d.id),
          name: d.Name || d.name || (isGDrive ? 'Google Drive' : 'Cloudflare R2'),
          provider: isGDrive ? 'Google Drive' : 'Cloudflare R2',
          providerCode: isGDrive ? 'gdrive' : 'r2',
          bucket: d.bucket || '',
          endpoint: '',
          region: 'auto',
          status: 'connected',
          scheduleCron: '0 2 * * *',
          retentionDays: 14,
          usedGB: 0,
          quotaGB: 100,
          lastBackupAt: 'Synced',
        };
      });
      setDestinations(mappedDests);
      if (!mappedDests.some((d) => d.id === manualDestId)) {
        setManualDestId(mappedDests[0]?.id ?? '');
      }
      if (!mappedDests.some((d) => d.id === schedDestId)) {
        setSchedDestId(mappedDests[0]?.id ?? '');
      }
    }

    const rawScheds = schedRes.ok ? schedRes.data?.schedules ?? [] : undefined;
    let nextScheds: BackupScheduleItem[] | undefined;
    if (Array.isArray(rawScheds)) {
      nextScheds = rawScheds.map((s) => {
          const destIdStr = String(s.DestinationID ?? s.destination_id ?? '');
          const matchedDest = mappedDests.find((d) => d.id === destIdStr);
          const providerCode =
            matchedDest?.providerCode ||
            (s.destination_provider === 'gdrive' ? 'gdrive' : 'r2');
          return {
            id: String(s.ID ?? s.id),
            appId: app.id,
            appSlug: app.slug,
            destinationId: destIdStr,
            destinationName:
              s.destination_name || matchedDest?.name || `Connector #${destIdStr}`,
            provider: providerCode === 'gdrive' ? 'Google Drive' : 'Cloudflare R2',
            destinationProvider: providerCode,
            type: (s.BackupType || s.type || 'full') as 'full' | 'volume' | 'app',
            volumeNames: s.VolumeNames ?? s.volume_name ?? '',
            cron: s.CronExpression || s.cron || '0 2 * * *',
            retention: s.RetentionCount ?? s.retention ?? 7,
            pauseContainers: Boolean(s.PauseContainers ?? s.pause_containers),
            enabled: s.Enabled ?? s.enabled ?? true,
            lastRun: s.LastRun || s.last_run || 'Never',
            nextRunAt: s.NextRunAt || s.next_run_at || '',
          };
      });
      setSchedules(nextScheds);
    }

    const rawHist = histRes.ok ? histRes.data?.history ?? [] : undefined;
    if (Array.isArray(rawHist)) {
      setTotalHistoryCount(histRes.data?.total ?? rawHist.length);
      setTotalHistoryPages(histRes.data?.total_pages ?? 1);
      const nextHist: BackupArchiveRecord[] = rawHist.map((h) => {
          const destIdStr = String(h.DestinationID ?? h.destination_id ?? '');
          const matchedDest = mappedDests.find((d) => d.id === destIdStr);
          const bType = (h.BackupType || h.type || 'full') as 'full' | 'volume' | 'app';
          const remotePath = h.RemotePath || h.file_path || '';
          const archiveFile = remotePath.split('/').pop() || `${app.slug}-${bType}.tar.gz`;
          const sizeBytes = h.SizeBytes ?? h.size ?? 0;
          const stRaw = (h.Status || h.status || 'completed').toLowerCase();
          return {
            id: String(h.ID ?? h.id),
            appId: app.id,
            appSlug: app.slug,
            destinationId: destIdStr,
            destinationName:
              h.destination_name || matchedDest?.name || `Connector #${destIdStr}`,
            destinationProvider:
              matchedDest?.providerCode ||
              (h.destination_provider === 'gdrive' ? 'gdrive' : 'r2'),
            provider:
              matchedDest?.provider ||
              (h.destination_provider === 'gdrive' ? 'Google Drive' : 'Cloudflare R2'),
            archiveName: archiveFile,
            remotePath,
            type: bType,
            backupType: bType,
            volumeName: h.VolumeName || h.volume_name || bType,
            sizeBytes,
            sizeMB: Number((sizeBytes / (1024 * 1024)).toFixed(2)),
            createdAt: h.CreatedAt || h.started_at || 'Recent',
            status:
              stRaw === 'running'
                ? 'running'
                : stRaw === 'failed'
                  ? 'failed'
                  : 'completed',
            errorMessage: h.ErrorMessage || h.error_message,
            includesVolumes: bType !== 'app',
            includesEnv: bType !== 'volume',
            remoteMissing: Boolean(h.RemoteMissing ?? h.remote_missing),
          };
      });
      setHistoryItems(nextHist);
      backupsCache.set(app.id, { destinations: mappedDests, schedules: nextScheds ?? schedules, history: nextHist });
    }
    setLoaded(true);
  };

  useEffect(() => {
    void fetchBackupsData(page, perPage);
  }, [app.id, page, perPage]);

  // A running backup finishes on the server: follow it until it is done.
  const hasRunning = historyItems.some((h) => h.status === 'running');
  useEffect(() => {
    if (!hasRunning) return;
    const t = window.setInterval(() => void fetchBackupsData(page, perPage), 3000);
    return () => window.clearInterval(t);
  }, [hasRunning, app.id, page, perPage]);

  const toggleManualVolume = (volName: string) => {
    setManualSelectedVols((prev) =>
      prev.includes(volName) ? prev.filter((v) => v !== volName) : [...prev, volName]
    );
  };

  const toggleSchedVolume = (volName: string) => {
    setSchedSelectedVols((prev) =>
      prev.includes(volName) ? prev.filter((v) => v !== volName) : [...prev, volName]
    );
  };

  const handleRunManualBackup = async (e: React.FormEvent) => {
    e.preventDefault();
    const targetDest = destinations.find((d) => d.id === manualDestId) || destinations[0];
    if (!targetDest) {
      toast.error('Connect a storage destination on the Backup Connectors page first');
      return;
    }
    const volCsv = manualType === 'volume' ? manualSelectedVols.join(',') : '';
    if (manualType === 'volume' && !volCsv) {
      toast.error('Select at least one Docker volume to back up');
      return;
    }

    setIsTriggeringBackup(true);
    const res = await backendApi.triggerManualBackup(
      app.id,
      targetDest.id,
      manualType,
      volCsv,
      manualPauseContainers
    );
    setIsTriggeringBackup(false);

    if (!res.ok) {
      toast.error(res.error || 'Failed to start backup job');
      return;
    }
    toast.success(`Backup started to ${targetDest.name}`);
    setPage(1);
    await fetchBackupsData(1, perPage);
  };

  const openCreateScheduleModal = () => {
    setEditingScheduleId(null);
    setSchedDestId(destinations[0]?.id || '');
    setSchedType(isPhp ? 'app' : 'full');
    setSchedSelectedVols(app.volumes.map((v) => v.name));
    setSchedCronPreset('0 2 * * *');
    setSchedCustomCron('0 2 * * *');
    setSchedRetention('7');
    setSchedPauseContainers(!isPhp);
    setScheduleModalOpen(true);
  };

  const openEditScheduleModal = (sc: BackupScheduleItem) => {
    setEditingScheduleId(sc.id);
    setSchedDestId(sc.destinationId);
    setSchedType(sc.type);
    const vols = Array.isArray(sc.volumeNames)
      ? sc.volumeNames
      : String(sc.volumeNames || '')
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean);
    setSchedSelectedVols(vols);
    const isPreset = CRON_PRESETS.some((p) => p.value === sc.cron);
    setSchedCronPreset(isPreset ? sc.cron : 'custom');
    setSchedCustomCron(sc.cron);
    setSchedRetention(String(sc.retention || 7));
    setSchedPauseContainers(sc.pauseContainers);
    setScheduleModalOpen(true);
  };

  const handleSaveSchedule = async (e: React.FormEvent) => {
    e.preventDefault();
    const targetDest = destinations.find((d) => d.id === schedDestId) || destinations[0];
    const finalCron = (schedCronPreset === 'custom' ? schedCustomCron : schedCronPreset).trim();
    const volCsv = schedType === 'volume' ? schedSelectedVols.join(',') : '';
    const retentionNum = Math.max(1, Number(schedRetention) || 7);

    if (editingScheduleId) {
      const res = await backendApi.backupScheduleUpdate(app.id, editingScheduleId, {
        destination: schedDestId,
        type: schedType,
        volume_names: volCsv,
        cron: finalCron,
        retention: retentionNum,
        pause_containers: schedPauseContainers,
      });
      if (!res.ok) {
        toast.error(res.error || 'Failed to update backup schedule');
        return;
      }
      await fetchBackupsData(page, perPage);
      setScheduleModalOpen(false);
      toast.success('Updated backup schedule');
      return;
    }

    const res = await backendApi.backupScheduleCreate(app.id, schedDestId, {
      type: schedType,
      volume_names: volCsv,
      cron: finalCron,
      retention: retentionNum,
      pause_containers: schedPauseContainers,
    });
    if (!res.ok) {
      toast.error(res.error || 'Failed to create backup schedule');
      return;
    }
    await fetchBackupsData(page, perPage);
    setScheduleModalOpen(false);
    toast.success(`Created backup schedule (${finalCron})`);
  };

  const handleConfirmRestore = async () => {
    if (!restoreTarget) return;
    setRestoreJobState({
      status: 'running',
      log: `[restore] Starting restore of ${restoreTarget.archiveName}...\n[rclone] Downloading archive from ${restoreTarget.destinationName}...`,
    });

    const res = await backendApi.restoreBackup(app.id, restoreTarget.id);
    if (!res.ok) {
      setRestoreJobState({
        status: 'failed',
        log: res.error || 'Failed to start restore job',
        error: res.error,
      });
      toast.error(res.error || 'Restore failed');
      return;
    }

    {
      const timer = window.setInterval(async () => {
        const st = await backendApi.backupRestoreStatus(app.id);
        if (!st.ok || !st.data) {
          window.clearInterval(timer);
          return;
        }
        setRestoreJobState({
          status: (st.data.status as 'running' | 'completed' | 'failed') || 'running',
          log: st.data.log || '',
          error: st.data.error,
        });
        if (st.data.status === 'completed') {
          window.clearInterval(timer);
          toast.success(`Restored ${app.slug} from ${restoreTarget.archiveName}`);
        } else if (st.data.status === 'failed') {
          window.clearInterval(timer);
          toast.error(st.data.error || 'Restore failed');
        }
      }, 1200);
      return;
    }
  };

  const computedTotalPages =
    totalHistoryPages ?? Math.max(1, Math.ceil(historyItems.length / perPage));
  const currentPage = Math.min(page, computedTotalPages);
  const displayedHistory =
    totalHistoryCount !== null
      ? historyItems
      : historyItems.slice((currentPage - 1) * perPage, currentPage * perPage);
  const totalCount = totalHistoryCount ?? historyItems.length;

  return (
    <div className="space-y-5">
      {/* SECTION 1: Manual Backup Runner Card (POST /apps/:id/backup/:destid) */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Archive className="h-4 w-4 text-orange-400" />
          </div>
          <h2 className="text-sm font-semibold text-zinc-100">Run Manual Backup</h2>
        </div>

        {loaded && destinations.length === 0 && (
          <div className="mb-4 rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
            No storage destination is connected yet.{' '}
            {onOpenBackupDestinations ? (
              <button
                type="button"
                onClick={onOpenBackupDestinations}
                className="font-semibold underline underline-offset-2 transition-colors hover:text-amber-100 cursor-pointer"
              >
                Add one under Integrations → Backup.
              </button>
            ) : (
              <span>Add one under Integrations → Backup.</span>
            )}
          </div>
        )}

        <form onSubmit={handleRunManualBackup} className="space-y-4">
          <div className="grid grid-cols-1 gap-3.5 md:grid-cols-3">
            {/* Storage Connector */}
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Storage Destination
              </label>
              <select
                aria-label="Storage destination"
                value={manualDestId}
                onChange={(e) => setManualDestId(e.target.value)}
                className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              >
                {destinations.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name} ({d.providerCode === 'gdrive' ? 'Google Drive' : 'Cloudflare R2'})
                  </option>
                ))}
              </select>
            </div>

            {/* Backup Type */}
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Backup Scope
              </label>
              <select
                aria-label="Backup scope"
                value={manualType}
                onChange={(e) => setManualType(e.target.value as 'full' | 'volume' | 'app')}
                className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              >
                {isPhp ? (
                  <option value="app">Site files + databases</option>
                ) : (
                  <>
                    <option value="full">Full Backup (App Files + All Volumes)</option>
                    <option value="volume">Docker Volume(s) Only</option>
                    <option value="app">App Files Only (Compose &amp; .env)</option>
                  </>
                )}
              </select>
            </div>

            {/* Pause Containers Toggle */}
            {!isPhp && (
            <div className="flex flex-col justify-end">
              <div className="flex h-9 items-center justify-between rounded-lg border border-white/[0.08] bg-[#09090b] px-3">
                <span className="text-xs text-zinc-200 truncate mr-2">
                  Pause containers during backup
                </span>
                <ShadcnSwitch
                  checked={manualPauseContainers}
                  onChange={setManualPauseContainers}
                />
              </div>
            </div>
            )}
          </div>

          {/* Multi-Volume Selector when type === 'volume' */}
          {manualType === 'volume' && (
            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 space-y-2">
              <div className="flex items-center justify-between text-[11px] text-zinc-400">
                <span>Select Docker Volumes</span>
                <button
                  type="button"
                  onClick={() =>
                    setManualSelectedVols(
                      manualSelectedVols.length === app.volumes.length
                        ? []
                        : app.volumes.map((v) => v.name)
                    )
                  }
                  className="font-mono text-orange-400 hover:underline cursor-pointer"
                >
                  {manualSelectedVols.length === app.volumes.length ? 'Clear all' : 'Select all'}
                </button>
              </div>
              {app.volumes.length === 0 ? (
                <div className="text-xs text-zinc-500">
                  No volumes mounted on this project.
                </div>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {app.volumes.map((v) => {
                    const checked = manualSelectedVols.includes(v.name);
                    return (
                      <label
                        key={v.name}
                        className={`inline-flex items-center gap-2 rounded-lg border px-2.5 py-1.5 font-mono text-xs cursor-pointer transition-colors ${
                          checked
                            ? 'border-orange-500/50 bg-orange-500/12 text-zinc-100'
                            : 'border-white/[0.07] bg-zinc-900/60 text-zinc-400'
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => toggleManualVolume(v.name)}
                          className="h-3.5 w-3.5 accent-orange-500"
                        />
                        <span>{v.name}</span>
                      </label>
                    );
                  })}
                </div>
              )}
            </div>
          )}

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={isTriggeringBackup || destinations.length === 0}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white disabled:opacity-50 cursor-pointer"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              {isTriggeringBackup ? 'Starting Backup...' : 'Start Backup Now'}
            </button>
          </div>
        </form>
      </div>

      {/* SECTION 2: Automated Backup Schedules (/apps/:id/backup/schedules) */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] px-4 py-3">
          <div className="flex items-center gap-2">
            <Calendar className="h-4 w-4 text-orange-400" />
            <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-200">
              Automated Backup Schedules ({schedules.length})
            </h3>
          </div>
          <button
            type="button"
            onClick={openCreateScheduleModal}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5 text-orange-400" />
            Add Schedule
          </button>
        </div>

        {schedules.length === 0 ? (
          <div className="p-6 text-center text-xs text-zinc-500">
            No automated schedules configured for this project.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.06] bg-[#09090b]/50 text-zinc-400">
                  <th className="px-4 py-2.5 font-medium">Storage Destination</th>
                  <th className="px-4 py-2.5 font-medium">{isPhp ? 'Backup Scope' : 'Backup Scope & Volumes'}</th>
                  <th className="px-4 py-2.5 font-medium">Schedule & Next Run</th>
                  <th className="px-4 py-2.5 font-medium">Retention Policy</th>
                  <th className="px-4 py-2.5 font-medium">Active</th>
                  <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {schedules.map((sc) => {
                  const volStr = Array.isArray(sc.volumeNames)
                    ? sc.volumeNames.join(', ')
                    : sc.volumeNames;
                  const isGDrive =
                    sc.destinationProvider === 'gdrive' ||
                    sc.provider === 'Google Drive' ||
                    sc.destinationName.toLowerCase().includes('google');
                  const scopeLabel =
                    isPhp
                      ? 'Files + databases'
                      : sc.type === 'full'
                      ? 'Full Stack'
                      : sc.type === 'volume'
                        ? 'Volumes Only'
                        : 'App Files';

                  return (
                    <tr key={sc.id} className="hover:bg-white/[0.02] transition-colors">
                      <td className="px-4 py-3.5 align-middle">
                        <div className="font-semibold text-zinc-100">{sc.destinationName}</div>
                        <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-zinc-400">
                          <span
                            className={`inline-block h-1.5 w-1.5 rounded-full ${
                              isGDrive ? 'bg-emerald-400' : 'bg-orange-400'
                            }`}
                          />
                          <span>{isGDrive ? 'Google Drive' : 'Cloudflare R2'}</span>
                          <span className="text-zinc-600">·</span>
                          <span className="text-zinc-500">
                            Last: {sc.lastRun || sc.lastRunAt || 'Never'}
                          </span>
                        </div>
                      </td>
                      <td className="px-4 py-3.5 align-middle">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="inline-flex items-center rounded-md border border-orange-500/25 bg-orange-500/10 px-2 py-0.5 text-[11px] font-medium text-orange-300">
                            {scopeLabel}
                          </span>
                          {volStr && (
                            <span className="inline-flex items-center rounded-md border border-white/[0.07] bg-zinc-900 px-2 py-0.5 text-[11px] text-zinc-300">
                              {volStr}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3.5 align-middle">
                        <div className="inline-flex items-center gap-1.5 rounded-md border border-white/[0.07] bg-zinc-900/90 px-2 py-0.5 font-mono text-[11px] text-zinc-200">
                          <Clock className="h-3 w-3 text-orange-400" />
                          {sc.cron}
                        </div>
                        {sc.nextRunAt && (
                          <div className="mt-1 text-[11px] font-medium text-emerald-400">
                            Next: {sc.nextRunAt}
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-3.5 align-middle">
                        <div className="font-medium text-zinc-200 tabular-nums">
                          Keep last {sc.retention} snapshots
                        </div>
                        <div className="mt-0.5 text-[11px] text-zinc-500">
                          {sc.pauseContainers ? 'Container pause enabled' : 'Live hot snapshot'}
                        </div>
                      </td>
                      <td className="px-4 py-3.5 align-middle">
                        <ShadcnSwitch
                          checked={sc.enabled}
                          onChange={(next) => {
                            void backendApi.backupScheduleToggle(app.id, sc.id, next);
                            setSchedules((prev) =>
                              prev.map((item) =>
                                item.id === sc.id ? { ...item, enabled: next } : item
                              )
                            );
                            toast.success(next ? 'Schedule enabled' : 'Schedule paused');
                          }}
                        />
                      </td>
                      <td className="px-3 py-3.5 text-right align-middle">
                        <ActionMenu
                          items={[
                            {
                              label: 'Edit Schedule',
                              icon: <Sliders className="h-3.5 w-3.5" />,
                              onClick: () => openEditScheduleModal(sc),
                            },
                            {
                              label: 'Run Schedule Now',
                              icon: <RotateCw className="h-3.5 w-3.5" />,
                              onClick: () => {
                                void backendApi.triggerManualBackup(
                                  app.id,
                                  sc.destinationId,
                                  sc.type,
                                  volStr,
                                  sc.pauseContainers
                                );
                                toast.success('Started scheduled backup job');
                              },
                            },
                            {
                              label: 'Delete Schedule',
                              icon: <Trash2 className="h-3.5 w-3.5" />,
                              variant: 'danger',
                              onClick: () => {
                                void backendApi.backupScheduleDelete(app.id, sc.id);
                                setSchedules((prev) => prev.filter((item) => item.id !== sc.id));
                                toast.success('Deleted backup schedule');
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
        )}
      </div>

      {/* SECTION 3: Paginated Backup History (/apps/:id/backup/history) */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] px-4 py-3">
          <div className="flex items-center gap-2">
            <Archive className="h-4 w-4 text-orange-400" />
            <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-200">
              Backup History ({totalCount})
            </h3>
          </div>
          <button
            type="button"
            onClick={() => void fetchBackupsData(page, perPage)}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
          >
            <RotateCw className="h-3 w-3 text-zinc-400" />
            Refresh
          </button>
        </div>

        {/* Desktop Table View */}
        <div className="hidden md:block overflow-x-auto">
          <table className="w-full min-w-[780px] text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.06] bg-[#09090b]/50 text-zinc-400">
                <th className="px-4 py-2.5 font-medium">Snapshot Archive</th>
                <th className="px-4 py-2.5 font-medium">{isPhp ? 'Scope' : 'Scope & Volumes'}</th>
                <th className="px-4 py-2.5 font-medium">Storage Destination</th>
                <th className="px-4 py-2.5 font-medium">Created</th>
                <th className="px-4 py-2.5 font-medium text-right">Size</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {displayedHistory.map((bk) => {
                const isGDrive =
                  bk.destinationProvider === 'gdrive' ||
                  bk.provider === 'Google Drive' ||
                  bk.destinationName.toLowerCase().includes('google');
                const rawScope = bk.backupType || bk.type || 'full';
                const scopeLabel =
                  isPhp
                    ? 'Files + databases'
                    : rawScope === 'full'
                    ? 'Full Stack'
                    : rawScope === 'volume'
                      ? 'Volume Snapshot'
                      : 'App Files';

                return (
                  <tr key={bk.id} className="hover:bg-white/[0.02] transition-colors">
                    <td className="px-4 py-3.5 align-middle">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold text-zinc-100">
                          {bk.archiveName}
                        </span>
                        {bk.remoteMissing && (
                          <span className="inline-flex items-center gap-1 rounded-md border border-rose-500/30 bg-rose-500/15 px-2 py-0.5 text-[11px] font-medium text-rose-300">
                            <AlertTriangle className="h-3 w-3" />
                            Missing on remote
                          </span>
                        )}
                      </div>
                      {bk.remotePath && (
                        <div className="mt-0.5 text-[11px] text-zinc-500 break-all">
                          Folder: <span className="text-zinc-400">{bk.remotePath.includes('/') ? bk.remotePath.slice(0, bk.remotePath.lastIndexOf('/')) : bk.remotePath}</span>
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-3.5 align-middle">
                      <div className="flex flex-col items-start gap-1">
                        <span className="inline-flex items-center rounded-md border border-orange-500/25 bg-orange-500/10 px-2 py-0.5 text-[11px] font-medium text-orange-300 whitespace-nowrap">
                          {scopeLabel}
                        </span>
                        {bk.volumeName && (
                          <span className="text-[11px] text-zinc-300 font-medium">
                            {bk.volumeName}
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3.5 align-middle">
                      <div className="font-medium text-zinc-100">{bk.destinationName}</div>
                      <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-zinc-400">
                        <span
                          className={`inline-block h-1.5 w-1.5 rounded-full ${
                            isGDrive ? 'bg-emerald-400' : 'bg-orange-400'
                          }`}
                        />
                        <span>{isGDrive ? 'Google Drive' : 'Cloudflare R2'}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3.5 align-middle whitespace-nowrap text-zinc-300 tabular-nums">
                      {bk.createdAt}
                    </td>
                    <td className="px-4 py-3.5 text-right align-middle whitespace-nowrap font-medium tabular-nums text-zinc-200">
                      {formatBytes(bk.sizeMB, bk.sizeBytes)}
                    </td>
                    <td className="px-4 py-3.5 align-middle whitespace-nowrap">
                      <StatusBadge
                        status={
                          bk.remoteMissing
                            ? 'error'
                            : bk.status === 'running'
                              ? 'building'
                              : bk.status
                        }
                        label={bk.remoteMissing ? 'Missing' : bk.status}
                      />
                    </td>
                    <td className="px-4 py-3.5 text-right align-middle whitespace-nowrap">
                      <div className="inline-flex items-center justify-end gap-1.5">
                        {!bk.remoteMissing && (
                          <button
                            type="button"
                            onClick={() => {
                              setRestoreJobState({ status: 'idle', log: '' });
                              setRestoreTarget(bk);
                            }}
                            className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 hover:text-white cursor-pointer"
                          >
                            <RotateCcw className="h-3 w-3 text-orange-400" />
                            Restore
                          </button>
                        )}
                        <ActionMenu
                          items={[
                            ...(!bk.remoteMissing
                              ? [
                                  {
                                    label: 'Restore Backup',
                                    icon: <RotateCcw className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      setRestoreJobState({ status: 'idle', log: '' });
                                      setRestoreTarget(bk);
                                    },
                                  },
                                ]
                              : []),
                            {
                              label: 'View Rclone Log',
                              icon: <FileText className="h-3.5 w-3.5" />,
                              onClick: async () => {
                                const res = await backendApi.backupLog(app.id, bk.id);
                                setLogModalData({
                                  title: bk.archiveName,
                                  status: res.status || bk.status,
                                  errorMessage: res.error_message || bk.errorMessage,
                                  log:
                                    res.log ||
                                    `[rclone] Backup job #${bk.id} for ${app.slug}\n[tar] Created archive ${bk.archiveName}\n[rclone] Transferred to ${bk.destinationName}\n[ok] Completed at ${bk.createdAt}`,
                                });
                              },
                            },
                            ...(isGDrive && !bk.remoteMissing
                              ? [
                                  {
                                    label: 'Open in Google Drive',
                                    icon: <ExternalLink className="h-3.5 w-3.5" />,
                                    onClick: async () => {
                                      const res = await backendApi.backupDriveLink(app.id, bk.id);
                                      if (res.url) {
                                        window.open(res.url, '_blank');
                                      } else {
                                        toast.error(res.error || 'Could not resolve Google Drive link');
                                      }
                                    },
                                  },
                                ]
                              : []),
                            {
                              label: 'Delete Backup',
                              icon: <Trash2 className="h-3.5 w-3.5" />,
                              variant: 'danger',
                              onClick: () => {
                                void backendApi.backupDelete(app.id, bk.id);
                                setHistoryItems((prev) => prev.filter((item) => item.id !== bk.id));
                                toast.success(`Deleted ${bk.archiveName}`);
                              },
                            },
                          ]}
                        />
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {loaded && displayedHistory.length === 0 && (
          <div className="p-6 text-center text-xs text-zinc-500">No backups yet.</div>
        )}

        {/* Mobile Responsive Cards View */}
        <div className="md:hidden divide-y divide-white/[0.06]">
          {displayedHistory.map((bk) => {
            const isGDrive =
              bk.destinationProvider === 'gdrive' ||
              bk.provider === 'Google Drive' ||
              bk.destinationName.toLowerCase().includes('google');
            return (
              <div key={bk.id} className="p-3.5 space-y-2.5">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="rounded bg-white/[0.06] px-1.5 py-0.5 font-mono text-[10px] uppercase text-orange-400">
                        {bk.backupType || bk.type || 'full'}
                      </span>
                      <span className="font-mono text-xs font-semibold text-zinc-100 truncate">
                        {bk.archiveName}
                      </span>
                    </div>
                    <div className="mt-1 font-mono text-[11px] text-zinc-400">
                      {bk.destinationName} · {formatBytes(bk.sizeMB, bk.sizeBytes)} · {bk.createdAt}
                    </div>
                    {bk.remoteMissing && (
                      <div className="mt-1 inline-flex items-center gap-1 rounded bg-rose-500/15 px-1.5 py-0.5 font-mono text-[10px] text-rose-300">
                        <AlertTriangle className="h-3 w-3" />
                        File missing on remote storage
                      </div>
                    )}
                  </div>
                  <StatusBadge
                    status={
                      bk.remoteMissing
                        ? 'error'
                        : bk.status === 'running'
                          ? 'building'
                          : bk.status
                    }
                    label={bk.remoteMissing ? 'Missing' : bk.status}
                  />
                </div>

                <div className="flex flex-wrap items-center gap-2 pt-1">
                  {!bk.remoteMissing && (
                    <button
                      type="button"
                      onClick={() => {
                        setRestoreJobState({ status: 'idle', log: '' });
                        setRestoreTarget(bk);
                      }}
                      className="inline-flex h-7 items-center gap-1 rounded-md border border-orange-500/30 bg-orange-500/10 px-2.5 text-[11px] font-medium text-orange-300 cursor-pointer"
                    >
                      <RotateCcw className="h-3 w-3" />
                      Restore
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={async () => {
                      const res = await backendApi.backupLog(app.id, bk.id);
                      setLogModalData({
                        title: bk.archiveName,
                        status: res.status || bk.status,
                        errorMessage: res.error_message || bk.errorMessage,
                        log:
                          res.log ||
                          `[rclone] Backup job #${bk.id} for ${app.slug}\n[ok] Completed at ${bk.createdAt}`,
                      });
                    }}
                    className="inline-flex h-7 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-300 cursor-pointer"
                  >
                    <FileText className="h-3 w-3" />
                    Log
                  </button>
                  {isGDrive && !bk.remoteMissing && (
                    <button
                      type="button"
                      onClick={async () => {
                        const res = await backendApi.backupDriveLink(app.id, bk.id);
                        if (res.url) window.open(res.url, '_blank');
                      }}
                      className="inline-flex h-7 items-center gap-1 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2.5 text-[11px] font-medium text-emerald-300 cursor-pointer"
                    >
                      <ExternalLink className="h-3 w-3" />
                      Drive
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => {
                      void backendApi.backupDelete(app.id, bk.id);
                      setHistoryItems((prev) => prev.filter((item) => item.id !== bk.id));
                      toast.success(`Deleted ${bk.archiveName}`);
                    }}
                    className="ml-auto inline-flex h-7 items-center gap-1 rounded-md border border-rose-500/25 bg-rose-500/10 px-2 text-[11px] text-rose-300 cursor-pointer"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>

        {/* Pagination Footer */}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.07] bg-[#09090b]/50 px-4 py-2.5 text-xs text-zinc-400">
          <div className="flex items-center gap-2 font-mono text-[11px] tabular-nums">
            <span>
              Page <strong className="text-zinc-200">{currentPage}</strong> of{' '}
              <strong className="text-zinc-200">{computedTotalPages}</strong> ({totalCount} total)
            </span>
            <select
              aria-label="Rows per page"
              value={perPage}
              onChange={(e) => {
                setPerPage(Number(e.target.value));
                setPage(1);
              }}
              className="rounded border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[11px] text-zinc-200"
            >
              <option value={5}>5 / page</option>
              <option value={10}>10 / page</option>
              <option value={20}>20 / page</option>
            </select>
          </div>

          <div className="flex items-center gap-1">
            <button
              type="button"
              disabled={currentPage <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              className="inline-flex h-7 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer"
            >
              <ChevronLeft className="h-3.5 w-3.5" />
              Prev
            </button>
            <button
              type="button"
              disabled={currentPage >= computedTotalPages}
              onClick={() => setPage((p) => Math.min(computedTotalPages, p + 1))}
              className="inline-flex h-7 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer"
            >
              Next
              <ChevronRight className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
      </div>

      {/* Add / Edit Schedule Modal */}
      <ShadcnDialog
        open={scheduleModalOpen}
        onClose={() => setScheduleModalOpen(false)}
        title={editingScheduleId ? 'Edit Backup Schedule' : 'Add Backup Schedule'}
        icon={<Calendar className="h-4 w-4 text-orange-400" />}
      >
        <form onSubmit={handleSaveSchedule} className="space-y-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Storage Connector
              </label>
              <select
                value={schedDestId}
                onChange={(e) => setSchedDestId(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              >
                {destinations.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Backup Scope
              </label>
              <select
                value={schedType}
                onChange={(e) => setSchedType(e.target.value as 'full' | 'volume' | 'app')}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              >
                {isPhp ? (
                  <option value="app">Site files + databases</option>
                ) : (
                  <>
                    <option value="full">Full Backup (App + All Volumes)</option>
                    <option value="volume">Docker Volume(s) Only</option>
                    <option value="app">App Files Only</option>
                  </>
                )}
              </select>
            </div>
          </div>

          {schedType === 'volume' && app.volumes.length > 0 && (
            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 space-y-1.5">
              <span className="text-[11px] text-zinc-400 block">Select Volumes</span>
              <div className="flex flex-wrap gap-2">
                {app.volumes.map((v) => {
                  const checked = schedSelectedVols.includes(v.name);
                  return (
                    <label
                      key={v.name}
                      className="inline-flex items-center gap-1.5 font-mono text-xs text-zinc-200 cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleSchedVolume(v.name)}
                        className="h-3.5 w-3.5 accent-orange-500"
                      />
                      <span>{v.name}</span>
                    </label>
                  );
                })}
              </div>
            </div>
          )}

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Schedule Frequency
              </label>
              <select
                value={schedCronPreset}
                onChange={(e) => {
                  setSchedCronPreset(e.target.value);
                  if (e.target.value !== 'custom') {
                    setSchedCustomCron(e.target.value);
                  }
                }}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              >
                {CRON_PRESETS.map((p) => (
                  <option key={p.value} value={p.value}>
                    {p.label}
                  </option>
                ))}
                <option value="custom">Custom Cron Expression...</option>
              </select>
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Keep Last N Backups (Retention)
              </label>
              <input
                type="number"
                min={1}
                max={365}
                value={schedRetention}
                onChange={(e) => setSchedRetention(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
          </div>

          {schedCronPreset === 'custom' && (
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1">
                5-Field Cron Expression (min hour dom mon dow)
              </label>
              <input
                type="text"
                required
                value={schedCustomCron}
                onChange={(e) => setSchedCustomCron(e.target.value)}
                placeholder="0 2 * * *"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-orange-400"
              />
            </div>
          )}

          {!isPhp && (
            <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] p-3">
              <span className="text-xs text-zinc-200">
                Pause containers during scheduled backup
              </span>
              <ShadcnSwitch checked={schedPauseContainers} onChange={setSchedPauseContainers} />
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setScheduleModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              {editingScheduleId ? 'Save Changes' : 'Create Schedule'}
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* Rclone Log Viewer Modal (/apps/:id/backup/history/:historyid/log) */}
      <ShadcnDialog
        open={Boolean(logModalData)}
        onClose={() => setLogModalData(null)}
        title={`Backup Log · ${logModalData?.title || ''}`}
        icon={<FileText className="h-4 w-4 text-orange-400" />}
        maxWidth="lg"
      >
        {logModalData && (
          <div className="space-y-3">
            {logModalData.errorMessage && (
              <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 p-2.5 font-mono text-xs text-rose-300">
                Error: {logModalData.errorMessage}
              </div>
            )}
            <pre className="max-h-80 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#070709] p-3.5 font-mono text-xs text-zinc-300 whitespace-pre-wrap">
              {logModalData.log}
            </pre>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={() => setLogModalData(null)}
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Close
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* Restore Backup Modal with Live Polling (/apps/:id/backup/restore/:historyid & /restore-status) */}
      <ShadcnDialog
        open={Boolean(restoreTarget)}
        onClose={() => setRestoreTarget(null)}
        title="Restore Project Backup"
        icon={<RotateCcw className="h-4 w-4 text-orange-400" />}
        maxWidth="lg"
      >
        {restoreTarget && (
          <div className="space-y-4">
            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 font-mono text-xs space-y-1">
              <div className="text-zinc-100 font-semibold">{restoreTarget.archiveName}</div>
              <div className="text-[11px] text-zinc-400">
                Connector: {restoreTarget.destinationName} · Scope:{' '}
                {restoreTarget.backupType || restoreTarget.type || 'full'}
              </div>
            </div>

            {restoreJobState.status !== 'idle' && (
              <div className="space-y-2">
                <div className="flex items-center justify-between font-mono text-xs">
                  <span className="flex items-center gap-1.5 text-zinc-200">
                    {restoreJobState.status === 'running' && (
                      <RotateCw className="h-3.5 w-3.5 animate-spin text-orange-400" />
                    )}
                    {restoreJobState.status === 'completed' && (
                      <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
                    )}
                    {restoreJobState.status === 'failed' && (
                      <AlertTriangle className="h-3.5 w-3.5 text-rose-400" />
                    )}
                    <span className="uppercase">{restoreJobState.status}</span>
                  </span>
                </div>
                <pre className="max-h-56 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#070709] p-3 font-mono text-xs text-zinc-300 whitespace-pre-wrap">
                  {restoreJobState.log}
                </pre>
              </div>
            )}

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setRestoreTarget(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Close
              </button>
              {restoreJobState.status !== 'completed' && (
                <button
                  type="button"
                  disabled={restoreJobState.status === 'running'}
                  onClick={() => void handleConfirmRestore()}
                  className="rounded-lg bg-orange-500 px-4 py-1.5 text-xs font-semibold text-white hover:bg-orange-400 disabled:opacity-50 cursor-pointer"
                >
                  {restoreJobState.status === 'running' ? 'Restoring...' : 'Confirm Restore'}
                </button>
              )}
            </div>
          </div>
        )}
      </ShadcnDialog>
    </div>
  );
};
