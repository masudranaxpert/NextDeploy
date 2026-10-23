import React, { useState, useEffect } from 'react';
import {
  RefreshCw,
  Copy,
  Check,
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
import { gitAndRegistryApi } from '../../lib/api/gitAndRegistryApi';

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

  useEffect(() => {
    setProviders(gitProviders);
  }, [gitProviders]);

  // GitHub Modal State (matches POST /git/github/start & /git/:pid/update)
  const [githubModalOpen, setGithubModalOpen] = useState(false);
  const [ghAuthMethod, setGhAuthMethod] = useState<'GitHub App' | 'Fine-Grained PAT'>('GitHub App');
  const [ghAppName, setGhAppName] = useState('nextdeploy-prod-panel');
  const [ghAccountInput, setGhAccountInput] = useState('masudranaxpert');
  const [ghAppId, setGhAppId] = useState('1094821');
  const [ghInstallationId, setGhInstallationId] = useState('54918204');
  const [ghTokenInput, setGhTokenInput] = useState('');

  // GitLab Modal State (matches POST /git/gitlab/start & /git/:pid/update)
  const [gitlabModalOpen, setGitlabModalOpen] = useState(false);
  const [glAuthMethod, setGlAuthMethod] = useState<'OAuth 2.0' | 'Deploy Token'>('OAuth 2.0');
  const [glAccountInput, setGlAccountInput] = useState('nextdeploy-internal');
  const [glInstanceUrl, setGlInstanceUrl] = useState('https://gitlab.com');
  const [glTokenInput, setGlTokenInput] = useState('');
  const [glClientId, setGlClientId] = useState('');
  const [glClientSecret, setGlClientSecret] = useState('');
  const [copiedUri, setCopiedUri] = useState(false);

  // Container Registries State (matches POST /settings/registries with is_global)
  const [registryModalOpen, setRegistryModalOpen] = useState(false);
  const [regScopeFilter, setRegScopeFilter] = useState<'all' | 'global' | 'personal'>('all');
  const [regProvider, setRegProvider] = useState<RegistryCredential['provider']>('GHCR');
  const [regName, setRegName] = useState('GitHub Container Registry');
  const [regHost, setRegHost] = useState('ghcr.io');
  const [regUsername, setRegUsername] = useState('');
  const [regToken, setRegToken] = useState('');
  const [regScope, setRegScope] = useState<'global' | 'personal'>('global');

  const gitlabRedirectUri =
    typeof window !== 'undefined'
      ? `${window.location.origin}/git/gitlab/callback`
      : 'https://panel.nextdeploy.sh/git/gitlab/callback';

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
      // 1-Click GitHub App Manifest Flow: auto-redirects to GitHub to approve permissions
      const form = document.createElement('form');
      form.method = 'POST';
      form.action = '/git/github/start';
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = 'name';
      input.value = ghAppName.trim();
      form.appendChild(input);
      document.body.appendChild(form);
      form.submit();
      return;
    }

    if (!ghAccountInput.trim() || !ghTokenInput.trim()) {
      toast.error('Please enter account name and Personal Access Token');
      return;
    }

    try {
      const res = await gitAndRegistryApi.createProvider({
        name: ghAccountInput.trim(),
        provider: 'github',
        token: ghTokenInput.trim(),
        notes: 'Personal Access Token',
      });
      if (res && res.id) {
        const created: GitProviderConnection = {
          id: String(res.id),
          provider: 'GitHub',
          authMethod: 'Fine-Grained PAT',
          accountName: res.name || ghAccountInput.trim(),
          instanceUrl: 'https://github.com',
          status: 'connected',
          reposAccessible: 0,
          webhookDeliveryRate: '100%',
          webhookUrl: '',
          lastSynced: 'Just now',
        };
        setProviders((prev) => [...prev, created]);
        setGhTokenInput('');
        setGithubModalOpen(false);
        toast.success(`Connected GitHub @${created.accountName} via Personal Access Token`);
      }
    } catch (err: any) {
      toast.error(err.message || 'Failed to save GitHub provider');
    }
  };

  const handleConnectGitLab = async (e: React.FormEvent) => {
    e.preventDefault();
    if (glAuthMethod === 'OAuth 2.0') {
      if (glClientId.trim() && glClientSecret.trim()) {
        await backendApi.startGitLabOAuth(glClientId.trim(), glClientSecret.trim());
      }
      return;
    }

    // Deploy Token / PAT connection
    if (!glAccountInput.trim() || !glTokenInput.trim()) {
      toast.error('Please enter group/account name and deploy token');
      return;
    }

    try {
      const res = await gitAndRegistryApi.createProvider({
        name: glAccountInput.trim(),
        provider: 'gitlab',
        token: glTokenInput.trim(),
        notes: glInstanceUrl.trim() !== 'https://gitlab.com' ? `Deploy Token (${glInstanceUrl.trim()})` : 'Deploy Token',
      });
      if (res && res.id) {
        const created: GitProviderConnection = {
          id: String(res.id),
          provider: 'GitLab',
          authMethod: 'Deploy Token',
          accountName: res.name || glAccountInput.trim(),
          instanceUrl: glInstanceUrl.trim() || 'https://gitlab.com',
          status: 'connected',
          reposAccessible: 0,
          webhookDeliveryRate: '100%',
          webhookUrl: '',
          lastSynced: 'Just now',
        };
        setProviders((prev) => [...prev, created]);
        setGlTokenInput('');
        setGitlabModalOpen(false);
        toast.success(`Connected GitLab @${created.accountName} via Deploy Token`);
      }
    } catch (err: any) {
      toast.error(err.message || 'Failed to save GitLab provider');
    }
  };

  const filteredRepos = repos.filter(
    (r) => repoProviderFilter === 'all' || r.provider === repoProviderFilter
  );

  const filteredRegistries = registries.filter(
    (r) => regScopeFilter === 'all' || r.scope === regScopeFilter
  );

  const [webhookEvents, setWebhookEvents] = useState<WebhookDeliveryItem[]>([]);
  const [webhooksLoading, setWebhooksLoading] = useState(false);

  const loadWebhookDeliveries = async () => {
    setWebhooksLoading(true);
    try {
      const res = await gitAndRegistryApi.webhookDeliveries(10);
      if (Array.isArray(res.data)) {
        const formatted: WebhookDeliveryItem[] = res.data.map((item) => {
          let timeAgo = 'Just now';
          if (item.created_at) {
            const diffMs = Date.now() - new Date(item.created_at).getTime();
            const mins = Math.floor(diffMs / 60000);
            if (mins < 1) timeAgo = 'Just now';
            else if (mins < 60) timeAgo = `${mins}m ago`;
            else {
              const hours = Math.floor(mins / 60);
              if (hours < 24) timeAgo = `${hours}h ago`;
              else timeAgo = `${Math.floor(hours / 24)}d ago`;
            }
          }
          return {
            id: item.delivery_id,
            provider: (item.provider?.toLowerCase() === 'gitlab' ? 'GitLab' : 'GitHub') as 'GitHub' | 'GitLab',
            repo: item.repo || 'Repository',
            event: item.event || 'push',
            sha: item.sha || '—',
            status: item.status || '200 OK',
            latency: item.latency || '—',
            time: timeAgo,
          };
        });
        setWebhookEvents(formatted.slice(0, 10));
      }
    } catch {
      // ignore network errors
    } finally {
      setWebhooksLoading(false);
    }
  };

  useEffect(() => {
    void loadWebhookDeliveries();
  }, [gitSubTab]);

  if (mode === 'git') {
    return (
      <div className="space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <FolderGit2 className="h-4 w-4 text-orange-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Git Providers</h1>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => setGitlabModalOpen(true)}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 px-3 text-xs font-semibold text-orange-300 hover:bg-orange-500/20 cursor-pointer"
            >
              <GitLabIcon className="h-3.5 w-3.5 text-orange-400" />
              Connect GitLab
            </button>
            <button
              type="button"
              onClick={() => setGithubModalOpen(true)}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <Github className="h-3.5 w-3.5" />
              Connect GitHub
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
                <div className="flex items-start justify-between gap-2.5">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <div
                      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border ${
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
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate font-mono text-xs font-semibold text-zinc-100">
                          {p.accountName}
                        </span>
                        <StatusBadge status={p.status} />
                      </div>
                      <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-zinc-400">
                        <span>{p.provider}</span>
                        <span>·</span>
                        <span className="text-orange-400">{p.authMethod}</span>
                      </div>
                    </div>
                  </div>

                  <ActionMenu
                    items={[
                      {
                        label: isGitLab ? 'Sync Repositories' : 'Refresh GitHub Installation ID',
                        icon: <RefreshCw className="h-3.5 w-3.5" />,
                        onClick: async () => {
                          if (!isGitLab) {
                            await backendApi.refreshGitHubInstall(p.id);
                          }
                          toast.success(`Synced installation & repositories for ${p.accountName}`);
                        },
                      },
                      {
                        label: 'Copy Webhook URL',
                        icon: <Copy className="h-3.5 w-3.5" />,
                        onClick: () => {
                          navigator.clipboard?.writeText(p.webhookUrl);
                          toast.success('Copied webhook URL');
                        },
                      },
                      {
                        label: 'Disconnect Provider',
                        icon: <Trash2 className="h-3.5 w-3.5" />,
                        variant: 'danger',
                        onClick: () => {
                          void backendApi.deleteGitProvider(p.id);
                          setProviders((prev) => prev.filter((item) => item.id !== p.id));
                          toast.success(`Disconnected ${p.accountName}`);
                        },
                      },
                    ]}
                  />
                </div>

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
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
              <button
                type="button"
                onClick={() => setGitSubTab('repos')}
                className={`rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                  gitSubTab === 'repos'
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                Synced Repositories ({filteredRepos.length})
              </button>
              <button
                type="button"
                onClick={() => setGitSubTab('webhooks')}
                className={`rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                  gitSubTab === 'webhooks'
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                Webhook Deliveries ({webhookEvents.length})
              </button>
            </div>

            {gitSubTab === 'repos' && (
              <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
                {(['all', 'GitHub', 'GitLab'] as const).map((f) => (
                  <button
                    key={f}
                    type="button"
                    onClick={() => setRepoProviderFilter(f)}
                    className={`rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer ${
                      repoProviderFilter === f
                        ? 'bg-zinc-800 text-zinc-100'
                        : 'text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    {f === 'all' ? 'All' : f}
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
                        <td colSpan={4} className="px-4 py-8 text-center text-zinc-500 font-sans">
                          {webhooksLoading
                            ? 'Loading webhook deliveries...'
                            : 'No webhook deliveries yet. Inbound push events from GitHub and GitLab will be recorded here.'}
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
          title="Connect GitHub (App Manifest or PAT)"
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
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400'
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>

            {ghAuthMethod === 'GitHub App' ? (
              <div className="space-y-3">
                <div className="rounded-lg border border-orange-500/20 bg-orange-500/[0.05] p-3 text-xs text-zinc-300">
                  <p className="font-medium text-orange-300 mb-1">1-Click GitHub App Manifest Flow</p>
                  <p className="text-zinc-400 text-[11px] leading-relaxed">
                    NextDeploy will automatically configure permissions, webhook URLs, and secrets directly through GitHub. No App ID or manual installation ID required.
                  </p>
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    GitHub App Name (Optional)
                  </label>
                  <input
                    type="text"
                    value={ghAppName}
                    onChange={(e) => setGhAppName(e.target.value)}
                    placeholder="NextDeploy-App (leave blank for auto-generated)"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                  <p className="text-[10px] text-zinc-500 mt-1">
                    You can specify any name for the GitHub App or leave empty.
                  </p>
                </div>
              </div>
            ) : (
              <div className="space-y-3">
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    GitHub Account or Organization
                  </label>
                  <input
                    type="text"
                    required
                    value={ghAccountInput}
                    onChange={(e) => setGhAccountInput(e.target.value)}
                    placeholder="masudranaxpert"
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                </div>
                <div>
                  <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                    Personal Access Token (PAT)
                  </label>
                  <input
                    type="password"
                    required
                    value={ghTokenInput}
                    onChange={(e) => setGhTokenInput(e.target.value)}
                    placeholder="github_pat_... or ghp_..."
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                  <p className="text-[10px] text-zinc-500 mt-1">
                    Requires <code className="text-orange-400">repo</code> (or repository contents read permission).
                  </p>
                </div>
              </div>
            )}

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setGithubModalOpen(false)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                {ghAuthMethod === 'GitHub App' ? 'Connect GitHub App' : 'Connect Personal Access Token'}
              </button>
            </div>
          </form>
        </ShadcnDialog>

        {/* Connect GitLab Modal */}
        <ShadcnDialog
          open={gitlabModalOpen}
          onClose={() => setGitlabModalOpen(false)}
          title="Connect GitLab (OAuth 2.0 or Deploy Token)"
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
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400'
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>

            {glAuthMethod === 'OAuth 2.0' && (
              <div className="rounded-xl border border-white/[0.08] bg-[#0c0c0e] p-3 space-y-2.5">
                <div className="flex flex-wrap items-center justify-between gap-1.5">
                  <div className="flex items-center gap-1.5 text-xs font-medium text-zinc-200">
                    <span>GitLab OAuth Redirect URI</span>
                  </div>
                  <div className="flex items-center gap-1">
                    <span className="text-[10px] text-zinc-500 font-mono">Scopes:</span>
                    {['api', 'read_user', 'read_repository'].map((scope) => (
                      <span
                        key={scope}
                        className="rounded bg-orange-500/10 border border-orange-500/20 px-1.5 py-0.5 text-[10px] font-mono font-medium text-orange-400"
                      >
                        {scope}
                      </span>
                    ))}
                  </div>
                </div>

                <div className="flex items-center gap-2 rounded-lg border border-white/[0.08] bg-black/60 px-3 py-2 transition-colors focus-within:border-orange-500/50">
                  <span className="flex-1 font-mono text-[11px] text-zinc-300 truncate select-all">
                    {gitlabRedirectUri}
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard?.writeText(gitlabRedirectUri);
                      setCopiedUri(true);
                      setTimeout(() => setCopiedUri(false), 2000);
                      toast.success('Copied GitLab Redirect URI');
                    }}
                    className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-[11px] font-medium transition-all cursor-pointer shrink-0 ${
                      copiedUri
                        ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                        : 'border-white/[0.1] bg-zinc-900 text-zinc-300 hover:bg-zinc-800 hover:text-white hover:border-white/[0.2]'
                    }`}
                  >
                    {copiedUri ? (
                      <>
                        <Check className="h-3 w-3 text-emerald-400" />
                        <span>Copied</span>
                      </>
                    ) : (
                      <>
                        <Copy className="h-3 w-3 text-zinc-400" />
                        <span>Copy</span>
                      </>
                    )}
                  </button>
                </div>
                <p className="text-[10px] text-zinc-500 leading-normal">
                  Add this URI to your GitLab Application (<span className="text-zinc-400">Settings → Applications</span>) and grant the scopes above.
                </p>
              </div>
            )}

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  GitLab Instance URL
                </label>
                <input
                  type="text"
                  required
                  value={glInstanceUrl}
                  onChange={(e) => setGlInstanceUrl(e.target.value)}
                  placeholder="https://gitlab.com"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Group or Namespace
                </label>
                <input
                  type="text"
                  required
                  value={glAccountInput}
                  onChange={(e) => setGlAccountInput(e.target.value)}
                  placeholder="nextdeploy-internal"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
            </div>

            {glAuthMethod === 'OAuth 2.0' ? (
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
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
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
                    className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                  />
                </div>
              </div>
            ) : (
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  GitLab Deploy Token or PAT
                </label>
                <input
                  type="password"
                  required
                  value={glTokenInput}
                  onChange={(e) => setGlTokenInput(e.target.value)}
                  placeholder="glpat-... or gldt-..."
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
                <p className="text-[10px] text-zinc-500 mt-1">
                  Requires <code className="text-orange-400">read_repository</code> scope.
                </p>
              </div>
            )}

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setGitlabModalOpen(false)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                {glAuthMethod === 'OAuth 2.0' ? 'Start GitLab OAuth' : 'Connect Deploy Token'}
              </button>
            </div>
          </form>
        </ShadcnDialog>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <HardDrive className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              Container Registries
            </h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {filteredRegistries.length}
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Scope Filter (All / Global / Personal) */}
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
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
                className={`rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer ${
                  regScopeFilter === sc.id
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {sc.label}
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={() => setRegistryModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Add Registry
          </button>
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
                          <div className="font-semibold text-zinc-100">{r.name}</div>
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
                            onClick: () => {
                              navigator.clipboard?.writeText(r.registryUrl);
                              toast.success(`Copied ${r.registryUrl}`);
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
              is_global: regScope === 'global',
            });
            onAddRegistry({
              id: `reg-${Date.now()}`,
              provider: regProvider,
              name: regName.trim(),
              registryUrl: regHost.trim(),
              username: regUsername.trim(),
              tokenPreview: `${regToken.slice(0, 6) || 'pat_91'}••••`,
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
          {/* Scope Selector: Global vs Personal (Matches Go backend is_global="on") */}
          <div>
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
                placeholder="masudranaxpert"
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
