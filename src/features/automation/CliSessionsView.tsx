import React, { useState } from 'react';
import {
  Copy,
  Trash2,
  Plus,
  KeyRound,
  Eye,
  EyeOff,
  Terminal,
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
  cliSessions: CliSessionItem[];
  onGenerateCliToken: (name: string) => void;
  onRevokeCliSession?: (id: string) => void;
}

export const CliSessionsView: React.FC<CliSessionsViewProps> = ({
  cliSessions,
  onGenerateCliToken,
  onRevokeCliSession,
}) => {
  const [tokenModalOpen, setTokenModalOpen] = useState(false);
  const [installCliOpen, setInstallCliOpen] = useState(false);
  const [tokenLabel, setTokenLabel] = useState('');
  const [revealedCliTokens, setRevealedCliTokens] = useState<Record<string, boolean>>({});
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
                const p = tokenPerms[s.id] || {
                  allow_env_reveal: true,
                  allow_container_exec: true,
                  allow_server_exec: false,
                  allow_app_delete: false,
                };
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
                  void backendApi.toggleApiTokenPerm(s.id, permKey);
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
                        {(
                          [
                            { key: 'allow_env_reveal', label: 'env:reveal' },
                            { key: 'allow_container_exec', label: 'container:exec' },
                            { key: 'allow_server_exec', label: 'host:exec' },
                            { key: 'allow_app_delete', label: 'app:delete' },
                          ] as const
                        ).map((sc) => {
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
                            label: 'Revoke Session',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger',
                            onClick: () => {
                              if (onRevokeCliSession) onRevokeCliSession(s.id);
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
