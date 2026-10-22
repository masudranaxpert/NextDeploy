import React, { useState } from 'react';
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
}

export const UsersAndSettingsView: React.FC<UsersAndSettingsViewProps> = ({
  mode,
  users: initialUsers,
  auditLogs,
}) => {
  const [users, setUsers] = useState<PanelUser[]>(initialUsers);
  const [userModalOpen, setUserModalOpen] = useState(false);
  const [newUsername, setNewUsername] = useState('');
  const [newEmail, setNewEmail] = useState('');
  const [newMaxApps, setNewMaxApps] = useState('10');
  const [newMemQuota, setNewMemQuota] = useState('16');
  const [allowDomainFileServer, setAllowDomainFileServer] = useState(true);

  const [cleanupEnabled, setCleanupEnabled] = useState(true);
  const [cleanupIntervalHours, setCleanupIntervalHours] = useState('24');
  const [cleanupIncludeImages, setCleanupIncludeImages] = useState(true);
  const [cleanupIncludeVolumes, setCleanupIncludeVolumes] = useState(false);
  const [tmpCount, setTmpCount] = useState(4);
  const [tmpTotalSize, setTmpTotalSize] = useState('128.4 MB');

  if (mode === 'users') {
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between">
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

        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <table className="w-full table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[32%] px-4 py-2.5 font-medium">User</th>
                <th className="w-[16%] px-4 py-2.5 font-medium">Role & 2FA</th>
                <th className="w-[22%] px-4 py-2.5 font-medium">Projects Quota</th>
                <th className="w-[20%] px-4 py-2.5 font-medium text-right">Memory Quota</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {users.map((u) => (
                <tr key={u.id} className="hover:bg-white/[0.03]">
                  <td className="px-4 py-3">
                    <div className="font-semibold text-zinc-100">{u.username}</div>
                    <div className="font-mono text-[11px] text-zinc-500">{u.email}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-1.5">
                      <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[10px] text-orange-400 uppercase">
                        {u.role}
                      </span>
                      {u.twoFactorEnabled && (
                        <span className="rounded-md bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] text-emerald-400">
                          2FA
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 font-mono text-zinc-300">
                    {u.appsCount} / {u.maxApps} projects
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-zinc-300">
                    {u.memUsedGB} / {u.memQuotaGB} GB
                  </td>
                  <td className="px-3 py-3 text-right">
                    <ActionMenu
                      items={[
                        {
                          label: 'Toggle Domain File Server Quota',
                          icon: <Globe className="h-3.5 w-3.5" />,
                          onClick: () => {
                            void backendApi.updateUserLimits(u.id, {
                              max_apps: u.maxApps,
                              max_ram_mb: u.memQuotaGB * 1024,
                              max_disk_mb: u.diskQuotaGB * 1024,
                              allow_domain_file_server: true,
                            });
                            toast.success(
                              `Updated domain static/media file server quota for ${u.username}`
                            );
                          },
                        },
                        {
                          label: 'Reset 2FA / Password',
                          icon: <KeyRound className="h-3.5 w-3.5" />,
                          onClick: () => toast.success(`Sent reset link to ${u.email}`),
                        },
                        {
                          label: 'Remove User',
                          icon: <Trash2 className="h-3.5 w-3.5" />,
                          variant: 'danger',
                          disabled: u.username === 'masud',
                          onClick: () => {
                            setUsers((prev) => prev.filter((item) => item.id !== u.id));
                            toast.success(`Removed ${u.username}`);
                          },
                        },
                      ]}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <ShadcnDialog
          open={userModalOpen}
          onClose={() => setUserModalOpen(false)}
          title="Create Panel User"
          icon={<Users className="h-4 w-4" />}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (!newUsername.trim()) return;
              setUsers((prev) => [
                ...prev,
                {
                  id: `u-${Date.now()}`,
                  username: newUsername.trim(),
                  email: newEmail.trim() || `${newUsername}@nextdeploy.sh`,
                  role: 'user',
                  appsCount: 0,
                  maxApps: Number(newMaxApps) || 10,
                  cpuQuotaCores: 4,
                  memUsedGB: 0,
                  memQuotaGB: Number(newMemQuota) || 16,
                  diskUsedGB: 0,
                  diskQuotaGB: 50,
                  twoFactorEnabled: false,
                  lastLoginAt: 'Never',
                },
              ]);
              setNewUsername('');
              setNewEmail('');
              setUserModalOpen(false);
              toast.success('Created user');
            }}
            className="space-y-4"
          >
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Username
                </label>
                <input
                  type="text"
                  required
                  value={newUsername}
                  onChange={(e) => setNewUsername(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
                />
              </div>
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Email</label>
                <input
                  type="email"
                  required
                  value={newEmail}
                  onChange={(e) => setNewEmail(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Max Projects Quota
                </label>
                <input
                  type="number"
                  value={newMaxApps}
                  onChange={(e) => setNewMaxApps(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  RAM Quota (GB)
                </label>
                <input
                  type="number"
                  value={newMemQuota}
                  onChange={(e) => setNewMemQuota(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
            </div>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] p-3">
              <span className="text-xs text-zinc-200">
                Allow Domain Static/Media File Server (allow_domain_file_server)
              </span>
              <ShadcnSwitch
                checked={allowDomainFileServer}
                onChange={setAllowDomainFileServer}
              />
            </div>
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
      </div>
    );
  }

  if (mode === 'audit-logs') {
    return (
      <div className="space-y-4">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <FileText className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">Audit Trail</h1>
        </div>

        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <table className="w-full table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[22%] px-4 py-2.5 font-medium">Action</th>
                <th className="w-[18%] px-4 py-2.5 font-medium">Actor</th>
                <th className="w-[34%] px-4 py-2.5 font-medium">Target</th>
                <th className="w-[12%] px-4 py-2.5 font-medium">Status</th>
                <th className="w-[14%] px-4 py-2.5 font-medium text-right">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {auditLogs.map((log) => (
                <tr key={log.id} className="hover:bg-white/[0.03]">
                  <td className="px-4 py-3 font-mono font-semibold text-orange-400 truncate">
                    {log.action}
                  </td>
                  <td className="px-4 py-3 text-zinc-200 truncate">
                    {log.actor}{' '}
                    <span className="font-mono text-[10px] text-zinc-500">({log.actorRole})</span>
                  </td>
                  <td className="px-4 py-3 font-mono text-zinc-400 truncate">{log.target}</td>
                  <td className="px-4 py-3">
                    <StatusBadge
                      status={log.status === 'success' ? 'ready' : 'failed'}
                      label={log.status}
                    />
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-[11px] text-zinc-500">
                    {log.timestamp.slice(11)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Settings className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">System Settings</h1>
        </div>

        <button
          type="button"
          onClick={() => {
            void backendApi.saveSettings({
              cleanup_enabled: cleanupEnabled,
              cleanup_interval_hours: Number(cleanupIntervalHours) || 24,
              cleanup_include_images: cleanupIncludeImages,
              cleanup_include_volumes: cleanupIncludeVolumes,
            });
            toast.success('Saved control plane & scheduled cleanup settings');
          }}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
        >
          Save Changes
        </button>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Server className="h-4 w-4 text-orange-400" />
              <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                Scheduled Docker Cleanup
              </h2>
            </div>
            <button
              type="button"
              onClick={async () => {
                await backendApi.runDockerCleanup();
                toast.success('Triggered immediate Docker system & image prune');
              }}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-orange-400 hover:bg-zinc-800 cursor-pointer"
            >
              Run Cleanup Now
            </button>
          </div>
          <div className="space-y-2.5 text-xs">
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-200">Enable Scheduled Prune</span>
              <ShadcnSwitch checked={cleanupEnabled} onChange={setCleanupEnabled} />
            </div>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-200">Cleanup Interval (Hours)</span>
              <input
                type="number"
                value={cleanupIntervalHours}
                onChange={(e) => setCleanupIntervalHours(e.target.value)}
                className="w-20 rounded border border-white/[0.09] bg-zinc-950 px-2 py-1 text-right font-mono text-xs text-zinc-100"
              />
            </div>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-200">Prune Unused Images</span>
              <ShadcnSwitch checked={cleanupIncludeImages} onChange={setCleanupIncludeImages} />
            </div>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-200">Prune Unused Volumes</span>
              <ShadcnSwitch checked={cleanupIncludeVolumes} onChange={setCleanupIncludeVolumes} />
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
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono">
              <div>
                <span className="text-zinc-200 block">Orphaned Backup/Export Archives</span>
                <span className="text-[11px] text-zinc-500">
                  {tmpCount} item(s) · {tmpTotalSize}
                </span>
              </div>
              <button
                type="button"
                onClick={async () => {
                  await backendApi.runTmpCleanup();
                  setTmpCount(0);
                  setTmpTotalSize('0 B');
                  toast.success('Cleaned orphaned temporary archives');
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
              <span className="text-zinc-500">Docker Socket</span>
              <span className="text-emerald-400">unix:///var/run/docker.sock</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
