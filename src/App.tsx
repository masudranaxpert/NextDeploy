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
      const data = res.data;
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
                apps={apps}
                destinations={INITIAL_BACKUP_DESTINATIONS}
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
