import React, { useEffect, useMemo, useState } from 'react';
import {
  CheckIcon,
  CopyIcon,
  CpuIcon,
  ExternalLinkIcon,
  EyeIcon,
  EyeOffIcon,
  KeyRoundIcon,
  Loader2Icon,
  PlusIcon,
  SearchIcon,
  ShieldCheckIcon,
  Trash2Icon,
  XIcon,
} from 'lucide-react';
import { toast } from 'sonner';

import { requestJson } from '@/lib/api/http';
import { cn } from '@/lib/utils';
import type { CopilotSecret, LoginProvider, OAuthPrompt, ProvidersConfig } from './types';

interface ProviderAuthModalProps {
  isOpen: boolean;
  onClose: () => void;
  providers: ProvidersConfig | null;
  loginProviders: LoginProvider[];
  oauthPrompt: OAuthPrompt | null;
  onStartOAuth: (providerId: string) => void;
  onSubmitOAuthInput: (id: string, value: string) => Promise<boolean>;
  onRemoveProvider: (providerId: string) => Promise<void>;
  initialTab?: 'providers' | 'secrets';
}

const btn =
  'inline-flex h-10 items-center justify-center gap-1.5 rounded-[4px] px-4 text-[13px] md:h-9 md:px-3.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:pointer-events-none disabled:opacity-50';

export const ProviderAuthModal: React.FC<ProviderAuthModalProps> = ({
  isOpen,
  onClose,
  providers,
  loginProviders,
  oauthPrompt,
  onStartOAuth,
  onSubmitOAuthInput,
  onRemoveProvider,
  initialTab = 'providers',
}) => {
  const [activeTab, setActiveTab] = useState<'providers' | 'secrets'>(initialTab);
  const [query, setQuery] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [code, setCode] = useState('');

  // Secrets management state
  const [secrets, setSecrets] = useState<CopilotSecret[]>([]);
  const [loadingSecrets, setLoadingSecrets] = useState(false);
  const [newKey, setNewKey] = useState('');
  const [newVal, setNewVal] = useState('');
  const [showNewVal, setShowNewVal] = useState(false);
  const [revealedSecrets, setRevealedSecrets] = useState<Record<string, boolean>>({});
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  useEffect(() => {
    if (oauthPrompt) setBusy(null);
  }, [oauthPrompt]);

  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isOpen, onClose]);

  // Load secrets when secrets tab is activated
  useEffect(() => {
    if (!isOpen || activeTab !== 'secrets') return;
    let mounted = true;
    setLoadingSecrets(true);
    requestJson<{ secrets: CopilotSecret[] }>('/api/v1/copilot/secrets')
      .then((res) => {
        if (!mounted) return;
        if (res.ok && res.data?.secrets) {
          setSecrets(res.data.secrets);
        }
      })
      .catch(() => {})
      .finally(() => {
        if (mounted) setLoadingSecrets(false);
      });
    return () => {
      mounted = false;
    };
  }, [isOpen, activeTab]);

  const status = (id: string) =>
    (providers as any)?.[id] as { configured?: boolean; maskedKey?: string; host?: string } | undefined;
  const isConnected = (p: LoginProvider) => p.authenticated || !!status(p.id)?.configured;

  const { connected, available } = useMemo(() => {
    const q = query.trim().toLowerCase();
    const matches = (p: LoginProvider) => !q || p.name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q);
    const all = [...loginProviders].sort((a, b) => a.name.localeCompare(b.name));
    return {
      connected: all.filter(isConnected),
      available: all.filter((p) => !isConnected(p) && matches(p)),
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loginProviders, providers, query]);

  if (!isOpen) return null;

  const submitCode = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!oauthPrompt || !code.trim()) return;
    setBusy('code');
    await onSubmitOAuthInput(oauthPrompt.id, code.trim());
    setBusy(null);
    setCode('');
  };

  const handleAddSecret = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanKey = newKey.trim();
    const cleanVal = newVal.trim();
    if (!cleanKey) {
      toast.error('Secret name is required');
      return;
    }
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(cleanKey)) {
      toast.error('Invalid name: letters, numbers, and underscores only');
      return;
    }
    if (!cleanVal) {
      toast.error('Secret value is required');
      return;
    }

    setBusy('add-secret');
    try {
      const res = await requestJson<{ ok: boolean; name: string }>('/api/v1/copilot/secrets', {
        method: 'POST',
        body: JSON.stringify({ name: cleanKey, value: cleanVal }),
      });
      if (res.ok) {
        toast.success(`Secret ${cleanKey} saved`);
        setNewKey('');
        setNewVal('');
        setShowNewVal(false);
        // Refresh secrets
        const listRes = await requestJson<{ secrets: CopilotSecret[] }>('/api/v1/copilot/secrets');
        if (listRes.ok && listRes.data?.secrets) {
          setSecrets(listRes.data.secrets);
        }
      } else {
        toast.error(res.error || 'Failed to save secret');
      }
    } finally {
      setBusy(null);
    }
  };

  const handleDeleteSecret = async (name: string) => {
    if (!confirm(`Delete secret ${name}?`)) return;
    setBusy(`del-${name}`);
    try {
      const res = await requestJson<{ ok: boolean }>(`/api/v1/copilot/secrets/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      });
      if (res.ok) {
        toast.success(`Secret ${name} removed`);
        setSecrets((prev) => prev.filter((s) => s.name !== name));
      } else {
        toast.error(res.error || 'Failed to delete secret');
      }
    } finally {
      setBusy(null);
    }
  };

  const handleCopySecret = async (name: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopiedKey(name);
      toast.success(`Copied ${name} to clipboard`);
      setTimeout(() => setCopiedKey(null), 2000);
    } catch {
      toast.error('Could not copy to clipboard');
    }
  };

  const toggleReveal = (name: string) => {
    setRevealedSecrets((prev) => ({ ...prev, [name]: !prev[name] }));
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 backdrop-blur-sm sm:items-center sm:p-4"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="AI Settings"
        onClick={(e) => e.stopPropagation()}
        className="glass-panel flex max-h-[92dvh] w-full flex-col overflow-hidden rounded-t-[8px] bg-background text-foreground sm:max-w-lg sm:rounded-[8px]"
      >
        {/* Header */}
        <div className="flex items-center justify-between gap-3 px-4 pt-4 pb-2">
          <div className="min-w-0">
            <h2 className="text-[15px] font-medium">AI & Copilot Settings</h2>
            <p className="text-[12px] text-muted-foreground">Configure model providers, API keys, and secure runtime secrets.</p>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className={cn(btn, 'size-11 shrink-0 px-0 text-muted-foreground hover:bg-accent md:size-10')}
          >
            <XIcon className="size-5" />
          </button>
        </div>

        {/* Tab Navigation */}
        <div className="flex border-b border-border/50 px-4" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'providers'}
            onClick={() => setActiveTab('providers')}
            className={cn(
              'relative flex items-center gap-2 border-b-2 py-2.5 text-[13px] font-medium transition-colors',
              activeTab === 'providers'
                ? 'border-primary text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            )}
          >
            <CpuIcon className="size-3.5" />
            <span>Model Providers</span>
            {connected.length > 0 && (
              <span className="rounded-full bg-emerald-500/10 px-1.5 py-0.5 text-[10.5px] font-mono text-emerald-500">
                {connected.length}
              </span>
            )}
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'secrets'}
            onClick={() => setActiveTab('secrets')}
            className={cn(
              'relative ml-4 flex items-center gap-2 border-b-2 py-2.5 text-[13px] font-medium transition-colors',
              activeTab === 'secrets'
                ? 'border-primary text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            )}
          >
            <KeyRoundIcon className="size-3.5" />
            <span>Secrets & Environment</span>
            {secrets.length > 0 && (
              <span className="rounded-full bg-primary/10 px-1.5 py-0.5 text-[10.5px] font-mono text-primary">
                {secrets.length}
              </span>
            )}
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4 [scrollbar-width:thin]">
          {/* TAB 1: MODEL PROVIDERS */}
          {activeTab === 'providers' && (
            <div className="flex flex-col gap-4">
              {oauthPrompt && (oauthPrompt.url || oauthPrompt.title) && (
                <form onSubmit={submitCode} className="glass-control flex flex-col gap-2 rounded-[6px] p-3">
                  <p className="text-[13px] font-medium">{oauthPrompt.title || 'Finish signing in'}</p>
                  {oauthPrompt.instructions && <p className="text-[12px] text-muted-foreground">{oauthPrompt.instructions}</p>}
                  {oauthPrompt.url && (
                    <a
                      href={oauthPrompt.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1.5 text-[12.5px] underline underline-offset-2"
                    >
                      Open the sign-in page <ExternalLinkIcon className="size-3.5" />
                    </a>
                  )}
                  {oauthPrompt.title && (
                    <div className="flex gap-2">
                      <input
                        type={oauthPrompt.secret ? 'password' : 'text'}
                        autoComplete="off"
                        spellCheck={false}
                        value={code}
                        onChange={(e) => setCode(e.target.value)}
                        placeholder={oauthPrompt.placeholder || 'Paste the code or redirect URL'}
                        className="h-10 min-w-0 flex-1 rounded-[4px] bg-background/60 px-2.5 text-[13px] md:h-9 outline-none placeholder:text-muted-foreground"
                      />
                      <button type="submit" disabled={!code.trim() || busy === 'code'} className={cn(btn, 'glass-chosen')}>
                        {busy === 'code' ? <Loader2Icon className="size-3.5 animate-spin" /> : 'Submit'}
                      </button>
                    </div>
                  )}
                </form>
              )}

              {connected.length > 0 && (
                <section>
                  <h3 className="mb-1.5 text-[11.5px] font-medium text-muted-foreground">Connected · {connected.length}</h3>
                  <ul className="flex flex-col gap-1.5">
                    {connected.map((p) => (
                      <li key={p.id} className="glass-control flex min-h-12 items-center gap-2.5 rounded-[6px] px-3 py-2">
                        <span className="size-1.5 shrink-0 rounded-full bg-emerald-400" aria-hidden />
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-[13px] font-medium">{p.name}</p>
                          {status(p.id)?.maskedKey && (
                            <p className="truncate font-mono text-[11px] text-muted-foreground">{status(p.id)?.maskedKey}</p>
                          )}
                        </div>
                        <button
                          type="button"
                          disabled={busy === p.id}
                          onClick={async () => {
                            setBusy(p.id);
                            await onRemoveProvider(p.id);
                            setBusy(null);
                          }}
                          className={cn(btn, 'text-muted-foreground hover:bg-accent hover:text-foreground')}
                        >
                          {busy === p.id ? <Loader2Icon className="size-3.5 animate-spin" /> : 'Disconnect'}
                        </button>
                      </li>
                    ))}
                  </ul>
                </section>
              )}

              <section>
                <h3 className="mb-1.5 text-[11.5px] font-medium text-muted-foreground">Add a provider</h3>
                <label className="mb-2 flex h-10 items-center gap-2 rounded-[4px] bg-accent/50 px-2.5 text-[13px] text-muted-foreground focus-within:bg-accent md:h-9">
                  <SearchIcon className="size-3.5 shrink-0" />
                  <input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Search providers"
                    className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
                  />
                </label>

                {loginProviders.length === 0 ? (
                  <p className="py-6 text-center text-[12.5px] text-muted-foreground">Waiting for the agent engine…</p>
                ) : available.length === 0 ? (
                  <p className="py-6 text-center text-[12.5px] text-muted-foreground">No providers match.</p>
                ) : (
                  <ul className="flex flex-col gap-1">
                    {available.map((p) => (
                      <li key={p.id} className="flex min-h-12 items-center gap-2 rounded-[6px] px-3 py-1.5 hover:bg-accent/40">
                        <p className="min-w-0 flex-1 truncate text-[13px]">{p.name}</p>
                        <button
                          type="button"
                          disabled={busy === p.id}
                          onClick={() => {
                            setBusy(p.id);
                            onStartOAuth(p.id);
                          }}
                          className={cn(btn, 'glass-chosen shrink-0')}
                        >
                          {busy === p.id ? <Loader2Icon className="size-3.5 animate-spin" /> : 'Connect'}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            </div>
          )}

          {/* TAB 2: SECRETS & ENVIRONMENT */}
          {activeTab === 'secrets' && (
            <div className="flex flex-col gap-4">
              {/* Informative Security Callout */}
              <div className="glass-control flex items-start gap-2.5 rounded-[6px] p-3 text-[12px] text-muted-foreground">
                <ShieldCheckIcon className="size-4 shrink-0 text-emerald-400 mt-0.5" />
                <div className="flex-1 leading-relaxed">
                  Environment variables injected into NextDeploy AI subprocesses. Values are automatically redacted from
                  model prompts and chat transcripts by OMP secret protection.
                </div>
              </div>

              {/* Add New Secret Form */}
              <form onSubmit={handleAddSecret} className="glass-control flex flex-col gap-2.5 rounded-[6px] p-3">
                <h3 className="text-[12px] font-medium text-foreground">Add Secret or Variable</h3>
                <div className="flex flex-col gap-2 sm:flex-row">
                  <div className="min-w-0 flex-1">
                    <input
                      type="text"
                      spellCheck={false}
                      autoCapitalize="characters"
                      value={newKey}
                      onChange={(e) => setNewKey(e.target.value.toUpperCase())}
                      placeholder="NAME (e.g. DB_PASSWORD)"
                      className="h-10 w-full rounded-[4px] bg-background/60 px-2.5 font-mono text-[12.5px] outline-none placeholder:font-sans placeholder:text-muted-foreground md:h-9"
                    />
                  </div>
                  <div className="relative min-w-0 flex-1">
                    <input
                      type={showNewVal ? 'text' : 'password'}
                      spellCheck={false}
                      value={newVal}
                      onChange={(e) => setNewVal(e.target.value)}
                      placeholder="Secret Value"
                      className="h-10 w-full rounded-[4px] bg-background/60 pl-2.5 pr-8 text-[12.5px] outline-none placeholder:text-muted-foreground md:h-9"
                    />
                    <button
                      type="button"
                      onClick={() => setShowNewVal(!showNewVal)}
                      className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                      aria-label={showNewVal ? 'Hide secret' : 'Show secret'}
                    >
                      {showNewVal ? <EyeOffIcon className="size-3.5" /> : <EyeIcon className="size-3.5" />}
                    </button>
                  </div>
                </div>
                <div className="flex justify-end">
                  <button
                    type="submit"
                    disabled={!newKey.trim() || !newVal.trim() || busy === 'add-secret'}
                    className={cn(btn, 'glass-chosen h-9 px-3 text-[12.5px]')}
                  >
                    {busy === 'add-secret' ? (
                      <Loader2Icon className="size-3.5 animate-spin" />
                    ) : (
                      <>
                        <PlusIcon className="size-3.5" />
                        <span>Add Secret</span>
                      </>
                    )}
                  </button>
                </div>
              </form>

              {/* Secrets List */}
              <section>
                <div className="mb-2 flex items-center justify-between">
                  <h3 className="text-[11.5px] font-medium text-muted-foreground">Configured Secrets · {secrets.length}</h3>
                  {loadingSecrets && <Loader2Icon className="size-3 animate-spin text-muted-foreground" />}
                </div>

                {secrets.length === 0 ? (
                  <div className="glass-control rounded-[6px] p-6 text-center text-[12.5px] text-muted-foreground">
                    No secrets configured yet. Add database credentials, API tokens, or custom environment variables
                    for the AI to use safely.
                  </div>
                ) : (
                  <ul className="flex flex-col gap-1.5">
                    {secrets.map((sec) => {
                      const isRevealed = !!revealedSecrets[sec.name];
                      const isBusy = busy === `del-${sec.name}`;
                      const isCopied = copiedKey === sec.name;

                      return (
                        <li
                          key={sec.name}
                          className="glass-control flex min-h-12 items-center gap-2.5 rounded-[6px] px-3 py-2"
                        >
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <span className="font-mono text-[12px] font-medium text-foreground">{sec.name}</span>
                            </div>
                            <p className="truncate font-mono text-[11px] text-muted-foreground">
                              {isRevealed ? sec.value : sec.masked || '••••••••'}
                            </p>
                          </div>

                          <div className="flex items-center gap-1">
                            <button
                              type="button"
                              onClick={() => toggleReveal(sec.name)}
                              title={isRevealed ? 'Mask secret' : 'Reveal secret'}
                              aria-label={isRevealed ? 'Mask secret' : 'Reveal secret'}
                              className={cn(
                                btn,
                                'size-8 px-0 text-muted-foreground hover:bg-accent hover:text-foreground md:size-8'
                              )}
                            >
                              {isRevealed ? <EyeOffIcon className="size-3.5" /> : <EyeIcon className="size-3.5" />}
                            </button>

                            <button
                              type="button"
                              onClick={() => handleCopySecret(sec.name, sec.value)}
                              title="Copy secret value"
                              aria-label="Copy secret value"
                              className={cn(
                                btn,
                                'size-8 px-0 text-muted-foreground hover:bg-accent hover:text-foreground md:size-8'
                              )}
                            >
                              {isCopied ? (
                                <CheckIcon className="size-3.5 text-emerald-400" />
                              ) : (
                                <CopyIcon className="size-3.5" />
                              )}
                            </button>

                            <button
                              type="button"
                              disabled={isBusy}
                              onClick={() => handleDeleteSecret(sec.name)}
                              title="Delete secret"
                              aria-label="Delete secret"
                              className={cn(
                                btn,
                                'size-8 px-0 text-muted-foreground hover:bg-destructive/15 hover:text-destructive md:size-8'
                              )}
                            >
                              {isBusy ? (
                                <Loader2Icon className="size-3.5 animate-spin" />
                              ) : (
                                <Trash2Icon className="size-3.5" />
                              )}
                            </button>
                          </div>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </section>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
