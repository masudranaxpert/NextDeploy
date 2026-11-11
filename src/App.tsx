import { OwnerContext } from './components/ui/OwnerBadge';
import React, { Suspense, useEffect, useMemo, useRef, useState } from 'react';
import { lazyRetry } from './lib/lazyRetry';
import { runWithViewTransition } from './lib/viewTransition';
import { normalizeCpuPct } from './lib/resourceUtils';
import {
  parseInitialRoute,
  buildRoutePath,
  hasLoggedInCookie,
  clearLoggedInCookie,
} from './lib/routes';
import {
  mapRawAppToStack,
  mapMigrateExports,
} from './lib/appStackMapper';
import { buildNavGroups, ADMIN_ONLY_NAV } from './components/layout/navItems';
import { AppSidebar } from './components/layout/AppSidebar';
import { AppHeader } from './components/layout/AppHeader';
import { Bot, Box } from 'lucide-react';
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
import { NextDeployLogoMark } from './components/ui/BrandIcons';
import { backendApi, formatWildcardDomain, getServerPublicIp, setServerPublicIp, setCachedShieldNodes, setPanelId } from './lib/backendApi';
import { copyToClipboard } from './lib/clipboard';
import { dataSync } from './lib/dataSync';
import { PageSkeleton, DashboardSkeleton } from './components/skeletons/PageSkeletons';

// Each screen is its own chunk with instant .preload() capability on hover.
const OverviewDashboard = lazyRetry(() => import('./features/overview/OverviewDashboard').then((m) => ({ default: m.OverviewDashboard })));
const AuthView = lazyRetry(() => import('./features/auth/AuthView').then((m) => ({ default: m.AuthView })));
const CommandPaletteModal = lazyRetry(() => import('./components/modals/CommandPaletteModal').then((m) => ({ default: m.CommandPaletteModal })));
const DeployNewAppModal = lazyRetry(() => import('./components/modals/DeployNewAppModal').then((m) => ({ default: m.DeployNewAppModal })));
const AppWorkspace = lazyRetry(() => import('./features/workspace/AppWorkspace').then((m) => ({ default: m.AppWorkspace })));
const ContainersView = lazyRetry(() => import('./features/docker/ContainersView').then((m) => ({ default: m.ContainersView })));
const ImagesView = lazyRetry(() => import('./features/docker/ImagesView').then((m) => ({ default: m.ImagesView })));
const VolumesView = lazyRetry(() => import('./features/docker/VolumesView').then((m) => ({ default: m.VolumesView })));
const VpsTerminalView = lazyRetry(() => import('./features/terminal/VpsTerminalView').then((m) => ({ default: m.VpsTerminalView })));
const MonitorView = lazyRetry(() => import('./features/monitoring/MonitorView').then((m) => ({ default: m.MonitorView })));
const GitAndRegistriesView = lazyRetry(() => import('./features/integrations/GitAndRegistriesView').then((m) => ({ default: m.GitAndRegistriesView })));
const CaddyProxyView = lazyRetry(() => import('./features/proxy/CaddyProxyView').then((m) => ({ default: m.CaddyProxyView })));
const CaddyOptionsView = lazyRetry(() => import('./features/proxy/CaddyOptionsView').then((m) => ({ default: m.CaddyOptionsView })));
const CaddyRoutesView = lazyRetry(() => import('./features/proxy/CaddyRoutesView').then((m) => ({ default: m.CaddyRoutesView })));
const BackupsView = lazyRetry(() => import('./features/backups/BackupsView').then((m) => ({ default: m.BackupsView })));
const MigrationView = lazyRetry(() => import('./features/migration/MigrationView').then((m) => ({ default: m.MigrationView })));
const McpAndCliView = lazyRetry(() => import('./features/automation/McpAndCliView').then((m) => ({ default: m.McpAndCliView })));
const UsersAndSettingsView = lazyRetry(() => import('./features/system/UsersAndSettingsView').then((m) => ({ default: m.UsersAndSettingsView })));
const PhpSitesView = lazyRetry(() => import('./features/php/PhpSitesView').then((m) => ({ default: m.PhpSitesView })));
const PhpInstanceView = lazyRetry(() => import('./features/php/PhpInstanceView').then((m) => ({ default: m.PhpInstanceView })));
const ProjectsListView = lazyRetry(() => import('./features/projects/ProjectsListView').then((m) => ({ default: m.default || m.ProjectsListView })));
const ShieldProxiesView = lazyRetry(() => import('./features/shield/ShieldProxiesView').then((m) => ({ default: m.ShieldProxiesView })));
const CopilotView = lazyRetry(() => import('./features/copilot/CopilotView').then((m) => ({ default: m.CopilotView })));

// On-demand route prefetching triggered by user hover (instant 0ms click feel)
const prefetchRoute = (nav: NavigationSection) => {
  switch (nav) {
    case 'overview':
      OverviewDashboard.preload?.();
      break;
    case 'apps':
      AppWorkspace.preload?.();
      ProjectsListView.preload?.();
      break;
    case 'containers':
      ContainersView.preload?.();
      break;
    case 'images':
      ImagesView.preload?.();
      break;
    case 'volumes':
      VolumesView.preload?.();
      break;
    case 'php-sites':
      PhpSitesView.preload?.();
      break;
    case 'php-instance':
      PhpInstanceView.preload?.();
      break;
    case 'proxy':
    case 'proxy-options':
    case 'proxy-routes':
      CaddyProxyView.preload?.();
      CaddyOptionsView.preload?.();
      CaddyRoutesView.preload?.();
      break;
    case 'shield-proxies':
      ShieldProxiesView.preload?.();
      break;
    case 'backup':
      BackupsView.preload?.();
      break;
    case 'git':
    case 'registries':
      GitAndRegistriesView.preload?.();
      break;
    case 'users':
    case 'settings':
    case 'audit-logs':
      UsersAndSettingsView.preload?.();
      break;
    case 'monitor':
      MonitorView.preload?.();
      break;
    case 'mcp':
    case 'cli-sessions':
      McpAndCliView.preload?.();
      break;
    case 'migrate':
      MigrationView.preload?.();
      break;
    case 'terminal':
      VpsTerminalView.preload?.();
      break;
    case 'nextdeploy-ai':
      CopilotView.preload?.();
      break;
  }
};

// Start downloading the code of the page in the address bar right away
if (typeof window !== 'undefined') {
  const path = window.location.pathname;
  if (path === '/' || path === '') {
    if (hasLoggedInCookie()) void import('./features/overview/OverviewDashboard');
    else void import('./features/auth/AuthView');
  }
  else if (path.startsWith('/overview')) void import('./features/overview/OverviewDashboard');
  else if (path.startsWith('/apps/')) void import('./features/workspace/AppWorkspace');
  else if (path.startsWith('/php-sites')) void import('./features/php/PhpSitesView');
  else if (path.startsWith('/php-instance')) void import('./features/php/PhpInstanceView');
  else if (path.startsWith('/apps')) void import('./features/projects/ProjectsListView');
  else if (path.startsWith('/containers')) void import('./features/docker/ContainersView');
  else if (path.startsWith('/images')) void import('./features/docker/ImagesView');
  else if (path.startsWith('/volumes')) void import('./features/docker/VolumesView');
  else if (path.startsWith('/nextdeploy-ai') || path.startsWith('/copilot')) void import('./features/copilot/CopilotView');
  else if (path.startsWith('/monitor')) void import('./features/monitoring/MonitorView');
  else if (path === '/login' || path === '/setup') void import('./features/auth/AuthView');
}

export default function App() {
  const initialRoute = parseInitialRoute();
  const [activeNav, setActiveNav] = useState<NavigationSection>(initialRoute.nav);
  const [selectedAppId, setSelectedAppId] = useState<string | null>(initialRoute.appId);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [authMode, setAuthMode] = useState<'setup' | 'login' | null>(() => {
    if (typeof window !== 'undefined') {
      if (window.location.pathname === '/setup') return 'setup';
      if (window.location.pathname === '/login') return 'login';
      if (window.location.pathname === '/' || window.location.pathname === '') {
        if (!hasLoggedInCookie()) {
          return 'login';
        }
      }
    }
    return null;
  });
  const [loadError, setLoadError] = useState('');
  const [isBootstrapping, setIsBootstrapping] = useState(() => {
    if (typeof window !== 'undefined') {
      if (window.location.pathname === '/setup' || window.location.pathname === '/login') {
        return false;
      }
      if (window.location.pathname === '/' || window.location.pathname === '') {
        if (!hasLoggedInCookie()) {
          return false;
        }
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
    prefetchRoute(nav);
    runWithViewTransition(() => {
      setActiveNav(nav);
      setSelectedAppId(appId);
      const targetPath = buildRoutePath(nav, appId, tab);
      if (window.location.pathname + window.location.search !== targetPath) {
        if (replace) {
          window.history.replaceState({ nav, appId, tab }, '', targetPath);
        } else {
          window.history.pushState({ nav, appId, tab }, '', targetPath);
        }
      }
    });
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
  const hostCaps = useMemo(
    () =>
      sys
        ? {
            memoryMB: Math.floor((sys.MemTotalGB || 0) * 1024),
            cpus: sys.NumCPU || 0,
            diskMB: Math.floor((sys.DiskTotalGB || 0) * 1024),
          }
        : undefined,
    [sys],
  );
  const [apps, setApps] = useState<AppStack[]>([]);
  const [liveContainers, setLiveContainers] = useState<any[]>([]);
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
  const [mcpEnabled, setMcpEnabled] = useState(false);
  const [apiTokens, setApiTokens] = useState<any[]>([]);
  const [systemSettings, setSystemSettings] = useState<Record<string, string>>({});
  const [myQuota, setMyQuota] = useState<any>(null);
  React.useEffect(() => {
    if (currentUser && currentUser.role !== 'admin' && !selectedAppId && ADMIN_ONLY_NAV.includes(activeNav)) {
      setActiveNav('overview');
      window.history.replaceState({ nav: 'overview', appId: null }, '', '/overview');
    }
  }, [currentUser, activeNav, selectedAppId]);
  const [cgroupInfo, setCgroupInfo] = useState<{ mode?: string; supported?: boolean }>({ mode: 'systemd', supported: true });
  const [dockerVersion, setDockerVersion] = useState('');
  const [caddyInfo, setCaddyInfo] = useState<{ up: boolean; status: string; config?: Record<string, string> } | null>(null);

  const handleLogout = async () => {
    clearLoggedInCookie();
    try {
      sessionStorage.removeItem('nextdeploy_bootstrap_cache_v1');
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

  const isAdminRef = React.useRef(false);
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setCmdPaletteOpen((prev) => !prev);
      } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'j' && isAdminRef.current) {
        e.preventDefault();
        navigateTo('nextdeploy-ai');
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  const cpuCapacityRef = useRef<number>(1);
  const loadLiveApps = (
    rawApps: any[],
    liveContainers: any[] = [],
    liveImages: any[] = [],
    cpuCapacityCores = cpuCapacityRef.current
  ) => {
    setApps(rawApps.map((a) => mapRawAppToStack(a, liveContainers, liveImages, cpuCapacityCores)));
  };

  const applyLiveMetrics = React.useCallback(
    (data: {
      sys?: any;
      history?: TelemetryPoint[];
      containers?: any[];
      apps_usage?: Record<string, { cpu_percent?: number; memory_mb?: number }>;
    }) => {
      if (!data || typeof data !== 'object') return;
      if (data.sys) {
        setSys(data.sys);
        if (isAdminRef.current && Number(data.sys.NumCPU) > 0) {
          cpuCapacityRef.current = Number(data.sys.NumCPU);
        }
      }
      if (Array.isArray(data.history) && data.history.length > 0) {
        setTelemetry(data.history);
      }
      const capCores = cpuCapacityRef.current || 1;
      if (Array.isArray(data.containers) && data.containers.length > 0) {
        const usageMap = new Map<string, { cpu_percent: number; memory_mb: number }>();
        data.containers.forEach((c) => {
          if (c?.id) {
            usageMap.set(c.id, {
              cpu_percent: normalizeCpuPct(c.cpu_percent || 0, capCores),
              memory_mb: c.memory_mb || 0,
            });
          }
        });
        setLiveContainers((prev) =>
          prev.map((c) => {
            const rawId = c.ID || c.id || '';
            const u =
              usageMap.get(rawId) ||
              (rawId.length >= 12 ? usageMap.get(rawId.slice(0, 12)) : undefined);
            return u ? { ...c, cpu_percent: u.cpu_percent, cpuPct: u.cpu_percent, memUsedMB: u.memory_mb, memory_mb: u.memory_mb } : c;
          })
        );
      }
      if (data.apps_usage && typeof data.apps_usage === 'object') {
        const appsUsage = data.apps_usage;
        setApps((prev) =>
          prev.map((app) => {
            const u = appsUsage[app.id];
            if (!u) return app;
            const cpuPct = normalizeCpuPct(u.cpu_percent || 0, capCores);
            const memMB = u.memory_mb || 0;
            if (app.cpuPct === cpuPct && app.memMB === memMB) return app;
            return { ...app, cpuPct, memMB };
          })
        );
      }
    },
    []
  );

  const applyBootstrapData = React.useCallback((data: any) => {
    if (!data || typeof data !== 'object') return;
    if (data.public_ip) {
      setServerPublicIp(data.public_ip);
    }
    if (data.panel_id) {
      setPanelId(data.panel_id);
    }
    if (Array.isArray(data.shield_nodes)) {
      setCachedShieldNodes(data.shield_nodes, data.origin_ip || data.public_ip || data.sys?.PublicIP || '', data.panel_id);
    }
    if (data.user) {
      setCurrentUser({
        id: data.user.id,
        username: data.user.username || 'admin',
        role: data.user.role || 'admin',
      });
    }

    setPhpEnabled(Boolean((data as any).php?.enabled));
    setMcpEnabled(Boolean((data as any).mcp_enabled));
    setApiTokens(Array.isArray((data as any).api_tokens) ? (data as any).api_tokens : []);
    const quotaObj = (data as any).quota;
    setMyQuota(quotaObj && !quotaObj.unlimited ? quotaObj : null);

    const isAdminRole = (data.user?.role || currentUser?.role) === 'admin';
    const cpuCap =
      !isAdminRole && quotaObj?.max_cpus > 0
        ? Number(quotaObj.max_cpus)
        : Number(data.sys?.NumCPU) || Number(quotaObj?.host_cpus) || cpuCapacityRef.current || 1;
    cpuCapacityRef.current = cpuCap;

    const rawContainers: any[] = Array.isArray(data.containers) ? data.containers : [];
    const liveContainers: any[] = rawContainers.map((c: any) => ({
      ...c,
      cpu_percent: normalizeCpuPct(c?.cpu_percent || 0, cpuCap),
    }));
    const liveImages: any[] = Array.isArray(data.images) ? data.images : [];

    setLiveContainers(liveContainers);

    // 1. Live Apps
    if (Array.isArray(data.apps)) {
      data.apps.forEach((a: any) => {
        if (a.php || a.PHP) {
          try { sessionStorage.setItem(`nd_php_app_${a.id || a.ID}`, '1'); } catch {}
        }
      });
      loadLiveApps(data.apps, liveContainers, liveImages, cpuCap);
    }

    // 2. Live Docker Images with Live Container Usage
    if (Array.isArray(data.images)) {
      const normalizeImageRef = (ref: string) => {
        if (!ref) return '';
        let s = ref.trim().toLowerCase();
        s = s.replace(/^docker\.io\/library\//, '');
        s = s.replace(/^docker\.io\//, '');
        s = s.replace(/^library\//, '');
        return s;
      };

      setImages(
        data.images.map((img: any) => {
          const repoTags: string[] = Array.isArray(img.RepoTags) ? img.RepoTags : [];
          const tagStr = repoTags.length > 0 ? repoTags[0] : (img.Tags || img.ID.slice(0, 12));
          const parts = tagStr.split(':');
          const repo = parts[0] || tagStr;
          const tag = parts[1] || 'latest';
          const shortId = img.ID.slice(0, 12).toLowerCase();

          // Collect all canonical representations for this image to match against containers
          const candidateRefs = new Set<string>();
          for (const rt of repoTags) {
            if (rt && rt !== '<none>') {
              candidateRefs.add(normalizeImageRef(rt));
              const p = rt.split(':');
              if (p[1] === 'latest') {
                candidateRefs.add(normalizeImageRef(p[0]));
              }
            }
          }
          if (tagStr && tagStr !== '<none>') {
            candidateRefs.add(normalizeImageRef(tagStr));
            if (tag === 'latest') {
              candidateRefs.add(normalizeImageRef(repo));
            }
          }

          const inUseBy = liveContainers
            .filter((c: any) => {
              const cImg = normalizeImageRef(c.Image || '');
              if (!cImg) return false;
              if (candidateRefs.has(cImg)) return true;
              if (cImg === normalizeImageRef(tagStr)) return true;
              if (tag === 'latest' && cImg === normalizeImageRef(repo)) return true;
              if (shortId && cImg.includes(shortId)) return true;
              return false;
            })
            .map((c: any) => c.Name || c.ID);

          return {
            id: shortId,
            repository: repo,
            tag: tag,
            sizeMB: Math.round((img.Size || 0) / (1024 * 1024)),
            created: img.Created ? new Date(img.Created).toLocaleDateString() : 'Recently',
            inUseBy,
            owner: data.resource_owners?.images?.[img.ID],
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
        caddyRequests: 0,
        panelRequests: 0,
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
      const volSizes: Record<string, number> = data.volume_sizes || {};
      const volHumanSizes: Record<string, string> = data.volume_human_sizes || {};
      setVolumes(
        data.volumes.map((v: string) => ({
          name: v,
          appSlug: v.split('_')[0] || 'system',
          mountPoint: `/var/lib/docker/volumes/${v}/_data`,
          driver: 'local',
          sizeMB: typeof volSizes[v] === 'number' ? volSizes[v] : 0,
          sizeHuman: volHumanSizes[v] || '',
          createdAt: 'Active',
          filesCount: 0,
          sampleFiles: [],
          owner: data.resource_owners?.volumes?.[v],
        }))
      );
    }

    // 6. Live Users & Audit Logs
    if (Array.isArray(data.users)) {
      setUsers(
        data.users.map((u: any) => {
          const uid = String(u.ID || u.id);
          const uUsage = data.user_usage?.[uid];
          let appsCountFromList = 0;
          if (Array.isArray(data.apps)) {
            appsCountFromList = data.apps.filter((a: any) => String(a.owner_id ?? a.OwnerID ?? '') === uid).length;
          }
          const appsCount = Math.max(uUsage?.apps_count ?? 0, appsCountFromList);
          const databasesCount = uUsage?.databases_count ?? 0;
          const maxDatabases = uUsage?.max_databases ?? 5;

          return {
            id: uid,
            username: u.Username || u.username,
            email: u.Email || u.email || `${u.Username || u.username}@nextdeploy.local`,
            role: (u.Role || u.role || 'user') as any,
            status: (u.Status || u.status || 'active') as any,
            appsCount,
            databasesCount,
            maxDatabases,
            maxApps: u.MaxApps || u.max_apps || 5,
            cpuQuotaCores: u.MaxCPUs || u.max_cpus || 2,
            memUsedGB: (uUsage?.memory_mb || 0) / 1024,
            memUsedMB: uUsage?.memory_mb || 0,
            cpuUsedCores: uUsage?.cpu_cores || 0,
            storageUsedBytes: uUsage?.storage_bytes || 0,
            storageBreakdown: uUsage?.storage,
            memQuotaGB: Math.round((u.MaxMemoryMB || u.max_memory_mb || 2048) / 1024),
            maxMemoryMB: u.MaxMemoryMB || u.max_memory_mb || 2048,
            diskUsedGB: (uUsage?.storage_bytes || 0) / (1024 * 1024 * 1024),
            diskQuotaGB: Math.round((u.MaxStorageMB || u.max_storage_mb || 5120) / 1024),
            maxStorageMB: u.MaxStorageMB || u.max_storage_mb || 5120,
            bandwidthLimitMbps: u.BandwidthLimitMbps ?? u.bandwidth_limit_mbps ?? 0,
            allowDomainFileServer: u.AllowDomainFileServer ?? u.allow_domain_file_server ?? false,
            twoFactorEnabled: false,
            lastLoginAt: 'Recently',
          };
        })
      );
    }
    const apiTokensMap = new Map<string, string>();
    if (Array.isArray(data.api_tokens)) {
      for (const t of data.api_tokens) {
        const full = String(t.token || t.Token || '').trim();
        if (full) {
          if (t.token_prefix) apiTokensMap.set(String(t.token_prefix), full);
          if (t.TokenPrefix) apiTokensMap.set(String(t.TokenPrefix), full);
          if (t.name) apiTokensMap.set(String(t.name), full);
          if (t.Name) apiTokensMap.set(String(t.Name), full);
          if (t.id) apiTokensMap.set(String(t.id), full);
          if (t.ID) apiTokensMap.set(String(t.ID), full);
        }
      }
    }

    const sessionsList: CliSessionItem[] = [];
    const seenTokenKeys = new Set<string>();

    if (Array.isArray(data.cli_sessions)) {
      for (const cs of data.cli_sessions) {
        const prefix = cs.token_prefix || cs.TokenPrefix || 'nd_cli_...';
        const name = cs.token_name || cs.TokenName || 'CLI Token';
        const id = String(cs.id || cs.ID);
        const fullToken =
          cs.fullToken ||
          cs.token ||
          apiTokensMap.get(prefix) ||
          apiTokensMap.get(name) ||
          apiTokensMap.get(id) ||
          '';

        seenTokenKeys.add(prefix);
        if (name) seenTokenKeys.add(name);
        if (id) seenTokenKeys.add(id);

        sessionsList.push({
          id,
          tokenName: name,
          prefix,
          fullToken,
          deviceHost: cs.hostname || cs.Hostname || 'workstation',
          osArch: `${cs.os || cs.OS || 'linux'}/${cs.arch || cs.Arch || 'amd64'}`,
          cliVersion: cs.version || cs.Version || 'nd v1.8.4',
          lastCommand: cs.last_command || cs.LastCommand || '-',
          lastActiveAt:
            cs.last_seen || cs.LastSeen
              ? new Date(cs.last_seen || cs.LastSeen).toLocaleString()
              : 'Recently',
          ipAddress: getServerPublicIp() || '127.0.0.1',
        });
      }
    }

    if (Array.isArray(data.api_tokens)) {
      for (const tok of data.api_tokens) {
        const kind = String(tok.kind || tok.Kind || 'cli').toLowerCase();
        const prefix = tok.token_prefix || tok.TokenPrefix || '';
        const name = tok.name || tok.Name || 'CLI Token';
        const id = String(tok.id || tok.ID);
        if (
          kind === 'cli' &&
          !seenTokenKeys.has(prefix) &&
          !seenTokenKeys.has(name) &&
          !seenTokenKeys.has(id)
        ) {
          const fullToken = String(tok.token || tok.Token || '');
          sessionsList.push({
            id,
            tokenName: name,
            prefix: prefix || (fullToken ? `${fullToken.slice(0, 11)}...` : 'nd_cli_...'),
            fullToken,
            deviceHost: '—',
            osArch: '—',
            cliVersion: '—',
            lastCommand: 'not used yet',
            lastActiveAt:
              tok.created_at || tok.CreatedAt
                ? new Date(tok.created_at || tok.CreatedAt).toLocaleString()
                : 'Never',
            ipAddress: '—',
          });
        }
      }
    }

    setCliSessions(sessionsList);
    if (Array.isArray(data.audit_logs)) {
      setAuditLogs(
        data.audit_logs.map((al: any) => ({
          id: String(al.id ?? al.ID),
          actor: al.username || al.Username || 'system',
          actorRole: al.role || al.Role || ((al.username || al.Username) === 'system' ? 'system' : 'user'),
          targetOwner: al.target_owner || undefined,
          targetOwnerRole: al.target_owner_role || undefined,
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
          id: String(r.ID ?? r.id),
          provider: 'Custom' as const,
          name: r.Name ?? r.name,
          registryUrl: r.ServerAddress || r.url || 'docker.io',
          username: r.Username || r.username || '',
          tokenPreview: '••••••••',
          scope: (r.UserID ?? r.user_id) == null ? ('global' as const) : ('personal' as const),
          owner: data.resource_owners?.registries?.[String(r.ID ?? r.id)],
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
          owner: data.resource_owners?.providers?.[String(gp.id)],
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
        data.backup_destinations.map((bd: any) => {
          let cfg: any = {};
          try {
            cfg = JSON.parse(bd.Config || bd.config || '{}');
          } catch {}
          const isGDrive = (bd.Provider ?? bd.provider) === 'gdrive';
          const usedBytes = Number(cfg.storage_used ?? bd.storage_used ?? 0);
          const limitBytes = Number(cfg.storage_limit ?? bd.storage_limit ?? 0);
          return {
            id: String(bd.ID ?? bd.id),
            owner: data.resource_owners?.destinations?.[String(bd.ID ?? bd.id)],
            name: bd.Name ?? bd.name,
            provider: isGDrive ? ('Google Drive' as const) : ('Cloudflare R2' as const),
            providerCode: (bd.Provider || bd.provider || 'r2') as 'r2' | 'gdrive',
            bucket: cfg.bucket || bd.bucket || (isGDrive ? 'gdrive:/nextdeploy' : 'r2://backups'),
            endpoint: '',
            region: 'auto',
            email: cfg.email || bd.email || '',
            displayName: cfg.display_name || bd.display_name || '',
            status: 'connected' as const,
            scheduleCron: '0 2 * * *',
            retentionDays: 7,
            usedGB: Number((usedBytes / (1024 * 1024 * 1024)).toFixed(2)),
            quotaGB: limitBytes > 0 ? Number((limitBytes / (1024 * 1024 * 1024)).toFixed(0)) : 0,
            lastBackupAt: 'Active',
          };
        })
      );
    }
    if (Array.isArray(data.migrate_exports)) {
      setMigrationBundles(mapMigrateExports(data.migrate_exports));
    }
    if (data.caddy) {
      setCaddyInfo(data.caddy);
    }
    if (data.settings && typeof data.settings === 'object') {
      setSystemSettings(data.settings);
    }
    setDockerVersion(typeof data.docker_version === 'string' ? data.docker_version : '');
  }, []);

  // Any request answered with 401 means the session ended: show the sign-in screen once.
  useEffect(() => {
    const onUnauthorized = () => {
      clearLoggedInCookie();
      justLoggedInRef.current = false;
      setAuthMode((cur) => cur ?? 'login');
      setIsBootstrapping(false);
      if (window.location.pathname !== '/login' && window.location.pathname !== '/setup') {
        window.history.replaceState(null, '', '/login');
      }
    };
    window.addEventListener('nd:unauthorized', onUnauthorized);
    return () => window.removeEventListener('nd:unauthorized', onUnauthorized);
  }, []);

  // Tell the user when the panel was updated while this tab was open.
  useEffect(() => {
    if (isBootstrapping || authMode) return;
    let baseline = '';
    let stopped = false;
    const check = async () => {
      try {
        const r = await fetch('/api/panel/version', { cache: 'no-store' });
        if (!r.ok) return;
        const { build } = (await r.json()) as { build?: string };
        if (!build || stopped) return;
        if (!baseline) baseline = build;
        else if (build !== baseline) {
          toast.message('NextDeploy was updated', {
            id: 'new-version',
            description: 'Reload to use the new version.',
            duration: Infinity,
            action: { label: 'Reload', onClick: () => window.location.reload() },
          });
        }
      } catch {
        // offline: try again later
      }
    };
    void check();
    const t = window.setInterval(check, 90000);
    const onFocus = () => void check();
    window.addEventListener('focus', onFocus);
    return () => {
      stopped = true;
      window.clearInterval(t);
      window.removeEventListener('focus', onFocus);
    };
  }, [isBootstrapping, authMode]);

  // Smart Data Sync: revalidate when navigating between pages
  useEffect(() => {
    if (isBootstrapping || authMode) return;
    const isLiveNav = activeNav === 'overview' || activeNav === 'containers' || activeNav === 'monitor';
    if (isLiveNav) {
      // Live views only need the ultra-lightweight metrics (<2KB) — bypass heavy full-bootstrap
      backendApi
        .metricsHistory()
        .then((res) => {
          if (res?.ok && res?.data) {
            applyLiveMetrics(res.data);
          }
        })
        .catch(() => {});
    } else {
      // Resource views (apps, volumes, backups, settings) revalidate full state if stale
      dataSync.checkRouteRevalidate(
        () => backendApi.bootstrap(),
        (data) => applyBootstrapData(data)
      );
    }
  }, [activeNav, selectedAppId, isBootstrapping, authMode, applyBootstrapData, applyLiveMetrics]);

  // Live Telemetry & Metrics Polling (adaptive: 4s active -> 15s idle -> pause on 10m inactivity)
  useEffect(() => {
    if (isBootstrapping || authMode) return;
    const isLiveNav = activeNav === 'overview' || activeNav === 'containers' || activeNav === 'monitor';
    if (!isLiveNav) return;

    let mounted = true;
    let inFlight = false;
    let timerId: number | undefined;
    let lastActivity = Date.now();

    const getInterval = () => {
      const idleMs = Date.now() - lastActivity;
      if (idleMs > 10 * 60 * 1000) return 0; // Paused after 10m idle
      if (idleMs > 2 * 60 * 1000) return 15000; // Back off to 15s after 2m idle
      return 4000; // Active 4s polling
    };

    const scheduleNext = () => {
      if (!mounted) return;
      if (timerId !== undefined) window.clearTimeout(timerId);
      const delay = getInterval();
      if (delay > 0) {
        timerId = window.setTimeout(() => {
          void tick();
        }, delay);
      }
    };

    const tick = async () => {
      if (!mounted) return;
      if (typeof document !== 'undefined' && document.visibilityState !== 'visible') {
        scheduleNext();
        return;
      }
      if (inFlight) {
        scheduleNext();
        return;
      }
      inFlight = true;
      try {
        const res = await backendApi.metricsHistory();
        if (res?.ok && res?.data && mounted) {
          applyLiveMetrics(res.data);
        }
      } catch {
        // network blip: ignore
      } finally {
        inFlight = false;
        scheduleNext();
      }
    };

    const onUserActivity = () => {
      const wasIdle = Date.now() - lastActivity > 2 * 60 * 1000;
      lastActivity = Date.now();
      if (wasIdle && mounted) {
        // Instantly wake up from idle backoff
        void tick();
      }
    };

    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        lastActivity = Date.now();
        void tick();
      }
    };

    document.addEventListener('visibilitychange', onVisibilityChange);
    window.addEventListener('mousemove', onUserActivity, { passive: true });
    window.addEventListener('keydown', onUserActivity, { passive: true });
    window.addEventListener('touchstart', onUserActivity, { passive: true });

    // Start initial tick
    scheduleNext();

    return () => {
      mounted = false;
      if (timerId !== undefined) window.clearTimeout(timerId);
      document.removeEventListener('visibilitychange', onVisibilityChange);
      window.removeEventListener('mousemove', onUserActivity);
      window.removeEventListener('keydown', onUserActivity);
      window.removeEventListener('touchstart', onUserActivity);
    };
  }, [activeNav, isBootstrapping, authMode, applyLiveMetrics]);

  // Window Focus revalidation (guarded by 30s focus cooldown)
  useEffect(() => {
    if (isBootstrapping || authMode) return;
    const onFocus = () => {
      dataSync.checkFocusRevalidate(
        () => backendApi.bootstrap(),
        (data) => applyBootstrapData(data)
      );
    };
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [isBootstrapping, authMode, applyBootstrapData]);

  // Instant mutation invalidation: triggered on create, delete, or update
  useEffect(() => {
    const onInvalidate = () => {
      dataSync.invalidate(
        () => backendApi.bootstrap(),
        (data) => applyBootstrapData(data)
      );
    };
    window.addEventListener('nd:invalidate', onInvalidate);
    return () => window.removeEventListener('nd:invalidate', onInvalidate);
  }, [applyBootstrapData]);

  const bootGenRef = useRef(0);
  const justLoggedInRef = useRef(false);

  // First page data. Only a 401 means "not signed in". A slow or failed request is retried and, if
  // it keeps failing, shown as an error: it must never put a signed-in user on the login page.
  const loadBootstrap = React.useCallback(
    async (attempt = 0, gen = ++bootGenRef.current): Promise<void> => {
      const res = await backendApi.bootstrap();
      if (gen !== bootGenRef.current) return;
      if (res.status === 401 || res.data?.needs_setup) {
        clearLoggedInCookie();
        setIsBootstrapping(false);
        const nextMode = res.data?.needs_setup || window.location.pathname === '/setup' ? 'setup' : 'login';
        if (justLoggedInRef.current) {
          justLoggedInRef.current = false;
          toast.error('Session did not persist after sign-in. Check browser cookies or HTTPS settings.');
        }
        setAuthMode(nextMode);
        const targetPath = nextMode === 'setup' ? '/setup' : '/login';
        if (window.location.pathname !== targetPath) {
          window.history.replaceState(null, '', targetPath);
        }
        return;
      }
      if (!res.ok || !res.data) {
        if (attempt < 4) {
          await new Promise((r) => setTimeout(r, 800 * (attempt + 1)));
          if (gen !== bootGenRef.current) return;
          return loadBootstrap(attempt + 1, gen);
        }
        justLoggedInRef.current = false;
        setIsBootstrapping(false);
        setLoadError(res.error || 'Cannot reach the server');
        return;
      }
      justLoggedInRef.current = false;
      setLoadError('');
      setAuthMode(null);
      applyBootstrapData(res.data);
      if (window.location.pathname === '/login' || window.location.pathname === '/setup') {
        window.history.replaceState(null, '', '/overview');
        setActiveNav('overview');
      }
      setIsBootstrapping(false);
    },
    [applyBootstrapData]
  );

  React.useEffect(() => {
    try {
      // an older version kept the whole first-page data (including secrets) in sessionStorage
      sessionStorage.removeItem('nextdeploy_bootstrap_cache_v1');
    } catch {
      // ignore
    }
    if (authMode) return;
    void loadBootstrap();
  }, [loadBootstrap, authMode]);

  // A running export finishes on the server: refresh until none is running.
  const exportRunning = migrationBundles.some((b) => b.status === 'running');
  React.useEffect(() => {
    if (!exportRunning) return;
    let inFlight = false;
    const t = window.setInterval(async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const r = await backendApi.bootstrap();
        if (r.ok && Array.isArray(r.data?.migrate_exports)) {
          setMigrationBundles(mapMigrateExports(r.data.migrate_exports));
        }
      } finally {
        inFlight = false;
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
      caddyRequests: 0,
      panelRequests: 0,
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

  // running only: a project's running containers, and each PHP site that is up
  const containersCount = apps.reduce(
    (acc, a) =>
      acc + (a.php ? (a.status === 'running' ? 1 : 0) : (a.services || []).filter((s) => s.state === 'running').length),
    0,
  );
  const selectedApp = apps.find((a) => a.id === selectedAppId) || null;

  const isAdminUser = currentUser?.role === 'admin';
  useEffect(() => {
    isAdminRef.current = isAdminUser;
  }, [isAdminUser]);

  const ownerCtxValue = useMemo(
    () => ({ viewerId: currentUser ? Number(currentUser.id) : 0, isAdmin: isAdminUser }),
    [currentUser, isAdminUser]
  );

  const visibleNavGroups = React.useMemo(() => {
    return buildNavGroups({
      apps,
      phpEnabled,
      userRole: currentUser?.role,
    });
  }, [apps, phpEnabled, currentUser?.role]);

  const currentNavLabel =
    visibleNavGroups.flatMap((g) => g.items).find((i) => i.id === activeNav)?.label || 'Overview';

  if (authMode) {
    return (
      <>
        <Toaster
          position="bottom-right"
          theme="dark"
          richColors
          closeButton
          duration={4500}
          visibleToasts={4}
          mobileOffset={{ bottom: 16, left: 16, right: 16 }}
          toastOptions={{ classNames: { toast: "font-sans text-xs max-w-[calc(100vw-32px)]" } }}
        />
        <Suspense fallback={null}>
          <AuthView
            mode={authMode}
            onSuccess={() => {
              OverviewDashboard.preload?.();
              justLoggedInRef.current = true;
              runWithViewTransition(() => {
                setSelectedAppId(null);
                setActiveNav('overview');
                setLoadError('');
                setIsBootstrapping(true);
                setAuthMode(null);
                window.history.replaceState({ nav: 'overview', appId: null }, '', '/overview');
              });
            }}
          />
        </Suspense>
      </>
    );
  }

  if (loadError && !isBootstrapping) {
    return (
      <div className="min-h-screen w-full bg-[#09090b] text-zinc-100 flex flex-col items-center justify-center gap-4 px-6 text-center">
        <NextDeployLogoMark className="h-12 w-12 rounded-xl shadow-[0_10px_25px_-5px_rgba(254,110,0,0.35)]" />
        <p className="text-sm font-semibold">Cannot load NextDeploy</p>
        <p className="font-mono text-xs text-zinc-400">{loadError}</p>
        <button
          type="button"
          onClick={() => {
            setLoadError('');
            setIsBootstrapping(true);
            void loadBootstrap();
          }}
          className="h-8 rounded-lg bg-orange-500 px-4 text-xs font-semibold text-white hover:bg-orange-600 cursor-pointer"
        >
          Try again
        </button>
      </div>
    );
  }

  return (
    <OwnerContext.Provider value={ownerCtxValue}>
    <div className="min-h-screen bg-[#09090b] text-zinc-100 flex selection:bg-orange-500/30">
      <Toaster
        position="bottom-right"
        theme="dark"
        richColors
        closeButton
        duration={4500}
        visibleToasts={4}
        mobileOffset={{ bottom: 16, left: 16, right: 16 }}
        toastOptions={{ classNames: { toast: "font-sans text-xs max-w-[calc(100vw-32px)]" } }}
      />

      <AppSidebar
        mobileMenuOpen={mobileMenuOpen}
        setMobileMenuOpen={setMobileMenuOpen}
        activeNav={activeNav}
        selectedAppId={selectedAppId}
        selectedApp={selectedApp}
        visibleNavGroups={visibleNavGroups}
        navigateTo={navigateTo}
        prefetchRoute={prefetchRoute}
        setCmdPaletteOpen={setCmdPaletteOpen}
        currentUser={currentUser}
        onLogout={handleLogout}
      />

      {/* shadcn Sidebar-Inset Floating Main Canvas */}
      <div className="flex min-w-0 flex-1 flex-col lg:py-2 lg:pr-2">
        <div className="relative flex min-h-full flex-1 flex-col overflow-hidden lg:rounded-lg lg:border lg:border-white/[0.07] bg-[#0c0c0f] shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_24px_60px_rgba(0,0,0,0.65)]">
          {/* Subtle Top Ambient Ember Glow */}
          <div className="pointer-events-none absolute inset-x-0 top-0 h-44 bg-[radial-gradient(ellipse_60%_50%_at_50%_-15%,rgba(249,115,22,0.07),transparent)]" />

          {/* Top Header Bar */}
          <AppHeader
            setMobileMenuOpen={setMobileMenuOpen}
            navigateTo={navigateTo}
            selectedApp={selectedApp}
            currentNavLabel={currentNavLabel}
            isAdminUser={isAdminUser}
            dockerVersion={dockerVersion}
            caddyInfo={caddyInfo}
            setNewAppModalOpen={setNewAppModalOpen}
          />

          {/* Main Viewport */}
          <main
            className={
              activeNav === 'nextdeploy-ai'
                ? "relative w-full flex-1 p-0 h-[calc(100vh-3.5rem)] max-w-none overflow-hidden"
                : "relative mx-auto w-full max-w-[1680px] 2xl:max-w-[1960px] flex-1 px-4 sm:px-6 lg:px-8 xl:px-10 py-4 sm:py-6 lg:py-7"
            }
          >
            <Suspense fallback={<PageSkeleton nav={selectedApp ? 'workspace' : activeNav} />}>
            {isBootstrapping ? (
              <PageSkeleton nav={selectedApp ? 'workspace' : activeNav} />
            ) : selectedApp ? (
              <AppWorkspace
                app={selectedApp}
                onBack={() => navigateTo(selectedApp.php ? 'php-sites' : 'apps')}
                onNavigateToBackup={() => navigateTo('backup')}
                quota={myQuota}
                onRefresh={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && r.data) applyBootstrapData(r.data);
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
              (() => {
                const isPhpSite404 = Boolean(
                  selectedAppId &&
                    (sessionStorage.getItem(`nd_php_app_${selectedAppId}`) === '1' ||
                      window.location.search.includes('php') ||
                      activeNav === 'php-sites')
                );
                return (
                  <div className="rounded-xl border border-white/[0.08] bg-[#0e0e12] p-8 text-center max-w-lg mx-auto mt-12 space-y-3">
                    <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-xl border border-white/[0.08] bg-white/[0.03]">
                      <Box className="h-5 w-5 text-zinc-400" />
                    </div>
                    <h3 className="font-semibold text-zinc-200">
                      {isPhpSite404 ? 'PHP site not found' : 'App not found'}
                    </h3>
                    <p className="text-xs text-zinc-400">
                      The {isPhpSite404 ? 'PHP site' : 'application'}{' '}
                      <code className="text-orange-400 bg-white/[0.05] px-1.5 py-0.5 rounded">{selectedAppId}</code>{' '}
                      could not be found or has been removed.
                    </p>
                    <div className="pt-2">
                      <button
                        type="button"
                        onClick={() => navigateTo(isPhpSite404 ? 'php-sites' : 'apps')}
                        className={`inline-flex h-8 items-center gap-1.5 rounded-lg px-3.5 text-xs font-semibold transition-colors cursor-pointer ${
                          isPhpSite404
                            ? 'bg-orange-600 hover:bg-orange-500 text-white'
                            : 'bg-white/[0.06] hover:bg-white/[0.1] text-zinc-200'
                        }`}
                      >
                        {isPhpSite404 ? 'Back to PHP Sites' : 'Back to Projects'}
                      </button>
                    </div>
                  </div>
                );
              })()
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
                onRefresh={async () => {
                  const r = await backendApi.metricsHistory();
                  if (r?.ok && r?.data) applyLiveMetrics(r.data);
                }}
                onSelectApp={(id) => navigateTo('apps', id)}
                onSelectNav={(nav) => navigateTo(nav)}
              />
            ) : isAdminUser && activeNav === 'nextdeploy-ai' ? (
              <CopilotView currentUser={currentUser} />
            ) : activeNav === 'apps' ? (
              <ProjectsListView
                apps={apps.filter((a) => !a.php)}
                onSelectApp={(id) => navigateTo('apps', id)}
                onUpdateApps={setApps}
                onCreateApp={() => setNewAppModalOpen(true)}
                onRefresh={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && r.data) applyBootstrapData(r.data);
                }}
              />
            ) : activeNav === 'php-sites' ? (
              <PhpSitesView
                apps={apps}
                isAdmin={currentUser?.role === 'admin'}
                onOpenSite={(id) => {
                  try { sessionStorage.setItem(`nd_php_app_${id}`, '1'); } catch {}
                  navigateTo('apps', id);
                }}
                onDeleteSite={(id) => {
                  setApps((prev) => prev.filter((a) => a.id !== id));
                }}
                onRefresh={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && r.data) applyBootstrapData(r.data);
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
                hostMemMB={sys?.MemTotalGB ? Math.round(sys.MemTotalGB * 1024) : undefined}
                hostCPUs={sys?.NumCPU || undefined}
                onSelectApp={(id) => navigateTo('apps', id)}
                onStreamTick={appendTelemetryTick}
                onUpdateTelemetry={setTelemetry}
                onLiveMetrics={applyLiveMetrics}
              />
            ) : activeNav === 'containers' ? (
              <ContainersView
                apps={apps}
                liveContainers={liveContainers}
                onOpenAppTab={(id) => navigateTo('apps', id)}
                onRefresh={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && r.data) applyBootstrapData(r.data);
                }}
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
            ) : activeNav === 'proxy-options' ? (
              <CaddyOptionsView />
            ) : activeNav === 'proxy-routes' ? (
              <CaddyRoutesView />
            ) : activeNav === 'shield-proxies' ? (
              <ShieldProxiesView />
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
                mcpEnabled={mcpEnabled}
                isAdmin={isAdminUser}
                apiTokens={apiTokens}
                onTokensChanged={async () => {
                  const r = await backendApi.bootstrap();
                  if (r.ok && Array.isArray((r.data as any)?.api_tokens)) setApiTokens((r.data as any).api_tokens);
                }}
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
                host={hostCaps}
                users={users}
                auditLogs={auditLogs}
                cgroupInfo={cgroupInfo}
                settings={systemSettings}
                currentUserId={currentUser ? String(currentUser.id) : undefined}
              />
            ) : activeNav === 'audit-logs' ? (
              <UsersAndSettingsView
                mode="audit-logs"
                users={users}
                auditLogs={auditLogs}
                cgroupInfo={cgroupInfo}
                currentUserId={currentUser ? String(currentUser.id) : undefined}
              />
            ) : isAdminUser && activeNav === 'settings' ? (
              <UsersAndSettingsView
                mode="settings"
                host={hostCaps}
                users={users}
                auditLogs={auditLogs}
                cgroupInfo={cgroupInfo}
                settings={systemSettings}
                currentUserId={currentUser ? String(currentUser.id) : undefined}
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
            </Suspense>
          </main>
        </div>
      </div>

      {/* Modals */}
      {cmdPaletteOpen && (
        <Suspense fallback={null}>
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
        </Suspense>
      )}

      {newAppModalOpen && (
        <Suspense fallback={null}>
          <DeployNewAppModal
            open={newAppModalOpen}
            onClose={() => setNewAppModalOpen(false)}
            onCreateApp={handleCreateApp}
          />
        </Suspense>
      )}

      {/* Floating shortcut to the NextDeploy AI page */}
      {isAdminUser && activeNav !== 'nextdeploy-ai' && (
        <button
          type="button"
          onClick={() => navigateTo('nextdeploy-ai')}
          aria-label="Open NextDeploy AI"
          className="fixed right-4 bottom-[max(1rem,env(safe-area-inset-bottom))] z-40 flex size-11 items-center justify-center gap-2 rounded-md border border-white/10 bg-zinc-900/95 text-xs font-medium sm:right-5 sm:bottom-5 sm:size-auto sm:px-3 sm:py-2 text-zinc-200 shadow-xl backdrop-blur-md hover:border-white/20 hover:bg-zinc-800 active:scale-95 transition-all cursor-pointer group"
        >
          <div className="flex h-5 w-5 items-center justify-center rounded bg-orange-500/20 text-orange-400">
            <Bot className="h-3.5 w-3.5" />
          </div>
          <span className="max-sm:hidden">ND AI</span>
          <kbd className="hidden sm:inline-block rounded bg-white/[0.08] px-1.5 py-0.5 font-mono text-[9px] text-zinc-400">
            ⌘J
          </kbd>
        </button>
      )}

    </div>
    </OwnerContext.Provider>
  );
}
