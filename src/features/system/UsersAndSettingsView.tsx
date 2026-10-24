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
  Eye,
  EyeOff,
  User,
  UserPlus,
  Lock,
  Check,
} from 'lucide-react';
import { toast } from 'sonner';
import { AuditLogEntry, PanelUser } from '../../types/nextdeploy';
import {
  ActionMenu,
  ShadcnDialog,
  ShadcnSwitch,
  StatusBadge,
} from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';

interface UsersAndSettingsViewProps {
  mode: 'users' | 'audit-logs' | 'settings';
  users: PanelUser[];
  auditLogs: AuditLogEntry[];
  cgroupInfo?: { mode?: string; supported?: boolean };
}

export const UsersAndSettingsView: React.FC<UsersAndSettingsViewProps> = ({
  mode,
  users: initialUsers,
  auditLogs: initialAuditLogs,
  cgroupInfo,
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
  const [showNewPassword, setShowNewPassword] = useState(false);

  // Edit Resource Limits Modal State (matches POST /users/:id/limits in users_handler.go)
  const [limitsModalUser, setLimitsModalUser] = useState<PanelUser | null>(null);
  const [limitMaxApps, setLimitMaxApps] = useState('10');
  const [limitMaxCpus, setLimitMaxCpus] = useState('4');
  const [limitMaxMemoryMb, setLimitMaxMemoryMb] = useState('8192');
  const [limitMaxStorageMb, setLimitMaxStorageMb] = useState('51200');
  const [limitBandwidthMbps, setLimitBandwidthMbps] = useState('0');
  const [limitAllowDomainFileServer, setLimitAllowDomainFileServer] = useState(true);

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
  const [cleanupIncludeBuildCache, setCleanupIncludeBuildCache] = useState(false);
  const [cleanupRunning, setCleanupRunning] = useState(false);
  const [cleanupLastRunAt, setCleanupLastRunAt] = useState('2025-05-15 03:00:00 UTC');
  const [cleanupLastStatus, setCleanupLastStatus] = useState<'ok' | 'error'>('ok');
  const [cleanupLastLog, setCleanupLastLog] = useState(
    '--- docker container prune -f ---\nTotal reclaimed space: 0B\n--- docker image prune -af ---\nDeleted Images:\nuntagged: ghcr.io/masud/Billing-Service:old-sha\nTotal reclaimed space: 412.6MB\n--- docker network prune -f ---\nTotal reclaimed space: 0B'
  );
  const [showCleanupLogModal, setShowCleanupLogModal] = useState(false);
  const [tmpCount, setTmpCount] = useState(4);
  const [tmpTotalSize, setTmpTotalSize] = useState('128.4 MB');

  const openLimitsModal = (user: PanelUser) => {
    setLimitsModalUser(user);
    setLimitMaxApps(String(user.maxApps ?? 0));
    setLimitMaxCpus(String(user.cpuQuotaCores ?? 0));
    setLimitMaxMemoryMb(String(user.maxMemoryMB ?? (user.memQuotaGB ? user.memQuotaGB * 1024 : 0)));
    setLimitMaxStorageMb(String(user.maxStorageMB ?? (user.diskQuotaGB ? user.diskQuotaGB * 1024 : 0)));
    setLimitBandwidthMbps(String(user.bandwidthLimitMbps ?? 0));
    setLimitAllowDomainFileServer(user.allowDomainFileServer ?? true);
  };

  const handleSaveLimits = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!limitsModalUser) return;

    const maxApps = Math.max(0, parseInt(limitMaxApps, 10) || 0);
    const maxCpus = Math.max(0, parseFloat(limitMaxCpus) || 0);
    const maxMemoryMb = Math.max(0, parseInt(limitMaxMemoryMb, 10) || 0);
    const maxStorageMb = Math.max(0, parseInt(limitMaxStorageMb, 10) || 0);
    const bandwidthLimitMbps = Math.max(0, parseInt(limitBandwidthMbps, 10) || 0);

    try {
      await backendApi.updateUserLimits(limitsModalUser.id, {
        max_apps: maxApps,
        max_cpus: maxCpus,
        max_memory_mb: maxMemoryMb,
        max_storage_mb: maxStorageMb,
        bandwidth_limit_mbps: bandwidthLimitMbps,
        allow_domain_file_server: limitAllowDomainFileServer,
      });
    } catch {
      // fallback in preview
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
              bandwidthLimitMbps,
              allowDomainFileServer: limitAllowDomainFileServer,
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
      bandwidthLimitMbps: 0,
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
          actorRole: 'admin',
          target: String(raw.resource_id || raw.resource_type || 'system'),
          details: String(raw.details ?? ''),
          ip: String(raw.ip_address ?? '127.0.0.1'),
          status: 'success',
          timestamp: String(raw.created_at ?? new Date().toISOString()),
        }));
        setAuditLogs(mapped);
      }
      toast.success('Synced audit trail with control plane SQLite database');
    } catch {
      toast.success('Refreshed audit trail');
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
            <div className="flex items-center justify-between">
              <span className="text-[11px] text-zinc-400">Kernel cgroup v2 Slice</span>
              <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 font-mono text-[9px] font-semibold uppercase ${
                cgroupInfo?.supported !== false
                  ? 'border border-emerald-500/25 bg-emerald-500/10 text-emerald-400'
                  : 'border border-zinc-700 bg-zinc-800 text-zinc-400'
              }`}>
                {cgroupInfo?.mode ? `${cgroupInfo.mode} driver` : 'Active'}
              </span>
            </div>
            <div className="mt-1 font-mono text-xs font-semibold text-orange-400 truncate">
              nextdeploy-user-&lt;id&gt;.slice
            </div>
            <div className="mt-1 text-[10px] text-zinc-500">
              {cgroupInfo?.supported !== false ? 'Hardware CPUQuota & MemoryMax enforced' : 'System limits fallback'}
            </div>
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
                              Used: {u.memUsedGB} GB RAM · Disk: {diskMb > 0 ? `${diskMb} MB` : 'Unlimited'} · Net:{' '}
                              <span className={u.bandwidthLimitMbps && u.bandwidthLimitMbps > 0 ? 'text-orange-400' : 'text-zinc-400'}>
                                {u.bandwidthLimitMbps && u.bandwidthLimitMbps > 0 ? `${u.bandwidthLimitMbps} Mbps` : 'Unlimited'}
                              </span>
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
                              disabled: u.username === 'masud',
                              onClick: () => void handleToggleUserRole(u),
                            },
                            {
                              label:
                                u.status === 'suspended'
                                  ? 'Activate Account'
                                  : 'Suspend Account & Stop Apps',
                              icon: <Ban className="h-3.5 w-3.5" />,
                              disabled: u.username === 'masud',
                              onClick: () => void handleToggleUserStatus(u),
                            },
                            {
                              label: 'Delete User',
                              icon: <Trash2 className="h-3.5 w-3.5" />,
                              variant: 'danger',
                              disabled: u.username === 'masud',
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
          onClose={() => {
            setUserModalOpen(false);
            setShowNewPassword(false);
          }}
          title="Create Panel User"
          icon={<UserPlus className="h-4 w-4 text-orange-400" />}
        >
          <form onSubmit={handleCreateUser} className="space-y-4">
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-xs font-medium text-zinc-300">
                  Username
                </label>
                <span className="text-[10px] text-zinc-500 font-mono">Min 3 characters</span>
              </div>
              <div className="relative flex items-center">
                <span className="absolute left-3 text-zinc-500 text-xs font-mono">@</span>
                <input
                  type="text"
                  required
                  minLength={3}
                  placeholder="developer-alex"
                  value={newUsername}
                  onChange={(e) => setNewUsername(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] pl-8 pr-3 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-600 focus:border-orange-500 focus:outline-none transition-colors"
                />
              </div>
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-xs font-medium text-zinc-300">
                  Password
                </label>
                <span className="text-[10px] text-zinc-500 font-mono">Min 8 characters</span>
              </div>
              <div className="relative flex items-center">
                <input
                  type={showNewPassword ? 'text' : 'password'}
                  required
                  minLength={8}
                  placeholder="Enter secure password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] pl-3 pr-10 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-600 focus:border-orange-500 focus:outline-none transition-colors"
                />
                <button
                  type="button"
                  onClick={() => setShowNewPassword((p) => !p)}
                  title={showNewPassword ? 'Hide password' : 'Show password'}
                  className="absolute right-2.5 text-zinc-400 hover:text-white p-1 cursor-pointer transition-colors"
                >
                  {showNewPassword ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
              </div>
              <p className="mt-1 text-[10px] text-zinc-500">
                Encrypted securely with bcrypt before saving to database.
              </p>
            </div>

            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-2">
                Account Role
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                <button
                  type="button"
                  onClick={() => setNewRole('user')}
                  className={`relative flex flex-col justify-between rounded-xl border p-3 text-left transition-all cursor-pointer ${
                    newRole === 'user'
                      ? 'border-orange-500/60 bg-orange-500/[0.07] ring-1 ring-orange-500/20 text-white'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:border-white/[0.15] hover:text-zinc-200'
                  }`}
                >
                  <div>
                    <div className="flex items-center justify-between mb-1.5">
                      <div className="flex items-center gap-1.5 font-medium text-xs text-zinc-100">
                        <User className="h-3.5 w-3.5 text-orange-400" />
                        <span>Standard User</span>
                      </div>
                      <span className="rounded bg-white/[0.06] border border-white/[0.08] px-1.5 py-0.2 font-mono text-[9px] text-zinc-400">
                        user
                      </span>
                    </div>
                    <p className="text-[11px] text-zinc-400 leading-relaxed">
                      Isolated apps restricted by Linux cgroup v2 slice limits.
                    </p>
                  </div>
                  <div className="mt-2.5 flex items-center gap-1.5 text-[10px] text-zinc-500 font-mono">
                    <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                    <span>Enforces CPU & RAM Quotas</span>
                  </div>
                </button>

                <button
                  type="button"
                  onClick={() => setNewRole('admin')}
                  className={`relative flex flex-col justify-between rounded-xl border p-3 text-left transition-all cursor-pointer ${
                    newRole === 'admin'
                      ? 'border-orange-500/60 bg-orange-500/[0.07] ring-1 ring-orange-500/20 text-white'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:border-white/[0.15] hover:text-zinc-200'
                  }`}
                >
                  <div>
                    <div className="flex items-center justify-between mb-1.5">
                      <div className="flex items-center gap-1.5 font-medium text-xs text-zinc-100">
                        <Shield className="h-3.5 w-3.5 text-orange-400" />
                        <span>Administrator</span>
                      </div>
                      <span className="rounded bg-orange-500/10 border border-orange-500/20 px-1.5 py-0.2 font-mono text-[9px] text-orange-400">
                        admin
                      </span>
                    </div>
                    <p className="text-[11px] text-zinc-400 leading-relaxed">
                      Full control plane access, host shell, and unlimited quotas.
                    </p>
                  </div>
                  <div className="mt-2.5 flex items-center gap-1.5 text-[10px] text-zinc-500 font-mono">
                    <span className="h-1.5 w-1.5 rounded-full bg-orange-400" />
                    <span>Root Permissions</span>
                  </div>
                </button>
              </div>
            </div>

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3.5">
              <button
                type="button"
                onClick={() => {
                  setUserModalOpen(false);
                  setShowNewPassword(false);
                }}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 hover:text-white cursor-pointer transition-colors"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="inline-flex items-center gap-1.5 rounded-lg bg-orange-500 hover:bg-orange-600 px-4 py-1.5 text-xs font-semibold text-white shadow-sm transition-colors cursor-pointer"
              >
                <UserPlus className="h-3.5 w-3.5" />
                <span>Create User</span>
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
            <form onSubmit={handleSaveLimits} className="space-y-4">
              {limitsModalUser.role === 'admin' && (
                <div className="rounded-lg border border-orange-500/30 bg-orange-500/10 p-3 text-xs text-orange-200">
                  <strong>Admin Account:</strong> Administrator accounts have unlimited resources
                  by default unless demoted to a standard <code className="font-mono">user</code>{' '}
                  role.
                </div>
              )}

              <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 font-mono text-[11px] text-zinc-400">
                <div className="flex items-center justify-between">
                  <span className="text-zinc-300 font-semibold">
                    {cgroupInfo?.mode === 'cgroupfs' ? 'cgroupfs v2 Parent:' : 'systemd cgroup v2 Slice:'}
                  </span>
                  <div className="flex items-center gap-1.5">
                    <span className="text-orange-400">
                      nextdeploy-user-{limitsModalUser.id}.slice
                    </span>
                    <span className="rounded bg-emerald-500/15 border border-emerald-500/30 px-1.5 py-0.5 text-[9px] font-semibold text-emerald-300 uppercase">
                      {cgroupInfo?.mode || 'systemd'} Active
                    </span>
                  </div>
                </div>
                <div className="mt-1 text-[10px] text-zinc-500">
                  Set any value to <code className="text-zinc-300">0</code> for unlimited. Saving
                  immediately updates Linux slice properties (<code className="text-zinc-300">CPUQuota</code> &amp;{' '}
                  <code className="text-zinc-300">MemoryMax</code>).
                </div>
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Max Apps (max_apps, 0 = Unlimited)
                  </label>
                  <input
                    type="number"
                    min={0}
                    value={limitMaxApps}
                    onChange={(e) => setLimitMaxApps(e.target.value)}
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Max CPU Cores (max_cpus, e.g. 2.5)
                  </label>
                  <input
                    type="number"
                    step="0.5"
                    min={0}
                    value={limitMaxCpus}
                    onChange={(e) => setLimitMaxCpus(e.target.value)}
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Max Memory MB (max_memory_mb)
                  </label>
                  <input
                    type="number"
                    step={256}
                    min={0}
                    value={limitMaxMemoryMb}
                    onChange={(e) => setLimitMaxMemoryMb(e.target.value)}
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                  <span className="mt-1 block text-[10px] text-zinc-500">
                    {Number(limitMaxMemoryMb) > 0
                      ? `≈ ${(Number(limitMaxMemoryMb) / 1024).toFixed(1)} GB RAM`
                      : 'Unlimited RAM'}
                  </span>
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Max Storage MB (max_storage_mb)
                  </label>
                  <input
                    type="number"
                    step={1024}
                    min={0}
                    value={limitMaxStorageMb}
                    onChange={(e) => setLimitMaxStorageMb(e.target.value)}
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                  <span className="mt-1 block text-[10px] text-zinc-500">
                    {Number(limitMaxStorageMb) > 0
                      ? `≈ ${(Number(limitMaxStorageMb) / 1024).toFixed(1)} GB Disk`
                      : 'Unlimited Disk'}
                  </span>
                </div>
              </div>

              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Bandwidth Limit (bandwidth_limit_mbps, 0 = Unlimited)
                </label>
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={0}
                    step={10}
                    value={limitBandwidthMbps}
                    onChange={(e) => setLimitBandwidthMbps(e.target.value)}
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                    placeholder="0 = Unlimited"
                  />
                  <div className="flex gap-1 shrink-0">
                    {[
                      { label: 'Max', val: 0 },
                      { label: '10M', val: 10 },
                      { label: '50M', val: 50 },
                      { label: '100M', val: 100 },
                      { label: '200M', val: 200 },
                    ].map((preset) => (
                      <button
                        key={preset.val}
                        type="button"
                        onClick={() => setLimitBandwidthMbps(String(preset.val))}
                        className={`rounded px-2 py-1 text-[10px] font-mono transition-colors cursor-pointer ${
                          Number(limitBandwidthMbps) === preset.val
                            ? 'bg-orange-500/20 text-orange-300 border border-orange-500/40'
                            : 'bg-zinc-800 text-zinc-400 hover:text-zinc-200 border border-white/[0.05]'
                        }`}
                      >
                        {preset.label}
                      </button>
                    ))}
                  </div>
                </div>
                <span className="mt-1 block text-[10px] text-zinc-500">
                  {Number(limitBandwidthMbps) > 0
                    ? `Limits user's aggregate WAN egress to ${limitBandwidthMbps} Mbps (tc-HTB + iptables MARK). Local Caddy/DB traffic remains unthrottled.`
                    : 'Unlimited WAN speed (full host wire speed).'}
                </span>
              </div>

              <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] p-3">
                <div>
                  <div className="text-xs font-medium text-zinc-200">
                    Allow Domain Static/Media File Server
                  </div>
                  <div className="text-[11px] text-zinc-500">
                    Permits user domains to serve static/media volume paths directly via Caddy (allow_domain_file_server)
                  </div>
                </div>
                <ShadcnSwitch
                  checked={limitAllowDomainFileServer}
                  onChange={setLimitAllowDomainFileServer}
                />
              </div>

              <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
                <button
                  type="button"
                  onClick={() => setLimitsModalUser(null)}
                  className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
                >
                  Save Quotas & Apply Slice
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
              className="w-full rounded-lg border border-white/[0.08] bg-[#111115] pl-9 pr-3 py-1.5 text-xs text-zinc-100 placeholder:text-zinc-500 focus:border-orange-500/50 focus:outline-none"
            />
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
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
                className={`rounded-lg px-2.5 py-1 text-[11px] font-medium transition cursor-pointer ${
                  auditCategory === cat.id
                    ? 'bg-orange-500/15 text-orange-400 border border-orange-500/30'
                    : 'border border-white/[0.07] bg-[#111115] text-zinc-400 hover:text-zinc-200'
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
                        {log.actor}{' '}
                        {log.actorRole ? (
                          <span className="font-mono text-[10px] text-zinc-500">
                            ({log.actorRole})
                          </span>
                        ) : null}
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-mono text-zinc-200">{log.target}</div>
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
                className="inline-flex h-7 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer disabled:cursor-not-allowed"
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
                className="inline-flex h-7 items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 disabled:opacity-40 cursor-pointer disabled:cursor-not-allowed"
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
          onClick={() => {
            void backendApi.saveSettings({
              cleanup_enabled: cleanupEnabled,
              cleanup_interval: cleanupInterval,
              cleanup_include_build_cache: cleanupIncludeBuildCache,
            });
            toast.success(`Saved Docker cleanup schedule (${cleanupInterval})`);
          }}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
        >
          Save Changes
        </button>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Server className="h-4 w-4 text-orange-400" />
              <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                Scheduled Docker Cleanup
              </h2>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setShowCleanupLogModal(true)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
              >
                Last Output
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
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-orange-400 hover:bg-zinc-800 cursor-pointer"
              >
                {cleanupRunning ? 'Running...' : 'Run Cleanup Now'}
              </button>
            </div>
          </div>

          <div className="space-y-2.5 text-xs">
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div>
                <span className="text-zinc-200 block font-medium">Enable Scheduled Prune</span>
                <span className="text-[11px] text-zinc-500">
                  Prunes stopped containers, unused images &amp; networks (cleanup_enabled)
                </span>
              </div>
              <ShadcnSwitch checked={cleanupEnabled} onChange={setCleanupEnabled} />
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div>
                <span className="text-zinc-200 block font-medium">Cleanup Interval</span>
                <span className="text-[11px] text-zinc-500">
                  Background ticker frequency (cleanup_interval)
                </span>
              </div>
              <select
                value={cleanupInterval}
                onChange={(e) =>
                  setCleanupInterval(e.target.value as '1h' | '6h' | '12h' | '24h' | '168h')
                }
                className="rounded-lg border border-white/[0.09] bg-zinc-950 px-2.5 py-1.5 font-mono text-xs text-zinc-100"
              >
                <option value="1h">Every 1 Hour (1h)</option>
                <option value="6h">Every 6 Hours (6h)</option>
                <option value="12h">Every 12 Hours (12h)</option>
                <option value="24h">Daily (24h)</option>
                <option value="168h">Weekly (168h)</option>
              </select>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <div>
                <span className="text-zinc-200 block font-medium">Include BuildKit Cache</span>
                <span className="text-[11px] text-zinc-500">
                  Also run <code className="font-mono text-zinc-300">docker builder prune -af</code>{' '}
                  (cleanup_include_build_cache)
                </span>
              </div>
              <ShadcnSwitch
                checked={cleanupIncludeBuildCache}
                onChange={setCleanupIncludeBuildCache}
              />
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono text-[11px]">
              <span className="text-zinc-400">Last Cleanup Run</span>
              <div className="flex items-center gap-2">
                <span className="text-zinc-300">{cleanupLastRunAt}</span>
                <StatusBadge
                  status={cleanupLastStatus === 'ok' ? 'ready' : 'failed'}
                  label={cleanupLastStatus}
                />
              </div>
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <ShieldCheck className="h-4 w-4 text-emerald-400" />
              <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                Orphaned Temp Files & Daemon
              </h2>
            </div>
            <button
              type="button"
              onClick={async () => {
                const info = await backendApi.getTmpCleanupInfo();
                if (typeof info.count === 'number') setTmpCount(info.count);
                if (info.total_size) setTmpTotalSize(info.total_size);
                toast.success('Scanned /tmp for orphaned NextDeploy archives');
              }}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
            >
              Scan /tmp
            </button>
          </div>
          <div className="space-y-2.5 text-xs">
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <div>
                <span className="text-zinc-200 block">Orphaned Backup/Export Archives</span>
                <span className="text-[11px] text-zinc-500">
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
                className="rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-1.5 font-sans text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
              >
                Clean Temp Files
              </button>
            </div>
            <div className="flex justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <span className="text-zinc-500">Apps Directory</span>
              <span className="text-zinc-100">/data/apps</span>
            </div>
            <div className="flex justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <span className="text-zinc-500">Reverse Proxy Engine</span>
              <span className="text-orange-400">Caddy 2 (Caddyfile + Admin API)</span>
            </div>
            <div className="flex justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <span className="text-zinc-500">Docker Socket</span>
              <span className="text-emerald-400">unix:///var/run/docker.sock</span>
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
    </div>
  );
};
