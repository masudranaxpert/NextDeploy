import React, { useEffect, useState } from 'react';
import {
  Activity,
  Archive,
  ArrowLeftRight,
  Box,
  ChevronRight,
  FileCode2,
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
  Loader2,
  LogOut,
} from 'lucide-react';
import { Toaster, toast } from 'sonner';
import {
  AppStack,
  AuditLogEntry,
  BackupArchiveRecord,
  BackupDestination,
  CliSessionItem,
  DockerImageItem,
  DockerVolumeItem,
  GitProviderConnection,
  MigrationExportBundle,
  NavigationSection,
  PanelUser,
  RegistryCredential,
  SyncedGitRepository,
  TelemetryPoint,
} from './types/nextdeploy';
import {
  MCP_TOOLS_CATALOG,
} from './data/mcpTools';
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
import { PhpSitesView } from './features/php/PhpSitesView';
import { PhpInstanceView } from './features/php/PhpInstanceView';
import { backendApi, formatWildcardDomain, getServerPublicIp, setServerPublicIp } from './lib/backendApi';
import { copyToClipboard } from './lib/clipboard';



function mapMigrateExports(raw: any[]): MigrationExportBundle[] {
  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  return raw.map((me: any) => ({
    id: String(me.id),
    createdAt: me.created_at || 'Recently',
    selectedApps: me.apps ? me.apps.split(',') : [],
    estimatedSizeMB: Math.round((me.size_bytes || 0) / (1024 * 1024)),
    actualSizeMB: Math.round((me.size_bytes || 0) / (1024 * 1024)),
    workspaceBytes: 0,
    volumeBytes: me.size_bytes || 0,
    status: me.consumed_at ? 'consumed' : me.status || 'ready',
    token: me.token || '',
    expiresIn: me.expires_at
      ? `${Math.max(0, Math.round((new Date(me.expires_at).getTime() - Date.now()) / (1000 * 60)))}m`
      : '3h',
    downloadUrl: `${origin}/migrate/download/${me.token}`,
    migrateCmd: `curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/migrate.sh | sudo bash -s -- --url "${origin}/migrate/download/${me.token}"`,
    progressLog: me.log ? me.log.split('\n') : [],
  })) as MigrationExportBundle[];
}

export default function App() {
  // Parse route from window.location.pathname on load
  const parseInitialRoute = (): { nav: NavigationSection; appId: string | null } => {
    if (typeof window === 'undefined') return { nav: 'overview', appId: null };
    const path = window.location.pathname.replace(/^\/+|\/+$/g, '');
    if (!path || path === 'overview') return { nav: 'overview', appId: null };

    // Matches /apps/:id
    const appMatch = path.match(/^apps\/([^/]+)$/);
    if (appMatch) {
      return { nav: 'apps', appId: appMatch[1] };
    }

    const validNavs: NavigationSection[] = [
      'overview',
      'monitor',
      'apps',
      'containers',
      'images',
      'volumes',
      'terminal',
      'git',
      'registries',
      'backup',
      'mcp',
      'cli-sessions',
      'migrate',
      'proxy',
      'settings',
      'audit-logs',
      'users',
      'php-sites',
      'php-instance',
    ];

    if (path === 'caddy') return { nav: 'proxy', appId: null };
    if (validNavs.includes(path as NavigationSection)) {
      return { nav: path as NavigationSection, appId: null };
    }
    return { nav: 'overview', appId: null };
  };

  const initialRoute = parseInitialRoute();
  const [activeNav, setActiveNav] = useState<NavigationSection>(initialRoute.nav);
  const [selectedAppId, setSelectedAppId] = useState<string | null>(initialRoute.appId);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [authMode, setAuthMode] = useState<'setup' | 'login' | null>(() => {
    if (typeof window !== 'undefined') {
      if (window.location.pathname === '/setup') return 'setup';
      if (window.location.pathname === '/login') return 'login';
    }
    return null;
  });
  const [isBootstrapping, setIsBootstrapping] = useState(() => {
    if (typeof window !== 'undefined') {
      if (window.location.pathname === '/setup' || window.location.pathname === '/login') {
        return false;
      }
    }
    return true;
  });

  // Sync URL with state navigation
  const navigateTo = (
    nav: NavigationSection,
    appId: string | null = null,
    replace = false,
    tab?: string
  ) => {
    setActiveNav(nav);
    setSelectedAppId(appId);
    let targetPath = `/${nav}`;
    if (appId) {
      targetPath = `/apps/${appId}${tab ? `?tab=${tab}` : ''}`;
    } else if (nav === 'overview') {
      targetPath = '/overview';
    }
    if (window.location.pathname + window.location.search !== targetPath) {
      if (replace) {
        window.history.replaceState({ nav, appId, tab }, '', targetPath);
      } else {
        window.history.pushState({ nav, appId, tab }, '', targetPath);
      }
    }
  };

  // Handle browser Back / Forward buttons
  React.useEffect(() => {
    const handlePopState = () => {
      const route = parseInitialRoute();
      setActiveNav(route.nav);
      setSelectedAppId(route.appId);
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  // Core State
  const [sys, setSys] = useState<any>(null);
  const [apps, setApps] = useState<AppStack[]>([]);
  const [telemetry, setTelemetry] = useState<TelemetryPoint[]>([]);
  const [images, setImages] = useState<DockerImageItem[]>([]);
  const [registries, setRegistries] = useState<RegistryCredential[]>([]);
  const [backupArchives, setBackupArchives] =
    useState<BackupArchiveRecord[]>([]);
  const [migrationBundles, setMigrationBundles] =
    useState<MigrationExportBundle[]>([]);
  const [cliSessions, setCliSessions] = useState<CliSessionItem[]>([]);
  const [volumes, setVolumes] = useState<DockerVolumeItem[]>([]);
  const [users, setUsers] = useState<PanelUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLogEntry[]>([]);
  const [gitProviders, setGitProviders] = useState<GitProviderConnection[]>([]);
  const [syncedRepos, setSyncedRepos] = useState<SyncedGitRepository[]>([]);
  const [backupDestinations, setBackupDestinations] = useState<BackupDestination[]>([]);
  const [currentUser, setCurrentUser] = useState<{ id: number; username: string; role: string } | null>(null);
  const [phpEnabled, setPhpEnabled] = useState(false);
  const [myQuota, setMyQuota] = useState<any>(null);
  React.useEffect(() => {
    const adminOnly = ['monitor', 'terminal', 'proxy', 'migrate', 'users', 'settings', 'cli-sessions', 'php-instance'];
    if (currentUser && currentUser.role !== 'admin' && !selectedAppId && adminOnly.includes(activeNav)) {
      setActiveNav('overview');
      window.history.replaceState({ nav: 'overview', appId: null }, '', '/overview');
    }
  }, [currentUser, activeNav, selectedAppId]);
  const [cgroupInfo, setCgroupInfo] = useState<{ mode?: string; supported?: boolean }>({ mode: 'systemd', supported: true });
  const [dockerVersion, setDockerVersion] = useState('');
  const [caddyInfo, setCaddyInfo] = useState<{ up: boolean; status: string; config?: Record<string, string> } | null>(null);

  const handleLogout = async () => {
    try {
      await backendApi.logout();
    } catch {
      // ignore
    }
    window.location.href = '/login';
  };

  const liveSyncedRepos: SyncedGitRepository[] = React.useMemo(() => {
    return apps
      .filter((a) => Boolean(a.gitRepo))
      .map((a) => {
        const rawRepo = (a.gitRepo || '')
          .replace(/^https?:\/\//, '')
          .replace(/^github\.com\//, '')
          .replace(/^gitlab\.com\//, '');
        const isGitLab = (a.gitRepo || '').includes('gitlab');
        return {
          id: `repo-${a.id}`,
          provider: isGitLab ? 'GitLab' : 'GitHub',
          fullName: rawRepo || a.name,
          defaultBranch: a.gitBranch || 'main',
          private: true,
          linkedAppSlug: a.slug || a.name,
          lastCommitSha: a.gitCommitSha ? a.gitCommitSha.slice(0, 7) : 'latest',
          lastPushAt: 'Active',
          autoDeploy: a.gitSync !== false,
        };
      });
  }, [apps]);

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

  const mapRawAppToStack = (a: any, liveContainers: any[] = [], liveImages: any[] = []): AppStack => {
    const idNorm = (a.id || '').toLowerCase();
    const idUnderscore = idNorm.replace(/-/g, '_');
    const nameNorm = (a.name || '').toLowerCase();

    const appContainers = liveContainers.filter((c: any) => {
      const cp = (c.ComposeProject || '').toLowerCase();
      return (
        cp === idNorm ||
        cp === idUnderscore ||
        cp === nameNorm ||
        cp.startsWith(`${idNorm}_`) ||
        cp.startsWith(`${idUnderscore}_`) ||
        cp.startsWith(`${nameNorm}_`) ||
        cp.startsWith(`${idNorm}-`) ||
        cp.startsWith(`${idUnderscore}-`) ||
        cp.startsWith(`${nameNorm}-`)
      );
    });

    const checkSlotAvailable = (slot: 'blue' | 'green'): boolean => {
      const prefixes = [`${idNorm}-${slot}`, `${nameNorm}-${slot}`, `${idUnderscore}-${slot}`];
      const hasContainer = appContainers.some((c: any) => {
        const cp = (c.ComposeProject || '').toLowerCase();
        const cname = (c.Name || '').toLowerCase();
        return prefixes.some((p) => cp === p || cname.startsWith(`${p}-`));
      });
      if (hasContainer) return true;
      return liveImages.some((img: any) => {
        const tags: string[] = Array.isArray(img.RepoTags) ? img.RepoTags : img.Tags ? [img.Tags] : [];
        return tags.some((t) => {
          const tl = String(t || '').toLowerCase();
          return prefixes.some((p) => tl.startsWith(`${p}-`) || tl.startsWith(`${p}:`) || tl.startsWith(`${p}/`));
        });
      });
    };

    const slotImages = {
      blue: checkSlotAvailable('blue'),
      green: checkSlotAvailable('green'),
    };

    const hasRunningContainer = appContainers.some(
      (c: any) => (c.State || '').toLowerCase() === 'running'
    );
    const latestDeploy = Array.isArray(a.deploy_logs) ? a.deploy_logs[0] : null;
    const computedStatus: AppStack['status'] = a.php
      ? a.php.state === 'running'
        ? 'running'
        : 'stopped'
      : hasRunningContainer
      ? 'running'
      : latestDeploy && latestDeploy.ok === false
      ? 'failed'
      : 'stopped';

    return {
      id: a.id,
      php: a.php
        ? {
            siteType: a.php.site_type,
            phpVersion: a.php.php_version || '',
            rootDir: a.php.root_dir || '',
            uid: a.php.uid || 0,
          }
        : undefined,
      name: a.name,
      slug: a.name,
      description: a.description || '',
      sourceType: (a.source_type || 'compose') as any,
      gitRepo: a.git_config?.repo_url || a.git_repo || '',
      gitBranch: a.git_config?.branch || a.git_branch || 'main',
      gitCommitSha: a.git_config?.last_commit_sha || a.active_slot || 'main',
      gitCommitMessage: a.git_config?.last_commit_msg || 'Production build',
      webhookAutoDeploy: a.git_config?.auto_deploy ?? false,
      webhookSecret: a.git_config?.webhook_secret,
      status: computedStatus,
      activeSlot: a.active_slot,
      standbySlot: a.standby_slot,
      pinnedSlot: a.pinned_slot || '',
      slotImages,
      primaryDomain: (() => {
        const doms = Array.isArray(a.domains) ? a.domains : [];
        const pinned = doms.find((d: any) => d.is_primary);
        if (pinned?.domain) return pinned.domain;
        const custom = doms.find(
          (d: any) =>
            d.domain &&
            !String(d.domain).toLowerCase().endsWith('.sslip.io') &&
            !String(d.domain).toLowerCase().endsWith('.nip.io')
        );
        if (custom?.domain) return custom.domain;
        return doms[0]?.domain || '';
      })(),
      preview: a.preview,
      previewHostBase: a.preview_host_base || '',
      ports: Array.isArray(a.ports) ? a.ports : [],
      composeFilePath: 'docker-compose.yml',
      owner: 'admin',
      updatedAt: a.updated_at || 'Recently',
      cpuPct: 0,
      memMB: 0,
      diskMB: 0,
      reqPerMin: 0,
      services:
        appContainers.length > 0
          ? appContainers.map((c: any, sIdx: number) => ({
              id: c.ID || `svc-${a.id}-${sIdx}`,
              name: c.Name || `${a.name}_${sIdx}`,
              service: c.ComposeService || (Array.isArray(a.services) && a.services[sIdx]) || 'app',
              image: c.Image || `${a.name}:latest`,
              state: ((c.State || '').toLowerCase() === 'running'
                ? 'running'
                : 'exited') as any,
              uptime: c.Status || 'Active',
              cpuPct: 0,
              memUsedMB: 0,
              memLimitMB: 1024,
              ports: c.Ports || '',
            }))
          : [],
    domains: Array.isArray(a.domains)
      ? a.domains.map((d: any) => ({
          id: String(d.id),
          domain: d.domain,
          targetService: d.service,
          targetPort: d.port,
          isPrimary: Boolean(d.is_primary),
          tlsProvider: 'LetsEncrypt' as const,
          tlsStatus: 'active' as const,
          forceHttps: d.enable_https ?? true,
          enableWww: d.enable_www ?? false,
          serveStatic: d.serve_static ?? false,
          staticPath: d.static_path,
          staticUrlPrefix: d.static_url_prefix,
          serveMedia: d.serve_media ?? false,
          mediaPath: d.media_path,
          mediaUrlPrefix: d.media_url_prefix,
          gzip: true,
          createdAt: d.created_at || 'Active',
        }))
      : [],
    files: [
      {
        path: 'docker-compose.yml',
        name: 'docker-compose.yml',
        type: 'file' as const,
        sizeBytes: 100,
        updatedAt: 'Recently',
        language: 'yaml' as const,
        content: 'services:\n  app:\n    build: .\n',
      },
    ],
    envVars: (a.env_content || '')
      .split('\n')
      .filter((l: string) => l.includes('='))
      .map((l: string, evIdx: number) => {
        const parts = l.split('=');
        return {
          id: `ev-${evIdx}`,
          key: parts[0].trim(),
          value: parts.slice(1).join('=').trim(),
          isSecret: /key|secret|pass|token/i.test(parts[0]),
        };
      }),
    deployments: Array.isArray(a.deploy_logs)
      ? a.deploy_logs.map((dl: any) => ({
          id: `dep-${dl.id}`,
          commitSha: dl.commit_sha || 'latest',
          commitMessage: dl.action || 'Deploy',
          branch: 'main',
          trigger: 'manual-compose' as const,
          author: 'admin',
          status: dl.ok ? ('ready' as const) : ('failed' as const),
          durationSec: 15,
          startedAt: dl.created_at,
          slot: dl.slot,
          rawOutput: dl.output || "",
          buildSteps: [],
        }))
      : [],
    logs: [],
    volumes: Array.isArray(a.volumes)
      ? a.volumes.map((v: string) => ({
          name: v,
          mountPath: `/var/lib/${v}`,
          driver: 'local',
          sizeMB: 0,
        }))
      : [],
      collaborators: Array.isArray(a.collaborators)
        ? a.collaborators.map((c: any) => {
            let perms: string[] = [];
            try {
              if (Array.isArray(c.permissions)) perms = c.permissions;
              else if (typeof c.permissions === 'string' && c.permissions.startsWith('['))
                perms = JSON.parse(c.permissions);
              else if (typeof c.Permissions === 'string' && c.Permissions.startsWith('['))
                perms = JSON.parse(c.Permissions);
            } catch {
              perms = [];
            }
            return {
              userId: String(c.user_id || c.UserID || ''),
              username: c.username || c.Username || `user_${c.user_id || c.UserID}`,
              email: c.email || '',
              role: c.role || c.Role || 'viewer',
              permissions: perms,
              addedAt: c.created_at || c.CreatedAt || 'Active',
            };
          })
        : [],
      hasSchedule: Array.isArray(a.backup_schedules) && a.backup_schedules.some((sc: any) => sc.enabled),
      backupSchedules: Array.isArray(a.backup_schedules)
        ? a.backup_schedules.map((sc: any) => ({
            id: sc.id,
            destinationId: sc.destination_id,
            cronExpression: sc.cron_schedule || sc.cron_expression || '0 3 * * *',
            retentionCount: sc.retention_count || 7,
            backupType: sc.backup_type || 'app',
            enabled: Boolean(sc.enabled),
          }))
        : [],
    };
  };

  const loadLiveApps = (rawApps: any[], liveContainers: any[] = [], liveImages: any[] = []) => {
    setApps(rawApps.map((a) => mapRawAppToStack(a, liveContainers, liveImages)));
  };

  // Hydrate live Go Fiber backend state when running inside the embedded NextDeploy binary
  React.useEffect(() => {
    void backendApi.bootstrap().then((res) => {
      setIsBootstrapping(false);
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
      if (data.public_ip) {
        setServerPublicIp(data.public_ip);
      }
      if (data.user) {
        setCurrentUser({
          id: data.user.id,
          username: data.user.username || 'masud',
          role: data.user.role || 'admin',
        });
      }

      setPhpEnabled(Boolean((data as any).php?.enabled));
      setMyQuota((data as any).quota && !(data as any).quota.unlimited ? (data as any).quota : null);

      const liveContainers: any[] = Array.isArray(data.containers) ? data.containers : [];
      const liveImages: any[] = Array.isArray(data.images) ? data.images : [];

      // 1. Live Apps
      if (Array.isArray(data.apps)) {
        loadLiveApps(data.apps, liveContainers, liveImages);
      }

      // 2. Live Docker Images with Live Container Usage
      if (Array.isArray(data.images)) {
        setImages(
          data.images.map((img: any) => {
            const tagStr =
              Array.isArray(img.RepoTags) && img.RepoTags.length > 0
                ? img.RepoTags[0]
                : img.Tags || img.ID.slice(0, 12);
            const parts = tagStr.split(':');
            const repo = parts[0] || tagStr;
            const tag = parts[1] || 'latest';
            const shortId = img.ID.slice(0, 12);

            // Match running containers referencing this image (by RepoTag, name, or ID)
            const inUseBy = liveContainers
              .filter((c: any) => {
                const cImg = c.Image || '';
                return (
                  cImg === tagStr ||
                  cImg === repo ||
                  cImg.startsWith(repo + ':') ||
                  cImg.includes(shortId)
                );
              })
              .map((c: any) => c.Name || c.ID);

            return {
              id: shortId,
              repository: repo,
              tag: tag,
              sizeMB: Math.round((img.Size || 0) / (1024 * 1024)),
              created: img.Created ? new Date(img.Created).toLocaleDateString() : 'Recently',
              inUseBy,
            };
          })
        );
      }

      // 3. Live Host Telemetry & Historical Series
      if (Array.isArray(data.telemetry_history) && data.telemetry_history.length > 0) {
        setTelemetry(data.telemetry_history);
      } else if (data.sys) {
        const sys = data.sys;
        const currentCpu = Math.round(sys.CPUPercent || 0);
        const currentMem = Math.round(sys.MemUsedPct || 0);
        const currentPoint: TelemetryPoint = {
          time: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
          cpu: currentCpu,
          memory: currentMem,
          diskIo: Math.round(sys.DiskUsedPct || 0),
          netInMbps: 0,
          netOutMbps: 0,
          requests: 0,
        };
        setTelemetry((prev) => [...prev.slice(1), currentPoint]);
      }
      if (data.sys) {
        setSys(data.sys);
      }
      if (data.cgroup) {
        setCgroupInfo(data.cgroup);
      }

      // 5. Live Volumes
      if (Array.isArray(data.volumes)) {
        setVolumes(
          data.volumes.map((v: string) => ({
            name: v,
            appSlug: v.split('_')[0] || 'system',
            mountPoint: `/var/lib/docker/volumes/${v}/_data`,
            driver: 'local',
            sizeMB: 0,
            createdAt: 'Active',
            filesCount: 0,
            sampleFiles: [],
          }))
        );
      }

      // 6. Live Users & Audit Logs
      if (Array.isArray(data.users)) {
        setUsers(
          data.users.map((u: any) => ({
            id: String(u.ID || u.id),
            username: u.Username || u.username,
            email: u.Email || u.email || `${u.Username || u.username}@nextdeploy.local`,
            role: (u.Role || u.role || 'user') as any,
            status: (u.Status || u.status || 'active') as any,
            appsCount: 0,
            maxApps: u.MaxApps || u.max_apps || 5,
            cpuQuotaCores: u.MaxCPUs || u.max_cpus || 2,
            memUsedGB: 0,
            memQuotaGB: Math.round((u.MaxMemoryMB || u.max_memory_mb || 2048) / 1024),
            maxMemoryMB: u.MaxMemoryMB || u.max_memory_mb || 2048,
            diskUsedGB: Math.round((u.storage_used_bytes || 0) / (1024 * 1024 * 1024)),
            diskQuotaGB: Math.round((u.MaxStorageMB || u.max_storage_mb || 5120) / 1024),
            maxStorageMB: u.MaxStorageMB || u.max_storage_mb || 5120,
            bandwidthLimitMbps: u.BandwidthLimitMbps ?? u.bandwidth_limit_mbps ?? 0,
            allowDomainFileServer: u.AllowDomainFileServer ?? u.allow_domain_file_server ?? false,
            twoFactorEnabled: false,
            lastLoginAt: 'Recently',
          }))
        );
      }
      if (Array.isArray(data.cli_sessions)) {
        setCliSessions(
          data.cli_sessions.map((cs: any) => ({
            id: String(cs.id || cs.ID),
            tokenName: cs.token_name || cs.TokenName || 'CLI Token',
            prefix: cs.token_prefix || cs.TokenPrefix || 'nd_cli_...',
            fullToken: '',
            deviceHost: cs.hostname || cs.Hostname || 'workstation',
            osArch: `${cs.os || cs.OS || 'linux'}/${cs.arch || cs.Arch || 'amd64'}`,
            cliVersion: cs.version || cs.Version || 'nd v1.8.4',
            lastCommand: 'nd login',
            lastActiveAt: cs.last_seen || cs.LastSeen ? new Date(cs.last_seen || cs.LastSeen).toLocaleString() : 'Recently',
            ipAddress: getServerPublicIp() || '127.0.0.1',
          }))
        );
      }
      if (Array.isArray(data.audit_logs)) {
        setAuditLogs(
          data.audit_logs.map((al: any) => ({
            id: String(al.id ?? al.ID),
            actor: al.username || al.Username || 'system',
            actorRole: al.role || al.Role || (al.user_id === 1 || al.UserID === 1 ? 'admin' : (al.username || al.Username === 'system' ? 'system' : 'user')),
            action: al.action || al.Action || 'system.event',
            target: (al.target_id || al.TargetID)
              ? `${al.target_type || al.TargetType || 'resource'}: ${al.target_id || al.TargetID}`
              : (al.target_type || al.TargetType || 'system'),
            details: al.details || al.Details || '',
            ip: al.ip_address || al.IPAddress || al.ip || '127.0.0.1',
            timestamp: al.created_at || al.CreatedAt ? new Date(al.created_at || al.CreatedAt).toLocaleString() : 'Just now',
            status: 'success' as const,
          }))
        );
      }

      // 7. Live Registries & Git Providers
      if (Array.isArray(data.registries)) {
        setRegistries(
          data.registries.map((r: any) => ({
            id: String(r.id),
            provider: 'Custom' as const,
            name: r.name,
            registryUrl: r.url || 'docker.io',
            username: r.username || 'admin',
            tokenPreview: '••••••••',
            scope: 'global' as const,
            connectedApps: 0,
            imagesCount: 0,
            status: 'connected' as const,
            lastVerified: 'Recently',
          }))
        );
      }
      if (Array.isArray(data.git_providers)) {
        setGitProviders(
          data.git_providers.map((gp: any) => ({
            id: String(gp.id),
            provider: (gp.provider === 'gitlab' ? 'GitLab' : 'GitHub') as any,
            authMethod: (gp.auth_method || (gp.provider === 'gitlab' ? 'OAuth 2.0' : 'GitHub App')) as any,
            accountName: gp.account_login || gp.name,
            instanceUrl: gp.provider === 'gitlab' ? 'https://gitlab.com' : 'https://github.com',
            status: gp.needs_install ? ('error' as const) : ('connected' as const),
            reposAccessible: typeof gp.repos_accessible === 'number' ? gp.repos_accessible : 0,
            webhookDeliveryRate: '100%',
            webhookUrl: '',
            lastSynced: 'Recently',
            needsInstall: Boolean(gp.needs_install),
            installUrl: gp.install_url,
            appSlug: gp.app_slug,
            installationId: gp.installation_id,
          }))
        );
      }

      // 8. Live Backup Destinations & Migration Exports
      if (Array.isArray(data.backup_destinations)) {
        setBackupDestinations(
          data.backup_destinations.map((bd: any) => ({
            id: String(bd.id),
            name: bd.name,
            provider: bd.provider === 'gdrive' ? ('Google Drive' as const) : ('Cloudflare R2' as const),
            providerCode: (bd.provider || 'r2') as 'r2' | 'gdrive',
            bucket: bd.bucket || '',
            endpoint: '',
            region: 'auto',
            status: 'connected' as const,
            scheduleCron: '0 2 * * *',
            retentionDays: 7,
            usedGB: 0,
            quotaGB: 100,
            lastBackupAt: 'Never',
          }))
        );
      }
      if (Array.isArray(data.migrate_exports)) {
        setMigrationBundles(mapMigrateExports(data.migrate_exports));
      }
      if (data.caddy) {
        setCaddyInfo(data.caddy);
      }
      setDockerVersion(typeof data.docker_version === 'string' ? data.docker_version : '');
    });
  }, []);

  // A running export finishes on the server: refresh until none is running.
  const exportRunning = migrationBundles.some((b) => b.status === 'running');
  useEffect(() => {
    if (!exportRunning) return;
    const t = window.setInterval(async () => {
      const r = await backendApi.bootstrap();
      if (r.ok && Array.isArray(r.data?.migrate_exports)) {
        setMigrationBundles(mapMigrateExports(r.data.migrate_exports));
      }
    }, 3000);
    return () => window.clearInterval(t);
  }, [exportRunning]);

  const appendTelemetryTick = () => {
    const now = new Date();
    const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(
      now.getMinutes()
    ).padStart(2, '0')}`;
    const cpuVal = sys ? Math.round(sys.CPUPercent || 0) : 0;
    const memVal = sys ? Math.round(sys.MemUsedPct || 0) : 0;
    const diskVal = sys ? Math.round(sys.DiskUsedPct || 0) : 0;
    const nextPoint: TelemetryPoint = {
      time: timeStr,
      cpu: cpuVal,
      memory: memVal,
      diskIo: diskVal,
      netInMbps: 0,
      netOutMbps: 0,
      requests: 0,
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
    }).then(async (res) => {
      if (!res.ok) {
        toast.error(res.error || 'Failed to create project');
        return;
      }
      const createdId = (res.data as any)?.id || slug;
      if (payload.composeContent) {
        await backendApi.saveComposeFile(createdId, 'docker-compose.yml', payload.composeContent);
      }
      const bootRes = await backendApi.bootstrap();
      if (bootRes.ok && bootRes.data?.apps) {
        loadLiveApps(bootRes.data.apps);
      }
      setSelectedAppId(createdId);
      navigateTo('apps', createdId);
      toast.success(`Created ${slug}`);
    });
  };

  const containersCount = apps.reduce((acc, a) => acc + (a.services?.length || 0), 0);
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
          badge: { text: String(apps.filter((a) => !a.php).length), tone: 'count' },
        },
        { id: 'monitor', label: 'Monitoring', icon: <Activity className="h-3.5 w-3.5" /> },
      ],
    },
    ...(phpEnabled || currentUser?.role === 'admin'
      ? [
          {
            group: 'PHP Hosting',
            items: [
              {
                id: 'php-sites' as NavigationSection,
                label: 'PHP Sites',
                icon: <FileCode2 className="h-3.5 w-3.5" />,
                badge: { text: String(apps.filter((a) => a.php).length), tone: 'count' as const },
              },
              ...(currentUser?.role === 'admin'
                ? [{ id: 'php-instance' as NavigationSection, label: 'Instance', icon: <Server className="h-3.5 w-3.5" /> }]
                : []),
            ],
          },
        ]
      : []),
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

  // Pages that only make sense for (and are only allowed to) administrators.
  const ADMIN_ONLY_NAV: NavigationSection[] = [
    'monitor',
    'terminal',
    'proxy',
    'migrate',
    'users',
    'settings',
    'cli-sessions',
    'php-instance',
  ];
  const isAdminUser = currentUser?.role === 'admin';
  const visibleNavGroups = navGroups
    .map((g) => ({ ...g, items: g.items.filter((i) => isAdminUser || !ADMIN_ONLY_NAV.includes(i.id)) }))
    .filter((g) => g.items.length > 0);

  const currentNavLabel =
    visibleNavGroups.flatMap((g) => g.items).find((i) => i.id === activeNav)?.label || 'Overview';

  if (authMode) {
    return (
      <AuthView
        mode={authMode}
        onSuccess={() => {
          setAuthMode(null);
          window.location.href = '/overview';
        }}
      />
    );
  }

  if (isBootstrapping) {
    return (
      <div className="min-h-screen w-full bg-[#09090b] text-zinc-100 flex flex-col items-center justify-center gap-4">
        <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-orange-500/10 border border-orange-500/20">
          <div className="font-mono text-lg font-bold text-orange-400">ND</div>
        </div>
        <div className="flex items-center gap-2.5 font-mono text-xs text-zinc-400">
          <Loader2 className="h-4 w-4 animate-spin text-orange-400" />
          <span>Loading NextDeploy...</span>
        </div>
      </div>
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
        className={`fixed inset-y-0 left-0 z-50 flex w-60 xl:w-64 2xl:w-68 shrink-0 flex-col bg-[#09090b] border-r border-white/[0.06] transition-transform lg:sticky lg:top-0 lg:h-screen lg:self-start lg:translate-x-0 ${
          mobileMenuOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="flex h-14 items-center justify-between px-3.5 pt-2 shrink-0">
          <button
            type="button"
            onClick={() => {
              navigateTo('overview');
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
        <div className="px-3.5 pt-2.5 pb-1 shrink-0">
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
        <nav className="flex-1 min-h-0 overflow-y-auto sidebar-scrollbar px-3 py-2 space-y-4">
          {visibleNavGroups.map((grp) => (
            <div key={grp.group} className="space-y-0.5">
              <p className="mb-1 px-2.5 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                {grp.group}
              </p>
              {grp.items.map((item) => {
                const isActive =
                  (!selectedAppId && activeNav === item.id) ||
                  (selectedAppId && item.id === (selectedApp?.php ? 'php-sites' : 'apps'));
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => {
                      navigateTo(item.id);
                      setMobileMenuOpen(false);
                    }}
                    className={`relative flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 xl:py-2 text-xs font-medium transition-all cursor-pointer whitespace-nowrap ${
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
        <div className="p-3 shrink-0 border-t border-white/[0.05]">
          <div className="flex items-center justify-between gap-2 rounded-xl border border-white/[0.07] bg-[#111115] px-2.5 py-2">
            <div className="flex items-center gap-2 min-w-0">
              <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-orange-500/15 font-mono text-[10px] font-bold text-orange-400 uppercase ring-1 ring-orange-500/30">
                {(currentUser?.username || 'masud').slice(0, 2).toUpperCase()}
              </div>
              <div className="min-w-0">
                <span className="block truncate text-xs font-medium text-zinc-200">
                  {currentUser?.username || 'masud'}
                </span>
              </div>
            </div>
            <button
              type="button"
              onClick={handleLogout}
              title="Sign out of NextDeploy"
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 px-2.5 py-1 text-[11px] font-medium text-zinc-400 hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-300 transition-all cursor-pointer shrink-0"
            >
              <LogOut className="h-3 w-3" />
              <span>Logout</span>
            </button>
          </div>
        </div>
      </aside>

      {/* shadcn Sidebar-Inset Floating Main Canvas */}
      <div className="flex min-w-0 flex-1 flex-col lg:py-2 lg:pr-2">
        <div className="relative flex min-h-full flex-1 flex-col overflow-hidden lg:rounded-2xl lg:border lg:border-white/[0.07] bg-[#0c0c0f] shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_24px_60px_rgba(0,0,0,0.65)]">
          {/* Subtle Top Ambient Ember Glow */}
          <div className="pointer-events-none absolute inset-x-0 top-0 h-44 bg-[radial-gradient(ellipse_60%_50%_at_50%_-15%,rgba(249,115,22,0.07),transparent)]" />

          {/* Top Header Bar */}
          <header className="sticky top-0 z-30 flex h-13 xl:h-14 items-center justify-between gap-4 border-b border-white/[0.06] bg-[#0c0c0f]/85 px-4 sm:px-6 lg:px-8 xl:px-10 backdrop-blur-xl">
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
                  navigateTo('overview');
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
                      navigateTo(selectedApp.php ? 'php-sites' : 'apps');
                    }}
                    className="hover:text-zinc-100 transition-colors cursor-pointer whitespace-nowrap"
                  >
                    {selectedApp.php ? 'PHP Sites' : 'Projects'}
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
              {isAdminUser && dockerVersion && (
                <div className="hidden md:flex items-center gap-1.5 rounded-lg border border-white/[0.07] bg-[#111115] px-2.5 py-1 font-mono text-[11px] text-zinc-400">
                  <Server className={`h-3 w-3 ${caddyInfo?.up === false ? 'text-amber-400' : 'text-emerald-400'}`} />
                  <span>Docker {dockerVersion}</span>
                  <span className="text-zinc-600">·</span>
                  <span className={caddyInfo?.up === false ? 'text-amber-400' : 'text-emerald-400'}>
                    {caddyInfo?.up === false ? 'Caddy down' : 'Caddy'}
                  </span>
                </div>
              )}

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
          <main className="relative mx-auto w-full max-w-[1680px] 2xl:max-w-[1960px] flex-1 px-4 sm:px-6 lg:px-8 xl:px-10 py-4 sm:py-6 lg:py-7">
            {selectedApp ? (
              <AppWorkspace
                app={selectedApp}
                onBack={() => navigateTo(selectedApp.php ? 'php-sites' : 'apps')}
                quota={myQuota}
                onRefresh={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && r.data?.apps) loadLiveApps(r.data.apps, (r.data as any).containers || [], (r.data as any).images || []);
                }}
                onUpdateApp={(updated) =>
                  setApps((prev) => prev.map((a) => (a.id === updated.id ? updated : a)))
                }
                onDeleteApp={async (id) => {
                  const res = await backendApi.deleteApp(id);
                  if (!res.ok) {
                    toast.error(res.error || 'Failed to delete project');
                    return;
                  }
                  setApps((prev) => prev.filter((a) => a.id !== id));
                  navigateTo(selectedApp.php ? 'php-sites' : 'apps');
                  toast.success('Deleted project');
                }}
              />
            ) : selectedAppId && !selectedApp ? (
              <div className="rounded-xl border border-white/[0.08] bg-[#0e0e12] p-8 text-center max-w-lg mx-auto mt-12 space-y-3">
                <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-xl border border-white/[0.08] bg-white/[0.03]">
                  <Box className="h-5 w-5 text-zinc-400" />
                </div>
                <h3 className="font-semibold text-zinc-200">App not found</h3>
                <p className="text-xs text-zinc-400">
                  The application <code className="text-orange-400 bg-white/[0.05] px-1.5 py-0.5 rounded">{selectedAppId}</code> could not be found or you don&apos;t have permission to access it.
                </p>
                <div className="pt-2">
                  <button
                    type="button"
                    onClick={() => navigateTo('apps')}
                    className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-white/[0.06] hover:bg-white/[0.1] px-3.5 text-xs font-semibold text-zinc-200 transition-colors cursor-pointer"
                  >
                    Back to Projects
                  </button>
                </div>
              </div>
            ) : activeNav === 'overview' ? (
              <OverviewDashboard
                telemetry={telemetry}
                sys={sys}
                apps={apps}
                containersCount={containersCount}
                isAdmin={isAdminUser}
                quota={myQuota}
                services={[
                  { name: 'docker', ver: dockerVersion ? `v${dockerVersion}` : '', meta: `${containersCount} containers`, ok: !!dockerVersion },
                  { name: 'caddy-proxy', ver: '', meta: caddyInfo?.up ? 'serving' : caddyInfo?.status || 'not reachable', ok: !!caddyInfo?.up },
                  { name: 'mcp-server', ver: 'JSON-RPC', meta: `${MCP_TOOLS_CATALOG.length} tools`, ok: true },
                ]}
                onSelectApp={(id) => navigateTo('apps', id)}
                onSelectNav={(nav) => navigateTo(nav)}
              />
            ) : activeNav === 'apps' ? (
              <ProjectsListView
                apps={apps.filter((a) => !a.php)}
                onSelectApp={(id) => navigateTo('apps', id)}
                onUpdateApps={setApps}
                onCreateApp={() => setNewAppModalOpen(true)}
              />
            ) : activeNav === 'php-sites' ? (
              <PhpSitesView
                apps={apps}
                isAdmin={currentUser?.role === 'admin'}
                onOpenSite={(id) => navigateTo('apps', id)}
                onRefresh={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && r.data?.apps) loadLiveApps(r.data.apps, (r.data as any).containers || [], (r.data as any).images || []);
                }}
                onGoInstance={() => navigateTo('php-instance')}
              />
            ) : activeNav === 'php-instance' ? (
              <PhpInstanceView
                onChanged={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok) setPhpEnabled(Boolean((r.data as any)?.php?.enabled));
                }}
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
                onOpenAppTab={(id) => navigateTo('apps', id)}
                onUpdateApp={(updated) =>
                  setApps((prev) => prev.map((a) => (a.id === updated.id ? updated : a)))
                }
              />
            ) : activeNav === 'images' ? (
              <ImagesView
                images={images}
                isAdmin={isAdminUser}
                onDeleteImage={async (id) => {
                  try {
                    const res = await backendApi.globalImageRemove(id);
                    if (res.ok) {
                      setImages((prev) => prev.filter((i) => i.id !== id));
                      toast.success(`Removed image ${id}`);
                    } else {
                      toast.error(res.error || `Failed to remove image ${id}`);
                    }
                  } catch (err: any) {
                    toast.error(err?.message || `Failed to remove image`);
                  }
                }}
                onPruneUnused={async () => {
                  try {
                    const res = await backendApi.globalImagesPrune();
                    if (res.ok) {
                      setImages((prev) => prev.filter((i) => i.inUseBy.length > 0));
                      toast.success('Successfully pruned all unused Docker images');
                    } else {
                      toast.error(res.error || 'Failed to prune images');
                    }
                  } catch (err: any) {
                    toast.error(err?.message || 'Failed to prune images');
                  }
                }}
              />
            ) : activeNav === 'volumes' ? (
              <VolumesView
                volumes={volumes}
                onDeleteVolume={async (name) => {
                  const res = await backendApi.globalVolumeRemove(name);
                  if (!res.ok) {
                    toast.error(res.error || `Could not delete ${name}`);
                    return;
                  }
                  setVolumes((prev) => prev.filter((v) => v.name !== name));
                  toast.success(`Deleted volume ${name}`);
                }}
              />
            ) : activeNav === 'terminal' ? (
              <VpsTerminalView />
            ) : activeNav === 'git' ? (
              <GitAndRegistriesView
                mode="git"
                gitProviders={gitProviders}
                syncedRepos={syncedRepos.length > 0 ? syncedRepos : liveSyncedRepos}
                registries={registries}
                onAddRegistry={(r) => setRegistries((prev) => [...prev, r])}
              />
            ) : activeNav === 'registries' ? (
              <GitAndRegistriesView
                mode="registries"
                gitProviders={gitProviders}
                syncedRepos={syncedRepos.length > 0 ? syncedRepos : liveSyncedRepos}
                registries={registries}
                onAddRegistry={(r) => setRegistries((prev) => [...prev, r])}
                onDeleteRegistry={(id) =>
                  setRegistries((prev) => prev.filter((item) => item.id !== id))
                }
              />
            ) : activeNav === 'proxy' ? (
              <CaddyProxyView
                apps={apps}
                caddyInfo={caddyInfo}
                onRefreshCaddy={async () => {
                  const res = await backendApi.bootstrap();
                  if (res.ok && res.data?.caddy) {
                    setCaddyInfo(res.data.caddy);
                  }
                }}
              />
            ) : activeNav === 'backup' ? (
              <BackupsView
                apps={apps}
                destinations={backupDestinations}
                archives={backupArchives}
                onSelectApp={(id) => navigateTo('apps', id)}
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
                onExportBundle={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && Array.isArray(r.data?.migrate_exports)) {
                    setMigrationBundles(mapMigrateExports(r.data.migrate_exports));
                  }
                }}
                onDeleteBundle={(id) => {
                  void backendApi.migrateExportDelete(id);
                  setMigrationBundles((prev) => prev.filter((b) => b.id !== id));
                }}
              />
            ) : activeNav === 'mcp' ? (
              <McpAndCliView
                mode="mcp"
                tools={MCP_TOOLS_CATALOG}
                cliSessions={cliSessions}
                isAdmin={isAdminUser}
                onGenerateCliToken={() => {}}
              />
            ) : activeNav === 'cli-sessions' ? (
              <McpAndCliView
                mode="cli"
                tools={MCP_TOOLS_CATALOG}
                cliSessions={cliSessions}
                onGenerateCliToken={async (label) => {
                  const res = await backendApi.createCliToken(label);
                  const fullToken = res.data?.token;
                  if (!res.ok || !fullToken) {
                    toast.error(res.error || 'Could not create the token');
                    return;
                  }
                  const created: CliSessionItem = {
                    id: String(res.data?.id ?? Date.now()),
                    tokenName: label,
                    prefix: `${fullToken.slice(0, 11)}...`,
                    fullToken,
                    deviceHost: '—',
                    osArch: '—',
                    cliVersion: '—',
                    lastCommand: 'not used yet',
                    lastActiveAt: 'Never',
                    ipAddress: '—',
                  };
                  setCliSessions((prev) => [created, ...prev]);
                  void copyToClipboard(fullToken);
                  toast.success(`Created CLI token for ${label} (copied)`);
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
                users={users}
                auditLogs={auditLogs}
                cgroupInfo={cgroupInfo}
              />
            ) : activeNav === 'audit-logs' ? (
              <UsersAndSettingsView
                mode="audit-logs"
                users={users}
                auditLogs={auditLogs}
                cgroupInfo={cgroupInfo}
              />
            ) : isAdminUser && activeNav === 'settings' ? (
              <UsersAndSettingsView
                mode="settings"
                users={users}
                auditLogs={auditLogs}
                cgroupInfo={cgroupInfo}
              />
            ) : (
              <OverviewDashboard
                telemetry={telemetry}
                sys={sys}
                apps={apps}
                containersCount={containersCount}
                isAdmin={isAdminUser}
                quota={myQuota}
                services={[
                  { name: 'docker', ver: dockerVersion ? `v${dockerVersion}` : '', meta: `${containersCount} containers`, ok: !!dockerVersion },
                  { name: 'caddy-proxy', ver: '', meta: caddyInfo?.up ? 'serving' : caddyInfo?.status || 'not reachable', ok: !!caddyInfo?.up },
                  { name: 'mcp-server', ver: 'JSON-RPC', meta: `${MCP_TOOLS_CATALOG.length} tools`, ok: true },
                ]}
                onSelectApp={(id) => navigateTo('apps', id)}
                onSelectNav={(nav) => navigateTo(nav)}
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
        images={images}
        volumes={volumes}
        gitProviders={gitProviders}
        isAdmin={isAdminUser}
        onSelectNav={(nav) => navigateTo(nav)}
        onSelectApp={(id, tab) => navigateTo('apps', id, false, tab)}
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
