import React, { useEffect, useState } from 'react';
import {
  GitBranch,
  RefreshCw,
  Webhook,
  Trash2,
  CheckCircle2,
  AlertTriangle,
  Pause,
  Play,
  Key,
  Eye,
  EyeOff,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { ShadcnDialog, ShadcnSwitch, StatusBadge } from '../../../components/ui/Primitives';
import { backendApi } from '../../../lib/backendApi';
import { copyToClipboard } from '../../../lib/clipboard';

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
    app.gitAuthMode || 'github_app'
  );
  const [providers, setProviders] = useState<
    Array<{ id: string; provider: string; accountName: string; authMethod: string }>
  >([]);
  const [selectedProviderId, setSelectedProviderId] = useState<string>(app.gitProviderId || '');
  const [repoUrl, setRepoUrl] = useState<string>(app.gitRepo || '');
  const [selectedRepoFullName, setSelectedRepoFullName] = useState<string>(
    app.gitRepo ? app.gitRepo.replace(/^https?:\/\/(github|gitlab)\.com\//, '').replace(/\.git$/, '') : ''
  );
  const [branch, setBranch] = useState<string>(app.gitBranch || 'main');
  const [autoDeploy, setAutoDeploy] = useState<boolean>(Boolean(app.webhookAutoDeploy));
  const [webhookSecret, setWebhookSecret] = useState<string>(app.webhookSecret || '');
  const [showSecret, setShowSecret] = useState<boolean>(false);

  useEffect(() => {
    setGitSyncEnabled(app.gitSync ?? (app.sourceType === 'git'));
    if (app.gitAuthMode) setAuthMode(app.gitAuthMode as any);
    if (app.gitProviderId) setSelectedProviderId(app.gitProviderId);
    if (app.gitRepo) {
      setRepoUrl(app.gitRepo);
      setSelectedRepoFullName(app.gitRepo.replace(/^https?:\/\/(github|gitlab)\.com\//, '').replace(/\.git$/, ''));
    }
    if (app.gitBranch) setBranch(app.gitBranch);
    if (app.webhookAutoDeploy !== undefined) setAutoDeploy(Boolean(app.webhookAutoDeploy));
    if (app.webhookSecret) setWebhookSecret(app.webhookSecret);
  }, [app]);

  // Dynamic Repos & Branches fetched from /apps/:id/git/providers/:pid/repos & /branches
  const [providerRepos, setProviderRepos] = useState<RepoOption[]>([]);
  const [providerBranches, setProviderBranches] = useState<string[]>(app.gitBranch ? [app.gitBranch] : []);

  const filteredProviders = providers.filter((p) => {
    if (authMode === 'gitlab_token') return p.provider === 'GitLab';
    if (authMode === 'github_app') return p.provider === 'GitHub';
    return true;
  });

  const loadProviderBranches = async (pid: string, repoFull: string) => {
    if (!pid || !repoFull) return;
    setIsLoadingBranches(true);
    const res = await backendApi.gitProviderBranches(app.id, pid, repoFull);
    setIsLoadingBranches(false);
    if (res.ok) {
      const raw = (res.data as any)?.branches ?? [];
      const list: string[] = raw
        .map((b: any) => (typeof b === 'string' ? b : b?.name || ''))
        .filter(Boolean);
      setProviderBranches(list);
    }
  };

  const loadProviderRepos = async (pid: string, targetMode?: string) => {
    if (!pid) {
      setProviderRepos([]);
      return;
    }
    setIsLoadingRepos(true);
    const res = await backendApi.gitProviderRepos(app.id, pid);
    setIsLoadingRepos(false);
    if (res.ok) {
      const repos = res.data?.repos ?? [];
      setProviderRepos(repos);
      if (repos.length > 0) {
        const exists = repos.find(
          (r) => r.clone_url === repoUrl || r.full_name === selectedRepoFullName
        );
        const target = exists || repos[0];
        setSelectedRepoFullName(target.full_name);
        setRepoUrl(target.clone_url);
        setBranch((cur) => (cur && cur !== 'main' ? cur : target.default_branch || 'main'));
        void loadProviderBranches(pid, target.full_name);
      }
    }
  };

  // The connected providers come from the server, never from sample data.
  useEffect(() => {
    let live = true;
    void backendApi.gitProvidersList().then((res) => {
      if (!live || !res.ok) return;
      const list = (res.data?.providers ?? []).map((p) => ({
        id: String(p.id),
        provider: p.provider === 'gitlab' ? 'GitLab' : 'GitHub',
        accountName: p.accountName || p.name,
        authMethod: p.authMethod || (p.provider === 'gitlab' ? 'Token' : 'GitHub'),
      }));
      setProviders(list);

      // Auto-detect the matching authMode if not explicitly saved on app
      let nextMode = authMode;
      if (app.gitAuthMode) {
        nextMode = app.gitAuthMode as any;
      } else if (app.gitRepo) {
        if (app.gitRepo.includes('gitlab.com')) nextMode = 'gitlab_token';
        else if (app.gitRepo.includes('github.com')) nextMode = 'github_app';
      } else {
        const hasGitHub = list.some((p) => p.provider === 'GitHub');
        const hasGitLab = list.some((p) => p.provider === 'GitLab');
        if (!hasGitHub && hasGitLab) nextMode = 'gitlab_token';
        else if (hasGitHub && !hasGitLab) nextMode = 'github_app';
      }
      setAuthMode(nextMode);

      if (nextMode !== 'public') {
        const matching = list.filter((p) =>
          nextMode === 'gitlab_token' ? p.provider === 'GitLab' : p.provider === 'GitHub'
        );
        const preferredId = app.gitProviderId && matching.some((p) => p.id === app.gitProviderId)
          ? app.gitProviderId
          : matching[0]?.id || '';
        setSelectedProviderId(preferredId);
        if (preferredId) {
          void loadProviderRepos(preferredId, nextMode);
        }
      }
    });
    return () => {
      live = false;
    };
  }, [app.id, app.gitAuthMode, app.gitRepo, app.gitProviderId]);

  const handleSwitchAuthMode = (mode: 'public' | 'github_app' | 'gitlab_token') => {
    setAuthMode(mode);
    if (mode === 'public') {
      setSelectedProviderId('');
      setProviderRepos([]);
      return;
    }
    const matching = providers.filter((p) =>
      mode === 'gitlab_token' ? p.provider === 'GitLab' : p.provider === 'GitHub'
    );
    const nextPid = matching[0]?.id || '';
    setSelectedProviderId(nextPid);
    if (nextPid) {
      void loadProviderRepos(nextPid, mode);
    } else {
      setProviderRepos([]);
      setSelectedRepoFullName('');
      setRepoUrl('');
    }
  };

  const [isLoadingRepos, setIsLoadingRepos] = useState(false);
  const [isLoadingBranches, setIsLoadingBranches] = useState(false);
  const [isSyncingNow, setIsSyncingNow] = useState(false);

  // Force Copy Git Checkout Confirmation Modal (when pausing GitSync with existing workspace files)
  const [forceSyncModalOpen, setForceSyncModalOpen] = useState(false);
  const [forceSyncWarning, setForceSyncWarning] = useState('');

  useEffect(() => {
    if (authMode !== 'public' && selectedProviderId) {
      void loadProviderRepos(selectedProviderId);
    }
  }, [selectedProviderId]);

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
    if (!repoUrl.trim()) {
      toast.error('Please enter or select a repository clone URL');
      return;
    }
    const nextSync = app.gitRepo ? gitSyncEnabled : true;
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
    setGitSyncEnabled(nextSync);
    const savedSecret = (res.data as any)?.webhook_secret || webhookSecret;
    if (savedSecret) {
      setWebhookSecret(savedSecret);
    }
    onUpdateApp({
      ...app,
      sourceType: nextSync ? 'git' : 'compose',
      gitSync: nextSync,
      gitAuthMode: authMode,
      gitProviderId: selectedProviderId,
      gitRepo: repoUrl.trim(),
      gitBranch: branch.trim() || 'main',
      webhookAutoDeploy: autoDeploy,
      webhookSecret: savedSecret,
    });
    toast.success(
      app.gitRepo
        ? `Saved Git configuration (${branch.trim() || 'main'})`
        : `Connected Git repository (${branch.trim() || 'main'})`
    );
  };

  const handleDisconnectGit = async () => {
    await backendApi.deleteAppGit(app.id);
    await backendApi.switchAppSource(app.id, 'files');
    onUpdateApp({
      ...app,
      sourceType: 'compose',
      gitSync: false,
      gitRepo: undefined,
      webhookAutoDeploy: false,
    });
    toast.success('Disconnected Git repository — project is now in Pure Manual Files mode');
  };

  // The push endpoint is per project: the panel looks the project up by this id.
  const webhookEndpoint = authMode === 'gitlab_token' ? 'gitlab' : 'github';
  const webhookUrl = `${window.location.origin}/webhooks/${webhookEndpoint}/${encodeURIComponent(app.id)}`;

  return (
    <div className="space-y-4">
      {/* Row 1: Git Sync ON / OFF Control Bar OR Onboarding banner if no repo connected */}
      {!app.gitRepo ? (
        <div className="rounded-xl border border-orange-500/30 bg-orange-500/10 p-4 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-orange-500/30 bg-orange-500/15">
              <GitBranch className="h-4.5 w-4.5 text-orange-400" />
            </span>
            <div>
              <div className="text-xs font-semibold text-zinc-100">Connect a Git Repository</div>
              <div className="text-[11px] text-zinc-400 mt-0.5">
                Connect your GitHub or GitLab repository below to enable automated deployments and branch syncing.
              </div>
            </div>
          </div>
        </div>
      ) : (
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

            <button
              type="button"
              onClick={() => void handleDisconnectGit()}
              className="inline-flex h-8 items-center gap-1 rounded-lg border border-rose-500/25 bg-rose-500/10 px-2.5 text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Disconnect Git
            </button>
          </div>
        </div>
      )}

      {/* Row 2: Git Provider, Repository & Branch Configuration Form (POST /apps/:id/git) */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <GitBranch className="h-4 w-4 text-orange-400" />
            <h2 className="text-sm font-semibold text-zinc-100">
              {app.gitRepo ? 'Git Repository & Branch' : 'Configure Repository & Branch'}
            </h2>
            {app.gitRepo && (
              <StatusBadge
                status={gitSyncEnabled ? 'active' : 'stopped'}
                label={gitSyncEnabled ? 'Git Sync ON' : 'Git Sync OFF'}
              />
            )}
          </div>
          {app.gitRepo && (
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
          )}
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
                onClick={() => handleSwitchAuthMode(mode.id)}
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
                    Connected Git Provider
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
                {filteredProviders.length === 0 ? (
                  <div className="flex h-9 items-center justify-between rounded-lg border border-white/[0.09] bg-[#09090b] px-3 text-xs text-zinc-400">
                    <span>No {authMode === 'gitlab_token' ? 'GitLab' : 'GitHub'} provider connected</span>
                    <a
                      href="/git"
                      className="text-[11px] font-semibold text-orange-400 hover:underline"
                    >
                      + Connect {authMode === 'gitlab_token' ? 'GitLab' : 'GitHub'}
                    </a>
                  </div>
                ) : (
                  <select
                    value={selectedProviderId}
                    onChange={(e) => {
                      setSelectedProviderId(e.target.value);
                      void loadProviderRepos(e.target.value);
                    }}
                    className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                  >
                    {filteredProviders.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.provider} · {p.accountName} ({p.authMethod})
                      </option>
                    ))}
                  </select>
                )}
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
                      const domain = authMode === 'gitlab_token' ? 'gitlab.com' : 'github.com';
                      setRepoUrl(`https://${domain}/${full}.git`);
                    }
                    void loadProviderBranches(selectedProviderId, full);
                  }}
                  className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
                >
                  {providerRepos.length === 0 && <option value="">No repositories found</option>}
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
                placeholder={
                  authMode === 'gitlab_token'
                    ? 'https://gitlab.com/username/project.git'
                    : 'https://github.com/org/repo.git'
                }
                className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-[11px] font-medium text-zinc-400">Target Branch</label>
                {authMode !== 'public' && (
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
                {providerBranches.map((b) => {
                  const name = typeof b === 'string' ? b : (b as any)?.name || String(b);
                  return <option key={name} value={name} />;
                })}
              </datalist>
            </div>
          </div>

          {/* Quick Branch Pills */}
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-zinc-500 mr-1">Branches:</span>
            {providerBranches.map((br) => {
              const name = typeof br === 'string' ? br : (br as any)?.name || String(br);
              return (
                <button
                  key={name}
                  type="button"
                  onClick={() => setBranch(name)}
                  className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-[11px] cursor-pointer ${
                    branch === name
                      ? 'border-orange-500/50 bg-orange-500/15 text-orange-300 font-semibold'
                      : 'border-white/[0.07] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  <GitBranch className="h-3 w-3" />
                  {name}
                </button>
              );
            })}
          </div>

          {/* Webhook URL, Secret Token & Auto-Deploy Toggle */}
          <div className="space-y-2.5 pt-1">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
                <div className="flex items-center gap-2.5 min-w-0">
                  <Webhook className="h-4 w-4 text-orange-400 shrink-0" />
                  <div className="min-w-0">
                    <div className="text-xs font-medium text-zinc-200">Push Webhook Endpoint</div>
                    <div className="truncate font-mono text-[11px] text-zinc-500">{webhookUrl}</div>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={async () => {
                    const ok = await copyToClipboard(webhookUrl);
                    if (ok) toast.success('Copied webhook URL');
                  }}
                  className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer shrink-0"
                >
                  Copy
                </button>
              </div>

              <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
                <div className="flex items-center gap-2.5 min-w-0">
                  <Key className="h-4 w-4 text-amber-400 shrink-0" />
                  <div className="min-w-0">
                    <div className="text-xs font-medium text-zinc-200">Webhook Secret Token</div>
                    <div className="truncate font-mono text-[11px] text-zinc-400">
                      {webhookSecret ? (showSecret ? webhookSecret : '••••••••••••••••') : 'Auto-generated on save'}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-1.5 shrink-0">
                  {webhookSecret && (
                    <button
                      type="button"
                      onClick={() => setShowSecret((v) => !v)}
                      className="rounded-lg border border-white/[0.08] bg-zinc-900 p-1.5 text-zinc-400 hover:text-zinc-200 cursor-pointer"
                      title={showSecret ? 'Hide secret' : 'Show secret'}
                    >
                      {showSecret ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                    </button>
                  )}
                  <button
                    type="button"
                    disabled={!webhookSecret}
                    onClick={async () => {
                      if (!webhookSecret) return;
                      const ok = await copyToClipboard(webhookSecret);
                      if (ok) toast.success('Copied Webhook Secret Token');
                    }}
                    className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer disabled:opacity-40"
                  >
                    Copy
                  </button>
                </div>
              </div>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
              <div>
                <div className="text-xs font-medium text-zinc-200">
                  Auto-Deploy on Push
                </div>
                <div className="text-[11px] text-zinc-500">
                  Redeploy automatically when commits are pushed to {branch}
                </div>
              </div>
              <ShadcnSwitch checked={autoDeploy} onChange={setAutoDeploy} />
            </div>

            <div className="rounded-lg border border-white/[0.05] bg-white/[0.02] p-2.5 text-[11px] text-zinc-400 leading-relaxed">
              <span className="font-semibold text-zinc-300">GitLab Webhook Setup: </span>
              In GitLab, go to <span className="text-zinc-200 font-medium">Settings ➔ Webhooks ➔ Add new webhook</span>. Paste the <span className="text-orange-400 font-mono">Push Webhook Endpoint</span> into <strong>URL</strong>, and paste the <span className="text-amber-400 font-mono">Webhook Secret Token</span> into <strong>Secret token</strong>. Check <strong>Push events</strong>.
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="submit"
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-orange-500 px-4 text-xs font-semibold text-white hover:bg-orange-400 cursor-pointer"
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              {app.gitRepo ? 'Save Git Configuration' : 'Connect Repository'}
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
