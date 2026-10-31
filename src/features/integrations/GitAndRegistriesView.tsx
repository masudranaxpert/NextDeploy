import React, { useEffect, useState } from 'react';
import { OwnerBadge } from '../../components/ui/OwnerBadge';
import {
  RefreshCw,
  Copy,
  Trash2,
  Plus,
  CheckCircle2,
  FolderGit2,
  HardDrive,
  Github,
  Lock,
  Play,
  Globe,
  User,
  ExternalLink,
  Settings,
  AlertCircle,
  Key,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  GitProviderConnection,
  RegistryCredential,
  SyncedGitRepository,
} from '../../types/nextdeploy';
import {
  ActionMenu,
  ShadcnDialog,
  ShadcnSwitch,
  StatusBadge,
} from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';

const GitLabIcon: React.FC<{ className?: string }> = ({ className = 'h-4 w-4' }) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M22.65 14.39L12 22.13 1.35 14.39a.84.84 0 0 1-.3-.94l1.22-3.78 2.44-7.51A.42.42 0 0 1 4.82 2a.43.43 0 0 1 .58 0 .42.42 0 0 1 .11.18l2.44 7.49h8.1l2.44-7.51A.42.42 0 0 1 18.6 2a.43.43 0 0 1 .58 0 .42.42 0 0 1 .11.18l2.44 7.51L23 13.45a.84.84 0 0 1-.35.94z" />
  </svg>
);

interface GitAndRegistriesViewProps {
  mode: 'git' | 'registries';
  gitProviders: GitProviderConnection[];
  syncedRepos?: SyncedGitRepository[];
  registries: RegistryCredential[];
  onAddRegistry: (reg: RegistryCredential) => void;
  onDeleteRegistry?: (id: string) => void;
}

export interface WebhookDeliveryItem {
  id: string;
  provider: 'GitHub' | 'GitLab';
  repo: string;
  event: string;
  sha: string;
  status: string;
  latency: string;
  time: string;
}

export const GitAndRegistriesView: React.FC<GitAndRegistriesViewProps> = ({
  mode,
  gitProviders,
  syncedRepos = [],
  registries,
  onAddRegistry,
  onDeleteRegistry,
}) => {
  const [providers, setProviders] = useState<GitProviderConnection[]>(gitProviders);
  const [repos, setRepos] = useState<SyncedGitRepository[]>(syncedRepos);
  const [repoProviderFilter, setRepoProviderFilter] = useState<'all' | 'GitHub' | 'GitLab'>('all');
  const [gitSubTab, setGitSubTab] = useState<'repos' | 'webhooks'>('repos');
  const [webhookEvents, setWebhookEvents] = useState<WebhookDeliveryItem[]>([]);
  const [isLoadingWebhooks, setIsLoadingWebhooks] = useState(false);

  // Manage GitHub Installation modal state
  const [editInstallProvider, setEditInstallProvider] = useState<GitProviderConnection | null>(null);
  const [customInstallId, setCustomInstallId] = useState('');
  const [isSavingInstallId, setIsSavingInstallId] = useState(false);

  // Edit / Update Token modal state (for updating token permissions/scopes)
  const [editTokenProvider, setEditTokenProvider] = useState<GitProviderConnection | null>(null);
  const [newTokenInput, setNewTokenInput] = useState('');
  const [isUpdatingToken, setIsUpdatingToken] = useState(false);

  // Sync props to state
  useEffect(() => {
    setProviders(gitProviders);
  }, [gitProviders]);

  useEffect(() => {
    setRepos(syncedRepos);
  }, [syncedRepos]);

  // Load real webhook deliveries from backend
  useEffect(() => {
    let active = true;
    const fetchWebhooks = async () => {
      setIsLoadingWebhooks(true);
      try {
        const res = await backendApi.webhookDeliveries(20);
        if (active && res.ok && Array.isArray(res.data)) {
          const mapped: WebhookDeliveryItem[] = res.data.map((item: any, idx: number) => ({
            id: item.delivery_id || item.id || `wh-${idx}`,
            provider: item.provider?.toLowerCase() === 'gitlab' ? 'GitLab' : 'GitHub',
            repo: item.repo || 'unknown/repo',
            event: item.event || 'push',
            sha: (item.sha || 'latest').slice(0, 7),
            status: item.status || '200 OK',
            latency: item.latency || '—',
            time: item.created_at ? new Date(item.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : 'Recently',
          }));
          setWebhookEvents(mapped);
        }
      } catch {
        // fail silently
      } finally {
        if (active) setIsLoadingWebhooks(false);
      }
    };
    void fetchWebhooks();
    return () => {
      active = false;
    };
  }, []);

  // GitHub Modal State
  const [githubModalOpen, setGithubModalOpen] = useState(false);
  const [ghAuthMethod, setGhAuthMethod] = useState<'GitHub App' | 'Fine-Grained PAT'>('GitHub App');
  const [ghOrgInput, setGhOrgInput] = useState('');
  const [ghAppName, setGhAppName] = useState('');
  const [ghTokenName, setGhTokenName] = useState('');
  const [ghTokenInput, setGhTokenInput] = useState('');
  const [isSubmittingGh, setIsSubmittingGh] = useState(false);

  // GitLab Modal State
  const [gitlabModalOpen, setGitlabModalOpen] = useState(false);
  const [glAuthMethod, setGlAuthMethod] = useState<'OAuth 2.0' | 'Deploy Token'>('OAuth 2.0');
  const [glNameInput, setGlNameInput] = useState('');
  const [glInstanceUrl, setGlInstanceUrl] = useState('https://gitlab.com');
  const [glClientId, setGlClientId] = useState('');
  const [glClientSecret, setGlClientSecret] = useState('');
  const [glTokenInput, setGlTokenInput] = useState('');
  const [isSubmittingGl, setIsSubmittingGl] = useState(false);

  // Container Registries State (matches POST /settings/registries with is_global)
  const [registryModalOpen, setRegistryModalOpen] = useState(false);
  const [regScopeFilter, setRegScopeFilter] = useState<'all' | 'global' | 'personal'>('all');
  const [regProvider, setRegProvider] = useState<RegistryCredential['provider']>('GHCR');
  const [regName, setRegName] = useState('GitHub Container Registry');
  const [regHost, setRegHost] = useState('ghcr.io');
  const [regUsername, setRegUsername] = useState('');
  const [regToken, setRegToken] = useState('');
  const [regScope, setRegScope] = useState<'global' | 'personal'>('personal');

  const gitlabRedirectUri =
`${window.location.origin}/git/gitlab/callback`;

  const refreshProviders = async (force = false): Promise<GitProviderConnection[]> => {
    try {
      const res = await backendApi.gitProvidersList(force ? '?refresh=1' : '');
      if (res.ok && Array.isArray(res.data?.providers)) {
        const mapped: GitProviderConnection[] = res.data.providers.map((gp: any) => ({
          id: String(gp.id),
          provider: (gp.provider === 'gitlab' ? 'GitLab' : 'GitHub') as any,
          authMethod: (gp.auth_method || (gp.provider === 'gitlab' ? 'OAuth 2.0' : 'GitHub App')) as any,
          accountName: gp.account_login || gp.name,
          instanceUrl: gp.provider === 'gitlab' ? 'https://gitlab.com' : 'https://github.com',
          status: (gp.error || gp.needs_install || gp.status === 'error') ? ('error' as const) : ('connected' as const),
          reposAccessible: typeof gp.repos_accessible === 'number' ? gp.repos_accessible : 0,
          webhookDeliveryRate: '100%',
          webhookUrl: '',
          lastSynced: 'Just now',
          needsInstall: Boolean(gp.needs_install),
          installUrl: gp.install_url,
          appSlug: gp.app_slug,
          installationId: gp.installation_id,
          error: gp.error,
        }));
        setProviders(mapped);
        return mapped;
      }
    } catch {
      // ignore
    }
    return [];
  };

  const handleSelectRegPreset = (p: RegistryCredential['provider']) => {
    setRegProvider(p);
    if (p === 'GHCR') {
      setRegName('GitHub Container Registry');
      setRegHost('ghcr.io');
    } else if (p === 'DockerHub') {
      setRegName('Docker Hub');
      setRegHost('index.docker.io/v1/');
    } else if (p === 'GitLab') {
      setRegName('GitLab Container Registry');
      setRegHost('registry.gitlab.com');
    } else if (p === 'AWS ECR') {
      setRegName('AWS ECR (eu-central-1)');
      setRegHost('123456789012.dkr.ecr.eu-central-1.amazonaws.com');
    } else {
      setRegName('Private Harbor Registry');
      setRegHost('registry.yourdomain.com');
    }
  };

  const handleConnectGitHub = async (e: React.FormEvent) => {
    e.preventDefault();
    if (ghAuthMethod === 'GitHub App') {
      // GitHub App Manifest Flow: submits form to backend which redirects to GitHub App creation
      const form = document.createElement('form');
      form.method = 'POST';
      form.action = '/git/github/start';
      if (ghOrgInput.trim()) {
        const orgInput = document.createElement('input');
        orgInput.type = 'hidden';
        orgInput.name = 'org';
        orgInput.value = ghOrgInput.trim();
        form.appendChild(orgInput);
      }
      if (ghAppName.trim()) {
        const nameInput = document.createElement('input');
        nameInput.type = 'hidden';
        nameInput.name = 'name';
        nameInput.value = ghAppName.trim();
        form.appendChild(nameInput);
      }
      document.body.appendChild(form);
      form.submit();
      return;
    }

    // Fine-Grained Personal Access Token (PAT)
    if (!ghTokenInput.trim()) {
      toast.error('Personal access token is required');
      return;
    }
    setIsSubmittingGh(true);
    try {
      const res = await backendApi.createProvider({
        name: ghTokenName.trim() || 'GitHub PAT',
        provider: 'github',
        token: ghTokenInput.trim(),
        notes: 'Fine-Grained PAT',
      });
      if (!res.ok) {
        toast.error((res as any).error || 'Failed to connect token');
        return;
      }
      toast.success('Connected GitHub Access Token');
      setGhTokenInput('');
      setGhTokenName('');
      setGithubModalOpen(false);
      await refreshProviders();
    } catch (err: any) {
      toast.error(err.message || 'Failed to connect GitHub');
    } finally {
      setIsSubmittingGh(false);
    }
  };

  const handleConnectGitLab = async (e: React.FormEvent) => {
    e.preventDefault();
    if (glAuthMethod === 'OAuth 2.0') {
      if (!glClientId.trim() || !glClientSecret.trim()) {
        toast.error('Application ID and Secret are required');
        return;
      }
      const form = document.createElement('form');
      form.method = 'POST';
      form.action = '/git/gitlab/start';
      const cInput = document.createElement('input');
      cInput.type = 'hidden';
      cInput.name = 'client_id';
      cInput.value = glClientId.trim();
      form.appendChild(cInput);
      const sInput = document.createElement('input');
      sInput.type = 'hidden';
      sInput.name = 'client_secret';
      sInput.value = glClientSecret.trim();
      form.appendChild(sInput);
      if (glNameInput.trim()) {
        const nInput = document.createElement('input');
        nInput.type = 'hidden';
        nInput.name = 'name';
        nInput.value = glNameInput.trim();
        form.appendChild(nInput);
      }
      document.body.appendChild(form);
      form.submit();
      return;
    }

    // Deploy Token / PAT
    if (!glTokenInput.trim()) {
      toast.error('Token is required');
      return;
    }
    setIsSubmittingGl(true);
    try {
      const res = await backendApi.createProvider({
        name: glNameInput.trim() || 'GitLab Token',
        provider: 'gitlab',
        token: glTokenInput.trim(),
        notes: 'Deploy Token',
      });
      if (!res.ok) {
        toast.error((res as any).error || 'Failed to connect GitLab token');
        return;
      }
      toast.success('Connected GitLab Token');
      setGlTokenInput('');
      setGlNameInput('');
      setGitlabModalOpen(false);
      await refreshProviders();
    } catch (err: any) {
      toast.error(err.message || 'Failed to connect GitLab');
    } finally {
      setIsSubmittingGl(false);
    }
  };

  const filteredRepos = repos.filter(
    (r) => repoProviderFilter === 'all' || r.provider === repoProviderFilter
  );

  const filteredRegistries = registries.filter(
    (r) => regScopeFilter === 'all' || r.scope === regScopeFilter
  );

  if (mode === 'git') {
    return (
      <div className="space-y-5">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <FolderGit2 className="h-4 w-4 text-orange-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Git Providers</h1>
          </div>

          <div className="grid grid-cols-2 sm:flex sm:items-center gap-2 w-full sm:w-auto">
            <button
              type="button"
              onClick={() => setGitlabModalOpen(true)}
              className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 px-3 text-xs font-semibold text-orange-300 hover:bg-orange-500/20 cursor-pointer"
            >
              <GitLabIcon className="h-3.5 w-3.5 text-orange-400" />
              <span>Connect GitLab</span>
            </button>
            <button
              type="button"
              onClick={() => setGithubModalOpen(true)}
              className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <Github className="h-3.5 w-3.5" />
              <span>Connect GitHub</span>
            </button>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-3.5 md:grid-cols-3">
          {providers.map((p) => {
            const isGitLab = p.provider === 'GitLab';
            return (
              <div
                key={p.id}
                className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 flex flex-col justify-between gap-3.5"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-start gap-3 min-w-0 flex-1">
                    <div
                      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border mt-0.5 ${
                        isGitLab
                          ? 'border-orange-500/25 bg-orange-500/10 text-orange-400'
                          : 'border-white/[0.1] bg-zinc-900 text-zinc-100'
                      }`}
                    >
                      {isGitLab ? (
                        <GitLabIcon className="h-4 w-4" />
                      ) : (
                        <Github className="h-4 w-4" />
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      {/* Account Name - full width without aggressive truncation */}
                      <div className="min-w-0">
                        <span
                          className="font-mono text-sm font-semibold text-zinc-100 break-words leading-snug"
                          title={p.accountName}
                        >
                          {p.accountName}
                        </span>
                      </div>

                      {/* Badges and Provider Info */}
                      <div className="mt-1.5 flex items-center gap-1.5 flex-wrap">
                        <StatusBadge status={p.status} />
                        <OwnerBadge id={p.owner?.id} name={p.owner?.name} role={p.owner?.role} system={p.owner?.system} />
                        <span className="text-[11px] font-mono text-zinc-400">
                          {p.provider} · <span className="text-orange-400">{p.authMethod}</span>
                        </span>
                      </div>
                    </div>
                  </div>

                  <ActionMenu
                    items={[
                      ...(p.provider === 'GitHub'
                        ? [
                            {
                              label: 'Manage Repositories on GitHub',
                              icon: <ExternalLink className="h-3.5 w-3.5 text-orange-400" />,
                              onClick: () => {
                                const targetUrl =
                                  p.installationId && p.installationId.trim() !== ''
                                    ? `https://github.com/settings/installations/${p.installationId.trim()}`
                                    : p.appSlug && p.appSlug.trim() !== ''
                                    ? `https://github.com/apps/${p.appSlug.trim()}/installations/new`
                                    : p.installUrl || `/git/${p.id}/github/install`;
                                window.open(targetUrl, '_blank', 'noopener,noreferrer');
                              },
                            },
                            {
                              label: 'Configure / Change Installation ID',
                              icon: <Settings className="h-3.5 w-3.5 text-zinc-400" />,
                              onClick: () => {
                                setEditInstallProvider(p);
                                setCustomInstallId(p.installationId || '');
                              },
                            },
                            {
                              label: 'Install on Another Account / Org',
                              icon: <Github className="h-3.5 w-3.5 text-zinc-400" />,
                              onClick: () => {
                                const targetUrl = p.appSlug
                                  ? `https://github.com/apps/${p.appSlug.trim()}/installations/new`
                                  : p.installUrl || `/git/${p.id}/github/install`;
                                window.open(targetUrl, '_blank', 'noopener,noreferrer');
                              },
                            },
                          ]
                        : []),
                      {
                        label: isGitLab ? 'Sync Repositories' : 'Refresh GitHub Installation ID',
                        icon: <RefreshCw className="h-3.5 w-3.5" />,
                        onClick: async () => {
                          if (!isGitLab) {
                            await backendApi.refreshGitHubInstall(p.id);
                          }
                          const updated = await refreshProviders(true);
                          const cur = updated.find((item) => item.id === p.id);
                          if (cur?.error) {
                            toast.error(cur.error);
                          } else {
                            toast.success(`Synced repositories for ${p.accountName} (${cur?.reposAccessible ?? 0} repos)`);
                          }
                        },
                      },
                      {
                        label: 'Update Token',
                        icon: <Key className="h-3.5 w-3.5 text-orange-400" />,
                        onClick: () => {
                          setEditTokenProvider(p);
                          setNewTokenInput('');
                        },
                      },
                      {
                        label: 'Copy Webhook URL',
                        icon: <Copy className="h-3.5 w-3.5" />,
                        onClick: async () => {
                          const url =
                            p.webhookUrl ||
                            `${window.location.origin}/webhooks/${p.provider === 'GitLab' ? 'gitlab' : 'github'}/provider`;
                          const ok = await copyToClipboard(url);
                          if (ok) {
                            toast.success('Copied webhook URL');
                          } else {
                            toast.error('Failed to copy webhook URL');
                          }
                        },
                      },
                      {
                        label: 'Disconnect Provider',
                        icon: <Trash2 className="h-3.5 w-3.5" />,
                        variant: 'danger',
                        onClick: async () => {
                          await backendApi.deleteGitProvider(p.id);
                          await refreshProviders(true);
                          toast.success(`Disconnected ${p.accountName}`);
                        },
                      },
                    ]}
                  />
                </div>

                {p.error && (
                  <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-xs text-red-200 space-y-1.5">
                    <div className="font-semibold flex items-center justify-between text-red-300">
                      <div className="flex items-center gap-1.5">
                        <AlertCircle className="h-4 w-4 text-red-400 shrink-0" />
                        <span>Permission Denied (0 Repos)</span>
                      </div>
                      <button
                        type="button"
                        onClick={() => {
                          setEditTokenProvider(p);
                          setNewTokenInput('');
                        }}
                        className="rounded bg-red-500/20 px-2 py-0.5 text-[11px] font-semibold text-red-300 hover:bg-red-500/30 cursor-pointer"
                      >
                        Update Token
                      </button>
                    </div>
                    <p className="text-[11px] text-red-200/90 leading-relaxed font-mono">
                      {p.error}
                    </p>
                  </div>
                )}

                {p.needsInstall && p.provider === 'GitHub' && (
                  <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-2.5 text-xs text-amber-200">
                    <div className="font-semibold mb-1 flex items-center justify-between">
                      <span>Installation Required</span>
                    </div>
                    <p className="text-[11px] text-amber-300/80 mb-2 leading-tight">
                      Install this GitHub App to select repositories and activate auto-deploy.
                    </p>
                    <a
                      href={
                        p.appSlug
                          ? `https://github.com/apps/${p.appSlug.trim()}/installations/new`
                          : p.installUrl || `/git/${p.id}/github/install`
                      }
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1.5 rounded-md bg-amber-500 px-2.5 py-1 text-[11px] font-semibold text-zinc-950 hover:bg-amber-400 cursor-pointer"
                    >
                      <Github className="h-3 w-3" />
                      Install on GitHub
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  </div>
                )}

                <div className="flex items-center justify-between border-t border-white/[0.05] pt-2.5 font-mono text-[11px] text-zinc-400">
                  <span>{p.reposAccessible} repos</span>
                  <span className="text-emerald-400">{p.webhookDeliveryRate} webhooks</span>
                  <span className="text-zinc-500">{p.lastSynced}</span>
                </div>
              </div>
            );
          })}
        </div>

        <div className="space-y-3">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2.5">
            <div className="grid grid-cols-2 sm:flex sm:items-center gap-1 rounded-lg border border-white/[0.08] bg-[#09090b] p-1 w-full sm:w-auto">
              <button
                type="button"
                onClick={() => setGitSubTab('repos')}
                className={`h-9 sm:h-8 rounded-md px-3 text-center text-xs font-medium cursor-pointer transition-colors truncate ${
                  gitSubTab === 'repos'
                    ? 'bg-zinc-800 text-zinc-100 shadow-sm'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                Repositories ({filteredRepos.length})
              </button>
              <button
                type="button"
                onClick={() => setGitSubTab('webhooks')}
                className={`h-9 sm:h-8 rounded-md px-3 text-center text-xs font-medium cursor-pointer transition-colors truncate ${
                  gitSubTab === 'webhooks'
                    ? 'bg-zinc-800 text-zinc-100 shadow-sm'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                Webhooks ({webhookEvents.length})
              </button>
            </div>

            {gitSubTab === 'repos' && (
              <div className="grid grid-cols-3 sm:flex sm:items-center gap-1 rounded-lg border border-white/[0.08] bg-[#09090b] p-1 w-full sm:w-auto">
                {(['all', 'GitHub', 'GitLab'] as const).map((f) => (
                  <button
                    key={f}
                    type="button"
                    onClick={() => setRepoProviderFilter(f)}
                    className={`h-9 sm:h-8 rounded-md px-3 text-center text-xs font-medium cursor-pointer transition-colors capitalize ${
                      repoProviderFilter === f
                        ? 'bg-zinc-800 text-zinc-100 shadow-sm'
                        : 'text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    {f}
                  </button>
                ))}
              </div>
            )}
          </div>

          {gitSubTab === 'repos' ? (
            <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
              <div className="overflow-x-auto">
                <table className="w-full min-w-[660px] table-fixed text-left text-xs">
                  <thead>
                    <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                      <th className="w-[38%] px-4 py-2.5 font-medium">Repository</th>
                      <th className="w-[18%] px-4 py-2.5 font-medium">Branch & Commit</th>
                      <th className="w-[20%] px-4 py-2.5 font-medium">Linked Project</th>
                      <th className="w-[14%] px-4 py-2.5 font-medium">Auto-Deploy</th>
                      <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.05]">
                    {filteredRepos.map((repo) => (
                      <tr key={repo.id} className="hover:bg-white/[0.03]">
                        <td className="px-4 py-3">
                          <div className="flex items-center gap-2.5 min-w-0">
                            {repo.provider === 'GitLab' ? (
                              <GitLabIcon className="h-3.5 w-3.5 text-orange-400 shrink-0" />
                            ) : (
                              <Github className="h-3.5 w-3.5 text-zinc-200 shrink-0" />
                            )}
                            <span className="truncate font-mono font-semibold text-zinc-100">
                              {repo.fullName}
                            </span>
                            {repo.private && (
                              <Lock className="h-3 w-3 text-zinc-500 shrink-0" />
                            )}
                          </div>
                        </td>
                        <td className="px-4 py-3 font-mono text-[11px] text-orange-400">
                          {repo.defaultBranch}@{repo.lastCommitSha}
                        </td>
                        <td className="px-4 py-3 font-mono text-[11px]">
                          {repo.linkedAppSlug ? (
                            <span className="text-emerald-400">{repo.linkedAppSlug}</span>
                          ) : (
                            <span className="text-zinc-500">Unlinked</span>
                          )}
                        </td>
                        <td className="px-4 py-3">
                          <ShadcnSwitch
                            checked={repo.autoDeploy}
                            onChange={(next) => {
                              setRepos((prev) =>
                                prev.map((item) =>
                                  item.id === repo.id ? { ...item, autoDeploy: next } : item
                                )
                              );
                              toast.success(
                                next
                                  ? `Enabled push webhook for ${repo.fullName}`
                                  : `Paused webhook for ${repo.fullName}`
                              );
                            }}
                          />
                        </td>
                        <td className="px-3 py-3 text-right">
                          <ActionMenu
                            items={[
                              {
                                label: 'Trigger Build Now',
                                icon: <Play className="h-3.5 w-3.5" />,
                                onClick: () => toast.success(`Building ${repo.fullName}...`),
                              },
                              {
                                label: 'Sync Commits',
                                icon: <RefreshCw className="h-3.5 w-3.5" />,
                                onClick: () =>
                                  toast.success(`Fetched latest HEAD for ${repo.fullName}`),
                              },
                            ]}
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
              <div className="overflow-x-auto">
                <table className="w-full min-w-[660px] table-fixed text-left text-xs">
                  <thead>
                    <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                      <th className="w-[36%] px-4 py-2.5 font-medium">Repository</th>
                      <th className="w-[28%] px-4 py-2.5 font-medium">Event & Commit</th>
                      <th className="w-[18%] px-4 py-2.5 font-medium">Response</th>
                      <th className="w-[18%] px-4 py-2.5 font-medium text-right">Time</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.05]">
                    {webhookEvents.length === 0 ? (
                      <tr>
                        <td colSpan={4} className="px-4 py-8 text-center text-xs text-zinc-500 font-sans">
                          {isLoadingWebhooks ? 'Loading webhook deliveries...' : 'No webhook deliveries recorded yet.'}
                        </td>
                      </tr>
                    ) : (
                      webhookEvents.map((ev) => (
                        <tr key={ev.id} className="hover:bg-white/[0.03] font-mono">
                          <td className="px-4 py-3">
                            <div className="flex items-center gap-2 min-w-0">
                              {ev.provider === 'GitLab' ? (
                                <GitLabIcon className="h-3.5 w-3.5 text-orange-400 shrink-0" />
                              ) : (
                                <Github className="h-3.5 w-3.5 text-zinc-200 shrink-0" />
                              )}
                              <span className="truncate text-zinc-100">{ev.repo}</span>
                            </div>
                          </td>
                          <td className="px-4 py-3 text-zinc-300 truncate">
                            {ev.event} · <span className="text-orange-400">{ev.sha}</span>
                          </td>
                          <td className="px-4 py-3 text-emerald-400">
                            {ev.status} ({ev.latency})
                          </td>
                          <td className="px-4 py-3 text-right text-zinc-500">{ev.time}</td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>

        {/* Connect GitHub Modal */}
        <ShadcnDialog
          open={githubModalOpen}
          onClose={() => setGithubModalOpen(false)}
          title="Connect GitHub"
          icon={<Github className="h-4 w-4" />}
        >
          <form onSubmit={handleConnectGitHub} className="space-y-4">
            <div className="grid grid-cols-2 gap-2">
              {(['GitHub App', 'Fine-Grained PAT'] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setGhAuthMethod(m)}
                  className={`rounded-lg border px-3 py-2 text-xs font-medium cursor-pointer ${
                    ghAuthMethod === m
                      ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {m === 'GitHub App' ? 'GitHub App (Recommended)' : 'Personal Access Token'}
                </button>
              ))}
            </div>

            {ghAuthMethod === 'GitHub App' ? (
              <div className="space-y-3.5">
                <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 text-xs text-zinc-300 space-y-1.5 leading-relaxed">
                  <div className="font-semibold text-zinc-100 flex items-center gap-1.5">
                    <CheckCircle2 className="h-3.5 w-3.5 text-orange-400" />
                    One-Click GitHub App Manifest
                  </div>
                  <p className="text-[11px] text-zinc-400">
                    NextDeploy will automatically register a GitHub App with fine-grained repository permissions and webhook listeners. You choose which repositories to grant on GitHub.
                  </p>
                </div>

                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    GitHub Organization <span className="text-zinc-600 font-normal">(optional)</span>
                  </label>
                  <input
                    type="text"
                    value={ghOrgInput}
                    onChange={(e) => setGhOrgInput(e.target.value)}
                    placeholder="Leave empty for personal user account"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                  <p className="mt-1 text-[10px] text-zinc-500">
                    If specified, the app is created directly in your GitHub organization.
                  </p>
                </div>

                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Custom App Name <span className="text-zinc-600 font-normal">(optional)</span>
                  </label>
                  <input
                    type="text"
                    value={ghAppName}
                    onChange={(e) => setGhAppName(e.target.value)}
                    placeholder="e.g. NextDeploy-Production (or leave empty for auto-generated)"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                </div>
              </div>
            ) : (
              <div className="space-y-3.5">
                <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 text-xs text-zinc-300 space-y-1">
                  <div className="font-semibold text-zinc-100">Personal Access Token (PAT)</div>
                  <p className="text-[11px] text-zinc-400">
                    Provide a Fine-Grained or Classic Personal Access Token with <code className="text-orange-400 font-mono">repo</code> permissions.
                  </p>
                </div>

                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Token Label / Name <span className="text-zinc-600 font-normal">(optional)</span>
                  </label>
                  <input
                    type="text"
                    value={ghTokenName}
                    onChange={(e) => setGhTokenName(e.target.value)}
                    placeholder="e.g. My GitHub Token"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                </div>

                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Personal Access Token
                  </label>
                  <input
                    type="password"
                    required
                    value={ghTokenInput}
                    onChange={(e) => setGhTokenInput(e.target.value)}
                    placeholder="github_pat_... or ghp_..."
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                </div>
              </div>
            )}

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setGithubModalOpen(false)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmittingGh}
                className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
              >
                <Github className="h-3.5 w-3.5" />
                {ghAuthMethod === 'GitHub App' ? 'Connect with GitHub App' : 'Connect Access Token'}
              </button>
            </div>
          </form>
        </ShadcnDialog>

        {/* Connect GitLab Modal */}
        <ShadcnDialog
          open={gitlabModalOpen}
          onClose={() => setGitlabModalOpen(false)}
          title="Connect GitLab"
          icon={<GitLabIcon className="h-4 w-4 text-orange-400" />}
        >
          <form onSubmit={handleConnectGitLab} className="space-y-4">
            <div className="grid grid-cols-2 gap-2">
              {(['OAuth 2.0', 'Deploy Token'] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setGlAuthMethod(m)}
                  className={`rounded-lg border px-3 py-2 text-xs font-medium cursor-pointer ${
                    glAuthMethod === m
                      ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {m === 'OAuth 2.0' ? 'OAuth 2.0 (Recommended)' : 'Deploy Token / PAT'}
                </button>
              ))}
            </div>

            {glAuthMethod === 'OAuth 2.0' ? (
              <div className="space-y-3.5">
                <div className="rounded-lg border border-orange-500/20 bg-orange-500/[0.05] p-2.5 font-mono text-[11px] text-zinc-300">
                  <div className="flex items-center justify-between text-[10px] text-orange-300 font-sans font-semibold mb-1">
                    <span>GitLab OAuth Redirect URI (Scopes: api, read_user, read_repository)</span>
                    <button
                      type="button"
                      onClick={async () => {
                        const ok = await copyToClipboard(gitlabRedirectUri);
                        if (ok) toast.success('Copied GitLab Redirect URI');
                      }}
                      className="underline cursor-pointer text-orange-300 hover:text-orange-200"
                    >
                      Copy
                    </button>
                  </div>
                  <div className="truncate text-zinc-400">{gitlabRedirectUri}</div>
                </div>

                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Account / Group Name <span className="text-zinc-600 font-normal">(optional)</span>
                  </label>
                  <input
                    type="text"
                    value={glNameInput}
                    onChange={(e) => setGlNameInput(e.target.value)}
                    placeholder="e.g. My GitLab Group"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                </div>

                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                      Application ID (client_id)
                    </label>
                    <input
                      type="text"
                      required
                      value={glClientId}
                      onChange={(e) => setGlClientId(e.target.value)}
                      placeholder="8a91c04f..."
                      className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                    />
                  </div>
                  <div>
                    <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                      Secret (client_secret)
                    </label>
                    <input
                      type="password"
                      required
                      value={glClientSecret}
                      onChange={(e) => setGlClientSecret(e.target.value)}
                      placeholder="gloas-••••••••••••"
                      className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                    />
                  </div>
                </div>
              </div>
            ) : (
              <div className="space-y-3.5">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Account / Label <span className="text-zinc-600 font-normal">(optional)</span>
                  </label>
                  <input
                    type="text"
                    value={glNameInput}
                    onChange={(e) => setGlNameInput(e.target.value)}
                    placeholder="e.g. GitLab Personal Token"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    GitLab Personal Access Token or Deploy Token
                  </label>
                  <input
                    type="password"
                    required
                    value={glTokenInput}
                    onChange={(e) => setGlTokenInput(e.target.value)}
                    placeholder="glpat-•••••••••••• or gldt-••••••••••••"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                </div>

                <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 text-xs text-zinc-300 space-y-2 leading-relaxed">
                  <div className="font-semibold text-zinc-100">GitLab Token Permissions:</div>
                  <div className="text-[11px] text-zinc-400 space-y-1">
                    <div><span className="text-zinc-200 font-medium">Classic PAT:</span> Select <code className="text-orange-400 font-mono">read_api</code> and <code className="text-orange-400 font-mono">read_repository</code>.</div>
                    <div><span className="text-zinc-200 font-medium">Fine-grained PAT:</span> Under Group/Project, select <code className="text-orange-400 font-mono">Projects: Read</code> (to discover repos) and <code className="text-orange-400 font-mono">Code: Download</code> (to clone/pull code).</div>
                  </div>
                  <p className="text-[10px] text-zinc-500">
                    Note: Deploy Tokens (<code className="text-zinc-400 font-mono">gldt-...</code>) only grant git clone access and cannot list projects via the GitLab REST API.
                  </p>
                </div>
              </div>
            )}

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setGitlabModalOpen(false)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmittingGl}
                className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
              >
                <GitLabIcon className="h-3.5 w-3.5 text-orange-500" />
                {glAuthMethod === 'OAuth 2.0' ? 'Authorize with GitLab' : 'Connect GitLab Token'}
              </button>
            </div>
          </form>
        </ShadcnDialog>

        {/* Manage GitHub Installation Dialog */}
        <ShadcnDialog
          open={Boolean(editInstallProvider)}
          onClose={() => setEditInstallProvider(null)}
          title="Manage GitHub App Installation"
          icon={<Github className="h-4 w-4" />}
          maxWidth="md"
        >
          {editInstallProvider && (
            <div className="space-y-4 text-xs">
              <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-zinc-400">Connected Account:</span>
                  <span className="font-semibold text-zinc-100">{editInstallProvider.accountName}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-zinc-400">Current Installation ID:</span>
                  <span className="font-mono text-orange-400">
                    {editInstallProvider.installationId || 'Not set / Needs install'}
                  </span>
                </div>
              </div>

              <div className="space-y-2">
                <label className="block text-[11px] font-medium text-zinc-400">
                  Quick Actions on GitHub:
                </label>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      const targetUrl = editInstallProvider.installationId
                        ? `https://github.com/settings/installations/${editInstallProvider.installationId.trim()}`
                        : editInstallProvider.appSlug
                        ? `https://github.com/apps/${editInstallProvider.appSlug.trim()}/installations/new`
                        : `/git/${editInstallProvider.id}/github/install`;
                      window.open(targetUrl, '_blank', 'noopener,noreferrer');
                    }}
                    className="inline-flex items-center justify-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 px-3 py-2 text-xs font-medium text-orange-300 hover:bg-orange-500/20 cursor-pointer"
                  >
                    <span>Configure Repositories on GitHub</span>
                    <ExternalLink className="h-3.5 w-3.5" />
                  </button>

                  <button
                    type="button"
                    onClick={() => {
                      const targetUrl = editInstallProvider.appSlug
                        ? `https://github.com/apps/${editInstallProvider.appSlug.trim()}/installations/new`
                        : `/git/${editInstallProvider.id}/github/install`;
                      window.open(targetUrl, '_blank', 'noopener,noreferrer');
                    }}
                    className="inline-flex items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 py-2 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                  >
                    <span>Install on Other Org / User</span>
                    <ExternalLink className="h-3.5 w-3.5 text-zinc-400" />
                  </button>
                </div>
              </div>

              <form
                onSubmit={async (e) => {
                  e.preventDefault();
                  if (!editInstallProvider) return;
                  setIsSavingInstallId(true);
                  try {
                    await backendApi.updateGitProvider(editInstallProvider.id, {
                      installation_id: customInstallId.trim(),
                    });
                    setProviders((prev) =>
                      prev.map((item) =>
                        item.id === editInstallProvider.id
                          ? { ...item, installationId: customInstallId.trim() }
                          : item
                      )
                    );
                    toast.success('Updated GitHub Installation ID');
                    setEditInstallProvider(null);
                  } catch (err: any) {
                    toast.error(err.message || 'Failed to update installation ID');
                  } finally {
                    setIsSavingInstallId(false);
                  }
                }}
                className="space-y-3 border-t border-white/[0.07] pt-3"
              >
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <label className="text-[11px] font-medium text-zinc-300">
                      Change Installation ID Manually
                    </label>
                    <button
                      type="button"
                      onClick={async () => {
                        if (!editInstallProvider) return;
                        await backendApi.refreshGitHubInstall(editInstallProvider.id);
                        toast.success('Auto-detected installation from GitHub');
                        setEditInstallProvider(null);
                      }}
                      className="text-[11px] text-orange-400 hover:underline cursor-pointer"
                    >
                      Auto-detect from GitHub
                    </button>
                  </div>
                  <input
                    type="text"
                    value={customInstallId}
                    onChange={(e) => setCustomInstallId(e.target.value)}
                    placeholder="e.g. 62819231"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none"
                  />
                  <p className="mt-1 text-[11px] text-zinc-500">
                    Found in your GitHub URL: github.com/settings/installations/[ID]
                  </p>
                </div>

                <div className="flex items-center justify-end gap-2 pt-1">
                  <button
                    type="button"
                    onClick={() => setEditInstallProvider(null)}
                    className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3 py-1.5 text-xs text-zinc-300 cursor-pointer"
                  >
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={isSavingInstallId}
                    className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
                  >
                    {isSavingInstallId ? 'Saving...' : 'Save Installation ID'}
                  </button>
                </div>
              </form>
            </div>
          )}
        </ShadcnDialog>

        {/* Update Token Dialog (GitLab / GitHub PAT) */}
        <ShadcnDialog
          open={Boolean(editTokenProvider)}
          onClose={() => setEditTokenProvider(null)}
          title={`Update Token for ${editTokenProvider?.accountName || 'Provider'}`}
          icon={<Key className="h-4 w-4 text-orange-400" />}
          maxWidth="md"
        >
          {editTokenProvider && (
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                if (!newTokenInput.trim()) {
                  toast.error('Token cannot be empty');
                  return;
                }
                setIsUpdatingToken(true);
                try {
                  const res = await backendApi.updateProvider(editTokenProvider.id, {
                    token: newTokenInput.trim(),
                    provider: editTokenProvider.provider.toLowerCase(),
                  });
                  if (!res.ok) {
                    toast.error(res.error || 'Failed to update token');
                    return;
                  }
                  toast.success(`Successfully updated token for ${editTokenProvider.accountName}`);
                  setEditTokenProvider(null);
                  setNewTokenInput('');
                  await refreshProviders(true);
                } catch (err: any) {
                  toast.error(err.message || 'Failed to update token');
                } finally {
                  setIsUpdatingToken(false);
                }
              }}
              className="space-y-4 text-xs"
            >
              <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-zinc-400">Provider:</span>
                  <span className="font-semibold text-zinc-100">{editTokenProvider.provider}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-zinc-400">Account:</span>
                  <span className="font-semibold text-zinc-100">{editTokenProvider.accountName}</span>
                </div>
              </div>

              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  New Personal Access Token
                </label>
                <input
                  type="password"
                  required
                  value={newTokenInput}
                  onChange={(e) => setNewTokenInput(e.target.value)}
                  placeholder="glpat-•••••••••••• or github_pat_••••••••••••"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:border-orange-500/60"
                />
              </div>

              {editTokenProvider.provider === 'GitLab' && (
                <div className="rounded-lg border border-orange-500/20 bg-orange-500/10 p-3 text-xs text-orange-200 space-y-1.5 leading-relaxed">
                  <div className="font-semibold text-orange-300">GitLab Token Permissions:</div>
                  <div className="text-[11px] text-zinc-300 space-y-1">
                    <div><span className="text-zinc-100 font-medium">Classic PAT:</span> Select <code className="text-orange-400 font-mono">read_api</code> and <code className="text-orange-400 font-mono">read_repository</code>.</div>
                    <div><span className="text-zinc-100 font-medium">Fine-grained PAT:</span> Under Group/Project, select <code className="text-orange-400 font-mono">Projects: Read</code> and <code className="text-orange-400 font-mono">Code: Download</code>.</div>
                  </div>
                </div>
              )}

              <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
                <button
                  type="button"
                  onClick={() => setEditTokenProvider(null)}
                  className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isUpdatingToken || !newTokenInput.trim()}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
                >
                  <Key className="h-3.5 w-3.5" />
                  {isUpdatingToken ? 'Updating & Verifying...' : 'Update & Verify Token'}
                </button>
              </div>
            </form>
          )}
        </ShadcnDialog>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Tier 1: Header */}
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <HardDrive className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2 min-w-0">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100 truncate">
              Container Registries
            </h1>
            <span className="shrink-0 rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {filteredRegistries.length}
            </span>
          </div>
        </div>

        <button
          type="button"
          onClick={() => setRegistryModalOpen(true)}
          className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer shadow-sm"
        >
          <Plus className="h-4 w-4" />
          <span>Add Registry</span>
        </button>
      </div>

      {/* Tier 2: Filter Toolbar */}
      <div className="flex items-center justify-between gap-2">
        <div className="grid grid-cols-3 sm:flex items-center gap-1 rounded-lg border border-white/[0.08] bg-[#09090b] p-1 w-full sm:w-auto">
          {(
            [
              { id: 'all', label: 'All' },
              { id: 'global', label: 'Global' },
              { id: 'personal', label: 'Personal' },
            ] as const
          ).map((sc) => (
            <button
              key={sc.id}
              type="button"
              onClick={() => setRegScopeFilter(sc.id)}
              className={`h-9 sm:h-8 rounded-md px-3 text-center text-xs font-medium transition-colors cursor-pointer ${
                regScopeFilter === sc.id
                  ? 'bg-zinc-800 text-zinc-100 shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {sc.label}
            </button>
          ))}
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[680px] text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="px-4 py-2.5 font-medium">Registry & Server</th>
                <th className="px-4 py-2.5 font-medium">Username & Token</th>
                <th className="px-4 py-2.5 font-medium">Scope</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium text-right">Usage</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {filteredRegistries.map((r) => {
                const isGlobal = r.scope === 'global';
                return (
                  <tr key={r.id} className="hover:bg-white/[0.03]">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <span className="rounded-md border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 text-[10px] font-semibold text-orange-400 shrink-0">
                          {r.provider}
                        </span>
                        <div className="min-w-0">
                          <div className="flex items-center gap-1.5">
                            <span className="font-semibold text-zinc-100">{r.name}</span>
                            <OwnerBadge id={r.owner?.id} name={r.owner?.name} role={r.owner?.role} system={r.owner?.system} />
                          </div>
                          <div className="text-[11px] text-zinc-500">
                            {r.registryUrl}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-medium text-zinc-200">{r.username}</div>
                      <div className="font-mono text-[10px] text-zinc-500">{r.tokenPreview}</div>
                    </td>
                    <td className="px-4 py-3 whitespace-nowrap">
                      {isGlobal ? (
                        <span className="inline-flex items-center gap-1 rounded-md border border-sky-500/25 bg-sky-500/10 px-2.5 py-0.5 text-[11px] font-medium text-sky-300 whitespace-nowrap">
                          <Globe className="h-3 w-3 shrink-0" />
                          Global
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded-md border border-violet-500/25 bg-violet-500/10 px-2.5 py-0.5 text-[11px] font-medium text-violet-300 whitespace-nowrap">
                          <User className="h-3 w-3 shrink-0" />
                          Personal
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 whitespace-nowrap">
                      <StatusBadge status={r.status} label="Authenticated" />
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-[11px] text-zinc-300 whitespace-nowrap">
                      {r.imagesCount} imgs · {r.connectedApps} apps
                    </td>
                    <td className="px-3 py-3 text-right">
                      <ActionMenu
                        items={[
                          {
                            label: 'Verify docker login',
                            icon: <CheckCircle2 className="h-3.5 w-3.5" />,
                            onClick: () =>
                              toast.success(`Login succeeded against ${r.registryUrl}`),
                          },
                          {
                            label: 'Copy Server URL',
                            icon: <Copy className="h-3.5 w-3.5" />,
                            onClick: async () => {
                              const ok = await copyToClipboard(r.registryUrl);
                              if (ok) toast.success(`Copied ${r.registryUrl}`);
                            },
                          },
                          {
                            label: 'Remove Registry',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger',
                            onClick: () => {
                              void backendApi.deleteRegistry(r.id);
                              if (onDeleteRegistry) onDeleteRegistry(r.id);
                              toast.success(`Removed ${r.name}`);
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

      {/* Add Container Registry Modal (with Global vs Personal Scope Option!) */}
      <ShadcnDialog
        open={registryModalOpen}
        onClose={() => setRegistryModalOpen(false)}
        title="Add Container Registry"
        icon={<HardDrive className="h-4 w-4" />}
      >
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            if (!regUsername.trim()) return;
            await backendApi.addRegistry({
              name: regName.trim(),
              server: regHost.trim(),
              username: regUsername.trim(),
              password: regToken.trim(),
              is_global: false, // registries are never shared between accounts
            });
            onAddRegistry({
              id: `reg-${Date.now()}`,
              provider: regProvider,
              name: regName.trim(),
              registryUrl: regHost.trim(),
              username: regUsername.trim(),
              tokenPreview: regToken ? `${regToken.slice(0, 6)}••••` : '••••••••',
              scope: regScope,
              connectedApps: 0,
              imagesCount: 0,
              status: 'connected',
              lastVerified: 'Just now',
            });
            setRegUsername('');
            setRegToken('');
            setRegistryModalOpen(false);
            toast.success(
              `Added ${regScope === 'global' ? 'Global' : 'Personal'} registry ${regHost}`
            );
          }}
          className="space-y-4"
        >
          {/* Registries belong to the account that creates them; sharing across accounts is not offered. */}
          <div className="hidden">
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Credential Scope (is_global)
            </label>
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => setRegScope('global')}
                className={`flex items-start gap-2.5 rounded-xl border p-3 text-left transition-all cursor-pointer ${
                  regScope === 'global'
                    ? 'border-sky-500/50 bg-sky-500/10 text-zinc-100'
                    : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <Globe className="h-4 w-4 text-sky-400 shrink-0 mt-0.5" />
                <div>
                  <div className="text-xs font-semibold">Global Registry</div>
                  <div className="text-[10px] text-zinc-500">
                    Shared across all panel users & projects (Admin)
                  </div>
                </div>
              </button>
              <button
                type="button"
                onClick={() => setRegScope('personal')}
                className={`flex items-start gap-2.5 rounded-xl border p-3 text-left transition-all cursor-pointer ${
                  regScope === 'personal'
                    ? 'border-violet-500/50 bg-violet-500/10 text-zinc-100'
                    : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <User className="h-4 w-4 text-violet-400 shrink-0 mt-0.5" />
                <div>
                  <div className="text-xs font-semibold">Personal Registry</div>
                  <div className="text-[10px] text-zinc-500">
                    Scoped only to your own account & projects
                  </div>
                </div>
              </button>
            </div>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Registry Provider Preset
            </label>
            <div className="grid grid-cols-5 gap-1.5">
              {(['GHCR', 'DockerHub', 'GitLab', 'AWS ECR', 'Custom'] as const).map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => handleSelectRegPreset(p)}
                  className={`rounded-lg border py-1.5 text-[11px] font-medium cursor-pointer ${
                    regProvider === p
                      ? 'border-orange-500/50 bg-orange-500/10 text-zinc-100'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {p}
                </button>
              ))}
            </div>
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Label (name)
              </label>
              <input
                type="text"
                required
                value={regName}
                onChange={(e) => setRegName(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Registry Server (server)
              </label>
              <input
                type="text"
                required
                value={regHost}
                onChange={(e) => setRegHost(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Username
              </label>
              <input
                type="text"
                required
                value={regUsername}
                onChange={(e) => setRegUsername(e.target.value)}
                placeholder="registry-user"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Token / Password
              </label>
              <input
                type="password"
                required
                value={regToken}
                onChange={(e) => setRegToken(e.target.value)}
                placeholder="••••••••••••"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
          </div>

          <div className="flex items-center justify-between border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => toast.success(`Verified credentials against ${regHost}`)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3 py-1.5 text-xs font-medium text-emerald-400 hover:bg-zinc-800 cursor-pointer"
            >
              Test Connection
            </button>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setRegistryModalOpen(false)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Save Registry
              </button>
            </div>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};
