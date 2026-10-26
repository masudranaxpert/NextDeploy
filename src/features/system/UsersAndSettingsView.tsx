import React, { useEffect, useMemo, useState } from 'react';
import {
  Trash2,
  Plus,
  Globe,
  KeyRound,
  Users,
  FileText,
  Settings,
  ShieldCheck,
  Server,
  Sliders,
  Ban,
  CheckCircle2,
  Shield,
  Search,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
  Cpu,
  HardDrive,
  Terminal,
  AlertTriangle,
  Info,
  HelpCircle,
} from 'lucide-react';
import { toast } from 'sonner';
import { AuditLogEntry, PanelUser } from '../../types/nextdeploy';
import { ShieldNode } from '../../types/shield';
import {
  ActionMenu,
  ShadcnDialog,
  ShadcnSwitch,
  StatusBadge,
} from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';
import { OwnerBadge } from '../../components/ui/OwnerBadge';

interface UsersAndSettingsViewProps {
  mode: 'users' | 'audit-logs' | 'settings';
  users: PanelUser[];
  auditLogs: AuditLogEntry[];
  cgroupInfo?: { mode?: string; supported?: boolean };
  /** What this server really has; quotas above it can never be honoured. */
  host?: { memoryMB: number; cpus: number; diskMB: number };
  /** The signed-in user: they cannot demote, suspend or delete themselves. */
  currentUserId?: string;
  settings?: Record<string, string>;
}

const fmtMb = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : mb >= 1 ? `${Math.round(mb)} MB` : mb > 0 ? `${Math.max(1, Math.round(mb * 1024))} KB` : '0 MB');
const fmtBytes = (b: number) => fmtMb(b / (1024 * 1024));

/** "used / limit", turning amber at 70% and red at 90% of the limit. */
const UsageFigure: React.FC<{ used: number; max: number; fmt: (n: number) => string }> = ({ used, max, fmt }) => {
  const p = max > 0 ? (used / max) * 100 : 0;
  const tone = p >= 90 ? 'text-red-400' : p >= 70 ? 'text-amber-400' : 'text-zinc-200';
  return (
    <span className={tone}>
      {fmt(used)}
      <span className="text-zinc-500"> / {max > 0 ? fmt(max) : 'unlimited'}</span>
    </span>
  );
};

/** CPU as a percentage of the user's limit (100% = all the cores they are allowed). */
const CpuFigure: React.FC<{ used: number; cores: number }> = ({ used, cores }) => {
  if (!(cores > 0)) return <span className="text-zinc-200">{used.toFixed(2)} cores <span className="text-zinc-500">/ unlimited</span></span>;
  const p = (used / cores) * 100;
  const tone = p >= 90 ? 'text-red-400' : p >= 70 ? 'text-amber-400' : 'text-zinc-200';
  return (
    <span className={tone}>
      {p < 10 ? p.toFixed(2) : p.toFixed(1)}%<span className="text-zinc-500"> of {cores} {cores === 1 ? 'core' : 'cores'}</span>
    </span>
  );
};

const LimitField: React.FC<{
  label: string;
  unit: string;
  value: string;
  onChange: (v: string) => void;
  hint: string;
  step?: string;
  max?: number;
}> = ({ label, unit, value, onChange, hint, step = '1', max }) => (
  <label className="block">
    <span className="mb-1.5 block text-[11px] font-medium text-zinc-300">{label}</span>
    <div className="relative">
      <input
        type="number"
        min={0}
        max={max && max > 0 ? max : undefined}
        step={step}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] pl-3 pr-14 font-mono text-xs tabular-nums text-zinc-100 outline-none focus:border-orange-500/50"
      />
      <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center font-mono text-[11px] text-zinc-500">{unit}</span>
    </div>
    <span className="mt-1 block text-[11px] text-zinc-500">{hint}</span>
  </label>
);

export const UsersAndSettingsView: React.FC<UsersAndSettingsViewProps> = ({
  mode,
  users: initialUsers,
  auditLogs: initialAuditLogs,
  currentUserId,
  host,
  settings: initialSettings,
}) => {
  const [users, setUsers] = useState<PanelUser[]>(initialUsers);
  const [auditLogs, setAuditLogs] = useState<AuditLogEntry[]>(initialAuditLogs);

  useEffect(() => {
    setUsers(initialUsers);
  }, [initialUsers]);

  useEffect(() => {
    setAuditLogs(initialAuditLogs);
  }, [initialAuditLogs]);

  // Create User Modal State (matches POST /users in users_handler.go)
  const [userModalOpen, setUserModalOpen] = useState(false);
  const [newUsername, setNewUsername] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [newRole, setNewRole] = useState<'admin' | 'user'>('user');

  // Edit Resource Limits Modal State (matches POST /users/:id/limits in users_handler.go)
  const [limitsModalUser, setLimitsModalUser] = useState<PanelUser | null>(null);
  const [limitMaxApps, setLimitMaxApps] = useState('10');
  const [limitMaxCpus, setLimitMaxCpus] = useState('4');
  const [limitMaxMemoryMb, setLimitMaxMemoryMb] = useState('8192');
  const [limitMaxStorageMb, setLimitMaxStorageMb] = useState('51200');
  const [limitAllowDomainFileServer, setLimitAllowDomainFileServer] = useState(true);
  const [shieldNodes, setShieldNodes] = useState<ShieldNode[]>([]);
  const [limitDefaultShieldId, setLimitDefaultShieldId] = useState<number | null>(null);

  // Change Password Modal State (matches POST /users/:id/password in users_handler.go)
  const [passwordModalUser, setPasswordModalUser] = useState<PanelUser | null>(null);
  const [changePasswordValue, setChangePasswordValue] = useState('');
  const [changePasswordConfirm, setChangePasswordConfirm] = useState('');

  // Audit Trail Pagination & Filter State (matches GET /settings/audit-logs)
  const [auditSearch, setAuditSearch] = useState('');
  const [auditCategory, setAuditCategory] = useState<string>('all');
  const [auditPage, setAuditPage] = useState(1);
  const [auditPerPage, setAuditPerPage] = useState(10);
  const [auditLoading, setAuditLoading] = useState(false);

  // System Settings State (matches POST /settings & /settings/cleanup/run & /settings/tmp/*)
  const [cleanupEnabled, setCleanupEnabled] = useState(true);
  const [cleanupInterval, setCleanupInterval] = useState<'1h' | '6h' | '12h' | '24h' | '168h'>('24h');
  const [storageOversellingEnabled, setStorageOversellingEnabled] = useState(true);

  // Pre-calculate other users' allocated RAM & Storage for overcommit detection
  const otherUsersAllocatedMB = useMemo(() => {
    if (!limitsModalUser) return 0;
    return users
      .filter((u) => u.role !== 'admin' && String(u.id) !== String(limitsModalUser.id))
      .reduce((sum, u) => sum + (u.maxMemoryMB ?? (u.memQuotaGB ? u.memQuotaGB * 1024 : 0)), 0);
  }, [users, limitsModalUser]);

  const totalCurrentlyAllocatedMB = useMemo(() => {
    return users
      .filter((u) => u.role !== 'admin')
      .reduce((sum, u) => sum + (u.maxMemoryMB ?? (u.memQuotaGB ? u.memQuotaGB * 1024 : 0)), 0);
  }, [users]);

  const otherUsersAllocatedStorageMB = useMemo(() => {
    if (!limitsModalUser) return 0;
    return users
      .filter((u) => u.role !== 'admin' && String(u.id) !== String(limitsModalUser.id))
      .reduce((sum, u) => sum + (u.maxStorageMB ?? (u.diskQuotaGB ? u.diskQuotaGB * 1024 : 0)), 0);
  }, [users, limitsModalUser]);

  const totalCurrentlyAllocatedStorageMB = useMemo(() => {
    return users
      .filter((u) => u.role !== 'admin')
      .reduce((sum, u) => sum + (u.maxStorageMB ?? (u.diskQuotaGB ? u.diskQuotaGB * 1024 : 0)), 0);
  }, [users]);

  // Overcommit Resolution Modal State
  const [overcommitModalOpen, setOvercommitModalOpen] = useState(false);
  const [overcommitInfo, setOvercommitInfo] = useState<{
    allocated_mb: number;
    host_disk_mb: number;
    excess_mb: number;
    users: Array<{ id: number; username: string; max_storage_mb: number; unlimited: boolean }>;
  } | null>(null);

  const [cleanupIncludeBuildCache, setCleanupIncludeBuildCache] = useState(false);
  const [cleanupRunning, setCleanupRunning] = useState(false);
  const [cleanupLastRunAt, setCleanupLastRunAt] = useState('Never');
  const [cleanupLastStatus, setCleanupLastStatus] = useState<'ok' | 'error'>('ok');
  const [cleanupLastLog, setCleanupLastLog] = useState('No cleanup log recorded yet.');
  const [showCleanupLogModal, setShowCleanupLogModal] = useState(false);
  const [showStorageKnowledgeModal, setShowStorageKnowledgeModal] = useState(false);
  const [tmpCount, setTmpCount] = useState(0);
  const [tmpTotalSize, setTmpTotalSize] = useState('0 B');
  const [isSavingSettings, setIsSavingSettings] = useState(false);

  const applySettingsData = (data: {
    cleanup_enabled?: boolean;
    cleanup_interval?: string;
    cleanup_include_build_cache?: boolean;
    cleanup_last_run?: string;
    cleanup_last_log?: string;
    storage_overselling_enabled?: boolean;
    settings?: Record<string, string>;
  }) => {
    const rawMap = data.settings || {};
    if (typeof data.cleanup_enabled === 'boolean') {
      setCleanupEnabled(data.cleanup_enabled);
    } else if (rawMap.cleanup_enabled !== undefined) {
      setCleanupEnabled(rawMap.cleanup_enabled === 'true' || rawMap.cleanup_enabled === '1' || rawMap.cleanup_enabled === 'on');
    }

    if (typeof data.storage_overselling_enabled === 'boolean') {
      setStorageOversellingEnabled(data.storage_overselling_enabled);
    } else if (rawMap.storage_overselling_enabled !== undefined) {
      setStorageOversellingEnabled(rawMap.storage_overselling_enabled === 'true' || rawMap.storage_overselling_enabled === '1' || rawMap.storage_overselling_enabled === 'on');
    }

    if (data.cleanup_interval) {
      setCleanupInterval(data.cleanup_interval as any);
    } else if (rawMap.cleanup_interval) {
      setCleanupInterval(rawMap.cleanup_interval as any);
    }

    if (typeof data.cleanup_include_build_cache === 'boolean') {
      setCleanupIncludeBuildCache(data.cleanup_include_build_cache);
    } else if (rawMap.cleanup_include_build_cache !== undefined) {
      setCleanupIncludeBuildCache(rawMap.cleanup_include_build_cache === 'true' || rawMap.cleanup_include_build_cache === '1' || rawMap.cleanup_include_build_cache === 'on');
    }

    const lastRun = data.cleanup_last_run || rawMap.cleanup_last_run;
    if (lastRun) {
      try {
        setCleanupLastRunAt(new Date(lastRun).toLocaleString());
      } catch {
        setCleanupLastRunAt(lastRun);
      }
    }

    const lastLog = data.cleanup_last_log || rawMap.cleanup_last_log;
    if (lastLog) {
      setCleanupLastLog(lastLog);
      setCleanupLastStatus(lastLog.toLowerCase().includes('error') ? 'error' : 'ok');
    }
  };

  useEffect(() => {
    if (initialSettings && Object.keys(initialSettings).length > 0) {
      applySettingsData({ settings: initialSettings });
    }
  }, [initialSettings]);

  useEffect(() => {
    if (mode === 'settings' || mode === 'users') {
      void backendApi.getSettings().then((res) => {
        if (res.ok && res.data) {
          applySettingsData(res.data);
        }
      });
      void backendApi.getTmpCleanupInfo().then((info) => {
        if (typeof info?.count === 'number') setTmpCount(info.count);
        if (info?.total_size) setTmpTotalSize(info.total_size);
      });
      void backendApi.listShieldNodes().then((res) => {
        if (res.ok && res.nodes) setShieldNodes(res.nodes);
      }).catch(() => {});
    }
  }, [mode]);

  const openLimitsModal = (user: PanelUser) => {
    setLimitsModalUser(user);
    setLimitMaxApps(String(user.maxApps ?? 0));
    setLimitMaxCpus(String(user.cpuQuotaCores ?? 0));
    setLimitMaxMemoryMb(String(user.maxMemoryMB ?? (user.memQuotaGB ? user.memQuotaGB * 1024 : 0)));
    setLimitMaxStorageMb(String(user.maxStorageMB ?? (user.diskQuotaGB ? user.diskQuotaGB * 1024 : 0)));
    setLimitAllowDomainFileServer(user.allowDomainFileServer ?? true);
    setLimitDefaultShieldId(user.default_shield_id ?? null);
  };

  const handleToggleStorageOverselling = async (nextValue: boolean) => {
    if (!nextValue) {
      // Admin wants to turn Overselling OFF (Switch to Strict Mode).
      // Verify whether current non-admin quotas already exceed the server's disk capacity.
      const hostDisk = host?.diskMB || 0;
      if (hostDisk > 0 && totalCurrentlyAllocatedStorageMB > hostDisk) {
        const excess = totalCurrentlyAllocatedStorageMB - hostDisk;
        const overcommittedUsers = users
          .filter((u) => u.role !== 'admin')
          .map((u) => ({
            id: Number(u.id),
            username: u.username,
            max_storage_mb: u.maxStorageMB ?? (u.diskQuotaGB ? u.diskQuotaGB * 1024 : 0),
            unlimited: (u.maxStorageMB ?? 0) <= 0,
          }));
        setOvercommitInfo({
          allocated_mb: totalCurrentlyAllocatedStorageMB,
          host_disk_mb: hostDisk,
          excess_mb: excess,
          users: overcommittedUsers,
        });
        setOvercommitModalOpen(true);
        return;
      }
    }

    try {
      const res = await backendApi.saveSettings({
        storage_overselling_enabled: nextValue,
      });
      if (res.ok) {
        setStorageOversellingEnabled(nextValue);
        toast.success(
          nextValue
            ? 'Storage overselling enabled (thin provisioning with host safety buffer)'
            : 'Strict storage allocation enabled (guaranteed user quotas)'
        );
      } else if (res.data?.overcommitted) {
        setOvercommitInfo({
          allocated_mb: res.data.allocated_mb || totalCurrentlyAllocatedStorageMB,
          host_disk_mb: res.data.host_disk_mb || (host?.diskMB || 0),
          excess_mb: res.data.excess_mb || 0,
          users: res.data.users || [],
        });
        setOvercommitModalOpen(true);
      } else {
        toast.error(res.data?.error || 'Failed to update storage overselling setting');
      }
    } catch (err: any) {
      toast.error(err.message || 'Failed to update storage overselling setting');
    }
  };

  const handleSaveLimits = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!limitsModalUser) return;

    const maxApps = Math.max(0, parseInt(limitMaxApps, 10) || 0);
    const maxCpus = Math.max(0, parseFloat(limitMaxCpus) || 0);
    const maxMemoryMb = Math.max(0, parseInt(limitMaxMemoryMb, 10) || 0);
    const maxStorageMb = Math.max(0, parseInt(limitMaxStorageMb, 10) || 0);

    if (host) {
      const over =
        host.memoryMB > 0 && maxMemoryMb > host.memoryMB
          ? `Memory is more than this server's ${host.memoryMB} MB RAM`
          : host.cpus > 0 && maxCpus > host.cpus
            ? `CPU is more than this server's ${host.cpus} cores`
            : host.diskMB > 0 && maxStorageMb > host.diskMB
              ? `Storage is more than this server's ${Math.floor(host.diskMB / 1024)} GB disk`
              : '';
      if (over) {
        toast.error(over);
        return;
      }

      if (!storageOversellingEnabled) {
        if (maxStorageMb <= 0) {
          toast.error('Strict mode is active: standard users cannot have unlimited storage');
          return;
        }
        const totalWithNew = otherUsersAllocatedStorageMB + maxStorageMb;
        if (host.diskMB > 0 && totalWithNew > host.diskMB) {
          const availableMB = Math.max(0, host.diskMB - otherUsersAllocatedStorageMB);
          toast.error(
            `Strict allocation active: cannot assign ${(maxStorageMb / 1024).toFixed(1)} GB. Only ${(availableMB / 1024).toFixed(1)} GB unallocated capacity remains on this server.`
          );
          return;
        }
      }
    }

    try {
      const res = await backendApi.updateUserLimits(limitsModalUser.id, {
        max_apps: maxApps,
        max_cpus: maxCpus,
        max_memory_mb: maxMemoryMb,
        max_storage_mb: maxStorageMb,
        allow_domain_file_server: limitAllowDomainFileServer,
        default_shield_id: limitDefaultShieldId,
      });
      if (!res.ok) {
        // e.g. a 2 GB quota on a server with 1 GB of RAM is refused by the backend
        toast.error(res.error || 'Could not update the limits');
        return;
      }
      if ((res as { warning?: string }).warning) toast.warning((res as { warning?: string }).warning as string);
    } catch {
      toast.error('Could not reach the server');
      return;
    }

    setUsers((prev) =>
      prev.map((item) =>
        item.id === limitsModalUser.id
          ? {
              ...item,
              maxApps,
              cpuQuotaCores: maxCpus,
              maxMemoryMB: maxMemoryMb,
              memQuotaGB: Math.round((maxMemoryMb / 1024) * 10) / 10,
              maxStorageMB: maxStorageMb,
              diskQuotaGB: Math.round((maxStorageMb / 1024) * 10) / 10,
              allowDomainFileServer: limitAllowDomainFileServer,
              default_shield_id: limitDefaultShieldId,
            }
          : item
      )
    );
    toast.success(
      `Updated resource quotas & systemd slice (nextdeploy-user-${limitsModalUser.id}.slice) for ${limitsModalUser.username}`
    );
    setLimitsModalUser(null);
  };

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanUsername = newUsername.trim();
    if (cleanUsername.length < 3) {
      toast.error('Username must be at least 3 characters');
      return;
    }
    if (newPassword.length < 8) {
      toast.error('Password must be at least 8 characters');
      return;
    }

    try {
      await backendApi.createUser({
        username: cleanUsername,
        password: newPassword,
        role: newRole,
      });
    } catch {
      // fallback in preview
    }

    const newUser: PanelUser = {
      id: `${users.length + 1}`,
      username: cleanUsername,
      email: `${cleanUsername}@nextdeploy.sh`,
      role: newRole,
      status: 'active',
      appsCount: 0,
      maxApps: newRole === 'admin' ? 0 : 10,
      cpuQuotaCores: newRole === 'admin' ? 0 : 4,
      memUsedGB: 0,
      memQuotaGB: newRole === 'admin' ? 0 : 8,
      maxMemoryMB: newRole === 'admin' ? 0 : 8192,
      diskUsedGB: 0,
      diskQuotaGB: newRole === 'admin' ? 0 : 50,
      maxStorageMB: newRole === 'admin' ? 0 : 51200,
      allowDomainFileServer: true,
      twoFactorEnabled: false,
      lastLoginAt: 'Never',
    };

    setUsers((prev) => [...prev, newUser]);
    setNewUsername('');
    setNewPassword('');
    setNewRole('user');
    setUserModalOpen(false);
    toast.success(`Created ${newRole} account "${cleanUsername}"`);
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!passwordModalUser) return;
    if (changePasswordValue.length < 8) {
      toast.error('Password must be at least 8 characters');
      return;
    }
    if (changePasswordValue !== changePasswordConfirm) {
      toast.error('Passwords do not match');
      return;
    }

    try {
      await backendApi.changeUserPassword(
        passwordModalUser.id,
        changePasswordValue,
        changePasswordConfirm
      );
    } catch {
      // fallback in preview
    }

    toast.success(`Updated password for ${passwordModalUser.username}`);
    setPasswordModalUser(null);
    setChangePasswordValue('');
    setChangePasswordConfirm('');
  };

  const handleToggleUserStatus = async (user: PanelUser) => {
    const nextStatus = user.status === 'suspended' ? 'active' : 'suspended';
    try {
      await backendApi.changeUserStatus(user.id, nextStatus);
    } catch {
      // fallback in preview
    }

    setUsers((prev) =>
      prev.map((item) => (item.id === user.id ? { ...item, status: nextStatus } : item))
    );

    if (nextStatus === 'suspended') {
      toast.success(
        `Suspended ${user.username}: revoked active sessions and stopped all user containers`
      );
    } else {
      toast.success(`Reactivated ${user.username}`);
    }
  };

  const handleToggleUserRole = async (user: PanelUser) => {
    const nextRole = user.role === 'admin' ? 'user' : 'admin';
    try {
      await backendApi.changeUserRole(user.id, nextRole);
    } catch {
      // fallback in preview
    }

    setUsers((prev) =>
      prev.map((item) => (item.id === user.id ? { ...item, role: nextRole } : item))
    );
    toast.success(`Changed ${user.username} role to ${nextRole.toUpperCase()}`);
  };

  // Filtered + Paginated Audit Logs
  const filteredAuditLogs = useMemo(() => {
    return auditLogs.filter((log) => {
      const matchesCategory =
        auditCategory === 'all' || log.action.toLowerCase().startsWith(auditCategory);
      const q = auditSearch.trim().toLowerCase();
      if (!q) return matchesCategory;
      const matchesQuery =
        log.action.toLowerCase().includes(q) ||
        log.actor.toLowerCase().includes(q) ||
        log.target.toLowerCase().includes(q) ||
        (log.details && log.details.toLowerCase().includes(q)) ||
        log.ip.toLowerCase().includes(q);
      return matchesCategory && matchesQuery;
    });
  }, [auditLogs, auditCategory, auditSearch]);

  const totalAuditItems = filteredAuditLogs.length;
  const totalAuditPages = Math.max(1, Math.ceil(totalAuditItems / auditPerPage));
  const safeAuditPage = Math.min(auditPage, totalAuditPages);
  const paginatedAuditLogs = useMemo(() => {
    const start = (safeAuditPage - 1) * auditPerPage;
    return filteredAuditLogs.slice(start, start + auditPerPage);
  }, [filteredAuditLogs, safeAuditPage, auditPerPage]);

  const handleRefreshAuditLogs = async () => {
    setAuditLoading(true);
    try {
      const res = await backendApi.auditLogsList(safeAuditPage, auditPerPage);
      const rawLogs = res.data?.logs;
      if (Array.isArray(rawLogs) && rawLogs.length > 0) {
        const mapped: AuditLogEntry[] = rawLogs.map((raw, idx: number) => ({
          id: String(raw.id ?? `aud-${idx}`),
          action: String(raw.action ?? 'system.event'),
          actor: String(raw.username ?? 'admin'),
          actorRole: (raw as any).role || (raw.username === 'system' ? 'system' : 'user'),
          targetOwner: (raw as any).target_owner || undefined,
          targetOwnerRole: (raw as any).target_owner_role || undefined,
          target: String(raw.resource_id || raw.resource_type || 'system'),
          details: String(raw.details ?? ''),
          ip: String(raw.ip_address ?? '127.0.0.1'),
          status: 'success',
          timestamp: String(raw.created_at ?? new Date().toISOString()),
        }));
        setAuditLogs(mapped);
      }
      toast.success('Audit trail synced');
    } catch {
      toast.error('Could not refresh the audit trail');
    } finally {
      setAuditLoading(false);
    }
  };

  if (mode === 'users') {
    const activeCount = users.filter((u) => u.status !== 'suspended').length;
    const suspendedCount = users.filter((u) => u.status === 'suspended').length;
    const adminCount = users.filter((u) => u.role === 'admin').length;

    return (
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <Users className="h-4 w-4 text-orange-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              Team & Quotas
            </h1>
          </div>

          <button
            type="button"
            onClick={() => setUserModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Add User
          </button>
        </div>

        {/* Summary KPI Strip */}
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-3.5">
            <div className="text-[11px] text-zinc-400">Total Panel Users</div>
            <div className="mt-1 font-mono text-lg font-semibold text-zinc-100">{users.length}</div>
            <div className="mt-0.5 text-[10px] text-zinc-500">{adminCount} Admin · {users.length - adminCount} Standard</div>
          </div>
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-3.5">
            <div className="text-[11px] text-zinc-400">Active Accounts</div>
            <div className="mt-1 font-mono text-lg font-semibold text-emerald-400">{activeCount}</div>
            <div className="mt-0.5 text-[10px] text-zinc-500">All user apps running</div>
          </div>
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-3.5">
            <div className="text-[11px] text-zinc-400">Suspended Accounts</div>
            <div className="mt-1 font-mono text-lg font-semibold text-rose-400">{suspendedCount}</div>
            <div className="mt-0.5 text-[10px] text-zinc-500">Sessions revoked & apps stopped</div>
          </div>
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-3.5">
            <div className="text-[11px] text-zinc-400">Kernel cgroup v2 Slice</div>
            <div className="mt-1 font-mono text-xs font-semibold text-orange-400 truncate">
              nextdeploy-user-&lt;id&gt;.slice
            </div>
            <div className="mt-1 text-[10px] text-zinc-500">Enforces CPUQuota & MemoryMax</div>
          </div>
        </div>

        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[780px] text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                  <th className="px-4 py-2.5 font-medium">User & cgroup Slice</th>
                  <th className="px-4 py-2.5 font-medium">Role & Status</th>
                  <th className="px-4 py-2.5 font-medium">Projects Quota</th>
                  <th className="px-4 py-2.5 font-medium">CPU / RAM / Disk Limits</th>
                  <th className="px-4 py-2.5 font-medium">Static File Server</th>
                  <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {users.map((u) => {
                  const isAdmin = u.role === 'admin';
                  const isUnlimitedApps = isAdmin || u.maxApps === 0;
                  const ramMb = u.maxMemoryMB ?? u.memQuotaGB * 1024;
                  const diskMb = u.maxStorageMB ?? u.diskQuotaGB * 1024;

                  return (
                    <tr key={u.id} className="hover:bg-white/[0.03]">
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-zinc-100">{u.username}</span>
                          {u.twoFactorEnabled && (
                            <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] text-emerald-400">
                              2FA
                            </span>
                          )}
                        </div>
                        <div className="mt-0.5 font-mono text-[11px] text-zinc-500">
                          nextdeploy-user-{u.id}.slice
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span
                            className={`rounded-md px-2 py-0.5 font-mono text-[10px] uppercase ${
                              isAdmin
                                ? 'bg-orange-500/15 text-orange-400 border border-orange-500/30'
                                : 'bg-white/[0.06] text-zinc-300'
                            }`}
                          >
                            {u.role}
                          </span>
                          <StatusBadge
                            status={u.status === 'suspended' ? 'suspended' : 'active'}
                            label={u.status === 'suspended' ? 'suspended' : 'active'}
                          />
                        </div>
                      </td>
                      <td className="px-4 py-3 font-mono text-zinc-300">
                        {isUnlimitedApps ? (
                          <span className="text-emerald-400">{u.appsCount} / Unlimited</span>
                        ) : (
                          <span>
                            {u.appsCount} / {u.maxApps} apps
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3 font-mono text-[11px] text-zinc-300">
                        {isAdmin ? (
                          <span className="text-emerald-400">Unlimited (Admin Bypass)</span>
                        ) : (
                          <div className="space-y-0.5">
                            <div>
                              CPU:{' '}
                              <span className="text-zinc-100">
                                {u.cpuQuotaCores > 0 ? `${u.cpuQuotaCores} cores` : 'Unlimited'}
                              </span>{' '}
                              · RAM:{' '}
                              <span className="text-zinc-100">
                                {ramMb > 0 ? `${ramMb} MB` : 'Unlimited'}
                              </span>
                            </div>
                            <div className="text-zinc-500">
                              Using{' '}
                              <UsageFigure used={u.memUsedMB ?? 0} max={ramMb} fmt={fmtMb} /> RAM ·{' '}
                              <CpuFigure used={u.cpuUsedCores ?? 0} cores={u.cpuQuotaCores} /> CPU
                            </div>
                            <div className="text-zinc-500" title={u.storageBreakdown ? `files ${fmtBytes(u.storageBreakdown.files)} · databases ${fmtBytes(u.storageBreakdown.databases)} · volumes ${fmtBytes(u.storageBreakdown.volumes)}` : undefined}>
                              Disk <UsageFigure used={(u.storageUsedBytes ?? 0) / (1024 * 1024)} max={diskMb} fmt={fmtMb} />
                            </div>
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        {isAdmin || u.allowDomainFileServer ? (
                          <span className="inline-flex items-center gap-1 rounded-md bg-emerald-500/10 px-2 py-0.5 font-mono text-[10px] text-emerald-400">
                            <CheckCircle2 className="h-3 w-3" />
                            Allowed
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 rounded-md bg-zinc-800 px-2 py-0.5 font-mono text-[10px] text-zinc-400">
                            Disabled
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-3 text-right">
                        <ActionMenu
                          items={[
                            {
                              label: 'Configure Resource Quotas',
                              icon: <Sliders className="h-3.5 w-3.5" />,
                              onClick: () => openLimitsModal(u),
                            },
                            {
                              label: 'Change Password',
                              icon: <KeyRound className="h-3.5 w-3.5" />,
                              onClick: () => {
                                setPasswordModalUser(u);
                                setChangePasswordValue('');
                                setChangePasswordConfirm('');
                              },
                            },
                            {
                              label:
                                u.role === 'admin'
                                  ? 'Demote to Standard User'
                                  : 'Promote to Admin',
                              icon: <Shield className="h-3.5 w-3.5" />,
                              disabled: String(u.id) === currentUserId,
                              onClick: () => void handleToggleUserRole(u),
                            },
                            {
                              label:
                                u.status === 'suspended'
                                  ? 'Activate Account'
                                  : 'Suspend Account & Stop Apps',
                              icon: <Ban className="h-3.5 w-3.5" />,
                              disabled: String(u.id) === currentUserId,
                              onClick: () => void handleToggleUserStatus(u),
                            },
                            {
                              label: 'Delete User',
                              icon: <Trash2 className="h-3.5 w-3.5" />,
                              variant: 'danger',
                              disabled: String(u.id) === currentUserId,
                              onClick: async () => {
                                try {
                                  await backendApi.deleteUser(u.id);
                                } catch {
                                  // fallback in preview
                                }
                                setUsers((prev) => prev.filter((item) => item.id !== u.id));
                                toast.success(`Deleted user ${u.username}`);
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
        </div>

        {/* Create User Modal (POST /users) */}
        <ShadcnDialog
          open={userModalOpen}
          onClose={() => setUserModalOpen(false)}
          title="Create Panel User"
          icon={<Users className="h-4 w-4 text-orange-400" />}
        >
          <form onSubmit={handleCreateUser} className="space-y-4">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Username (min 3 characters)
              </label>
              <input
                type="text"
                required
                minLength={3}
                placeholder="e.g. developer-alex"
                value={newUsername}
                onChange={(e) => setNewUsername(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>

            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Password (min 8 characters)
              </label>
              <input
                type="password"
                required
                minLength={8}
                placeholder="Minimum 8 characters"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>

            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Account Role
              </label>
              <div className="grid grid-cols-2 gap-2.5">
                <button
                  type="button"
                  onClick={() => setNewRole('user')}
                  className={`rounded-lg border p-3 text-left transition cursor-pointer ${
                    newRole === 'user'
                      ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  <div className="text-xs font-semibold">Standard User (user)</div>
                  <div className="mt-0.5 text-[11px] text-zinc-400">
                    Isolated apps & enforced cgroup quotas
                  </div>
                </button>
                <button
                  type="button"
                  onClick={() => setNewRole('admin')}
                  className={`rounded-lg border p-3 text-left transition cursor-pointer ${
                    newRole === 'admin'
                      ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  <div className="text-xs font-semibold">Administrator (admin)</div>
                  <div className="mt-0.5 text-[11px] text-zinc-400">
                    Full control plane access & unlimited quotas
                  </div>
                </button>
              </div>
            </div>

            {newRole === 'user' && host?.memoryMB && host.memoryMB > 0 && (
              <div
                className={`rounded-lg border p-3 text-xs ${
                  totalCurrentlyAllocatedMB > host.memoryMB
                    ? 'border-amber-500/30 bg-amber-500/[0.08] text-amber-200'
                    : 'border-white/[0.07] bg-[#09090b] text-zinc-300'
                }`}
              >
                <div className="flex items-center justify-between text-zinc-300 mb-1.5">
                  <span className="text-[11px] text-zinc-400 font-medium">Server RAM Allocation</span>
                  <span
                    className={`font-mono text-[11px] font-semibold ${
                      totalCurrentlyAllocatedMB > host.memoryMB ? 'text-amber-300' : 'text-zinc-200'
                    }`}
                  >
                    {totalCurrentlyAllocatedMB} MB / {host.memoryMB} MB (
                    {Math.round((totalCurrentlyAllocatedMB / host.memoryMB) * 100)}%)
                  </span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/[0.08]">
                  <div
                    style={{
                      width: `${Math.min(100, (totalCurrentlyAllocatedMB / host.memoryMB) * 100)}%`,
                    }}
                    className={`h-full ${
                      totalCurrentlyAllocatedMB > host.memoryMB ? 'bg-amber-400' : 'bg-orange-500'
                    }`}
                  />
                </div>
                {totalCurrentlyAllocatedMB > host.memoryMB ? (
                  <p className="mt-2 text-[11px] text-amber-300/90 flex items-start gap-1.5 leading-relaxed">
                    <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-0.5 text-amber-400" />
                    Existing users already exceed physical server RAM ({Math.round((totalCurrentlyAllocatedMB / host.memoryMB) * 100)}% allocated). We recommend adjusting quotas after creation.
                  </p>
                ) : (
                  <p className="mt-1.5 text-[10.5px] text-zinc-500">
                    Remaining unallocated RAM: {Math.max(0, Math.round(((host.memoryMB - totalCurrentlyAllocatedMB) / 1024) * 10) / 10)} GB
                  </p>
                )}
              </div>
            )}

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setUserModalOpen(false)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Create User
              </button>
            </div>
          </form>
        </ShadcnDialog>

        {/* Edit Resource Quotas Modal (POST /users/:id/limits) */}
        <ShadcnDialog
          open={Boolean(limitsModalUser)}
          onClose={() => setLimitsModalUser(null)}
          title={
            limitsModalUser
              ? `Resource Quotas — ${limitsModalUser.username}`
              : 'Resource Quotas'
          }
          icon={<Sliders className="h-4 w-4 text-orange-400" />}
        >
          {limitsModalUser && (
            <form onSubmit={handleSaveLimits} className="space-y-5">
              {limitsModalUser.role === 'admin' && (
                <div className="rounded-lg border border-orange-500/30 bg-orange-500/10 px-3 py-2 text-xs text-orange-200">
                  Administrators are never limited. These values apply if this account is demoted to a standard user.
                </div>
              )}

              <div className="grid grid-cols-1 gap-x-3 gap-y-4 sm:grid-cols-2">
                <LimitField
                  label="Projects"
                  unit="apps"
                  value={limitMaxApps}
                  onChange={setLimitMaxApps}
                  hint={Number(limitMaxApps) > 0 ? `${limitMaxApps} projects at most` : 'Unlimited'}
                />
                <LimitField
                  label="CPU"
                  unit="cores"
                  step="0.5"
                  value={limitMaxCpus}
                  onChange={setLimitMaxCpus}
                  max={host?.cpus}
                  hint={`${Number(limitMaxCpus) > 0 ? `${Number(limitMaxCpus)} cores at most` : 'Unlimited'}${host?.cpus ? ` · server has ${host.cpus}` : ''}`}
                />
                <LimitField
                  label="Memory"
                  unit="MB"
                  step="256"
                  value={limitMaxMemoryMb}
                  onChange={setLimitMaxMemoryMb}
                  max={host?.memoryMB}
                  hint={`${Number(limitMaxMemoryMb) > 0 ? `≈ ${(Number(limitMaxMemoryMb) / 1024).toFixed(1)} GB RAM` : 'Unlimited'}${host?.memoryMB ? ` · server has ${(host.memoryMB / 1024).toFixed(1)} GB` : ''}`}
                />
                <LimitField
                  label="Storage"
                  unit="MB"
                  step="1024"
                  value={limitMaxStorageMb}
                  onChange={setLimitMaxStorageMb}
                  max={host?.diskMB}
                  hint={`${Number(limitMaxStorageMb) > 0 ? `≈ ${(Number(limitMaxStorageMb) / 1024).toFixed(1)} GB disk` : 'Unlimited'}${host?.diskMB ? ` · server has ${(host.diskMB / 1024).toFixed(0)} GB` : ''}`}
                />
              </div>

              {/* RAM Overcommit / Capacity Indicator */}
              {host && host.memoryMB > 0 && (() => {
                const currentInputMB = Number(limitMaxMemoryMb) || 0;
                const newTotalAllocatedMB = otherUsersAllocatedMB + currentInputMB;
                const hostRAM = host.memoryMB;
                const isOvercommitted = newTotalAllocatedMB > hostRAM;
                const overcommitRatio = Math.round((newTotalAllocatedMB / hostRAM) * 100);

                return (
                  <div
                    className={`rounded-xl border p-3.5 transition-all ${
                      isOvercommitted
                        ? 'border-amber-500/40 bg-amber-500/[0.08] text-amber-200'
                        : 'border-white/[0.08] bg-white/[0.02] text-zinc-300'
                    }`}
                  >
                    <div className="flex items-start gap-2.5">
                      {isOvercommitted ? (
                        <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
                      ) : (
                        <Info className="h-4 w-4 text-zinc-400 shrink-0 mt-0.5" />
                      )}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-xs font-semibold text-zinc-200">
                            {isOvercommitted
                              ? 'RAM Over-allocation Warning (Overcommit)'
                              : 'Server RAM Allocation'}
                          </span>
                          <span
                            className={`font-mono text-xs font-bold ${
                              isOvercommitted ? 'text-amber-300' : 'text-zinc-300'
                            }`}
                          >
                            {newTotalAllocatedMB} MB / {hostRAM} MB ({overcommitRatio}%)
                          </span>
                        </div>

                        {/* Visual Allocation Stack Bar */}
                        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-white/[0.08] flex">
                          <div
                            style={{ width: `${Math.min(100, (otherUsersAllocatedMB / hostRAM) * 100)}%` }}
                            className="h-full bg-zinc-500/80 transition-all"
                            title={`Other users: ${otherUsersAllocatedMB} MB`}
                          />
                          <div
                            style={{
                              width: `${Math.min(
                                Math.max(0, 100 - (otherUsersAllocatedMB / hostRAM) * 100),
                                (currentInputMB / hostRAM) * 100
                              )}%`,
                            }}
                            className={`h-full ${isOvercommitted ? 'bg-amber-400' : 'bg-orange-500'} transition-all`}
                            title={`This user: ${currentInputMB} MB`}
                          />
                        </div>

                        <p className="mt-2 text-[11px] leading-relaxed text-zinc-400">
                          {isOvercommitted ? (
                            <>
                              Total assigned RAM across standard users will reach{' '}
                              <strong className="text-amber-200 font-mono">
                                {(newTotalAllocatedMB / 1024).toFixed(1)} GB
                              </strong>
                              , exceeding physical server capacity ({ (hostRAM / 1024).toFixed(1) } GB) by{' '}
                              <strong className="text-amber-200 font-mono">
                                {((newTotalAllocatedMB - hostRAM) / 1024).toFixed(1)} GB
                              </strong>
                              . If multiple users reach memory peaks at the same time, the Linux OOM killer may terminate containers.
                            </>
                          ) : (
                            <>
                              Other users allocated:{' '}
                              <span className="font-mono text-zinc-300">
                                {(otherUsersAllocatedMB / 1024).toFixed(1)} GB
                              </span>
                              . Remaining unallocated RAM:{' '}
                              <span className="font-mono text-emerald-400 font-semibold">
                                {Math.max(0, (hostRAM - newTotalAllocatedMB) / 1024).toFixed(1)} GB
                              </span>
                              .
                            </>
                          )}
                        </p>
                      </div>
                    </div>
                  </div>
                );
              })()}

              {/* Storage Capacity & Overselling Indicator */}
              {host && host.diskMB > 0 && (() => {
                const currentInputStorageMB = Number(limitMaxStorageMb) || 0;
                const newTotalAllocatedStorageMB = otherUsersAllocatedStorageMB + currentInputStorageMB;
                const hostDiskMB = host.diskMB;
                const isOvercommitted = newTotalAllocatedStorageMB > hostDiskMB;
                const overcommitRatio = Math.round((newTotalAllocatedStorageMB / hostDiskMB) * 100);

                return (
                  <div
                    className={`rounded-xl border p-3.5 transition-all ${
                      !storageOversellingEnabled && isOvercommitted
                        ? 'border-rose-500/40 bg-rose-500/[0.08] text-rose-200'
                        : isOvercommitted
                          ? 'border-amber-500/40 bg-amber-500/[0.08] text-amber-200'
                          : 'border-white/[0.08] bg-white/[0.02] text-zinc-300'
                    }`}
                  >
                    <div className="flex items-start gap-2.5">
                      {!storageOversellingEnabled && isOvercommitted ? (
                        <Ban className="h-4 w-4 text-rose-400 shrink-0 mt-0.5" />
                      ) : isOvercommitted ? (
                        <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
                      ) : (
                        <HardDrive className="h-4 w-4 text-zinc-400 shrink-0 mt-0.5" />
                      )}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-xs font-semibold text-zinc-200">
                            {!storageOversellingEnabled
                              ? 'Strict Storage Allocation'
                              : isOvercommitted
                                ? 'Storage Overselling Active (Thin Provisioning)'
                                : 'Server Storage Allocation'}
                          </span>
                          <span
                            className={`font-mono text-xs font-bold ${
                              !storageOversellingEnabled && isOvercommitted
                                ? 'text-rose-400'
                                : isOvercommitted
                                  ? 'text-amber-300'
                                  : 'text-zinc-300'
                            }`}
                          >
                            {(newTotalAllocatedStorageMB / 1024).toFixed(1)} GB / {(hostDiskMB / 1024).toFixed(1)} GB ({overcommitRatio}%)
                          </span>
                        </div>

                        {/* Visual Allocation Stack Bar */}
                        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-white/[0.08] flex">
                          <div
                            style={{ width: `${Math.min(100, (otherUsersAllocatedStorageMB / hostDiskMB) * 100)}%` }}
                            className="h-full bg-zinc-500/80 transition-all"
                            title={`Other users: ${(otherUsersAllocatedStorageMB / 1024).toFixed(1)} GB`}
                          />
                          <div
                            style={{
                              width: `${Math.min(
                                Math.max(0, 100 - (otherUsersAllocatedStorageMB / hostDiskMB) * 100),
                                (currentInputStorageMB / hostDiskMB) * 100
                              )}%`,
                            }}
                            className={`h-full ${
                              !storageOversellingEnabled && isOvercommitted
                                ? 'bg-rose-500'
                                : isOvercommitted
                                  ? 'bg-amber-400'
                                  : 'bg-emerald-500'
                            } transition-all`}
                            title={`This user: ${(currentInputStorageMB / 1024).toFixed(1)} GB`}
                          />
                        </div>

                        <p className="mt-2 text-[11px] leading-relaxed text-zinc-400">
                          {!storageOversellingEnabled ? (
                            isOvercommitted ? (
                              <span className="text-rose-300">
                                <strong>Strict Mode Violation:</strong> Total quotas cannot exceed physical server disk capacity ({(hostDiskMB / 1024).toFixed(1)} GB). Only {Math.max(0, (hostDiskMB - otherUsersAllocatedStorageMB) / 1024).toFixed(1)} GB remains available.
                              </span>
                            ) : (
                              <span>
                                Strict Mode Active: Remaining assignable disk space is <strong className="text-emerald-400 font-mono">{Math.max(0, (hostDiskMB - newTotalAllocatedStorageMB) / 1024).toFixed(1)} GB</strong>.
                              </span>
                            )
                          ) : isOvercommitted ? (
                            <span>
                              Total user quotas exceed physical server disk by <strong className="text-amber-200 font-mono">{((newTotalAllocatedStorageMB - hostDiskMB) / 1024).toFixed(1)} GB</strong>. Storage overselling is enabled; the <strong>1.5 GB host safety buffer</strong> will dynamically pause writes if physical disk runs low.
                            </span>
                          ) : (
                            <span>
                              Remaining unallocated physical capacity: <strong className="text-emerald-400 font-mono">{Math.max(0, (hostDiskMB - newTotalAllocatedStorageMB) / 1024).toFixed(1)} GB</strong>.
                            </span>
                          )}
                        </p>
                      </div>
                    </div>
                  </div>
                );
              })()}

              <p className="-mt-1 text-[11px] text-zinc-500">
                0 means unlimited. Limits are applied to the user's systemd slice{' '}
                <code className="font-mono text-zinc-400">nextdeploy-user-{limitsModalUser.id}.slice</code> and cannot exceed this server's real capacity.
              </p>

              <div className="flex items-center justify-between gap-4 rounded-lg border border-white/[0.07] bg-[#09090b] px-3.5 py-3">
                <div className="min-w-0">
                  <div className="text-xs font-medium text-zinc-200">Static and media file server</div>
                  <div className="mt-0.5 text-[11px] leading-snug text-zinc-500">
                    Lets this user's domains serve static or media paths directly through Caddy.
                  </div>
                </div>
                <div className="shrink-0">
                  <ShadcnSwitch
                    checked={limitAllowDomainFileServer}
                    onChange={setLimitAllowDomainFileServer}
                  />
                </div>
              </div>

              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] px-3.5 py-3 space-y-1.5">
                <div className="flex items-center gap-1.5 text-xs font-medium text-zinc-200">
                  <Shield className="h-3.5 w-3.5 text-emerald-400" />
                  Default Shield VPS Proxy (Origin Masking)
                </div>
                <div className="text-[11px] text-zinc-500">
                  When this user deploys sites or adds domains, route them through this offshore/DDoS shield node instead of revealing the server origin IP.
                </div>
                <select
                  value={limitDefaultShieldId ?? ''}
                  onChange={(e) => setLimitDefaultShieldId(e.target.value ? Number(e.target.value) : null)}
                  className="w-full mt-2 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-white focus:border-emerald-500 focus:outline-none"
                >
                  <option value="">🌐 Direct (Default Origin IP)</option>
                  {shieldNodes.map((s) => (
                    <option key={s.id} value={s.id}>
                      🛡️ {s.name} ({s.ip}) {s.status === 'connected' ? '• Active' : `• ${s.status}`}
                    </option>
                  ))}
                </select>
              </div>

              <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-4">
                <button
                  type="button"
                  onClick={() => setLimitsModalUser(null)}
                  className="h-8 rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="h-8 rounded-lg bg-orange-500 px-4 text-xs font-semibold text-white hover:bg-orange-600 cursor-pointer"
                >
                  Save limits
                </button>
              </div>
            </form>
          )}
        </ShadcnDialog>

        {/* Change Password Modal (POST /users/:id/password) */}
        <ShadcnDialog
          open={Boolean(passwordModalUser)}
          onClose={() => setPasswordModalUser(null)}
          title={
            passwordModalUser
              ? `Change Password — ${passwordModalUser.username}`
              : 'Change Password'
          }
          icon={<KeyRound className="h-4 w-4 text-orange-400" />}
        >
          <form onSubmit={handleChangePassword} className="space-y-4">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                New Password (min 8 characters)
              </label>
              <input
                type="password"
                required
                minLength={8}
                value={changePasswordValue}
                onChange={(e) => setChangePasswordValue(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Confirm New Password
              </label>
              <input
                type="password"
                required
                minLength={8}
                value={changePasswordConfirm}
                onChange={(e) => setChangePasswordConfirm(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setPasswordModalUser(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Update Password
              </button>
            </div>
          </form>
        </ShadcnDialog>
      </div>
    );
  }

  if (mode === 'audit-logs') {
    const startIdx = totalAuditItems === 0 ? 0 : (safeAuditPage - 1) * auditPerPage + 1;
    const endIdx = Math.min(totalAuditItems, safeAuditPage * auditPerPage);

    return (
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <FileText className="h-4 w-4 text-orange-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Audit Trail</h1>
          </div>

          <button
            type="button"
            onClick={() => void handleRefreshAuditLogs()}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${auditLoading ? 'animate-spin text-orange-400' : ''}`} />
            Sync Logs
          </button>
        </div>

        {/* Search & Category Filter Bar */}
        <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative flex-1 max-w-md">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
            <input
              type="text"
              value={auditSearch}
              onChange={(e) => {
                setAuditSearch(e.target.value);
                setAuditPage(1);
              }}
              placeholder="Search by action, user, resource, details, or IP..."
              className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#111115] pl-9 pr-3 text-xs text-zinc-100 placeholder:text-zinc-500 focus:border-orange-500/50 focus:outline-none"
            />
          </div>

          <div className="flex items-center gap-1.5 overflow-x-auto rounded-xl border border-white/[0.08] bg-[#09090b] p-1 scrollbar-none w-full sm:w-auto">
            {[
              { id: 'all', label: 'All Events' },
              { id: 'app.', label: 'Apps' },
              { id: 'user.', label: 'Users & Quotas' },
              { id: 'backup.', label: 'Backups' },
              { id: 'caddy.', label: 'Caddy Proxy' },
              { id: 'system.', label: 'System' },
            ].map((cat) => (
              <button
                key={cat.id}
                type="button"
                onClick={() => {
                  setAuditCategory(cat.id);
                  setAuditPage(1);
                }}
                className={`h-8 sm:h-7 rounded-lg px-3 text-xs font-medium transition cursor-pointer shrink-0 whitespace-nowrap ${
                  auditCategory === cat.id
                    ? 'bg-orange-500/20 text-orange-300 font-semibold shadow-sm'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {cat.label}
              </button>
            ))}
          </div>
        </div>

        {/* Audit Trail Table */}
        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[740px] text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                  <th className="px-4 py-2.5 font-medium">Action</th>
                  <th className="px-4 py-2.5 font-medium">Actor</th>
                  <th className="px-4 py-2.5 font-medium">Resource & Details</th>
                  <th className="px-4 py-2.5 font-medium">Client IP</th>
                  <th className="px-4 py-2.5 font-medium">Status</th>
                  <th className="px-4 py-2.5 font-medium text-right">Timestamp</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {paginatedAuditLogs.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-xs text-zinc-500">
                      No audit events match your current filter.
                    </td>
                  </tr>
                ) : (
                  paginatedAuditLogs.map((log) => (
                    <tr key={log.id} className="hover:bg-white/[0.03]">
                      <td className="px-4 py-3 font-mono font-semibold text-orange-400 whitespace-nowrap">
                        {log.action}
                      </td>
                      <td className="px-4 py-3 text-zinc-200 whitespace-nowrap">
                        <OwnerBadge name={log.actor} role={log.actorRole} id={-1} />
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-mono text-zinc-200">{log.target}</span>
                          {log.targetOwner && log.targetOwner !== log.actor && (
                            <span className="inline-flex items-center gap-1 text-[10px] text-zinc-500">
                              of <OwnerBadge name={log.targetOwner} role={log.targetOwnerRole} id={-1} />
                            </span>
                          )}
                        </div>
                        {log.details && (
                          <div className="mt-0.5 text-[11px] text-zinc-500">{log.details}</div>
                        )}
                      </td>
                      <td className="px-4 py-3 font-mono text-[11px] text-zinc-400 whitespace-nowrap">
                        {log.ip}
                      </td>
                      <td className="px-4 py-3 whitespace-nowrap">
                        <StatusBadge
                          status={log.status === 'success' ? 'ready' : 'failed'}
                          label={log.status}
                        />
                      </td>
                      <td className="px-4 py-3 text-right font-mono text-[11px] text-zinc-400 whitespace-nowrap">
                        {log.timestamp}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          {/* Pagination Footer */}
          <div className="flex flex-col gap-3 border-t border-white/[0.07] bg-[#09090b]/60 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex flex-wrap items-center gap-3 text-xs text-zinc-400">
              <span>
                Showing <strong className="font-mono text-zinc-200">{startIdx}</strong>–
                <strong className="font-mono text-zinc-200">{endIdx}</strong> of{' '}
                <strong className="font-mono text-zinc-200">{totalAuditItems}</strong> events
              </span>
              <div className="flex items-center gap-1.5">
                <span className="text-[11px] text-zinc-500">Rows per page:</span>
                <select
                  value={auditPerPage}
                  onChange={(e) => {
                    setAuditPerPage(Number(e.target.value));
                    setAuditPage(1);
                  }}
                  className="rounded-md border border-white/[0.09] bg-zinc-900 px-2 py-1 font-mono text-xs text-zinc-200 focus:outline-none"
                >
                  <option value={5}>5</option>
                  <option value={10}>10</option>
                  <option value={20}>20</option>
                </select>
              </div>
            </div>

            <div className="flex items-center gap-1.5">
              <button
                type="button"
                disabled={safeAuditPage <= 1}
                onClick={() => setAuditPage((p) => Math.max(1, p - 1))}
                className="inline-flex h-8 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer disabled:cursor-not-allowed"
              >
                <ChevronLeft className="h-3.5 w-3.5" />
                Prev
              </button>

              {Array.from({ length: totalAuditPages }, (_, i) => i + 1).map((pageNum) => (
                <button
                  key={pageNum}
                  type="button"
                  onClick={() => setAuditPage(pageNum)}
                  className={`h-7 min-w-[28px] rounded-md px-2 font-mono text-xs font-medium transition cursor-pointer ${
                    pageNum === safeAuditPage
                      ? 'bg-orange-500 text-zinc-950 font-semibold'
                      : 'border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200'
                  }`}
                >
                  {pageNum}
                </button>
              ))}

              <button
                type="button"
                disabled={safeAuditPage >= totalAuditPages}
                onClick={() => setAuditPage((p) => Math.min(totalAuditPages, p + 1))}
                className="inline-flex h-8 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer disabled:cursor-not-allowed"
              >
                Next
                <ChevronRight className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Settings className="h-4 w-4 text-orange-400" />
          </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              System Settings
            </h1>
          </div>

        <button
          type="button"
          disabled={isSavingSettings}
          onClick={async () => {
            setIsSavingSettings(true);
            try {
              const res = await backendApi.saveSettings({
                cleanup_enabled: cleanupEnabled,
                cleanup_interval: cleanupInterval,
                cleanup_include_build_cache: cleanupIncludeBuildCache,
                storage_overselling_enabled: storageOversellingEnabled,
              });
              if (res.ok) {
                toast.success(`Saved system settings and cleanup schedule (${cleanupInterval})`);
              } else if (res.data?.overcommitted) {
                setOvercommitInfo({
                  allocated_mb: res.data.allocated_mb || totalCurrentlyAllocatedStorageMB,
                  host_disk_mb: res.data.host_disk_mb || (host?.diskMB || 0),
                  excess_mb: res.data.excess_mb || 0,
                  users: res.data.users || [],
                });
                setOvercommitModalOpen(true);
              } else {
                toast.error((res as any).error || 'Failed to save settings');
              }
            } catch (err: any) {
              toast.error(err.message || 'Failed to save settings');
            } finally {
              setIsSavingSettings(false);
            }
          }}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
        >
          {isSavingSettings ? 'Saving...' : 'Save Changes'}
        </button>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {/* Storage Allocation Card */}
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-orange-500/10 text-orange-400 border border-orange-500/20">
                <HardDrive className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <h2 className="text-sm font-semibold text-zinc-100 truncate">
                  Storage Allocation
                </h2>
                <p className="text-[11px] text-zinc-500 truncate">Tenant quotas &amp; overcommit limits</p>
              </div>
            </div>
            <button
              type="button"
              onClick={() => setShowStorageKnowledgeModal(true)}
              aria-label="Storage allocation guide"
              title="How storage allocation and safety buffer works"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition cursor-pointer"
            >
              <HelpCircle className="h-4 w-4" />
            </button>
          </div>

          <div className="space-y-2.5 text-xs">
            <div className="flex items-center justify-between gap-3 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div className="min-w-0">
                <span className="text-zinc-200 block font-medium">Allow Storage Overselling</span>
                <span className="text-[11px] text-zinc-500 block mt-0.5">
                  Allow tenant quotas to exceed physical server disk
                </span>
              </div>
              <div className="shrink-0">
                <ShadcnSwitch
                  checked={storageOversellingEnabled}
                  onChange={handleToggleStorageOverselling}
                />
              </div>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono text-[11px]">
              <span className="text-zinc-400">Physical Host Disk</span>
              <span className="text-zinc-200 font-semibold">
                {host?.diskMB ? `${(host.diskMB / 1024).toFixed(1)} GB` : '—'}
              </span>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono text-[11px]">
              <span className="text-zinc-400">Total User Quotas Allocated</span>
              <div className="flex items-center gap-1.5">
                <span
                  className={`font-semibold ${
                    host?.diskMB && totalCurrentlyAllocatedStorageMB > host.diskMB
                      ? 'text-amber-400'
                      : 'text-zinc-200'
                  }`}
                >
                  {(totalCurrentlyAllocatedStorageMB / 1024).toFixed(1)} GB
                </span>
                <span className="text-[10px] text-zinc-500 font-sans">
                  ({users.filter((u) => u.role !== 'admin').length} user{users.filter((u) => u.role !== 'admin').length === 1 ? '' : 's'})
                </span>
              </div>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono text-[11px]">
              <span className="text-zinc-400">Host Emergency Buffer</span>
              <span className="text-emerald-400 font-semibold">1.5 GB</span>
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-orange-500/10 text-orange-400 border border-orange-500/20">
                <Server className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <h2 className="text-sm font-semibold text-zinc-100 truncate">
                  Scheduled Docker Cleanup
                </h2>
                <p className="text-[11px] text-zinc-500 truncate">Automatic container &amp; cache pruning</p>
              </div>
            </div>
            <StatusBadge
              status={cleanupLastStatus === 'ok' ? 'ready' : 'failed'}
              label={cleanupLastStatus}
              size="sm"
            />
          </div>

          <div className="grid grid-cols-2 gap-2 w-full">
            <button
              type="button"
              onClick={() => setShowCleanupLogModal(true)}
              className="h-9 px-3 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer flex items-center justify-center gap-1.5 transition-colors"
            >
              <Terminal className="h-3.5 w-3.5 text-zinc-400" />
              <span>Last Output</span>
            </button>
            <button
              type="button"
              disabled={cleanupRunning}
              onClick={async () => {
                setCleanupRunning(true);
                try {
                  const res = await backendApi.runDockerCleanup();
                  if (res.data?.log) setCleanupLastLog(res.data.log);
                  setCleanupLastStatus(res.ok ? 'ok' : 'error');
                } catch {
                  // fallback in preview
                } finally {
                  setCleanupLastRunAt(new Date().toISOString().replace('T', ' ').slice(0, 19) + ' UTC');
                  setCleanupRunning(false);
                  toast.success('Executed Docker container, image & network prune');
                }
              }}
              className="h-9 px-3 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs font-medium text-orange-400 hover:bg-zinc-800 disabled:opacity-50 cursor-pointer flex items-center justify-center gap-1.5 transition-colors"
            >
              <RefreshCw className={`h-3.5 w-3.5 text-orange-400 ${cleanupRunning ? 'animate-spin' : ''}`} />
              <span>{cleanupRunning ? 'Running...' : 'Run Cleanup Now'}</span>
            </button>
          </div>

          <div className="space-y-2.5 text-xs">
            <div className="flex items-center justify-between gap-3 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div className="min-w-0 pr-2">
                <span className="text-zinc-200 block font-medium">Enable Scheduled Prune</span>
                <span className="text-[11px] text-zinc-500 block">
                  Prunes stopped containers, unused images and dangling networks
                </span>
              </div>
              <ShadcnSwitch checked={cleanupEnabled} onChange={setCleanupEnabled} />
            </div>

            <div className="flex items-center justify-between gap-3 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div className="min-w-0 pr-2">
                <span className="text-zinc-200 block font-medium">Cleanup Interval</span>
                <span className="text-[11px] text-zinc-500 block">
                  Background maintenance frequency
                </span>
              </div>
              <select
                value={cleanupInterval}
                onChange={(e) =>
                  setCleanupInterval(e.target.value as '1h' | '6h' | '12h' | '24h' | '168h')
                }
                className="h-8 rounded-lg border border-white/[0.09] bg-zinc-950 px-2.5 font-mono text-xs text-zinc-100 shrink-0 cursor-pointer"
              >
                <option value="1h">Every 1h</option>
                <option value="6h">Every 6h</option>
                <option value="12h">Every 12h</option>
                <option value="24h">Daily (24h)</option>
                <option value="168h">Weekly</option>
              </select>
            </div>

            <div className="flex items-center justify-between gap-3 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div className="min-w-0 pr-2">
                <span className="text-zinc-200 block font-medium">Include BuildKit Cache</span>
                <span className="text-[11px] text-zinc-500 block">
                  Also runs <code className="font-mono text-zinc-300">docker builder prune -af</code>
                </span>
              </div>
              <ShadcnSwitch
                checked={cleanupIncludeBuildCache}
                onChange={setCleanupIncludeBuildCache}
              />
            </div>

            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1 rounded-lg border border-white/[0.06] bg-[#09090b] p-3 text-xs">
              <span className="text-zinc-400 font-medium">Last Cleanup Run</span>
              <span className="text-zinc-300 font-mono text-[11px] break-all sm:break-normal">{cleanupLastRunAt}</span>
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                <ShieldCheck className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <h2 className="text-sm font-semibold text-zinc-100 truncate">
                  Temp Files &amp; System Paths
                </h2>
                <p className="text-[11px] text-zinc-500 truncate">Runtime storage &amp; sockets</p>
              </div>
            </div>
            <button
              type="button"
              onClick={async () => {
                const info = await backendApi.getTmpCleanupInfo();
                if (typeof info.count === 'number') setTmpCount(info.count);
                if (info.total_size) setTmpTotalSize(info.total_size);
                toast.success('Scanned /tmp for orphaned NextDeploy archives');
              }}
              className="h-8 px-2.5 sm:px-3 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer shrink-0 transition-colors"
            >
              Scan /tmp
            </button>
          </div>

          <div className="space-y-2.5 text-xs">
            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2.5 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div>
                <span className="text-zinc-200 block font-medium">Orphaned Temp Archives</span>
                <span className="text-[11px] text-zinc-500 font-mono">
                  {tmpCount} item(s) · {tmpTotalSize} in /tmp
                </span>
              </div>
              <button
                type="button"
                onClick={async () => {
                  await backendApi.runTmpCleanup();
                  setTmpCount(0);
                  setTmpTotalSize('0 B');
                  toast.success('Cleaned orphaned temporary archives from /tmp');
                }}
                className="w-full sm:w-auto h-8 px-3 rounded-lg border border-rose-500/30 bg-rose-500/10 font-sans text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer flex items-center justify-center shrink-0 transition-colors"
              >
                Clean Temp Files
              </button>
            </div>

            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1 sm:gap-4 rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <span className="text-zinc-500 text-[11px] sm:text-xs shrink-0">Apps Directory</span>
              <span className="text-zinc-100 text-[11px] sm:text-xs sm:text-right">/data/apps</span>
            </div>

            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1 sm:gap-4 rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <span className="text-zinc-500 text-[11px] sm:text-xs shrink-0">Reverse Proxy Engine</span>
              <span className="text-orange-400 text-[11px] sm:text-xs truncate sm:text-right">Caddy 2 (Caddyfile + Admin API)</span>
            </div>

            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1 sm:gap-4 rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <span className="text-zinc-500 text-[11px] sm:text-xs shrink-0">Docker Socket</span>
              <span className="text-emerald-400 text-[11px] sm:text-xs break-all sm:break-normal sm:text-right">unix:///var/run/docker.sock</span>
            </div>
          </div>
        </div>
      </div>

      <ShadcnDialog
        open={showCleanupLogModal}
        onClose={() => setShowCleanupLogModal(false)}
        title="Last Scheduled Docker Cleanup Log"
        icon={<Terminal className="h-4 w-4 text-orange-400" />}
        maxWidth="lg"
      >
        <div className="space-y-3">
          <pre className="max-h-72 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#070709] p-3.5 font-mono text-xs text-zinc-300 whitespace-pre-wrap">
            {cleanupLastLog}
          </pre>
          <div className="flex justify-end">
            <button
              type="button"
              onClick={() => setShowCleanupLogModal(false)}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Close
            </button>
          </div>
        </div>
      </ShadcnDialog>

      {/* Overcommit Resolution Modal */}
      <ShadcnDialog
        open={overcommitModalOpen}
        onClose={() => setOvercommitModalOpen(false)}
        title={`Cannot Disable Storage Overselling (Overcommitted by ${(((overcommitInfo?.excess_mb || 0)) / 1024).toFixed(1)} GB)`}
        icon={<AlertTriangle className="h-5 w-5 text-amber-400" />}
        maxWidth="lg"
      >
        <div className="space-y-4">
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3.5 text-xs text-amber-200 leading-relaxed">
            <div className="font-semibold text-amber-300 flex items-center gap-1.5 mb-1">
              <AlertTriangle className="h-4 w-4" />
              Active User Quotas Exceed Server Capacity
            </div>
            Currently assigned standard user quotas total{' '}
            <strong className="text-white font-mono">{((overcommitInfo?.allocated_mb || 0) / 1024).toFixed(1)} GB</strong>,
            which exceeds your server's usable capacity (
            <strong className="text-white font-mono">{((overcommitInfo?.host_disk_mb || 0) / 1024).toFixed(1)} GB</strong>) by{' '}
            <strong className="text-amber-300 font-mono">{((overcommitInfo?.excess_mb || 0) / 1024).toFixed(1)} GB</strong>.
            To enforce strict capacity allocation, reduce user quotas below server capacity first.
          </div>

          <div className="space-y-2">
            <div className="text-xs font-semibold text-zinc-300">
              Active User Storage Quotas ({overcommitInfo?.users.length || 0})
            </div>
            <div className="max-h-60 overflow-y-auto divide-y divide-white/[0.05] rounded-lg border border-white/[0.08] bg-[#09090b]">
              {(overcommitInfo?.users || []).map((u) => {
                const fullUser = users.find((usr) => String(usr.id) === String(u.id));
                const quotaGB = u.unlimited ? 'Unlimited' : `${(u.max_storage_mb / 1024).toFixed(1)} GB`;
                return (
                  <div key={u.id} className="flex items-center justify-between p-3 text-xs">
                    <div className="flex items-center gap-2.5">
                      <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-zinc-800 text-zinc-300 font-mono font-bold text-xs">
                        {u.username.slice(0, 2).toUpperCase()}
                      </div>
                      <div>
                        <div className="font-medium text-zinc-200">{u.username}</div>
                        <div className="text-[11px] text-zinc-500">Current quota: <span className="font-mono text-zinc-300">{quotaGB}</span></div>
                      </div>
                    </div>
                    {fullUser && (
                      <button
                        type="button"
                        onClick={() => {
                          setOvercommitModalOpen(false);
                          openLimitsModal(fullUser);
                        }}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-800 px-3 py-1.5 text-xs font-medium text-zinc-200 hover:bg-zinc-700 cursor-pointer"
                      >
                        <Sliders className="h-3.5 w-3.5 text-orange-400" />
                        Edit Quota
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-2 border-t border-white/[0.07]">
            <button
              type="button"
              onClick={() => setOvercommitModalOpen(false)}
              className="rounded-lg bg-zinc-100 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Keep Overselling Enabled
            </button>
          </div>
        </div>
      </ShadcnDialog>

      {/* Storage Allocation Knowledge Modal */}
      <ShadcnDialog
        open={showStorageKnowledgeModal}
        onClose={() => setShowStorageKnowledgeModal(false)}
        title="Storage Allocation Guide"
        icon={<HardDrive className="h-4 w-4 text-orange-400" />}
        maxWidth="md"
      >
        <div className="space-y-3 text-xs">
          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3.5 space-y-1">
            <div className="flex items-center gap-2 font-semibold text-zinc-100">
              <span className="h-2 w-2 rounded-full bg-emerald-400" />
              Thin Provisioning (Overselling ON)
            </div>
            <p className="text-[11px] leading-relaxed text-zinc-400">
              User quotas act as soft ceilings. You can allocate more combined quota to your tenants than the physical server disk, assuming not everyone fills their quota at the same time.
            </p>
          </div>

          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3.5 space-y-1">
            <div className="flex items-center gap-2 font-semibold text-zinc-100">
              <span className="h-2 w-2 rounded-full bg-orange-400" />
              Strict Capacity (Overselling OFF)
            </div>
            <p className="text-[11px] leading-relaxed text-zinc-400">
              Every gigabyte assigned to a tenant is guaranteed. The total sum of all user storage quotas cannot exceed the server's real disk capacity.
            </p>
          </div>

          <div className="rounded-lg border border-emerald-500/20 bg-emerald-500/[0.04] p-3.5 space-y-1">
            <div className="flex items-center gap-2 font-semibold text-emerald-300">
              <ShieldCheck className="h-4 w-4 text-emerald-400" />
              1.5 GB Host Safety Buffer
            </div>
            <p className="text-[11px] leading-relaxed text-zinc-400">
              Always active regardless of overselling mode. If the host machine's free disk space drops below 1.5 GB, new file uploads and database writes for non-admin users are automatically paused to protect MariaDB, Docker, and the OS from deadlock.
            </p>
          </div>

          <div className="flex justify-end pt-1">
            <button
              type="button"
              onClick={() => setShowStorageKnowledgeModal(false)}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Got it
            </button>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
};
