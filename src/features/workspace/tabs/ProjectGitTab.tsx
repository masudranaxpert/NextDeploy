import React, { useEffect, useMemo, useState } from 'react';
import {
  GitBranch,
  RefreshCw,
  Webhook,
  Trash2,
  CheckCircle2,
  AlertTriangle,
  Pause,
  Play,
  KeyRound,
  Eye,
  EyeOff,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { ShadcnDialog, ShadcnSwitch, StatusBadge } from '../../../components/ui/Primitives';
import { backendApi } from '../../../lib/backendApi';

interface ProjectGitTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
}

interface RepoOption {
  full_name: string;
  clone_url: string;
  html_url?: string;
  private: boolean;
  default_branch: string;
}

export const ProjectGitTab: React.FC<ProjectGitTabProps> = ({ app, onUpdateApp }) => {
  const [gitSyncEnabled, setGitSyncEnabled] = useState<boolean>(app.gitSync ?? (app.sourceType === 'git'));
  const [authMode, setAuthMode] = useState<'public' | 'github_app' | 'gitlab_token'>(
    app.gitAuthMode || 'public'
  );

  const [availableProviders, setAvailableProviders] = useState<any[]>([]);
  const [selectedProviderId, setSelectedProviderId] = useState<string>(app.gitProviderId || '');

  const [repoUrl, setRepoUrl] = useState<string>(app.gitRepo || '');
  const [selectedRepoFullName, setSelectedRepoFullName] = useState<string>(
    app.gitRepo ? app.gitRepo.replace(/^https?:\/\/(github|gitlab)\.com\//, '').replace(/\.git$/, '') : ''
  );
  const [branch, setBranch] = useState<string>(app.gitBranch || 'main');
  const [autoDeploy, setAutoDeploy] = useState<boolean>(Boolean(app.webhookAutoDeploy));

  useEffect(() => {
    setGitSyncEnabled(app.gitSync ?? (app.sourceType === 'git'));
  }, [app.gitSync, app.sourceType]);

  const [providerRepos, setProviderRepos] = useState<RepoOption[]>([]);
  const [providerBranches, setProviderBranches] = useState<string[]>(['main', 'master', 'production']);
  const [isLoadingRepos, setIsLoadingRepos] = useState(false);
  const [isLoadingBranches, setIsLoadingBranches] = useState(false);
  const [isSyncingNow, setIsSyncingNow] = useState(false);

  // Webhook Secret State
  const [webhookSecret] = useState<string>(() => {
    if (app.webhookSecret) return app.webhookSecret;
    const cleanSlug = app.slug.replace(/[^a-z0-9]/g, '').slice(0, 8) || 'a1b2c3d4';
    return `nd_whsec_${cleanSlug}`;
  });
  const [showSecret, setShowSecret] = useState(false);

  // Fetch real providers on mount
  useEffect(() => {
    void backendApi.gitProvidersList().then((res) => {
      const providers = res.data?.providers || [];
      setAvailableProviders(providers);
      if (providers.length > 0 && !selectedProviderId) {
        setSelectedProviderId(String(providers[0].id));
      }
    });
  }, []);

  const handleSelectAuthMode = (mode: 'public' | 'github_app' | 'gitlab_token') => {
    setAuthMode(mode);
    const matched = availableProviders.find(
      (p) => p.provider?.toLowerCase() === (mode === 'gitlab_token' ? 'gitlab' : 'github')
    );
    if (matched) {
      setSelectedProviderId(String(matched.id));
      void loadProviderRepos(matched.id);
    }
  };

  // Force Copy Git Checkout Confirmation Modal (when pausing GitSync with existing workspace files)
  const [forceSyncModalOpen, setForceSyncModalOpen] = useState(false);
  const [forceSyncWarning, setForceSyncWarning] = useState('');

  const loadProviderRepos = async (pid: string) => {
    if (!pid) return;
    setIsLoadingRepos(true);
    const res = await backendApi.gitProviderRepos(app.id, pid);
    setIsLoadingRepos(false);
    if (res.ok && Array.isArray(res.data?.repos) && res.data.repos.length > 0) {
      setProviderRepos(res.data.repos);
    }
  };

  const loadProviderBranches = async (pid: string, repoFull: string) => {
    if (!pid || !repoFull) return;
    setIsLoadingBranches(true);
    const res = await backendApi.gitProviderBranches(app.id, pid, repoFull);
    setIsLoadingBranches(false);
    if (res.ok && Array.isArray(res.data?.branches) && res.data.branches.length > 0) {
      setProviderBranches(res.data.branches);
    }
  };

  const scanPublicBranches = async (urlToScan?: string) => {
    const targetUrl = (urlToScan || repoUrl).trim();
    if (!targetUrl || (!targetUrl.startsWith('http://') && !targetUrl.startsWith('https://') && !targetUrl.startsWith('git://'))) {
      toast.error('Enter a valid Git HTTP/HTTPS URL first');
      return;
    }
    setIsLoadingBranches(true);
    const res = await backendApi.publicGitRemoteBranches(app.id, targetUrl);
    setIsLoadingBranches(false);
    if (res.ok && Array.isArray(res.data?.branches) && res.data.branches.length > 0) {
      setProviderBranches(res.data.branches);
      if (!branch || !res.data.branches.includes(branch)) {
        setBranch(res.data.branches[0]);
      }
      toast.success(`Found ${res.data.branches.length} remote branch(es)`);
    } else {
      toast.error(res.data?.error || 'No remote branches found');
    }
  };

  useEffect(() => {
    if (authMode !== 'public' && selectedProviderId) {
      void loadProviderRepos(selectedProviderId);
    }
  }, [authMode, selectedProviderId]);

  const handleToggleGitSync = async (nextEnabled: boolean, force = false) => {
    const res = await backendApi.setGitSync(app.id, nextEnabled, force);
    if (res.data?.confirm_needed && !force) {
      setForceSyncWarning(
        res.data.warning ||
          'Workspace already contains local files. Pausing Git Sync will copy the Git checkout into the workspace and overwrite matching files.'
      );
      setForceSyncModalOpen(true);
      return;
    }
    setGitSyncEnabled(nextEnabled);
    setForceSyncModalOpen(false);
    onUpdateApp({
      ...app,
      sourceType: nextEnabled ? 'git' : 'compose',
      gitSync: nextEnabled,
    });
    toast.success(
      nextEnabled
        ? 'Git Sync ON — deployments automatically pull from Git'
        : 'Git Sync OFF — deployments use Workspace Files (use Sync Now for manual Git pull)'
    );
  };

  const handleManualSyncNow = async () => {
    setIsSyncingNow(true);
    const res = await backendApi.gitSyncNow(app.id, false);
    setIsSyncingNow(false);
    if (!res.ok && !res.offlineFallback) {
      toast.error(res.error || 'Repository sync failed');
      return;
    }
    toast.success(
      gitSyncEnabled
        ? `Pulled latest commits from ${branch}`
        : `Manually synced ${branch} into File Manager workspace`
    );
  };

  const handleSaveGitConfig = async (e: React.FormEvent) => {
    e.preventDefault();
    const res = await backendApi.saveAppGit(app.id, {
      auth_mode: authMode,
      git_provider_id: authMode === 'public' ? '' : selectedProviderId,
      repo_url: repoUrl.trim(),
      branch: branch.trim() || 'main',
      auto_deploy: autoDeploy,
    });
    if (!res.ok && !res.offlineFallback) {
      toast.error(res.error || 'Failed to save Git configuration');
      return;
    }
    onUpdateApp({
      ...app,
      sourceType: gitSyncEnabled ? 'git' : 'compose',
      gitSync: gitSyncEnabled,
      gitAuthMode: authMode,
      gitProviderId: selectedProviderId,
      gitRepo: repoUrl.trim(),
      gitBranch: branch.trim() || 'main',
      webhookAutoDeploy: autoDeploy,
    });
    toast.success(`Saved Git configuration (${branch.trim() || 'main'})`);
  };

  const handleDisconnectGit = async () => {
    await backendApi.deleteAppGit(app.id);
    onUpdateApp({
      ...app,
      sourceType: 'compose',
      gitSync: false,
      gitRepo: undefined,
      webhookAutoDeploy: false,
    });
    toast.success('Disconnected Git repository — project is now in Pure Manual Files mode');
  };

  const webhookUrl = useMemo(() => {
    const origin =
      typeof window !== 'undefined'
        ? window.location.origin
        : 'https://panel.nextdeploy.sh';
    if (authMode === 'gitlab_token') {
      return `${origin}/webhooks/gitlab/${app.id}`;
    }
    return `${origin}/webhooks/github/${app.id}`;
  }, [authMode, app.id]);

  return (
    <div className="space-y-4">
      {/* Row 1: Git Sync ON / OFF Control Bar + Manual Sync Now */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-zinc-200">Git Sync:</span>
            <ShadcnSwitch
              checked={gitSyncEnabled}
              onChange={(next) => void handleToggleGitSync(next)}
            />
          </div>
          {gitSyncEnabled ? (
            <span className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2 py-0.5 font-mono text-[11px] font-medium text-emerald-300">
              Git Sync ON · Auto-pulls {branch} on deploy
            </span>
          ) : (
            <span className="rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 font-mono text-[11px] font-medium text-amber-300">
              Git Sync OFF · Manual File Edits Active (Pull manually with Sync Now)
            </span>
          )}
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void handleManualSyncNow()}
            disabled={isSyncingNow}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-orange-500/35 bg-orange-500/12 px-3 text-xs font-semibold text-orange-300 hover:bg-orange-500/20 disabled:opacity-50 cursor-pointer"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${isSyncingNow ? 'animate-spin' : ''}`} />
            {isSyncingNow ? 'Syncing...' : 'Sync Now (Pull from Git)'}
          </button>

          {app.gitRepo && (
            <button
              type="button"
              onClick={() => void handleDisconnectGit()}
              className="inline-flex h-8 items-center gap-1 rounded-lg border border-rose-500/25 bg-rose-500/10 px-2.5 text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Disconnect Git
            </button>
          )}
        </div>
      </div>

      {/* Row 2: Git Provider, Repository & Branch Configuration Form (POST /apps/:id/git) */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <GitBranch className="h-4 w-4 text-orange-400" />
            <h2 className="text-sm font-semibold text-zinc-100">
              Git Repository &amp; Branch
            </h2>
            <StatusBadge
              status={gitSyncEnabled ? 'active' : 'stopped'}
              label={gitSyncEnabled ? 'Git Sync ON' : 'Git Sync OFF'}
            />
          </div>
          <button
            type="button"
            onClick={() => void handleToggleGitSync(!gitSyncEnabled)}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            {gitSyncEnabled ? (
              <>
                <Pause className="h-3 w-3 text-amber-400" />
                Turn Off Git Sync
              </>
            ) : (
              <>
                <Play className="h-3 w-3 text-emerald-400" />
                Turn On Git Sync
              </>
            )}
          </button>
        </div>

        <form onSubmit={handleSaveGitConfig} className="space-y-4">
          {/* Auth Mode Selector */}
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
            {(
              [
                {
                  id: 'github_app',
                  label: 'GitHub App Installation',
                  sub: 'Auto repo & branch discovery',
                },
                {
                  id: 'gitlab_token',
                  label: 'GitLab OAuth 2.0 / Token',
                  sub: 'GitLab repositories & webhooks',
                },
                {
                  id: 'public',
                  label: 'Public Git URL',
                  sub: 'Any public HTTPS repository',
                },
              ] as const
            ).map((mode) => (
              <button
                key={mode.id}
                type="button"
                onClick={() => handleSelectAuthMode(mode.id)}
                className={`rounded-xl border p-3 text-left transition-all cursor-pointer ${
                  authMode === mode.id
                    ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100'
                    : 'border-white/[0.07] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <div className="text-xs font-semibold">{mode.label}</div>
                <div className="mt-0.5 text-[10px] text-zinc-500">{mode.sub}</div>
              </button>
            ))}
          </div>

          {/* Connected Provider + Live Repository Picker */}
          {authMode !== 'public' && (
            <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
              <div>
                <div className="flex items-center justify-between mb-1.5">
                  <label className="text-[11px] font-medium text-zinc-400">
                    Connected Git Provider ({authMode === 'gitlab_token' ? 'GitLab' : 'GitHub'})
                  </label>
                  <button
                    type="button"
                    onClick={() => void loadProviderRepos(selectedProviderId)}
                    className="inline-flex items-center gap-1 font-mono text-[10px] text-orange-400 hover:underline cursor-pointer"
                  >
                    <RefreshCw className={`h-2.5 w-2.5 ${isLoadingRepos ? 'animate-spin' : ''}`} />
                    Refresh Repos
                  </button>
                </div>
                <select
                  value={selectedProviderId}
                  onChange={(e) => {
                    setSelectedProviderId(e.target.value);
                    void loadProviderRepos(e.target.value);
                  }}
                  className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                >
                  {availableProviders.map((p: any) => (
                    <option key={p.id} value={p.id}>
                      {(p.provider?.toLowerCase() === 'gitlab' ? 'GitLab' : 'GitHub')} · {p.accountName || p.name} ({p.authMethod || p.notes || (p.token ? 'Token' : 'GitHub App')})
                    </option>
                  ))}
                  {availableProviders.length === 0 && (
                    <option value="">No {authMode === 'gitlab_token' ? 'GitLab' : 'GitHub'} providers connected</option>
                  )}
                </select>
              </div>

              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Select Repository ({providerRepos.length} available)
                </label>
                <select
                  value={selectedRepoFullName}
                  onChange={(e) => {
                    const full = e.target.value;
                    setSelectedRepoFullName(full);
                    const matched = providerRepos.find((r) => r.full_name === full);
                    if (matched) {
                      setRepoUrl(matched.clone_url);
                      setBranch(matched.default_branch || 'main');
                    } else {
                      const prefix = authMode === 'gitlab_token' ? 'https://gitlab.com/' : 'https://github.com/';
                      setRepoUrl(`${prefix}${full}.git`);
                    }
                    void loadProviderBranches(selectedProviderId, full);
                  }}
                  className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                >
                  {providerRepos.map((r) => (
                    <option key={r.full_name} value={r.full_name}>
                      {r.full_name} {r.private ? '(Private)' : '(Public)'}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          )}

          {/* Repo URL & Branch Selector */}
          <div className="grid grid-cols-1 gap-3.5 md:grid-cols-3">
            <div className="md:col-span-2">
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Repository Clone URL
              </label>
              <input
                type="text"
                required
                value={repoUrl}
                onChange={(e) => setRepoUrl(e.target.value)}
                onBlur={() => {
                  if (authMode === 'public' && repoUrl.trim()) {
                    void scanPublicBranches();
                  }
                }}
                placeholder={
                  authMode === 'gitlab_token'
                    ? 'https://gitlab.com/group/project.git'
                    : 'https://github.com/org/repo.git'
                }
                className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-[11px] font-medium text-zinc-400">Target Branch</label>
                {authMode !== 'public' ? (
                  <button
                    type="button"
                    onClick={() => void loadProviderBranches(selectedProviderId, selectedRepoFullName)}
                    className="inline-flex items-center gap-1 font-mono text-[10px] text-orange-400 hover:underline cursor-pointer"
                  >
                    <RefreshCw
                      className={`h-2.5 w-2.5 ${isLoadingBranches ? 'animate-spin' : ''}`}
                    />
                    Fetch Branches
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => void scanPublicBranches()}
                    className="inline-flex items-center gap-1 font-mono text-[10px] text-orange-400 hover:underline cursor-pointer"
                  >
                    <RefreshCw
                      className={`h-2.5 w-2.5 ${isLoadingBranches ? 'animate-spin' : ''}`}
                    />
                    Scan Branches
                  </button>
                )}
              </div>
              <input
                type="text"
                required
                list="git-provider-branches"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="main"
                className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-orange-400 font-semibold focus:border-orange-500 focus:outline-none"
              />
              <datalist id="git-provider-branches">
                {providerBranches.map((b) => (
                  <option key={b} value={b} />
                ))}
              </datalist>
            </div>
          </div>

          {/* Quick Branch Pills */}
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-zinc-500 mr-1">Branches:</span>
            {providerBranches.map((br) => (
              <button
                key={br}
                type="button"
                onClick={() => setBranch(br)}
                className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-[11px] cursor-pointer ${
                  branch === br
                    ? 'border-orange-500/50 bg-orange-500/15 text-orange-300 font-semibold'
                    : 'border-white/[0.07] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <GitBranch className="h-3 w-3" />
                {br}
              </button>
            ))}
          </div>

          {/* Webhook Endpoint, Webhook Secret & Auto-Deploy Toggle */}
          <div className="space-y-3 pt-1">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
                <div className="flex items-center gap-2.5 min-w-0">
                  <Webhook className="h-4 w-4 text-orange-400 shrink-0" />
                  <div className="min-w-0">
                    <div className="text-xs font-medium text-zinc-200">
                      Push Webhook Endpoint ({authMode === 'gitlab_token' ? 'GitLab' : 'GitHub'})
                    </div>
                    <div className="truncate font-mono text-[11px] text-zinc-400">{webhookUrl}</div>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    navigator.clipboard?.writeText(webhookUrl);
                    toast.success('Copied webhook URL');
                  }}
                  className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer shrink-0"
                >
                  Copy
                </button>
              </div>

              <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
                <div className="flex items-center gap-2.5 min-w-0">
                  <KeyRound className="h-4 w-4 text-amber-400 shrink-0" />
                  <div className="min-w-0">
                    <div className="text-xs font-medium text-zinc-200">
                      Webhook Secret {authMode === 'gitlab_token' ? '(Secret Token)' : '(HMAC Secret)'}
                    </div>
                    <div className="truncate font-mono text-[11px] text-zinc-400">
                      {showSecret ? webhookSecret : '••••••••••••••••••••••••'}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-1.5 shrink-0">
                  <button
                    type="button"
                    onClick={() => setShowSecret(!showSecret)}
                    className="rounded-lg border border-white/[0.08] bg-zinc-900 p-1.5 text-zinc-400 hover:text-zinc-200 cursor-pointer"
                    title={showSecret ? 'Hide Secret' : 'Show Secret'}
                  >
                    {showSecret ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard?.writeText(webhookSecret);
                      toast.success('Copied webhook secret');
                    }}
                    className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                  >
                    Copy
                  </button>
                </div>
              </div>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
              <div>
                <div className="text-xs font-medium text-zinc-200">Auto-Deploy on Push</div>
                <div className="text-[11px] text-zinc-500">
                  Redeploy automatically when commits are pushed to {branch} via {authMode === 'gitlab_token' ? 'GitLab' : 'GitHub'} Webhook
                </div>
              </div>
              <ShadcnSwitch checked={autoDeploy} onChange={setAutoDeploy} />
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="submit"
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              Save Git Configuration
            </button>
          </div>
        </form>
      </div>

      {/* Force Pause Git Sync Confirmation Modal */}
      <ShadcnDialog
        open={forceSyncModalOpen}
        onClose={() => setForceSyncModalOpen(false)}
        title="Copy Git Checkout into Workspace?"
        icon={<AlertTriangle className="h-4 w-4 text-amber-400" />}
      >
        <div className="space-y-4">
          <p className="text-xs text-zinc-300 leading-relaxed">{forceSyncWarning}</p>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setForceSyncModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => void handleToggleGitSync(false, true)}
              className="rounded-lg bg-orange-500 px-4 py-1.5 text-xs font-semibold text-white hover:bg-orange-400 cursor-pointer"
            >
              Overwrite &amp; Pause Git Sync
            </button>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
};
