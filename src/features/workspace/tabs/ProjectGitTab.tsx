import React, { useEffect, useState } from 'react';
import {
  GitBranch,
  RefreshCw,
  Webhook,
  Trash2,
  CheckCircle2,
  AlertTriangle,
  Key,
  Eye,
  EyeOff,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { ShadcnDialog, ShadcnSwitch } from '../../../components/ui/Primitives';
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
  const generateRandomSecret = () => {
    const bytes = new Uint8Array(16);
    if (typeof window !== 'undefined' && window.crypto) {
      window.crypto.getRandomValues(bytes);
      return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
    }
    return Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);
  };
  const [webhookSecret, setWebhookSecret] = useState<string>(() => app.webhookSecret || generateRandomSecret());
  const [showSecret, setShowSecret] = useState<boolean>(false);
  const [isSavingGit, setIsSavingGit] = useState<boolean>(false);

  useEffect(() => {
    setGitSyncEnabled(app.gitSync ?? (app.sourceType === 'git'));
    setAuthMode((app.gitAuthMode as any) || 'github_app');
    setSelectedProviderId(app.gitProviderId || '');
    const nextRepo = app.gitRepo || '';
    setRepoUrl(nextRepo);
    setSelectedRepoFullName(
      nextRepo ? nextRepo.replace(/^https?:\/\/(github|gitlab)\.com\//, '').replace(/\.git$/, '') : ''
    );
    setBranch(app.gitBranch || 'main');
    setAutoDeploy(Boolean(app.webhookAutoDeploy));
    setWebhookSecret(app.webhookSecret || generateRandomSecret());
  }, [app.id]);

  // Dynamic Repos & Branches fetched from /apps/:id/git/providers/:pid/repos & /branches
  const [providerRepos, setProviderRepos] = useState<RepoOption[]>([]);
  const [providerBranches, setProviderBranches] = useState<string[]>(app.gitBranch ? [app.gitBranch] : []);

  const filteredProviders = providers.filter((p) => {
    if (authMode === 'gitlab_token') return p.provider === 'GitLab';
    if (authMode === 'github_app') return p.provider === 'GitHub';
    return true;
  });

  const savedAuthMode: 'public' | 'github_app' | 'gitlab_token' = app.gitAuthMode
    ? (app.gitAuthMode as 'public' | 'github_app' | 'gitlab_token')
    : app.gitRepo
      ? !app.gitProviderId
        ? 'public'
        : app.gitRepo.includes('gitlab.com')
          ? 'gitlab_token'
          : 'github_app'
      : 'github_app';

  const getRepoFullNameFromUrl = (url: string) =>
    url
      .trim()
      .replace(/^https?:\/\/(github|gitlab)\.com\//i, '')
      .replace(/\.git$/i, '');

  const normalizeRepoKey = (raw: string) =>
    raw.trim().replace(/\.git$/i, '').toLowerCase();

  const loadProviderBranches = async (pid: string, repoFull: string) => {
    if (!pid || !repoFull) return;
    setIsLoadingBranches(true);
    try {
      const res = await backendApi.gitProviderBranches(app.id, pid, repoFull);
      if (res.ok) {
        const raw = (res.data as any)?.branches ?? [];
        const list: string[] = raw
          .map((b: any) => (typeof b === 'string' ? b : b?.name || ''))
          .filter(Boolean);
        setProviderBranches(list);
      }
    } finally {
      setIsLoadingBranches(false);
    }
  };

  const loadPublicBranches = async (targetUrl: string, silent = false) => {
    const cleaned = targetUrl.trim();
    if (!cleaned) {
      if (!silent) toast.error('Enter a repository URL first');
      return;
    }
    setIsLoadingBranches(true);
    try {
      const res = await backendApi.publicGitRemoteBranches(app.id, cleaned);
      if (res.ok && Array.isArray(res.data?.branches)) {
        setProviderBranches(res.data.branches);
      } else if (!silent) {
        toast.error(res.error || 'Could not fetch remote branches');
      }
    } finally {
      setIsLoadingBranches(false);
    }
  };

  const loadProviderRepos = async (
    pid: string,
    currentRepoUrl = repoUrl,
    currentFullName = selectedRepoFullName,
    isCancelled?: () => boolean,
    switchToPublicIfMissing = false
  ) => {
    if (!pid) {
      setProviderRepos([]);
      setSelectedRepoFullName('');
      return;
    }
    setIsLoadingRepos(true);
    try {
      const res = await backendApi.gitProviderRepos(app.id, pid);
      if (isCancelled?.()) return;
      if (res.ok) {
        const repos = res.data?.repos ?? [];
        setProviderRepos(repos);
        if (repos.length === 0) {
          setSelectedRepoFullName('');
          setProviderBranches([]);
          if (!switchToPublicIfMissing) {
            setRepoUrl('');
            setBranch('main');
          }
          return;
        }
        const normUrl = normalizeRepoKey(currentRepoUrl);
        const normFull = currentFullName.trim().toLowerCase();
        const exists = repos.find(
          (r) =>
            (normUrl && normalizeRepoKey(r.clone_url) === normUrl) ||
            (normFull && r.full_name.toLowerCase() === normFull)
        );
        if (exists) {
          setSelectedRepoFullName(exists.full_name);
          setRepoUrl(exists.clone_url);
          void loadProviderBranches(pid, exists.full_name);
        } else if (switchToPublicIfMissing && normUrl && repos.length < 100) {
          setAuthMode('public');
          setSelectedProviderId('');
          setProviderRepos([]);
          setSelectedRepoFullName('');
          void loadPublicBranches(currentRepoUrl, true);
        } else if (!normUrl) {
          const target = repos[0];
          setSelectedRepoFullName(target.full_name);
          setRepoUrl(target.clone_url);
          setBranch(target.default_branch || 'main');
          void loadProviderBranches(pid, target.full_name);
        } else {
          setSelectedRepoFullName('');
        }
      }
    } finally {
      if (!isCancelled?.()) setIsLoadingRepos(false);
    }
  };

  // Connected providers come from the server.
  useEffect(() => {
    let live = true;
    void backendApi.gitProvidersList().then((res) => {
      if (!live || !res.ok) return;
      const list = (res.data?.providers ?? []).map((p) => ({
        id: String(p.id),
        provider: p.provider === 'gitlab' ? 'GitLab' : 'GitHub',
        accountName: p.account_login || p.accountName || p.name,
        authMethod: p.auth_method || p.authMethod || (p.provider === 'gitlab' ? 'Token' : 'GitHub'),
      }));
      setProviders(list);

      let nextMode: 'public' | 'github_app' | 'gitlab_token' = authMode;
      if (app.gitAuthMode) {
        nextMode = app.gitAuthMode as 'public' | 'github_app' | 'gitlab_token';
      } else if (app.gitRepo) {
        // A repo without a linked provider was added as a plain public URL.
        if (!app.gitProviderId) nextMode = 'public';
        else if (app.gitRepo.includes('gitlab.com')) nextMode = 'gitlab_token';
        else nextMode = 'github_app';
      } else {
        const hasGitHub = list.some((p) => p.provider === 'GitHub');
        const hasGitLab = list.some((p) => p.provider === 'GitLab');
        if (!hasGitHub && hasGitLab) nextMode = 'gitlab_token';
        else if (hasGitHub && !hasGitLab) nextMode = 'github_app';
      }
      setAuthMode(nextMode);
      if (!app.gitRepo && app.webhookAutoDeploy === undefined) {
        setAutoDeploy(nextMode !== 'public');
      }

      if (nextMode !== 'public') {
        const matching = list.filter((p) =>
          nextMode === 'gitlab_token' ? p.provider === 'GitLab' : p.provider === 'GitHub'
        );
        const preferredId =
          app.gitProviderId && matching.some((p) => p.id === app.gitProviderId)
            ? app.gitProviderId
            : matching[0]?.id || '';
        setSelectedProviderId(preferredId);
        if (preferredId) {
          const initialFull = app.gitRepo
            ? app.gitRepo.replace(/^https?:\/\/(github|gitlab)\.com\//, '').replace(/\.git$/, '')
            : '';
          void loadProviderRepos(preferredId, app.gitRepo || '', initialFull, () => !live, Boolean(app.gitRepo));
        }
      }
    });
    return () => {
      live = false;
    };
  }, [app.id, app.gitAuthMode, app.gitRepo, app.gitProviderId]);

  const handleSwitchAuthMode = (mode: 'public' | 'github_app' | 'gitlab_token') => {
    if (mode === authMode) return;
    setAuthMode(mode);
    const returningToSaved = Boolean(app.gitRepo) && mode === savedAuthMode;
    if (!app.gitRepo) {
      setAutoDeploy(mode !== 'public');
    }

    if (mode === 'public') {
      setSelectedProviderId('');
      setProviderRepos([]);
      setSelectedRepoFullName('');
      if (returningToSaved && app.gitRepo) {
        setRepoUrl(app.gitRepo);
        setBranch(app.gitBranch || 'main');
        setProviderBranches(app.gitBranch ? [app.gitBranch] : []);
        void loadPublicBranches(app.gitRepo, true);
      } else {
        setRepoUrl('');
        setBranch('main');
        setProviderBranches([]);
      }
      return;
    }

    const matching = providers.filter((p) =>
      mode === 'gitlab_token' ? p.provider === 'GitLab' : p.provider === 'GitHub'
    );
    const preferredPid =
      returningToSaved && app.gitProviderId && matching.some((p) => p.id === app.gitProviderId)
        ? app.gitProviderId
        : matching[0]?.id || '';
    setSelectedProviderId(preferredPid);

    if (returningToSaved && app.gitRepo && preferredPid) {
      const savedFull = getRepoFullNameFromUrl(app.gitRepo);
      setRepoUrl(app.gitRepo);
      setSelectedRepoFullName(savedFull);
      setBranch(app.gitBranch || 'main');
      setProviderBranches(app.gitBranch ? [app.gitBranch] : []);
      void loadProviderRepos(preferredPid, app.gitRepo, savedFull);
      return;
    }

    setProviderRepos([]);
    setSelectedRepoFullName('');
    setRepoUrl('');
    setBranch('main');
    setProviderBranches([]);
    if (preferredPid) {
      void loadProviderRepos(preferredPid, '', '');
    }
  };

  const [isLoadingRepos, setIsLoadingRepos] = useState(false);
  const [isLoadingBranches, setIsLoadingBranches] = useState(false);
  const [isSyncingNow, setIsSyncingNow] = useState(false);

  // Force Confirmation Modal (both enabling with dirty git checkout or pausing with existing workspace files)
  const [forceSyncModalOpen, setForceSyncModalOpen] = useState(false);
  const [forceSyncTarget, setForceSyncTarget] = useState(false);
  const [forceSyncWarning, setForceSyncWarning] = useState('');
  const [isTogglingSync, setIsTogglingSync] = useState(false);

  const handleToggleGitSync = async (nextEnabled: boolean, force = false) => {
    if (isTogglingSync) return;
    setIsTogglingSync(true);
    try {
      const res = await backendApi.setGitSync(app.id, nextEnabled, force);
      if (res.data?.confirm_needed && !force) {
        setForceSyncTarget(nextEnabled);
        setForceSyncWarning(
          res.data.warning ||
            (nextEnabled
              ? 'The Git checkout has uncommitted local edits that the next Git sync would discard.'
              : 'Workspace already contains local files. Pausing Git Sync will copy the Git checkout into the workspace and overwrite matching files.')
        );
        setForceSyncModalOpen(true);
        return;
      }
      if (!res.ok && !res.offlineFallback) {
        toast.error(res.error || 'Failed to update Git Sync');
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
    } finally {
      setIsTogglingSync(false);
    }
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
    if (authMode !== 'public' && !canSubmitGit) {
      toast.error(
        providerRepos.length === 0
          ? 'No repositories available for this provider — grant repo access or switch provider'
          : 'Please select a repository from this account'
      );
      return;
    }
    if (!repoUrl.trim()) {
      toast.error('Please enter a repository clone URL');
      return;
    }
    if (isSavingGit) return;
    setIsSavingGit(true);
    try {
      const nextSync = app.gitRepo ? gitSyncEnabled : true;
      const res = await backendApi.saveAppGit(app.id, {
        auth_mode: authMode,
        git_provider_id: authMode === 'public' ? '' : selectedProviderId,
        repo_url: repoUrl.trim(),
        branch: branch.trim() || 'main',
        auto_deploy: autoDeploy,
        webhook_secret: webhookSecret,
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
    } finally {
      setIsSavingGit(false);
    }
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
    toast.success('Disconnected Git repository — switched app to workspace files mode');
  };

  const selectedProvider = providers.find((p) => p.id === selectedProviderId);
  // The saved repo may not belong to this account (e.g. it was added as a public URL);
  // then the picker shows a placeholder instead of silently showing the first repo.
  const selectedRepoListed = providerRepos.some((r) => r.full_name === selectedRepoFullName);
  const canSubmitGit =
    authMode === 'public'
      ? Boolean(repoUrl.trim())
      : filteredProviders.length > 0 &&
        Boolean(selectedProviderId) &&
        providerRepos.length > 0 &&
        selectedRepoListed &&
        Boolean(repoUrl.trim());
  const isDirty = Boolean(
    app.gitRepo &&
      (authMode !== savedAuthMode ||
        (authMode !== 'public' && selectedProviderId !== (app.gitProviderId || '')) ||
        repoUrl.trim() !== (app.gitRepo || '').trim() ||
        (branch.trim() || 'main') !== (app.gitBranch || 'main').trim() ||
        autoDeploy !== Boolean(app.webhookAutoDeploy))
  );
  const isGitHubAppInstall =
    authMode === 'github_app' && selectedProvider?.authMethod === 'GitHub App';
  const showManualWebhook = autoDeploy;
  const normalizedUrlLower = repoUrl.trim().toLowerCase();
  const publicWebhookKind: 'github' | 'gitlab' | 'generic' = normalizedUrlLower.includes('gitlab')
    ? 'gitlab'
    : normalizedUrlLower.includes('github') || !normalizedUrlLower
      ? 'github'
      : 'generic';
  const webhookPath =
    authMode === 'gitlab_token'
      ? `webhooks/gitlab/${encodeURIComponent(app.id)}`
      : authMode === 'github_app'
        ? `webhooks/github/${encodeURIComponent(app.id)}`
        : publicWebhookKind === 'gitlab'
          ? `webhooks/gitlab/${encodeURIComponent(app.id)}`
          : publicWebhookKind === 'github'
            ? `webhooks/github/${encodeURIComponent(app.id)}`
            : `webhooks/${encodeURIComponent(app.id)}`;
  const webhookUrl = `${window.location.origin}/${webhookPath}`;

  return (
    <div className="space-y-4 pb-24">
      {/* Row 1: Git Sync ON / OFF Control Bar OR Onboarding banner if no repo connected */}
      {!app.gitRepo ? (
        <div className="rounded-xl border border-orange-500/30 bg-orange-500/10 p-4 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-orange-500/30 bg-orange-500/15">
              <GitBranch className="h-4.5 w-4.5 text-orange-400" />
            </span>
            <div>
              <div className="text-xs font-semibold text-zinc-100">Connect a Repository</div>
              <div className="text-[11px] text-zinc-400 mt-0.5">
                Link a GitHub or GitLab repository to deploy from a branch.
              </div>
            </div>
          </div>
        </div>
      ) : (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2">
              <span className="text-xs font-semibold text-zinc-200">Git Sync</span>
              <ShadcnSwitch
                checked={gitSyncEnabled}
                onChange={(next) => void handleToggleGitSync(next)}
              />
            </div>
            <span
              className={`rounded-md border px-2 py-0.5 font-mono text-[11px] font-medium ${
                gitSyncEnabled
                  ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                  : 'border-amber-500/30 bg-amber-500/10 text-amber-300'
              }`}
            >
              {gitSyncEnabled
                ? `Auto-pulls ${app.gitBranch || branch} on deploy`
                : 'Paused · Workspace files active'}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => void handleManualSyncNow()}
              disabled={isSyncingNow}
              title={isDirty ? 'Save Git configuration before syncing new changes' : undefined}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-orange-500/35 bg-orange-500/12 px-3 text-xs font-semibold text-orange-300 hover:bg-orange-500/20 disabled:opacity-50 cursor-pointer"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${isSyncingNow ? 'animate-spin' : ''}`} />
              {isSyncingNow ? 'Syncing...' : 'Sync Now'}
            </button>

            <button
              type="button"
              onClick={() => void handleDisconnectGit()}
              className="inline-flex h-8 items-center gap-1 rounded-lg border border-rose-500/25 bg-rose-500/10 px-2.5 text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Disconnect
            </button>
          </div>
        </div>
      )}

      {/* Row 2: Git Provider, Repository & Branch Configuration Form */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <GitBranch className="h-4 w-4 text-orange-400" />
            <h2 className="text-sm font-semibold text-zinc-100">
              {app.gitRepo ? 'Repository & Branch' : 'Configure Repository & Branch'}
            </h2>
          </div>
          {isDirty && (
            <span className="rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300">
              Unsaved changes
            </span>
          )}
        </div>

        <form onSubmit={handleSaveGitConfig} className="space-y-4">
          {/* Auth Mode Selector */}
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
            {(
              [
                {
                  id: 'github_app',
                  label: 'Connected GitHub',
                  sub: 'Linked account, repos & auto-deploy',
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
                <div className="mt-0.5 text-[10px] text-zinc-400">{mode.sub}</div>
              </button>
            ))}
          </div>

          {/* Connected Provider + Live Repository Picker */}
          {authMode !== 'public' && (
            <div className="space-y-2">
              <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <label className="text-[11px] font-medium text-zinc-400 leading-normal">
                      Connected Git Provider
                    </label>
                    <button
                      type="button"
                      disabled={!selectedProviderId || isLoadingRepos}
                      onClick={() =>
                        void loadProviderRepos(
                          selectedProviderId,
                          repoUrl,
                          selectedRepoFullName
                        )
                      }
                      className="inline-flex items-center gap-1 py-0.5 font-mono text-[11px] leading-normal text-orange-400 hover:underline disabled:opacity-40 cursor-pointer disabled:cursor-not-allowed"
                    >
                      <RefreshCw className={`h-3 w-3 ${isLoadingRepos ? 'animate-spin' : ''}`} />
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
                        const nextPid = e.target.value;
                        setSelectedProviderId(nextPid);
                        if (
                          nextPid &&
                          nextPid === app.gitProviderId &&
                          authMode === savedAuthMode &&
                          app.gitRepo
                        ) {
                          const savedFull = getRepoFullNameFromUrl(app.gitRepo);
                          setRepoUrl(app.gitRepo);
                          setSelectedRepoFullName(savedFull);
                          setBranch(app.gitBranch || 'main');
                          void loadProviderRepos(nextPid, app.gitRepo, savedFull);
                        } else {
                          setProviderRepos([]);
                          setSelectedRepoFullName('');
                          setRepoUrl('');
                          setBranch('main');
                          setProviderBranches([]);
                          void loadProviderRepos(nextPid, '', '');
                        }
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
                  <label className="block text-[11px] font-medium text-zinc-400 leading-normal py-0.5 mb-1.5">
                    Select Repository ({providerRepos.length} available)
                  </label>
                  <select
                    value={selectedRepoListed ? selectedRepoFullName : ''}
                    disabled={filteredProviders.length === 0 || providerRepos.length === 0}
                    onChange={(e) => {
                      const full = e.target.value;
                      if (!full) return;
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
                    className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none disabled:opacity-60"
                  >
                    {providerRepos.length === 0 ? (
                      <option value="">No repositories found</option>
                    ) : (
                      !selectedRepoListed && (
                        <option value="">
                          Select a repository from this account
                        </option>
                      )
                    )}
                    {providerRepos.map((r) => (
                      <option key={r.full_name} value={r.full_name}>
                        {r.full_name} {r.private ? '(Private)' : '(Public)'}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              {filteredProviders.length > 0 && !isLoadingRepos && providerRepos.length === 0 && (
                <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-200">
                  <span>
                    No repositories are accessible on this{' '}
                    {authMode === 'gitlab_token' ? 'GitLab' : 'GitHub'} connection. Grant repository
                    access or refresh after updating permissions.
                  </span>
                  <a
                    href="/git"
                    className="font-semibold text-orange-300 hover:underline shrink-0"
                  >
                    Manage Git Providers →
                  </a>
                </div>
              )}
            </div>
          )}

          {/* Repo URL & Branch Selector */}
          <div className="grid grid-cols-1 gap-3.5 md:grid-cols-3">
            <div className="md:col-span-2">
              <label className="block text-[11px] font-medium text-zinc-400 leading-normal py-0.5 mb-1.5">
                Repository Clone URL
              </label>
              <input
                type="text"
                required
                readOnly={authMode !== 'public'}
                value={repoUrl}
                onChange={(e) => setRepoUrl(e.target.value)}
                placeholder={
                  authMode !== 'public'
                    ? 'Select a repository above to populate its clone URL'
                    : 'https://github.com/org/repo.git'
                }
                className={`h-9 w-full rounded-lg border border-white/[0.09] px-3 font-mono text-xs focus:border-orange-500 focus:outline-none ${
                  authMode !== 'public'
                    ? 'bg-[#09090b]/60 text-zinc-300 cursor-default'
                    : 'bg-[#09090b] text-zinc-100'
                }`}
              />
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-[11px] font-medium text-zinc-400 leading-normal">
                  Target Branch
                </label>
                <button
                  type="button"
                  disabled={
                    isLoadingBranches ||
                    (authMode === 'public' ? !repoUrl.trim() : !selectedRepoListed)
                  }
                  onClick={() =>
                    void (authMode === 'public'
                      ? loadPublicBranches(repoUrl)
                      : loadProviderBranches(selectedProviderId, selectedRepoFullName))
                  }
                  className="inline-flex items-center gap-1 py-0.5 font-mono text-[11px] leading-normal text-orange-400 hover:underline disabled:opacity-40 cursor-pointer disabled:cursor-not-allowed"
                >
                  <RefreshCw
                    className={`h-3 w-3 ${isLoadingBranches ? 'animate-spin' : ''}`}
                  />
                  Fetch Branches
                </button>
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
          {providerBranches.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[11px] text-zinc-400 mr-1">Branches:</span>
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
          )}

          {/* Auto-Deploy & Conditional Webhook Configuration */}
          <div className="space-y-2.5 pt-1">
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
              <div>
                <div className="text-xs font-medium text-zinc-200">
                  Auto-Deploy on Push
                </div>
                <div className="text-[11px] text-zinc-400">
                  {isGitHubAppInstall
                    ? `Handled automatically by your connected GitHub App for ${branch}.`
                    : authMode === 'github_app'
                      ? `Connected via GitHub Token/OAuth. To receive push events for ${branch}, add the Webhook below in your GitHub repository.`
                      : authMode === 'public' && !autoDeploy
                        ? 'Public repositories deploy without requiring a webhook.'
                        : `Redeploy automatically when commits are pushed to ${branch}. Add the Webhook below in your repository settings.`}
                </div>
              </div>
              <ShadcnSwitch checked={autoDeploy} onChange={setAutoDeploy} />
            </div>

            {showManualWebhook &&
              (() => {
                const webhookBody = (
                  <>
                    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                      <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] px-3.5 py-2.5">
                        <div className="flex items-center gap-2.5 min-w-0">
                          <Webhook className="h-4 w-4 text-orange-400 shrink-0" />
                          <div className="min-w-0">
                            <div className="text-xs font-medium text-zinc-200">
                              Push Webhook Endpoint
                            </div>
                            <div className="truncate font-mono text-[11px] text-zinc-400">
                              {webhookUrl}
                            </div>
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
                            <div className="text-xs font-medium text-zinc-200">
                              Webhook Secret Token
                            </div>
                            <div className="truncate font-mono text-[11px] text-zinc-400">
                              {webhookSecret
                                ? showSecret
                                  ? webhookSecret
                                  : '••••••••••••••••'
                                : 'Auto-generated on save'}
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
                              {showSecret ? (
                                <EyeOff className="h-3.5 w-3.5" />
                              ) : (
                                <Eye className="h-3.5 w-3.5" />
                              )}
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

                    <div className="rounded-lg border border-white/[0.05] bg-white/[0.02] p-2.5 text-[11px] text-zinc-400 leading-relaxed">
                      {authMode === 'gitlab_token' ||
                      (authMode === 'public' && publicWebhookKind === 'gitlab') ? (
                        <>
                          <span className="font-semibold text-zinc-300">
                            GitLab Webhook Setup:{' '}
                          </span>
                          In GitLab, go to{' '}
                          <span className="text-zinc-200 font-medium">
                            Settings → Webhooks → Add new webhook
                          </span>
                          , set <strong>URL</strong> to the Push Webhook Endpoint, set{' '}
                          <strong>Secret token</strong>, and select <strong>Push events</strong>.
                        </>
                      ) : authMode === 'public' && publicWebhookKind === 'generic' ? (
                        <>
                          <span className="font-semibold text-zinc-300">Push Webhook Setup: </span>
                          Point your Git host webhook to the Push Webhook Endpoint above with the{' '}
                          <strong>Secret</strong> (supports GitHub{' '}
                          <span className="font-mono text-zinc-200">X-Hub-Signature-256</span> and
                          GitLab <span className="font-mono text-zinc-200">X-Gitlab-Token</span>{' '}
                          push events).
                        </>
                      ) : (
                        <>
                          <span className="font-semibold text-zinc-300">
                            GitHub Webhook Setup:{' '}
                          </span>
                          In your GitHub repository{' '}
                          <span className="text-zinc-200 font-medium">
                            Settings → Webhooks → Add webhook
                          </span>
                          , set <strong>Payload URL</strong> to the Push Webhook Endpoint above,
                          choose <span className="font-mono text-zinc-200">application/json</span>,
                          paste the <strong>Secret</strong>, and select{' '}
                          <strong>Just the push event</strong>.
                        </>
                      )}
                    </div>
                  </>
                );

                if (isGitHubAppInstall) {
                  return (
                    <details className="rounded-lg border border-white/[0.06] bg-[#09090b]/70 px-3.5 py-2.5">
                      <summary className="cursor-pointer text-[11px] font-medium text-zinc-300 select-none">
                        Optional: Manual Webhook Fallback (already handled automatically by GitHub
                        App)
                      </summary>
                      <div className="mt-3 space-y-2.5">
                        <div className="text-[11px] text-amber-200/90">
                          Only add a manual repo webhook if GitHub App push events are disabled;
                          enabling both may trigger duplicate deploys on push.
                        </div>
                        {webhookBody}
                      </div>
                    </details>
                  );
                }

                return webhookBody;
              })()}
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3 sm:pr-28">
            <button
              type="submit"
              disabled={isSavingGit || !canSubmitGit}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-orange-500 px-4 text-xs font-semibold text-white hover:bg-orange-400 disabled:opacity-60 cursor-pointer disabled:cursor-not-allowed"
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              {isSavingGit
                ? 'Saving...'
                : app.gitRepo
                  ? 'Save Git Configuration'
                  : 'Connect Repository'}
            </button>
          </div>
        </form>
      </div>

      {/* Force Git Sync Confirmation Modal */}
      <ShadcnDialog
        open={forceSyncModalOpen}
        onClose={() => !isTogglingSync && setForceSyncModalOpen(false)}
        title={
          forceSyncTarget
            ? 'Discard Uncommitted Edits & Enable Git Sync?'
            : 'Copy Git Checkout into Workspace?'
        }
        icon={<AlertTriangle className="h-4 w-4 text-amber-400" />}
      >
        <div className="space-y-4">
          <p className="text-xs text-zinc-300 leading-relaxed">{forceSyncWarning}</p>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              disabled={isTogglingSync}
              onClick={() => setForceSyncModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 disabled:opacity-50 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={isTogglingSync}
              onClick={() => void handleToggleGitSync(forceSyncTarget, true)}
              className="rounded-lg bg-orange-500 px-4 py-1.5 text-xs font-semibold text-white hover:bg-orange-400 disabled:opacity-60 cursor-pointer disabled:cursor-not-allowed"
            >
              {isTogglingSync
                ? 'Updating...'
                : forceSyncTarget
                  ? 'Overwrite & Enable Git Sync'
                  : 'Overwrite & Pause Git Sync'}
            </button>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
};
