import React, { useEffect, useMemo, useState } from 'react';
import { ExternalLinkIcon, Loader2Icon, SearchIcon, XIcon } from 'lucide-react';

import { cn } from '@/lib/utils';
import type { LoginProvider, OAuthPrompt, ProvidersConfig } from './types';

interface ProviderAuthModalProps {
  isOpen: boolean;
  onClose: () => void;
  providers: ProvidersConfig | null;
  loginProviders: LoginProvider[];
  oauthPrompt: OAuthPrompt | null;
  onStartOAuth: (providerId: string) => void;
  onSubmitOAuthInput: (id: string, value: string) => Promise<boolean>;
  onRemoveProvider: (providerId: string) => Promise<void>;
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
}) => {
  const [query, setQuery] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [code, setCode] = useState('');

  useEffect(() => {
    if (oauthPrompt) setBusy(null);
  }, [oauthPrompt]);

  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isOpen, onClose]);

  const status = (id: string) => (providers as any)?.[id] as { configured?: boolean; maskedKey?: string; host?: string } | undefined;
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

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 backdrop-blur-sm sm:items-center sm:p-4"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Model providers"
        onClick={(e) => e.stopPropagation()}
        className="glass-panel flex max-h-[92dvh] w-full flex-col overflow-hidden rounded-t-[8px] bg-background text-foreground sm:max-w-lg sm:rounded-[8px]"
      >
        <div className="flex items-center justify-between gap-3 px-4 pt-4 pb-3">
          <div className="min-w-0">
            <h2 className="text-[15px] font-medium">Model providers</h2>
            <p className="text-[12px] text-muted-foreground">Sign in once; logins are kept in the panel's data volume.</p>
          </div>
          <button type="button" aria-label="Close" onClick={onClose} className={cn(btn, 'size-10 shrink-0 px-0 md:size-9 text-muted-foreground hover:bg-accent')}>
            <XIcon className="size-4" />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4 [scrollbar-width:thin]">
          {oauthPrompt && (oauthPrompt.url || oauthPrompt.title) && (
            <form onSubmit={submitCode} className="glass-control mb-4 flex flex-col gap-2 rounded-[6px] p-3">
              <p className="text-[13px]">{oauthPrompt.title || 'Finish signing in'}</p>
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
            <section className="mb-5">
              <h3 className="mb-1.5 text-[11.5px] text-muted-foreground">Connected · {connected.length}</h3>
              <ul className="flex flex-col gap-1">
                {connected.map((p) => (
                  <li key={p.id} className="glass-control flex min-h-12 items-center gap-2.5 rounded-[6px] px-3 py-2">
                    <span className="size-1.5 shrink-0 rounded-full bg-emerald-400" aria-hidden />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[13px]">{p.name}</p>
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
            <h3 className="mb-1.5 text-[11.5px] text-muted-foreground">Add a provider</h3>
            <label className="mb-2 flex h-10 items-center gap-2 rounded-[4px] bg-accent/50 md:h-9 px-2.5 text-[13px] text-muted-foreground focus-within:bg-accent">
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
      </div>
    </div>
  );
};
