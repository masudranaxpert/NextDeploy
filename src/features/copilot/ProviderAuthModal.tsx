import React, { useState } from 'react';
import {
  X,
  Key,
  CheckCircle,
  ExternalLink,
  Loader2,
  Server,
  Eye,
  EyeOff,
  LogIn,
  ShieldCheck,
} from 'lucide-react';
import type { ProvidersConfig, LoginProvider, OAuthPrompt } from './types';

interface ProviderAuthModalProps {
  isOpen: boolean;
  onClose: () => void;
  providers: ProvidersConfig | null;
  loginProviders: LoginProvider[];
  oauthPrompt: OAuthPrompt | null;
  onStartOAuth: (providerId: string) => void;
  onSubmitOAuthInput: (id: string, value: string) => Promise<boolean>;
  onSaveKeys: (keys: Record<string, string>) => Promise<boolean>;
}

interface ProviderMeta {
  key: string;
  name: string;
  docsUrl: string;
  placeholder: string;
  isHost?: boolean;
}

const API_KEY_METAS: ProviderMeta[] = [
  {
    key: 'anthropic',
    name: 'Anthropic Claude',
    docsUrl: 'https://console.anthropic.com/settings/keys',
    placeholder: 'sk-ant-api03-...',
  },
  {
    key: 'openai',
    name: 'OpenAI',
    docsUrl: 'https://platform.openai.com/api-keys',
    placeholder: 'sk-proj-...',
  },
  {
    key: 'deepseek',
    name: 'DeepSeek',
    docsUrl: 'https://platform.deepseek.com/api_keys',
    placeholder: 'sk-...',
  },
  {
    key: 'gemini',
    name: 'Google Gemini',
    docsUrl: 'https://aistudio.google.com/app/apikey',
    placeholder: 'AIzaSy...',
  },
  {
    key: 'ollama',
    name: 'Ollama (Local Models)',
    docsUrl: 'https://ollama.com',
    placeholder: 'http://localhost:11434',
    isHost: true,
  },
];

export const ProviderAuthModal: React.FC<ProviderAuthModalProps> = ({
  isOpen,
  onClose,
  providers,
  loginProviders,
  oauthPrompt,
  onStartOAuth,
  onSubmitOAuthInput,
  onSaveKeys,
}) => {
  const [activeTab, setActiveTab] = useState<'oauth' | 'keys'>('oauth');
  const [formData, setFormData] = useState<Record<string, string>>({});
  const [showKeys, setShowKeys] = useState<Record<string, boolean>>({});
  const [isSaving, setIsSaving] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [authCode, setAuthCode] = useState('');
  const [isSubmittingCode, setIsSubmittingCode] = useState(false);

  if (!isOpen) return null;

  const handleChange = (key: string, value: string) => {
    setFormData((prev) => ({ ...prev, [key]: value }));
  };

  const toggleShow = (key: string) => {
    setShowKeys((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const handleSaveKeys = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    setSaveSuccess(false);

    const ok = await onSaveKeys(formData);
    setIsSaving(false);
    if (ok) {
      setSaveSuccess(true);
      setTimeout(() => {
        setSaveSuccess(false);
        onClose();
      }, 1200);
    }
  };

  const handleOAuthSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!oauthPrompt || !authCode.trim()) return;
    setIsSubmittingCode(true);
    await onSubmitOAuthInput(oauthPrompt.id, authCode.trim());
    setIsSubmittingCode(false);
    setAuthCode('');
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm animate-in fade-in duration-150">
      <div
        className="relative w-full max-w-xl rounded-2xl border border-white/[0.1] bg-[#0c0d0e] p-6 text-zinc-100 shadow-2xl transition-all"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-white/[0.08] pb-4">
          <div className="flex items-center gap-2.5">
            <div className="flex size-9 items-center justify-center rounded-xl bg-orange-500/10 text-orange-400 border border-orange-500/20">
              <ShieldCheck className="size-4" />
            </div>
            <div>
              <h2 className="text-base font-semibold tracking-tight text-white">
                AI Authentication & Providers
              </h2>
              <p className="text-xs text-zinc-400">
                Log in directly via OMP or configure Bring-Your-Own-Key (BYOK).
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="flex size-8 items-center justify-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-white transition-colors"
          >
            <X className="size-4" />
          </button>
        </div>

        {/* Tabs: OAuth vs API Keys */}
        <div className="flex border-b border-white/[0.08] mt-3">
          <button
            type="button"
            onClick={() => setActiveTab('oauth')}
            className={`flex items-center gap-2 px-4 py-2 text-xs font-medium border-b-2 transition-colors ${
              activeTab === 'oauth'
                ? 'border-orange-500 text-orange-400 font-semibold'
                : 'border-transparent text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <LogIn className="size-3.5" />
            <span>OAuth Logins ({loginProviders.length})</span>
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('keys')}
            className={`flex items-center gap-2 px-4 py-2 text-xs font-medium border-b-2 transition-colors ${
              activeTab === 'keys'
                ? 'border-orange-500 text-orange-400 font-semibold'
                : 'border-transparent text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <Key className="size-3.5" />
            <span>API Keys (BYOK)</span>
          </button>
        </div>

        {/* OAuth Flow Active Banner */}
        {oauthPrompt && (
          <div className="mt-4 rounded-xl border border-blue-500/30 bg-blue-500/10 p-4 text-xs">
            <div className="flex items-center justify-between font-semibold text-blue-300 mb-2">
              <span>Authentication In Progress</span>
              <span className="animate-pulse flex size-2 rounded-full bg-blue-400" />
            </div>
            {oauthPrompt.instructions && (
              <p className="text-zinc-300 text-[11px] mb-3 leading-relaxed">
                {oauthPrompt.instructions}
              </p>
            )}

            {oauthPrompt.url && (
              <a
                href={oauthPrompt.url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-blue-500 transition-colors mb-3"
              >
                <span>Authorize in Browser</span>
                <ExternalLink className="size-3" />
              </a>
            )}

            <form onSubmit={handleOAuthSubmit} className="flex gap-2">
              <input
                type="text"
                value={authCode}
                onChange={(e) => setAuthCode(e.target.value)}
                placeholder={oauthPrompt.placeholder || 'Paste authorization code or redirect URL...'}
                className="flex-1 rounded-lg border border-white/[0.1] bg-black/50 px-3 py-1.5 text-xs text-white placeholder:text-zinc-600 outline-none focus:border-blue-400"
              />
              <button
                type="submit"
                disabled={!authCode.trim() || isSubmittingCode}
                className="rounded-lg bg-orange-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-orange-500 disabled:opacity-50 transition-colors"
              >
                {isSubmittingCode ? <Loader2 className="size-3.5 animate-spin" /> : 'Submit'}
              </button>
            </form>
          </div>
        )}

        {/* Body content based on tab */}
        {activeTab === 'oauth' ? (
          <div className="mt-4 max-h-[50vh] overflow-y-auto space-y-2 pr-1 divide-y divide-white/[0.04]">
            {loginProviders.length === 0 ? (
              <div className="p-6 text-center text-xs text-zinc-500">
                Fetching OAuth providers from engine...
              </div>
            ) : (
              loginProviders.map((lp) => (
                <div
                  key={lp.id}
                  className="flex items-center justify-between py-2.5 px-2 hover:bg-white/[0.02] rounded-lg transition-colors"
                >
                  <div className="flex items-center gap-2.5">
                    <LogIn className="size-3.5 text-zinc-400" />
                    <div>
                      <span className="text-xs font-medium text-zinc-200">{lp.name}</span>
                      <span className="block text-[10px] text-zinc-500 font-mono">{lp.id}</span>
                    </div>
                  </div>

                  <div>
                    {lp.authenticated ? (
                      <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2.5 py-1 text-[11px] font-medium text-emerald-400 border border-emerald-500/20">
                        <CheckCircle className="size-3" />
                        Connected
                      </span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => onStartOAuth(lp.id)}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.1] bg-white/[0.04] px-3 py-1 text-xs font-medium text-zinc-300 hover:border-orange-500/40 hover:bg-orange-500/10 hover:text-orange-300 transition-colors"
                      >
                        <span>Connect</span>
                        <ExternalLink className="size-2.5" />
                      </button>
                    )}
                  </div>
                </div>
              ))
            )}
          </div>
        ) : (
          <form onSubmit={handleSaveKeys} className="mt-4 space-y-3 max-h-[50vh] overflow-y-auto pr-1">
            {API_KEY_METAS.map((prov) => {
              const status = providers ? (providers as any)[prov.key] : null;
              const isConfigured = status?.configured;
              const currentValue = formData[prov.key] !== undefined ? formData[prov.key] : '';
              const isHost = prov.isHost;
              const isPassword = !isHost && !showKeys[prov.key];

              return (
                <div
                  key={prov.key}
                  className="group rounded-xl border border-white/[0.06] bg-white/[0.02] p-3 transition-colors focus-within:border-orange-500/40"
                >
                  <div className="flex items-center justify-between mb-1.5">
                    <div className="flex items-center gap-2">
                      {isHost ? (
                        <Server className="size-3.5 text-zinc-400" />
                      ) : (
                        <Key className="size-3.5 text-zinc-400" />
                      )}
                      <span className="text-xs font-medium text-zinc-200">{prov.name}</span>
                      {isConfigured && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-400 border border-emerald-500/20">
                          <CheckCircle className="size-2.5" />
                          Configured
                        </span>
                      )}
                    </div>
                    <a
                      href={prov.docsUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-[11px] text-zinc-500 hover:text-zinc-300 transition-colors"
                    >
                      <span>Get Key</span>
                      <ExternalLink className="size-2.5" />
                    </a>
                  </div>

                  <div className="relative flex items-center">
                    <input
                      type={isPassword ? 'password' : 'text'}
                      value={currentValue}
                      placeholder={
                        isConfigured
                          ? isHost
                            ? status.host || prov.placeholder
                            : status.maskedKey || '••••••••••••••••'
                          : prov.placeholder
                      }
                      onChange={(e) => handleChange(prov.key, e.target.value)}
                      className="w-full rounded-lg border border-white/[0.08] bg-black/40 px-3 py-1.5 text-xs text-zinc-100 placeholder:text-zinc-600 outline-none transition-colors focus:border-orange-500/60"
                    />
                    {!isHost && (
                      <button
                        type="button"
                        onClick={() => toggleShow(prov.key)}
                        className="absolute right-2.5 text-zinc-500 hover:text-zinc-300 transition-colors"
                      >
                        {showKeys[prov.key] ? (
                          <EyeOff className="size-3.5" />
                        ) : (
                          <Eye className="size-3.5" />
                        )}
                      </button>
                    )}
                  </div>
                </div>
              );
            })}

            <div className="flex items-center justify-end gap-3 pt-3 border-t border-white/[0.08]">
              <button
                type="button"
                onClick={onClose}
                disabled={isSaving}
                className="rounded-lg px-3 py-1.5 text-xs font-medium text-zinc-300 hover:bg-white/[0.06] transition-colors"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSaving}
                className="inline-flex items-center gap-2 rounded-lg bg-orange-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-orange-500 disabled:opacity-50 transition-colors"
              >
                {isSaving ? (
                  <>
                    <Loader2 className="size-3.5 animate-spin" />
                    <span>Saving...</span>
                  </>
                ) : saveSuccess ? (
                  <>
                    <CheckCircle className="size-3.5 text-white" />
                    <span>Saved!</span>
                  </>
                ) : (
                  <span>Save & Apply</span>
                )}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
};
