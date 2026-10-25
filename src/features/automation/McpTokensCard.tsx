import React, { useState } from 'react';
import { KeyRound, Plus, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { CopyField, EmptyState, GhostButton, Pill, PrimaryButton } from '../php/ui';

export interface McpTokenRow {
  id: number;
  name: string;
  token_prefix: string;
  kind: string;
  allow_env_reveal: boolean;
  allow_server_exec: boolean;
  allow_container_exec: boolean;
  allow_app_delete: boolean;
  created_at?: string;
  last_used_at?: string | null;
}

type ScopeKey = 'allow_env_reveal' | 'allow_container_exec' | 'allow_server_exec' | 'allow_app_delete';

const SCOPES: Array<{ key: ScopeKey; label: string; text: string; adminOnly?: boolean }> = [
  { key: 'allow_env_reveal', label: 'Read secrets', text: 'Show environment variable values.' },
  { key: 'allow_container_exec', label: 'Run in containers', text: 'Run commands inside your containers.' },
  { key: 'allow_app_delete', label: 'Delete projects', text: 'Remove a project and its data.' },
  { key: 'allow_server_exec', label: 'Run on the server', text: 'Run commands on the host. Administrators only.', adminOnly: true },
];

const when = (iso?: string | null) => {
  if (!iso) return 'never';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? 'never' : d.toLocaleString();
};

/** The signed-in user's own MCP tokens. A token acts with exactly its owner's permissions. */
export const McpTokensCard: React.FC<{
  tokens: McpTokenRow[];
  isAdmin: boolean;
  mcpUrl: string;
  onChanged: () => Promise<void> | void;
}> = ({ tokens, isAdmin, mcpUrl, onChanged }) => {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<Record<ScopeKey, boolean>>({
    allow_env_reveal: false,
    allow_container_exec: false,
    allow_server_exec: false,
    allow_app_delete: false,
  });
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState('');

  const close = () => {
    setOpen(false);
    setCreated('');
    setName('');
    setScopes({ allow_env_reveal: false, allow_container_exec: false, allow_server_exec: false, allow_app_delete: false });
  };

  const create = async () => {
    setBusy(true);
    const res = await backendApi.createApiToken({ name: name.trim() || 'MCP token', kind: 'mcp', ...scopes });
    setBusy(false);
    if (!res.ok || !res.data?.token) {
      toast.error(res.error || 'Could not create the token');
      return;
    }
    setCreated(res.data.token);
    await onChanged();
  };

  const toggle = async (t: McpTokenRow, key: ScopeKey) => {
    const res = await backendApi.toggleApiTokenPerm(t.id, key, 'mcp');
    if (!res.ok) toast.error(res.error || 'Could not change this permission');
    await onChanged();
  };

  const revoke = async (t: McpTokenRow) => {
    if (!window.confirm(`Revoke "${t.name}"? Anything using it stops working.`)) return;
    const res = await backendApi.deleteMcpToken(t.id);
    if (!res.ok) toast.error(res.error || 'Could not revoke the token');
    else toast.success('Token revoked');
    await onChanged();
  };

  const mine = tokens.filter((t) => t.kind === 'mcp');

  return (
    <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/[0.06] px-4 py-3">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-zinc-100">Your tokens</h2>
          <Pill>{mine.length}</Pill>
        </div>
        <PrimaryButton onClick={() => setOpen(true)}>
          <Plus className="h-3.5 w-3.5" /> Create token
        </PrimaryButton>
      </div>

      <div className="p-4">
        {mine.length === 0 ? (
          <EmptyState icon={<KeyRound className="h-6 w-6" />} text="No tokens yet. Create one to connect Cursor or Claude." />
        ) : (
          <ul className="divide-y divide-white/[0.05]">
            {mine.map((t) => (
              <li key={t.id} className="flex flex-wrap items-center justify-between gap-3 py-3 first:pt-0 last:pb-0">
                <div className="min-w-0">
                  <div className="text-xs font-medium text-zinc-100">{t.name}</div>
                  <div className="mt-0.5 font-mono text-[11px] text-zinc-500">
                    {t.token_prefix}… · used {when(t.last_used_at)}
                  </div>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">
                  {SCOPES.filter((s) => !s.adminOnly || isAdmin).map((s) => {
                    const on = Boolean(t[s.key]);
                    return (
                      <button
                        key={s.key}
                        type="button"
                        title={s.text}
                        onClick={() => void toggle(t, s.key)}
                        className={`cursor-pointer rounded-md border px-2 py-0.5 text-[11px] transition-colors ${
                          on
                            ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                            : 'border-white/[0.08] bg-zinc-900 text-zinc-500 hover:text-zinc-300'
                        }`}
                      >
                        {s.label}
                      </button>
                    );
                  })}
                  <GhostButton tone="danger" title="Revoke" onClick={() => void revoke(t)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </GhostButton>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      <ShadcnDialog
        open={open}
        onClose={() => !busy && close()}
        title={created ? 'Token created' : 'Create token'}
        icon={<KeyRound className="h-4 w-4" />}
        maxWidth="lg"
        footer={
          created ? (
            <PrimaryButton onClick={close}>Done</PrimaryButton>
          ) : (
            <>
              <GhostButton onClick={close} disabled={busy}>Cancel</GhostButton>
              <PrimaryButton busy={busy} onClick={create}>Create</PrimaryButton>
            </>
          )
        }
      >
        {created ? (
          <div className="space-y-4">
            <p className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
              Copy it now. It is shown only once.
            </p>
            <CopyField label="Token" value={created} secret />
            <CopyField label="Endpoint" value={mcpUrl} />
          </div>
        ) : (
          <div className="space-y-4">
            <label className="block">
              <span className="mb-1.5 block text-[11px] font-medium text-zinc-300">Name</span>
              <input
                className="h-8 w-full rounded-lg border border-white/[0.08] bg-[#09090b] px-2.5 text-xs text-zinc-100 outline-none focus:border-orange-500/50"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Cursor on my laptop"
                autoFocus
              />
            </label>
            <div>
              <div className="mb-1.5 text-[11px] font-medium text-zinc-300">Allowed to</div>
              <ul className="divide-y divide-white/[0.05] rounded-lg border border-white/[0.08]">
                {SCOPES.filter((s) => !s.adminOnly || isAdmin).map((s) => (
                  <li key={s.key} className="flex items-center justify-between gap-3 px-3 py-2">
                    <div className="min-w-0">
                      <div className="text-xs text-zinc-100">{s.label}</div>
                      <div className="text-[11px] text-zinc-500">{s.text}</div>
                    </div>
                    <input
                      type="checkbox"
                      aria-label={s.label}
                      checked={scopes[s.key]}
                      onChange={(e) => setScopes({ ...scopes, [s.key]: e.target.checked })}
                      className="h-4 w-4 accent-orange-500"
                    />
                  </li>
                ))}
              </ul>
            </div>
            <p className="text-[11px] text-zinc-500">A token can only do what you can do: your projects, your limits.</p>
          </div>
        )}
      </ShadcnDialog>
    </div>
  );
};
