import React, { useState } from 'react';
import {
  Activity,
  Archive,
  ArrowLeftRight,
  ArrowUpRight,
  Box,
  ChevronRight,
  Copy,
  ExternalLink,
  FileText,
  FolderGit2,
  GitBranch,
  Globe,
  HardDrive,
  Kanban,
  KeyRound,
  Layers,
  LayoutGrid,
  List,
  Lock,
  Menu,
  Play,
  Plus,
  RotateCw,
  Search,
  Server,
  Settings,
  ShieldCheck,
  Sparkles,
  Square,
  Terminal,
  Trash2,
  Users,
  X,
} from 'lucide-react';
import { Toaster, toast } from 'sonner';
import {
  AppStack,
  BackupArchiveRecord,
  CliSessionItem,
  DockerImageItem,
  MigrationExportBundle,
  NavigationSection,
  RegistryCredential,
  TelemetryPoint,
} from './types/nextdeploy';
import {
  INITIAL_APPS,
  INITIAL_AUDIT_LOGS,
  INITIAL_BACKUP_ARCHIVES,
  INITIAL_BACKUP_DESTINATIONS,
  INITIAL_CLI_SESSIONS,
  INITIAL_GIT_PROVIDERS,
  INITIAL_IMAGES,
  INITIAL_MIGRATION_BUNDLES,
  INITIAL_REGISTRIES,
  INITIAL_SYNCED_REPOS,
  INITIAL_TELEMETRY,
  INITIAL_USERS,
  INITIAL_VOLUMES,
  MCP_TOOLS_CATALOG,
} from './data/initialData';
import {
  ActionMenu,
  CommandPaletteModal,
  DeployNewAppModal,
  EvilAreaChart,
  StatusBadge,
} from './components/ui/Primitives';
import { AppWorkspace } from './components/AppWorkspace';
import {
  ContainersView,
  ImagesView,
  VolumesView,
  VpsTerminalView,
} from './components/DockerEngineViews';
import {
  BackupsView,
  CaddyProxyView,
  GitAndRegistriesView,
  McpAndCliView,
  MigrationView,
  MonitorView,
  UsersAndSettingsView,
} from './components/InfraAndAiViews';
import { backendApi } from './lib/backendApi';

export default function App() {
  const [activeNav, setActiveNav] = useState<NavigationSection>('overview');
  const [selectedAppId, setSelectedAppId] = useState<string | null>(null);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  // Core State
  const [apps, setApps] = useState<AppStack[]>(INITIAL_APPS);
  const [telemetry, setTelemetry] = useState<TelemetryPoint[]>(INITIAL_TELEMETRY);
  const [images, setImages] = useState<DockerImageItem[]>(INITIAL_IMAGES);
  const [registries, setRegistries] = useState<RegistryCredential[]>(INITIAL_REGISTRIES);
  const [backupArchives, setBackupArchives] =
    useState<BackupArchiveRecord[]>(INITIAL_BACKUP_ARCHIVES);
  const [migrationBundles, setMigrationBundles] =
    useState<MigrationExportBundle[]>(INITIAL_MIGRATION_BUNDLES);
  const [cliSessions, setCliSessions] = useState<CliSessionItem[]>(INITIAL_CLI_SESSIONS);

  // View Controls
  const [chartMetric, setChartMetric] = useState<'requests' | 'cpu-mem' | 'network'>('requests');
  const [appsLayout, setAppsLayout] = useState<'grid' | 'board' | 'table'>('grid');
  const [appSearch, setAppSearch] = useState('');
  const [appStatusFilter, setAppStatusFilter] = useState<
    'all' | 'running' | 'building' | 'stopped'
  >('all');

  // Modals
  const [cmdPaletteOpen, setCmdPaletteOpen] = useState(false);
  const [newAppModalOpen, setNewAppModalOpen] = useState(false);

  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setCmdPaletteOpen((prev) => !prev);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  // Hydrate live Go Fiber backend state when running inside the embedded NextDeploy binary
  React.useEffect(() => {
    void backendApi.bootstrap().then((data) => {
      if (!data) return;
      if (Array.isArray(data.cli_sessions) && data.cli_sessions.length > 0) {
        setCliSessions(
          data.cli_sessions.map((s) => ({
            id: s.id,
            tokenName: s.name || 'CLI Session',
            prefix: s.token_prefix || 'nd_cli_...',
            deviceHost: s.hostname || 'workstation',
            osArch: `${s.os || 'linux'}/${s.arch || 'amd64'}`,
            cliVersion: 'nd v2.0',
            lastCommand: 'nd status',
            lastActiveAt: s.last_used_at || s.created_at || 'Active',
            ipAddress: s.ip || '127.0.0.1',
          }))
        );
      }
    });
  }, []);

  const appendTelemetryTick = () => {
    const now = new Date();
    const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(
      now.getMinutes()
    ).padStart(2, '0')}`;
    const nextPoint: TelemetryPoint = {
      time: timeStr,
      cpu: Math.floor(19 + Math.random() * 22),
      memory: Math.floor(46 + Math.random() * 6),
      diskIo: Math.floor(12 + Math.random() * 25),
      netInMbps: Math.floor(32 + Math.random() * 28),
      netOutMbps: Math.floor(24 + Math.random() * 22),
      requests: Math.floor(1650 + Math.random() * 750),
    };
    setTelemetry((prev) => [...prev.slice(1), nextPoint]);
  };

  const handleCreateApp = (payload: {
    name: string;
    description: string;
    sourceType: 'git' | 'compose' | 'zip';
    gitRepo?: string;
    gitBranch?: string;
    primaryDomain?: string;
    composeContent: string;
  }) => {
    const slug = payload.name
      .toLowerCase()
      .replace(/[^a-z0-9-]+/g, '-')
      .replace(/^-|-$/g, '');
    void backendApi.createApp({
      name: slug,
      description: payload.description,
      sourceType: payload.sourceType,
      gitRepo: payload.gitRepo,
      gitBranch: payload.gitBranch,
      composeContent: payload.composeContent,
    });
    const created: AppStack = {
      id: `app-${Math.random().toString(16).slice(2, 10)}`,
      name: slug,
      slug,
      description: payload.description,
      sourceType: payload.sourceType,
      gitRepo: payload.gitRepo,
      gitBranch: payload.gitBranch || 'main',
      gitCommitSha: payload.sourceType === 'git' ? 'e91b40a' : 'compose',
      gitCommitMessage: 'Initial deploy',
      webhookAutoDeploy: payload.sourceType === 'git',
      status: 'running',
      primaryDomain: payload.primaryDomain,
      composeFilePath: 'docker-compose.yml',
      owner: 'masud',
      updatedAt: 'Just now',
      cpuPct: 4.1,
      memMB: 256,
      diskMB: 512,
      reqPerMin: 180,
      services: [
        {
          id: `c-${Math.random().toString(16).slice(2, 9)}`,
          name: `${slug}-app-1`,
          service: 'app',
          image: `ghcr.io/masudranaxpert/${slug}:latest`,
          state: 'running',
          uptime: 'Just started',
          cpuPct: 4.1,
          memUsedMB: 256,
          memLimitMB: 1024,
          ports: '3000/tcp',
        },
      ],
      domains: payload.primaryDomain
        ? [
            {
              id: `dom-${Date.now()}`,
              domain: payload.primaryDomain,
              targetService: 'app',
              targetPort: 3000,
              tlsProvider: 'LetsEncrypt',
              tlsStatus: 'active',
              forceHttps: true,
              gzip: true,
              createdAt: 'Just now',
            },
          ]
        : [],
      files: [
        {
          path: 'docker-compose.yml',
          name: 'docker-compose.yml',
          type: 'file',
          sizeBytes: payload.composeContent.length,
          updatedAt: 'Just now',
          language: 'yaml',
          content: payload.composeContent,
        },
      ],
      envVars: [{ id: `ev-${Date.now()}`, key: 'PORT', value: '3000', isSecret: false }],
      deployments: [
        {
          id: `dep-${Math.floor(5000 + Math.random() * 900)}`,
          commitSha: 'e91b40a',
          commitMessage: 'Initial deploy',
          branch: payload.gitBranch || 'main',
          trigger: 'manual-compose',
          author: 'masud',
          status: 'ready',
          durationSec: 14,
          startedAt: 'Just now',
          buildSteps: [],
        },
      ],
      logs: [
        {
          id: `log-${Date.now()}`,
          timestamp: new Date().toISOString(),
          container: 'app',
          level: 'info',
          message: `Container ${slug}-app-1 started on :3000`,
        },
      ],
      volumes: [],
      collaborators: [
        {
          userId: 'u1',
          username: 'masud',
          email: 'masud132213@gmail.com',
          role: 'owner',
          addedAt: 'Just now',
        },
      ],
    };

    setApps((prev) => [created, ...prev]);
    setSelectedAppId(created.id);
    toast.success(`Created ${created.name}`);
  };

  const selectedApp = apps.find((a) => a.id === selectedAppId) || null;

  const filteredApps = apps.filter((a) => {
    if (appStatusFilter !== 'all' && a.status !== appStatusFilter) return false;
    if (
      appSearch &&
      !a.name.toLowerCase().includes(appSearch.toLowerCase()) &&
      !(a.primaryDomain && a.primaryDomain.toLowerCase().includes(appSearch.toLowerCase()))
    ) {
      return false;
    }
    return true;
  });

  const navGroups: {
    group: string;
    items: {
      id: NavigationSection;
      label: string;
      icon: React.ReactNode;
      badge?: { text: string; tone: 'emerald' | 'violet' | 'count' };
    }[];
  }[] = [
    {
      group: 'Platform',
      items: [
        { id: 'overview', label: 'Overview', icon: <LayoutGrid className="h-3.5 w-3.5" /> },
        {
          id: 'apps',
          label: 'Projects',
          icon: <Layers className="h-3.5 w-3.5" />,
          badge: { text: String(apps.length), tone: 'count' },
        },
        { id: 'monitor', label: 'Monitoring', icon: <Activity className="h-3.5 w-3.5" /> },
      ],
    },
    {
      group: 'Docker Engine',
      items: [
        { id: 'containers', label: 'Containers', icon: <Box className="h-3.5 w-3.5" /> },
        { id: 'images', label: 'Images', icon: <Layers className="h-3.5 w-3.5" /> },
        { id: 'volumes', label: 'Volumes', icon: <HardDrive className="h-3.5 w-3.5" /> },
        { id: 'terminal', label: 'Host Terminal', icon: <Terminal className="h-3.5 w-3.5" /> },
      ],
    },
    {
      group: 'Integrations',
      items: [
        { id: 'git', label: 'Git Providers', icon: <FolderGit2 className="h-3.5 w-3.5" /> },
        { id: 'registries', label: 'Registries', icon: <HardDrive className="h-3.5 w-3.5" /> },
        { id: 'backup', label: 'Backups & S3', icon: <Archive className="h-3.5 w-3.5" /> },
      ],
    },
    {
      group: 'Automation',
      items: [
        {
          id: 'mcp',
          label: 'MCP Server',
          icon: <Sparkles className="h-3.5 w-3.5" />,
          badge: { text: 'AI', tone: 'emerald' },
        },
        {
          id: 'cli-sessions',
          label: 'CLI Sessions',
          icon: <KeyRound className="h-3.5 w-3.5" />,
          badge: { text: 'nd', tone: 'violet' },
        },
      ],
    },
    {
      group: 'System',
      items: [
        { id: 'proxy', label: 'Traefik / Caddy', icon: <Globe className="h-3.5 w-3.5" /> },
        { id: 'migrate', label: 'Migration', icon: <ArrowLeftRight className="h-3.5 w-3.5" /> },
        { id: 'users', label: 'Team & Quotas', icon: <Users className="h-3.5 w-3.5" /> },
        { id: 'audit-logs', label: 'Audit Trail', icon: <FileText className="h-3.5 w-3.5" /> },
        { id: 'settings', label: 'Settings', icon: <Settings className="h-3.5 w-3.5" /> },
      ],
    },
  ];

  const currentNavLabel =
    navGroups.flatMap((g) => g.items).find((i) => i.id === activeNav)?.label || 'Overview';

  const getAppActions = (app: AppStack) => [
    {
      label: 'Open Workspace',
      icon: <ExternalLink className="h-3.5 w-3.5" />,
      onClick: () => setSelectedAppId(app.id),
    },
    {
      label: 'Trigger Deploy',
      icon: <Play className="h-3.5 w-3.5" />,
      onClick: () => toast.success(`Deploying ${app.name}...`),
    },
    {
      label: 'Copy App ID',
      icon: <Copy className="h-3.5 w-3.5" />,
      onClick: () => {
        navigator.clipboard?.writeText(app.id);
        toast.success(`Copied ${app.id}`);
      },
    },
    {
      label: 'Restart Containers',
      icon: <RotateCw className="h-3.5 w-3.5" />,
      onClick: () => toast.success(`Restarted ${app.name}`),
    },
    {
      label: app.status === 'stopped' ? 'Start Stack' : 'Stop Stack',
      icon:
        app.status === 'stopped' ? (
          <Play className="h-3.5 w-3.5" />
        ) : (
          <Square className="h-3.5 w-3.5" />
        ),
      onClick: () => {
        const nextStatus = app.status === 'stopped' ? ('running' as const) : ('stopped' as const);
        setApps((prev) =>
          prev.map((a) => (a.id === app.id ? { ...a, status: nextStatus } : a))
        );
        toast.success(`${nextStatus === 'running' ? 'Started' : 'Stopped'} ${app.name}`);
      },
    },
    {
      label: 'Delete Project',
      icon: <Trash2 className="h-3.5 w-3.5" />,
      variant: 'danger' as const,
      onClick: () => {
        setApps((prev) => prev.filter((a) => a.id !== app.id));
        toast.success(`Deleted ${app.name}`);
      },
    },
  ];

  const renderAppsCollection = () => (
    <div className="space-y-3.5">
      <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={appSearch}
              onChange={(e) => setAppSearch(e.target.value)}
              placeholder="Filter projects..."
              className="h-8 w-52 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-3 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none transition-colors"
            />
          </div>
          <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
            {(['all', 'running', 'building', 'stopped'] as const).map((st) => (
              <button
                key={st}
                type="button"
                onClick={() => setAppStatusFilter(st)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium capitalize transition-all cursor-pointer ${
                  appStatusFilter === st
                    ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {st}
              </button>
            ))}
          </div>
        </div>

        <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5 self-start sm:self-auto">
          <button
            type="button"
            onClick={() => setAppsLayout('grid')}
            className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-all ${
              appsLayout === 'grid'
                ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <LayoutGrid className="h-3.5 w-3.5" />
            Cards
          </button>
          <button
            type="button"
            onClick={() => setAppsLayout('board')}
            className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-all ${
              appsLayout === 'board'
                ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <Kanban className="h-3.5 w-3.5" />
            Board
          </button>
          <button
            type="button"
            onClick={() => setAppsLayout('table')}
            className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer transition-all ${
              appsLayout === 'table'
                ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <List className="h-3.5 w-3.5" />
            List
          </button>
        </div>
      </div>

      {/* Cards View */}
      {appsLayout === 'grid' && (
        <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
          {filteredApps.map((app) => (
            <div
              key={app.id}
              onClick={() => setSelectedAppId(app.id)}
              className="group relative overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/80 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] transition-all hover:border-white/[0.15] hover:bg-[#141419] cursor-pointer flex flex-col justify-between gap-3.5"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3 min-w-0">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900 font-display text-xs font-bold text-orange-400 uppercase shadow-inner group-hover:border-orange-500/30 transition-colors">
                    {app.name.slice(0, 2)}
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="truncate text-sm font-semibold text-zinc-100 group-hover:text-white transition-colors">
                        {app.name}
                      </h3>
                      <StatusBadge status={app.status} />
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 text-[11px] text-zinc-400">
                      {app.primaryDomain ? (
                        <span className="inline-flex items-center gap-1 font-mono text-zinc-300 group-hover:text-orange-300 transition-colors truncate">
                          <Globe className="h-3 w-3 text-emerald-400 shrink-0" />
                          {app.primaryDomain}
                          <ArrowUpRight className="h-2.5 w-2.5 opacity-60 shrink-0" />
                        </span>
                      ) : (
                        <span className="font-mono text-zinc-500">internal compose network</span>
                      )}
                    </div>
                  </div>
                </div>

                <ActionMenu items={getAppActions(app)} />
              </div>

              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-1.5">
                  {app.services.map((svc) => (
                    <span
                      key={svc.id}
                      className="inline-flex items-center gap-1.5 rounded-md border border-white/[0.06] bg-[#09090b]/90 px-2 py-0.5 font-mono text-[10px] text-zinc-300"
                    >
                      <span
                        className={`h-1.5 w-1.5 rounded-full ${
                          svc.state === 'running' ? 'bg-emerald-400' : 'bg-zinc-500'
                        }`}
                      />
                      {svc.service}
                    </span>
                  ))}
                </div>

                {app.gitBranch && (
                  <span className="inline-flex items-center gap-1 rounded-md border border-white/[0.06] bg-[#09090b]/60 px-2 py-0.5 font-mono text-[10px] text-zinc-400">
                    <GitBranch className="h-2.5 w-2.5 text-orange-400" />
                    {app.gitBranch}
                    {app.gitCommitSha && (
                      <span className="text-zinc-500">@{app.gitCommitSha.slice(0, 7)}</span>
                    )}
                  </span>
                )}
              </div>

              <div className="flex items-center justify-between border-t border-white/[0.05] pt-2.5 text-[11px] text-zinc-500 font-mono tabular-nums">
                <div className="flex items-center gap-3">
                  <span>
                    CPU <strong className="font-medium text-zinc-300">{app.cpuPct}%</strong>
                  </span>
                  <span>
                    RAM <strong className="font-medium text-zinc-300">{app.memMB} MB</strong>
                  </span>
                </div>
                <span>{app.updatedAt}</span>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Board View */}
      {appsLayout === 'board' && (
        <div className="grid grid-cols-1 gap-3.5 lg:grid-cols-3">
          {(
            [
              { key: 'running', title: 'Running' },
              { key: 'building', title: 'Building' },
              { key: 'stopped', title: 'Stopped' },
            ] as const
          ).map((col) => {
            const colApps = filteredApps.filter((a) => a.status === col.key);
            return (
              <div
                key={col.key}
                className="rounded-xl border border-white/[0.07] bg-[#09090b]/60 p-3.5 space-y-2.5"
              >
                <div className="flex items-center justify-between text-xs font-semibold text-zinc-300">
                  <StatusBadge status={col.key} label={col.title} />
                  <span className="font-mono text-zinc-500">{colApps.length}</span>
                </div>
                <div className="space-y-2">
                  {colApps.map((app) => (
                    <div
                      key={app.id}
                      onClick={() => setSelectedAppId(app.id)}
                      className="flex items-center justify-between rounded-xl border border-white/[0.07] bg-[#111115] p-3 hover:border-white/[0.15] cursor-pointer transition-all"
                    >
                      <div className="min-w-0">
                        <div className="truncate text-xs font-semibold text-zinc-100">
                          {app.name}
                        </div>
                        <div className="mt-0.5 font-mono text-[11px] text-zinc-500">
                          {app.services.length} services · {app.cpuPct}% CPU
                        </div>
                      </div>
                      <ActionMenu items={getAppActions(app)} />
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Table View */}
      {appsLayout === 'table' && (
        <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/80">
          <table className="w-full table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                <th className="w-[38%] px-4 py-2.5 font-medium">Project</th>
                <th className="w-[20%] px-4 py-2.5 font-medium">Status</th>
                <th className="w-[20%] px-4 py-2.5 font-medium">Containers</th>
                <th className="w-[16%] px-4 py-2.5 font-medium text-right">Usage</th>
                <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {filteredApps.map((app) => (
                <tr
                  key={app.id}
                  onClick={() => setSelectedAppId(app.id)}
                  className="hover:bg-white/[0.03] cursor-pointer transition-colors"
                >
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2.5 min-w-0">
                      <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 font-display text-[11px] font-bold text-orange-400 uppercase">
                        {app.name.slice(0, 2)}
                      </div>
                      <div className="min-w-0">
                        <div className="truncate font-semibold text-zinc-100">{app.name}</div>
                        {app.primaryDomain && (
                          <div className="truncate font-mono text-[10px] text-zinc-500">
                            {app.primaryDomain}
                          </div>
                        )}
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={app.status} />
                  </td>
                  <td className="px-4 py-3 font-mono text-[11px] text-zinc-400">
                    {app.services.filter((s) => s.state === 'running').length}/{app.services.length}{' '}
                    online
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-300">
                    {app.cpuPct}% · {app.memMB} MB
                  </td>
                  <td className="px-3 py-3 text-right">
                    <ActionMenu items={getAppActions(app)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );

  // Recent Deployments across all projects for the Overview Dashboard
  const recentDeploymentsFeed = [
    {
      id: 'dep-4920',
      appId: 'app-9f81c2a0',
      appName: 'commerce-core-api',
      branch: 'main',
      sha: '8c4f19a',
      message: 'perf(checkout): batch inventory reservation lock in redis',
      trigger: 'GitHub Push',
      duration: '28s',
      time: '4m ago',
      status: 'ready' as const,
    },
    {
      id: 'dep-4917',
      appId: 'app-6b12f0e9',
      appName: 'temporal-worker-queue',
      branch: 'main',
      sha: '19d04e2',
      message: 'chore(ffmpeg): bump worker concurrency limit to 8 threads',
      trigger: 'GitLab CI',
      duration: '19s',
      time: '12m ago',
      status: 'building' as const,
    },
    {
      id: 'dep-4915',
      appId: 'app-7d24e8b1',
      appName: 'studio-web-dashboard',
      branch: 'production',
      sha: 'f42a90c',
      message: 'feat(billing): integrate instant SEPA & invoice PDF stream',
      trigger: 'GitHub Push',
      duration: '42s',
      time: '38m ago',
      status: 'ready' as const,
    },
    {
      id: 'dep-4912',
      appId: 'app-9f81c2a0',
      appName: 'commerce-core-api',
      branch: 'main',
      sha: '3b10e4c',
      message: 'feat(orders): add idempotency key header middleware',
      trigger: 'nd CLI',
      duration: '31s',
      time: '5h ago',
      status: 'ready' as const,
    },
    {
      id: 'dep-4908',
      appId: 'app-3c90a1f4',
      appName: 'plausible-analytics',
      branch: 'v2.1.4',
      sha: 'compose',
      message: 'ClickHouse 24.3-alpine + Plausible v2.1 rollout',
      trigger: 'Compose',
      duration: '16s',
      time: '2d ago',
      status: 'ready' as const,
    },
  ];

  return (
    <div className="min-h-screen bg-[#09090b] text-zinc-100 flex selection:bg-orange-500/30">
      <Toaster position="bottom-right" theme="dark" />

      {mobileMenuOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/70 backdrop-blur-xs lg:hidden"
          onClick={() => setMobileMenuOpen(false)}
        />
      )}

      {/* Dokploy / shadcn Inset Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-50 flex w-60 flex-col bg-[#09090b] transition-transform lg:static lg:translate-x-0 ${
          mobileMenuOpen ? 'translate-x-0 border-r border-white/[0.08]' : '-translate-x-full'
        }`}
      >
        <div className="flex h-14 items-center justify-between px-3.5 pt-2">
          <button
            type="button"
            onClick={() => {
              setSelectedAppId(null);
              setActiveNav('overview');
            }}
            className="flex flex-1 items-center gap-2.5 rounded-xl border border-white/[0.07] bg-[#111115] px-2.5 py-1.5 text-left hover:border-white/[0.14] transition-all cursor-pointer"
          >
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gradient-to-br from-orange-500 to-orange-600 font-display text-[11px] font-bold text-white shadow-sm">
              ND
            </div>
            <div className="min-w-0 flex-1 leading-tight">
              <div className="flex items-center gap-1.5">
                <span className="truncate font-display text-xs font-semibold text-zinc-100">
                  NextDeploy
                </span>
                <span className="rounded bg-white/[0.06] px-1 py-0.2 font-mono text-[9px] text-zinc-400">
                  v2.4
                </span>
              </div>
            </div>
          </button>
          <button
            type="button"
            onClick={() => setMobileMenuOpen(false)}
            className="ml-2 lg:hidden text-zinc-400 hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Sidebar Search Trigger */}
        <div className="px-3.5 pt-2.5 pb-1">
          <button
            type="button"
            onClick={() => setCmdPaletteOpen(true)}
            className="flex w-full items-center justify-between rounded-lg border border-white/[0.07] bg-[#111115]/70 px-2.5 py-1.5 text-xs text-zinc-400 hover:border-white/[0.14] hover:text-zinc-200 transition-colors cursor-pointer"
          >
            <span className="flex items-center gap-2">
              <Search className="h-3.5 w-3.5 text-zinc-500" />
              <span>Quick Jump...</span>
            </span>
            <kbd className="rounded border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] text-zinc-500">
              ⌘K
            </kbd>
          </button>
        </div>

        {/* Navigation Links */}
        <nav className="flex-1 overflow-y-auto no-scrollbar px-3 py-2 space-y-4">
          {navGroups.map((grp) => (
            <div key={grp.group} className="space-y-0.5">
              <p className="mb-1 px-2.5 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                {grp.group}
              </p>
              {grp.items.map((item) => {
                const isActive =
                  (!selectedAppId && activeNav === item.id) ||
                  (selectedAppId && item.id === 'apps');
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => {
                      setSelectedAppId(null);
                      setActiveNav(item.id);
                      setMobileMenuOpen(false);
                    }}
                    className={`relative flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-xs font-medium transition-all cursor-pointer whitespace-nowrap ${
                      isActive
                        ? 'bg-white/[0.07] text-zinc-100 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.06)]'
                        : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                    }`}
                  >
                    {isActive && (
                      <span className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-r bg-orange-500" />
                    )}
                    <span className={isActive ? 'text-orange-400 shrink-0' : 'text-zinc-500 shrink-0'}>
                      {item.icon}
                    </span>
                    <span>{item.label}</span>
                    {item.badge && (
                      <span
                        className={`ml-auto rounded-md px-1.5 py-0.5 font-mono text-[10px] font-medium ${
                          item.badge.tone === 'emerald'
                            ? 'bg-emerald-500/12 text-emerald-400 ring-1 ring-emerald-500/25'
                            : item.badge.tone === 'violet'
                              ? 'bg-violet-500/12 text-violet-300 ring-1 ring-violet-500/25'
                              : 'bg-white/[0.06] text-zinc-400'
                        }`}
                      >
                        {item.badge.text}
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </nav>

        {/* User Footer */}
        <div className="p-3">
          <div className="flex items-center justify-between rounded-xl border border-white/[0.07] bg-[#111115] px-3 py-2">
            <div className="flex items-center gap-2 min-w-0">
              <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-orange-500/15 font-mono text-[10px] font-bold text-orange-400 uppercase ring-1 ring-orange-500/30">
                MA
              </div>
              <div className="min-w-0">
                <span className="block truncate text-xs font-medium text-zinc-200">masud</span>
              </div>
            </div>
            <span className="rounded-md bg-white/[0.06] px-1.5 py-0.5 font-mono text-[10px] font-medium text-zinc-400">
              root
            </span>
          </div>
        </div>
      </aside>

      {/* shadcn Sidebar-Inset Floating Main Canvas */}
      <div className="flex min-w-0 flex-1 flex-col lg:py-2 lg:pr-2">
        <div className="relative flex min-h-full flex-1 flex-col overflow-hidden lg:rounded-2xl lg:border lg:border-white/[0.07] bg-[#0c0c0f] shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_24px_60px_rgba(0,0,0,0.65)]">
          {/* Subtle Top Ambient Ember Glow */}
          <div className="pointer-events-none absolute inset-x-0 top-0 h-44 bg-[radial-gradient(ellipse_60%_50%_at_50%_-15%,rgba(249,115,22,0.07),transparent)]" />

          {/* Top Header Bar */}
          <header className="sticky top-0 z-30 flex h-13 items-center justify-between gap-4 border-b border-white/[0.06] bg-[#0c0c0f]/85 px-6 backdrop-blur-xl">
            <div className="flex items-center gap-2 text-xs font-medium text-zinc-400 min-w-0">
              <button
                type="button"
                onClick={() => setMobileMenuOpen(true)}
                className="lg:hidden mr-1 text-zinc-400 hover:text-white"
              >
                <Menu className="h-4 w-4" />
              </button>
              <button
                type="button"
                onClick={() => {
                  setSelectedAppId(null);
                  setActiveNav('overview');
                }}
                className="hover:text-zinc-100 transition-colors cursor-pointer whitespace-nowrap"
              >
                NextDeploy
              </button>
              <ChevronRight className="h-3.5 w-3.5 text-zinc-600 shrink-0" />
              {selectedApp ? (
                <>
                  <button
                    type="button"
                    onClick={() => {
                      setSelectedAppId(null);
                      setActiveNav('apps');
                    }}
                    className="hover:text-zinc-100 transition-colors cursor-pointer whitespace-nowrap"
                  >
                    Projects
                  </button>
                  <ChevronRight className="h-3.5 w-3.5 text-zinc-600 shrink-0" />
                  <span className="font-mono font-semibold text-zinc-100 truncate">
                    {selectedApp.name}
                  </span>
                </>
              ) : (
                <span className="font-semibold text-zinc-100 truncate">{currentNavLabel}</span>
              )}
            </div>

            <div className="flex items-center gap-2 shrink-0">
              <div className="hidden md:flex items-center gap-1.5 rounded-lg border border-white/[0.07] bg-[#111115] px-2.5 py-1 font-mono text-[11px] text-zinc-400">
                <Server className="h-3 w-3 text-emerald-400" />
                <span>Docker 27.3</span>
                <span className="text-zinc-600">·</span>
                <span className="text-emerald-400">Caddy TLS</span>
              </div>

              <button
                type="button"
                onClick={() => setNewAppModalOpen(true)}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-all cursor-pointer whitespace-nowrap"
              >
                <Plus className="h-3.5 w-3.5" />
                Create Project
              </button>
            </div>
          </header>

          {/* Main Viewport */}
          <main className="relative mx-auto w-full max-w-6xl flex-1 px-6 py-6">
            {selectedApp ? (
              <AppWorkspace
                app={selectedApp}
                onBack={() => setSelectedAppId(null)}
                onUpdateApp={(updated) =>
                  setApps((prev) => prev.map((a) => (a.id === updated.id ? updated : a)))
                }
                onDeleteApp={(id) => {
                  void backendApi.deleteApp(id);
                  setApps((prev) => prev.filter((a) => a.id !== id));
                  setSelectedAppId(null);
                  toast.success('Deleted project');
                }}
              />
            ) : activeNav === 'overview' ? (
              <div className="space-y-5">
                {/* Row 1: 4 Host Telemetry Cards */}
                <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
                  {/* Memory */}
                  <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
                    <div className="flex items-start justify-between">
                      <div>
                        <p className="text-[11px] font-medium text-zinc-400">Host Memory</p>
                        <p className="mt-1.5 font-mono text-xl font-bold tabular-nums text-zinc-100 leading-none">
                          15.4 <span className="text-xs font-normal text-zinc-500">/ 32 GB</span>
                        </p>
                      </div>
                      <span className="rounded-md bg-sky-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-sky-400 ring-1 ring-sky-500/20">
                        48%
                      </span>
                    </div>
                    <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
                      <div
                        className="h-full bg-gradient-to-r from-sky-500 to-sky-400 rounded-full"
                        style={{ width: '48%' }}
                      />
                    </div>
                  </div>

                  {/* Disk */}
                  <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
                    <div className="flex items-start justify-between">
                      <div>
                        <p className="text-[11px] font-medium text-zinc-400">NVMe Storage (/)</p>
                        <p className="mt-1.5 font-mono text-xl font-bold tabular-nums text-zinc-100 leading-none">
                          84.2 <span className="text-xs font-normal text-zinc-500">/ 320 GB</span>
                        </p>
                      </div>
                      <span className="rounded-md bg-orange-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-orange-400 ring-1 ring-orange-500/20">
                        27%
                      </span>
                    </div>
                    <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
                      <div
                        className="h-full bg-gradient-to-r from-orange-500 to-amber-400 rounded-full"
                        style={{ width: '27%' }}
                      />
                    </div>
                  </div>

                  {/* CPU */}
                  <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
                    <div className="flex items-start justify-between">
                      <div>
                        <p className="text-[11px] font-medium text-zinc-400">CPU Load (16 vCPU)</p>
                        <p className="mt-1.5 font-mono text-xl font-bold tabular-nums text-zinc-100 leading-none">
                          {telemetry[telemetry.length - 1]?.cpu || 23}
                          <span className="text-xs font-normal text-zinc-500">%</span>
                        </p>
                      </div>
                      <span className="rounded-md bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-emerald-400 ring-1 ring-emerald-500/20">
                        Healthy
                      </span>
                    </div>
                    <div className="mt-3.5 h-1.5 w-full rounded-full bg-zinc-900 overflow-hidden">
                      <div
                        className="h-full bg-gradient-to-r from-emerald-500 to-emerald-400 rounded-full"
                        style={{ width: `${telemetry[telemetry.length - 1]?.cpu || 23}%` }}
                      />
                    </div>
                  </div>

                  {/* Runtime */}
                  <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between">
                    <div className="flex items-start justify-between">
                      <div>
                        <p className="text-[11px] font-medium text-zinc-400">Containers & TLS</p>
                        <p className="mt-1.5 font-mono text-xl font-bold tabular-nums text-zinc-100 leading-none">
                          7 <span className="text-xs font-normal text-zinc-500">running</span>
                        </p>
                      </div>
                      <StatusBadge status="active" label="TLS 1.3" />
                    </div>
                    <div className="mt-3 flex items-center justify-between font-mono text-[11px] text-zinc-500">
                      <span>4 projects</span>
                      <span>4 SSL routes</span>
                    </div>
                  </div>
                </div>

                {/* Row 2: Telemetry Area Chart Card */}
                <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
                  <div className="flex flex-wrap items-center justify-between gap-2 mb-3">
                    <div className="flex items-center gap-2">
                      <h2 className="text-xs font-semibold text-zinc-200">Host Throughput</h2>
                    </div>
                    <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
                      {(
                        [
                          { id: 'requests', label: 'Ingress req/m' },
                          { id: 'cpu-mem', label: 'CPU & RAM' },
                          { id: 'network', label: 'Network I/O' },
                        ] as const
                      ).map((tab) => (
                        <button
                          key={tab.id}
                          type="button"
                          onClick={() => setChartMetric(tab.id)}
                          className={`rounded-md px-2.5 py-1 text-xs font-medium transition-all cursor-pointer ${
                            chartMetric === tab.id
                              ? 'bg-zinc-800/90 text-zinc-100 shadow-2xs'
                              : 'text-zinc-400 hover:text-zinc-200'
                          }`}
                        >
                          {tab.label}
                        </button>
                      ))}
                    </div>
                  </div>

                  {chartMetric === 'requests' && (
                    <EvilAreaChart
                      data={telemetry}
                      dataKey="requests"
                      color="#f97316"
                      unit=" req/m"
                      height={175}
                      label="Requests"
                    />
                  )}
                  {chartMetric === 'cpu-mem' && (
                    <EvilAreaChart
                      data={telemetry}
                      dataKey="cpu"
                      secondaryKey="memory"
                      color="#10b981"
                      secondaryColor="#38bdf8"
                      unit="%"
                      height={175}
                      label="CPU"
                      secondaryLabel="RAM"
                    />
                  )}
                  {chartMetric === 'network' && (
                    <EvilAreaChart
                      data={telemetry}
                      dataKey="netInMbps"
                      secondaryKey="netOutMbps"
                      color="#f97316"
                      secondaryColor="#eab308"
                      unit=" Mbps"
                      height={175}
                      label="Inbound"
                      secondaryLabel="Outbound"
                    />
                  )}
                </div>

                {/* Row 3: Recent Deployments & Control Plane Health Bento (Replaces Projects 4) */}
                <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
                  {/* Left 2 Columns: Recent Deployments Feed */}
                  <div className="lg:col-span-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden flex flex-col justify-between">
                    <div>
                      <div className="flex items-center justify-between border-b border-white/[0.06] px-4 py-3">
                        <div className="flex items-center gap-2">
                          <GitBranch className="h-3.5 w-3.5 text-orange-400" />
                          <h2 className="text-xs font-semibold text-zinc-100">
                            Recent Deployments
                          </h2>
                        </div>
                        <button
                          type="button"
                          onClick={() => setActiveNav('apps')}
                          className="text-[11px] font-medium text-zinc-400 hover:text-zinc-100 cursor-pointer"
                        >
                          All Projects →
                        </button>
                      </div>

                      <div className="divide-y divide-white/[0.05]">
                        {recentDeploymentsFeed.map((dep) => (
                          <div
                            key={dep.id}
                            onClick={() => setSelectedAppId(dep.appId)}
                            className="flex items-center justify-between gap-3 px-4 py-2.5 hover:bg-white/[0.03] transition-colors cursor-pointer text-xs"
                          >
                            <div className="flex items-center gap-3 min-w-0">
                              <StatusBadge status={dep.status} />
                              <div className="min-w-0">
                                <div className="flex items-center gap-2">
                                  <span className="font-mono font-semibold text-zinc-100">
                                    {dep.appName}
                                  </span>
                                  <span className="rounded bg-white/[0.05] px-1.5 py-0.2 font-mono text-[10px] text-orange-400">
                                    {dep.branch}@{dep.sha}
                                  </span>
                                </div>
                                <div className="truncate text-[11px] text-zinc-500 mt-0.5">
                                  {dep.message}
                                </div>
                              </div>
                            </div>

                            <div className="flex items-center gap-3 shrink-0 font-mono text-[11px] text-zinc-400">
                              <span className="hidden sm:inline rounded border border-white/[0.06] bg-[#09090b] px-1.5 py-0.5 text-[10px] text-zinc-400">
                                {dep.trigger}
                              </span>
                              <span>{dep.duration}</span>
                              <span className="text-zinc-500 w-12 text-right">{dep.time}</span>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  </div>

                  {/* Right Column: System Daemons */}
                  <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden flex flex-col justify-between">
                    <div>
                      <div className="flex items-center justify-between border-b border-white/[0.06] px-4 py-3">
                        <div className="flex items-center gap-2">
                          <Server className="h-3.5 w-3.5 text-emerald-400" />
                          <span className="text-xs font-semibold text-zinc-100">System Daemons</span>
                        </div>
                        <span className="font-mono text-[10px] text-emerald-400">5/5 Online</span>
                      </div>

                      <div className="divide-y divide-white/[0.05] font-mono text-xs">
                        {[
                          { name: 'dockerd', ver: 'v27.3.1', meta: '7 containers', status: 'running' as const },
                          { name: 'caddy-proxy', ver: 'v2.8.4', meta: 'TLS 1.3 · HTTP/3', status: 'active' as const },
                          { name: 'nextdeployd', ver: 'v2.4.0', meta: '24 MB RAM', status: 'running' as const },
                          { name: 'mcp-server', ver: 'SSE/JSON-RPC', meta: '16 tools', status: 'active' as const },
                          { name: 'rclone-cron', ver: '0 */6 * * *', meta: 'R2 + S3', status: 'connected' as const },
                        ].map((d) => (
                          <div
                            key={d.name}
                            className="flex items-center justify-between px-4 py-2.5 hover:bg-white/[0.02] transition-colors"
                          >
                            <div className="min-w-0">
                              <div className="flex items-center gap-2">
                                <span className="text-zinc-100 font-semibold">{d.name}</span>
                                <span className="text-[10px] text-zinc-500">{d.ver}</span>
                              </div>
                              <div className="text-[10px] text-zinc-500 mt-0.5">{d.meta}</div>
                            </div>
                            <span className="inline-flex items-center gap-1.5 rounded-md bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-400 ring-1 ring-emerald-500/20">
                              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                              ok
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>

                    <div className="flex items-center justify-between border-t border-white/[0.06] bg-[#09090b]/50 px-4 py-2.5 font-mono text-[10px] text-zinc-500">
                      <span>Ubuntu 24.04 LTS</span>
                      <span className="text-zinc-400">Uptime 42d 18h</span>
                    </div>
                  </div>
                </div>
              </div>
            ) : activeNav === 'apps' ? (
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2.5">
                    <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
                      <Layers className="h-4 w-4 text-orange-400" />
                    </div>
                    <h1 className="text-base font-semibold tracking-tight text-zinc-100">
                      Projects
                    </h1>
                  </div>
                </div>
                {renderAppsCollection()}
              </div>
            ) : activeNav === 'monitor' ? (
              <MonitorView
                telemetry={telemetry}
                apps={apps}
                onStreamTick={appendTelemetryTick}
              />
            ) : activeNav === 'containers' ? (
              <ContainersView
                apps={apps}
                onOpenAppTab={(id) => setSelectedAppId(id)}
                onUpdateApp={(updated) =>
                  setApps((prev) => prev.map((a) => (a.id === updated.id ? updated : a)))
                }
              />
            ) : activeNav === 'images' ? (
              <ImagesView
                images={images}
                onDeleteImage={(id) => {
                  setImages((prev) => prev.filter((i) => i.id !== id));
                  toast.success(`Removed ${id}`);
                }}
                onPruneUnused={() => {
                  setImages((prev) => prev.filter((i) => i.inUseBy.length > 0));
                  toast.success('Deleted unused images');
                }}
              />
            ) : activeNav === 'volumes' ? (
              <VolumesView volumes={INITIAL_VOLUMES} />
            ) : activeNav === 'terminal' ? (
              <VpsTerminalView />
            ) : activeNav === 'git' ? (
              <GitAndRegistriesView
                mode="git"
                gitProviders={INITIAL_GIT_PROVIDERS}
                syncedRepos={INITIAL_SYNCED_REPOS}
                registries={registries}
                onAddRegistry={(r) => setRegistries((prev) => [...prev, r])}
              />
            ) : activeNav === 'registries' ? (
              <GitAndRegistriesView
                mode="registries"
                gitProviders={INITIAL_GIT_PROVIDERS}
                syncedRepos={INITIAL_SYNCED_REPOS}
                registries={registries}
                onAddRegistry={(r) => setRegistries((prev) => [...prev, r])}
                onDeleteRegistry={(id) =>
                  setRegistries((prev) => prev.filter((item) => item.id !== id))
                }
              />
            ) : activeNav === 'proxy' ? (
              <CaddyProxyView apps={apps} />
            ) : activeNav === 'backup' ? (
              <BackupsView
                destinations={INITIAL_BACKUP_DESTINATIONS}
                archives={backupArchives}
                onTriggerBackup={() => {
                  const nextArchive: BackupArchiveRecord = {
                    id: `bk-${Date.now()}`,
                    appSlug: 'commerce-core-api',
                    destinationName: 'Cloudflare R2 Primary (EU-Central)',
                    archiveName: `commerce-core-api_${new Date()
                      .toISOString()
                      .slice(0, 10)}.nd-backup.tar.gz`,
                    sizeMB: 1424.0,
                    createdAt: 'Just now',
                    status: 'completed',
                    includesVolumes: true,
                    includesEnv: true,
                  };
                  setBackupArchives((prev) => [nextArchive, ...prev]);
                  toast.success('Backup created');
                }}
              />
            ) : activeNav === 'migrate' ? (
              <MigrationView
                apps={apps}
                bundles={migrationBundles}
                onExportBundle={(slugs) => {
                  const nextBundle: MigrationExportBundle = {
                    id: `mig-${Math.floor(205 + Math.random() * 90)}`,
                    createdAt: 'Just now',
                    selectedApps: slugs,
                    estimatedSizeMB: 4800,
                    actualSizeMB: 2110,
                    status: 'ready',
                    token: `ndm_live_${Math.random().toString(36).slice(2, 16)}`,
                    expiresIn: '3h',
                    progressLog: [
                      `[18:25:01] Quiescing ${slugs.join(', ')}...`,
                      `[18:25:04] Compressed volumes...`,
                      `[18:25:09] Bundle ready.`,
                    ],
                  };
                  setMigrationBundles((prev) => [nextBundle, ...prev]);
                  toast.success(`Export ${nextBundle.id} ready`);
                }}
              />
            ) : activeNav === 'mcp' ? (
              <McpAndCliView
                mode="mcp"
                tools={MCP_TOOLS_CATALOG}
                cliSessions={cliSessions}
                onGenerateCliToken={() => {}}
              />
            ) : activeNav === 'cli-sessions' ? (
              <McpAndCliView
                mode="cli"
                tools={MCP_TOOLS_CATALOG}
                cliSessions={cliSessions}
                onGenerateCliToken={(label) => {
                  void backendApi.createCliToken(label);
                  const created: CliSessionItem = {
                    id: `cli-${Date.now()}`,
                    tokenName: label,
                    prefix: `nd_cli_${Math.random().toString(16).slice(2, 6)}...`,
                    deviceHost: 'workstation',
                    osArch: 'darwin/arm64',
                    cliVersion: 'nd v1.8.4',
                    lastCommand: 'nd login',
                    lastActiveAt: 'Just now',
                    ipAddress: '103.148.22.91',
                  };
                  setCliSessions((prev) => [created, ...prev]);
                  toast.success(`Generated token for ${label}`);
                }}
                onRevokeCliSession={(id) => {
                  void backendApi.deleteCliSession(id);
                  setCliSessions((prev) => prev.filter((s) => s.id !== id));
                  toast.success('Revoked CLI session');
                }}
              />
            ) : activeNav === 'users' ? (
              <UsersAndSettingsView
                mode="users"
                users={INITIAL_USERS}
                auditLogs={INITIAL_AUDIT_LOGS}
              />
            ) : activeNav === 'audit-logs' ? (
              <UsersAndSettingsView
                mode="audit-logs"
                users={INITIAL_USERS}
                auditLogs={INITIAL_AUDIT_LOGS}
              />
            ) : (
              <UsersAndSettingsView
                mode="settings"
                users={INITIAL_USERS}
                auditLogs={INITIAL_AUDIT_LOGS}
              />
            )}
          </main>
        </div>
      </div>

      {/* Modals */}
      <CommandPaletteModal
        open={cmdPaletteOpen}
        onClose={() => setCmdPaletteOpen(false)}
        apps={apps}
        onSelectNav={(nav) => {
          setSelectedAppId(null);
          setActiveNav(nav);
        }}
        onSelectApp={(id) => setSelectedAppId(id)}
        onOpenNewApp={() => setNewAppModalOpen(true)}
      />

      <DeployNewAppModal
        open={newAppModalOpen}
        onClose={() => setNewAppModalOpen(false)}
        onCreateApp={handleCreateApp}
      />
    </div>
  );
}
