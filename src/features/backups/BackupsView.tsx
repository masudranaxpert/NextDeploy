import { OwnerBadge } from '../../components/ui/OwnerBadge';
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
  AlertCircle,
  ExternalLink,
  KeyRound,
  ArrowUpRight,
  Terminal,
  Box,
  Clock,
  Activity,
  Folder,
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
import { copyToClipboard } from '../../lib/clipboard';

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

const G_DRIVE_OAUTH_URL =
  'https://accounts.google.com/o/oauth2/v2/auth?access_type=offline&client_id=202264815644.apps.googleusercontent.com&prompt=consent&redirect_uri=http%3A%2F%2F127.0.0.1%3A53682%2F&response_type=code&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fdrive%20https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fuserinfo.email%20https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fuserinfo.profile';

function extractGoogleCode(raw: string): string {
  const t = raw.trim();
  if (!t) return '';
  if (t.includes('code=')) {
    try {
      const u = new URL(t.startsWith('http') ? t : `http://${t}`);
      const c = u.searchParams.get('code');
      if (c) return decodeURIComponent(c);
    } catch {
      const m = t.match(/[?&]code=([^&#]+)/);
      if (m && m[1]) return decodeURIComponent(m[1]);
    }
  }
  return t;
}

export const BackupsView: React.FC<BackupsViewProps> = ({
  apps = [],
  destinations: initialDestinations,
  onSelectApp,
}) => {
  const [destinations, setDestinations] = useState<BackupDestination[]>(initialDestinations);
  const [providerFilter, setProviderFilter] = useState<'all' | 'gdrive' | 'r2'>('all');
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [testingDestId, setTestingDestId] = useState<string | null>(null);

  // Connect / Reconnect Destination Modal (Rclone: Google Drive + Cloudflare R2)
  const [destModalOpen, setDestModalOpen] = useState(false);
  const [destProvider, setDestProvider] = useState<'r2' | 'gdrive'>('gdrive');
  const [destName, setDestName] = useState('');
  const [editDestId, setEditDestId] = useState<string | null>(null);

  // Google Drive mode: quick (Rclone default) vs custom (User provided client credentials)
  const [gdriveMode, setGdriveMode] = useState<'quick' | 'custom'>('quick');
  const [gdriveInput, setGdriveInput] = useState('');
  const extractedCode = extractGoogleCode(gdriveInput);

  // Cloudflare R2 fields (matches internal/handlers/panel_backup_destinations.go)
  const [r2AccountId, setR2AccountId] = useState('');
  const [r2Bucket, setR2Bucket] = useState('');
  const [r2AccessKeyId, setR2AccessKeyId] = useState('');
  const [r2SecretAccessKey, setR2SecretAccessKey] = useState('');

  // Google Drive OAuth 2.0 Client ID & Secret (Custom mode)
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
    if (!Array.isArray(list)) return;
    if (list.length === 0) {
      setDestinations([]);
      return;
    }

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

  const openConnectModal = (
    provider: 'gdrive' | 'r2',
    presetName = '',
    destId: string | null = null
  ) => {
    setDestProvider(provider);
    setDestName(presetName);
    setEditDestId(destId);
    setGdriveInput('');
    setGdriveMode('quick');
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

  const handleTestDestination = async (destId: string) => {
    setTestingDestId(destId);
    try {
      const res = await backendApi.backupDestinationTest(destId);
      if (res.ok && res.data?.ok) {
        toast.success(res.data.message || 'Connection verified successfully');
        setDestinations((prev) =>
          prev.map((d) => {
            if (d.id === destId) {
              const updatedUsedGB = res.data?.storage_used != null
                ? Number((Number(res.data.storage_used) / (1024 * 1024 * 1024)).toFixed(2))
                : d.usedGB;
              const updatedQuotaGB = res.data?.storage_limit != null && Number(res.data.storage_limit) > 0
                ? Number((Number(res.data.storage_limit) / (1024 * 1024 * 1024)).toFixed(0))
                : d.quotaGB;
              return {
                ...d,
                status: 'connected',
                infoError: undefined,
                email: res.data?.email || d.email,
                displayName: res.data?.display_name || d.displayName,
                folderId: res.data?.folder_id || d.folderId,
                usedGB: updatedUsedGB,
                quotaGB: updatedQuotaGB,
              };
            }
            return d;
          })
        );
      } else {
        const errMsg = res.data?.error || res.error || 'Connection test failed';
        toast.error(errMsg);
        setDestinations((prev) =>
          prev.map((d) => (d.id === destId ? { ...d, infoError: errMsg, status: 'error' } : d))
        );
      }
    } catch (err: any) {
      toast.error(err?.message || 'Connection test encountered an error');
    } finally {
      setTestingDestId(null);
    }
  };

  const handleConnectDestination = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSubmitting(true);

    if (destProvider === 'gdrive') {
      const cleanName = destName.trim() || 'Google Drive';

      if (gdriveMode === 'quick') {
        const codeToUse = extractGoogleCode(gdriveInput);
        if (!codeToUse) {
          toast.error('Please authorize and paste the redirected URL or code.');
          setIsSubmitting(false);
          return;
        }

        const res = await backendApi.backupGDriveExchangeCode({
          code: codeToUse,
          name: cleanName,
          id: editDestId || undefined,
        });
        setIsSubmitting(false);

        if (res.ok) {
          toast.success(res.data?.message || `Connected Google Drive "${cleanName}"`);
          setDestModalOpen(false);
          await loadDestinations(false);
        } else {
          toast.error(res.error || 'Google Drive authorization failed. Please check the code.');
        }
        return;
      }

      // Custom Google Cloud OAuth flow
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
        id: editDestId || undefined,
        client_id: gdriveClientId.trim(),
        client_secret: gdriveClientSecret.trim(),
        auth_mode: 'custom_oauth',
      });
      setIsSubmitting(false);

      if (res.ok && res.data?.auth_url) {
        window.location.href = res.data.auth_url;
        return;
      }

      if (res.ok) {
        toast.success(`Connected Google Drive "${cleanName}"`);
        setDestModalOpen(false);
        await loadDestinations(false);
      } else {
        toast.error(res.error || 'Failed to connect Google Drive destination');
      }
      return;
    }

    const cleanR2Name = destName.trim() || `Cloudflare R2 (${r2Bucket.trim()})`;
    const res = await backendApi.backupDestinationCreate({
      provider: 'r2',
      name: cleanR2Name,
      id: editDestId || undefined,
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

    toast.success(`Connected Cloudflare R2 bucket "${r2Bucket.trim()}"`);
    setDestModalOpen(false);
    await loadDestinations(false);
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
            className="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 transition-colors cursor-pointer"
          >
            <RotateCw className={`h-3.5 w-3.5 text-zinc-400 ${isRefreshing ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            type="button"
            onClick={() => openConnectModal('gdrive')}
            className="inline-flex h-8 items-center gap-1.5 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 transition-colors cursor-pointer whitespace-nowrap"
          >
            <HardDrive className="h-3.5 w-3.5 text-emerald-400" />
            Connect Google Drive
          </button>
          <button
            type="button"
            onClick={() => openConnectModal('r2')}
            className="inline-flex h-8 items-center gap-1.5 rounded-md bg-zinc-100 px-3.5 text-xs font-medium text-zinc-950 hover:bg-white transition-colors cursor-pointer whitespace-nowrap"
          >
            <Plus className="h-3.5 w-3.5" />
            Connect Cloudflare R2
          </button>
        </div>
      </div>

      {/* Provider Filter Tabs */}
      <div className="flex items-center gap-1 rounded-md border border-white/[0.07] bg-[#09090b] p-0.5 w-fit">
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
            className={`rounded px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer whitespace-nowrap ${
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
        <div className="rounded-lg border border-white/[0.07] bg-[#111115]/90 p-8 text-center space-y-3">
          <div className="mx-auto flex h-9 w-9 items-center justify-center rounded-md border border-white/[0.08] bg-zinc-900">
            <Cloud className="h-4 w-4 text-zinc-400" />
          </div>
          <h3 className="text-sm font-medium text-zinc-200">No backup destinations configured</h3>
          <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
            <button
              type="button"
              onClick={() => openConnectModal('gdrive')}
              className="inline-flex h-8 items-center gap-1.5 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 cursor-pointer"
            >
              <HardDrive className="h-3.5 w-3.5" />
              Connect Google Drive
            </button>
            <button
              type="button"
              onClick={() => openConnectModal('r2')}
              className="inline-flex h-8 items-center gap-1.5 rounded-md bg-zinc-100 px-3 text-xs font-medium text-zinc-950 hover:bg-white cursor-pointer"
            >
              <Cloud className="h-3.5 w-3.5" />
              Connect Cloudflare R2
            </button>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
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
                className="rounded-lg border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 flex flex-col justify-between gap-4"
              >
                <div className="space-y-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-center gap-3 min-w-0">
                      <div
                        className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-md border ${
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
                        {
                          label: 'Test Connection',
                          icon: <Activity className="h-3.5 w-3.5 text-orange-400" />,
                          onClick: () => void handleTestDestination(d.id),
                        },
                        ...(isGDrive
                          ? [
                              {
                                label: 'Reconnect Google Drive',
                                icon: <KeyRound className="h-3.5 w-3.5 text-emerald-400" />,
                                onClick: () => openConnectModal('gdrive', d.name, d.id),
                              },
                            ]
                          : [
                              {
                                label: 'Edit Cloudflare R2',
                                icon: <KeyRound className="h-3.5 w-3.5 text-orange-400" />,
                                onClick: () => openConnectModal('r2', d.name, d.id),
                              },
                            ]),
                        {
                          label: 'Copy Destination Name',
                          icon: <Copy className="h-3.5 w-3.5" />,
                          onClick: async () => {
                            const ok = await copyToClipboard(d.name);
                            if (ok) toast.success(`Copied "${d.name}"`);
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
                            {d.email || (d.displayName ? d.displayName : 'Authorized')}
                          </span>
                        </div>
                        <div>
                          <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500 block">
                            Drive Folder
                          </span>
                          <span className="font-mono text-[11px] text-emerald-400 truncate block mt-0.5" title={d.folderId || 'nextdeploy'}>
                            {d.folderId ? `nextdeploy (${d.folderId.slice(0, 8)}...)` : 'nextdeploy (root)'}
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
                          onClick={() => openConnectModal('gdrive', d.name, d.id)}
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
                      <div className="h-1.5 w-full rounded bg-zinc-900 overflow-hidden">
                        <div
                          className={`h-full rounded ${
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
                    <div className="flex flex-wrap items-center gap-1.5">
                      <button
                        type="button"
                        disabled={testingDestId === d.id}
                        onClick={() => void handleTestDestination(d.id)}
                        className="inline-flex items-center gap-1.5 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer disabled:opacity-50"
                      >
                        <Activity className={`h-3 w-3 ${testingDestId === d.id ? 'animate-spin text-orange-400' : 'text-zinc-400'}`} />
                        <span>{testingDestId === d.id ? 'Testing...' : 'Test Connection'}</span>
                      </button>
                      {isGDrive && (
                        <button
                          type="button"
                          onClick={() => openConnectModal('gdrive', d.name, d.id)}
                          className="inline-flex items-center gap-1.5 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer"
                        >
                          <RotateCw className="h-3 w-3 text-emerald-400" />
                          <span>Reconnect</span>
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => void handleDeleteDestination(d)}
                        className="inline-flex items-center gap-1 rounded-md border border-rose-500/25 bg-rose-500/10 px-2 py-1 text-[11px] font-medium text-rose-300 hover:bg-rose-500/20 transition-colors cursor-pointer"
                        title="Delete destination"
                      >
                        <Trash2 className="h-3 w-3" />
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
        <div className="rounded-lg border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
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
              <div className="flex h-9 w-9 items-center justify-center rounded-md border border-white/[0.08] bg-zinc-900/60 text-zinc-500">
                <Clock className="h-4 w-4" />
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
                              <OwnerBadge id={app.ownerId} name={app.owner} role={app.ownerRole} />
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

      {/* Connect / Reconnect Destination Modal (Google Drive OAuth / Cloudflare R2) */}
      <ShadcnDialog
        open={destModalOpen}
        onClose={() => setDestModalOpen(false)}
        title={
          destProvider === 'gdrive'
            ? editDestId
              ? 'Reconnect Google Drive'
              : 'Connect Google Drive'
            : editDestId
              ? 'Edit Cloudflare R2'
              : 'Connect Cloudflare R2'
        }
        icon={
          destProvider === 'gdrive' ? (
            <HardDrive className="h-4 w-4 text-emerald-400" />
          ) : (
            <Cloud className="h-4 w-4 text-orange-400" />
          )
        }
        maxWidth="lg"
      >
        <form onSubmit={handleConnectDestination} className="space-y-3.5">
          {/* Provider Selection Tabs */}
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <button
              type="button"
              onClick={() => setDestProvider('gdrive')}
              className={`flex items-center gap-2.5 rounded border p-2.5 text-left transition-colors cursor-pointer ${
                destProvider === 'gdrive'
                  ? 'border-white/20 bg-zinc-800/80 text-zinc-100'
                  : 'border-white/[0.06] bg-[#09090b]/60 text-zinc-400 hover:border-white/10 hover:text-zinc-200'
              }`}
            >
              <HardDrive className={`h-4 w-4 shrink-0 ${destProvider === 'gdrive' ? 'text-emerald-400' : 'text-zinc-500'}`} />
              <div className="min-w-0">
                <div className="text-xs font-medium">Google Drive</div>
                <div className="text-[10px] text-zinc-500">Google Cloud / Rclone OAuth</div>
              </div>
            </button>
            <button
              type="button"
              onClick={() => setDestProvider('r2')}
              className={`flex items-center gap-2.5 rounded border p-2.5 text-left transition-colors cursor-pointer ${
                destProvider === 'r2'
                  ? 'border-white/20 bg-zinc-800/80 text-zinc-100'
                  : 'border-white/[0.06] bg-[#09090b]/60 text-zinc-400 hover:border-white/10 hover:text-zinc-200'
              }`}
            >
              <Cloud className={`h-4 w-4 shrink-0 ${destProvider === 'r2' ? 'text-orange-400' : 'text-zinc-500'}`} />
              <div className="min-w-0">
                <div className="text-xs font-medium">Cloudflare R2</div>
                <div className="text-[10px] text-zinc-500">S3-compatible object storage</div>
              </div>
            </button>
          </div>

          {/* Destination Name */}
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1">
              Destination Label (Optional)
            </label>
            <input
              type="text"
              value={destName}
              onChange={(e) => setDestName(e.target.value)}
              placeholder={
                destProvider === 'gdrive'
                  ? 'Google Drive (default)'
                  : 'Cloudflare R2 (default)'
              }
              className="w-full rounded border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:border-zinc-500 focus:outline-none"
            />
          </div>

          {destProvider === 'gdrive' ? (
            <div className="space-y-3">
              {/* Google Drive Sub-Tabs: Quick Setup vs Custom App */}
              <div className="flex items-center gap-1 rounded border border-white/[0.07] bg-[#09090b] p-0.5 w-fit">
                <button
                  type="button"
                  onClick={() => setGdriveMode('quick')}
                  className={`inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer ${
                    gdriveMode === 'quick'
                      ? 'bg-zinc-800 text-zinc-100'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  Quick Setup
                </button>
                <button
                  type="button"
                  onClick={() => setGdriveMode('custom')}
                  className={`inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer ${
                    gdriveMode === 'custom'
                      ? 'bg-zinc-800 text-zinc-100'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  <KeyRound className="h-3 w-3 text-zinc-400" />
                  Custom Credentials
                </button>
              </div>

              {gdriveMode === 'quick' ? (
                <div className="space-y-3 rounded border border-white/[0.06] bg-zinc-900/30 p-3.5 text-xs">
                  <div className="flex items-start gap-2.5">
                    <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded bg-zinc-800 font-mono text-[11px] font-semibold text-zinc-300 border border-white/[0.06]">
                      1
                    </span>
                    <div className="flex-1 space-y-1.5">
                      <div className="font-medium text-zinc-200">Google Authorization</div>
                      <p className="text-[11px] leading-relaxed text-zinc-400">
                        Sign in to grant NextDeploy permission to manage backup archives in your Google Drive.
                      </p>
                      <div className="pt-0.5">
                        <a
                          href={G_DRIVE_OAUTH_URL}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="inline-flex items-center gap-2.5 rounded-md bg-white px-3.5 py-1.5 text-xs font-semibold text-zinc-900 shadow-sm hover:bg-zinc-100 hover:shadow active:scale-[0.99] transition-all cursor-pointer border border-zinc-200"
                        >
                          <svg className="h-4 w-4 shrink-0" viewBox="0 0 24 24">
                            <path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" />
                            <path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" />
                            <path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l2.85-2.22.81-.63z" />
                            <path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.52 6.16-4.52z" />
                          </svg>
                          <span>Sign in with Google</span>
                          <ExternalLink className="h-3 w-3 text-zinc-500 ml-0.5" />
                        </a>
                      </div>
                    </div>
                  </div>

                  <div className="border-t border-white/[0.06]" />

                  <div className="flex items-start gap-2.5">
                    <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded bg-zinc-800 font-mono text-[11px] font-semibold text-zinc-300 border border-white/[0.06]">
                      2
                    </span>
                    <div className="flex-1 space-y-1.5">
                      <div className="font-medium text-zinc-200">Paste Redirect URL or Code</div>
                      <p className="text-[11px] leading-relaxed text-zinc-400">
                        After granting permission, copy the full address from your browser bar (starting with{' '}
                        <code className="rounded bg-black/40 px-1 py-0.5 font-mono text-zinc-300">
                          http://127.0.0.1:53682
                        </code>
                        ) and paste it below. Even if the page shows a connection error, the authorization token is inside the URL.
                      </p>
                      <input
                        type="text"
                        value={gdriveInput}
                        onChange={(e) => setGdriveInput(e.target.value)}
                        placeholder="Paste redirected address: http://127.0.0.1:53682/?state=...&code=..."
                        className="w-full rounded border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:border-zinc-500 focus:outline-none"
                      />
                      {extractedCode ? (
                        <div className="flex items-center gap-1.5 text-[11px] text-emerald-400 font-mono">
                          <CheckCircle2 className="h-3 w-3 shrink-0" />
                          <span className="truncate">Authorization code ready: {extractedCode.slice(0, 16)}...</span>
                        </div>
                      ) : gdriveInput.trim() ? (
                        <div className="flex items-center gap-1.5 text-[11px] text-amber-400 font-mono">
                          <AlertCircle className="h-3 w-3 shrink-0" />
                          <span>Could not find code. Please paste the full address from your browser.</span>
                        </div>
                      ) : null}
                    </div>
                  </div>
                </div>
              ) : (
                <div className="space-y-3 rounded-md border border-white/[0.06] bg-zinc-900/30 p-3.5 text-xs">
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
                        className="w-full rounded-md border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-zinc-500 focus:outline-none"
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
                        className="w-full rounded-md border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-zinc-500 focus:outline-none"
                      />
                    </div>
                  </div>

                  <div className="rounded-md border border-white/[0.06] bg-[#09090b] p-2.5">
                    <div className="flex items-center justify-between text-[10px] text-zinc-400 mb-1">
                      <span>Authorized Redirect URI</span>
                      <button
                        type="button"
                        onClick={async () => {
                          const ok = await copyToClipboard(gdriveRedirectUrl);
                          if (ok) toast.success('Copied Google OAuth redirect URI');
                        }}
                        className="inline-flex items-center gap-1 text-emerald-400 hover:underline cursor-pointer"
                      >
                        <Copy className="h-3 w-3" />
                        Copy URI
                      </button>
                    </div>
                    <div className="font-mono text-[11px] text-zinc-300 select-all break-all">
                      {gdriveRedirectUrl}
                    </div>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-3 rounded-md border border-white/[0.06] bg-zinc-900/30 p-3.5 text-xs">
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
                    className="w-full rounded-md border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-zinc-500 focus:outline-none"
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
                    className="w-full rounded-md border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-zinc-500 focus:outline-none"
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
                    className="w-full rounded-md border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-zinc-500 focus:outline-none"
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
                    className="w-full rounded-md border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-zinc-500 focus:outline-none"
                  />
                </div>
              </div>
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setDestModalOpen(false)}
              className="rounded border border-white/10 bg-transparent px-3 py-1.5 text-xs text-zinc-300 hover:bg-white/[0.05] transition-colors cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={
                isSubmitting ||
                (destProvider === 'gdrive' && gdriveMode === 'quick' && !extractedCode) ||
                (destProvider === 'gdrive' && gdriveMode === 'custom' && (!gdriveClientId.trim() || !gdriveClientSecret.trim())) ||
                (destProvider === 'r2' && (!r2AccountId.trim() || !r2Bucket.trim() || !r2AccessKeyId.trim() || !r2SecretAccessKey.trim()))
              }
              className="inline-flex items-center gap-1.5 rounded bg-zinc-100 px-3.5 py-1.5 text-xs font-medium text-zinc-950 hover:bg-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
            >
              {isSubmitting ? (
                <>
                  <RotateCw className="h-3.5 w-3.5 animate-spin" />
                  <span>Connecting...</span>
                </>
              ) : destProvider === 'gdrive' ? (
                gdriveMode === 'quick' ? (
                  <>
                    <CheckCircle2 className="h-3.5 w-3.5 text-emerald-600" />
                    <span>{extractedCode ? 'Connect Google Drive' : 'Paste Code to Connect'}</span>
                  </>
                ) : (
                  <>
                    <ExternalLink className="h-3.5 w-3.5" />
                    <span>Authorize with Google</span>
                  </>
                )
              ) : (
                <>
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  <span>Save Destination</span>
                </>
              )}
            </button>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};
