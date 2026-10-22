import React, { useState } from 'react';
import {
  Activity,
  Archive,
  ArrowLeftRight,
  Box,
  ChevronRight,
  FileText,
  FolderGit2,
  Globe,
  HardDrive,
  KeyRound,
  Layers,
  LayoutGrid,
  Menu,
  Plus,
  Search,
  Server,
  Settings,
  Sparkles,
  Terminal,
  Users,
  X,
} from 'lucide-react';
import { Toaster, toast } from 'sonner';
import {
  AppStack,
  BackupArchiveRecord,
  BackupDestination,
  CliSessionItem,
  DockerImageItem,
  DockerVolumeItem,
  GitProviderConnection,
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
import { CommandPaletteModal, DeployNewAppModal } from './components/ui/Primitives';
import { OverviewDashboard } from './features/overview/OverviewDashboard';
import { ProjectsListView } from './features/projects/ProjectsListView';
import { AppWorkspace } from './features/workspace/AppWorkspace';
import { ContainersView } from './features/docker/ContainersView';
import { ImagesView } from './features/docker/ImagesView';
import { VolumesView } from './features/docker/VolumesView';
import { VpsTerminalView } from './features/terminal/VpsTerminalView';
import { MonitorView } from './features/monitoring/MonitorView';
import { GitAndRegistriesView } from './features/integrations/GitAndRegistriesView';
import { CaddyProxyView } from './features/proxy/CaddyProxyView';
import { BackupsView } from './features/backups/BackupsView';
import { MigrationView } from './features/migration/MigrationView';
import { McpAndCliView } from './features/automation/McpAndCliView';
import { UsersAndSettingsView } from './features/system/UsersAndSettingsView';
import { AuthView } from './features/auth/AuthView';
import { backendApi } from './lib/backendApi';

export default function App() {
  const [activeNav, setActiveNav] = useState<NavigationSection>('overview');
  const [selectedAppId, setSelectedAppId] = useState<string | null>(null);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [authMode, setAuthMode] = useState<'setup' | 'login' | null>(null);

  // Core State
  const [apps, setApps] = useState<AppStack[]>(INITIAL_APPS);
  const [telemetry, setTelemetry] = useState<TelemetryPoint[]>(INITIAL_TELEMETRY);
  const [images, setImages] = useState<DockerImageItem[]>(INITIAL_IMAGES);
  const [volumes, setVolumes] = useState<DockerVolumeItem[]>(INITIAL_VOLUMES);
  const [gitProviders, setGitProviders] = useState<GitProviderConnection[]>(INITIAL_GIT_PROVIDERS);
  const [destinations, setDestinations] = useState<BackupDestination[]>(INITIAL_BACKUP_DESTINATIONS);
  const [registries, setRegistries] = useState<RegistryCredential[]>(INITIAL_REGISTRIES);
  const [backupArchives, setBackupArchives] =
    useState<BackupArchiveRecord[]>(INITIAL_BACKUP_ARCHIVES);
  const [migrationBundles, setMigrationBundles] =
    useState<MigrationExportBundle[]>(INITIAL_MIGRATION_BUNDLES);
  const [cliSessions, setCliSessions] = useState<CliSessionItem[]>(INITIAL_CLI_SESSIONS);

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
    void backendApi.bootstrap().then((res) => {
      if (res.status === 401 || !res.ok) {
        if (res.data?.needs_setup || window.location.pathname === '/setup') {
          setAuthMode('setup');
        } else {
          setAuthMode('login');
        }
        return;
      }
      setAuthMode(null);
      const data = res.data;
      if (!data) return;

      if (Array.isArray(data.apps) && data.apps.length > 0) {
        const mappedApps: AppStack[] = data.apps.map((raw) => {
          const primaryDomain = raw.domains && raw.domains.length > 0 ? raw.domains[0].domain : undefined;
          const servicesList = (raw.services || ['app']).map((svcName, sIdx) => ({
            id: `c-${raw.id}-${sIdx}`,
            name: `${raw.name}-${svcName}`,
            service: svcName,
            image: `${raw.name}:${svcName}`,
            state: (raw.status === 'stopped' ? 'exited' : 'running') as 'running' | 'exited' | 'restarting',
            uptime: raw.status === 'stopped' ? 'Exited (0)' : 'Up active',
            cpuPct: raw.status === 'stopped' ? 0 : 2.5,
            memUsedMB: raw.status === 'stopped' ? 0 : 128,
            memLimitMB: 512,
            ports: '3000/tcp',
          }));
          const mappedDeployments = (raw.deploy_logs || []).map((dl) => ({
            id: String(dl.id),
            branch: raw.git_config?.branch || raw.git_branch || 'main',
            commitSha: raw.git_config?.last_commit_sha || 'main',
            commitMessage: dl.action || 'Deploy stack',
            trigger: (raw.source_type === 'git' ? 'git-webhook' : 'manual-compose') as
              | 'git-webhook'
              | 'nd-cli'
              | 'mcp-agent'
              | 'manual-compose',
            status: dl.ok ? ('ready' as const) : ('failed' as const),
            startedAt: dl.created_at || 'Recently',
            durationSec: 14,
            author: 'masud',
            buildSteps: [
              {
                name: 'Deploy Stack',
                status: dl.ok ? ('done' as const) : ('error' as const),
                durationMs: 14000,
              },
            ],
          }));
          const mappedDomains = (raw.domains || []).map((dom) => ({
            id: String(dom.id),
            domain: dom.domain,
            targetService: dom.service,
            targetPort: dom.port,
            tlsProvider: 'LetsEncrypt' as const,
            tlsStatus: dom.enable_https ? ('active' as const) : ('provisioning' as const),
            forceHttps: dom.enable_https ?? true,
            gzip: true,
            sslStatus: dom.enable_https ? ('active' as const) : ('inactive' as const),
            primary: dom.domain === primaryDomain,
            createdAt: 'Active',
          }));
          const mappedVolumes = (raw.volumes || []).map((vName) => ({
            name: vName,
            mountPath: `/data/${vName}`,
            driver: 'local',
            sizeMB: 32,
          }));
          const mappedCollabs = (raw.collaborators || []).map((c) => ({
            userId: String(c.user_id),
            username: `user_${c.user_id}`,
            email: `user_${c.user_id}@nextdeploy.local`,
            role: (c.role === 'developer' ? 'deployer' : c.role === 'viewer' ? 'viewer' : 'owner') as 'owner' | 'deployer' | 'viewer',
            addedAt: c.created_at || 'Recently',
          }));
          const envVars = (raw.env_content || '')
            .split('\n')
            .filter((line) => line.trim() && !line.startsWith('#') && line.includes('='))
            .map((line, idx) => {
              const eqIdx = line.indexOf('=');
              const key = line.slice(0, eqIdx).trim();
              const lowerKey = key.toLowerCase();
              return {
                id: `env-${idx}`,
                key,
                value: line.slice(eqIdx + 1).trim(),
                isSecret:
                  lowerKey.includes('secret') ||
                  lowerKey.includes('key') ||
                  lowerKey.includes('pass') ||
                  lowerKey.includes('token'),
              };
            });

          return {
            id: raw.id,
            name: raw.name,
            slug: raw.id,
            description: `Production stack for ${raw.name}`,
            sourceType: (raw.source_type === 'git' ? 'git' : raw.source_type === 'upload' ? 'zip' : 'compose') as 'git' | 'compose' | 'zip',
            gitRepo: raw.git_config?.repo_url || raw.git_repo,
            gitBranch: raw.git_config?.branch || raw.git_branch || 'main',
            gitCommitSha: raw.git_config?.last_commit_sha || 'main',
            gitCommitMessage: raw.git_config?.last_commit_msg || 'Production release',
            webhookAutoDeploy: Boolean(raw.git_config?.auto_deploy),
            gitSync: Boolean(raw.git_config?.git_sync),
            status: (raw.status === 'stopped' ? 'stopped' : 'running') as 'running' | 'building' | 'stopped',
            primaryDomain,
            composeFilePath: 'docker-compose.yml',
            owner: 'admin',
            updatedAt: raw.updated_at || 'Recently',
            cpuPct: 2.5,
            memMB: 256,
            diskMB: 512,
            reqPerMin: 120,
            services: servicesList,
            domains: mappedDomains,
            volumes: mappedVolumes,
            deployments: mappedDeployments,
            collaborators: mappedCollabs,
            envVars:
              envVars.length > 0
                ? envVars
                : [
                    { id: 'env-1', key: 'PORT', value: '3000', isSecret: false },
                    { id: 'env-2', key: 'NODE_ENV', value: 'production', isSecret: false },
                  ],
            logs: [],
            files: [],
          };
        });
        setApps(mappedApps);
      }

      if (Array.isArray(data.images) && data.images.length > 0) {
        setImages(
          data.images.map((img) => {
            const tag = (img.repo_tags && img.repo_tags[0]) || img.id.slice(0, 12);
            const parts = tag.split(':');
            return {
              id: img.id.slice(0, 12),
              repository: parts[0] || 'app',
              tag: parts[1] || 'latest',
              sizeMB: Math.round((img.size || 0) / (1024 * 1024)),
              created: img.created ? new Date(img.created * 1000).toISOString().split('T')[0] : 'Recently',
              inUseBy: [],
            };
          })
        );
      }

      if (Array.isArray(data.volumes) && data.volumes.length > 0) {
        setVolumes(
          data.volumes.map((vName) => ({
            name: vName,
            driver: 'local',
            mountPoint: `/var/lib/docker/volumes/${vName}/_data`,
            appSlug: vName.split('_')[0] || 'system',
            sizeMB: 16.4,
            createdAt: 'Active',
            filesCount: 2,
            sampleFiles: [],
          }))
        );
      }

      if (Array.isArray(data.git_providers) && data.git_providers.length > 0) {
        setGitProviders(
          data.git_providers.map((gp) => {
            const isGitLab = (gp.provider || '').toLowerCase().includes('gitlab');
            return {
              id: String(gp.id),
              provider: isGitLab ? ('GitLab' as const) : ('GitHub' as const),
              authMethod: isGitLab ? ('OAuth 2.0' as const) : ('GitHub App' as const),
              accountName: gp.name || 'Provider',
              instanceUrl: isGitLab ? 'https://gitlab.com' : 'https://github.com',
              status: 'connected' as const,
              reposAccessible: 12,
              webhookDeliveryRate: '99.8%',
              webhookUrl: '/webhooks/github/provider',
              lastSynced: 'Just now',
            };
          })
        );
      }

      if (Array.isArray(data.backup_destinations) && data.backup_destinations.length > 0) {
        setDestinations(
          data.backup_destinations.map((bd) => {
            const isGDrive = (bd.provider || '').toLowerCase() === 'gdrive';
            return {
              id: String(bd.id),
              name: bd.name || (isGDrive ? 'Google Drive' : 'Cloudflare R2'),
              provider: isGDrive ? ('Google Drive' as const) : ('Cloudflare R2' as const),
              providerCode: isGDrive ? ('gdrive' as const) : ('r2' as const),
              bucket: bd.bucket || '',
              endpoint: '',
              region: 'auto',
              status: 'connected' as const,
              scheduleCron: '0 2 * * *',
              retentionDays: 14,
              usedGB: 0,
              quotaGB: 100,
              lastBackupAt: 'Connected',
            };
          })
        );
      }

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

    // Real-time telemetry via /monitor/ws
    let ws: WebSocket | null = null;
    try {
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      ws = new WebSocket(`${proto}//${window.location.host}/monitor/ws`);
      ws.onmessage = (e) => {
        try {
          const d = JSON.parse(e.data);
          const now = new Date();
          const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(
            now.getMinutes()
          ).padStart(2, '0')}`;
          const cpuVal = typeof d.cpu_pct === 'number' ? Math.round(d.cpu_pct) : 22;
          const memVal = typeof d.mem_pct === 'number' ? Math.round(d.mem_pct) : 48;
          const diskVal = typeof d.disk_pct === 'number' ? Math.round(d.disk_pct) : 28;
          const rxVal = typeof d.net_rx_mbps === 'number' ? Math.round(d.net_rx_mbps) : 34;
          const txVal = typeof d.net_tx_mbps === 'number' ? Math.round(d.net_tx_mbps) : 26;

          setTelemetry((prev) => [
            ...prev.slice(1),
            {
              time: timeStr,
              cpu: cpuVal,
              memory: memVal,
              diskIo: diskVal,
              netInMbps: rxVal,
              netOutMbps: txVal,
              requests: 1800,
            },
          ]);
        } catch {
          // ignore parsing error
        }
      };
    } catch {
      // offline fallback
    }

    return () => {
      if (ws) ws.close();
    };
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
        { id: 'backup', label: 'Backup', icon: <Archive className="h-3.5 w-3.5" /> },
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
        { id: 'proxy', label: 'Caddy Proxy', icon: <Globe className="h-3.5 w-3.5" /> },
        { id: 'migrate', label: 'Migration', icon: <ArrowLeftRight className="h-3.5 w-3.5" /> },
        { id: 'users', label: 'Team & Quotas', icon: <Users className="h-3.5 w-3.5" /> },
        { id: 'audit-logs', label: 'Audit Trail', icon: <FileText className="h-3.5 w-3.5" /> },
        { id: 'settings', label: 'Settings', icon: <Settings className="h-3.5 w-3.5" /> },
      ],
    },
  ];

  const currentNavLabel =
    navGroups.flatMap((g) => g.items).find((i) => i.id === activeNav)?.label || 'Overview';

  if (authMode) {
    return (
      <>
        <Toaster position="bottom-right" theme="dark" />
        <AuthView
          mode={authMode}
          onSuccess={() => {
            setAuthMode(null);
            window.location.href = '/overview';
          }}
        />
      </>
    );
  }

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
          <header className="sticky top-0 z-30 flex h-13 items-center justify-between gap-4 border-b border-white/[0.06] bg-[#0c0c0f]/85 px-4 sm:px-6 backdrop-blur-xl">
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
                <span className="hidden sm:inline">Create Project</span>
                <span className="sm:hidden">New</span>
              </button>
            </div>
          </header>

          {/* Main Viewport */}
          <main className="relative mx-auto w-full max-w-6xl flex-1 px-4 sm:px-6 py-4 sm:py-6">
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
              <OverviewDashboard
                telemetry={telemetry}
                onSelectApp={(id) => setSelectedAppId(id)}
                onSelectNav={(nav) => setActiveNav(nav)}
              />
            ) : activeNav === 'apps' ? (
              <ProjectsListView
                apps={apps}
                onSelectApp={(id) => setSelectedAppId(id)}
                onUpdateApps={setApps}
              />
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
                onDeleteImage={async (id) => {
                  await backendApi.globalImageRemove(id);
                  setImages((prev) => prev.filter((i) => i.id !== id));
                  toast.success(`Removed image ${id}`);
                }}
                onPruneUnused={async () => {
                  await backendApi.globalImagesPrune();
                  setImages((prev) => prev.filter((i) => i.inUseBy.length > 0));
                  toast.success('Pruned unused images');
                }}
              />
            ) : activeNav === 'volumes' ? (
              <VolumesView
                volumes={volumes}
                onCreateVolume={(vol) => setVolumes((prev) => [vol, ...prev])}
                onDeleteVolume={async (name) => {
                  const res = await backendApi.globalVolumeRemove(name);
                  if (res.ok) {
                    setVolumes((prev) => prev.filter((v) => v.name !== name));
                    toast.success(`Removed volume ${name}`);
                  } else {
                    toast.error(res.error || `Failed to remove volume ${name}`);
                  }
                }}
              />
            ) : activeNav === 'terminal' ? (
              <VpsTerminalView />
            ) : activeNav === 'git' ? (
              <GitAndRegistriesView
                mode="git"
                gitProviders={gitProviders}
                syncedRepos={INITIAL_SYNCED_REPOS}
                registries={registries}
                onAddRegistry={(r) => setRegistries((prev) => [...prev, r])}
              />
            ) : activeNav === 'registries' ? (
              <GitAndRegistriesView
                mode="registries"
                gitProviders={gitProviders}
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
                apps={apps}
                destinations={destinations}
                archives={backupArchives}
                onSelectApp={(id) => setSelectedAppId(id)}
                onTriggerBackup={(created) => {
                  if (created) {
                    setBackupArchives((prev) => [created, ...prev]);
                  }
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
                    workspaceBytes: 38 * 1024 * 1024,
                    volumeBytes: 2072 * 1024 * 1024,
                    status: 'ready',
                    token: `ndm_live_${Math.random().toString(36).slice(2, 16)}`,
                    expiresIn: '3h',
                    downloadUrl: `https://panel.nextdeploy.sh/migrate/download/ndm_live_${Math.random().toString(36).slice(2, 14)}`,
                    migrateCmd: `curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- --url "https://panel.nextdeploy.sh/migrate/download/ndm_live_bundle"`,
                    progressLog: [
                      `[18:25:01] Quiescing ${slugs.join(', ')}...`,
                      `[18:25:04] Compressed volumes...`,
                      `[18:25:09] Bundle ready.`,
                    ],
                  };
                  setMigrationBundles((prev) => [nextBundle, ...prev]);
                  toast.success(`Export ${nextBundle.id} ready`);
                }}
                onDeleteBundle={(id) => {
                  setMigrationBundles((prev) => prev.filter((b) => b.id !== id));
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
