import React, { useState } from 'react';
import {
  Copy,
  Trash2,
  Plus,
  KeyRound,
  Eye,
  EyeOff,
  Terminal,
  ShieldCheck,
  UserCheck,
  ChevronDown,
  ChevronRight,
} from 'lucide-react';
import { toast } from 'sonner';
import { CliSessionItem } from '../../types/nextdeploy';
import {
  ActionMenu,
  CopyButton,
  ShadcnDialog,
} from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';

interface CliSessionsViewProps {
  isAdmin?: boolean;
  cliSessions: CliSessionItem[];
  onGenerateCliToken: (name: string) => void;
  onRevokeCliSession?: (session: CliSessionItem) => void;
}

const USER_CLI_COMMANDS = [
  { cmd: 'nd login <panel_url>', desc: 'Authenticate & store account role/token' },
  { cmd: 'nd whoami / nd quota', desc: 'Inspect role, token scopes & resource limits' },
  { cmd: 'nd apps / nd create <name>', desc: 'List or provision owned applications' },
  { cmd: 'nd push --deploy --prune', desc: 'Delta sync local files & trigger deploy' },
  { cmd: 'nd logs -f / nd shell', desc: 'Stream logs or open container PTY terminal' },
  { cmd: 'nd env list|get|set|unset', desc: 'Manage environment variables safely' },
  { cmd: 'nd domain list|add|rm', desc: 'Attach and manage custom app domains' },
  { cmd: 'nd php ... / nd db ...', desc: 'Manage PHP sites, routing, MariaDB & crons' },
  { cmd: 'nd proxy list|add|enable|rm', desc: 'Manage your personal reverse proxy routes' },
  { cmd: 'nd volume ... / nd backup ...', desc: 'Manage volumes & backups for your apps' },
];

const ADMIN_CLI_COMMANDS = [
  { cmd: 'nd containers / nd images', desc: 'Inspect all host containers & Docker images' },
  { cmd: 'nd server-exec / nd server-shell', desc: 'Run host shell commands or PTY session [admin]' },
  { cmd: 'nd clean / nd volume prune', desc: 'Prune global build cache, images & volumes [admin]' },
  { cmd: 'nd proxy reload|logs|status|options|engine', desc: 'Switch or configure proxy engine (caddy/angie/ferron)' },
  { cmd: 'nd shield add|update|rm|reassign', desc: 'Manage Edge Shield nodes & tokens [admin]' },
  { cmd: 'nd edge <subcommand>', desc: 'Manage multi-panel Edge Shield daemon [admin]' },
  { cmd: 'nd users [list]', desc: 'Inspect panel users, roles & quota allocations [admin]' },
  { cmd: 'nd audit -n 50', desc: 'Review global security & deployment audit logs' },
];

export const CliSessionsView: React.FC<CliSessionsViewProps> = ({
  isAdmin = false,
  cliSessions,
  onGenerateCliToken,
  onRevokeCliSession,
}) => {
  const [tokenModalOpen, setTokenModalOpen] = useState(false);
  const [installCliOpen, setInstallCliOpen] = useState(false);
  const [tokenLabel, setTokenLabel] = useState('');
  const [revealedCliTokens, setRevealedCliTokens] = useState<Record<string, boolean>>({});
  const [refOpen, setRefOpen] = useState(false);
  const [refRoleTab, setRefRoleTab] = useState<'user' | 'admin'>(isAdmin ? 'admin' : 'user');
  const [tokenPerms, setTokenPerms] = useState<
    Record<
      string,
      {
        allow_env_reveal: boolean;
        allow_container_exec: boolean;
        allow_server_exec: boolean;
        allow_app_delete: boolean;
      }
    >
  >({});

  const visibleScopes = isAdmin
    ? ([
        { key: 'allow_env_reveal', label: 'env:reveal' },
        { key: 'allow_container_exec', label: 'container:exec' },
        { key: 'allow_server_exec', label: 'host:exec' },
        { key: 'allow_app_delete', label: 'app:delete' },
      ] as const)
    : ([
        { key: 'allow_env_reveal', label: 'env:reveal' },
        { key: 'allow_container_exec', label: 'container:exec' },
        { key: 'allow_app_delete', label: 'app:delete' },
      ] as const);

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 shrink-0">
            <KeyRound className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">CLI Sessions</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {cliSessions.length}
            </span>
          </div>
        </div>

        <div className="grid grid-cols-2 sm:flex sm:items-center gap-2 w-full sm:w-auto">
          <button
            type="button"
            onClick={() => setInstallCliOpen(true)}
            className="inline-flex h-9 sm:h-8 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer transition-colors"
          >
            <Terminal className="h-3.5 w-3.5 text-orange-400 shrink-0" />
            <span>Install CLI</span>
          </button>
          <button
            type="button"
            onClick={() => setTokenModalOpen(true)}
            className="inline-flex h-9 sm:h-8 items-center justify-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer shadow-sm transition-colors"
          >
            <Plus className="h-3.5 w-3.5 shrink-0" />
            <span>Generate Token</span>
          </button>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[680px] table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[30%] px-4 py-2.5 font-medium">Workstation & CLI Token</th>
                <th className="w-[20%] px-4 py-2.5 font-medium">Host & OS</th>
                <th className="w-[28%] px-4 py-2.5 font-medium">Token Scopes (Click to Toggle)</th>
                <th className="w-[12%] px-4 py-2.5 font-medium text-right">Active</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {cliSessions.map((s) => {
                const fullTokenValue = s.fullToken || s.prefix;
                const isRevealed = Boolean(revealedCliTokens[s.id]);
                const p = tokenPerms[s.id] || s.permissions || {
                  allow_env_reveal: false,
                  allow_container_exec: false,
                  allow_server_exec: false,
                  allow_app_delete: false,
                };
                const targetTokId = s.tokenId || s.id;
                const toggleScope = (
                  permKey:
                    | 'allow_env_reveal'
                    | 'allow_container_exec'
                    | 'allow_server_exec'
                    | 'allow_app_delete',
                  label: string
                ) => {
                  const nextVal = !p[permKey];
                  setTokenPerms((prev) => ({
                    ...prev,
                    [s.id]: { ...p, [permKey]: nextVal },
                  }));
                  void backendApi.toggleApiTokenPerm(targetTokId, permKey);
                  toast.success(`${nextVal ? 'Granted' : 'Revoked'} ${label} for ${s.tokenName}`);
                };

                return (
                  <tr key={s.id} className="hover:bg-white/[0.03]">
                    <td className="px-4 py-3">
                      <div className="font-semibold text-zinc-100 truncate">{s.tokenName}</div>
                      <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-zinc-400">
                        <span className="truncate text-orange-400">
                          {isRevealed ? fullTokenValue : s.prefix}
                        </span>
                        <button
                          type="button"
                          onClick={() =>
                            setRevealedCliTokens((prev) => ({ ...prev, [s.id]: !prev[s.id] }))
                          }
                          title={isRevealed ? 'Hide token' : 'Reveal full CLI token'}
                          className="rounded p-0.5 text-zinc-500 hover:text-zinc-200 cursor-pointer"
                        >
                          {isRevealed ? (
                            <EyeOff className="h-3 w-3" />
                          ) : (
                            <Eye className="h-3 w-3" />
                          )}
                        </button>
                        <CopyButton
                          value={fullTokenValue}
                          title="Copy full CLI token"
                          toastMessage={`Copied full CLI token for ${s.tokenName}`}
                          className="rounded p-0.5 text-zinc-500 hover:text-zinc-200 cursor-pointer inline-flex items-center"
                          iconClassName="h-3 w-3"
                        />
                        <span className="text-zinc-600">·</span>
                        <span className="text-zinc-500">{s.ipAddress}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-zinc-300 truncate">
                      <div>{s.deviceHost}</div>
                      <div className="text-[10px] text-zinc-500">
                        {s.osArch} · {s.cliVersion}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1 font-mono text-[10px]">
                        {visibleScopes.map((sc) => {
                          const active = p[sc.key];
                          return (
                            <button
                              key={sc.key}
                              type="button"
                              onClick={() => toggleScope(sc.key, sc.label)}
                              className={`rounded border px-1.5 py-0.5 transition-colors cursor-pointer ${
                                active
                                  ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                                  : 'border-white/[0.07] bg-zinc-900 text-zinc-500 hover:text-zinc-300'
                              }`}
                            >
                              {sc.label}
                            </button>
                          );
                        })}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-zinc-400">{s.lastActiveAt}</td>
                    <td className="px-3 py-3 text-right">
                      <ActionMenu
                        items={[
                          {
                            label: 'Copy Full Token',
                            icon: <Copy className="h-3.5 w-3.5" />,
                            onClick: async () => {
                              const ok = await copyToClipboard(fullTokenValue);
                              if (ok) {
                                toast.success(`Copied full token for ${s.tokenName}`);
                              } else {
                                toast.error('Failed to copy token');
                              }
                            },
                          },
                          {
                            label: 'Revoke Token',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger',
                            onClick: () => {
                              if (onRevokeCliSession) onRevokeCliSession(s);
                            },
                          },
                        ]}
                      />
                    </td>
                  </tr>
                );
              })}
              {cliSessions.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-xs text-zinc-500">
                    No CLI tokens yet. Click "Generate Token" to authenticate <code className="text-zinc-300">nd login</code>.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Role-separated CLI command reference (collapsed dropdown by default) */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
        <button
          type="button"
          onClick={() => setRefOpen((prev) => !prev)}
          aria-expanded={refOpen}
          aria-controls="cli-command-reference-panel"
          className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left hover:bg-white/[0.02] cursor-pointer transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-zinc-500"
        >
          <div className="flex items-center gap-2.5 min-w-0">
            <Terminal className="h-3.5 w-3.5 text-orange-400 shrink-0" />
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <h2 className="text-xs font-semibold text-zinc-100">CLI Command Reference (nd)</h2>
                <span className="rounded bg-white/[0.06] px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">
                  {isAdmin ? `${USER_CLI_COMMANDS.length} user / ${ADMIN_CLI_COMMANDS.length} admin` : `${USER_CLI_COMMANDS.length} user`}
                </span>
              </div>
              <p className="text-[11px] text-zinc-500 truncate">
                Commands are separated by role. Run <code className="text-zinc-300">nd help user</code> or <code className="text-zinc-300">nd help admin</code> locally.
              </p>
            </div>
          </div>
          <span className="inline-flex items-center gap-1 text-[11px] text-zinc-400 shrink-0">
            <span>{refOpen ? 'Hide' : 'Show'}</span>
            {refOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </span>
        </button>

        {refOpen && (
          <div id="cli-command-reference-panel" className="border-t border-white/[0.07] p-4 space-y-3">
            {isAdmin && (
              <div className="inline-flex rounded-lg border border-white/[0.08] bg-zinc-950 p-0.5 text-[11px]">
                <button
                  type="button"
                  onClick={() => setRefRoleTab('user')}
                  className={`inline-flex items-center gap-1 rounded-md px-2.5 py-1 font-medium cursor-pointer transition-colors ${
                    refRoleTab === 'user' ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  <UserCheck className="h-3 w-3 text-cyan-400" />
                  User Commands
                </button>
                <button
                  type="button"
                  onClick={() => setRefRoleTab('admin')}
                  className={`inline-flex items-center gap-1 rounded-md px-2.5 py-1 font-medium cursor-pointer transition-colors ${
                    refRoleTab === 'admin' ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  <ShieldCheck className="h-3 w-3 text-amber-400" />
                  Admin Commands
                </button>
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
              {(isAdmin && refRoleTab === 'admin' ? ADMIN_CLI_COMMANDS : USER_CLI_COMMANDS).map((item) => (
                <div
                  key={item.cmd}
                  className="flex flex-col justify-between rounded-lg border border-white/[0.06] bg-[#09090b]/80 px-3 py-2"
                >
                  <code className="font-mono text-[11px] text-orange-300">{item.cmd}</code>
                  <span className="mt-0.5 text-[11px] text-zinc-400">{item.desc}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      <ShadcnDialog
        open={installCliOpen}
        onClose={() => setInstallCliOpen(false)}
        title="Install NextDeploy CLI (nd)"
        icon={<Terminal className="h-4 w-4" />}
      >
        <div className="space-y-3 font-mono text-xs">
          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 flex items-center justify-between">
            <span className="text-zinc-200 truncate">
              curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install-nd.sh | sh
            </span>
            <CopyButton
              value="curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install-nd.sh | sh"
              label="Copy"
              toastMessage="Copied install script"
              className="text-orange-400 hover:underline cursor-pointer shrink-0 ml-2 inline-flex items-center gap-1 font-mono text-xs"
            />
          </div>
          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 space-y-1 text-[11px] text-zinc-400">
            <div>$ nd login {typeof window !== 'undefined' ? window.location.origin : 'https://panel.yourdomain.com'}</div>
            <div>$ nd whoami</div>
            <div>$ nd link my-app</div>
            <div>$ nd push --deploy</div>
            <div>$ nd logs -f</div>
          </div>
        </div>
      </ShadcnDialog>

      <ShadcnDialog
        open={tokenModalOpen}
        onClose={() => setTokenModalOpen(false)}
        title="Generate CLI Token"
        icon={<KeyRound className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!tokenLabel.trim()) return;
            onGenerateCliToken(tokenLabel.trim());
            setTokenLabel('');
            setTokenModalOpen(false);
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Workstation or CI Label
            </label>
            <input
              type="text"
              required
              value={tokenLabel}
              onChange={(e) => setTokenLabel(e.target.value)}
              placeholder="MacBook-Pro-M4 or GitLab-Runner"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setTokenModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Generate Token
            </button>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};
