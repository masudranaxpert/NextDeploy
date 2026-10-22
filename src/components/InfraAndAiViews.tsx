import React, { useState } from 'react';
import {
  Pin,
  RefreshCw,
  Copy,
  Trash2,
  Plus,
  Download,
  RotateCw,
  Cloud,
  CheckCircle2,
  Activity,
  FolderGit2,
  HardDrive,
  Globe,
  Archive,
  ArrowLeftRight,
  Sparkles,
  KeyRound,
  Users,
  FileText,
  Settings,
  Github,
  ShieldCheck,
  Server,
  ExternalLink,
  Search,
  Eye,
  EyeOff,
  FileCode2,
  Lock,
  GitBranch,
  Terminal,
  Play,
  Webhook,
  Check,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  AppStack,
  AuditLogEntry,
  BackupArchiveRecord,
  BackupDestination,
  CliSessionItem,
  GitProviderConnection,
  McpToolItem,
  MigrationExportBundle,
  PanelUser,
  RegistryCredential,
  SyncedGitRepository,
  TelemetryPoint,
} from '../types/nextdeploy';
import {
  ActionMenu,
  EvilAreaChart,
  ShadcnDialog,
  ShadcnSwitch,
  StatusBadge,
} from './ui/Primitives';
import { CodeEditorBox } from './ui/CodeEditorBox';

// Crisp GitLab SVG Icon
const GitLabIcon: React.FC<{ className?: string }> = ({ className = 'h-4 w-4' }) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M22.65 14.39L12 22.13 1.35 14.39a.84.84 0 0 1-.3-.94l1.22-3.78 2.44-7.51A.42.42 0 0 1 4.82 2a.43.43 0 0 1 .58 0 .42.42 0 0 1 .11.18l2.44 7.49h8.1l2.44-7.51A.42.42 0 0 1 18.6 2a.43.43 0 0 1 .58 0 .42.42 0 0 1 .11.18l2.44 7.51L23 13.45a.84.84 0 0 1-.35.94z" />
  </svg>
);

// ----------------------------------------------------------------------------
// 1. Monitor Page (/monitor) — Show/Hide toggles for CPU, Memory, and Network charts
// ----------------------------------------------------------------------------
interface MonitorViewProps {
  telemetry: TelemetryPoint[];
  apps: AppStack[];
  onStreamTick: () => void;
}

export const MonitorView: React.FC<MonitorViewProps> = ({ telemetry, apps, onStreamTick }) => {
  const [pinnedApps, setPinnedApps] = useState<string[]>([
    'commerce-core-api',
    'studio-web-dashboard',
  ]);
  const [monitorSearch, setMonitorSearch] = useState('');
  const [monitorStatusFilter, setMonitorStatusFilter] = useState<
    'all' | 'running' | 'building' | 'stopped'
  >('all');
  const [showCpuChart, setShowCpuChart] = useState(true);
  const [showMemChart, setShowMemChart] = useState(true);
  const [showNetChart, setShowNetChart] = useState(true);

  const togglePin = (slug: string) => {
    setPinnedApps((prev) =>
      prev.includes(slug) ? prev.filter((s) => s !== slug) : [...prev, slug]
    );
  };

  const filteredMonitorApps = apps.filter((app) => {
    if (monitorStatusFilter !== 'all' && app.status !== monitorStatusFilter) return false;
    const q = monitorSearch.trim().toLowerCase();
    if (!q) return true;
    return (
      app.name.toLowerCase().includes(q) ||
      app.slug.toLowerCase().includes(q) ||
      (app.primaryDomain && app.primaryDomain.toLowerCase().includes(q)) ||
      app.services.some((s) => s.service.toLowerCase().includes(q) || s.name.toLowerCase().includes(q))
    );
  });

  const sortedApps = [...filteredMonitorApps].sort((a, b) => {
    const aPin = pinnedApps.includes(a.slug) ? 1 : 0;
    const bPin = pinnedApps.includes(b.slug) ? 1 : 0;
    return bPin - aPin;
  });

  const latest = telemetry[telemetry.length - 1] || {
    cpu: 23,
    memory: 48,
    netInMbps: 44,
    netOutMbps: 33,
  };

  const anyChartVisible = showCpuChart || showMemChart || showNetChart;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Activity className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Monitoring</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {sortedApps.length}
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Search Input */}
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={monitorSearch}
              onChange={(e) => setMonitorSearch(e.target.value)}
              placeholder="Search projects or services..."
              className="h-8 w-52 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-7 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none transition-colors"
            />
            {monitorSearch && (
              <button
                type="button"
                onClick={() => setMonitorSearch('')}
                className="absolute right-2 top-2 text-zinc-500 hover:text-zinc-200 cursor-pointer"
                title="Clear search"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-1">
            <span className="px-2 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
              Charts
            </span>
            <button
              type="button"
              onClick={() => setShowCpuChart((p) => !p)}
              className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors cursor-pointer ${
                showCpuChart
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {showCpuChart ? <Eye className="h-3 w-3 text-emerald-400" /> : <EyeOff className="h-3 w-3" />}
              CPU
            </button>
            <button
              type="button"
              onClick={() => setShowMemChart((p) => !p)}
              className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors cursor-pointer ${
                showMemChart
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {showMemChart ? <Eye className="h-3 w-3 text-sky-400" /> : <EyeOff className="h-3 w-3" />}
              RAM
            </button>
            <button
              type="button"
              onClick={() => setShowNetChart((p) => !p)}
              className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors cursor-pointer ${
                showNetChart
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {showNetChart ? <Eye className="h-3 w-3 text-orange-400" /> : <EyeOff className="h-3 w-3" />}
              Network
            </button>
          </div>

          <button
            type="button"
            onClick={() => {
              onStreamTick();
              toast.success('Updated telemetry');
            }}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <RefreshCw className="h-3.5 w-3.5 text-orange-400" />
            Refresh
          </button>
        </div>
      </div>

      {anyChartVisible && (
        <div
          className={`grid grid-cols-1 gap-3.5 ${
            [showCpuChart, showMemChart, showNetChart].filter(Boolean).length === 3
              ? 'lg:grid-cols-3'
              : [showCpuChart, showMemChart, showNetChart].filter(Boolean).length === 2
                ? 'lg:grid-cols-2'
                : 'lg:grid-cols-1'
          }`}
        >
          {showCpuChart && (
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-300">CPU Load</span>
                  <span className="font-mono text-xs font-bold text-emerald-400">
                    {latest.cpu}%
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => setShowCpuChart(false)}
                  className="rounded p-1 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                  title="Hide CPU chart"
                >
                  <EyeOff className="h-3.5 w-3.5" />
                </button>
              </div>
              <EvilAreaChart
                data={telemetry}
                dataKey="cpu"
                color="#10b981"
                unit="%"
                height={145}
                label="CPU"
              />
            </div>
          )}

          {showMemChart && (
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-300">Memory</span>
                  <span className="font-mono text-xs font-bold text-sky-400">
                    {latest.memory}%
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => setShowMemChart(false)}
                  className="rounded p-1 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                  title="Hide RAM chart"
                >
                  <EyeOff className="h-3.5 w-3.5" />
                </button>
              </div>
              <EvilAreaChart
                data={telemetry}
                dataKey="memory"
                color="#38bdf8"
                unit="%"
                height={145}
                label="Memory"
              />
            </div>
          )}

          {showNetChart && (
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-300">Network I/O</span>
                  <span className="font-mono text-xs font-bold text-orange-400">
                    {latest.netInMbps} Mbps
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => setShowNetChart(false)}
                  className="rounded p-1 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                  title="Hide Network chart"
                >
                  <EyeOff className="h-3.5 w-3.5" />
                </button>
              </div>
              <EvilAreaChart
                data={telemetry}
                dataKey="netInMbps"
                secondaryKey="netOutMbps"
                color="#f97316"
                secondaryColor="#eab308"
                unit=" Mbps"
                height={145}
                label="Inbound"
                secondaryLabel="Outbound"
              />
            </div>
          )}
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        {/* Table Filter Bar */}
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/40 px-4 py-2.5">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-zinc-200">Project Resource Usage</span>
            <span className="font-mono text-[11px] text-zinc-500">
              ({sortedApps.length} of {apps.length})
            </span>
          </div>
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            {(['all', 'running', 'building', 'stopped'] as const).map((st) => (
              <button
                key={st}
                type="button"
                onClick={() => setMonitorStatusFilter(st)}
                className={`rounded-md px-2 py-0.5 text-[11px] font-medium capitalize transition-colors cursor-pointer ${
                  monitorStatusFilter === st
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {st}
              </button>
            ))}
          </div>
        </div>

        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[34%] px-4 py-2.5 font-medium">Project</th>
              <th className="w-[18%] px-4 py-2.5 font-medium">Status</th>
              <th className="w-[16%] px-4 py-2.5 font-medium text-right">CPU</th>
              <th className="w-[16%] px-4 py-2.5 font-medium text-right">Memory</th>
              <th className="w-[16%] px-4 py-2.5 font-medium text-right">Disk</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {sortedApps.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-xs text-zinc-500">
                  No projects match &ldquo;{monitorSearch}&rdquo;
                </td>
              </tr>
            ) : (
              sortedApps.map((app) => {
                const isPinned = pinnedApps.includes(app.slug);
                return (
                  <tr key={app.id} className="hover:bg-white/[0.03] transition-colors">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <button
                          type="button"
                          onClick={() => togglePin(app.slug)}
                          className={`cursor-pointer ${
                            isPinned ? 'text-orange-400' : 'text-zinc-600 hover:text-zinc-300'
                          }`}
                        >
                          <Pin className="h-3.5 w-3.5" />
                        </button>
                        <div className="min-w-0">
                          <span className="block truncate font-mono font-semibold text-zinc-100">
                            {app.name}
                          </span>
                          {app.primaryDomain && (
                            <span className="block truncate font-mono text-[10px] text-zinc-500">
                              {app.primaryDomain}
                            </span>
                          )}
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge status={app.status} />
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                      {app.cpuPct}%
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                      {app.memMB} MB
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-400">
                      {(app.diskMB / 1024).toFixed(2)} GB
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 2. Git Providers & Container Registries (/git & /registries)
// ----------------------------------------------------------------------------
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

  // Git Modals
  const [githubModalOpen, setGithubModalOpen] = useState(false);
  const [gitlabModalOpen, setGitlabModalOpen] = useState(false);

  const [ghAccountInput, setGhAccountInput] = useState('');
  const [ghAuthMethod, setGhAuthMethod] = useState<'GitHub App' | 'Fine-Grained PAT'>('GitHub App');
  const [ghTokenInput, setGhTokenInput] = useState('');

  const [glAccountInput, setGlAccountInput] = useState('');
  const [glInstanceUrl, setGlInstanceUrl] = useState('https://gitlab.com');
  const [glAuthMethod, setGlAuthMethod] = useState<'OAuth 2.0' | 'Deploy Token'>('OAuth 2.0');
  const [glTokenInput, setGlTokenInput] = useState('');

  // Registry Modal State
  const [registryModalOpen, setRegistryModalOpen] = useState(false);
  const [regProvider, setRegProvider] = useState<RegistryCredential['provider']>('GHCR');
  const [regName, setRegName] = useState('GitHub Container Registry');
  const [regHost, setRegHost] = useState('ghcr.io');
  const [regUsername, setRegUsername] = useState('');
  const [regToken, setRegToken] = useState('');
  const [regScope, setRegScope] = useState<'global' | 'project'>('global');

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

  const handleConnectGitHub = (e: React.FormEvent) => {
    e.preventDefault();
    if (!ghAccountInput.trim()) return;
    const created: GitProviderConnection = {
      id: `git-gh-${Date.now()}`,
      provider: 'GitHub',
      authMethod: ghAuthMethod,
      accountName: ghAccountInput.trim(),
      instanceUrl: 'https://github.com',
      status: 'connected',
      reposAccessible: 16,
      webhookDeliveryRate: '100%',
      webhookUrl: 'https://panel.nextdeploy.sh/api/v1/webhooks/github',
      lastSynced: 'Just now',
    };
    setProviders((prev) => [...prev, created]);
    setGhAccountInput('');
    setGhTokenInput('');
    setGithubModalOpen(false);
    toast.success(`Connected GitHub @${created.accountName}`);
  };

  const handleConnectGitLab = (e: React.FormEvent) => {
    e.preventDefault();
    if (!glAccountInput.trim()) return;
    const created: GitProviderConnection = {
      id: `git-gl-${Date.now()}`,
      provider: 'GitLab',
      authMethod: glAuthMethod,
      accountName: glAccountInput.trim(),
      instanceUrl: glInstanceUrl.trim() || 'https://gitlab.com',
      status: 'connected',
      reposAccessible: 12,
      webhookDeliveryRate: '100%',
      webhookUrl: 'https://panel.nextdeploy.sh/api/v1/webhooks/gitlab',
      lastSynced: 'Just now',
    };
    setProviders((prev) => [...prev, created]);
    setGlAccountInput('');
    setGlTokenInput('');
    setGitlabModalOpen(false);
    toast.success(`Connected GitLab @${created.accountName}`);
  };

  const filteredRepos = repos.filter(
    (r) => repoProviderFilter === 'all' || r.provider === repoProviderFilter
  );

  const webhookEvents = [
    {
      id: 'wh-901',
      provider: 'GitHub' as const,
      repo: 'masudranaxpert/commerce-core-api',
      event: 'push (refs/heads/main)',
      sha: '8c4f19a',
      status: '200 OK',
      latency: '112ms',
      time: '4m ago',
    },
    {
      id: 'wh-900',
      provider: 'GitLab' as const,
      repo: 'nextdeploy-internal/temporal-worker-queue',
      event: 'Push Hook (main)',
      sha: '19d04e2',
      status: '200 OK',
      latency: '148ms',
      time: '12m ago',
    },
    {
      id: 'wh-899',
      provider: 'GitHub' as const,
      repo: 'masudranaxpert/studio-web-dashboard',
      event: 'push (refs/heads/production)',
      sha: 'f42a90c',
      status: '200 OK',
      latency: '96ms',
      time: '38m ago',
    },
  ];

  if (mode === 'git') {
    return (
      <div className="space-y-5">
        {/* Header */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <FolderGit2 className="h-4 w-4 text-orange-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">Git Providers</h1>
          </div>

          <div className="flex items-center gap-2">
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

        {/* GitHub & GitLab Provider Integration Cards */}
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
                        label: 'Sync Repositories',
                        icon: <RefreshCw className="h-3.5 w-3.5" />,
                        onClick: () => toast.success(`Synced ${p.accountName}`),
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

        {/* Synced Repositories & Webhook Deliveries Section */}
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
              <table className="w-full table-fixed text-left text-xs">
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
                              onClick: () => toast.success(`Fetched latest HEAD for ${repo.fullName}`),
                            },
                          ]}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
              <table className="w-full table-fixed text-left text-xs">
                <thead>
                  <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                    <th className="w-[36%] px-4 py-2.5 font-medium">Repository</th>
                    <th className="w-[28%] px-4 py-2.5 font-medium">Event & Commit</th>
                    <th className="w-[18%] px-4 py-2.5 font-medium">Response</th>
                    <th className="w-[18%] px-4 py-2.5 font-medium text-right">Time</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.05]">
                  {webhookEvents.map((ev) => (
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
                  ))}
                </tbody>
              </table>
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
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400'
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>
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
            {ghAuthMethod === 'Fine-Grained PAT' && (
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Personal Access Token
                </label>
                <input
                  type="password"
                  required
                  value={ghTokenInput}
                  onChange={(e) => setGhTokenInput(e.target.value)}
                  placeholder="github_pat_..."
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
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
                Connect GitHub
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
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400'
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>
            <div className="grid grid-cols-2 gap-3">
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
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                {glAuthMethod === 'OAuth 2.0' ? 'Application Secret / Token' : 'GitLab Deploy Token'}
              </label>
              <input
                type="password"
                required
                value={glTokenInput}
                onChange={(e) => setGlTokenInput(e.target.value)}
                placeholder="glpat-••••••••••••"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
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
                Connect GitLab
              </button>
            </div>
          </form>
        </ShadcnDialog>
      </div>
    );
  }

  // Container Registries Mode (/registries)
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <HardDrive className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              Container Registries
            </h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {registries.length}
            </span>
          </div>
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

      {/* Connected Registries Table */}
      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[34%] px-4 py-2.5 font-medium">Registry & Host</th>
              <th className="w-[22%] px-4 py-2.5 font-medium">Username & Token</th>
              <th className="w-[18%] px-4 py-2.5 font-medium">Status</th>
              <th className="w-[18%] px-4 py-2.5 font-medium text-right">Usage</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {registries.map((r) => (
              <tr key={r.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <span className="rounded-md border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-orange-400 shrink-0">
                      {r.provider}
                    </span>
                    <div className="min-w-0">
                      <div className="truncate font-semibold text-zinc-100">{r.name}</div>
                      <div className="truncate font-mono text-[11px] text-zinc-500">
                        {r.registryUrl}
                      </div>
                    </div>
                  </div>
                </td>
                <td className="px-4 py-3 font-mono">
                  <div className="truncate text-zinc-200">{r.username}</div>
                  <div className="text-[10px] text-zinc-500">{r.tokenPreview}</div>
                </td>
                <td className="px-4 py-3">
                  <StatusBadge status={r.status} label="Authenticated" />
                </td>
                <td className="px-4 py-3 text-right font-mono text-[11px] text-zinc-300">
                  {r.imagesCount} images · {r.connectedApps} apps
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
                        label: 'Copy Host URL',
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
                          if (onDeleteRegistry) onDeleteRegistry(r.id);
                          toast.success(`Removed ${r.name}`);
                        },
                      },
                    ]}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Add Registry Modal */}
      <ShadcnDialog
        open={registryModalOpen}
        onClose={() => setRegistryModalOpen(false)}
        title="Add Container Registry"
        icon={<HardDrive className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!regUsername.trim()) return;
            onAddRegistry({
              id: `reg-${Date.now()}`,
              provider: regProvider,
              name: regName,
              registryUrl: regHost,
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
            toast.success(`Connected ${regHost}`);
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Registry Provider
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

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Label</label>
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
                Registry Host
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

          <div className="grid grid-cols-2 gap-3">
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

// ----------------------------------------------------------------------------
// 3. Reverse Proxy & TLS Page (/proxy)
// ----------------------------------------------------------------------------
export const CaddyProxyView: React.FC<{ apps: AppStack[] }> = ({ apps }) => {
  const [panelDomain, setPanelDomain] = useState('panel.nextdeploy.sh');
  const [acmeEmail, setAcmeEmail] = useState('masud132213@gmail.com');
  const [panelModalOpen, setPanelModalOpen] = useState(false);
  const [addRouteModalOpen, setAddRouteModalOpen] = useState(false);
  const [caddyfileModalOpen, setCaddyfileModalOpen] = useState(false);

  const [newDomain, setNewDomain] = useState('');
  const [newUpstream, setNewUpstream] = useState('127.0.0.1:4000');

  const initialRoutes = apps.flatMap((a) =>
    a.domains.map((d) => ({
      ...d,
      appName: a.name,
      upstream: `${a.slug}-${d.targetService}:${d.targetPort}`,
    }))
  );

  const [routes, setRoutes] = useState(initialRoutes);
  const [caddyfileContent, setCaddyfileContent] = useState(
    `{\n  email ${acmeEmail}\n  admin 127.0.0.1:2019\n}\n\n${panelDomain} {\n  encode zstd gzip\n  reverse_proxy 127.0.0.1:8080\n}\n`
  );

  const handleAddRoute = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = newDomain
      .trim()
      .toLowerCase()
      .replace(/^https?:\/\//, '');
    if (!clean) return;
    setRoutes((prev) => [
      {
        id: `route-${Date.now()}`,
        domain: clean,
        targetService: 'custom',
        targetPort: 4000,
        tlsProvider: 'LetsEncrypt',
        tlsStatus: 'active',
        forceHttps: true,
        gzip: true,
        createdAt: 'Just now',
        appName: 'custom-upstream',
        upstream: newUpstream,
      },
      ...prev,
    ]);
    setNewDomain('');
    setAddRouteModalOpen(false);
    toast.success(`Added HTTPS route for ${clean}`);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Globe className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">
            Reverse Proxy & TLS
          </h1>
          <StatusBadge status="active" label="Caddy 2.8 Online" />
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setPanelModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Lock className="h-3.5 w-3.5 text-emerald-400" />
            Panel Domain
          </button>
          <button
            type="button"
            onClick={() => setCaddyfileModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <FileCode2 className="h-3.5 w-3.5 text-orange-400" />
            Caddyfile
          </button>
          <button
            type="button"
            onClick={() => setAddRouteModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Add Route
          </button>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[34%] px-4 py-2.5 font-medium">Domain</th>
              <th className="w-[28%] px-4 py-2.5 font-medium">Upstream Target</th>
              <th className="w-[22%] px-4 py-2.5 font-medium">TLS Certificate</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {routes.map((r) => (
              <tr key={r.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3 font-mono font-semibold text-zinc-100 truncate">
                  https://{r.domain}
                </td>
                <td className="px-4 py-3 font-mono text-orange-400 truncate">{r.upstream}</td>
                <td className="px-4 py-3">
                  <StatusBadge status={r.tlsStatus} label="Let's Encrypt TLS 1.3" />
                </td>
                <td className="px-3 py-3 text-right">
                  <ActionMenu
                    items={[
                      {
                        label: 'Open URL',
                        icon: <ExternalLink className="h-3.5 w-3.5" />,
                        onClick: () => window.open(`https://${r.domain}`, '_blank'),
                      },
                      {
                        label: 'Delete Route',
                        icon: <Trash2 className="h-3.5 w-3.5" />,
                        variant: 'danger',
                        onClick: () => {
                          setRoutes((prev) => prev.filter((item) => item.id !== r.id));
                          toast.success(`Removed ${r.domain}`);
                        },
                      },
                    ]}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <ShadcnDialog
        open={panelModalOpen}
        onClose={() => setPanelModalOpen(false)}
        title="Control Plane Domain & ACME"
        icon={<Lock className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setPanelModalOpen(false);
            toast.success(`Updated panel domain to https://${panelDomain}`);
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Panel Domain
            </label>
            <input
              type="text"
              value={panelDomain}
              onChange={(e) => setPanelDomain(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Let's Encrypt Email
            </label>
            <input
              type="email"
              value={acmeEmail}
              onChange={(e) => setAcmeEmail(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setPanelModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Save TLS Config
            </button>
          </div>
        </form>
      </ShadcnDialog>

      <ShadcnDialog
        open={addRouteModalOpen}
        onClose={() => setAddRouteModalOpen(false)}
        title="Add Reverse Proxy Route"
        icon={<Globe className="h-4 w-4" />}
      >
        <form onSubmit={handleAddRoute} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Domain</label>
            <input
              type="text"
              required
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
              placeholder="grafana.nextdeploy.sh"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Upstream Address
            </label>
            <input
              type="text"
              required
              value={newUpstream}
              onChange={(e) => setNewUpstream(e.target.value)}
              placeholder="127.0.0.1:4000"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setAddRouteModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Create Route
            </button>
          </div>
        </form>
      </ShadcnDialog>

      <ShadcnDialog
        open={caddyfileModalOpen}
        onClose={() => setCaddyfileModalOpen(false)}
        title="/etc/caddy/Caddyfile"
        icon={<FileCode2 className="h-4 w-4" />}
        maxWidth="lg"
      >
        <CodeEditorBox
          filename="Caddyfile"
          language="caddyfile"
          value={caddyfileContent}
          onChange={setCaddyfileContent}
          onSave={() => {
            setCaddyfileModalOpen(false);
            toast.success('Reloaded Caddy daemon');
          }}
          height={260}
        />
      </ShadcnDialog>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 4. Backups & S3 Page (/backup)
// ----------------------------------------------------------------------------
interface BackupsViewProps {
  destinations: BackupDestination[];
  archives: BackupArchiveRecord[];
  onTriggerBackup: () => void;
}

export const BackupsView: React.FC<BackupsViewProps> = ({
  destinations: initialDestinations,
  archives,
  onTriggerBackup,
}) => {
  const [destinations, setDestinations] = useState<BackupDestination[]>(initialDestinations);
  const [r2ModalOpen, setR2ModalOpen] = useState(false);
  const [r2Bucket, setR2Bucket] = useState('nextdeploy-prod-backups');
  const [r2Endpoint, setR2Endpoint] = useState('https://91a8c4f.r2.cloudflarestorage.com');

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Archive className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">Backups & S3</h1>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setR2ModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Cloud className="h-3.5 w-3.5 text-orange-400" />
            Add S3 / R2 Destination
          </button>
          <button
            type="button"
            onClick={onTriggerBackup}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Create Snapshot
          </button>
        </div>
      </div>

      {/* Destinations Grid */}
      <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
        {destinations.map((d) => (
          <div
            key={d.id}
            className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 flex items-center justify-between"
          >
            <div className="flex items-center gap-3 min-w-0">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900">
                <Cloud className="h-4 w-4 text-orange-400" />
              </div>
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-100 truncate">{d.name}</span>
                  <StatusBadge status={d.status} />
                </div>
                <div className="mt-0.5 font-mono text-[11px] text-zinc-400 truncate">
                  {d.bucket} · cron {d.scheduleCron}
                </div>
              </div>
            </div>
            <span className="font-mono text-xs text-zinc-300 shrink-0">
              {d.usedGB} / {d.quotaGB} GB
            </span>
          </div>
        ))}
      </div>

      {/* Archives Table */}
      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[44%] px-4 py-2.5 font-medium">Archive</th>
              <th className="w-[22%] px-4 py-2.5 font-medium">Project</th>
              <th className="w-[14%] px-4 py-2.5 font-medium">Created</th>
              <th className="w-[12%] px-4 py-2.5 font-medium text-right">Size</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {archives.map((bk) => (
              <tr key={bk.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3 font-mono font-semibold text-zinc-100 truncate">
                  {bk.archiveName}
                </td>
                <td className="px-4 py-3 font-mono text-orange-400 truncate">{bk.appSlug}</td>
                <td className="px-4 py-3 font-mono text-zinc-400">{bk.createdAt}</td>
                <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                  {(bk.sizeMB / 1024).toFixed(2)} GB
                </td>
                <td className="px-3 py-3 text-right">
                  <ActionMenu
                    items={[
                      {
                        label: 'Restore Snapshot',
                        icon: <RotateCw className="h-3.5 w-3.5" />,
                        onClick: () => toast.success(`Restoring ${bk.archiveName}...`),
                      },
                      {
                        label: 'Download .tar.gz',
                        icon: <Download className="h-3.5 w-3.5" />,
                        onClick: () => toast.success(`Downloading ${bk.archiveName}`),
                      },
                    ]}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <ShadcnDialog
        open={r2ModalOpen}
        onClose={() => setR2ModalOpen(false)}
        title="Connect S3 / Cloudflare R2"
        icon={<Cloud className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setDestinations((prev) => [
              ...prev,
              {
                id: `dest-${Date.now()}`,
                provider: 'Cloudflare R2',
                name: `R2 (${r2Bucket})`,
                bucket: `s3://${r2Bucket}`,
                endpoint: r2Endpoint,
                region: 'auto',
                status: 'connected',
                scheduleCron: '0 3 * * *',
                retentionDays: 30,
                usedGB: 0.1,
                quotaGB: 100,
                lastBackupAt: 'Just now',
              },
            ]);
            setR2ModalOpen(false);
            toast.success('Connected S3/R2 bucket');
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Bucket Name
            </label>
            <input
              type="text"
              required
              value={r2Bucket}
              onChange={(e) => setR2Bucket(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              S3 Endpoint URL
            </label>
            <input
              type="text"
              required
              value={r2Endpoint}
              onChange={(e) => setR2Endpoint(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setR2ModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Connect Bucket
            </button>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 5. Host Migration View (/migrate)
// ----------------------------------------------------------------------------
interface MigrationViewProps {
  apps: AppStack[];
  bundles: MigrationExportBundle[];
  onExportBundle: (selectedSlugs: string[]) => void;
}

export const MigrationView: React.FC<MigrationViewProps> = ({
  apps,
  bundles,
  onExportBundle,
}) => {
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [selectedSlugs, setSelectedSlugs] = useState<string[]>(apps.map((a) => a.slug));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <ArrowLeftRight className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">
            Server Migration
          </h1>
        </div>

        <button
          type="button"
          onClick={() => setExportModalOpen(true)}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
        >
          <Plus className="h-3.5 w-3.5" />
          Create Export Bundle
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[22%] px-4 py-2.5 font-medium">Bundle ID</th>
              <th className="w-[38%] px-4 py-2.5 font-medium">Projects Included</th>
              <th className="w-[16%] px-4 py-2.5 font-medium">Status</th>
              <th className="w-[14%] px-4 py-2.5 font-medium text-right">Size</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {bundles.map((b) => (
              <tr key={b.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3 font-mono font-semibold text-zinc-100">{b.id}</td>
                <td className="px-4 py-3 font-mono text-orange-400 truncate">
                  {b.selectedApps.join(', ')}
                </td>
                <td className="px-4 py-3">
                  <StatusBadge status={b.status} />
                </td>
                <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                  {(b.actualSizeMB / 1024).toFixed(2)} GB
                </td>
                <td className="px-3 py-3 text-right">
                  <ActionMenu
                    items={[
                      {
                        label: 'Copy Migration Token',
                        icon: <Copy className="h-3.5 w-3.5" />,
                        onClick: () => {
                          navigator.clipboard?.writeText(b.token);
                          toast.success('Copied migration token');
                        },
                      },
                      {
                        label: 'Download Bundle',
                        icon: <Download className="h-3.5 w-3.5" />,
                        onClick: () => toast.success(`Downloading ${b.id}.tar.zst`),
                      },
                    ]}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <ShadcnDialog
        open={exportModalOpen}
        onClose={() => setExportModalOpen(false)}
        title="Create Migration Bundle"
        icon={<ArrowLeftRight className="h-4 w-4" />}
      >
        <div className="space-y-3">
          {apps.map((a) => {
            const checked = selectedSlugs.includes(a.slug);
            return (
              <label
                key={a.id}
                className="flex items-center justify-between rounded-lg border border-white/[0.08] bg-[#09090b] px-3 py-2.5 cursor-pointer"
              >
                <span className="font-mono text-xs font-medium text-zinc-100">{a.name}</span>
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={() =>
                    setSelectedSlugs((prev) =>
                      checked ? prev.filter((s) => s !== a.slug) : [...prev, a.slug]
                    )
                  }
                  className="accent-orange-500"
                />
              </label>
            );
          })}
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setExportModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => {
                onExportBundle(selectedSlugs);
                setExportModalOpen(false);
              }}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Generate Bundle
            </button>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
};

// ----------------------------------------------------------------------------
// 6. MCP Server & CLI Sessions (/mcp & /cli-sessions)
// ----------------------------------------------------------------------------
interface McpAndCliViewProps {
  mode: 'mcp' | 'cli';
  tools: McpToolItem[];
  cliSessions: CliSessionItem[];
  onGenerateCliToken: (name: string) => void;
  onRevokeCliSession?: (id: string) => void;
}

export const McpAndCliView: React.FC<McpAndCliViewProps> = ({
  mode,
  tools,
  cliSessions,
  onGenerateCliToken,
  onRevokeCliSession,
}) => {
  // MCP State
  const [mcpTab, setMcpTab] = useState<'tools' | 'rpc-logs'>('tools');
  const [mcpCategory, setMcpCategory] = useState<string>('all');
  const [disabledTools, setDisabledTools] = useState<Record<string, boolean>>({});
  const [clientConfigOpen, setClientConfigOpen] = useState(false);
  const [testToolTarget, setTestToolTarget] = useState<McpToolItem | null>(null);
  const [revealMcpToken, setRevealMcpToken] = useState(false);

  // CLI State
  const [tokenModalOpen, setTokenModalOpen] = useState(false);
  const [installCliOpen, setInstallCliOpen] = useState(false);
  const [tokenLabel, setTokenLabel] = useState('');

  const mcpRpcLogs = [
    {
      id: 'rpc-1',
      client: 'Claude Code CLI',
      tool: 'deploy',
      args: '{"app_id": "commerce-core-api"}',
      status: '200 OK',
      duration: '840ms',
      time: '4m ago',
    },
    {
      id: 'rpc-2',
      client: 'Cursor IDE',
      tool: 'container_logs',
      args: '{"app_id": "commerce-core-api", "tail": 50}',
      status: '200 OK',
      duration: '64ms',
      time: '9m ago',
    },
    {
      id: 'rpc-3',
      client: 'Claude Desktop',
      tool: 'monitor_snapshot',
      args: '{}',
      status: '200 OK',
      duration: '19ms',
      time: '21m ago',
    },
    {
      id: 'rpc-4',
      client: 'Cursor IDE',
      tool: 'env_set',
      args: '{"app_id": "studio-web-dashboard"}',
      status: '200 OK',
      duration: '112ms',
      time: '38m ago',
    },
  ];

  if (mode === 'mcp') {
    const filteredTools = tools.filter(
      (t) => mcpCategory === 'all' || t.category === mcpCategory
    );

    const mcpConfigJson = `{\n  "mcpServers": {\n    "nextdeploy": {\n      "url": "https://panel.nextdeploy.sh/mcp/sse",\n      "headers": {\n        "Authorization": "Bearer nd_mcp_live_98f1a2c4e6b80192"\n      }\n    }\n  }\n}`;

    return (
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <Sparkles className="h-4 w-4 text-emerald-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">MCP Server</h1>
            <StatusBadge status="active" label="SSE Online · 16 Tools" />
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setClientConfigOpen(true)}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <FileCode2 className="h-3.5 w-3.5" />
              Client Setup (Cursor / Claude)
            </button>
          </div>
        </div>

        {/* Endpoint & Bearer Token Bar */}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <div className="flex items-center justify-between rounded-xl border border-white/[0.07] bg-[#111115]/90 px-4 py-3">
            <div className="min-w-0">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 block">
                SSE Endpoint
              </span>
              <span className="font-mono text-xs text-zinc-100 truncate block mt-0.5">
                https://panel.nextdeploy.sh/mcp/sse
              </span>
            </div>
            <button
              type="button"
              onClick={() => {
                navigator.clipboard?.writeText('https://panel.nextdeploy.sh/mcp/sse');
                toast.success('Copied SSE endpoint');
              }}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs text-zinc-300 hover:text-white cursor-pointer"
            >
              Copy
            </button>
          </div>

          <div className="flex items-center justify-between rounded-xl border border-white/[0.07] bg-[#111115]/90 px-4 py-3">
            <div className="min-w-0">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500 block">
                MCP Bearer Key
              </span>
              <span className="font-mono text-xs text-orange-400 truncate block mt-0.5">
                {revealMcpToken
                  ? 'nd_mcp_live_98f1a2c4e6b80192'
                  : 'nd_mcp_live_••••••••••••••••'}
              </span>
            </div>
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => setRevealMcpToken((p) => !p)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 p-1.5 text-zinc-400 hover:text-white cursor-pointer"
              >
                {revealMcpToken ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </button>
              <button
                type="button"
                onClick={() => {
                  navigator.clipboard?.writeText('nd_mcp_live_98f1a2c4e6b80192');
                  toast.success('Copied MCP token');
                }}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs text-zinc-300 hover:text-white cursor-pointer"
              >
                Copy
              </button>
            </div>
          </div>
        </div>

        {/* Sub-Tabs & Category Filter */}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            <button
              type="button"
              onClick={() => setMcpTab('tools')}
              className={`rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                mcpTab === 'tools'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Tools Catalog ({filteredTools.length})
            </button>
            <button
              type="button"
              onClick={() => setMcpTab('rpc-logs')}
              className={`rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                mcpTab === 'rpc-logs'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Live RPC Stream ({mcpRpcLogs.length})
            </button>
          </div>

          {mcpTab === 'tools' && (
            <div className="flex flex-wrap items-center gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
              {(
                [
                  'all',
                  'Deploy & Compose',
                  'Workspace & Files',
                  'Logs & Telemetry',
                  'Domains & Env',
                ] as const
              ).map((cat) => (
                <button
                  key={cat}
                  type="button"
                  onClick={() => setMcpCategory(cat)}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer ${
                    mcpCategory === cat
                      ? 'bg-zinc-800 text-zinc-100'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {cat === 'all' ? 'All' : cat}
                </button>
              ))}
            </div>
          )}
        </div>

        {mcpTab === 'tools' ? (
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
            <table className="w-full table-fixed text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                  <th className="w-[24%] px-4 py-2.5 font-medium">Tool</th>
                  <th className="w-[40%] px-4 py-2.5 font-medium">Signature</th>
                  <th className="w-[20%] px-4 py-2.5 font-medium">Category</th>
                  <th className="w-[10%] px-4 py-2.5 font-medium">Enabled</th>
                  <th className="w-16 px-3 py-2.5 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {filteredTools.map((t) => {
                  const enabled = !disabledTools[t.name];
                  return (
                    <tr key={t.name} className="hover:bg-white/[0.03]">
                      <td className="px-4 py-3 font-mono font-semibold text-orange-400 truncate">
                        {t.name}
                      </td>
                      <td className="px-4 py-3 font-mono text-[11px] text-zinc-300 truncate">
                        {t.signature}
                      </td>
                      <td className="px-4 py-3 font-mono text-[11px] text-zinc-400 truncate">
                        {t.category}
                      </td>
                      <td className="px-4 py-3">
                        <ShadcnSwitch
                          checked={enabled}
                          onChange={() =>
                            setDisabledTools((prev) => ({
                              ...prev,
                              [t.name]: !prev[t.name],
                            }))
                          }
                        />
                      </td>
                      <td className="px-3 py-3 text-right">
                        <button
                          type="button"
                          onClick={() => setTestToolTarget(t)}
                          className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-1 font-mono text-[11px] text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
                        >
                          Test
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
            <table className="w-full table-fixed text-left text-xs font-mono">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400 font-sans">
                  <th className="w-[22%] px-4 py-2.5 font-medium">Client</th>
                  <th className="w-[20%] px-4 py-2.5 font-medium">Tool</th>
                  <th className="w-[34%] px-4 py-2.5 font-medium">Arguments</th>
                  <th className="w-[14%] px-4 py-2.5 font-medium">Status</th>
                  <th className="w-[10%] px-4 py-2.5 font-medium text-right">Time</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {mcpRpcLogs.map((log) => (
                  <tr key={log.id} className="hover:bg-white/[0.03]">
                    <td className="px-4 py-3 text-zinc-100 font-sans font-medium">{log.client}</td>
                    <td className="px-4 py-3 text-orange-400">{log.tool}</td>
                    <td className="px-4 py-3 text-zinc-400 truncate">{log.args}</td>
                    <td className="px-4 py-3 text-emerald-400">
                      {log.status} ({log.duration})
                    </td>
                    <td className="px-4 py-3 text-right text-zinc-500">{log.time}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* Client Setup Modal */}
        <ShadcnDialog
          open={clientConfigOpen}
          onClose={() => setClientConfigOpen(false)}
          title="Connect Cursor / Claude Desktop"
          icon={<Sparkles className="h-4 w-4 text-emerald-400" />}
          maxWidth="lg"
        >
          <CodeEditorBox
            filename="mcp.json"
            language="json"
            value={mcpConfigJson}
            onChange={() => {}}
            readOnly
            height={210}
          />
        </ShadcnDialog>

        {/* Interactive Tool Tester Modal */}
        <ShadcnDialog
          open={Boolean(testToolTarget)}
          onClose={() => setTestToolTarget(null)}
          title={testToolTarget ? `Invoke Tool: ${testToolTarget.name}` : 'Test Tool'}
          icon={<Play className="h-4 w-4 text-orange-400" />}
        >
          {testToolTarget && (
            <div className="space-y-3 font-mono text-xs">
              <div className="rounded-lg border border-white/[0.07] bg-[#070709] p-3 space-y-1.5">
                <div className="text-zinc-500">// JSON-RPC 2.0 Response (18ms)</div>
                <div className="text-emerald-400">
                  {`{"jsonrpc": "2.0", "status": "ok", "tool": "${testToolTarget.name}", "host": "prod-eu-node-01"}`}
                </div>
              </div>
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => {
                    toast.success(`Executed ${testToolTarget.name}`);
                    setTestToolTarget(null);
                  }}
                  className="rounded-lg bg-zinc-100 px-4 py-1.5 font-sans text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
                >
                  Done
                </button>
              </div>
            </div>
          )}
        </ShadcnDialog>
      </div>
    );
  }

  // CLI Sessions Mode (/cli-sessions)
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <KeyRound className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">CLI Sessions</h1>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {cliSessions.length}
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setInstallCliOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Terminal className="h-3.5 w-3.5 text-orange-400" />
            Install CLI
          </button>
          <button
            type="button"
            onClick={() => setTokenModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Generate Token
          </button>
        </div>
      </div>

      {/* Active Sessions Table */}
      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[30%] px-4 py-2.5 font-medium">Workstation & Token</th>
              <th className="w-[22%] px-4 py-2.5 font-medium">Host & OS</th>
              <th className="w-[26%] px-4 py-2.5 font-medium">Last Command</th>
              <th className="w-[14%] px-4 py-2.5 font-medium text-right">Active</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {cliSessions.map((s) => (
              <tr key={s.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3">
                  <div className="font-semibold text-zinc-100 truncate">{s.tokenName}</div>
                  <div className="font-mono text-[11px] text-zinc-500">
                    {s.prefix} · {s.ipAddress}
                  </div>
                </td>
                <td className="px-4 py-3 font-mono text-zinc-300 truncate">
                  <div>{s.deviceHost}</div>
                  <div className="text-[10px] text-zinc-500">
                    {s.osArch} · {s.cliVersion}
                  </div>
                </td>
                <td className="px-4 py-3 font-mono text-orange-400 truncate">{s.lastCommand}</td>
                <td className="px-4 py-3 text-right font-mono text-zinc-400">{s.lastActiveAt}</td>
                <td className="px-3 py-3 text-right">
                  <ActionMenu
                    items={[
                      {
                        label: 'Copy Token Prefix',
                        icon: <Copy className="h-3.5 w-3.5" />,
                        onClick: () => {
                          navigator.clipboard?.writeText(s.prefix);
                          toast.success(`Copied ${s.prefix}`);
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
            ))}
          </tbody>
        </table>
      </div>

      {/* Install CLI Modal */}
      <ShadcnDialog
        open={installCliOpen}
        onClose={() => setInstallCliOpen(false)}
        title="Install NextDeploy CLI (nd)"
        icon={<Terminal className="h-4 w-4" />}
      >
        <div className="space-y-3 font-mono text-xs">
          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 flex items-center justify-between">
            <span className="text-zinc-200 truncate">
              curl -fsSL https://nextdeploy.sh/install.sh | sh
            </span>
            <button
              type="button"
              onClick={() => {
                navigator.clipboard?.writeText('curl -fsSL https://nextdeploy.sh/install.sh | sh');
                toast.success('Copied install script');
              }}
              className="text-orange-400 hover:underline cursor-pointer shrink-0 ml-2"
            >
              Copy
            </button>
          </div>
          <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3 space-y-1 text-[11px] text-zinc-400">
            <div>$ nd login https://panel.nextdeploy.sh</div>
            <div>$ nd push --app commerce-core-api --deploy</div>
            <div>$ nd logs commerce-core-api --follow</div>
          </div>
        </div>
      </ShadcnDialog>

      {/* Generate CLI Token Modal */}
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

// ----------------------------------------------------------------------------
// 7. Users, Audit Logs & Settings (/users, /audit-logs, /settings)
// ----------------------------------------------------------------------------
interface UsersAndSettingsViewProps {
  mode: 'users' | 'audit-logs' | 'settings';
  users: PanelUser[];
  auditLogs: AuditLogEntry[];
}

export const UsersAndSettingsView: React.FC<UsersAndSettingsViewProps> = ({
  mode,
  users: initialUsers,
  auditLogs,
}) => {
  const [users, setUsers] = useState<PanelUser[]>(initialUsers);
  const [userModalOpen, setUserModalOpen] = useState(false);
  const [newUsername, setNewUsername] = useState('');
  const [newEmail, setNewEmail] = useState('');
  const [newMaxApps, setNewMaxApps] = useState('10');
  const [newMemQuota, setNewMemQuota] = useState('16');

  if (mode === 'users') {
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
              <Users className="h-4 w-4 text-orange-400" />
            </div>
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              Team & Quotas
            </h1>
          </div>

          <button
            type="button"
            onClick={() => setUserModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Add User
          </button>
        </div>

        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <table className="w-full table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[32%] px-4 py-2.5 font-medium">User</th>
                <th className="w-[16%] px-4 py-2.5 font-medium">Role & 2FA</th>
                <th className="w-[22%] px-4 py-2.5 font-medium">Projects Quota</th>
                <th className="w-[20%] px-4 py-2.5 font-medium text-right">Memory Quota</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {users.map((u) => (
                <tr key={u.id} className="hover:bg-white/[0.03]">
                  <td className="px-4 py-3">
                    <div className="font-semibold text-zinc-100">{u.username}</div>
                    <div className="font-mono text-[11px] text-zinc-500">{u.email}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-1.5">
                      <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[10px] text-orange-400 uppercase">
                        {u.role}
                      </span>
                      {u.twoFactorEnabled && (
                        <span className="rounded-md bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] text-emerald-400">
                          2FA
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 font-mono text-zinc-300">
                    {u.appsCount} / {u.maxApps} projects
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-zinc-300">
                    {u.memUsedGB} / {u.memQuotaGB} GB
                  </td>
                  <td className="px-3 py-3 text-right">
                    <ActionMenu
                      items={[
                        {
                          label: 'Reset 2FA / Password',
                          icon: <KeyRound className="h-3.5 w-3.5" />,
                          onClick: () => toast.success(`Sent reset link to ${u.email}`),
                        },
                        {
                          label: 'Remove User',
                          icon: <Trash2 className="h-3.5 w-3.5" />,
                          variant: 'danger',
                          disabled: u.username === 'masud',
                          onClick: () => {
                            setUsers((prev) => prev.filter((item) => item.id !== u.id));
                            toast.success(`Removed ${u.username}`);
                          },
                        },
                      ]}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <ShadcnDialog
          open={userModalOpen}
          onClose={() => setUserModalOpen(false)}
          title="Create Panel User"
          icon={<Users className="h-4 w-4" />}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (!newUsername.trim()) return;
              setUsers((prev) => [
                ...prev,
                {
                  id: `u-${Date.now()}`,
                  username: newUsername.trim(),
                  email: newEmail.trim() || `${newUsername}@nextdeploy.sh`,
                  role: 'user',
                  appsCount: 0,
                  maxApps: Number(newMaxApps) || 10,
                  cpuQuotaCores: 4,
                  memUsedGB: 0,
                  memQuotaGB: Number(newMemQuota) || 16,
                  diskUsedGB: 0,
                  diskQuotaGB: 50,
                  twoFactorEnabled: false,
                  lastLoginAt: 'Never',
                },
              ]);
              setNewUsername('');
              setNewEmail('');
              setUserModalOpen(false);
              toast.success('Created user');
            }}
            className="space-y-4"
          >
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Username
                </label>
                <input
                  type="text"
                  required
                  value={newUsername}
                  onChange={(e) => setNewUsername(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
                />
              </div>
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Email</label>
                <input
                  type="email"
                  required
                  value={newEmail}
                  onChange={(e) => setNewEmail(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  Max Projects Quota
                </label>
                <input
                  type="number"
                  value={newMaxApps}
                  onChange={(e) => setNewMaxApps(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
              <div>
                <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                  RAM Quota (GB)
                </label>
                <input
                  type="number"
                  value={newMemQuota}
                  onChange={(e) => setNewMemQuota(e.target.value)}
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                />
              </div>
            </div>
            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setUserModalOpen(false)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Create User
              </button>
            </div>
          </form>
        </ShadcnDialog>
      </div>
    );
  }

  if (mode === 'audit-logs') {
    return (
      <div className="space-y-4">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <FileText className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">Audit Trail</h1>
        </div>

        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
          <table className="w-full table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[22%] px-4 py-2.5 font-medium">Action</th>
                <th className="w-[18%] px-4 py-2.5 font-medium">Actor</th>
                <th className="w-[34%] px-4 py-2.5 font-medium">Target</th>
                <th className="w-[12%] px-4 py-2.5 font-medium">Status</th>
                <th className="w-[14%] px-4 py-2.5 font-medium text-right">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {auditLogs.map((log) => (
                <tr key={log.id} className="hover:bg-white/[0.03]">
                  <td className="px-4 py-3 font-mono font-semibold text-orange-400 truncate">
                    {log.action}
                  </td>
                  <td className="px-4 py-3 text-zinc-200 truncate">
                    {log.actor}{' '}
                    <span className="font-mono text-[10px] text-zinc-500">({log.actorRole})</span>
                  </td>
                  <td className="px-4 py-3 font-mono text-zinc-400 truncate">{log.target}</td>
                  <td className="px-4 py-3">
                    <StatusBadge
                      status={log.status === 'success' ? 'ready' : 'failed'}
                      label={log.status}
                    />
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-[11px] text-zinc-500">
                    {log.timestamp.slice(11)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Settings className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">System Settings</h1>
        </div>

        <button
          type="button"
          onClick={() => toast.success('Saved control plane settings')}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
        >
          Save Changes
        </button>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 space-y-3">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-orange-400" />
            <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
              Daemon & Runtime
            </h2>
          </div>
          <div className="space-y-2 font-mono text-xs">
            <div className="flex justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-500">Apps Directory</span>
              <span className="text-zinc-100">/data/apps</span>
            </div>
            <div className="flex justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-500">Docker Socket</span>
              <span className="text-emerald-400">unix:///var/run/docker.sock</span>
            </div>
            <div className="flex justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-500">Caddy Admin API</span>
              <span className="text-zinc-100">http://127.0.0.1:2019</span>
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 space-y-3">
          <div className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-emerald-400" />
            <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
              Security & Automation
            </h2>
          </div>
          <div className="space-y-2.5 text-xs">
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-200">Daily Unused Image Prune</span>
              <ShadcnSwitch checked={true} onChange={() => toast.success('Updated setting')} />
            </div>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-200">Automatic Let's Encrypt Renewal</span>
              <ShadcnSwitch checked={true} onChange={() => toast.success('Updated setting')} />
            </div>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
              <span className="text-zinc-200">Enforce 2FA for Destructive Actions</span>
              <ShadcnSwitch checked={true} onChange={() => toast.success('Updated setting')} />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
