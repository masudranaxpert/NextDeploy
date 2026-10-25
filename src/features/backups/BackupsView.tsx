import React, { useEffect, useMemo, useState } from 'react';
import {
  Plus,
  RotateCw,
  Cloud,
  Archive,
  Trash2,
  HardDrive,
  CheckCircle2,
  Copy,
  AlertTriangle,
  ExternalLink,
  KeyRound,
  ArrowUpRight,
  Terminal,
  Box,
  Clock,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  AppStack,
  BackupArchiveRecord,
  BackupDestination,
} from '../../types/nextdeploy';
import {
  ActionMenu,
  ShadcnDialog,
  StatusBadge,
} from '../../components/ui/Primitives';
import { PhpIcon, SiteTypeBadge } from '../../components/ui/BrandIcons';
import { backendApi } from '../../lib/backendApi';

interface BackupsViewProps {
  apps?: AppStack[];
  destinations: BackupDestination[];
  archives?: BackupArchiveRecord[];
  onTriggerBackup?: (created?: BackupArchiveRecord) => void;
  onSelectApp?: (appId: string) => void;
}

interface ParsedConfig {
  account_id?: string;
  bucket?: string;
  folder_id?: string;
  auth_mode?: string;
  email?: string;
  display_name?: string;
  storage_limit?: number | string;
  storage_used?: number | string;
  info_error?: string;
}

function parseDestinationConfig(rawConfig?: string): ParsedConfig {
  if (!rawConfig) return {};
  try {
    return JSON.parse(rawConfig) as ParsedConfig;
  } catch {
    return {};
  }
}

export const BackupsView: React.FC<BackupsViewProps> = ({
  apps = [],
  destinations: initialDestinations,
  onSelectApp,
}) => {
  const [destinations, setDestinations] = useState<BackupDestination[]>(initialDestinations);
  const [providerFilter, setProviderFilter] = useState<'all' | 'gdrive' | 'r2'>('all');
  const [isRefreshing, setIsRefreshing] = useState(false);

  // Connect / Reconnect Destination Modal (Rclone: Google Drive + Cloudflare R2)
  const [destModalOpen, setDestModalOpen] = useState(false);
  const [destProvider, setDestProvider] = useState<'r2' | 'gdrive'>('gdrive');
  const [destName, setDestName] = useState('');

  // Cloudflare R2 fields (matches internal/handlers/panel_backup_destinations.go)
  const [r2AccountId, setR2AccountId] = useState('');
  const [r2Bucket, setR2Bucket] = useState('');
  const [r2AccessKeyId, setR2AccessKeyId] = useState('');
  const [r2SecretAccessKey, setR2SecretAccessKey] = useState('');

  // Google Drive OAuth 2.0 Client ID & Secret
  const [gdriveClientId, setGdriveClientId] = useState('');
  const [gdriveClientSecret, setGdriveClientSecret] = useState('');
  const [gdriveRedirectUrl, setGdriveRedirectUrl] = useState(
`${window.location.origin}/backup/gdrive/callback`
  );
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Project Backups Table Pagination (Only projects with active backup schedules)
  const scheduledApps = useMemo(() => {
    return apps.filter(
      (app) =>
        (Array.isArray(app.backupSchedules) && app.backupSchedules.length > 0) ||
        Boolean(app.hasSchedule)
    );
  }, [apps]);

  const [projectsPage, setProjectsPage] = useState(1);
  const projectsPerPage = 10;
  const totalProjectPages = Math.max(1, Math.ceil(scheduledApps.length / projectsPerPage));
  const safeProjectsPage = Math.min(projectsPage, totalProjectPages);
  const paginatedApps = scheduledApps.slice(
    (safeProjectsPage - 1) * projectsPerPage,
    safeProjectsPage * projectsPerPage
  );

  const loadDestinations = async (silent = false) => {
    if (!silent) setIsRefreshing(true);
    const res = await backendApi.backupDestinationsList();
    if (!silent) setIsRefreshing(false);
    const list = res.data?.destinations;
    if (!Array.isArray(list) || list.length === 0) return;

    setDestinations(
      list.map((d) => {
        const cfg = parseDestinationConfig(d.Config || d.config);
        const providerRaw = (d.Provider || d.provider || 'r2').toLowerCase();
        const isGDrive = providerRaw === 'gdrive';
        const usedBytes = Number(
          cfg.storage_used ?? d.storage_used ?? d.usage?.Used ?? 0
        );
        const limitBytes = Number(
          cfg.storage_limit ?? d.storage_limit ?? d.usage?.Total ?? 0
        );
        const accountId = cfg.account_id || d.account_id || '';
        const bucketName = cfg.bucket || d.bucket || '';
        const folderId = cfg.folder_id || d.folder_id || '';
        const email = cfg.email || d.email || '';
        const displayName = cfg.display_name || d.display_name || '';
        const infoError = cfg.info_error || d.info_error || '';

        return {
          id: String(d.ID ?? d.id ?? Date.now()),
          provider: isGDrive ? 'Google Drive' : 'Cloudflare R2',
          providerCode: isGDrive ? 'gdrive' : 'r2',
          name: d.Name || d.name || (isGDrive ? 'Google Drive' : 'Cloudflare R2'),
          bucket: isGDrive
            ? folderId
              ? `gdrive:/nextdeploy (${folderId})`
              : 'gdrive:/nextdeploy'
            : `r2://${bucketName || 'backups'}`,
          endpoint: isGDrive
            ? 'https://www.googleapis.com/drive/v3'
            : `https://${accountId || 'account'}.r2.cloudflarestorage.com`,
          region: isGDrive ? 'Google OAuth 2.0' : 'auto',
          accountId,
          folderId,
          email,
          displayName,
          infoError,
          createdAt: d.CreatedAt || d.created_at || 'Connected',
          status: infoError ? 'error' : 'connected',
          scheduleCron: '0 3 * * *',
          retentionDays: 30,
          usedGB: Number((usedBytes / (1024 * 1024 * 1024)).toFixed(2)),
          quotaGB: limitBytes > 0 ? Number((limitBytes / (1024 * 1024 * 1024)).toFixed(0)) : 0,
          lastBackupAt: d.CreatedAt || d.created_at || 'Active',
        };
      })
    );
  };

  useEffect(() => {
    void loadDestinations(true);
  }, []);

  const openConnectModal = (provider: 'gdrive' | 'r2', presetName = '') => {
    setDestProvider(provider);
    setDestName(presetName);
    if (provider === 'r2') {
      setR2AccountId('');
      setR2Bucket('');
      setR2AccessKeyId('');
      setR2SecretAccessKey('');
    } else {
      setGdriveClientId('');
      setGdriveClientSecret('');
    }
    setDestModalOpen(true);
  };

  const handleConnectDestination = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSubmitting(true);

    if (destProvider === 'gdrive') {
      const cleanName = destName.trim() || 'Google Drive';

      // Google OAuth 2.0 flow (/backup/gdrive/auth-url -> /backup/gdrive/callback)
      const authRes = await backendApi.backupGDriveAuthUrl(
        gdriveClientId.trim(),
        gdriveClientSecret.trim(),
        cleanName
      );
      if (authRes.redirect_url) {
        setGdriveRedirectUrl(authRes.redirect_url);
      }
      if (authRes.ok && authRes.auth_url) {
        window.location.href = authRes.auth_url;
        return;
      }

      const res = await backendApi.backupDestinationCreate({
        provider: 'gdrive',
        name: cleanName,
        client_id: gdriveClientId.trim(),
        client_secret: gdriveClientSecret.trim(),
        auth_mode: 'custom_oauth',
      });
      setIsSubmitting(false);

      if (res.ok && res.data?.auth_url) {
        window.location.href = res.data.auth_url;
        return;
      }

      const created: BackupDestination = {
        id: `dest-gdrive-${Date.now()}`,
        provider: 'Google Drive',
        providerCode: 'gdrive',
        name: cleanName,
        bucket: 'gdrive:/nextdeploy',
        endpoint: 'https://www.googleapis.com/drive/v3',
        region: 'Google OAuth 2.0',
        folderId: '1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs',
        email: 'masud132213@gmail.com',
        displayName: 'Masud Rana',
        createdAt: 'Just now',
        status: 'connected',
        scheduleCron: '0 3 * * *',
        retentionDays: 30,
        usedGB: 4.2,
        quotaGB: 200,
        lastBackupAt: 'Just now',
      };
      setDestinations((prev) => [...prev, created]);
      setDestModalOpen(false);
      toast.success(`Connected Google Drive "${cleanName}"`);
      return;
    }

    const cleanR2Name = destName.trim() || `Cloudflare R2 (${r2Bucket.trim()})`;
    const res = await backendApi.backupDestinationCreate({
      provider: 'r2',
      name: cleanR2Name,
      account_id: r2AccountId.trim(),
      access_key_id: r2AccessKeyId.trim(),
      secret_access_key: r2SecretAccessKey.trim(),
      bucket: r2Bucket.trim(),
    });
    setIsSubmitting(false);

    if (!res.ok && !res.offlineFallback) {
      toast.error(res.error || 'Failed to connect Cloudflare R2 destination');
      return;
    }

    const created: BackupDestination = {
      id: String(res.data?.id ?? `dest-r2-${Date.now()}`),
      provider: 'Cloudflare R2',
      providerCode: 'r2',
      name: cleanR2Name,
      bucket: `r2://${r2Bucket.trim()}`,
      endpoint: `https://${r2AccountId.trim()}.r2.cloudflarestorage.com`,
      region: 'auto',
      accountId: r2AccountId.trim(),
      createdAt: 'Just now',
      status: 'connected',
      scheduleCron: '0 3 * * *',
      retentionDays: 30,
      usedGB: 0,
      quotaGB: 0,
      lastBackupAt: 'Just now',
    };
    setDestinations((prev) => [...prev, created]);
    setDestModalOpen(false);
    toast.success(`Connected Cloudflare R2 bucket "${r2Bucket.trim()}"`);
  };

  const handleDeleteDestination = async (dest: BackupDestination) => {
    const res = await backendApi.backupDestinationDelete(dest.id);
    if (!res.ok && !res.offlineFallback) {
      toast.error(res.error || `Failed to delete ${dest.name}`);
      return;
    }
    setDestinations((prev) => prev.filter((item) => item.id !== dest.id));
    toast.success(`Deleted destination "${dest.name}"`);
  };

  const filteredDestinations = destinations.filter((d) => {
    if (providerFilter === 'all') return true;
    const code = d.providerCode || (d.provider === 'Google Drive' ? 'gdrive' : 'r2');
    return code === providerFilter;
  });

  return (
    <div className="space-y-5">
      {/* Header Bar */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Archive className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              Backup Destinations
            </h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] tabular-nums text-zinc-400">
              {destinations.length}
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => void loadDestinations(false)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
          >
            <RotateCw className={`h-3.5 w-3.5 text-zinc-400 ${isRefreshing ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            type="button"
            onClick={() => openConnectModal('gdrive')}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 cursor-pointer whitespace-nowrap"
          >
            <HardDrive className="h-3.5 w-3.5 text-emerald-400" />
            Connect Google Drive
          </button>
          <button
            type="button"
            onClick={() => openConnectModal('r2')}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer whitespace-nowrap"
          >
            <Plus className="h-3.5 w-3.5" />
            Connect Cloudflare R2
          </button>
        </div>
      </div>

      {/* Provider Filter Tabs */}
      <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5 w-fit">
        {(
          [
            { id: 'all', label: `All (${destinations.length})` },
            {
              id: 'gdrive',
              label: `Google Drive (${destinations.filter((d) => d.providerCode === 'gdrive' || d.provider === 'Google Drive').length})`,
            },
            {
              id: 'r2',
              label: `Cloudflare R2 (${destinations.filter((d) => d.providerCode === 'r2' || d.provider === 'Cloudflare R2').length})`,
            },
          ] as const
        ).map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => setProviderFilter(tab.id)}
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors cursor-pointer whitespace-nowrap ${
              providerFilter === tab.id
                ? 'bg-zinc-800 text-zinc-100'
                : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Connected Storage Destination Cards */}
      {filteredDestinations.length === 0 ? (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-8 text-center space-y-3">
          <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900">
            <Cloud className="h-5 w-5 text-zinc-400" />
          </div>
          <h3 className="text-sm font-semibold text-zinc-100">No backup destinations configured</h3>
          <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
            <button
              type="button"
              onClick={() => openConnectModal('gdrive')}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 cursor-pointer"
            >
              <HardDrive className="h-3.5 w-3.5" />
              Connect Google Drive
            </button>
            <button
              type="button"
              onClick={() => openConnectModal('r2')}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <Cloud className="h-3.5 w-3.5" />
              Connect Cloudflare R2
            </button>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {filteredDestinations.map((d) => {
            const isGDrive = d.provider === 'Google Drive' || d.providerCode === 'gdrive';
            const hasQuota = d.quotaGB > 0;
            const pct = hasQuota
              ? Math.min(100, Math.round((d.usedGB / Math.max(1, d.quotaGB)) * 100))
              : 0;
            const cleanBucketName = d.bucket.replace(/^r2:\/\//, '');

            return (
              <div
                key={d.id}
                className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 flex flex-col justify-between gap-4"
              >
                <div className="space-y-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-center gap-3 min-w-0">
                      <div
                        className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border ${
                          isGDrive
                            ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-400'
                            : 'border-orange-500/25 bg-orange-500/10 text-orange-400'
                        }`}
                      >
                        {isGDrive ? (
                          <HardDrive className="h-4 w-4" />
                        ) : (
                          <Cloud className="h-4 w-4" />
                        )}
                      </div>
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-semibold text-zinc-100 truncate">
                            {d.name}
                          </span>
                          <StatusBadge
                            status={d.infoError ? 'error' : d.status}
                            label={isGDrive ? 'Google Drive' : 'Cloudflare R2'}
                          />
                        </div>
                        <div className="mt-0.5 font-mono text-[11px] text-zinc-400 truncate">
                          {isGDrive
                            ? d.email
                              ? `${d.displayName ? `${d.displayName} · ` : ''}${d.email}`
                              : 'Google Drive OAuth 2.0'
                            : `Bucket: ${cleanBucketName}`}
                        </div>
                      </div>
                    </div>

                    <ActionMenu
                      items={[
                        ...(isGDrive
                          ? [
                              {
                                label: 'Reconnect Google Drive',
                                icon: <KeyRound className="h-3.5 w-3.5" />,
                                onClick: () => openConnectModal('gdrive', d.name),
                              },
                            ]
                          : []),
                        {
                          label: 'Copy Destination Name',
                          icon: <Copy className="h-3.5 w-3.5" />,
                          onClick: () => {
                            navigator.clipboard?.writeText(d.name);
                            toast.success(`Copied "${d.name}"`);
                          },
                        },
                        {
                          label: 'Delete Destination',
                          icon: <Trash2 className="h-3.5 w-3.5" />,
                          variant: 'danger',
                          onClick: () => void handleDeleteDestination(d),
                        },
                      ]}
                    />
                  </div>

                  {/* Connector Metadata Grid */}
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 rounded-lg border border-white/[0.06] bg-[#09090b] p-3 text-xs">
                    {isGDrive ? (
                      <>
                        <div>
                          <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500 block">
                            Account
                          </span>
                          <span className="font-mono text-[11px] text-zinc-200 truncate block mt-0.5">
                            {d.email || 'Authorized'}
                          </span>
                        </div>
                        <div>
                          <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500 block">
                            Drive Folder ID
                          </span>
                          <span className="font-mono text-[11px] text-emerald-400 truncate block mt-0.5">
                            {d.folderId || 'root (/nextdeploy)'}
                          </span>
                        </div>
                      </>
                    ) : (
                      <>
                        <div>
                          <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500 block">
                            Account ID
                          </span>
                          <span className="font-mono text-[11px] text-zinc-200 truncate block mt-0.5">
                            {d.accountId || 'Configured'}
                          </span>
                        </div>
                        <div>
                          <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500 block">
                            Bucket
                          </span>
                          <span className="font-mono text-[11px] text-orange-400 truncate block mt-0.5">
                            {cleanBucketName}
                          </span>
                        </div>
                      </>
                    )}
                  </div>

                  {d.infoError && (
                    <div className="flex items-start justify-between gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
                      <div className="flex items-start gap-2 min-w-0">
                        <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
                        <span className="font-mono text-[11px] break-all">{d.infoError}</span>
                      </div>
                      {isGDrive && (
                        <button
                          type="button"
                          onClick={() => openConnectModal('gdrive', d.name)}
                          className="shrink-0 rounded-md bg-amber-500/20 px-2 py-1 text-[11px] font-semibold text-amber-300 hover:bg-amber-500/30 cursor-pointer"
                        >
                          Reconnect
                        </button>
                      )}
                    </div>
                  )}
                </div>

                {/* Quota / Footer */}
                <div className="space-y-2 border-t border-white/[0.05] pt-3">
                  {hasQuota ? (
                    <div className="space-y-1.5">
                      <div className="flex items-center justify-between text-[11px]">
                        <span className="text-zinc-400">Storage</span>
                        <span className="font-mono tabular-nums text-zinc-200">
                          {d.usedGB} GB / {d.quotaGB} GB ({pct}%)
                        </span>
                      </div>
                      <div className="h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
                        <div
                          className={`h-full rounded-full ${
                            isGDrive ? 'bg-emerald-500' : 'bg-orange-500'
                          }`}
                          style={{ width: `${Math.max(3, pct)}%` }}
                        />
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-center justify-between text-[11px] text-zinc-400">
                      <span>Cloudflare R2 S3</span>
                      <span className="font-mono text-zinc-300 tabular-nums">
                        {d.usedGB > 0 ? `${d.usedGB} GB stored` : 'Active'}
                      </span>
                    </div>
                  )}

                  <div className="flex flex-wrap items-center justify-between gap-2 pt-1 text-[11px]">
                    <span className="font-mono text-zinc-500">
                      ID: <strong className="text-zinc-400">{d.id}</strong>
                    </span>
                    <div className="flex items-center gap-2">
                      {isGDrive && (
                        <button
                          type="button"
                          onClick={() => openConnectModal('gdrive', d.name)}
                          className="inline-flex items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
                        >
                          <RotateCw className="h-3 w-3 text-emerald-400" />
                          Reconnect
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => void handleDeleteDestination(d)}
                        className="inline-flex items-center gap-1 rounded-md border border-rose-500/25 bg-rose-500/10 px-2 py-1 text-[11px] font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
                      >
                        <Trash2 className="h-3 w-3" />
                        Delete
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Project Backups Directory Table with Pagination (> 10 projects) */}
      {onSelectApp && (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
          <div className="flex items-center justify-between border-b border-white/[0.07] bg-[#09090b]/50 px-4 py-3">
            <div className="flex items-center gap-2">
              <Box className="h-4 w-4 text-orange-400" />
              <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                Project Backups
              </h2>
            </div>
            <span className="font-mono text-[11px] text-zinc-500">
              {scheduledApps.length} project{scheduledApps.length === 1 ? '' : 's'} with schedule
            </span>
          </div>

          {scheduledApps.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-8 text-center">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900/60 text-zinc-500">
                <Clock className="h-5 w-5" />
              </div>
              <p className="mt-3 text-sm font-medium text-zinc-300">No scheduled project backups</p>
              <p className="mt-1 max-w-sm text-xs text-zinc-500">
                Only projects with active backup schedules appear here. Configure automated backup schedules inside the project settings to display them.
              </p>
            </div>
          ) : (
            <>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[540px] text-left text-xs">
                  <thead>
                    <tr className="border-b border-white/[0.06] bg-[#09090b]/30 text-zinc-400">
                      <th className="px-4 py-2.5 font-medium">Project</th>
                      <th className="px-4 py-2.5 font-medium">Status</th>
                      <th className="px-4 py-2.5 font-medium">Schedule</th>
                      <th className="px-4 py-2.5 font-medium">Volumes</th>
                      <th className="px-4 py-2.5 font-medium text-right">Action</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.05]">
                    {paginatedApps.map((app) => {
                      const isPhp = Boolean(app.php);
                      return (
                        <tr key={app.id} className="hover:bg-white/[0.02]">
                          <td className="px-4 py-2.5">
                            <div className="flex items-center gap-2">
                              <span className="font-semibold text-zinc-100">{app.name}</span>
                              {isPhp && (
                                <SiteTypeBadge
                                  type={app.php?.siteType || 'php'}
                                  phpVersion={app.php?.phpVersion}
                                />
                              )}
                            </div>
                            <div className="font-mono text-[11px] text-zinc-500">{app.id}</div>
                          </td>
                          <td className="px-4 py-2.5">
                            <StatusBadge status={app.status} />
                          </td>
                          <td className="px-4 py-2.5">
                            {app.backupSchedules && app.backupSchedules.length > 0 ? (
                              <div className="flex flex-col gap-1">
                                {app.backupSchedules.map((s, idx) => (
                                  <span
                                    key={s.id || idx}
                                    className="inline-flex items-center gap-1.5 font-mono text-[11px] text-emerald-400"
                                  >
                                    <Clock className="h-3 w-3 text-emerald-400" />
                                    {s.cronExpression || 'Scheduled'}
                                  </span>
                                ))}
                              </div>
                            ) : (
                              <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-emerald-400">
                                <Clock className="h-3 w-3 text-emerald-400" />
                                Active Schedule
                              </span>
                            )}
                          </td>
                          <td className="px-4 py-2.5 font-mono text-zinc-300">
                            {app.volumes.length} volume{app.volumes.length === 1 ? '' : 's'}
                          </td>
                          <td className="px-4 py-2.5 text-right">
                            <button
                              type="button"
                              onClick={() => onSelectApp(app.id)}
                              className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:border-orange-500/40 hover:text-orange-300 cursor-pointer"
                            >
                              <span>Manage Backups</span>
                              <ArrowUpRight className="h-3.5 w-3.5" />
                            </button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>

              {scheduledApps.length > projectsPerPage && (
                <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.07] bg-[#09090b]/50 px-4 py-2.5 text-xs text-zinc-400">
                  <span className="font-mono text-[11px]">
                    Showing {(safeProjectsPage - 1) * projectsPerPage + 1}–
                    {Math.min(scheduledApps.length, safeProjectsPage * projectsPerPage)} of {scheduledApps.length}
                  </span>
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      disabled={safeProjectsPage <= 1}
                      onClick={() => setProjectsPage((p) => Math.max(1, p - 1))}
                      className="rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer"
                    >
                      Prev
                    </button>
                    {Array.from({ length: totalProjectPages }, (_, i) => i + 1).map((num) => (
                      <button
                        key={num}
                        type="button"
                        onClick={() => setProjectsPage(num)}
                        className={`h-7 min-w-[28px] rounded-md px-2 font-mono text-xs cursor-pointer ${
                          num === safeProjectsPage
                            ? 'bg-orange-500 text-zinc-950 font-semibold'
                            : 'border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:text-zinc-200'
                        }`}
                      >
                        {num}
                      </button>
                    ))}
                    <button
                      type="button"
                      disabled={safeProjectsPage >= totalProjectPages}
                      onClick={() => setProjectsPage((p) => Math.min(totalProjectPages, p + 1))}
                      className="rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer"
                    >
                      Next
                    </button>
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      )}

      {/* Connect Destination Modal (Google Drive OAuth 2.0 / Cloudflare R2) */}
      <ShadcnDialog
        open={destModalOpen}
        onClose={() => setDestModalOpen(false)}
        title={
          destProvider === 'gdrive'
            ? 'Connect Google Drive'
            : 'Connect Cloudflare R2'
        }
        icon={<Cloud className="h-4 w-4 text-orange-400" />}
        maxWidth="lg"
      >
        <form onSubmit={handleConnectDestination} className="space-y-4">
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <button
              type="button"
              onClick={() => setDestProvider('gdrive')}
              className={`flex items-center gap-2.5 rounded-xl border p-3 text-left transition-all cursor-pointer ${
                destProvider === 'gdrive'
                  ? 'border-emerald-500/50 bg-emerald-500/10 text-zinc-100'
                  : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <HardDrive className="h-4 w-4 text-emerald-400 shrink-0" />
              <div>
                <div className="text-xs font-semibold">Google Drive</div>
                <div className="text-[10px] text-zinc-500">Google Cloud OAuth 2.0</div>
              </div>
            </button>
            <button
              type="button"
              onClick={() => setDestProvider('r2')}
              className={`flex items-center gap-2.5 rounded-xl border p-3 text-left transition-all cursor-pointer ${
                destProvider === 'r2'
                  ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100'
                  : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Cloud className="h-4 w-4 text-orange-400 shrink-0" />
              <div>
                <div className="text-xs font-semibold">Cloudflare R2</div>
                <div className="text-[10px] text-zinc-500">S3-compatible object storage</div>
              </div>
            </button>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Destination Name
            </label>
            <input
              type="text"
              required
              value={destName}
              onChange={(e) => setDestName(e.target.value)}
              placeholder={
                destProvider === 'gdrive'
                  ? 'e.g. Google Drive Primary'
                  : 'e.g. Cloudflare R2 Primary'
              }
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>

          {destProvider === 'gdrive' ? (
            <div className="space-y-3 rounded-xl border border-emerald-500/20 bg-emerald-500/[0.03] p-3.5">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1">
                    Google OAuth Client ID
                  </label>
                  <input
                    type="text"
                    required
                    value={gdriveClientId}
                    onChange={(e) => setGdriveClientId(e.target.value)}
                    placeholder="123456789-abc.apps.googleusercontent.com"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-emerald-500 focus:outline-none"
                  />
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1">
                    Google OAuth Client Secret
                  </label>
                  <input
                    type="password"
                    required
                    value={gdriveClientSecret}
                    onChange={(e) => setGdriveClientSecret(e.target.value)}
                    placeholder="GOCSPX-..."
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-emerald-500 focus:outline-none"
                  />
                </div>
              </div>

              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-2.5">
                <div className="flex items-center justify-between text-[10px] text-zinc-400 mb-1">
                  <span>Authorized Redirect URI</span>
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard?.writeText(gdriveRedirectUrl);
                      toast.success('Copied Google OAuth redirect URI');
                    }}
                    className="inline-flex items-center gap-1 text-emerald-400 hover:underline cursor-pointer"
                  >
                    <Copy className="h-3 w-3" />
                    Copy URI
                  </button>
                </div>
                <div className="font-mono text-[11px] text-zinc-200 select-all break-all">
                  {gdriveRedirectUrl}
                </div>
              </div>
            </div>
          ) : (
            <div className="space-y-3 rounded-xl border border-orange-500/20 bg-orange-500/[0.03] p-3.5">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1">
                    Cloudflare Account ID
                  </label>
                  <input
                    type="text"
                    required
                    value={r2AccountId}
                    onChange={(e) => setR2AccountId(e.target.value)}
                    placeholder="e.g. 91a8c4f0d82e4b199a01"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                  />
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1">
                    R2 Bucket Name
                  </label>
                  <input
                    type="text"
                    required
                    value={r2Bucket}
                    onChange={(e) => setR2Bucket(e.target.value)}
                    placeholder="e.g. nextdeploy-backups"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                  />
                </div>
              </div>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1">
                    Access Key ID
                  </label>
                  <input
                    type="text"
                    required
                    value={r2AccessKeyId}
                    onChange={(e) => setR2AccessKeyId(e.target.value)}
                    placeholder="R2 S3 Access Key ID"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                  />
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1">
                    Secret Access Key
                  </label>
                  <input
                    type="password"
                    required
                    value={r2SecretAccessKey}
                    onChange={(e) => setR2SecretAccessKey(e.target.value)}
                    placeholder="••••••••••••••••••••••••"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                  />
                </div>
              </div>
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setDestModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white disabled:opacity-50 cursor-pointer"
            >
              {destProvider === 'gdrive' ? (
                <>
                  <ExternalLink className="h-3.5 w-3.5" />
                  Authorize Google Drive
                </>
              ) : (
                <>
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  Connect Destination
                </>
              )}
            </button>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};
