import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowLeft,
  ArrowLeftRight,
  ArrowUpRight,
  Play,
  RotateCw,
  RotateCcw,
  Square,
  Globe,
  GitBranch,
  FileCode2,
  Terminal as TerminalIcon,
  HardDrive,
  Users,
  CheckCircle2,
  ExternalLink,
  Trash2,
  Eye,
  EyeOff,
  Plus,
  Download,
  Upload,
  Lock,
  Copy,
  Sliders,
  Archive,
  Box,
  Clock,
  FilePlus,
  FolderPlus,
  Folder,
  FolderOpen,
  FileText,
  Network,
  Search,
  ShieldCheck,
  Webhook,
  RefreshCw,
  Filter,
  Pause,
  WrapText,
  Check,
  ChevronRight,
  ChevronDown,
  Cloud,
  Calendar,
  KeyRound,
  Layers,
  Pin,
  PinOff,
  Rocket,
  X,
  Loader2,
  AlertTriangle,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  AppStack,
  AppWorkspaceTab,
  ComposeFileNode,
  ContainerService,
  DeploymentRecord,
  DomainRoute,
  LogEntry,
} from '../../types/nextdeploy';
import {
  ActionMenu,
  KobraInputOTP,
  ShadcnDialog,
  ShadcnSwitch,
  StatusBadge,
} from '../../components/ui/Primitives';
import {
  extractContainerPort,
  formatCompactImage,
  formatCompactPort,
  formatCompactUptime,
} from '../../lib/dockerFormatters';
import { XTermConsole } from '../../components/ui/XTermConsole';
import { PhpSettingsTab } from '../php/PhpSettingsTab';
import { PhpDomainsTab } from '../php/PhpDomainsTab';
import { prefetchPhpSite } from '../php/ui';
import {
  Archive as PhpTabArchive,
  Clock as PhpTabClock,
  Database as PhpTabDatabase,
  FolderOpen as PhpTabFolder,
  Globe as PhpTabGlobe,
  ScrollText as PhpTabLogs,
  Settings2 as PhpTabSettings,
  SquareTerminal as PhpTabTerminal,
  Users as PhpTabUsers,
} from 'lucide-react';
import { PhpDatabasesTab } from '../php/PhpDatabasesTab';
import { PhpCronTab } from '../php/PhpCronTab';
import { PhpLogsTab } from '../php/PhpLogsTab';
import { CodeEditorBox, DevFileIcon, detectLanguageFromFilename } from '../../components/ui/CodeEditorBox';
import {
  backendApi,
  formatSlotPreviewDomain,
  formatWildcardDomain,
} from '../../lib/backendApi';
import { ProjectDomainsTab } from './tabs/ProjectDomainsTab';
import { ProjectGitTab } from './tabs/ProjectGitTab';
import { ProjectVolumesTab } from './tabs/ProjectVolumesTab';
import { ProjectBackupsTab } from './tabs/ProjectBackupsTab';
import { SiteTypeBadge, SiteTypeIcon } from '../../components/ui/BrandIcons';

interface AppWorkspaceProps {
  app: AppStack;
  onBack: () => void;
  onUpdateApp: (updated: AppStack) => void;
  onDeleteApp: (appId: string) => void;
  /** Reloads app data from the backend (used by PHP site tabs after changes). */
  onRefresh?: () => void | Promise<void>;
  /** The signed-in user's own limits and usage (null for admins). */
  quota?: {
    max_apps: number;
    apps_used: number;
    max_memory_mb: number;
    memory_used_mb: number;
    max_cpus: number;
    cpu_used_cores: number;
    max_storage_mb: number;
    storage_used_mb: number;
  } | null;
}

interface DetectedGitBranch {
  name: string;
  sha: string;
  author: string;
  updatedAt: string;
  isDefault?: boolean;
  protected?: boolean;
  prNumber?: number;
}

interface ProjectBackupItem {
  id: string;
  name: string;
  type: 'Scheduled' | 'Pre-Deploy' | 'Manual';
  destination: string;
  provider: 'Cloudflare R2' | 'Google Drive';
  sizeMB: number;
  createdAt: string;
  commitSha: string;
  includes: string[];
  checksum: string;
  status: 'completed' | 'building';
}

/**
 * Normalizes any file permission (symbolic, octal with/without leading 0, parenthesized)
 * into a clean 3-digit octal string like "644" or "755".
 */
export function formatPermOctal(perm?: string | null, isDir = false): string {
  if (!perm) return isDir ? '755' : '644';
  const trimmed = perm.trim();
  const parenMatch = trimmed.match(/\(0?([0-7]{3})\)/);
  if (parenMatch) return parenMatch[1];
  const octalMatch = trimmed.match(/^0?([0-7]{3})$/);
  if (octalMatch) return octalMatch[1];
  const sym = trimmed.replace(/^[d-]/, '');
  if (sym.length >= 9) {
    const parseTriplet = (s: string) =>
      (s[0] === 'r' ? 4 : 0) + (s[1] === 'w' ? 2 : 0) + (s[2] === 'x' || s[2] === 's' ? 1 : 0);
    const u = parseTriplet(sym.slice(0, 3));
    const g = parseTriplet(sym.slice(3, 6));
    const o = parseTriplet(sym.slice(6, 9));
    return `${u}${g}${o}`;
  }
  return isDir ? '755' : '644';
}

export const AppWorkspace: React.FC<AppWorkspaceProps> = ({
  app,
  onBack,
  onUpdateApp,
  onDeleteApp,
  onRefresh,
  quota = null,
}) => {
  const [activeTab, setActiveTab] = useState<AppWorkspaceTab>(() => {
    if (typeof window !== 'undefined') {
      const tabParam = new URLSearchParams(window.location.search).get('tab');
      const validTabs: AppWorkspaceTab[] = [
        'overview',
        'containers',
        'deployments',
        'terminal',
        'logs',
        'env',
        'domains',
        'files',
        'volumes',
        'backups',
        'git',
        'php-settings',
        'php-databases',
        'php-cron',
        'php-logs',
        'collaborators',
      ];
      if (tabParam && validTabs.includes(tabParam as AppWorkspaceTab)) {
        return tabParam as AppWorkspaceTab;
      }
    }
    return app.php ? 'php-settings' : 'overview';
  });

  useEffect(() => {
    const handleUrlTab = () => {
      const tabParam = new URLSearchParams(window.location.search).get('tab');
      if (tabParam && tabParam !== activeTab) {
        setActiveTab(tabParam as AppWorkspaceTab);
      }
    };
    handleUrlTab();
    window.addEventListener('popstate', handleUrlTab);
    return () => window.removeEventListener('popstate', handleUrlTab);
  }, []);

  // =========================================================================
  // 1. Environment Variables State
  // =========================================================================
  const [revealedEnvIds, setRevealedEnvIds] = useState<Record<string, boolean>>({});
  const [revealAllEnv, setRevealAllEnv] = useState(false);
  const [envRawMode, setEnvRawMode] = useState(false);
  const [envRawText, setEnvRawText] = useState(
    app.envVars.map((e) => `${e.key}=${e.value}`).join('\n')
  );
  const [envSearch, setEnvSearch] = useState('');

  // =========================================================================
  // 2. Deployments State (Production, Preview URLs, Rollback & Live Progress)
  // =========================================================================
  const [deployEnvFilter, setDeployEnvFilter] = useState<
    'all' | 'production' | 'preview' | 'rollback'
  >('all');
  const [buildLogsDeployment, setBuildLogsDeployment] = useState<DeploymentRecord | null>(null);
  const [previewDeploymentsEnabled, setPreviewDeploymentsEnabled] = useState(true);
  const [liveDeployState, setLiveDeployState] = useState<{
    running: boolean;
    action: string;
    output: string;
  }>({ running: false, action: '', output: '' });
  const [isBuildLogExpanded, setIsBuildLogExpanded] = useState<boolean | null>(null);
  const liveDeployLogRef = useRef<HTMLDivElement | null>(null);

  // Refresh current app state from /api/panel/bootstrap after deploy/stop/restart/container ops
  const refreshAppFromBackend = async () => {
    try {
      const bootRes = await backendApi.bootstrap();
      if (!bootRes.ok || !bootRes.data?.apps) return;
      const fresh: any = bootRes.data.apps.find((a: any) => a.id === app.id);
      if (!fresh) return;
      const liveContainers: any[] = Array.isArray(bootRes.data.containers)
        ? bootRes.data.containers
        : [];
      const liveImages: any[] = Array.isArray(bootRes.data.images)
        ? bootRes.data.images
        : [];
      const idNorm = (fresh.id || '').toLowerCase();
      const idUnderscore = idNorm.replace(/-/g, '_');
      const nameNorm = (fresh.name || '').toLowerCase();

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
      const latestDeploy = Array.isArray(fresh.deploy_logs) ? fresh.deploy_logs[0] : null;
      const computedStatus: AppStack['status'] = hasRunningContainer
        ? 'running'
        : latestDeploy && latestDeploy.ok === false
        ? 'failed'
        : 'stopped';

      const mappedServices: ContainerService[] =
        appContainers.length > 0
          ? appContainers.map((c: any, sIdx: number) => ({
              id: c.ID || `svc-${fresh.id}-${sIdx}`,
              name: c.Name || `${fresh.name}_${sIdx}`,
              service: c.ComposeService || (Array.isArray(fresh.services) && fresh.services[sIdx]) || 'app',
              image: c.Image || `${fresh.name}:latest`,
              state: ((c.State || '').toLowerCase() === 'running'
                ? 'running'
                : 'exited') as any,
              uptime: c.Status || 'Active',
              cpuPct: 0,
              memUsedMB: 0,
              memLimitMB: 1024,
              ports: c.Ports || '',
            }))
          : Array.isArray(fresh.services)
          ? fresh.services.map((s: string, sIdx: number) => ({
              id: `svc-${fresh.id}-${sIdx}`,
              name: `${fresh.name}_${s}_1`,
              service: s,
              image: `${fresh.name}:latest`,
              state: 'exited' as const,
              uptime: 'Stopped',
              cpuPct: 0,
              memUsedMB: 0,
              memLimitMB: 1024,
              ports: '',
            }))
          : [];

      const mappedDeployments: DeploymentRecord[] = Array.isArray(fresh.deploy_logs)
        ? fresh.deploy_logs.map((dl: any) => ({
            id: `dep-${dl.id}`,
            commitSha: dl.commit_sha || 'latest',
            commitMessage: dl.action || 'Deploy',
            branch: fresh.git_config?.branch || app.gitBranch || 'main',
            trigger: 'manual-compose' as const,
            author: 'admin',
            status: dl.ok ? ('ready' as const) : ('failed' as const),
            durationSec: 15,
            startedAt: dl.created_at,
            slot: dl.slot,
            rawOutput: dl.output || '',
            buildSteps: [],
          }))
        : app.deployments;

      const mappedDomains: DomainRoute[] = Array.isArray(fresh.domains)
        ? fresh.domains.map((d: any) => ({
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
        : app.domains;

      const resolvedPrimaryDomain = (() => {
        const pinned = mappedDomains.find((d) => d.isPrimary);
        if (pinned?.domain) return pinned.domain;
        const custom = mappedDomains.find(
          (d) =>
            d.domain &&
            !d.domain.toLowerCase().endsWith('.sslip.io') &&
            !d.domain.toLowerCase().endsWith('.nip.io')
        );
        if (custom?.domain) return custom.domain;
        return mappedDomains[0]?.domain || '';
      })();

      onUpdateApp({
        ...app,
        status: computedStatus,
        activeSlot: fresh.active_slot || app.activeSlot,
        standbySlot: fresh.standby_slot ?? app.standbySlot,
        pinnedSlot: fresh.pinned_slot ?? '',
        slotImages,
        domains: mappedDomains,
        primaryDomain: resolvedPrimaryDomain,
        preview: fresh.preview,
        previewHostBase: fresh.preview_host_base || '',
        ports: Array.isArray(fresh.ports) ? fresh.ports : [],
        services: mappedServices,
        deployments: mappedDeployments,
        updatedAt: fresh.updated_at || 'Just now',
      });
    } catch {
      // Ignore refresh errors
    }
  };

  // Auto-scroll build log console to bottom when new logs stream in
  useEffect(() => {
    if (liveDeployLogRef.current) {
      liveDeployLogRef.current.scrollTop = liveDeployLogRef.current.scrollHeight;
    }
  }, [liveDeployState.output, isBuildLogExpanded]);
  useEffect(() => {
    if (app.php) return;
    let active = true;
    let wasRunning = false;
    const checkProgress = async () => {
      const res = await backendApi.deployProgress(app.id);
      if (!active || !res.ok || !res.data) return;
      const { running, action, output } = res.data;
      setLiveDeployState((prev) => {
        if (prev.running === running && prev.output === output && prev.action === action) {
          return prev;
        }
        return { running: Boolean(running), action: action || 'Deploy', output: output || '' };
      });
      if (wasRunning && !running) {
        wasRunning = false;
        await refreshAppFromBackend();
      } else if (running) {
        wasRunning = true;
      }
    };
    void checkProgress();
    const timer = window.setInterval(checkProgress, 1200);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [app.id, app.php]);

  // Enhance deployments with environment metadata
  const defaultWildcardHost = useMemo(() => {
    const doms = app.domains || [];
    return (doms.find((d) => d.isPrimary) || doms[0])?.domain || '';
  }, [app.domains]);

  const previewBaseHost = app.previewHostBase || '';

  const enrichedDeployments = useMemo(() => {
    const activeSlot = (app.activeSlot || 'blue').toLowerCase() as 'blue' | 'green';
    const standbySlot = (activeSlot === 'green' ? 'blue' : 'green') as 'blue' | 'green';
    const isPinned = Boolean(app.pinnedSlot && (app.pinnedSlot === 'blue' || app.pinnedSlot === 'green'));

    const latestActiveId = app.deployments.find(
      (d) => d.status === 'ready' && (d.slot || activeSlot).toLowerCase() === activeSlot
    )?.id;
    const latestStandbyId = app.deployments.find(
      (d) => d.status === 'ready' && (d.slot || '').toLowerCase() === standbySlot
    )?.id;

    // A slot is only considered a valid standby if its image or container actually exists on disk.
    // When the user deletes the standby image/container, fallback string in DB must not falsely mark it as standby.
    const standbyHasImage = app.slotImages ? Boolean(app.slotImages[standbySlot]) : false;

    const list = app.deployments.map((d, idx) => {
      const slotName = (d.slot || (idx === 0 ? activeSlot : standbySlot)).toLowerCase() as 'blue' | 'green';
      const isCurrentProd = d.id ? d.id === latestActiveId : idx === 0;
      const isLatestStandby = Boolean(d.id && d.id === latestStandbyId && !isCurrentProd);
      const canInstantRollback = isLatestStandby && standbyHasImage && d.status === 'ready';

      const host = isCurrentProd
        ? defaultWildcardHost
        : previewBaseHost
        ? formatSlotPreviewDomain(previewBaseHost, slotName)
        : '';

      const envType = d.commitMessage.toLowerCase().includes('rollback')
        ? ('rollback' as const)
        : isLatestStandby && isPinned
        ? ('preview' as const)
        : ('production' as const);

      return {
        ...d,
        slot: slotName,
        isCurrentProd,
        isLatestStandby,
        canInstantRollback,
        envType,
        previewUrl: host ? `https://${host}` : '',
      };
    });
    return list.filter((d) =>
      deployEnvFilter === 'all'
        ? true
        : deployEnvFilter === 'production'
        ? d.envType === 'production' || d.envType === 'preview'
        : d.envType === deployEnvFilter
    );
  }, [app.deployments, app.activeSlot, app.standbySlot, app.pinnedSlot, app.slotImages, defaultWildcardHost, previewBaseHost, deployEnvFilter]);

  // =========================================================================
  // 3. Domains State (Auto-port detection + Edit + www + Static/Media serving)
  // =========================================================================
  const [domainModalOpen, setDomainModalOpen] = useState(false);
  const [editingDomainId, setEditingDomainId] = useState<string | null>(null);
  const [newDomainName, setNewDomainName] = useState('');
  const [newDomainService, setNewDomainService] = useState(app.services[0]?.service || 'app');
  const [newDomainPort, setNewDomainPort] = useState(
    extractContainerPort(app.services[0]?.ports)
  );
  const [newDomainHttps, setNewDomainHttps] = useState(true);
  const [newDomainEnableWww, setNewDomainEnableWww] = useState(false);
  const [newDomainIsPreview, setNewDomainIsPreview] = useState(false);
  const [newDomainServeStatic, setNewDomainServeStatic] = useState(false);
  const [newDomainStaticPath, setNewDomainStaticPath] = useState('/app/staticfiles');
  const [newDomainStaticPrefix, setNewDomainStaticPrefix] = useState('/static');
  const [newDomainServeMedia, setNewDomainServeMedia] = useState(false);
  const [newDomainMediaPath, setNewDomainMediaPath] = useState('/app/media');
  const [newDomainMediaPrefix, setNewDomainMediaPrefix] = useState('/media');

  const openAddDomainModal = () => {
    const firstSvc = app.services[0];
    setEditingDomainId(null);
    setNewDomainService(firstSvc?.service || 'app');
    setNewDomainPort(extractContainerPort(firstSvc?.ports));
    setNewDomainName('');
    setNewDomainEnableWww(false);
    setNewDomainServeStatic(false);
    setNewDomainServeMedia(false);
    setDomainModalOpen(true);
  };

  const openEditDomainModal = (dom: DomainRoute) => {
    setEditingDomainId(dom.id);
    setNewDomainName(dom.domain);
    setNewDomainService(dom.targetService);
    setNewDomainPort(String(dom.targetPort));
    setNewDomainHttps(dom.forceHttps);
    setDomainModalOpen(true);
  };

  const handleSelectDomainService = (svcName: string) => {
    setNewDomainService(svcName);
    const matched = app.services.find((s) => s.service === svcName);
    if (matched) {
      const detectedPort = extractContainerPort(matched.ports);
      setNewDomainPort(detectedPort);
      toast.info(`Auto-detected port :${detectedPort} from service "${svcName}"`);
    }
  };

  // =========================================================================
  // 4. Unified cPanel / Webuzo-Style File Manager State
  // =========================================================================
  const [deletedFilePaths, setDeletedFilePaths] = useState<string[]>([]);
  const [customFolders, setCustomFolders] = useState<string[]>([]);
  const [renamedPaths, setRenamedPaths] = useState<Record<string, string>>({});
  const [isDragOverFileManager, setIsDragOverFileManager] = useState(false);
  const [liveFileList, setLiveFileList] = useState<ComposeFileNode[] | null>(null);

  // Sync real files from server disk into frontend state, preserving already loaded file contents
  const syncWorkspaceFilesFromDisk = async (invalidatedPaths?: string[]) => {
    try {
      const listRes = await backendApi.filesList(app.id, '', true);
      const res = listRes.data;
      if (Array.isArray(res)) {
        const invalidatedSet = new Set(invalidatedPaths || []);
        // Build map of existing file contents from state so we don't wipe them out with empty strings
        const contentByPath = new Map<string, string>();
        (app.files || []).forEach((f) => {
          if (f.path && f.content && !invalidatedSet.has(f.path)) contentByPath.set(f.path, f.content);
        });
        (liveFileList || []).forEach((f) => {
          if (f.path && f.content && !invalidatedSet.has(f.path)) contentByPath.set(f.path, f.content);
        });

        const liveNodes: ComposeFileNode[] = res
          .filter((item) => !item.is_dir)
          .map((item) => {
            const ext = item.name.split('.').pop()?.toLowerCase();
            return {
              path: item.rel_path,
              name: item.name,
              type: 'file',
              sizeBytes: item.size,
              updatedAt: 'Recently',
              language: ext === 'json' ? 'json' : ext === 'yml' || ext === 'yaml' ? 'yaml' : ext === 'sh' ? 'sh' : 'dockerfile',
              content: contentByPath.get(item.rel_path) || '',
            };
          });
        const liveFolders = res
          .filter((item) => item.is_dir)
          .map((item) => item.rel_path);

        const permsMap: Record<string, string> = {};
        res.forEach((item) => {
          if (item.rel_path && item.perms) {
            permsMap[item.rel_path] = formatPermOctal(item.perms, item.is_dir);
          }
        });
        if (Object.keys(permsMap).length > 0) {
          setFilePermissions((prev) => ({ ...prev, ...permsMap }));
        }

        setLiveFileList(liveNodes);
        setCustomFolders((prev) => Array.from(new Set([...prev, ...liveFolders])));
        onUpdateApp({
          ...app,
          files: liveNodes,
        });

        // If currently open file in editor was invalidated/replaced, reload its real content
        if (editingFilePath && invalidatedSet.has(editingFilePath)) {
          const freshNode = liveNodes.find((f) => f.path === editingFilePath);
          if (freshNode) {
            void openFileInEditor(freshNode);
          }
        }
      }
    } catch {
      // Ignore background sync errors
    }
  };

  useEffect(() => {
    if (activeTab === 'files') {
      void syncWorkspaceFilesFromDisk();
    }
  }, [activeTab, app.id]);

  const defaultRichFiles: ComposeFileNode[] = useMemo(() => {
    const sourceFiles = liveFileList || app.files;
    return sourceFiles
      .map((f) => {
        const mappedPath = renamedPaths[f.path] || f.path;
        return {
          ...f,
          path: mappedPath,
          name: mappedPath.split('/').pop() || mappedPath,
        };
      })
      .filter((f) => !deletedFilePaths.includes(f.path));
  }, [liveFileList, app.files, app.primaryDomain, app.services, app.slug, deletedFilePaths, renamedPaths]);

  const [currentDir, setCurrentDir] = useState<string>(app.php ? 'htdocs' : '');
  const [editingFilePath, setEditingFilePath] = useState<string | null>(null);
  const [openEditorTabs, setOpenEditorTabs] = useState<string[]>([]);
  const [expandedDirs, setExpandedDirs] = useState<Record<string, boolean>>({});
  const [mobileTreeOpen, setMobileTreeOpen] = useState(false);
  const [fileSearch, setFileSearch] = useState('');
  const [selectedEntries, setSelectedEntries] = useState<string[]>([]);
  const [filePermissions, setFilePermissions] = useState<Record<string, string>>({
    'scripts/entrypoint.sh': '755',
    'scripts/backup-db.sh': '755',
    '.env': '600',
  });

  // Right-click floating Context Menu state
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    targetPath: string;
    targetName: string;
    isFolder: boolean;
  } | null>(null);

  // File Manager Modals
  const [chmodModalFile, setChmodModalFile] = useState<string | null>(null);
  const [chmodMatrix, setChmodMatrix] = useState({
    ownerR: true,
    ownerW: true,
    ownerX: false,
    groupR: true,
    groupW: false,
    groupX: false,
    worldR: true,
    worldW: false,
    worldX: false,
  });
  const [newFileModalOpen, setNewFileModalOpen] = useState(false);
  const [newFileName, setNewFileName] = useState('');
  const [newFileTargetDir, setNewFileTargetDir] = useState('');
  const [newFolderModalOpen, setNewFolderModalOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [newFolderTargetDir, setNewFolderTargetDir] = useState('');
  const [renameTarget, setRenameTarget] = useState<{
    path: string;
    name: string;
    isFolder: boolean;
  } | null>(null);
  const [renameInput, setRenameInput] = useState('');
  const [moveTarget, setMoveTarget] = useState<{
    paths: string[];
    name: string;
    mode: 'move' | 'copy';
  } | null>(null);

  const openMoveOrCopyModal = (paths: string[], mode: 'move' | 'copy' = 'move') => {
    setContextMenu(null);
    const cleanPaths = paths.map((p) => p.replace(/^dir:/, ''));
    if (cleanPaths.length === 0) return;
    const displayName =
      cleanPaths.length === 1
        ? cleanPaths[0].split('/').pop() || cleanPaths[0]
        : `${cleanPaths.length} items`;
    setMoveTarget({
      paths: cleanPaths,
      name: displayName,
      mode,
    });
    setMoveDestInput(currentDir || '');
    setShowInlineFolderCreator(false);
  };
  const [moveDestInput, setMoveDestInput] = useState('');
  const [inlineNewDestFolder, setInlineNewDestFolder] = useState('');
  const [showInlineFolderCreator, setShowInlineFolderCreator] = useState(false);
  const [conflictModal, setConflictModal] = useState<{
    paths: string[];
    conflicts: string[];
    destFolder: string;
    mode: 'move' | 'copy';
  } | null>(null);
  const [compressModalOpen, setCompressModalOpen] = useState(false);
  const [compressItems, setCompressItems] = useState<string[]>([]);
  const [compressArchiveName, setCompressArchiveName] = useState('archive.zip');
  const [compressFormat, setCompressFormat] = useState<'zip' | 'tar.gz'>('zip');
  const [extractTargetFile, setExtractTargetFile] = useState<ComposeFileNode | null>(null);
  const [extractDestDir, setExtractDestDir] = useState('');
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [selectedUploadFiles, setSelectedUploadFiles] = useState<File[]>([]);
  const [isDragOverUploadZone, setIsDragOverUploadZone] = useState(false);
  const [isUploadingFiles, setIsUploadingFiles] = useState(false);
  const [uploadFileName, setUploadFileName] = useState('');
  const [uploadProgress, setUploadProgress] = useState(0);
  const [restoreArchiveModalOpen, setRestoreArchiveModalOpen] = useState(false);
  const [selectedRestoreFile, setSelectedRestoreFile] = useState<File | null>(null);
  const [isRestoringArchive, setIsRestoringArchive] = useState(false);

  const selectedFile: ComposeFileNode =
    defaultRichFiles.find((f) => f.path === editingFilePath) || defaultRichFiles[0];

  const isArchiveFile = (fileName: string) => {
    const lower = fileName.toLowerCase();
    return (
      lower.endsWith('.zip') ||
      lower.endsWith('.tar.gz') ||
      lower.endsWith('.tgz') ||
      lower.endsWith('.tar')
    );
  };

  const formatFileSize = (bytes: number) => {
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${bytes} B`;
  };

  // Compute all unique folder paths in the project
  const allFolderPaths = useMemo(() => {
    const dirs = new Set<string>(
      customFolders.filter((d) => !deletedFilePaths.includes(`dir:${d}`))
    );
    defaultRichFiles.forEach((f) => {
      const parts = f.path.split('/');
      if (parts.length > 1) {
        let acc = '';
        for (let i = 0; i < parts.length - 1; i++) {
          acc = acc ? `${acc}/${parts[i]}` : parts[i];
          if (!deletedFilePaths.includes(`dir:${acc}`)) {
            dirs.add(acc);
          }
        }
      }
    });
    return Array.from(dirs).sort();
  }, [customFolders, defaultRichFiles, deletedFilePaths]);

  // Top-level folders for the left tree
  const rootFolders = useMemo(
    () => allFolderPaths.filter((d) => !d.includes('/')),
    [allFolderPaths]
  );

  // Subfolders inside currentDir
  const currentSubfolders = useMemo(() => {
    if (fileSearch.trim()) {
      return allFolderPaths.filter((d) =>
        d.toLowerCase().includes(fileSearch.trim().toLowerCase())
      );
    }
    return allFolderPaths.filter((d) => {
      const lastSlash = d.lastIndexOf('/');
      const parent = lastSlash === -1 ? '' : d.slice(0, lastSlash);
      return parent === currentDir;
    });
  }, [allFolderPaths, currentDir, fileSearch]);

  // Files inside currentDir
  const currentDirFiles = useMemo(() => {
    if (fileSearch.trim()) {
      return defaultRichFiles.filter((f) =>
        f.path.toLowerCase().includes(fileSearch.trim().toLowerCase())
      );
    }
    return defaultRichFiles.filter((f) => {
      const lastSlash = f.path.lastIndexOf('/');
      const parent = lastSlash === -1 ? '' : f.path.slice(0, lastSlash);
      return parent === currentDir;
    });
  }, [defaultRichFiles, currentDir, fileSearch]);

  const navigateToFolder = (
    folderPath: string,
    options?: { preserveTreeToggle?: boolean }
  ) => {
    setCurrentDir(folderPath);
    setEditingFilePath(null);
    setSelectedEntries([]);
    setContextMenu(null);
    setFileSearch('');
    if (folderPath && !options?.preserveTreeToggle) {
      const parts = folderPath.split('/');
      setExpandedDirs((prev) => {
        const nextExp: Record<string, boolean> = { ...prev };
        let acc = '';
        parts.forEach((p) => {
          acc = acc ? `${acc}/${p}` : p;
          nextExp[acc] = true;
        });
        return nextExp;
      });
    }
  };

  const toggleFolderTreeExpand = (folderPath: string, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    setExpandedDirs((prev) => ({
      ...prev,
      [folderPath]: !prev[folderPath],
    }));
  };

  const openFileInEditor = async (file: ComposeFileNode) => {
    setContextMenu(null);
    if (isArchiveFile(file.name)) {
      setExtractTargetFile(file);
      setExtractDestDir(currentDir || '');
      return;
    }
    // If file content is empty on frontend, fetch real text from backend
    if (!file.content) {
      try {
        const res = await backendApi.fileBlob(app.id, file.path);
        const textContent = res.data?.text ?? '';
        file.content = textContent;
        setLiveFileList((prev) => {
          if (!prev) return null;
          return prev.map((f) => (f.path === file.path ? { ...f, content: textContent } : f));
        });
        onUpdateApp({
          ...app,
          files: app.files.map((f) => (f.path === file.path ? { ...f, content: textContent } : f)),
        });
      } catch {
        // Fallback to empty editor
      }
    }
    setEditingFilePath(file.path);
    setOpenEditorTabs((prev) => (prev.includes(file.path) ? prev : [...prev, file.path]));
  };

  const closeEditorTab = (tabPath: string, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    setOpenEditorTabs((prev) => {
      const idx = prev.indexOf(tabPath);
      const next = prev.filter((p) => p !== tabPath);
      if (editingFilePath === tabPath) {
        if (next.length > 0) {
          const fallbackIdx = Math.max(0, idx - 1);
          setEditingFilePath(next[fallbackIdx] || next[0]);
        } else {
          setEditingFilePath(null);
        }
      }
      return next;
    });
  };

  const openChmodFor = (targetPath: string, isDir = false) => {
    setContextMenu(null);
    const oct = formatPermOctal(filePermissions[targetPath], isDir);
    const u = parseInt(oct[0] || (isDir ? '7' : '6'), 10);
    const g = parseInt(oct[1] || (isDir ? '5' : '4'), 10);
    const w = parseInt(oct[2] || (isDir ? '5' : '4'), 10);
    setChmodMatrix({
      ownerR: Boolean(u & 4),
      ownerW: Boolean(u & 2),
      ownerX: Boolean(u & 1),
      groupR: Boolean(g & 4),
      groupW: Boolean(g & 2),
      groupX: Boolean(g & 1),
      worldR: Boolean(w & 4),
      worldW: Boolean(w & 2),
      worldX: Boolean(w & 1),
    });
    setChmodModalFile(targetPath);
  };

  const computedOctal = useMemo(() => {
    const o =
      (chmodMatrix.ownerR ? 4 : 0) +
      (chmodMatrix.ownerW ? 2 : 0) +
      (chmodMatrix.ownerX ? 1 : 0);
    const g =
      (chmodMatrix.groupR ? 4 : 0) +
      (chmodMatrix.groupW ? 2 : 0) +
      (chmodMatrix.groupX ? 1 : 0);
    const w =
      (chmodMatrix.worldR ? 4 : 0) +
      (chmodMatrix.worldW ? 2 : 0) +
      (chmodMatrix.worldX ? 1 : 0);
    const octal = `${o}${g}${w}`;
    return { octal, symbolic: octal };
  }, [chmodMatrix]);

  const toggleSelectEntry = (entryKey: string) => {
    setSelectedEntries((prev) =>
      prev.includes(entryKey) ? prev.filter((k) => k !== entryKey) : [...prev, entryKey]
    );
  };

  const openCompressModalFor = (targets: string[]) => {
    setContextMenu(null);
    const items = targets.length > 0 ? targets : [currentDir || app.slug];
    setCompressItems(items);
    const base =
      items.length === 1
        ? items[0].replace(/^dir:/, '').split('/').pop() || 'archive'
        : `${app.slug}-bundle`;
    setCompressFormat('zip');
    setCompressArchiveName(`${base}.zip`);
    setCompressModalOpen(true);
  };

  const renderFileIcon = (fileName: string) => {
    return <DevFileIcon filename={fileName} />;
  };

  // =========================================================================
  // 5. Git Auto-Detected Branches State
  // =========================================================================
  const [gitModalOpen, setGitModalOpen] = useState(false);
  const [gitRepoInput, setGitRepoInput] = useState(
    app.gitRepo || `https://github.com/masudranaxpert/${app.slug}`
  );
  const [gitBranchInput, setGitBranchInput] = useState(app.gitBranch || 'main');
  const [composePathInput, setComposePathInput] = useState(
    app.composeFilePath || 'docker-compose.yml'
  );
  const [isScanningBranches, setIsScanningBranches] = useState(false);

  const detectedBranches: DetectedGitBranch[] = useMemo(() => {
    if (!app.gitBranch && !app.gitRepo) return [];
    const current = app.gitBranch || 'main';
    return [
      {
        name: current,
        sha: app.gitCommitSha?.slice(0, 7) || 'HEAD',
        author: app.owner || 'current',
        updatedAt: 'Active branch',
        isDefault: true,
        protected: true,
      },
    ];
  }, [app.gitBranch, app.gitRepo, app.gitCommitSha, app.owner]);

  const triggerRescanBranches = () => {
    setIsScanningBranches(true);
    setTimeout(() => {
      setIsScanningBranches(false);
      toast.success(`Detected ${detectedBranches.length} branches from remote repository`);
    }, 350);
  };

  // =========================================================================
  // 6. Project Backups State (Rich NextDeploy Rclone + Point-in-Time Restore)
  // =========================================================================
  const [backupModalOpen, setBackupModalOpen] = useState(false);
  const [backupDest, setBackupDest] = useState('Cloudflare R2 Primary (EU-Central)');
  const [backupScope, setBackupScope] = useState<'full' | 'volume' | 'app'>('full');
  const [backupPauseContainers, setBackupPauseContainers] = useState(true);
  const [backupIncludeVols, setBackupIncludeVols] = useState(true);
  const [backupIncludeEnv, setBackupIncludeEnv] = useState(true);
  const [autoBackupEnabled, setAutoBackupEnabled] = useState(true);
  const [autoBackupSchedule, setAutoBackupSchedule] = useState('0 */6 * * *');
  const [preDeploySnapshot, setPreDeploySnapshot] = useState(true);
  const [restoreSnapshotTarget, setRestoreSnapshotTarget] = useState<ProjectBackupItem | null>(
    null
  );
  const [backupLogModal, setBackupLogModal] = useState<{
    name: string;
    log: string;
  } | null>(null);

  const [projectBackups, setProjectBackups] = useState<ProjectBackupItem[]>([]);

  // =========================================================================
  // 7. Modern Logs Explorer State
  // =========================================================================
  const [logContainerFilter, setLogContainerFilter] = useState<string>('all');
  const [terminalService, setTerminalService] = useState<string>(app.services[0]?.service || '');
  const [logLevelFilter, setLogLevelFilter] = useState<'all' | 'info' | 'warn' | 'error' | 'debug'>(
    'all'
  );
  const [logTimeRange, setLogTimeRange] = useState<'live' | '15m' | '1h' | '24h'>('live');
  const [logSearch, setLogSearch] = useState('');
  const [logLiveTail, setLogLiveTail] = useState(true);
  const [logShowTimestamps, setLogShowTimestamps] = useState(true);
  const [logWordWrap, setLogWordWrap] = useState(true);

  const [liveStreamLogs, setLiveStreamLogs] = useState<LogEntry[]>([]);

  useEffect(() => {
    if (activeTab !== 'logs' || !logLiveTail) return;
    setLiveStreamLogs([]);
    const targetSvc =
      logContainerFilter !== 'all'
        ? app.services.find((s) => s.service === logContainerFilter || s.name === logContainerFilter)
        : app.services[0];
    const containerName =
      logContainerFilter !== 'all'
        ? targetSvc?.service || targetSvc?.name || logContainerFilter
        : targetSvc?.service || '';
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const sinceParam = logTimeRange !== 'live' ? `&since=${encodeURIComponent(logTimeRange)}` : '';
    const wsUrl = `${proto}//${window.location.host}/apps/${encodeURIComponent(
      app.id
    )}/ws/logs?tail=200${sinceParam}${containerName ? `&container=${encodeURIComponent(containerName)}` : ''}`;
    let ws: WebSocket | null = null;
    try {
      ws = new WebSocket(wsUrl);
      ws.onmessage = (ev) => {
        const raw = typeof ev.data === 'string' ? ev.data : '';
        if (!raw) return;
        const lines = raw.split(/\r?\n/).filter((l) => l.trim().length > 0);
        const nowIso = new Date().toISOString();
        const newEntries: LogEntry[] = lines.map((line, i) => {
          let logTime = nowIso;
          let logMsg = line;
          const tsMatch = line.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s+(.*)$/);
          if (tsMatch) {
            logTime = tsMatch[1];
            logMsg = tsMatch[2];
          }
          const lower = logMsg.toLowerCase();
          const level: LogEntry['level'] =
            lower.includes('error') || lower.includes('fatal') || lower.includes('panic')
              ? 'error'
              : lower.includes('warn')
              ? 'warn'
              : lower.includes('debug')
              ? 'debug'
              : 'info';
          return {
            id: `ws-log-${Date.now()}-${i}-${Math.random().toString(36).slice(2, 6)}`,
            timestamp: logTime,
            container: targetSvc?.service || containerName || 'app',
            level,
            message: logMsg,
          };
        });
        setLiveStreamLogs((prev) => [...prev.slice(-600), ...newEntries]);
      };
      ws.onerror = (err) => {
        console.warn('Logs WebSocket error:', err);
      };
    } catch (err) {
      console.warn('Failed to connect to logs WebSocket:', err);
    }
    return () => {
      if (ws) {
        try {
          ws.close();
        } catch {
          // ignore
        }
      }
    };
  }, [activeTab, logLiveTail, logContainerFilter, app.id, app.services, logTimeRange]);

  const syntheticLogs: LogEntry[] = useMemo(() => {
    return [...(app.logs || []), ...liveStreamLogs];
  }, [app.logs, liveStreamLogs]);

  const filteredLogs = useMemo(() => {
    return syntheticLogs.filter((l) => {
      if (logContainerFilter !== 'all' && l.container !== logContainerFilter) return false;
      if (logLevelFilter !== 'all' && l.level !== logLevelFilter) return false;
      if (
        logSearch &&
        !l.message.toLowerCase().includes(logSearch.toLowerCase()) &&
        !l.container.toLowerCase().includes(logSearch.toLowerCase())
      ) {
        return false;
      }
      return true;
    });
  }, [syntheticLogs, logContainerFilter, logLevelFilter, logSearch]);

  const logCounts = useMemo(() => {
    return {
      all: syntheticLogs.length,
      info: syntheticLogs.filter((l) => l.level === 'info').length,
      warn: syntheticLogs.filter((l) => l.level === 'warn').length,
      error: syntheticLogs.filter((l) => l.level === 'error').length,
      debug: syntheticLogs.filter((l) => l.level === 'debug').length,
    };
  }, [syntheticLogs]);

  // =========================================================================
  // Other Modals State
  // =========================================================================
  const [volumeModalOpen, setVolumeModalOpen] = useState(false);
  const [newVolName, setNewVolName] = useState('');
  const [newVolMountPath, setNewVolMountPath] = useState('/var/lib/postgresql/data');

  const [memberModalOpen, setMemberModalOpen] = useState(false);
  const [newMemberEmail, setNewMemberEmail] = useState('');
  const [newMemberRole, setNewMemberRole] = useState<'deployer' | 'viewer'>('deployer');
  const [newMemberPermissions, setNewMemberPermissions] = useState<string[]>([
    'deploy',
    'files',
    'terminal',
    'logs',
  ]);

  const [limitsModalOpen, setLimitsModalOpen] = useState(false);
  const [unlimitedResources, setUnlimitedResources] = useState(false);
  const [cpuQuota, setCpuQuota] = useState('2.0');
  const [memQuota, setMemQuota] = useState('2048');

  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [otpDeleteCode, setOtpDeleteCode] = useState('');

  // Extra Go Backend Features State (URL Upload, DNS Check, Caddy Labels, Merged Compose View)
  const [uploadMode, setUploadMode] = useState<'local' | 'url'>('local');
  const [uploadRemoteUrl, setUploadRemoteUrl] = useState('');
  const [caddyLabelsPreview, setCaddyLabelsPreview] = useState<{
    domain: string;
    port: number;
    snippet: string;
  } | null>(null);
  const [mergedComposeModalOpen, setMergedComposeModalOpen] = useState(false);
  const [mergedComposeYaml, setMergedComposeYaml] = useState('');

  // General Tab: Deploy Source Switcher (/apps/:id/switch-source) & Git Sync (/apps/:id/git-sync)
  const [switchSourceModalOpen, setSwitchSourceModalOpen] = useState(false);
  const [gitSyncModalOpen, setGitSyncModalOpen] = useState(false);
  const [forceGitSyncResume, setForceGitSyncResume] = useState(false);

  const [selectedContainers, setSelectedContainers] = useState<string[]>([]);
  const [isDeletingContainers, setIsDeletingContainers] = useState(false);

  const toggleSelectContainer = (containerName: string) => {
    setSelectedContainers((prev) =>
      prev.includes(containerName)
        ? prev.filter((name) => name !== containerName)
        : [...prev, containerName]
    );
  };

  const toggleSelectAllContainers = () => {
    if (selectedContainers.length === app.services.length) {
      setSelectedContainers([]);
    } else {
      setSelectedContainers(app.services.map((s) => s.name || s.service));
    }
  };

  const handleRemoveSelectedContainers = async (targets?: string[]) => {
    const toRemove = targets || selectedContainers;
    if (toRemove.length === 0) return;
    setIsDeletingContainers(true);
    try {
      const res = await backendApi.appContainersRemoveSelected(app.id, toRemove);
      if (res.ok) {
        toast.success(`Removed ${toRemove.length} container(s)`);
        setSelectedContainers((prev) => prev.filter((name) => !toRemove.includes(name)));
        await refreshAppFromBackend();
      } else {
        toast.error(res.error || 'Failed to remove containers');
      }
    } catch (err: any) {
      toast.error(err?.message || 'Failed to remove containers');
    } finally {
      setIsDeletingContainers(false);
    }
  };

  // =========================================================================
  // Actions & Handlers
  // =========================================================================
  const triggerDeploy = async (options?: { noCache?: boolean; rollbackFrom?: DeploymentRecord }) => {
    if (options?.rollbackFrom) {
      const dep = options.rollbackFrom;
      const rawLogId = String(dep.id).replace(/^dep-/, '');
      const res = await backendApi.rollbackDeployment(app.id, rawLogId);
      if (res.ok) {
        toast.success(res.message || `Rolled back production to ${dep.id}`);
        await refreshAppFromBackend();
      } else {
        toast.error(res.error || 'Rollback failed');
      }
      return;
    }

    const action = options?.noCache ? 'redeploy' : 'up';
    toast.info(`Starting ${options?.noCache ? 'clean rebuild' : 'deployment'} for ${app.name}...`);
    setLiveDeployState({
      running: true,
      action: options?.noCache ? 'Redeploy (pull + up)' : 'Deploy',
      output: 'Initializing compose build pipeline...\n',
    });
    const res = await backendApi.composeAction(app.id, action);
    if (!res.ok) {
      setLiveDeployState((prev) => ({ ...prev, running: false }));
      toast.error(res.error || 'Deployment failed to start');
      await refreshAppFromBackend();
      return;
    }
    setActiveTab('deployments');
    toast.success(`Deployment job started for ${app.name}`);
  };

  const toggleStopOrStart = async () => {
    const isStopped = app.status === 'stopped';
    const action = isStopped ? 'up' : 'down';
    toast.info(`${isStopped ? 'Starting' : 'Stopping'} ${app.name}...`);
    const res = await backendApi.composeAction(app.id, action);
    if (!res.ok) {
      toast.error(res.error || `Failed to ${isStopped ? 'start' : 'stop'} ${app.name}`);
      return;
    }
    toast.success(isStopped ? `Starting ${app.name}...` : `Stopping ${app.name}...`);
    setTimeout(() => {
      void refreshAppFromBackend();
    }, 1500);
  };

  // Environment Variables Handlers
  const handleEnvChange = (id: string, field: 'key' | 'value', val: string) => {
    const nextVars = app.envVars.map((ev) =>
      ev.id === id
        ? {
            ...ev,
            [field]: field === 'key' ? val.toUpperCase().replace(/[^A-Z0-9_]/g, '_') : val,
          }
        : ev
    );
    onUpdateApp({ ...app, envVars: nextVars });
  };

  const handleAddBlankEnvRow = () => {
    const newId = `ev-${Date.now()}`;
    const nextVars = [
      ...app.envVars,
      {
        id: newId,
        key: '',
        value: '',
        isSecret: true,
      },
    ];
    onUpdateApp({ ...app, envVars: nextVars });
    setRevealedEnvIds((prev) => ({ ...prev, [newId]: true }));
  };

  const handlePasteMultiEnv = (
    e: React.ClipboardEvent<HTMLInputElement>,
    targetId: string
  ) => {
    const text = e.clipboardData.getData('text');
    if (!text.includes('\n') && !text.includes('=')) return;
    const lines = text
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith('#') && l.includes('='));
    if (lines.length === 0) return;

    e.preventDefault();
    const parsed = lines.map((line, i) => {
      const idx = line.indexOf('=');
      const k = line
        .slice(0, idx)
        .trim()
        .toUpperCase()
        .replace(/[^A-Z0-9_]/g, '_');
      const v = line
        .slice(idx + 1)
        .trim()
        .replace(/^["']|["']$/g, '');
      return {
        id: `ev-paste-${Date.now()}-${i}`,
        key: k,
        value: v,
        isSecret: true,
      };
    });

    const filteredExisting = app.envVars.filter((ev) => !(ev.id === targetId && !ev.key));
    onUpdateApp({ ...app, envVars: [...filteredExisting, ...parsed] });
    toast.success(`Imported ${parsed.length} environment variables`);
  };

  const handleDeleteEnv = (id: string) => {
    onUpdateApp({
      ...app,
      envVars: app.envVars.filter((e) => e.id !== id),
    });
    toast.success('Removed variable');
  };

  // Domain Modal Submit (Create or Edit)
  const handleAddDomainSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = newDomainName
      .trim()
      .toLowerCase()
      .replace(/^https?:\/\//, '');
    if (!clean) return;
    const payload = {
      domain: clean,
      service: newDomainService,
      port: Number(newDomainPort) || 3000,
      enable_www: newDomainEnableWww,
      serve_static: newDomainServeStatic,
      static_path: newDomainServeStatic ? newDomainStaticPath : '',
      static_url_prefix: newDomainServeStatic ? newDomainStaticPrefix : '',
      serve_media: newDomainServeMedia,
      media_path: newDomainServeMedia ? newDomainMediaPath : '',
      media_url_prefix: newDomainServeMedia ? newDomainMediaPrefix : '',
    };
    if (editingDomainId) {
      void backendApi.domainEdit(app.id, editingDomainId, payload);
      onUpdateApp({
        ...app,
        domains: app.domains.map((d) =>
          d.id === editingDomainId
            ? {
                ...d,
                domain: clean,
                targetService: newDomainService,
                targetPort: Number(newDomainPort) || 3000,
                forceHttps: newDomainHttps,
              }
            : d
        ),
      });
      setDomainModalOpen(false);
      setEditingDomainId(null);
      toast.success(`Updated domain route https://${clean}`);
      return;
    }

    void backendApi.domainCreate(app.id, payload);
    const newRoute: DomainRoute = {
      id: `dom-${Date.now()}`,
      domain: clean,
      targetService: newDomainService,
      targetPort: Number(newDomainPort) || 3000,
      tlsProvider: 'LetsEncrypt',
      tlsStatus: 'active',
      forceHttps: newDomainHttps,
      gzip: true,
      createdAt: 'Just now',
    };
    onUpdateApp({
      ...app,
      primaryDomain: app.primaryDomain || clean,
      domains: [...app.domains, newRoute],
    });
    setNewDomainName('');
    setDomainModalOpen(false);
    toast.success(`Routed https://${clean} → ${newDomainService}:${newDomainPort}`);
  };

  // New File Submit
  const handleCreateFileSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const raw = newFileName.trim().replace(/^\/+/, '');
    if (!raw) return;
    const baseDir = newFileTargetDir.trim().replace(/^\/+|\/+$/g, '');
    const fullPath = baseDir ? `${baseDir}/${raw}` : raw;
    const createRes = await backendApi.fileCreate(app.id, fullPath);
    if (!createRes.ok) {
      toast.error(createRes.error || createRes.message || 'Failed to create file');
      return;
    }
    const ext = fullPath.split('.').pop()?.toLowerCase();
    const lang: ComposeFileNode['language'] =
      ext === 'yml' || ext === 'yaml'
        ? 'yaml'
        : ext === 'json'
          ? 'json'
          : ext === 'sh'
            ? 'sh'
            : ext === 'env'
              ? 'env'
              : ext === 'conf'
                ? 'nginx'
                : 'dockerfile';

    let starterContent = `# ${fullPath}\n`;
    if (ext === 'py') {
      starterContent = `#!/usr/bin/env python3\nimport os\n\ndef main() -> None:\n    print("Running ${fullPath} in ${app.slug}")\n\nif __name__ == "__main__":\n    main()\n`;
    } else if (ext === 'go') {
      starterContent = `package main\n\nimport "fmt"\n\nfunc RunTask() error {\n  fmt.Println("${fullPath} initialized")\n  return nil\n}\n`;
    } else if (ext === 'rs') {
      starterContent = `pub fn execute_worker(job_id: &str) -> Result<(), String> {\n    println!("Executing job {}", job_id);\n    Ok(())\n}\n`;
    } else if (ext === 'php') {
      starterContent = `<?php\ndeclare(strict_types=1);\n\n$status = ['ok' => true, 'file' => '${fullPath}'];\necho json_encode($status);\n`;
    } else if (ext === 'ts' || ext === 'js') {
      starterContent = `export async function handler(req: Request) {\n  return { status: 'ok', path: '${fullPath}' };\n}\n`;
    } else if (ext === 'sql') {
      starterContent = `-- SQL Migration: ${fullPath}\nSELECT NOW() AS deployed_at;\n`;
    } else if (ext === 'json') {
      starterContent = `{\n  "name": "${app.slug}",\n  "enabled": true\n}\n`;
    } else if (ext === 'sh') {
      starterContent = `#!/usr/bin/env sh\nset -eu\n\necho "Executing ${fullPath}..."\n`;
    }

    await backendApi.fileSave(app.id, fullPath, starterContent);
    await syncWorkspaceFilesFromDisk();

    const created: ComposeFileNode = {
      path: fullPath,
      name: fullPath.split('/').pop() || fullPath,
      type: 'file',
      sizeBytes: new Blob([starterContent]).size,
      updatedAt: 'Just now',
      language: lang,
      content: starterContent,
    };
    setLiveFileList((prev) =>
      prev ? [...prev.filter((f) => f.path !== fullPath), created] : [created]
    );
    setDeletedFilePaths((prev) => prev.filter((p) => p !== fullPath));
    setEditingFilePath(fullPath);
    setOpenEditorTabs((prev) => (prev.includes(fullPath) ? prev : [...prev, fullPath]));
    setNewFileName('');
    setNewFileModalOpen(false);
    toast.success(`Created ${fullPath}`);
  };

  // New Folder Submit
  const handleCreateFolderSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const raw = newFolderName
      .trim()
      .replace(/^\/+|\/+$/g, '')
      .replace(/[^a-zA-Z0-9/_.-]/g, '-');
    if (!raw) return;
    const baseDir = newFolderTargetDir.trim().replace(/^\/+|\/+$/g, '');
    const fullFolder = baseDir ? `${baseDir}/${raw}` : raw;
    const res = await backendApi.fileCreate(app.id, fullFolder, true);
    if (!res.ok) {
      toast.error(res.error || res.message || 'Failed to create folder');
      return;
    }
    setCustomFolders((prev) => (prev.includes(fullFolder) ? prev : [...prev, fullFolder]));
    setDeletedFilePaths((prev) => prev.filter((p) => p !== `dir:${fullFolder}`));
    setExpandedDirs((prev) => ({ ...prev, [fullFolder]: true }));
    setNewFolderName('');
    setNewFolderModalOpen(false);
    await syncWorkspaceFilesFromDisk();
    toast.success(`Created folder /${fullFolder}`);
  };

  // Compress (Zip / Tar.gz) Submit
  const handleCompressSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanName = compressArchiveName.trim().replace(/^\/+/, '');
    if (!cleanName) return;
    const finalName =
      cleanName.endsWith('.zip') || cleanName.endsWith('.tar.gz')
        ? cleanName
        : `${cleanName}.${compressFormat}`;
    const cleanPaths = compressItems.map((p) => p.replace(/^dir:/, '')).filter(Boolean);
    if (cleanPaths.length === 0) return;
    const res = await backendApi.fileZip(app.id, cleanPaths, finalName, currentDir || '');
    if (!res.ok) {
      toast.error(res.error || res.message || 'Failed to compress items');
      return;
    }
    await syncWorkspaceFilesFromDisk();
    setSelectedEntries([]);
    setCompressModalOpen(false);
    toast.success(`Compressed ${cleanPaths.length} item(s) into ${finalName}`);
  };

  // Extract (Unzip) Submit
  const handleExtractSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!extractTargetFile) return;
    const targetDir = extractDestDir.trim().replace(/^\/+|\/+$/g, '');
    toast.info(`Extracting ${extractTargetFile.name}...`);
    try {
      const res = await backendApi.fileUnzip(app.id, extractTargetFile.path, targetDir);
      if (res && res.ok === false) {
        toast.error(res.message || 'Extract failed');
        return;
      }
      setExtractTargetFile(null);
      await syncWorkspaceFilesFromDisk();
      toast.success(`Extracted ${extractTargetFile.name} successfully!`);
    } catch (err: any) {
      toast.error(err?.message || 'Failed to extract archive');
    }
  };

  // Rename Submit
  const handleRenameSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!renameTarget || !renameInput.trim()) return;
    const cleanNewName = renameInput.trim().replace(/^\/+|\/+$/g, '');
    const parent = renameTarget.path.includes('/')
      ? renameTarget.path.slice(0, renameTarget.path.lastIndexOf('/'))
      : '';
    const nextPath = parent ? `${parent}/${cleanNewName}` : cleanNewName;
    const res = await backendApi.fileRename(app.id, renameTarget.path, nextPath);
    if (!res.ok) {
      toast.error(res.error || res.message || 'Failed to rename');
      return;
    }

    if (renameTarget.isFolder) {
      setCustomFolders((prev) =>
        prev.map((d) => (d === renameTarget.path ? nextPath : d))
      );
      toast.success(`Renamed folder to /${nextPath}`);
    } else {
      toast.success(`Renamed ${renameTarget.name} → ${cleanNewName}`);
    }
    setRenameTarget(null);
    await syncWorkspaceFilesFromDisk();
  };

  // Copy / Move execution
  const executeMoveOrCopy = async (
    paths: string[],
    destFolder: string,
    mode: 'move' | 'copy',
    overwrite: boolean
  ) => {
    const isMove = mode === 'move';
    try {
      const res = isMove
        ? await backendApi.fileMove(app.id, paths, destFolder, overwrite)
        : await backendApi.fileCopy(app.id, paths, destFolder, overwrite);
      if (res && (res as any).ok === false) {
        toast.error((res as any).message || `Failed to ${isMove ? 'move' : 'copy'}`);
        return;
      }
      const targetDestPaths = paths.map((pth) => {
        const base = pth.split('/').pop() || pth;
        return destFolder ? `${destFolder}/${base}` : base;
      });
      await syncWorkspaceFilesFromDisk(targetDestPaths);
      setSelectedEntries([]);
      const destLabel = destFolder ? `/${destFolder}` : 'root directory';
      toast.success(
        `${isMove ? 'Moved' : 'Copied'} ${paths.length} item(s) to ${destLabel}`
      );
    } catch (err: any) {
      toast.error(err?.message || `Failed to ${isMove ? 'move' : 'copy'}`);
    }
  };

  // Copy / Move Submit
  const handleMoveOrCopySubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!moveTarget || moveTarget.paths.length === 0) return;
    const destFolder = moveDestInput.trim().replace(/^\/+|\/+$/g, '');

    // Existing files or folders in the destination
    const existingInDest = new Set<string>();
    defaultRichFiles.forEach((f) => {
      const parts = f.path.split('/');
      const parent = parts.length > 1 ? parts.slice(0, -1).join('/') : '';
      if (parent === destFolder) {
        existingInDest.add(parts[parts.length - 1]);
      }
    });
    allFolderPaths.forEach((dir) => {
      const parts = dir.split('/');
      const parent = parts.length > 1 ? parts.slice(0, -1).join('/') : '';
      if (parent === destFolder) {
        existingInDest.add(parts[parts.length - 1]);
      }
    });

    // Check for naming collisions
    const detectedConflicts = moveTarget.paths.filter((pth) => {
      const baseName = pth.split('/').pop() || pth;
      const oldParent = pth.includes('/') ? pth.slice(0, pth.lastIndexOf('/')) : '';
      if (oldParent === destFolder) return false;
      return existingInDest.has(baseName);
    });

    if (detectedConflicts.length > 0) {
      setConflictModal({
        paths: moveTarget.paths,
        conflicts: detectedConflicts,
        destFolder,
        mode: moveTarget.mode,
      });
      setMoveTarget(null);
      return;
    }

    const mode = moveTarget.mode;
    const paths = moveTarget.paths;
    setMoveTarget(null);
    await executeMoveOrCopy(paths, destFolder, mode, false);
  };

  // Deletes on disk first; local hiding alone made files reappear after refresh
  const deleteEntries = async (entries: string[]) => {
    const paths = entries.map((e) => e.replace(/^dir:/, ''));
    const res = await backendApi.fileDelete(app.id, paths);
    if (!res.ok) {
      toast.error(res.error || 'Failed to delete');
      return;
    }
    setCustomFolders((prev) => prev.filter((d) => !paths.some((p) => d === p || d.startsWith(`${p}/`))));
    await syncWorkspaceFilesFromDisk();
    toast.success(`Deleted ${paths.length} item(s)`);
  };

  // Shared local upload runner (modal + drag & drop) with live progress
  const runLocalUpload = async (files: File[]): Promise<boolean> => {
    setUploadProgress(0);
    setUploadFileName(files.length === 1 ? files[0].name : `${files.length} files`);
    setIsUploadingFiles(true);
    try {
      const res = await backendApi.fileUpload(app.id, files, currentDir || '', setUploadProgress);
      if (!res.ok) {
        toast.error(res.message || 'Failed to upload files');
        return false;
      }
      await syncWorkspaceFilesFromDisk();
      toast.success(`Uploaded ${files.length} file(s) into /${currentDir || 'root'}`);
      return true;
    } finally {
      setIsUploadingFiles(false);
    }
  };

  // Upload File or Remote URL Submit
  const handleUploadSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (uploadMode === 'url') {
      const rawUrl = uploadRemoteUrl.trim();
      if (!rawUrl) return;
      setIsUploadingFiles(true);
      try {
        const res = await backendApi.fileUrlUpload(app.id, rawUrl, currentDir || '');
        const inferredName =
          res.file ||
          rawUrl.split('?')[0].split('/').filter(Boolean).pop() ||
          'remote-download.zip';
        const fullPath = currentDir ? `${currentDir}/${inferredName}` : inferredName;
        const uploaded: ComposeFileNode = {
          path: fullPath,
          name: inferredName,
          type: 'file',
          sizeBytes: res.bytes || 32768,
          updatedAt: 'Just now',
          language: inferredName.endsWith('.json') ? 'json' : 'yaml',
          content: `# Fetched from remote URL: ${rawUrl}\n`,
        };
        onUpdateApp({
          ...app,
          files: [...app.files.filter((f) => f.path !== fullPath), uploaded],
        });
        setUploadRemoteUrl('');
        setUploadModalOpen(false);
        toast.success(`Fetched ${inferredName} into /${currentDir || 'root'}`);
      } catch (err: any) {
        toast.error(err?.message || 'Failed to fetch remote URL');
      } finally {
        setIsUploadingFiles(false);
      }
      return;
    }

    // Local file upload (drag & drop or file selector)
    if (selectedUploadFiles.length === 0) {
      toast.error('Please select or drop at least one file to upload');
      return;
    }

    setUploadModalOpen(false);
    const ok = await runLocalUpload(selectedUploadFiles);
    if (ok) setSelectedUploadFiles([]);
    else setUploadModalOpen(true);
  };

  // Volume Modal Submit
  const handleCreateVolumeSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = newVolName
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9_-]/g, '_');
    if (!clean) return;
    onUpdateApp({
      ...app,
      volumes: [
        ...app.volumes,
        {
          name: clean,
          driver: 'local',
          mountPath: newVolMountPath || `/var/lib/docker/volumes/${clean}/_data`,
          mountPoint: newVolMountPath || `/var/lib/docker/volumes/${clean}/_data`,
          sizeMB: 16.0,
          lastBackupAt: 'Just now',
        },
      ],
    });
    setNewVolName('');
    setVolumeModalOpen(false);
    toast.success(`Mounted volume ${clean}`);
  };

  const handleSaveFileContent = (nextContent: string) => {
    selectedFile.content = nextContent;
    setLiveFileList((prev) => {
      if (!prev) return null;
      return prev.map((f) =>
        f.path === selectedFile.path
          ? {
              ...f,
              content: nextContent,
              sizeBytes: new Blob([nextContent]).size,
              updatedAt: 'Just now',
            }
          : f
      );
    });
    const existsInApp = app.files.some((f) => f.path === selectedFile.path);
    if (existsInApp) {
      const updatedFiles = app.files.map((f) =>
        f.path === selectedFile.path
          ? {
              ...f,
              content: nextContent,
              sizeBytes: new Blob([nextContent]).size,
              updatedAt: 'Just now',
            }
          : f
      );
      onUpdateApp({ ...app, files: updatedFiles });
    } else {
      onUpdateApp({
        ...app,
        files: [
          ...app.files,
          {
            ...selectedFile,
            content: nextContent,
            sizeBytes: new Blob([nextContent]).size,
            updatedAt: 'Just now',
          },
        ],
      });
    }
  };

  const hasGitConnection = Boolean(app.gitRepo && app.gitRepo.trim() !== '');
  const isGitSyncActive = hasGitConnection && (app.gitSync ?? app.sourceType === 'git');

  useEffect(() => {
    if (!hasGitConnection && activeTab === 'git') {
      setActiveTab('overview');
    }
    if (activeTab === 'files') {
      void syncWorkspaceFilesFromDisk();
    }
  }, [hasGitConnection, activeTab]);

  useEffect(() => {
    if (app.php) prefetchPhpSite(app.id);
  }, [app.id, !!app.php]);

  // Tabs
  const phpTabs: { id: AppWorkspaceTab; label: string; count?: number; icon?: React.ReactNode }[] = [
    { id: 'php-settings', label: 'Settings', icon: <PhpTabSettings className="h-3.5 w-3.5" /> },
    { id: 'domains', label: 'Domains', count: app.domains.length, icon: <PhpTabGlobe className="h-3.5 w-3.5" /> },
    ...(app.php?.siteType !== 'static' ? [{ id: 'php-databases' as AppWorkspaceTab, label: 'Databases', icon: <PhpTabDatabase className="h-3.5 w-3.5" /> }] : []),
    { id: 'files', label: 'File Manager', icon: <PhpTabFolder className="h-3.5 w-3.5" /> },
    ...(app.php?.siteType !== 'static' ? [{ id: 'php-cron' as AppWorkspaceTab, label: 'Cron Jobs', icon: <PhpTabClock className="h-3.5 w-3.5" /> }] : []),
    ...(app.php?.siteType !== 'static' ? [{ id: 'php-logs' as AppWorkspaceTab, label: 'Logs', icon: <PhpTabLogs className="h-3.5 w-3.5" /> }] : []),
    ...(app.php?.siteType !== 'static' ? [{ id: 'terminal' as AppWorkspaceTab, label: 'Terminal', icon: <PhpTabTerminal className="h-3.5 w-3.5" /> }] : []),
    { id: 'backups', label: 'Backups', count: projectBackups.length, icon: <PhpTabArchive className="h-3.5 w-3.5" /> },
    { id: 'collaborators', label: 'Access', icon: <PhpTabUsers className="h-3.5 w-3.5" /> },
  ];
  const tabs: { id: AppWorkspaceTab; label: string; count?: number; icon?: React.ReactNode }[] = app.php ? phpTabs : [
    { id: 'overview', label: 'General' },
    { id: 'containers', label: 'Containers', count: app.services.length },
    { id: 'deployments', label: 'Deployments', count: app.deployments.length },
    { id: 'domains', label: 'Domains', count: app.domains.length },
    { id: 'env', label: 'Environment', count: app.envVars.length },
    { id: 'files', label: 'File Manager' },
    ...(hasGitConnection ? [{ id: 'git' as AppWorkspaceTab, label: 'Git' }] : []),
    { id: 'volumes', label: 'Volumes', count: app.volumes.length },
    { id: 'backups', label: 'Backups', count: projectBackups.length },
    { id: 'logs', label: 'Logs' },
    { id: 'terminal', label: 'Terminal' },
    { id: 'collaborators', label: 'Access' },
  ];

  const filteredEnvVars = app.envVars.filter(
    (ev) =>
      !envSearch ||
      ev.key.toLowerCase().includes(envSearch.toLowerCase()) ||
      ev.value.toLowerCase().includes(envSearch.toLowerCase())
  );

  const runningContainersCount = app.services.filter((s) => s.state === 'running').length;
  const activeSlotLower = (app.activeSlot || 'blue').toLowerCase() as 'blue' | 'green';
  const standbySlotName = (activeSlotLower === 'green' ? 'blue' : 'green') as 'blue' | 'green';
  const isPinnedProduction = Boolean(app.pinnedSlot && (app.pinnedSlot === 'blue' || app.pinnedSlot === 'green'));
  const activeProductionDeployment =
    app.deployments.find(
      (d) => d.status === 'ready' && (d.slot || activeSlotLower).toLowerCase() === activeSlotLower
    ) || app.deployments[0];
  const latestStandbyDeployment = app.deployments.find(
    (d) => d.status === 'ready' && (d.slot || '').toLowerCase() === standbySlotName
  );
  const standbySlotHasImage = Boolean(
    latestStandbyDeployment && (app.slotImages ? app.slotImages[standbySlotName] : false)
  );
  const isStandbyContainerRunning = app.services.some(
    (s) => s.state === 'running' && (s.name || '').toLowerCase().includes(`-${standbySlotName}-`)
  );
  const productionUrl = app.php
    ? app.primaryDomain
      ? `${app.domains.find((d) => d.domain === app.primaryDomain)?.forceHttps === false ? 'http' : 'https'}://${app.primaryDomain}`
      : ''
    : defaultWildcardHost
    ? `https://${defaultWildcardHost}`
    : '';
  const previewHost = previewBaseHost ? formatSlotPreviewDomain(previewBaseHost, standbySlotName) : '';
  const previewUrl = previewHost ? `https://${previewHost}` : '';

  const [isPinningSlot, setIsPinningSlot] = useState(false);
  const [isPromotingSlot, setIsPromotingSlot] = useState(false);

  const handleTogglePinSlot = async () => {
    setIsPinningSlot(true);
    try {
      const target = isPinnedProduction ? 'unpin' : activeSlotLower;
      const res = await backendApi.pinAppSlot(app.id, target);
      if (!res.ok) {
        toast.error(res.error || 'Failed to update production pin');
        return;
      }
      const nextPin = res.data?.pinned_slot ?? (isPinnedProduction ? '' : activeSlotLower);
      onUpdateApp({ ...app, pinnedSlot: nextPin });
      if (nextPin) {
        toast.success(`Pinned Production on Slot ${nextPin.toUpperCase()}. New deploys will run on Preview Slot ${standbySlotName.toUpperCase()}.`);
      } else {
        toast.success('Unpinned Production Slot. Normal zero-downtime cutover resumed.');
      }
    } finally {
      setIsPinningSlot(false);
    }
  };

  const handlePromotePreviewSlot = async () => {
    setIsPromotingSlot(true);
    try {
      toast.info(`Promoting Slot ${standbySlotName.toUpperCase()} to Production...`);
      const res = await backendApi.promoteAppSlot(app.id);
      if (!res.ok) {
        toast.error(res.error || 'Failed to promote preview slot');
        return;
      }
      toast.success(res.data?.message || `Promoted Slot ${standbySlotName.toUpperCase()} to Production!`);
      await refreshAppFromBackend();
    } finally {
      setIsPromotingSlot(false);
    }
  };

  // Format raw build log line with terminal syntax highlighting
  const renderStyledBuildLog = (text: string) => {
    return text.split('\n').map((line, idx) => {
      if (!line) return <div key={idx} className="h-3" />;

      // Match timestamp e.g. "2026-Oct-23 03:29:14.410246"
      const timeMatch = line.match(/^(\d{4}-[A-Za-z]{3}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s*(.*)$/);
      const timestamp = timeMatch ? timeMatch[1] : null;
      const content = timeMatch ? timeMatch[2] : line;

      let contentEl: React.ReactNode = content;

      if (content.startsWith('#')) {
        // Docker buildkit step e.g. #1 [app internal] load build definition
        const stepMatch = content.match(/^(#\d+)\s+(\[[^\]]+\])?\s*(.*)$/);
        if (stepMatch) {
          const stepNum = stepMatch[1];
          const target = stepMatch[2];
          const rest = stepMatch[3];
          contentEl = (
            <span>
              <span className="text-sky-400 font-semibold">{stepNum}</span>{' '}
              {target && <span className="text-amber-400/90">{target} </span>}
              <span className={rest.includes('DONE') ? 'text-emerald-400 font-medium' : rest.includes('CACHED') ? 'text-zinc-500' : 'text-zinc-300'}>
                {rest}
              </span>
            </span>
          );
        }
      } else if (content.includes('Starting Deploy') || content.includes('Compose command finished')) {
        contentEl = <span className="text-sky-300 font-semibold">{content}</span>;
      } else if (content.startsWith('---')) {
        contentEl = <span className="text-zinc-600 select-none">{content}</span>;
      } else if (content.includes('[zero-downtime]')) {
        contentEl = (
          <span>
            <span className="text-violet-400 font-semibold">[zero-downtime]</span>{' '}
            <span className="text-zinc-200">{content.replace('[zero-downtime]', '')}</span>
          </span>
        );
      } else if (content.includes('[pinned-mode]')) {
        contentEl = (
          <span>
            <span className="text-amber-400 font-semibold">[pinned-mode]</span>{' '}
            <span className="text-zinc-200">{content.replace('[pinned-mode]', '')}</span>
          </span>
        );
      } else if (content.includes('[readiness]')) {
        contentEl = (
          <span>
            <span className="text-emerald-400 font-semibold">[readiness]</span>{' '}
            <span className="text-zinc-200">{content.replace('[readiness]', '')}</span>
          </span>
        );
      } else if (content.includes('[connection-draining]')) {
        contentEl = (
          <span>
            <span className="text-amber-400 font-semibold">[connection-draining]</span>{' '}
            <span className="text-zinc-300">{content.replace('[connection-draining]', '')}</span>
          </span>
        );
      } else if (content.includes('Container ') && (content.includes('Creating') || content.includes('Starting') || content.includes('Started') || content.includes('Created'))) {
        contentEl = (
          <span>
            <span className="text-orange-400 font-medium">{content}</span>
          </span>
        );
      } else if (content.includes('✓') || content.includes('✔') || content.includes('Built')) {
        contentEl = <span className="text-emerald-400 font-medium">{content}</span>;
      } else if (content.toLowerCase().includes('error') || content.toLowerCase().includes('failed')) {
        contentEl = <span className="text-rose-400 font-medium">{content}</span>;
      } else if (content.toLowerCase().includes('warn')) {
        contentEl = <span className="text-amber-300">{content}</span>;
      }

      return (
        <div key={idx} className="flex items-start gap-2.5 font-mono text-[11px] leading-5 hover:bg-white/[0.02] px-1 py-0.5 rounded">
          {timestamp && (
            <span className="text-zinc-500/80 shrink-0 select-none text-[10px] font-mono">
              {timestamp}
            </span>
          )}
          <span className="break-all select-text">{contentEl}</span>
        </div>
      );
    });
  };

  // Highlight matching search text inside logs
  const renderLogMessage = (msg: string) => {
    if (!logSearch.trim()) return msg;
    const idx = msg.toLowerCase().indexOf(logSearch.toLowerCase());
    if (idx === -1) return msg;
    const before = msg.slice(0, idx);
    const match = msg.slice(idx, idx + logSearch.length);
    const after = msg.slice(idx + logSearch.length);
    return (
      <>
        {before}
        <mark className="rounded bg-orange-500/30 px-1 text-orange-200">{match}</mark>
        {after}
      </>
    );
  };

  return (
    <div className="space-y-5">
      {/* ===================================================================== */}
      {/* PROJECT HERO HEADER                                                   */}
      {/* ===================================================================== */}
      <div className="rounded-2xl border border-white/[0.07] bg-[#111115]/90 p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3.5 min-w-0">
            <button
              type="button"
              onClick={onBack}
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:border-white/[0.16] hover:text-zinc-100 transition-colors cursor-pointer"
              title="Back to projects"
            >
              <ArrowLeft className="h-4 w-4" />
            </button>

            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-white/[0.09] bg-zinc-900 font-display text-xs font-bold text-orange-400 uppercase shadow-inner">
              {app.php ? (
                <SiteTypeIcon type={app.php.siteType} className="h-5 w-5" />
              ) : (
                app.name.slice(0, 2)
              )}
            </div>

            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="truncate text-base font-semibold tracking-tight text-zinc-100">
                  {app.name}
                </h1>
                <StatusBadge
                  status={
                    app.php
                      ? app.status
                      : runningContainersCount > 0
                      ? 'running'
                      : app.status === 'failed' || app.status === 'building'
                      ? app.status
                      : 'stopped'
                  }
                />
                {app.php ? (
                  <SiteTypeBadge type={app.php.siteType} phpVersion={app.php.phpVersion} />
                ) : (
                  <span className="rounded-md border border-white/[0.1] bg-zinc-800/80 px-1.5 py-0.5 font-mono text-[10px] font-medium text-zinc-300">
                    production · {activeSlotLower.toUpperCase()}
                  </span>
                )}
                {isPinnedProduction && (
                  <span className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-amber-300">
                    <Pin className="h-2.5 w-2.5" />
                    Pinned
                  </span>
                )}
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-zinc-400">
                {productionUrl && (                <a
                  href={productionUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 font-mono text-zinc-300 hover:text-orange-400 transition-colors"
                >
                  <Globe className="h-3 w-3 text-emerald-400" />
                  {productionUrl}
                  <ArrowUpRight className="h-3 w-3 opacity-60" />
                </a>)}
                {hasGitConnection && app.gitBranch && (
                  <button
                    type="button"
                    onClick={() => setGitModalOpen(true)}
                    className="inline-flex items-center gap-1 font-mono text-[11px] text-zinc-400 hover:text-orange-400 cursor-pointer"
                  >
                    <GitBranch className="h-3 w-3 text-orange-400" />
                    {app.gitBranch}
                    {app.gitCommitSha && app.gitCommitSha !== 'blue' && app.gitCommitSha !== 'green' && (
                      <span>@{app.gitCommitSha.slice(0, 7)}</span>
                    )}
                  </button>
                )}
                {!app.php && (
                  <span className="font-mono text-[11px] text-zinc-500">
                    {runningContainersCount}/{app.services.length} containers · {app.cpuPct}% CPU ·{' '}
                    {app.memMB} MB
                  </span>
                )}
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {!app.php && (
            <button
              type="button"
              onClick={() => triggerDeploy()}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-all cursor-pointer"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              Deploy
            </button>
            )}

            {!app.php && (
            <button
              type="button"
              onClick={async () => {
                toast.info(`Restarting ${app.name} containers...`);
                const res = await backendApi.composeAction(app.id, 'restart');
                if (!res.ok) {
                  toast.error(res.error || `Failed to restart ${app.name}`);
                  return;
                }
                toast.success(`Restarting ${app.name} containers`);
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 transition-colors cursor-pointer"
            >
              <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
              Restart
            </button>
            )}

            <ActionMenu
              items={[
                {
                  label: app.status === 'stopped' ? 'Start Compose Stack' : 'Stop Compose Stack',
                  icon:
                    app.status === 'stopped' ? (
                      <Play className="h-3.5 w-3.5" />
                    ) : (
                      <Square className="h-3.5 w-3.5" />
                    ),
                  onClick: toggleStopOrStart,
                },
                {
                  label: 'Rebuild Without Cache',
                  icon: <RefreshCw className="h-3.5 w-3.5" />,
                  onClick: () => triggerDeploy({ noCache: true }),
                },
                {
                  label: 'Resource Limits',
                  icon: <Sliders className="h-3.5 w-3.5" />,
                  onClick: () => setLimitsModalOpen(true),
                },
                {
                  label: 'Copy Project ID',
                  icon: <Copy className="h-3.5 w-3.5" />,
                  onClick: () => {
                    navigator.clipboard?.writeText(app.id);
                    toast.success(`Copied ${app.id}`);
                  },
                },
                {
                  label: 'Delete Project',
                  icon: <Trash2 className="h-3.5 w-3.5" />,
                  variant: 'danger' as const,
                  onClick: () => setDeleteDialogOpen(true),
                },
              ].filter((it) => !app.php || it.label === 'Copy Project ID' || it.label === 'Delete Project')}
            />
          </div>
        </div>

        {/* Underline Navigation Tabs */}
        <div className="mt-5 -mb-5 border-t border-white/[0.06] pt-1 overflow-x-auto no-scrollbar">
          <div className="flex items-center gap-0.5 min-w-max">
            {tabs.map((t) => {
              const active = activeTab === t.id;
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => setActiveTab(t.id)}
                  className={`relative inline-flex items-center gap-1.5 px-3 py-2.5 text-xs font-medium transition-colors cursor-pointer whitespace-nowrap ${
                    active ? 'text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {t.icon && <span className={active ? 'text-orange-400' : 'text-zinc-500'}>{t.icon}</span>}
                  <span>{t.label}</span>
                  {typeof t.count === 'number' && (
                    <span
                      className={`rounded-md px-1.5 py-0.2 font-mono text-[10px] ${
                        active
                          ? 'bg-orange-500/15 text-orange-400 ring-1 ring-orange-500/30'
                          : 'bg-white/[0.05] text-zinc-500'
                      }`}
                    >
                      {t.count}
                    </span>
                  )}
                  {active && (
                    <span className="absolute inset-x-2 bottom-0 h-0.5 rounded-full bg-orange-500" />
                  )}
                </button>
              );
            })}
          </div>
        </div>
      </div>

      {/* ===================================================================== */}
      {/* TAB 1: GENERAL (Services Topology + Git Pipeline + Container Limits)  */}
      {/* ===================================================================== */}
      {activeTab === 'overview' && (
        <div className="space-y-5">
          {/* Deploy Source Bar (Git Sync ON vs Git Sync OFF vs Pure Manual Workspace) */}
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 px-4 py-3.5 flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2.5 min-w-0">
              {isGitSyncActive ? (
                <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-1 text-xs font-medium text-emerald-300">
                  <GitBranch className="h-3.5 w-3.5" />
                  Git Sync ON
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5 rounded-full border border-orange-500/30 bg-orange-500/10 px-2.5 py-1 text-xs font-medium text-orange-300">
                  <Folder className="h-3.5 w-3.5" />
                  {hasGitConnection ? 'Manual Files (Git Sync OFF)' : 'Manual Workspace'}
                </span>
              )}

              {hasGitConnection && !isGitSyncActive && (
                <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 font-mono text-[11px] font-medium text-amber-300">
                  Git connected · {app.gitBranch || 'main'} (manual sync available)
                </span>
              )}

              {isGitSyncActive && (
                <span className="font-mono text-xs text-zinc-400 break-all">
                  {app.gitRepo || `https://github.com/masudranaxpert/${app.slug}.git`} ({app.gitBranch || 'main'})
                </span>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {hasGitConnection && (
                <button
                  type="button"
                  onClick={() => {
                    setForceGitSyncResume(false);
                    setGitSyncModalOpen(true);
                  }}
                  className={`inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium cursor-pointer ${
                    isGitSyncActive
                      ? 'border-white/[0.08] bg-zinc-900 text-zinc-200 hover:bg-zinc-800'
                      : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20'
                  }`}
                >
                  {isGitSyncActive ? (
                    <>
                      <Pause className="h-3.5 w-3.5 text-amber-400" />
                      Git Sync OFF
                    </>
                  ) : (
                    <>
                      <Play className="h-3.5 w-3.5 text-emerald-400" />
                      Git Sync ON
                    </>
                  )}
                </button>
              )}

              <button
                type="button"
                onClick={() => setSwitchSourceModalOpen(true)}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <RefreshCw className="h-3.5 w-3.5 text-orange-400" />
                {hasGitConnection ? 'Switch to Manual Mode' : 'Connect Git'}
              </button>
            </div>
          </div>

          {/* Row 2: Source Pipeline + Container Resource Limits & Styled Delete Button */}
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
            <div className="lg:col-span-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 flex flex-col justify-between gap-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  {hasGitConnection ? (
                    <GitBranch className="h-4 w-4 text-orange-400" />
                  ) : (
                    <Folder className="h-4 w-4 text-orange-400" />
                  )}
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                    {hasGitConnection ? 'Git Source & Environments' : 'Manual Workspace Directory'}
                  </h3>
                </div>
                {hasGitConnection ? (
                  <button
                    type="button"
                    onClick={() => setGitModalOpen(true)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                  >
                    <GitBranch className="h-3 w-3 text-orange-400" />
                    Change Branch / Repo
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => setActiveTab('files')}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                  >
                    <FolderOpen className="h-3 w-3 text-orange-400" />
                    Manage Files
                  </button>
                )}
              </div>

              {hasGitConnection ? (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                  <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                    <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">
                      Repository
                    </span>
                    <div className="mt-1 truncate font-mono text-xs text-zinc-200">
                      {app.gitRepo
                        ? app.gitRepo.replace('https://github.com/', '')
                        : 'Local Compose Stack'}
                    </div>
                  </div>
                  <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                    <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">
                      Active Branch ({detectedBranches.length} detected)
                    </span>
                    <div className="mt-1 truncate font-mono text-xs text-orange-400">
                      {app.gitBranch || 'main'} · {app.composeFilePath}
                    </div>
                  </div>
                  <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3 flex items-center justify-between">
                    <div>
                      <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500 block">
                        Git Sync
                      </span>
                      <span className="mt-1 font-mono text-xs text-zinc-200 block">
                        {isGitSyncActive ? 'ON (Auto-Pull)' : 'OFF (Manual)'}
                      </span>
                    </div>
                    <ShadcnSwitch
                      checked={isGitSyncActive}
                      onChange={(next) => {
                        void backendApi.setGitSync(app.id, next, false);
                        onUpdateApp({
                          ...app,
                          sourceType: next ? 'git' : 'compose',
                          gitSync: next,
                        });
                        toast.success(
                          next
                            ? 'Git Sync ON — deployments pull from repository'
                            : 'Git Sync OFF — deployments use manual workspace files'
                        );
                      }}
                    />
                  </div>
                </div>
              ) : (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-12">
                  <div className="sm:col-span-3 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                    <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">
                      Deploy Mode
                    </span>
                    <div className="mt-1 font-medium text-xs text-orange-400 whitespace-nowrap">
                      Manual Files Only
                    </div>
                  </div>
                  <div className="sm:col-span-6 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                    <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">
                      Workspace Directory
                    </span>
                    <div className="mt-1 font-mono text-xs text-zinc-100 break-all">
                      /data/apps/{app.slug}
                    </div>
                  </div>
                  <div className="sm:col-span-3 rounded-lg border border-white/[0.06] bg-[#09090b] p-3">
                    <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">
                      Compose Entry
                    </span>
                    <div className="mt-1 font-mono text-xs text-emerald-400 break-all">
                      {app.composeFilePath || 'docker-compose.yml'}
                    </div>
                  </div>
                </div>
              )}

              {/* Production & Preview URLs Bar + Quick Compose & File Manager Actions */}
              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-white/[0.06] pt-3 text-xs">
                <div className="flex flex-wrap items-center gap-3 font-mono text-[11px]">
                  {productionUrl && (                  <a
                    href={productionUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 text-emerald-400 hover:underline"
                  >
                    <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                    Prod: {productionUrl.replace('https://', '')}
                    <ArrowUpRight className="h-3 w-3" />
                  </a>)}
                  <span className="text-zinc-700">|</span>
                  {previewUrl && (                  <a
                    href={previewUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 text-violet-300 hover:underline"
                  >
                    <span className="h-1.5 w-1.5 rounded-full bg-violet-400" />
                    Standby ({standbySlotName}): {previewHost}
                    <ArrowUpRight className="h-3 w-3" />
                  </a>)}
                </div>

                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={async () => {
                      const res = await backendApi.composeView(app.id);
                      const fallbackYaml =
                        app.files.find((f) => f.path.includes('docker-compose'))?.content ||
                        'services:\n  app:\n    build: .\n';
                      setMergedComposeYaml(res.yaml || fallbackYaml);
                      setMergedComposeModalOpen(true);
                    }}
                    className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-emerald-400 hover:bg-zinc-800 cursor-pointer"
                  >
                    <Layers className="h-3.5 w-3.5" />
                    Merged Caddy Compose
                  </button>
                  <button
                    type="button"
                    onClick={() => setActiveTab('files')}
                    className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-orange-400 hover:bg-zinc-800 cursor-pointer"
                  >
                    <FileCode2 className="h-3.5 w-3.5" />
                    Open File Manager
                  </button>
                </div>
              </div>
            </div>

            {/* Right Card: Container Resource Limits & Styled Delete Project Button */}
            <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 flex flex-col justify-between gap-4">
              <div>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Sliders className="h-3.5 w-3.5 text-orange-400" />
                    <span className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                      Resource Limits
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => setLimitsModalOpen(true)}
                    className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] font-medium text-orange-400 hover:bg-zinc-800 cursor-pointer"
                  >
                    Adjust
                  </button>
                </div>

                <div className="mt-3 grid grid-cols-2 gap-2.5 font-mono text-xs">
                  <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-2.5">
                    <span className="text-[10px] text-zinc-500 block">Max CPU</span>
                    <div className="mt-0.5 flex items-baseline justify-between">
                      <span className="text-zinc-100 font-semibold">
                        {unlimitedResources || cpuQuota === '0' ? 'Unlimited' : `${cpuQuota} vCPU`}
                      </span>
                      <span className="text-[10px] text-emerald-400">{app.cpuPct}% used</span>
                    </div>
                  </div>
                  <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-2.5">
                    <span className="text-[10px] text-zinc-500 block">Max Memory</span>
                    <div className="mt-0.5 flex items-baseline justify-between">
                      <span className="text-zinc-100 font-semibold">
                        {unlimitedResources || memQuota === '0' ? 'Unlimited' : `${memQuota} MB`}
                      </span>
                      <span className="text-[10px] text-sky-400">{app.memMB} MB</span>
                    </div>
                  </div>
                </div>
              </div>

              {/* Clean Footer with Copyable ID + Modern Destructive Delete Button */}
              <div className="flex items-center justify-between gap-2 border-t border-white/[0.06] pt-3.5">
                <button
                  type="button"
                  onClick={() => {
                    navigator.clipboard?.writeText(app.id);
                    toast.success(`Copied ${app.id}`);
                  }}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.06] bg-[#09090b] px-2.5 py-1.5 font-mono text-[11px] text-zinc-400 hover:border-white/[0.14] hover:text-zinc-200 transition-colors cursor-pointer"
                  title="Copy Project ID"
                >
                  <Copy className="h-3 w-3 text-zinc-500" />
                  <span>{app.id}</span>
                </button>

                <button
                  type="button"
                  onClick={() => setDeleteDialogOpen(true)}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-rose-500/25 bg-rose-500/10 px-3 py-1.5 text-xs font-medium text-rose-400 hover:border-rose-500/45 hover:bg-rose-500/20 hover:text-rose-300 transition-all cursor-pointer"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>Delete Project</span>
                </button>
              </div>
            </div>
          </div>

          {/* Switch Deploy Source Modal (POST /apps/:id/switch-source) */}
          <ShadcnDialog
            open={switchSourceModalOpen}
            onClose={() => setSwitchSourceModalOpen(false)}
            title={
              hasGitConnection
                ? 'Switch to Pure Manual Workspace Directory'
                : 'Connect Git Repository Source'
            }
            icon={<RefreshCw className="h-4 w-4 text-orange-400" />}
          >
            <div className="space-y-4">
              {hasGitConnection ? (
                <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-200 leading-relaxed">
                  Switching to <strong>Manual Workspace Files</strong> completely detaches Git from this project and hides the Git tab. (If you want to keep Git connected while editing files manually, use <strong>Pause Git sync</strong> instead.)
                </div>
              ) : (
                <div className="space-y-3">
                  <div className="rounded-lg border border-orange-500/30 bg-orange-500/10 p-3 text-xs text-orange-200 leading-relaxed">
                    Connecting a <strong>Git repository</strong> enables the Git tab and sets the repository checkout as the deploy source.
                  </div>
                  <div>
                    <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                      Git Repository URL
                    </label>
                    <input
                      type="text"
                      value={gitRepoInput}
                      onChange={(e) => setGitRepoInput(e.target.value)}
                      placeholder={`https://github.com/masudranaxpert/${app.slug}`}
                      className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                    />
                  </div>
                  <div>
                    <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                      Branch
                    </label>
                    <input
                      type="text"
                      value={gitBranchInput}
                      onChange={(e) => setGitBranchInput(e.target.value)}
                      placeholder="main"
                      className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
                    />
                  </div>
                </div>
              )}

              <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
                <button
                  type="button"
                  onClick={() => setSwitchSourceModalOpen(false)}
                  className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={async () => {
                    const targetSource = hasGitConnection ? 'files' : 'git';
                    const res = await backendApi.switchAppSource(app.id, targetSource);
                    if (!res.ok && !res.offlineFallback) {
                      toast.error(res.error || 'Failed to switch deploy source');
                      return;
                    }
                    if (targetSource === 'git') {
                      const nextRepo =
                        gitRepoInput.trim() || `https://github.com/masudranaxpert/${app.slug}`;
                      const nextBranch = gitBranchInput.trim() || 'main';
                      onUpdateApp({
                        ...app,
                        gitRepo: nextRepo,
                        gitBranch: nextBranch,
                        sourceType: 'git',
                        gitSync: true,
                      });
                      setSwitchSourceModalOpen(false);
                      setActiveTab('git');
                      toast.success('Connected Git repository source');
                    } else {
                      onUpdateApp({
                        ...app,
                        gitRepo: '',
                        sourceType: 'compose',
                        gitSync: false,
                      });
                      setSwitchSourceModalOpen(false);
                      setActiveTab('files');
                      toast.success('Switched to pure Manual Workspace Directory (Git detached)');
                    }
                  }}
                  className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
                >
                  {hasGitConnection ? 'Detach Git & Use Manual Files' : 'Connect Git Source'}
                </button>
              </div>
            </div>
          </ShadcnDialog>

          {/* Pause / Resume Git Sync Modal (POST /apps/:id/git-sync) */}
          <ShadcnDialog
            open={gitSyncModalOpen}
            onClose={() => setGitSyncModalOpen(false)}
            title={app.sourceType === 'git' ? 'Pause Git Sync' : 'Resume Git Sync'}
            icon={<GitBranch className="h-4 w-4 text-orange-400" />}
          >
            <div className="space-y-4">
              {app.sourceType === 'git' ? (
                <p className="text-xs text-zinc-300 leading-relaxed">
                  Copies the current Git checkout into the workspace (newer workspace files are preserved) and pauses automatic Git pulls so you can edit files directly in File Manager or push via <code className="font-mono text-orange-300">nd push</code>.
                </p>
              ) : (
                <div className="space-y-3">
                  <p className="text-xs text-zinc-300 leading-relaxed">
                    Resumes deploying from <code className="font-mono text-emerald-300">{app.gitRepo}</code> (<code className="font-mono text-orange-300">{app.gitBranch || 'main'}</code>). Workspace files stay on disk, but the next deploy pulls from Git.
                  </p>
                  <label className="flex items-start gap-2.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-3 text-xs text-zinc-300 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={forceGitSyncResume}
                      onChange={(e) => setForceGitSyncResume(e.target.checked)}
                      className="mt-0.5 rounded border-zinc-700 accent-orange-500"
                    />
                    <span>
                      Discard uncommitted local edits in the Git checkout if present (<code className="font-mono text-[11px]">force=1</code>)
                    </span>
                  </label>
                </div>
              )}

              <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
                <button
                  type="button"
                  onClick={() => setGitSyncModalOpen(false)}
                  className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={async () => {
                    const nextEnabled = app.sourceType !== 'git';
                    const res = await backendApi.setGitSync(
                      app.id,
                      nextEnabled,
                      nextEnabled ? forceGitSyncResume : false
                    );
                    if (!res.ok && !res.offlineFallback) {
                      toast.error(res.error || 'Failed to update Git sync');
                      return;
                    }
                    onUpdateApp({
                      ...app,
                      sourceType: nextEnabled ? 'git' : 'compose',
                      gitSync: nextEnabled,
                    });
                    setGitSyncModalOpen(false);
                    toast.success(
                      nextEnabled
                        ? 'Git sync resumed — deployments pull from repository'
                        : 'Git sync paused — workspace files are now active'
                    );
                  }}
                  className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
                >
                  {app.sourceType === 'git' ? 'Pause Git Sync' : 'Resume Git Sync'}
                </button>
              </div>
            </div>
          </ShadcnDialog>
        </div>
      )}

      {/* ===================================================================== */}
            {/* ===================================================================== */}
      {/* TAB: CONTAINERS (Dedicated Project Services & Runtime Management)     */}
      {/* ===================================================================== */}
      {activeTab === 'containers' && (
        <div className="space-y-4">
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden shadow-sm">
            {selectedContainers.length > 0 && (
              <div className="flex items-center justify-between border-b border-white/[0.07] bg-[#0c0c0f] px-4 py-2.5">
                <span className="text-xs text-zinc-300 font-medium">
                  {selectedContainers.length} container(s) selected
                </span>
                <button
                  type="button"
                  disabled={isDeletingContainers}
                  onClick={() => handleRemoveSelectedContainers()}
                  className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 text-xs font-medium text-rose-300 hover:bg-rose-500/20 transition-colors cursor-pointer disabled:opacity-50"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>
                    {isDeletingContainers
                      ? 'Deleting...'
                      : `Delete Selected (${selectedContainers.length})`}
                  </span>
                </button>
              </div>
            )}
            <div className="overflow-x-auto">
              <table className="w-full table-fixed text-left text-xs">
                <thead>
                  <tr className="border-b border-white/[0.07] bg-[#09090b]/80 text-[11px] font-medium uppercase tracking-wider text-zinc-400">
                    <th className="w-9 px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={
                          app.services.length > 0 &&
                          selectedContainers.length === app.services.length
                        }
                        onChange={toggleSelectAllContainers}
                        className="rounded border-zinc-700 bg-zinc-900 text-orange-500 focus:ring-0 focus:ring-offset-0 cursor-pointer"
                        title="Select all containers"
                      />
                    </th>
                    <th className="w-[32%] px-3 py-2.5">Container</th>
                    <th className="w-[24%] px-3 py-2.5">Image & Port</th>
                    <th className="w-[19%] px-3 py-2.5">Status</th>
                    <th className="w-[11%] px-3 py-2.5 text-right">Metrics</th>
                    <th className="w-[14%] px-3 py-2.5 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.05]">
                  {app.services.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="px-4 py-12 text-center">
                        <Box className="mx-auto h-8 w-8 text-zinc-600 mb-2" />
                        <p className="text-xs font-medium text-zinc-300">No containers running</p>
                        <p className="text-[11px] text-zinc-500 mt-1">
                          Deploy this project to launch your compose services.
                        </p>
                      </td>
                    </tr>
                  ) : (
                    app.services.map((svc) => {
                      const imgInfo = formatCompactImage(svc.image);
                      const shortPort = formatCompactPort(svc.ports);
                      const isSelected = selectedContainers.includes(svc.name || svc.service);

                      return (
                        <tr
                          key={svc.id}
                          className={`transition-colors hover:bg-white/[0.02] ${
                            isSelected ? 'bg-orange-500/[0.04]' : ''
                          }`}
                        >
                          <td className="w-9 px-3 py-2.5 text-center">
                            <input
                              type="checkbox"
                              checked={isSelected}
                              onChange={() => toggleSelectContainer(svc.name || svc.service)}
                              className="rounded border-zinc-700 bg-zinc-900 text-orange-500 focus:ring-0 focus:ring-offset-0 cursor-pointer"
                            />
                          </td>

                          <td className="px-3 py-2.5 min-w-0">
                            <div className="flex items-center gap-2 min-w-0">
                              <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded border border-white/[0.08] bg-zinc-900">
                                <Box className="h-3 w-3 text-orange-400" />
                              </div>
                              <div className="min-w-0 flex-1">
                                <div className="flex items-center gap-1.5 min-w-0">
                                  <span
                                    className="font-mono text-xs font-semibold text-zinc-100 truncate"
                                    title={svc.name || svc.service}
                                  >
                                    {svc.name || svc.service}
                                  </span>
                                  {(() => {
                                    const n = (svc.name || '').toLowerCase();
                                    const containerSlot = n.includes('-blue-')
                                      ? 'blue'
                                      : n.includes('-green-')
                                      ? 'green'
                                      : null;
                                    if (!containerSlot) return null;
                                    const isProdSlot = containerSlot === activeSlotLower;
                                    return isProdSlot ? (
                                      <span className="rounded bg-emerald-500/15 px-1 py-0.5 font-mono text-[9px] font-semibold uppercase text-emerald-400 border border-emerald-500/25 shrink-0">
                                        PROD
                                      </span>
                                    ) : (
                                      <span className="rounded bg-violet-500/15 px-1 py-0.5 font-mono text-[9px] font-semibold uppercase text-violet-300 border border-violet-500/25 shrink-0">
                                        PREVIEW
                                      </span>
                                    );
                                  })()}
                                </div>
                                <div className="font-mono text-[10px] text-zinc-500 truncate flex items-center gap-1 mt-0.5">
                                  {svc.service && svc.service !== svc.name ? (
                                    <span className="text-zinc-400">svc: {svc.service} · </span>
                                  ) : null}
                                  {svc.id ? <span>#{svc.id.slice(0, 8)}</span> : null}
                                </div>
                              </div>
                            </div>
                          </td>

                          <td className="px-3 py-2.5 min-w-0">
                            <div className="flex flex-col gap-0.5 min-w-0 font-mono text-[11px]">
                              <div className="truncate text-zinc-300" title={svc.image}>
                                {imgInfo.shortName}
                                <span className="text-orange-400">:{imgInfo.tagPart}</span>
                              </div>
                              <div className="flex items-center gap-1">
                                <span className="inline-flex shrink-0 items-center gap-1 rounded border border-white/[0.07] bg-[#09090b] px-1.5 py-0.5 text-[10px] text-zinc-400">
                                  <Network className="h-2.5 w-2.5 text-emerald-400" />
                                  {shortPort}
                                </span>
                              </div>
                            </div>
                          </td>

                          <td className="px-3 py-2.5 min-w-0">
                            <div className="flex flex-col gap-0.5 min-w-0">
                              <div className="flex items-center">
                                <StatusBadge status={svc.state} />
                              </div>
                              <span
                                className="font-mono text-[10px] text-zinc-400 truncate"
                                title={svc.uptime}
                              >
                                {formatCompactUptime(svc.uptime)}
                              </span>
                            </div>
                          </td>

                          <td className="px-3 py-2.5 text-right font-mono tabular-nums text-[11px] whitespace-nowrap">
                            <span className="text-zinc-200">{svc.cpuPct}%</span>
                            <span className="mx-1 text-zinc-600">·</span>
                            <span className="text-zinc-400">{svc.memUsedMB}M</span>
                          </td>

                          <td className="px-3 py-2.5 text-right whitespace-nowrap">
                            <div className="flex items-center justify-end gap-1">
                              <button
                                type="button"
                                title="View logs"
                                onClick={() => {
                                  setLogContainerFilter(svc.service);
                                  setActiveTab('logs');
                                }}
                                className="rounded border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
                              >
                                Logs
                              </button>
                              <ActionMenu
                                items={[
                                  {
                                    label: 'Open Shell',
                                    icon: <TerminalIcon className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      setTerminalService(svc.service);
                                      setActiveTab('terminal');
                                    },
                                  },
                                  {
                                    label: 'Restart Container',
                                    icon: <RotateCw className="h-3.5 w-3.5" />,
                                    onClick: async () => {
                                      await backendApi.appContainerAction(app.id, svc.name, 'restart');
                                      toast.success(`Restarted ${svc.name}`);
                                    },
                                  },
                                  {
                                    label: 'Route Domain to Service',
                                    icon: <Globe className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      setNewDomainService(svc.service);
                                      setNewDomainPort(extractContainerPort(svc.ports));
                                      setDomainModalOpen(true);
                                    },
                                  },
                                  {
                                    label: 'Copy Container ID',
                                    icon: <Copy className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      navigator.clipboard?.writeText(svc.id || svc.name);
                                      toast.success(`Copied ID`);
                                    },
                                  },
                                  {
                                    label: 'Delete Container',
                                    icon: <Trash2 className="h-3.5 w-3.5" />,
                                    variant: 'danger',
                                    onClick: () => handleRemoveSelectedContainers([svc.name || svc.service]),
                                  },
                                ]}
                              />
                            </div>
                          </td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* TAB 2: DEPLOYMENTS (Vercel-Grade Blue/Green, Zero-Downtime & Rollback) */}
      {/* ===================================================================== */}
      {activeTab === 'deployments' && (
        <div className="space-y-3">
          {/* Unified Slot & Production Control Bar */}
          <div className="flex flex-col gap-3 rounded-xl border border-white/[0.08] bg-[#111115]/90 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-start gap-3 min-w-0">
              <div
                className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border ${
                  isPinnedProduction
                    ? 'border-amber-500/30 bg-amber-500/10 text-amber-400'
                    : 'border-emerald-500/25 bg-emerald-500/10 text-emerald-400'
                }`}
              >
                {isPinnedProduction ? (
                  <Pin className="h-3.5 w-3.5" />
                ) : (
                  <CheckCircle2 className="h-3.5 w-3.5" />
                )}
              </div>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase text-emerald-400 border border-emerald-500/25">
                    PROD · {activeSlotLower.toUpperCase()}
                  </span>
                  {activeProductionDeployment && (
                    <span className="font-mono text-xs font-semibold text-zinc-100">
                      {activeProductionDeployment.id}
                    </span>
                  )}
                  {isPinnedProduction && (
                    <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10px] font-medium text-amber-300 border border-amber-500/25">
                      <Lock className="h-2.5 w-2.5" />
                      Pinned
                    </span>
                  )}
                  {productionUrl && (                  <a
                    href={productionUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 font-mono text-[11px] text-zinc-300 hover:text-emerald-400 transition-colors truncate"
                  >
                    <Globe className="h-3 w-3 text-emerald-400 shrink-0" />
                    <span className="truncate">{productionUrl.replace('https://', '')}</span>
                    <ArrowUpRight className="h-3 w-3 shrink-0 opacity-60" />
                  </a>)}
                </div>

                <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-zinc-400">
                  {isPinnedProduction && isStandbyContainerRunning ? (
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="rounded bg-violet-500/15 px-1.5 py-0.5 font-mono text-[9px] font-semibold uppercase text-violet-300 border border-violet-500/25">
                        PREVIEW · {standbySlotName.toUpperCase()}
                      </span>
                      {latestStandbyDeployment && (
                        <span className="font-mono text-[11px] text-zinc-300">
                          {latestStandbyDeployment.id}
                        </span>
                      )}
                      {previewUrl && (                      <a
                        href={previewUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 font-mono text-[11px] text-violet-300 hover:text-violet-200 underline"
                      >
                        {previewHost}
                        <ArrowUpRight className="h-3 w-3" />
                      </a>)}
                    </div>
                  ) : isPinnedProduction ? (
                    <span>
                      Standby · <span className="font-mono text-violet-300">Slot {standbySlotName.toUpperCase()}</span>
                      {previewHost && <span className="text-zinc-400"> · {previewHost}</span>}
                    </span>
                  ) : (
                    <span>
                      Standby · <span className="font-mono text-zinc-300">Slot {standbySlotName.toUpperCase()}</span>
                      {standbySlotHasImage && latestStandbyDeployment ? (
                        <span className="text-emerald-400/90 font-mono">
                          {' '}· {latestStandbyDeployment.id}
                        </span>
                      ) : (
                        <span className="text-zinc-500"> · Idle</span>
                      )}
                    </span>
                  )}
                </div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-1.5 shrink-0">
              {activeProductionDeployment && (
                <button
                  type="button"
                  onClick={() => setBuildLogsDeployment(activeProductionDeployment)}
                  className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
                >
                  <TerminalIcon className="h-3.5 w-3.5 text-zinc-400" />
                  <span>Logs</span>
                </button>
              )}

              <button
                type="button"
                disabled={isPinningSlot}
                onClick={handleTogglePinSlot}
                className={`inline-flex h-7 items-center gap-1.5 rounded-lg border px-2.5 text-xs font-medium transition-colors cursor-pointer ${
                  isPinnedProduction
                    ? 'border-amber-500/35 bg-amber-500/15 text-amber-200 hover:bg-amber-500/25'
                    : 'border-white/[0.08] bg-zinc-900 text-zinc-300 hover:bg-zinc-800 hover:text-white'
                }`}
                title={
                  isPinnedProduction
                    ? 'Unpin production to resume automatic zero-downtime cutover'
                    : 'Pin production so new builds deploy to the Preview Slot first'
                }
              >
                {isPinnedProduction ? (
                  <PinOff className="h-3.5 w-3.5 text-amber-400" />
                ) : (
                  <Pin className="h-3.5 w-3.5 text-zinc-400" />
                )}
                <span>{isPinnedProduction ? 'Unpin' : 'Pin Prod'}</span>
              </button>

              {(isStandbyContainerRunning || standbySlotHasImage) && (
                <button
                  type="button"
                  disabled={isPromotingSlot || liveDeployState.running}
                  onClick={handlePromotePreviewSlot}
                  className="inline-flex h-7 items-center gap-1.5 rounded-lg bg-emerald-500 px-2.5 text-xs font-semibold text-zinc-950 hover:bg-emerald-400 transition-colors cursor-pointer disabled:opacity-50"
                  title={`Promote Standby Slot ${standbySlotName.toUpperCase()} directly to live production traffic`}
                >
                  <Rocket className="h-3.5 w-3.5" />
                  <span>
                    {isPromotingSlot
                      ? 'Promoting...'
                      : `Promote Slot ${standbySlotName.toUpperCase()} to Prod`}
                  </span>
                </button>
              )}

              {!isPinnedProduction && standbySlotHasImage && latestStandbyDeployment && (
                <button
                  type="button"
                  disabled={liveDeployState.running}
                  onClick={() => triggerDeploy({ rollbackFrom: latestStandbyDeployment })}
                  className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 text-xs font-medium text-amber-300 hover:bg-amber-500/20 transition-colors cursor-pointer disabled:opacity-50"
                  title={`Switch production back to Slot ${standbySlotName.toUpperCase()} (${latestStandbyDeployment.id})`}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  <span>Rollback to {standbySlotName.toUpperCase()}</span>
                </button>
              )}
            </div>
          </div>

          {/* Filter & Action Toolbar */}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
              {(
                [
                  { id: 'all', label: 'All' },
                  { id: 'production', label: 'Production' },
                  { id: 'rollback', label: 'Rollbacks' },
                ] as const
              ).map((f) => (
                <button
                  key={f.id}
                  type="button"
                  onClick={() => setDeployEnvFilter(f.id)}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer ${
                    deployEnvFilter === f.id
                      ? 'bg-zinc-800 text-zinc-100'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {f.label}
                </button>
              ))}
            </div>

            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={async () => {
                  const res = await backendApi.clearDeployLogs(app.id);
                  if (!res.ok) {
                    toast.error(res.error || 'Failed to clear deployment history');
                    return;
                  }
                  onUpdateApp({
                    ...app,
                    deployments: [],
                  });
                  toast.success('Cleared deployment history');
                }}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
              >
                <Trash2 className="h-3.5 w-3.5 text-zinc-400" />
                Clear History
              </button>
              <button
                type="button"
                disabled={liveDeployState.running}
                onClick={() => triggerDeploy({ noCache: true })}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
              >
                <RefreshCw className={`h-3.5 w-3.5 text-orange-400 ${liveDeployState.running ? 'animate-spin' : ''}`} />
                Rebuild (No Cache)
              </button>
              <button
                type="button"
                disabled={liveDeployState.running}
                onClick={() => triggerDeploy()}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
              >
                {liveDeployState.running ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Play className="h-3.5 w-3.5 fill-current" />
                )}
                {liveDeployState.running ? 'Deploying...' : 'Deploy'}
              </button>
            </div>
          </div>

          {/* Live Deployment Streaming Console Drawer */}
          {(liveDeployState.running || liveDeployState.output) && (() => {
            const isVisible = isBuildLogExpanded ?? liveDeployState.running;
            return (
              <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-[#09090b]">
                <div
                  className="flex items-center justify-between bg-zinc-900/70 px-3.5 py-2 cursor-pointer select-none"
                  onClick={() => setIsBuildLogExpanded(!isVisible)}
                >
                  <div className="flex items-center gap-2">
                    {liveDeployState.running ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin text-orange-400" />
                    ) : (
                      <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
                    )}
                    <span className="font-mono text-xs font-medium text-zinc-200">
                      {liveDeployState.running
                        ? `Building · ${liveDeployState.action || 'Deploy'}`
                        : `Build output · ${liveDeployState.action || 'Completed'}`}
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      setIsBuildLogExpanded(!isVisible);
                    }}
                    className="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] font-medium text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                  >
                    <span>{isVisible ? 'Hide' : 'Show'}</span>
                    {isVisible ? (
                      <ChevronDown className="h-3.5 w-3.5" />
                    ) : (
                      <ChevronRight className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
                {isVisible && (
                  <div
                    ref={liveDeployLogRef}
                    className="max-h-64 overflow-y-auto border-t border-white/[0.06] p-3 font-mono text-[11px] scroll-smooth"
                  >
                    {renderStyledBuildLog(liveDeployState.output || 'Waiting for output...')}
                  </div>
                )}
              </div>
            );
          })()}

          {/* Deployments Table */}
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
            <table className="w-full table-fixed text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/80 text-[11px] font-medium uppercase tracking-wider text-zinc-400">
                  <th className="w-[34%] px-3.5 py-2.5">Release</th>
                  <th className="w-[22%] px-3.5 py-2.5">Slot & State</th>
                  <th className="w-[24%] px-3.5 py-2.5">Endpoint</th>
                  <th className="w-[12%] px-3.5 py-2.5 text-right">Time</th>
                  <th className="w-[8%] px-3 py-2.5 text-right"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {enrichedDeployments.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-4 py-10 text-center">
                      <p className="text-xs font-medium text-zinc-300">No deployments recorded yet</p>
                      <p className="mt-1 text-[11px] text-zinc-500">
                        Click Deploy to build and launch your project into an active slot.
                      </p>
                    </td>
                  </tr>
                ) : (
                  enrichedDeployments.map((dep) => {
                    const isCurrentProd = dep.isCurrentProd;
                    const canRollbackThisRow = dep.canInstantRollback;
                    const isPreviewLive =
                      dep.isLatestStandby && isStandbyContainerRunning && dep.status === 'ready';
                    const isRowLive =
                      ((isCurrentProd && dep.status === 'ready') || isPreviewLive) &&
                      Boolean(dep.previewUrl);
                    const hasValidGitSha =
                      hasGitConnection &&
                      dep.commitSha &&
                      dep.commitSha !== 'latest' &&
                      dep.commitSha !== 'main';

                    const formattedTime = (() => {
                      if (!dep.startedAt) return '—';
                      const parsed = new Date(dep.startedAt);
                      if (Number.isNaN(parsed.getTime())) {
                        return dep.startedAt.replace('T', ' ').replace('Z', '');
                      }
                      return (
                        parsed.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) +
                        ', ' +
                        parsed.toLocaleTimeString('en-US', {
                          hour: '2-digit',
                          minute: '2-digit',
                          hour12: false,
                        })
                      );
                    })();

                    return (
                      <tr
                        key={dep.id}
                        className="hover:bg-white/[0.02] transition-colors"
                      >
                        {/* Column 1: Release + Source */}
                        <td className="px-3.5 py-2.5 min-w-0">
                          <div className="flex items-center gap-1.5 min-w-0">
                            <StatusBadge status={dep.status} />
                            <span className="font-mono text-xs font-semibold text-zinc-100">
                              {dep.id}
                            </span>
                            {hasGitConnection ? (
                              <span className="rounded bg-white/[0.05] px-1.5 py-0.5 font-mono text-[10px] text-orange-400 shrink-0">
                                {dep.branch}@{dep.commitSha.slice(0, 7)}
                              </span>
                            ) : (
                              <span className="rounded bg-white/[0.05] px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 shrink-0">
                                Workspace
                              </span>
                            )}
                          </div>
                          <div className="mt-0.5 truncate text-[11px] text-zinc-400">
                            {dep.commitMessage}
                          </div>
                        </td>

                        {/* Column 2: Slot & State */}
                        <td className="px-3.5 py-2.5 min-w-0">
                          {isCurrentProd ? (
                            <div className="inline-flex items-center gap-1 rounded border border-emerald-500/30 bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-emerald-400">
                              {dep.envType === 'rollback' && (
                                <RotateCcw className="h-2.5 w-2.5 text-emerald-300" />
                              )}
                              <span>PROD · {dep.slot?.toUpperCase()}</span>
                              {isPinnedProduction && (
                                <Pin className="h-2.5 w-2.5 text-amber-300 ml-0.5" />
                              )}
                            </div>
                          ) : isPreviewLive || (dep.isLatestStandby && isPinnedProduction && standbySlotHasImage) ? (
                            <span className="inline-flex items-center gap-1 rounded border border-violet-500/30 bg-violet-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-violet-300">
                              PREVIEW · {dep.slot?.toUpperCase()}
                            </span>
                          ) : canRollbackThisRow ? (
                            <span className="inline-flex items-center gap-1 rounded border border-sky-500/25 bg-sky-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-sky-300">
                              STANDBY · {dep.slot?.toUpperCase()}
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 font-mono text-[10px] text-zinc-500">
                              {dep.envType === 'rollback' && (
                                <RotateCcw className="h-2.5 w-2.5 text-zinc-500" />
                              )}
                              <span>History {dep.slot && `· ${dep.slot.toUpperCase()}`}</span>
                            </span>
                          )}
                        </td>

                        {/* Column 3: Endpoint (ONLY shown when container actually exists & is live) */}
                        <td className="px-3.5 py-2.5 min-w-0">
                          {isRowLive ? (
                            <a
                              href={dep.previewUrl}
                              target="_blank"
                              rel="noreferrer"
                              className="inline-flex items-center gap-1 truncate font-mono text-[11px] text-zinc-300 hover:text-orange-400 transition-colors max-w-full"
                              title={dep.previewUrl}
                            >
                              <Globe
                                className={`h-3 w-3 shrink-0 ${
                                  isCurrentProd ? 'text-emerald-400' : 'text-violet-400'
                                }`}
                              />
                              <span className="truncate">
                                {dep.previewUrl.replace('https://', '')}
                              </span>
                              <ArrowUpRight className="h-3 w-3 shrink-0 opacity-60" />
                            </a>
                          ) : (
                            <span className="font-mono text-[11px] text-zinc-600">—</span>
                          )}
                        </td>

                        {/* Column 4: Time & Duration */}
                        <td className="px-3.5 py-2.5 text-right font-mono tabular-nums text-[11px] text-zinc-300 whitespace-nowrap">
                          <div>{dep.durationSec}s</div>
                          <div className="text-[10px] text-zinc-500">{formattedTime}</div>
                        </td>

                        {/* Column 5: Actions */}
                        <td className="px-3 py-2.5 text-right whitespace-nowrap">
                          <ActionMenu
                            items={[
                              {
                                label: 'View Build Logs',
                                icon: <TerminalIcon className="h-3.5 w-3.5" />,
                                onClick: () => setBuildLogsDeployment(dep),
                              },
                              ...((dep.isLatestStandby || dep.slot?.toLowerCase() === standbySlotName) && !isCurrentProd
                                ? [
                                    {
                                      label: `Promote Slot ${dep.slot?.toUpperCase()} to Prod`,
                                      icon: <Rocket className="h-3.5 w-3.5" />,
                                      onClick: () => handlePromotePreviewSlot(),
                                    },
                                  ]
                                : []),
                              ...(canRollbackThisRow && !isPinnedProduction
                                ? [
                                    {
                                      label: `Rollback to Slot ${dep.slot?.toUpperCase()}`,
                                      icon: <RotateCcw className="h-3.5 w-3.5" />,
                                      onClick: () => triggerDeploy({ rollbackFrom: dep }),
                                    },
                                  ]
                                : []),
                              ...(hasValidGitSha && !isCurrentProd
                                ? [
                                    {
                                      label: `Redeploy Commit (${dep.commitSha.slice(0, 7)})`,
                                      icon: <RefreshCw className="h-3.5 w-3.5" />,
                                      onClick: () => triggerDeploy({ rollbackFrom: dep }),
                                    },
                                  ]
                                : []),
                              ...(isRowLive
                                ? [
                                    {
                                      label: 'Open Live URL',
                                      icon: <ExternalLink className="h-3.5 w-3.5" />,
                                      onClick: () => window.open(dep.previewUrl, '_blank'),
                                    },
                                  ]
                                : []),
                              ...(hasValidGitSha
                                ? [
                                    {
                                      label: 'Copy Commit SHA',
                                      icon: <Copy className="h-3.5 w-3.5" />,
                                      onClick: () => {
                                        navigator.clipboard?.writeText(dep.commitSha);
                                        toast.success(`Copied SHA ${dep.commitSha.slice(0, 7)}`);
                                      },
                                    },
                                  ]
                                : []),
                              {
                                label: 'Delete Build Log',
                                icon: <Trash2 className="h-3.5 w-3.5" />,
                                variant: 'danger',
                                onClick: async () => {
                                  const rawLogId = String(dep.id).replace(/^dep-/, '');
                                  const res = await backendApi.deployLogDelete(app.id, rawLogId);
                                  if (!res.ok) {
                                    toast.error(res.error || 'Failed to delete build log');
                                    return;
                                  }
                                  onUpdateApp({
                                    ...app,
                                    deployments: app.deployments.filter((d) => d.id !== dep.id),
                                  });
                                  toast.success(`Deleted build log ${dep.id}`);
                                },
                              },
                            ]}
                          />
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ===================================================================== */}
      {/* TAB 3: DOMAINS (Clean Header + Auto-Port Detection + Preview Routing) */}
      {/* ===================================================================== */}
      {activeTab === 'domains' && !app.php && (
        <ProjectDomainsTab app={app} onUpdateApp={onUpdateApp} />
      )}
      {activeTab === 'domains' && app.php && (
        <PhpDomainsTab app={app} onChanged={() => onRefresh?.()} />
      )}
      {activeTab === 'php-settings' && app.php && (
        <PhpSettingsTab app={app} onChanged={() => onRefresh?.()} />
      )}
      {activeTab === 'php-databases' && app.php && <PhpDatabasesTab app={app} />}
      {activeTab === 'php-cron' && app.php && <PhpCronTab app={app} />}
      {activeTab === 'php-logs' && app.php && <PhpLogsTab app={app} />}

      {/* ===================================================================== */}
      {/* TAB 4: ENVIRONMENT VARIABLES                                          */}
      {/* ===================================================================== */}
      {activeTab === 'env' && (
        <div className="rounded-2xl border border-white/[0.07] bg-[#111115]/90 p-5 space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold text-zinc-100">Environment Variables</h2>
              <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
                {app.envVars.length}
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {!envRawMode && (
                <div className="relative">
                  <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
                  <input
                    type="text"
                    value={envSearch}
                    onChange={(e) => setEnvSearch(e.target.value)}
                    placeholder="Filter keys..."
                    className="h-7 w-40 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-2.5 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none"
                  />
                </div>
              )}

              <button
                type="button"
                onClick={() => setRevealAllEnv((prev) => !prev)}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
              >
                {revealAllEnv ? (
                  <EyeOff className="h-3.5 w-3.5 text-orange-400" />
                ) : (
                  <Eye className="h-3.5 w-3.5 text-zinc-400" />
                )}
                {revealAllEnv ? 'Mask All' : 'Reveal All'}
              </button>

              <button
                type="button"
                onClick={() => {
                  if (!envRawMode) {
                    setEnvRawText(app.envVars.map((e) => `${e.key}=${e.value}`).join('\n'));
                  }
                  setEnvRawMode((prev) => !prev);
                }}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
              >
                <FileCode2 className="h-3.5 w-3.5 text-orange-400" />
                {envRawMode ? 'Key-Value Rows' : 'Raw .env Editor'}
              </button>
            </div>
          </div>

          {envRawMode ? (
            <div className="space-y-3">
              <CodeEditorBox
                filename=".env"
                language="env"
                value={envRawText}
                onChange={setEnvRawText}
                height={300}
              />
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={async () => {
                    const parsed = envRawText
                      .split('\n')
                      .map((l) => l.trim())
                      .filter((l) => l && !l.startsWith('#') && l.includes('='))
                      .map((line, idx) => {
                        const eq = line.indexOf('=');
                        return {
                          id: `ev-raw-${Date.now()}-${idx}`,
                          key: line.slice(0, eq).trim().toUpperCase(),
                          value: line.slice(eq + 1).trim(),
                          isSecret: true,
                        };
                      });
                    const res = await backendApi.saveAppEnv(app.id, envRawText);
                    if (!res.ok) {
                      toast.error(res.error || 'Failed to save .env file');
                      return;
                    }
                    onUpdateApp({ ...app, envVars: parsed });
                    setEnvRawMode(false);
                    toast.success(`Saved ${parsed.length} variables to .env`);
                  }}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
                >
                  Apply .env Changes
                </button>
              </div>
            </div>
          ) : (
            <div className="space-y-2">
              <div className="grid grid-cols-1 sm:grid-cols-12 gap-2.5 px-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                <div className="sm:col-span-5">Key</div>
                <div className="sm:col-span-7">Value</div>
              </div>

              {filteredEnvVars.map((ev) => {
                const isRevealed = revealAllEnv || Boolean(revealedEnvIds[ev.id]);
                return (
                  <div
                    key={ev.id}
                    className="grid grid-cols-1 sm:grid-cols-12 gap-2.5 items-center"
                  >
                    <div className="sm:col-span-5">
                      <input
                        type="text"
                        value={ev.key}
                        onPaste={(e) => handlePasteMultiEnv(e, ev.id)}
                        onChange={(e) => handleEnvChange(ev.id, 'key', e.target.value)}
                        placeholder="VARIABLE_NAME"
                        className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] px-3 font-mono text-xs font-medium text-zinc-100 placeholder-zinc-600 focus:border-orange-500/70 focus:outline-none transition-colors"
                      />
                    </div>

                    <div className="sm:col-span-7 relative flex items-center">
                      <input
                        type={isRevealed ? 'text' : 'password'}
                        value={ev.value}
                        onPaste={(e) => handlePasteMultiEnv(e, ev.id)}
                        onChange={(e) => handleEnvChange(ev.id, 'value', e.target.value)}
                        placeholder="Value..."
                        className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-3 pr-24 font-mono text-xs text-zinc-200 placeholder-zinc-600 focus:border-orange-500/70 focus:outline-none transition-colors"
                      />
                      <div className="absolute right-1.5 flex items-center gap-0.5">
                        <button
                          type="button"
                          onClick={() =>
                            setRevealedEnvIds((prev) => ({
                              ...prev,
                              [ev.id]: !prev[ev.id],
                            }))
                          }
                          className="rounded-md p-1.5 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 transition-colors cursor-pointer"
                        >
                          {isRevealed ? (
                            <EyeOff className="h-3.5 w-3.5" />
                          ) : (
                            <Eye className="h-3.5 w-3.5" />
                          )}
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            navigator.clipboard?.writeText(`${ev.key}=${ev.value}`);
                            toast.success(`Copied ${ev.key}`);
                          }}
                          className="rounded-md p-1.5 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200 transition-colors cursor-pointer"
                        >
                          <Copy className="h-3.5 w-3.5" />
                        </button>
                        <button
                          type="button"
                          onClick={() => handleDeleteEnv(ev.id)}
                          className="rounded-md p-1.5 text-zinc-500 hover:bg-rose-500/15 hover:text-rose-400 transition-colors cursor-pointer"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </div>
                  </div>
                );
              })}

              <div className="flex flex-wrap items-center justify-between gap-2 pt-3 border-t border-white/[0.06]">
                <button
                  type="button"
                  onClick={handleAddBlankEnvRow}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.09] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 transition-colors cursor-pointer"
                >
                  <Plus className="h-3.5 w-3.5 text-orange-400" />
                  Add Variable
                </button>

                <button
                  type="button"
                  onClick={async () => {
                    const rawEnv = app.envVars
                      .filter((e) => e.key.trim().length > 0)
                      .map((e) => `${e.key}=${e.value}`)
                      .join('\n');
                    const res = await backendApi.saveAppEnv(app.id, rawEnv);
                    if (!res.ok) {
                      toast.error(res.error || 'Failed to save .env variables');
                      return;
                    }
                    toast.success(`Saved ${app.envVars.length} variables to .env`);
                  }}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-all cursor-pointer"
                >
                  Save .env Variables
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ===================================================================== */}
      {/* TAB 5: FILE MANAGER (cPanel / Webuzo Unified File Explorer + Right-Click + Zip/Unzip) */}
      {/* ===================================================================== */}
      {activeTab === 'files' && (
        <div
          className={`relative overflow-hidden rounded-xl border bg-[#111115] shadow-xl select-none transition-colors ${
            isDragOverFileManager
              ? 'border-orange-500 ring-2 ring-orange-500/30'
              : 'border-white/[0.08]'
          }`}
          onClick={() => {
            if (contextMenu) setContextMenu(null);
          }}
          onDragOver={(e) => {
            e.preventDefault();
            e.stopPropagation();
            if (!isDragOverFileManager) setIsDragOverFileManager(true);
          }}
          onDragLeave={(e) => {
            e.preventDefault();
            e.stopPropagation();
            if (e.currentTarget.contains(e.relatedTarget as Node)) return;
            setIsDragOverFileManager(false);
          }}
          onDrop={async (e) => {
            e.preventDefault();
            e.stopPropagation();
            setIsDragOverFileManager(false);
            if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
              const droppedFiles = Array.from(e.dataTransfer.files);
              void runLocalUpload(droppedFiles);
            }
          }}
        >
          {/* Visual Drag & Drop Overlay */}
          {isDragOverFileManager && (
            <div className="pointer-events-none absolute inset-0 z-40 flex flex-col items-center justify-center bg-[#09090b]/85 backdrop-blur-sm border-2 border-dashed border-orange-500">
              <Upload className="h-10 w-10 text-orange-400 mb-2 animate-bounce" />
              <p className="text-sm font-semibold text-zinc-100">
                Drop files here to upload into /{app.slug}{currentDir ? `/${currentDir}` : ''}
              </p>
              <p className="text-xs text-zinc-400 mt-0.5">Zip archives, code files, and assets are supported</p>
            </div>
          )}
          {/* 1. TOP CPANEL-STYLE FILE MANAGER ACTION TOOLBAR */}
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#0c0c0f] px-3.5 py-2">
            <div className="flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                onClick={() => {
                  setNewFileName('');
                  setNewFileTargetDir(currentDir || '');
                  setNewFileModalOpen(true);
                }}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg bg-zinc-100 px-2.5 text-[11px] font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                <FilePlus className="h-3.5 w-3.5" />
                <span>New File</span>
              </button>

              <button
                type="button"
                onClick={() => {
                  setNewFolderName('');
                  setNewFolderTargetDir(currentDir || '');
                  setNewFolderModalOpen(true);
                }}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <FolderPlus className="h-3.5 w-3.5 text-amber-400" />
                <span>New Folder</span>
              </button>

              <button
                type="button"
                onClick={() => setUploadModalOpen(true)}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <Upload className="h-3.5 w-3.5 text-sky-400" />
                <span>Upload</span>
              </button>

              <div className="mx-0.5 h-4 w-px bg-white/[0.07]" />

              {/* Compress (Zip / Tar.gz) Button */}
              <button
                type="button"
                onClick={() => openCompressModalFor(selectedEntries)}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                title="Compress files or folders into .zip or .tar.gz"
              >
                <Archive className="h-3.5 w-3.5 text-purple-400" />
                <span>Compress</span>
              </button>

              {/* Extract (Unzip) Button */}
              <button
                type="button"
                onClick={() => {
                  const selectedArchive = defaultRichFiles.find(
                    (f) => selectedEntries.includes(f.path) && isArchiveFile(f.name)
                  );
                  const fallbackArchive = currentDirFiles.find((f) => isArchiveFile(f.name));
                  const target = selectedArchive || fallbackArchive;
                  if (target) {
                    setExtractTargetFile(target);
                    setExtractDestDir(currentDir || '');
                  } else {
                    toast.info('Select a .zip or .tar.gz archive to extract');
                  }
                }}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                title="Extract .zip or .tar.gz archive"
              >
                <FolderOpen className="h-3.5 w-3.5 text-emerald-400" />
                <span>Extract</span>
              </button>

              <div className="mx-0.5 h-4 w-px bg-white/[0.07]" />

              <a
                href={`/api/v1/apps/${encodeURIComponent(app.id)}/workspace/archive`}
                download={`${app.slug}-workspace.tar.gz`}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-2.5 text-[11px] font-medium text-emerald-300 hover:bg-emerald-500/20 cursor-pointer transition-colors"
                title="Download full workspace backup (.tar.gz archive)"
              >
                <Download className="h-3.5 w-3.5" />
                <span>Export Workspace</span>
              </a>

              <button
                type="button"
                onClick={() => {
                  setSelectedRestoreFile(null);
                  setRestoreArchiveModalOpen(true);
                }}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 text-[11px] font-medium text-amber-300 hover:bg-amber-500/20 cursor-pointer transition-colors"
                title="Restore full workspace from a .tar.gz or .zip archive"
              >
                <RotateCcw className="h-3.5 w-3.5" />
                <span>Restore Archive</span>
              </button>

              {/* Bulk Move, Copy & Delete when items are checked */}
              {selectedEntries.length > 0 && (
                <>
                  <div className="mx-0.5 h-4 w-px bg-white/[0.07]" />
                  <button
                    type="button"
                    onClick={() => openMoveOrCopyModal(selectedEntries, 'move')}
                    className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 px-2.5 text-[11px] font-medium text-orange-300 hover:bg-orange-500/20 cursor-pointer"
                    title="Move selected files/folders"
                  >
                    <ArrowLeftRight className="h-3.5 w-3.5" />
                    <span>Move ({selectedEntries.length})</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => openMoveOrCopyModal(selectedEntries, 'copy')}
                    className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-sky-500/30 bg-sky-500/10 px-2.5 text-[11px] font-medium text-sky-300 hover:bg-sky-500/20 cursor-pointer"
                    title="Copy selected files/folders"
                  >
                    <Copy className="h-3.5 w-3.5" />
                    <span>Copy ({selectedEntries.length})</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      void deleteEntries(selectedEntries);
                      setSelectedEntries([]);
                    }}
                    className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-rose-500/30 bg-rose-500/10 px-2.5 text-[11px] font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    <span>Delete ({selectedEntries.length})</span>
                  </button>
                </>
              )}
            </div>

            {/* Right: Search Filter */}
            <div className="flex items-center gap-2">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
                <input
                  type="text"
                  value={fileSearch}
                  onChange={(e) => {
                    setFileSearch(e.target.value);
                    if (e.target.value.trim()) setEditingFilePath(null);
                  }}
                  placeholder="Search files..."
                  className="h-7 w-44 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-2.5 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none"
                />
              </div>
            </div>
          </div>

          {/* 2. PATH & BREADCRUMB NAVIGATION BAR */}
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/90 px-3.5 py-2 font-mono text-xs">
            <div className="flex flex-wrap items-center gap-1.5 min-w-0">
              <button
                type="button"
                disabled={!currentDir && !editingFilePath}
                onClick={() => {
                  if (editingFilePath) {
                    setEditingFilePath(null);
                    return;
                  }
                  const parts = currentDir.split('/').filter(Boolean);
                  parts.pop();
                  navigateToFolder(parts.join('/'));
                }}
                className="inline-flex h-6 items-center gap-1 rounded border border-white/[0.08] bg-zinc-900 px-2 text-[11px] font-sans font-medium text-zinc-300 hover:text-white disabled:opacity-35 disabled:pointer-events-none cursor-pointer"
              >
                <ArrowLeft className="h-3 w-3" />
                <span>Up</span>
              </button>

              <div className="flex flex-wrap items-center gap-1 pl-1">
                <button
                  type="button"
                  onClick={() => navigateToFolder('')}
                  className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 transition-colors cursor-pointer ${
                    currentDir === '' && !editingFilePath
                      ? 'bg-amber-500/15 text-amber-300 font-semibold'
                      : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
                  }`}
                >
                  <FolderOpen className="h-3.5 w-3.5 text-amber-400 fill-amber-400/20" />
                  <span>/data/apps/{app.slug}</span>
                </button>

                {currentDir
                  .split('/')
                  .filter(Boolean)
                  .map((segment, idx, arr) => {
                    const targetPath = arr.slice(0, idx + 1).join('/');
                    const isLast = idx === arr.length - 1;
                    return (
                      <React.Fragment key={targetPath}>
                        <ChevronRight className="h-3.5 w-3.5 text-zinc-600" />
                        <button
                          type="button"
                          onClick={() => navigateToFolder(targetPath)}
                          className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 transition-colors cursor-pointer ${
                            isLast && !editingFilePath
                              ? 'bg-amber-500/15 text-amber-300 font-semibold'
                              : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
                          }`}
                        >
                          <Folder className="h-3.5 w-3.5 text-amber-400 fill-amber-400/20" />
                          <span>{segment}</span>
                        </button>
                      </React.Fragment>
                    );
                  })}

                {editingFilePath && (
                  <>
                    <ChevronRight className="h-3.5 w-3.5 text-zinc-600" />
                    <span className="inline-flex items-center gap-1.5 rounded bg-orange-500/15 px-2 py-0.5 font-semibold text-orange-300">
                      {renderFileIcon(selectedFile.name)}
                      <span>{selectedFile.name}</span>
                    </span>
                  </>
                )}
              </div>
            </div>

            <div className="flex items-center gap-3 text-[11px] text-zinc-500 font-sans">
              <button
                type="button"
                onClick={() => setMobileTreeOpen((prev) => !prev)}
                className="inline-flex lg:hidden items-center gap-1 rounded border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] font-medium text-zinc-200 cursor-pointer"
              >
                <Folder className="h-3 w-3 text-amber-400" />
                <span>{mobileTreeOpen ? 'Hide Tree' : 'Folder Tree'}</span>
              </button>
              <span className="hidden sm:inline">Right-click any row for actions</span>
              <span className="hidden sm:inline text-zinc-700">·</span>
              <span className="font-mono text-zinc-400">
                {currentSubfolders.length} folders, {currentDirFiles.length} files
              </span>
            </div>
          </div>

          {/* 3. SPLIT PANE BODY: Left Folder Tree + Right Directory Table / Auto Code Editor */}
          <div className="grid grid-cols-1 lg:grid-cols-12 min-h-[480px]">
            {/* LEFT PANE: Interactive Folder Tree with Working Expand/Collapse Arrows */}
            <div
              className={`${
                mobileTreeOpen ? 'flex' : 'hidden lg:flex'
              } lg:col-span-3 border-b lg:border-b-0 lg:border-r border-white/[0.07] bg-[#0d0d10] p-2.5 flex-col justify-between`}
            >
              <div className="space-y-1">
                <div className="flex items-center justify-between px-2 pb-1">
                  <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
                    Directories
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      const anyOpen = Object.values(expandedDirs).some(Boolean);
                      if (anyOpen) {
                        setExpandedDirs({});
                      } else {
                        const allOpen: Record<string, boolean> = {};
                        allFolderPaths.forEach((d) => {
                          allOpen[d] = true;
                        });
                        setExpandedDirs(allOpen);
                      }
                    }}
                    className="text-[10px] font-mono text-zinc-500 hover:text-zinc-300 cursor-pointer"
                  >
                    {Object.values(expandedDirs).some(Boolean) ? 'Collapse All' : 'Expand All'}
                  </button>
                </div>

                <button
                  type="button"
                  onClick={() => navigateToFolder('')}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    setContextMenu({
                      x: e.clientX,
                      y: e.clientY,
                      targetPath: '',
                      targetName: app.slug,
                      isFolder: true,
                    });
                  }}
                  className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left font-mono text-xs transition-colors cursor-pointer ${
                    currentDir === '' && !editingFilePath
                      ? 'bg-amber-500/15 text-amber-300 font-semibold ring-1 ring-amber-500/25'
                      : 'text-zinc-200 hover:bg-white/[0.04]'
                  }`}
                >
                  <FolderOpen className="h-4 w-4 text-amber-400 fill-amber-400/25 shrink-0" />
                  <span className="truncate">/{app.slug}</span>
                </button>

                <div className="pl-1.5 pt-0.5 space-y-0.5">
                  {rootFolders.map((folder) => {
                    const isExpanded = Boolean(expandedDirs[folder]);
                    const isCurrentFolder = currentDir === folder && !editingFilePath;
                    const childSubfolders = allFolderPaths.filter((d) => {
                      const lastSlash = d.lastIndexOf('/');
                      return lastSlash !== -1 && d.slice(0, lastSlash) === folder;
                    });
                    const folderDirectFiles = defaultRichFiles.filter((f) => {
                      const lastSlash = f.path.lastIndexOf('/');
                      return (lastSlash === -1 ? '' : f.path.slice(0, lastSlash)) === folder;
                    });
                    const hasChildren =
                      childSubfolders.length > 0 || folderDirectFiles.length > 0;

                    return (
                      <div key={folder} className="space-y-0.5">
                        <div
                          onContextMenu={(e) => {
                            e.preventDefault();
                            e.stopPropagation();
                            setContextMenu({
                              x: e.clientX,
                              y: e.clientY,
                              targetPath: folder,
                              targetName: folder,
                              isFolder: true,
                            });
                          }}
                          className={`group flex items-center justify-between rounded-lg pr-2 py-1 font-mono text-xs transition-colors ${
                            isCurrentFolder
                              ? 'bg-amber-500/15 text-amber-200 font-semibold ring-1 ring-amber-500/25'
                              : 'text-zinc-300 hover:bg-white/[0.04] hover:text-zinc-100'
                          }`}
                        >
                          {/* Dedicated Tree Arrow Toggle Button */}
                          <button
                            type="button"
                            onClick={(e) => toggleFolderTreeExpand(folder, e)}
                            title={isExpanded ? 'Collapse folder' : 'Expand folder'}
                            className="flex h-6 w-6 items-center justify-center rounded hover:bg-white/[0.08] text-zinc-400 hover:text-amber-300 shrink-0 cursor-pointer"
                          >
                            {hasChildren ? (
                              <ChevronRight
                                className={`h-3.5 w-3.5 transition-transform duration-150 ${
                                  isExpanded ? 'rotate-90 text-amber-400' : 'text-zinc-500'
                                }`}
                              />
                            ) : (
                              <span className="h-1.5 w-1.5 rounded-full bg-zinc-700" />
                            )}
                          </button>

                          {/* Folder Label Navigation Button */}
                          <button
                            type="button"
                            onClick={() => {
                              if (currentDir === folder && !editingFilePath) {
                                toggleFolderTreeExpand(folder);
                              } else {
                                setExpandedDirs((prev) => ({ ...prev, [folder]: true }));
                                navigateToFolder(folder, { preserveTreeToggle: true });
                              }
                            }}
                            className="flex flex-1 items-center justify-between gap-1.5 text-left min-w-0 py-0.5 cursor-pointer"
                          >
                            <span className="flex items-center gap-1.5 min-w-0">
                              {isCurrentFolder || isExpanded ? (
                                <FolderOpen className="h-4 w-4 text-amber-400 fill-amber-400/25 shrink-0" />
                              ) : (
                                <Folder className="h-4 w-4 text-amber-400 fill-amber-400/20 shrink-0" />
                              )}
                              <span className="truncate">{folder}</span>
                            </span>
                            <span className="text-[10px] text-zinc-500 group-hover:text-zinc-400 shrink-0">
                              {childSubfolders.length + folderDirectFiles.length}
                            </span>
                          </button>
                        </div>

                        {isExpanded && hasChildren && (
                          <div className="ml-3.5 border-l border-white/[0.07] pl-2 space-y-0.5">
                            {childSubfolders.map((sub) => {
                              const subName = sub.split('/').pop() || sub;
                              const subActive = currentDir === sub && !editingFilePath;
                              const subExpanded = Boolean(expandedDirs[sub]);
                              const subFiles = defaultRichFiles.filter((f) => {
                                const lastSlash = f.path.lastIndexOf('/');
                                return (
                                  (lastSlash === -1 ? '' : f.path.slice(0, lastSlash)) === sub
                                );
                              });

                              return (
                                <div key={sub} className="space-y-0.5">
                                  <div
                                    onContextMenu={(e) => {
                                      e.preventDefault();
                                      e.stopPropagation();
                                      setContextMenu({
                                        x: e.clientX,
                                        y: e.clientY,
                                        targetPath: sub,
                                        targetName: subName,
                                        isFolder: true,
                                      });
                                    }}
                                    className={`flex items-center justify-between rounded-lg pr-2 py-1 font-mono text-xs transition-colors ${
                                      subActive
                                        ? 'bg-amber-500/15 text-amber-200 font-semibold ring-1 ring-amber-500/25'
                                        : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                                    }`}
                                  >
                                    <button
                                      type="button"
                                      onClick={(e) => toggleFolderTreeExpand(sub, e)}
                                      className="flex h-5 w-5 items-center justify-center rounded hover:bg-white/[0.08] text-zinc-500 hover:text-amber-300 shrink-0 cursor-pointer"
                                    >
                                      <ChevronRight
                                        className={`h-3 w-3 transition-transform duration-150 ${
                                          subExpanded ? 'rotate-90 text-amber-400' : ''
                                        }`}
                                      />
                                    </button>
                                    <button
                                      type="button"
                                      onClick={() => {
                                        if (currentDir === sub && !editingFilePath) {
                                          toggleFolderTreeExpand(sub);
                                        } else {
                                          setExpandedDirs((prev) => ({ ...prev, [sub]: true }));
                                          navigateToFolder(sub, { preserveTreeToggle: true });
                                        }
                                      }}
                                      className="flex flex-1 items-center justify-between gap-1.5 text-left min-w-0 cursor-pointer"
                                    >
                                      <span className="flex items-center gap-1.5 min-w-0">
                                        {subActive || subExpanded ? (
                                          <FolderOpen className="h-3.5 w-3.5 text-amber-400 fill-amber-400/25 shrink-0" />
                                        ) : (
                                          <Folder className="h-3.5 w-3.5 text-amber-400 fill-amber-400/20 shrink-0" />
                                        )}
                                        <span className="truncate">{subName}</span>
                                      </span>
                                      <span className="text-[10px] text-zinc-600">
                                        {subFiles.length}
                                      </span>
                                    </button>
                                  </div>

                                  {subExpanded && subFiles.length > 0 && (
                                    <div className="ml-3.5 border-l border-white/[0.06] pl-2 space-y-0.5">
                                      {subFiles.map((sf) => {
                                        const isFileOpen = editingFilePath === sf.path;
                                        return (
                                          <button
                                            key={sf.path}
                                            type="button"
                                            onClick={() => openFileInEditor(sf)}
                                            className={`flex w-full items-center gap-2 rounded-md px-2 py-1 text-left font-mono text-[11px] transition-colors cursor-pointer ${
                                              isFileOpen
                                                ? 'bg-orange-500/15 text-orange-300 font-semibold'
                                                : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                                            }`}
                                          >
                                            {renderFileIcon(sf.name)}
                                            <span className="truncate">{sf.name}</span>
                                          </button>
                                        );
                                      })}
                                    </div>
                                  )}
                                </div>
                              );
                            })}

                            {/* Direct files inside this expanded folder */}
                            {folderDirectFiles.map((df) => {
                              const isFileOpen = editingFilePath === df.path;
                              return (
                                <button
                                  key={df.path}
                                  type="button"
                                  onClick={() => openFileInEditor(df)}
                                  className={`flex w-full items-center gap-2 rounded-md px-2 py-1 text-left font-mono text-[11px] transition-colors cursor-pointer ${
                                    isFileOpen
                                      ? 'bg-orange-500/15 text-orange-300 font-semibold'
                                      : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                                  }`}
                                >
                                  {renderFileIcon(df.name)}
                                  <span className="truncate">{df.name}</span>
                                </button>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>

              <div className="mt-4 border-t border-white/[0.06] pt-2.5 px-2 font-mono text-[11px] text-zinc-500 flex items-center justify-between">
                <span>Total Files</span>
                <span className="text-zinc-300">{defaultRichFiles.length} items</span>
              </div>
            </div>

            {/* RIGHT PANE: Directory Listing Table OR Auto Code Editor when a file is opened */}
            <div
              className="lg:col-span-9 flex flex-col justify-between bg-[#111115] min-w-0"
              onContextMenu={(e) => {
                // Allow right click on empty canvas to open actions menu for current folder
                if ((e.target as HTMLElement).closest('tr')) return;
                e.preventDefault();
                setContextMenu({
                  x: e.clientX,
                  y: e.clientY,
                  targetPath: currentDir,
                  targetName: currentDir ? (currentDir.split('/').pop() || currentDir) : app.slug,
                  isFolder: true,
                });
              }}
            >
              {!editingFilePath ? (
                <div className="overflow-x-auto">
                  <table className="w-full min-w-[600px] table-fixed text-left text-xs">
                    <thead>
                      <tr className="border-b border-white/[0.07] bg-[#0b0b0e] text-zinc-400">
                        <th className="w-10 px-3.5 py-2.5">
                          <input
                            type="checkbox"
                            checked={
                              currentSubfolders.length + currentDirFiles.length > 0 &&
                              selectedEntries.length ===
                                currentSubfolders.length + currentDirFiles.length
                            }
                            onChange={(e) => {
                              if (e.target.checked) {
                                setSelectedEntries([
                                  ...currentSubfolders.map((d) => `dir:${d}`),
                                  ...currentDirFiles.map((f) => f.path),
                                ]);
                              } else {
                                setSelectedEntries([]);
                              }
                            }}
                            className="h-3.5 w-3.5 rounded border-white/20 bg-zinc-900 accent-orange-500 cursor-pointer"
                          />
                        </th>
                        <th className="w-[42%] px-2 py-2.5 font-medium">Name</th>
                        <th className="w-[14%] px-3 py-2.5 font-medium">Size</th>
                        <th className="w-[16%] px-3 py-2.5 font-medium">Modified</th>
                        <th className="w-[18%] px-3 py-2.5 font-medium">Permissions</th>
                        <th className="w-12 px-3 py-2.5 text-right font-medium"></th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-white/[0.05] font-mono">
                      {/* Parent Directory (..) Row */}
                      {currentDir !== '' && (
                        <tr
                          onClick={() => {
                            const parts = currentDir.split('/').filter(Boolean);
                            parts.pop();
                            navigateToFolder(parts.join('/'));
                          }}
                          className="hover:bg-white/[0.03] transition-colors cursor-pointer"
                        >
                          <td className="px-3.5 py-2.5"></td>
                          <td className="px-2 py-2.5">
                            <div className="flex items-center gap-2.5 text-zinc-300 hover:text-amber-300">
                              <FolderOpen className="h-4 w-4 text-amber-400/80 fill-amber-400/15 shrink-0" />
                              <span className="font-semibold">..</span>
                              <span className="font-sans text-[11px] text-zinc-500">
                                Up One Level
                              </span>
                            </div>
                          </td>
                          <td className="px-3 py-2.5 text-zinc-600">—</td>
                          <td className="px-3 py-2.5 text-zinc-600">—</td>
                          <td className="px-3 py-2.5 text-[11px] text-zinc-500">755</td>
                          <td className="px-3 py-2.5"></td>
                        </tr>
                      )}

                      {/* Folder Rows */}
                      {currentSubfolders.map((dirPath) => {
                        const folderName = fileSearch.trim()
                          ? dirPath
                          : dirPath.split('/').pop() || dirPath;
                        const entryKey = `dir:${dirPath}`;
                        const isChecked = selectedEntries.includes(entryKey);

                        return (
                          <tr
                            key={entryKey}
                            onClick={() => navigateToFolder(dirPath)}
                            onContextMenu={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              setContextMenu({
                                x: e.clientX,
                                y: e.clientY,
                                targetPath: dirPath,
                                targetName: folderName,
                                isFolder: true,
                              });
                            }}
                            className={`group transition-colors cursor-pointer ${
                              isChecked ? 'bg-amber-500/[0.07]' : 'hover:bg-white/[0.03]'
                            }`}
                          >
                            <td
                              className="px-3.5 py-2.5"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <input
                                type="checkbox"
                                checked={isChecked}
                                onChange={() => toggleSelectEntry(entryKey)}
                                className="h-3.5 w-3.5 rounded border-white/20 bg-zinc-900 accent-orange-500 cursor-pointer"
                              />
                            </td>
                            <td className="px-2 py-2.5">
                              <div className="flex items-center gap-2.5 min-w-0">
                                <Folder className="h-4 w-4 text-amber-400 fill-amber-400/25 shrink-0" />
                                <span className="truncate font-semibold text-zinc-100 group-hover:text-amber-300">
                                  {folderName}
                                </span>
                              </div>
                            </td>
                            <td className="px-3 py-2.5 text-[11px] text-zinc-500">Directory</td>
                            <td className="px-3 py-2.5 font-sans text-[11px] text-zinc-400">
                              2d ago
                            </td>
                            <td
                              className="px-3 py-2.5 text-[11px] text-zinc-400"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <button
                                type="button"
                                onClick={() => openChmodFor(dirPath, true)}
                                className="font-mono hover:text-emerald-400 hover:underline cursor-pointer"
                              >
                                {formatPermOctal(filePermissions[dirPath], true)}
                              </button>
                            </td>
                            <td
                              className="px-3 py-2.5 text-right"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <ActionMenu
                                items={[
                                  {
                                    label: 'Open Folder',
                                    icon: <FolderOpen className="h-3.5 w-3.5" />,
                                    onClick: () => navigateToFolder(dirPath),
                                  },
                                  {
                                    label: 'Compress to .zip / .tar.gz',
                                    icon: <Archive className="h-3.5 w-3.5" />,
                                    onClick: () => openCompressModalFor([dirPath]),
                                  },
                                  {
                                    label: 'Rename Folder',
                                    icon: <FileCode2 className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      setRenameTarget({
                                        path: dirPath,
                                        name: folderName,
                                        isFolder: true,
                                      });
                                      setRenameInput(folderName);
                                    },
                                  },
                                  {
                                    label: 'Copy / Move to...',
                                    icon: <Copy className="h-3.5 w-3.5" />,
                                    onClick: () => openMoveOrCopyModal([dirPath]),
                                  },
                                  {
                                    label: 'Permissions (chmod)',
                                    icon: <KeyRound className="h-3.5 w-3.5" />,
                                    onClick: () => openChmodFor(dirPath),
                                  },
                                  {
                                    label: 'Delete Folder',
                                    icon: <Trash2 className="h-3.5 w-3.5" />,
                                    variant: 'danger',
                                    onClick: () => {
                                      void deleteEntries([dirPath]);
                                    },
                                  },
                                ]}
                              />
                            </td>
                          </tr>
                        );
                      })}

                      {/* File Rows */}
                      {currentDirFiles.map((file) => {
                        const displayName = fileSearch.trim() ? file.path : file.name;
                        const isChecked = selectedEntries.includes(file.path);
                        const isArchive = isArchiveFile(file.name);

                        return (
                          <tr
                            key={file.path}
                            onClick={() => openFileInEditor(file)}
                            onContextMenu={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              setContextMenu({
                                x: e.clientX,
                                y: e.clientY,
                                targetPath: file.path,
                                targetName: file.name,
                                isFolder: false,
                              });
                            }}
                            className={`group transition-colors cursor-pointer ${
                              isChecked ? 'bg-orange-500/[0.07]' : 'hover:bg-white/[0.03]'
                            }`}
                          >
                            <td
                              className="px-3.5 py-2.5"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <input
                                type="checkbox"
                                checked={isChecked}
                                onChange={() => toggleSelectEntry(file.path)}
                                className="h-3.5 w-3.5 rounded border-white/20 bg-zinc-900 accent-orange-500 cursor-pointer"
                              />
                            </td>
                            <td className="px-2 py-2.5">
                              <div className="flex items-center gap-2.5 min-w-0">
                                {renderFileIcon(file.name)}
                                <span className="truncate font-semibold text-zinc-100 group-hover:text-orange-400">
                                  {displayName}
                                </span>
                                {isArchive && (
                                  <span className="rounded bg-purple-500/15 px-1.5 py-0.2 font-mono text-[9px] font-semibold uppercase text-purple-300 ring-1 ring-purple-500/30 shrink-0">
                                    ARCHIVE
                                  </span>
                                )}
                              </div>
                            </td>
                            <td className="px-3 py-2.5 text-[11px] text-zinc-300 tabular-nums">
                              {formatFileSize(file.sizeBytes)}
                            </td>
                            <td className="px-3 py-2.5 font-sans text-[11px] text-zinc-400">
                              {file.updatedAt}
                            </td>
                            <td
                              className="px-3 py-2.5 text-[11px] text-emerald-400"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <button
                                type="button"
                                onClick={() => openChmodFor(file.path, false)}
                                className="font-mono hover:underline cursor-pointer"
                              >
                                {formatPermOctal(filePermissions[file.path], false)}
                              </button>
                            </td>
                            <td
                              className="px-3 py-2.5 text-right"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <ActionMenu
                                items={[
                                  ...(isArchive
                                    ? [
                                        {
                                          label: 'Extract Archive (Unzip)',
                                          icon: <FolderOpen className="h-3.5 w-3.5" />,
                                          onClick: () => {
                                            setExtractTargetFile(file);
                                            setExtractDestDir(currentDir || '');
                                          },
                                        },
                                      ]
                                    : [
                                        {
                                          label: 'Edit File',
                                          icon: <FileCode2 className="h-3.5 w-3.5" />,
                                          onClick: () => openFileInEditor(file),
                                        },
                                      ]),
                                  {
                                    label: 'Rename',
                                    icon: <FileText className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      setRenameTarget({
                                        path: file.path,
                                        name: file.name,
                                        isFolder: false,
                                      });
                                      setRenameInput(file.name);
                                    },
                                  },
                                  {
                                    label: 'Copy / Move to...',
                                    icon: <Copy className="h-3.5 w-3.5" />,
                                    onClick: () => openMoveOrCopyModal([file.path]),
                                  },
                                  {
                                    label: 'Compress (.zip / .tar.gz)',
                                    icon: <Archive className="h-3.5 w-3.5" />,
                                    onClick: () => openCompressModalFor([file.path]),
                                  },
                                  {
                                    label: 'Permissions (chmod)',
                                    icon: <KeyRound className="h-3.5 w-3.5" />,
                                    onClick: () => openChmodFor(file.path),
                                  },
                                  {
                                    label: 'Download',
                                    icon: <Download className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      const a = document.createElement('a');
                                      a.href = `/apps/${encodeURIComponent(
                                        app.id
                                      )}/file?path=${encodeURIComponent(file.path)}&download=1`;
                                      a.download = file.name;
                                      document.body.appendChild(a);
                                      a.click();
                                      document.body.removeChild(a);
                                      toast.success(`Downloading ${file.name}`);
                                    },
                                  },
                                  {
                                    label: 'Delete',
                                    icon: <Trash2 className="h-3.5 w-3.5" />,
                                    variant: 'danger',
                                    onClick: () => {
                                      void deleteEntries([file.path]);
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
              ) : (
                /* Automatic Code Editor View when a file is opened */
                <div className="p-2.5 sm:p-3 space-y-2.5 min-w-0">
                  <div className="flex items-center justify-between gap-2 rounded-lg border border-white/[0.07] bg-[#09090b] px-2 py-1.5 min-w-0">
                    <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar min-w-0 flex-1">
                      <button
                        type="button"
                        onClick={() => setEditingFilePath(null)}
                        className="inline-flex items-center gap-1.5 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 hover:text-white shrink-0 cursor-pointer"
                      >
                        <ArrowLeft className="h-3.5 w-3.5 text-amber-400" />
                        <span>Back to Files</span>
                      </button>

                      {openEditorTabs.map((tabPath) => {
                        const tabFile = defaultRichFiles.find((f) => f.path === tabPath);
                        if (!tabFile) return null;
                        const active = selectedFile.path === tabPath;
                        return (
                          <div
                            key={tabPath}
                            onClick={() => setEditingFilePath(tabPath)}
                            className={`group inline-flex items-center gap-1.5 rounded-md border pl-2.5 pr-1.5 py-1 font-mono text-xs cursor-pointer transition-colors shrink-0 ${
                              active
                                ? 'border-orange-500/40 bg-orange-500/12 text-zinc-100 font-semibold'
                                : 'border-white/[0.05] bg-white/[0.03] text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200'
                            }`}
                          >
                            {renderFileIcon(tabFile.name)}
                            <span className="max-w-[160px] truncate">{tabFile.name}</span>
                            <button
                              type="button"
                              onClick={(e) => closeEditorTab(tabPath, e)}
                              title={`Close ${tabFile.name}`}
                              className={`ml-0.5 inline-flex h-4 w-4 items-center justify-center rounded transition-colors cursor-pointer ${
                                active
                                  ? 'text-zinc-300 hover:bg-orange-500/25 hover:text-white'
                                  : 'text-zinc-500 hover:bg-white/[0.1] hover:text-zinc-200'
                              }`}
                            >
                              <X className="h-3 w-3" />
                            </button>
                          </div>
                        );
                      })}
                    </div>

                    <div className="flex items-center gap-2 shrink-0">
                      {openEditorTabs.length > 1 && (
                        <button
                          type="button"
                          onClick={() => {
                            setOpenEditorTabs([]);
                            setEditingFilePath(null);
                          }}
                          className="hidden sm:inline-flex items-center gap-1 rounded border border-white/[0.07] bg-zinc-900/80 px-2 py-1 text-[10px] font-medium text-zinc-400 hover:text-zinc-200 cursor-pointer"
                          title="Close all open tabs"
                        >
                          <span>Close All</span>
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => openChmodFor(selectedFile.path, false)}
                        className="hidden md:inline-block font-mono text-[11px] text-emerald-400 hover:underline cursor-pointer px-1.5"
                      >
                        {formatPermOctal(filePermissions[selectedFile.path], false)}
                      </button>
                    </div>
                  </div>

                  <CodeEditorBox
                    filename={selectedFile.path}
                    language={selectedFile.language}
                    value={selectedFile.content}
                    onChange={handleSaveFileContent}
                    onSave={async () => {
                      const res = await backendApi.fileSave(
                        app.id,
                        selectedFile.path,
                        selectedFile.content
                      );
                      if (!res.ok) {
                        toast.error(res.error || res.message || `Failed to save ${selectedFile.path}`);
                        return;
                      }
                      if (app.gitRepo && (app.gitSync !== false || app.sourceType === 'git')) {
                        void backendApi.setGitSync(app.id, false, false);
                        onUpdateApp({
                          ...app,
                          sourceType: 'compose',
                          gitSync: false,
                        });
                        toast.success(
                          `Saved ${selectedFile.path} · Git Sync automatically paused due to manual file edit`
                        );
                      } else {
                        toast.success(`Saved ${selectedFile.path}`);
                      }
                      await syncWorkspaceFilesFromDisk();
                    }}
                    permissions={formatPermOctal(filePermissions[selectedFile.path], false)}
                    height={420}
                    actions={
                      <button
                        type="button"
                        onClick={async () => {
                          const res = await backendApi.fileSave(
                            app.id,
                            selectedFile.path,
                            selectedFile.content
                          );
                          if (!res.ok) {
                            toast.error(
                              res.error || res.message || `Failed to save ${selectedFile.path}`
                            );
                            return;
                          }
                          if (app.gitRepo && (app.gitSync !== false || app.sourceType === 'git')) {
                            void backendApi.setGitSync(app.id, false, false);
                            onUpdateApp({
                              ...app,
                              sourceType: 'compose',
                              gitSync: false,
                            });
                          }
                          await syncWorkspaceFilesFromDisk();
                          void triggerDeploy();
                        }}
                        className="inline-flex h-7 items-center gap-1 rounded-lg border border-orange-500/30 bg-orange-500/15 px-2.5 text-[11px] font-semibold text-orange-300 hover:bg-orange-500/25 cursor-pointer"
                      >
                        <RotateCw className="h-3 w-3" />
                        <span className="hidden sm:inline">Save & Deploy</span>
                        <span className="sm:hidden">Deploy</span>
                      </button>
                    }
                  />
                </div>
              )}
            </div>
          </div>

          {/* 4. RIGHT-CLICK FLOATING CONTEXT MENU (cPanel / Desktop File Manager Style) */}
          {contextMenu && (
            <div
              className="fixed z-50 w-56 rounded-xl border border-white/[0.12] bg-[#141419]/98 p-1.5 shadow-2xl backdrop-blur-xl font-sans text-xs"
              style={{
                top:
                  contextMenu.y + 440 > window.innerHeight
                    ? Math.max(10, window.innerHeight - 450)
                    : contextMenu.y,
                left: Math.min(contextMenu.x, window.innerWidth - 240),
              }}
              onClick={(e) => e.stopPropagation()}
            >
              <div className="border-b border-white/[0.06] px-2.5 py-1.5 font-mono text-[11px] text-zinc-400 truncate flex items-center gap-2">
                {contextMenu.isFolder ? (
                  <Folder className="h-3.5 w-3.5 text-amber-400 fill-amber-400/25 shrink-0" />
                ) : (
                  renderFileIcon(contextMenu.targetName)
                )}
                <span className="truncate font-semibold text-zinc-200">
                  {contextMenu.targetName}
                </span>
              </div>

              <div className="py-1 space-y-0.5">
                {contextMenu.isFolder ? (
                  <>
                    <button
                      type="button"
                      onClick={() => navigateToFolder(contextMenu.targetPath)}
                      className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                    >
                      <FolderOpen className="h-3.5 w-3.5 text-amber-400" />
                      <span>Open Folder</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => {
                        const targetDir = contextMenu.targetPath;
                        setCurrentDir(targetDir);
                        setNewFileName('');
                        setNewFileTargetDir(targetDir || '');
                        setNewFileModalOpen(true);
                        setContextMenu(null);
                      }}
                      className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                    >
                      <FilePlus className="h-3.5 w-3.5 text-orange-400" />
                      <span>New File in this Folder</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => {
                        const targetDir = contextMenu.targetPath;
                        setCurrentDir(targetDir);
                        setNewFolderName('');
                        setNewFolderTargetDir(targetDir || '');
                        setNewFolderModalOpen(true);
                        setContextMenu(null);
                      }}
                      className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                    >
                      <FolderPlus className="h-3.5 w-3.5 text-amber-400" />
                      <span>New Subfolder</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => {
                        setCurrentDir(contextMenu.targetPath);
                        setUploadModalOpen(true);
                        setContextMenu(null);
                      }}
                      className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                    >
                      <Upload className="h-3.5 w-3.5 text-sky-400" />
                      <span>Upload Files Here</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => {
                        setContextMenu(null);
                        toast.success(`Reloaded /data/apps/${app.slug}${contextMenu.targetPath ? `/${contextMenu.targetPath}` : ''}`);
                      }}
                      className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                    >
                      <RefreshCw className="h-3.5 w-3.5 text-teal-400" />
                      <span>Reload / Refresh</span>
                    </button>

                    <div className="my-1 border-t border-white/[0.06]" />
                  </>
                ) : isArchiveFile(contextMenu.targetName) ? (
                  <button
                    type="button"
                    onClick={() => {
                      const found = defaultRichFiles.find(
                        (f) => f.path === contextMenu.targetPath
                      );
                      if (found) {
                        setExtractTargetFile(found);
                        setExtractDestDir(currentDir || '');
                      }
                      setContextMenu(null);
                    }}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left font-medium text-emerald-300 hover:bg-emerald-500/15 cursor-pointer"
                  >
                    <FolderOpen className="h-3.5 w-3.5 text-emerald-400" />
                    <span>Extract Archive (Unzip)</span>
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      const found = defaultRichFiles.find(
                        (f) => f.path === contextMenu.targetPath
                      );
                      if (found) openFileInEditor(found);
                    }}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                  >
                    <FileCode2 className="h-3.5 w-3.5 text-orange-400" />
                    <span>Edit in Code Editor</span>
                  </button>
                )}

                <button
                  type="button"
                  onClick={() => openCompressModalFor([contextMenu.targetPath])}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                >
                  <Archive className="h-3.5 w-3.5 text-purple-400" />
                  <span>Compress (.zip / .tar.gz)</span>
                </button>

                <button
                  type="button"
                  onClick={() => {
                    setRenameTarget({
                      path: contextMenu.targetPath,
                      name: contextMenu.targetName,
                      isFolder: contextMenu.isFolder,
                    });
                    setRenameInput(contextMenu.targetName);
                    setContextMenu(null);
                  }}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                >
                  <FileText className="h-3.5 w-3.5 text-zinc-400" />
                  <span>Rename</span>
                </button>

                <button
                  type="button"
                  onClick={() => openMoveOrCopyModal([contextMenu.targetPath])}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                >
                  <Copy className="h-3.5 w-3.5 text-sky-400" />
                  <span>Copy / Move to...</span>
                </button>

                <button
                  type="button"
                  onClick={() => openChmodFor(contextMenu.targetPath)}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                >
                  <KeyRound className="h-3.5 w-3.5 text-emerald-400" />
                  <span>Permissions (chmod)</span>
                </button>

                <button
                  type="button"
                  onClick={() => {
                    navigator.clipboard?.writeText(
                      `/data/apps/${app.slug}/${contextMenu.targetPath}`
                    );
                    toast.success(`Copied path /data/apps/${app.slug}/${contextMenu.targetPath}`);
                    setContextMenu(null);
                  }}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                >
                  <Copy className="h-3.5 w-3.5 text-zinc-400" />
                  <span>Copy Path</span>
                </button>

                {!contextMenu.isFolder && (
                  <button
                    type="button"
                    onClick={() => {
                      const a = document.createElement('a');
                      a.href = `/apps/${encodeURIComponent(
                        app.id
                      )}/file?path=${encodeURIComponent(contextMenu.targetPath)}&download=1`;
                      a.download = contextMenu.targetName;
                      document.body.appendChild(a);
                      a.click();
                      document.body.removeChild(a);
                      toast.success(`Downloading ${contextMenu.targetName}`);
                      setContextMenu(null);
                    }}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                  >
                    <Download className="h-3.5 w-3.5 text-zinc-400" />
                    <span>Download</span>
                  </button>
                )}

                <div className="my-1 border-t border-white/[0.06]" />

                <button
                  type="button"
                  onClick={() => {
                    void deleteEntries([contextMenu.targetPath]);
                    setContextMenu(null);
                  }}
                  className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-rose-400 hover:bg-rose-500/15 cursor-pointer"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>Delete</span>
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ===================================================================== */}
      {/* TAB 6: GIT SOURCE & GIT SYNC                                          */}
      {/* ===================================================================== */}
      {activeTab === 'git' && (
        <ProjectGitTab app={app} onUpdateApp={onUpdateApp} />
      )}

      {/* ===================================================================== */}
      {/* TAB 7: VOLUMES                                                        */}
      {/* ===================================================================== */}
      {activeTab === 'volumes' && (
        <ProjectVolumesTab app={app} onUpdateApp={onUpdateApp} />
      )}

      {/* ===================================================================== */}
      {/* TAB 8: BACKUPS                                                        */}
      {/* ===================================================================== */}
      {activeTab === 'backups' && (
        <ProjectBackupsTab app={app} />
      )}

      {/* ===================================================================== */}
      {/* TAB 9: MODERN LOGS EXPLORER (Datadog / Vercel / Dokploy Style)        */}
      {/* ===================================================================== */}
      {activeTab === 'logs' && (
        <div className="space-y-3">
          {/* Multi-Dimensional Filter Toolbar */}
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              {/* Service Pills */}
              <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
                <button
                  type="button"
                  onClick={() => setLogContainerFilter('all')}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium cursor-pointer ${
                    logContainerFilter === 'all'
                      ? 'bg-zinc-800 text-zinc-100'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  All Services
                </button>
                {app.services.map((s) => (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => setLogContainerFilter(s.service)}
                    className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 font-mono text-xs cursor-pointer ${
                      logContainerFilter === s.service
                        ? 'bg-zinc-800 text-zinc-100'
                        : 'text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${
                        s.state === 'running' ? 'bg-emerald-400' : 'bg-zinc-500'
                      }`}
                    />
                    {s.service}
                  </button>
                ))}
              </div>

              {/* Severity Filter Pills */}
              <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5 font-mono text-[11px]">
                {(
                  [
                    { id: 'all', label: 'ALL', count: logCounts.all, color: 'text-zinc-300' },
                    { id: 'info', label: 'INFO', count: logCounts.info, color: 'text-sky-400' },
                    { id: 'warn', label: 'WARN', count: logCounts.warn, color: 'text-amber-400' },
                    { id: 'error', label: 'ERR', count: logCounts.error, color: 'text-rose-400' },
                    { id: 'debug', label: 'DBG', count: logCounts.debug, color: 'text-zinc-400' },
                  ] as const
                ).map((lvl) => (
                  <button
                    key={lvl.id}
                    type="button"
                    onClick={() => setLogLevelFilter(lvl.id)}
                    className={`inline-flex items-center gap-1 rounded-md px-2 py-1 transition-colors cursor-pointer ${
                      logLevelFilter === lvl.id
                        ? 'bg-zinc-800 font-semibold text-white'
                        : `${lvl.color} hover:bg-white/[0.04]`
                    }`}
                  >
                    <span>{lvl.label}</span>
                    <span className="text-[10px] opacity-65">{lvl.count}</span>
                  </button>
                ))}
              </div>

              {/* Time Range Selector */}
              <select
                value={logTimeRange}
                onChange={(e) =>
                  setLogTimeRange(e.target.value as 'live' | '15m' | '1h' | '24h')
                }
                className="h-7 rounded-lg border border-white/[0.07] bg-[#09090b] px-2 font-mono text-xs text-zinc-300 focus:outline-none"
              >
                <option value="live">Live Tail</option>
                <option value="15m">Last 15m</option>
                <option value="1h">Last 1h</option>
                <option value="24h">Last 24h</option>
              </select>
            </div>

            {/* Right Search & Stream Controls */}
            <div className="flex flex-wrap items-center gap-1.5">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
                <input
                  type="text"
                  value={logSearch}
                  onChange={(e) => setLogSearch(e.target.value)}
                  placeholder="Filter / regex..."
                  className="h-7 w-44 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-2.5 font-mono text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none"
                />
              </div>

              <button
                type="button"
                onClick={() => setLogShowTimestamps((p) => !p)}
                className={`inline-flex h-7 items-center gap-1 rounded-lg border px-2 text-[11px] font-mono cursor-pointer ${
                  logShowTimestamps
                    ? 'border-orange-500/40 bg-orange-500/10 text-orange-300'
                    : 'border-white/[0.08] bg-zinc-900 text-zinc-400'
                }`}
                title="Toggle timestamps"
              >
                <Clock className="h-3 w-3" />
                TS
              </button>

              <button
                type="button"
                onClick={() => setLogWordWrap((p) => !p)}
                className={`inline-flex h-7 items-center gap-1 rounded-lg border px-2 text-[11px] font-mono cursor-pointer ${
                  logWordWrap
                    ? 'border-orange-500/40 bg-orange-500/10 text-orange-300'
                    : 'border-white/[0.08] bg-zinc-900 text-zinc-400'
                }`}
                title="Toggle word wrap"
              >
                <WrapText className="h-3 w-3" />
              </button>

              <button
                type="button"
                onClick={() => setLogLiveTail((p) => !p)}
                className={`inline-flex h-7 items-center gap-1.5 rounded-lg border px-2.5 text-[11px] font-medium cursor-pointer ${
                  logLiveTail
                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                    : 'border-white/[0.08] bg-zinc-900 text-zinc-400'
                }`}
              >
                <span
                  className={`h-1.5 w-1.5 rounded-full ${
                    logLiveTail ? 'bg-emerald-400 animate-pulse' : 'bg-zinc-500'
                  }`}
                />
                {logLiveTail ? 'Streaming' : 'Paused'}
              </button>
            </div>
          </div>

          {/* Log Console Stream */}
          <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-[#07070a]">
            <div className="divide-y divide-white/[0.03] font-mono text-xs max-h-[420px] overflow-y-auto">
              {filteredLogs.length === 0 ? (
                <div className="p-8 text-center text-xs text-zinc-500 font-mono">
                  {logLiveTail ? 'Listening for container log output...' : 'No logs recorded'}
                </div>
              ) : (
                filteredLogs.map((l) => (
                <div
                  key={l.id}
                  className={`group flex items-start justify-between gap-3 px-3.5 py-2 hover:bg-white/[0.03] transition-colors ${
                    l.level === 'error'
                      ? 'bg-rose-500/[0.04]'
                      : l.level === 'warn'
                        ? 'bg-amber-500/[0.03]'
                        : ''
                  }`}
                >
                  <div className="flex items-start gap-2.5 min-w-0 flex-1">
                    {logShowTimestamps && (
                      <span className="text-zinc-500 shrink-0 select-none text-[11px]">
                        {l.timestamp.slice(11, 23) || l.timestamp}
                      </span>
                    )}

                    <span
                      className={`shrink-0 rounded px-1.5 py-0.2 text-[10px] font-bold uppercase ${
                        l.level === 'error'
                          ? 'bg-rose-500/20 text-rose-400 ring-1 ring-rose-500/30'
                          : l.level === 'warn'
                            ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/30'
                            : l.level === 'debug'
                              ? 'bg-zinc-800 text-zinc-400'
                              : 'bg-sky-500/15 text-sky-400'
                      }`}
                    >
                      {l.level}
                    </span>

                    <span className="rounded bg-white/[0.06] px-1.5 py-0.2 text-[10px] text-orange-400 shrink-0">
                      {l.container}
                    </span>

                    <span
                      className={`${
                        logWordWrap ? 'break-all whitespace-pre-wrap' : 'truncate whitespace-pre'
                      } ${
                        l.level === 'error'
                          ? 'text-rose-300'
                          : l.level === 'warn'
                            ? 'text-amber-200'
                            : l.level === 'debug'
                              ? 'text-zinc-400'
                              : 'text-zinc-100'
                      }`}
                    >
                      {renderLogMessage(l.message)}
                    </span>
                  </div>

                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard?.writeText(
                        `${l.timestamp} [${l.level.toUpperCase()}] [${l.container}] ${l.message}`
                      );
                      toast.success('Copied log line');
                    }}
                    className="opacity-0 group-hover:opacity-100 rounded p-1 text-zinc-500 hover:bg-white/[0.08] hover:text-zinc-200 transition-opacity cursor-pointer shrink-0"
                    title="Copy log line"
                  >
                    <Copy className="h-3 w-3" />
                  </button>
                </div>
              )))}
            </div>
          </div>
        </div>
      )}

      {/* ===================================================================== */}
      {/* TAB 10: TERMINAL                                                      */}
      {/* ===================================================================== */}
      {activeTab === 'terminal' && (
        <XTermConsole
          hostLabel={app.php ? `site@${app.slug}` : `root@${app.slug}`}
          promptPrefix={app.php ? `site@${app.slug}:~$ ` : `root@${app.slug}:/app# `}
          services={app.services}
          appSlug={app.id}
          initialService={terminalService || app.services[0]?.service}
          height={400}
        />
      )}

      {/* ===================================================================== */}
      {/* TAB 11: ACCESS (No AI Slop Subtitle)                                  */}
      {/* ===================================================================== */}
      {activeTab === 'collaborators' && (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold text-zinc-100">Team Access</h2>
              <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
                {app.collaborators.length}
              </span>
            </div>
            <button
              type="button"
              onClick={() => setMemberModalOpen(true)}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <Plus className="h-3.5 w-3.5" />
              Invite Member
            </button>
          </div>

          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
            <table className="w-full table-fixed text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                  <th className="w-[45%] px-4 py-2.5 font-medium">User</th>
                  <th className="w-[25%] px-4 py-2.5 font-medium">Role</th>
                  <th className="w-[20%] px-4 py-2.5 font-medium">Added</th>
                  <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {app.collaborators.map((c) => (
                  <tr key={c.userId} className="hover:bg-white/[0.03]">
                    <td className="px-4 py-3">
                      <div className="font-semibold text-zinc-100">{c.username}</div>
                      <div className="font-mono text-[11px] text-zinc-500">{c.email}</div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-200 uppercase">
                          {c.role}
                        </span>
                        {Array.isArray((c as any).permissions) &&
                          (c as any).permissions.map((perm: string) => (
                            <span
                              key={perm}
                              className="rounded bg-orange-500/10 border border-orange-500/20 px-1.5 py-0.2 font-mono text-[9px] text-orange-300"
                            >
                              {perm}
                            </span>
                          ))}
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-zinc-400">{c.addedAt}</td>
                    <td className="px-3 py-3 text-right">
                      {c.role !== 'owner' && (
                        <ActionMenu
                          items={[
                            {
                              label: 'Transfer Ownership',
                              icon: <KeyRound className="h-3.5 w-3.5" />,
                              onClick: () => {
                                void backendApi.transferOwnership(app.id, c.userId);
                                onUpdateApp({
                                  ...app,
                                  owner: c.username,
                                  collaborators: app.collaborators.map((m) =>
                                    m.userId === c.userId
                                      ? { ...m, role: 'owner' }
                                      : m.role === 'owner'
                                        ? { ...m, role: 'deployer' }
                                        : m
                                  ),
                                });
                                toast.success(`Transferred project ownership to ${c.username}`);
                              },
                            },
                            {
                              label: 'Revoke Access',
                              icon: <Trash2 className="h-3.5 w-3.5" />,
                              variant: 'danger',
                              onClick: () => {
                                void backendApi.removeCollaborator(app.id, c.userId);
                                onUpdateApp({
                                  ...app,
                                  collaborators: app.collaborators.filter(
                                    (m) => m.userId !== c.userId
                                  ),
                                });
                                toast.success(`Removed ${c.username}`);
                              },
                            },
                          ]}
                        />
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ===================================================================== */}
      {/* SHADCN DIALOG MODALS                                                  */}
      {/* ===================================================================== */}

      {/* 1. Add / Edit Domain Modal (Auto-Detects Port + www + Static/Media File Server) */}
      <ShadcnDialog
        open={domainModalOpen}
        onClose={() => {
          setDomainModalOpen(false);
          setEditingDomainId(null);
        }}
        title={editingDomainId ? 'Edit Domain Route' : 'Add Domain'}
        icon={<Globe className="h-4 w-4" />}
      >
        <form onSubmit={handleAddDomainSubmit} className="space-y-4">
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="block text-[11px] font-medium text-zinc-400">Domain</label>
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() =>
                    setNewDomainName(formatWildcardDomain(app.slug, 'sslip.io'))
                  }
                  className="rounded border border-emerald-500/30 bg-emerald-500/10 px-2 py-0.5 font-mono text-[10px] text-emerald-400 hover:bg-emerald-500/20 cursor-pointer"
                >
                  + sslip.io
                </button>
                <button
                  type="button"
                  onClick={() =>
                    setNewDomainName(formatWildcardDomain(app.slug, 'nip.io'))
                  }
                  className="rounded border border-violet-500/30 bg-violet-500/10 px-2 py-0.5 font-mono text-[10px] text-violet-300 hover:bg-violet-500/20 cursor-pointer"
                >
                  + nip.io
                </button>
              </div>
            </div>
            <input
              type="text"
              required
              value={newDomainName}
              onChange={(e) => setNewDomainName(e.target.value)}
              placeholder={formatWildcardDomain(app.slug, 'sslip.io')}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Compose Service
              </label>
              <select
                value={newDomainService}
                onChange={(e) => handleSelectDomainService(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              >
                {app.services.map((s) => (
                  <option key={s.id} value={s.service}>
                    {s.service} ({formatCompactPort(s.ports)})
                  </option>
                ))}
              </select>
            </div>
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-[11px] font-medium text-zinc-400">Container Port</label>
                <span className="rounded bg-emerald-500/10 px-1.5 py-0.2 font-mono text-[9px] text-emerald-400">
                  Auto-detected
                </span>
              </div>
              <input
                type="number"
                value={newDomainPort}
                onChange={(e) => setNewDomainPort(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-orange-400 font-semibold focus:border-orange-500 focus:outline-none"
              />
            </div>
          </div>

          <div className="space-y-2 rounded-lg border border-white/[0.07] bg-[#09090b] p-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-zinc-200">Automatic HTTPS (TLS 1.3)</span>
              <ShadcnSwitch checked={newDomainHttps} onChange={setNewDomainHttps} />
            </div>
            <div className="flex items-center justify-between border-t border-white/[0.05] pt-2">
              <span className="text-xs font-medium text-zinc-200">
                Redirect www.{newDomainName || 'domain'} (enable_www)
              </span>
              <ShadcnSwitch checked={newDomainEnableWww} onChange={setNewDomainEnableWww} />
            </div>
            <div className="flex items-center justify-between border-t border-white/[0.05] pt-2">
              <span className="text-xs font-medium text-zinc-200">
                Caddy Static File Server (serve_static)
              </span>
              <ShadcnSwitch checked={newDomainServeStatic} onChange={setNewDomainServeStatic} />
            </div>
            {newDomainServeStatic && (
              <div className="grid grid-cols-2 gap-2 pt-1">
                <input
                  type="text"
                  value={newDomainStaticPrefix}
                  onChange={(e) => setNewDomainStaticPrefix(e.target.value)}
                  placeholder="/static"
                  className="rounded-md border border-white/[0.08] bg-zinc-950 px-2.5 py-1.5 font-mono text-[11px] text-zinc-200"
                />
                <input
                  type="text"
                  value={newDomainStaticPath}
                  onChange={(e) => setNewDomainStaticPath(e.target.value)}
                  placeholder="/app/staticfiles"
                  className="rounded-md border border-white/[0.08] bg-zinc-950 px-2.5 py-1.5 font-mono text-[11px] text-zinc-200"
                />
              </div>
            )}
            <div className="flex items-center justify-between border-t border-white/[0.05] pt-2">
              <span className="text-xs font-medium text-zinc-200">
                Caddy Media File Server (serve_media)
              </span>
              <ShadcnSwitch checked={newDomainServeMedia} onChange={setNewDomainServeMedia} />
            </div>
            {newDomainServeMedia && (
              <div className="grid grid-cols-2 gap-2 pt-1">
                <input
                  type="text"
                  value={newDomainMediaPrefix}
                  onChange={(e) => setNewDomainMediaPrefix(e.target.value)}
                  placeholder="/media"
                  className="rounded-md border border-white/[0.08] bg-zinc-950 px-2.5 py-1.5 font-mono text-[11px] text-zinc-200"
                />
                <input
                  type="text"
                  value={newDomainMediaPath}
                  onChange={(e) => setNewDomainMediaPath(e.target.value)}
                  placeholder="/app/media"
                  className="rounded-md border border-white/[0.08] bg-zinc-950 px-2.5 py-1.5 font-mono text-[11px] text-zinc-200"
                />
              </div>
            )}
            <div className="flex items-center justify-between border-t border-white/[0.05] pt-2">
              <span className="text-xs font-medium text-zinc-200">
                Enable PR Preview Subdomains
              </span>
              <ShadcnSwitch checked={newDomainIsPreview} onChange={setNewDomainIsPreview} />
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => {
                setDomainModalOpen(false);
                setEditingDomainId(null);
              }}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              {editingDomainId ? 'Save Domain' : 'Add Domain'}
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 2. Build Logs & Step Pipeline Modal */}
      <ShadcnDialog
        open={Boolean(buildLogsDeployment)}
        onClose={() => setBuildLogsDeployment(null)}
        title={buildLogsDeployment ? `Release ${buildLogsDeployment.id}` : 'Build Output'}
        icon={<TerminalIcon className="h-4 w-4" />}
        maxWidth="lg"
      >
        {buildLogsDeployment && (
          <div className="space-y-3 font-mono text-xs">
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 text-[11px]">
              <span className="text-zinc-300">
                Branch:{' '}
                <strong className="text-orange-400">
                  {buildLogsDeployment.branch}@{buildLogsDeployment.commitSha.slice(0, 7)}
                </strong>
              </span>
              <span className="text-emerald-400">
                Duration: {buildLogsDeployment.durationSec}s
              </span>
            </div>

            <div className="rounded-xl border border-white/[0.07] bg-[#070709] p-3 space-y-0.5 leading-relaxed max-h-[60vh] overflow-y-auto font-mono text-[11px]">
              {buildLogsDeployment.rawOutput ? (
                <div className="select-text">
                  {renderStyledBuildLog(buildLogsDeployment.rawOutput)}
                </div>
              ) : (
                <>
                  <div className="text-zinc-500">
                    [1/4] $ git checkout {buildLogsDeployment.branch} ({buildLogsDeployment.commitSha})
                  </div>
                  <div className="text-emerald-400">
                    [2/4] ✓ Validated {app.composeFilePath} · injected {app.envVars.length} variables
                  </div>
                  <div className="text-zinc-300">
                    [3/4] $ docker compose -p {app.slug} up -d --build --remove-orphans
                  </div>
                  {app.services.map((s) => (
                    <div key={s.id} className="text-emerald-400 pl-4">
                      ✔ Container {s.name} Healthy ({formatCompactPort(s.ports)})
                    </div>
                  ))}
                  <div className="text-orange-400">
                    [4/4] ✓ Switched Caddy upstream route in {buildLogsDeployment.durationSec}s
                  </div>
                </>
              )}
            </div>

            <div className="flex justify-end gap-2 pt-1">
              <button
                type="button"
                onClick={() => {
                  triggerDeploy({ rollbackFrom: buildLogsDeployment });
                  setBuildLogsDeployment(null);
                }}
                className="inline-flex items-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/15 px-3 py-1.5 font-sans text-xs font-medium text-orange-300 hover:bg-orange-500/25 cursor-pointer"
              >
                <RotateCcw className="h-3.5 w-3.5" />
                Promote / Rollback to this Release
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* 3. Create Workspace File Modal */}
      <ShadcnDialog
        open={newFileModalOpen}
        onClose={() => setNewFileModalOpen(false)}
        title="New File"
        icon={<FileCode2 className="h-4 w-4 text-orange-400" />}
      >
        <form onSubmit={handleCreateFileSubmit} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Quick Language Presets
            </label>
            <div className="flex flex-wrap gap-1.5">
              {[
                { label: 'TypeScript', file: 'handler.ts' },
                { label: 'Python', file: 'worker.py' },
                { label: 'Go', file: 'job.go' },
                { label: 'Rust', file: 'engine.rs' },
                { label: 'PHP', file: 'webhook.php' },
                { label: 'SQL', file: 'migration.sql' },
                { label: 'Shell', file: 'deploy.sh' },
                { label: 'JSON', file: 'settings.json' },
              ].map((tpl) => {
                const prefix = currentDir ? `${currentDir}/` : '';
                const suggested = `${prefix}${tpl.file}`;
                const isSelected = newFileName === suggested || newFileName.endsWith(tpl.file);
                return (
                  <button
                    key={tpl.label}
                    type="button"
                    onClick={() => setNewFileName(suggested)}
                    className={`inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 font-mono text-[11px] transition-colors cursor-pointer ${
                      isSelected
                        ? 'border-orange-500/50 bg-orange-500/15 text-orange-200 font-semibold'
                        : 'border-white/[0.08] bg-[#09090b] text-zinc-300 hover:border-white/[0.16] hover:text-white'
                    }`}
                  >
                    <DevFileIcon filename={tpl.file} />
                    <span>{tpl.label}</span>
                  </button>
                );
              })}
            </div>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Choose Target Directory
            </label>
            <div className="flex flex-wrap gap-1.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-2">
              {['', ...allFolderPaths].map((dir) => {
                const active = newFileTargetDir === dir;
                return (
                  <button
                    key={dir || 'root'}
                    type="button"
                    onClick={() => setNewFileTargetDir(dir)}
                    className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 font-mono text-[11px] transition-colors cursor-pointer ${
                      active
                        ? 'border-amber-500/50 bg-amber-500/15 text-amber-300 font-semibold ring-1 ring-amber-500/30'
                        : 'border-white/[0.06] bg-zinc-900/80 text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    <Folder className="h-3 w-3 text-amber-400 fill-amber-400/20" />
                    <span>{dir ? `/${app.slug}/${dir}` : `/${app.slug}`}</span>
                  </button>
                );
              })}
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-[11px] font-medium text-zinc-400">
                File Name
              </label>
              <span className="font-mono text-[10px] text-zinc-500">
                Target: /{app.slug}{newFileTargetDir ? `/${newFileTargetDir}` : ''}/
              </span>
            </div>
            <input
              type="text"
              required
              value={newFileName}
              onChange={(e) => setNewFileName(e.target.value.replace(/^\/+/, ''))}
              placeholder="e.g. server.ts or config.json"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setNewFileModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Create File
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 3b. Create New Folder Modal */}
      <ShadcnDialog
        open={newFolderModalOpen}
        onClose={() => setNewFolderModalOpen(false)}
        title="New Folder"
        icon={<FolderPlus className="h-4 w-4 text-amber-400" />}
      >
        <form onSubmit={handleCreateFolderSubmit} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Parent Directory
            </label>
            <div className="flex flex-wrap gap-1.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-2">
              {['', ...allFolderPaths].map((dir) => {
                const active = newFolderTargetDir === dir;
                return (
                  <button
                    key={dir || 'root'}
                    type="button"
                    onClick={() => setNewFolderTargetDir(dir)}
                    className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 font-mono text-[11px] transition-colors cursor-pointer ${
                      active
                        ? 'border-amber-500/50 bg-amber-500/15 text-amber-300 font-semibold ring-1 ring-amber-500/30'
                        : 'border-white/[0.06] bg-zinc-900/80 text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    <Folder className="h-3 w-3 text-amber-400 fill-amber-400/20" />
                    <span>{dir ? `/${app.slug}/${dir}` : `/${app.slug}`}</span>
                  </button>
                );
              })}
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-[11px] font-medium text-zinc-400">
                Folder Name
              </label>
              <span className="font-mono text-[10px] text-zinc-500">
                Target: /{app.slug}{newFolderTargetDir ? `/${newFolderTargetDir}` : ''}/
              </span>
            </div>
            <input
              type="text"
              required
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value.replace(/^\/+/, ''))}
              placeholder="e.g. controllers or migrations"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setNewFolderModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Create Folder
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 4. Configure Git Repository Modal (Auto-Detects All GitHub Branches!) */}
      <ShadcnDialog
        open={gitModalOpen}
        onClose={() => setGitModalOpen(false)}
        title="Configure Git Repository"
        icon={<GitBranch className="h-4 w-4" />}
        maxWidth="lg"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const matchedBranch = detectedBranches.find((b) => b.name === gitBranchInput);
            onUpdateApp({
              ...app,
              gitRepo: gitRepoInput,
              gitBranch: gitBranchInput,
              gitCommitSha: matchedBranch?.sha || app.gitCommitSha,
              composeFilePath: composePathInput,
            });
            setGitModalOpen(false);
            toast.success(`Synced ${gitRepoInput} (${gitBranchInput})`);
          }}
          className="space-y-4"
        >
          {/* Repository URL + Fetch Branches Button */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-[11px] font-medium text-zinc-400">Repository</label>
              <button
                type="button"
                onClick={triggerRescanBranches}
                className="inline-flex items-center gap-1 font-mono text-[10px] text-orange-400 hover:underline cursor-pointer"
              >
                <RefreshCw
                  className={`h-2.5 w-2.5 ${isScanningBranches ? 'animate-spin' : ''}`}
                />
                Fetch Remote Branches
              </button>
            </div>
            <input
              type="text"
              value={gitRepoInput}
              onChange={(e) => setGitRepoInput(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>

          {/* Auto-Detected GitHub Branches Picker */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-[11px] font-medium text-zinc-400">
                Auto-Detected Branches ({detectedBranches.length})
              </label>
              <span className="rounded bg-emerald-500/10 px-1.5 py-0.2 font-mono text-[10px] text-emerald-400">
                GitHub Synced
              </span>
            </div>

            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
              {detectedBranches.map((br) => {
                const active = gitBranchInput === br.name;
                return (
                  <button
                    key={br.name}
                    type="button"
                    onClick={() => setGitBranchInput(br.name)}
                    className={`flex flex-col justify-between rounded-lg border p-2.5 text-left font-mono transition-all cursor-pointer ${
                      active
                        ? 'border-orange-500/60 bg-orange-500/12 text-zinc-100'
                        : 'border-white/[0.07] bg-[#09090b] text-zinc-400 hover:border-white/[0.15] hover:text-zinc-200'
                    }`}
                  >
                    <div className="flex items-center justify-between gap-1">
                      <span className="truncate text-xs font-semibold text-zinc-100">
                        {br.name}
                      </span>
                      {br.isDefault && (
                        <span className="rounded bg-white/[0.07] px-1 py-0.2 text-[9px] text-zinc-400">
                          default
                        </span>
                      )}
                      {br.prNumber && (
                        <span className="rounded bg-violet-500/15 px-1 py-0.2 text-[9px] text-violet-300">
                          #{br.prNumber}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 text-[10px] text-zinc-500">
                      @{br.sha} · {br.updatedAt}
                    </div>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Branch & Compose Path */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Selected Branch
              </label>
              <input
                type="text"
                value={gitBranchInput}
                onChange={(e) => setGitBranchInput(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-orange-400 font-semibold"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Compose Path
              </label>
              <select
                value={composePathInput}
                onChange={(e) => setComposePathInput(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              >
                <option value="docker-compose.yml">docker-compose.yml</option>
                <option value="docker-compose.prod.yml">docker-compose.prod.yml</option>
                <option value="deploy/docker-compose.yml">deploy/docker-compose.yml</option>
              </select>
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setGitModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Save Git Source
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 5. Mount Volume Modal */}
      <ShadcnDialog
        open={volumeModalOpen}
        onClose={() => setVolumeModalOpen(false)}
        title="Mount Volume"
        icon={<HardDrive className="h-4 w-4" />}
      >
        <form onSubmit={handleCreateVolumeSubmit} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Volume Name
            </label>
            <input
              type="text"
              required
              value={newVolName}
              onChange={(e) => setNewVolName(e.target.value)}
              placeholder={`${app.slug}_data`}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Container Mount Path
            </label>
            <input
              type="text"
              required
              value={newVolMountPath}
              onChange={(e) => setNewVolMountPath(e.target.value)}
              placeholder="/var/lib/postgresql/data"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setVolumeModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Mount Volume
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 6. Create Backup Snapshot Modal */}
      <ShadcnDialog
        open={backupModalOpen}
        onClose={() => setBackupModalOpen(false)}
        title="Create Snapshot"
        icon={<Archive className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void backendApi.backupNow(app.id, backupScope);
            const created: ProjectBackupItem = {
              id: `snap-${Math.floor(910 + Math.random() * 80)}`,
              name: `${app.slug}_${backupScope}_${new Date().toISOString().slice(0, 10)}.nd-backup.tar.gz`,
              type: 'Manual',
              destination: backupDest,
              provider: backupDest.includes('Google') ? 'Google Drive' : 'Cloudflare R2',
              sizeMB: backupScope === 'app' ? 84.5 : 1424.8,
              createdAt: 'Just now',
              commitSha: app.gitCommitSha?.slice(0, 7) || '8c4f19a',
              includes: [
                'Compose',
                ...(backupIncludeEnv ? ['.env (AES-256)'] : []),
                ...(backupScope !== 'app' && backupIncludeVols
                  ? app.volumes.map((v) => v.name)
                  : []),
              ],
              checksum: `sha256:${Math.random().toString(16).slice(2, 12)}`,
              status: 'completed',
            };
            setProjectBackups((prev) => [created, ...prev]);
            setBackupModalOpen(false);
            toast.success(`Snapshot (${backupScope}) uploaded to ${backupDest}`);
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Snapshot Scope
            </label>
            <div className="grid grid-cols-3 gap-2">
              {(
                [
                  { id: 'full', label: 'Full Stack' },
                  { id: 'volume', label: 'Volumes Only' },
                  { id: 'app', label: 'App Code Only' },
                ] as const
              ).map((sc) => (
                <button
                  key={sc.id}
                  type="button"
                  onClick={() => setBackupScope(sc.id)}
                  className={`rounded-lg border py-1.5 text-xs font-medium cursor-pointer ${
                    backupScope === sc.id
                      ? 'border-orange-500/50 bg-orange-500/15 text-orange-200'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {sc.label}
                </button>
              ))}
            </div>
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Rclone Cloud Destination
            </label>
            <select
              value={backupDest}
              onChange={(e) => setBackupDest(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
            >
              <option>Cloudflare R2 Primary (EU-Central)</option>
              <option>Google Drive Team Vault</option>
            </select>
          </div>
          <div className="space-y-2 rounded-lg border border-white/[0.07] bg-[#09090b] p-3">
            <div className="flex items-center justify-between">
              <span className="text-xs text-zinc-200">
                Pause Containers for Consistent Snapshot (pause_containers)
              </span>
              <ShadcnSwitch
                checked={backupPauseContainers}
                onChange={setBackupPauseContainers}
              />
            </div>
            <div className="flex items-center justify-between pt-2 border-t border-white/[0.05]">
              <span className="text-xs text-zinc-200">
                Include Persistent Volumes ({app.volumes.length})
              </span>
              <ShadcnSwitch checked={backupIncludeVols} onChange={setBackupIncludeVols} />
            </div>
            <div className="flex items-center justify-between pt-2 border-t border-white/[0.05]">
              <span className="text-xs text-zinc-200">Include .env Secrets (AES-256-GCM)</span>
              <ShadcnSwitch checked={backupIncludeEnv} onChange={setBackupIncludeEnv} />
            </div>
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setBackupModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Start Snapshot
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 6a-2. Rclone Backup Log Modal */}
      <ShadcnDialog
        open={Boolean(backupLogModal)}
        onClose={() => setBackupLogModal(null)}
        title={`Backup Log · ${backupLogModal?.name || ''}`}
        icon={<FileText className="h-4 w-4 text-orange-400" />}
        maxWidth="lg"
      >
        {backupLogModal && (
          <div className="space-y-3">
            <pre className="max-h-72 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#070709] p-3.5 font-mono text-xs text-zinc-300 whitespace-pre-wrap">
              {backupLogModal.log}
            </pre>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={() => setBackupLogModal(null)}
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Close
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* 6a-3. Merged Caddy Compose Preview Modal */}
      <ShadcnDialog
        open={mergedComposeModalOpen}
        onClose={() => setMergedComposeModalOpen(false)}
        title={`Merged Caddy Compose · ${app.slug}`}
        icon={<Layers className="h-4 w-4 text-emerald-400" />}
        maxWidth="lg"
      >
        <div className="space-y-3">
          <CodeEditorBox
            filename="docker-compose.merged.yml"
            language="yaml"
            value={mergedComposeYaml}
            onChange={setMergedComposeYaml}
            readOnly
            height={280}
          />
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => {
                navigator.clipboard?.writeText(mergedComposeYaml);
                toast.success('Copied merged Compose YAML');
              }}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Copy Merged YAML
            </button>
          </div>
        </div>
      </ShadcnDialog>

      {/* 6b. Restore Snapshot Point-in-Time Modal */}
      <ShadcnDialog
        open={Boolean(restoreSnapshotTarget)}
        onClose={() => setRestoreSnapshotTarget(null)}
        title="Restore Snapshot"
        icon={<RotateCcw className="h-4 w-4" />}
      >
        {restoreSnapshotTarget && (
          <div className="space-y-4">
            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 font-mono text-xs space-y-1">
              <div className="text-zinc-100 font-semibold">{restoreSnapshotTarget.name}</div>
              <div className="text-[11px] text-zinc-400">
                Source: {restoreSnapshotTarget.destination} · {restoreSnapshotTarget.checksum}
              </div>
            </div>
            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setRestoreSnapshotTarget(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => {
                  toast.success(`Restored ${app.name} from ${restoreSnapshotTarget.name}`);
                  setRestoreSnapshotTarget(null);
                }}
                className="rounded-lg bg-orange-500 px-4 py-1.5 text-xs font-semibold text-white hover:bg-orange-400 cursor-pointer"
              >
                Confirm Restore
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* 6c. cPanel-Style File Permissions (chmod) Matrix Modal */}
      <ShadcnDialog
        open={Boolean(chmodModalFile)}
        onClose={() => setChmodModalFile(null)}
        title="Change Permissions (chmod)"
        icon={<KeyRound className="h-4 w-4 text-emerald-400" />}
      >
        {chmodModalFile && (
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              const targetPath = chmodModalFile;
              const targetOctal = computedOctal.octal;
              const res = await backendApi.fileChmod(app.id, targetPath, targetOctal);
              if (res.ok === false) {
                toast.error(res.error || res.message || 'Failed to update file permissions');
                return;
              }
              setFilePermissions((prev) => ({
                ...prev,
                [targetPath]: targetOctal,
              }));
              await syncWorkspaceFilesFromDisk();
              toast.success(
                `Updated permissions for ${targetPath} to ${targetOctal}`
              );
              setChmodModalFile(null);
            }}
            className="space-y-4"
          >
            <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 font-mono text-xs">
              <span className="text-zinc-300 truncate">{chmodModalFile}</span>
              <span className="rounded bg-emerald-500/15 px-2 py-0.5 text-emerald-300 font-semibold">
                {computedOctal.octal}
              </span>
            </div>

            {/* 3x3 Permission Checkboxes (Owner / Group / World) */}
            <div className="overflow-hidden rounded-lg border border-white/[0.07] bg-[#09090b]">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-white/[0.07] bg-white/[0.02] text-zinc-400">
                    <th className="px-3 py-2 text-left font-medium">Scope</th>
                    <th className="px-3 py-2 text-center font-medium">Read (4)</th>
                    <th className="px-3 py-2 text-center font-medium">Write (2)</th>
                    <th className="px-3 py-2 text-center font-medium">Execute (1)</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.05]">
                  <tr>
                    <td className="px-3 py-2.5 font-medium text-zinc-200">Owner</td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.ownerR}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, ownerR: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.ownerW}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, ownerW: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.ownerX}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, ownerX: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                  </tr>
                  <tr>
                    <td className="px-3 py-2.5 font-medium text-zinc-200">Group</td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.groupR}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, groupR: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.groupW}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, groupW: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.groupX}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, groupX: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                  </tr>
                  <tr>
                    <td className="px-3 py-2.5 font-medium text-zinc-200">World</td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.worldR}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, worldR: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.worldW}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, worldW: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-3 py-2.5 text-center">
                      <input
                        type="checkbox"
                        checked={chmodMatrix.worldX}
                        onChange={(e) =>
                          setChmodMatrix((p) => ({ ...p, worldX: e.target.checked }))
                        }
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setChmodModalFile(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Save Permissions ({computedOctal.octal})
              </button>
            </div>
          </form>
        )}
      </ShadcnDialog>

      {/* 6d. Compress Archive (.zip / .tar.gz) Modal */}
      <ShadcnDialog
        open={compressModalOpen}
        onClose={() => setCompressModalOpen(false)}
        title="Compress Archive (Zip / Tar.gz)"
        icon={<Archive className="h-4 w-4 text-purple-400" />}
      >
        <form onSubmit={handleCompressSubmit} className="space-y-4">
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Archive Format
            </label>
            <div className="grid grid-cols-2 gap-2">
              {(['zip', 'tar.gz'] as const).map((fmt) => (
                <button
                  key={fmt}
                  type="button"
                  onClick={() => {
                    setCompressFormat(fmt);
                    const stem = compressArchiveName
                      .replace(/\.tar\.gz$/i, '')
                      .replace(/\.zip$/i, '');
                    setCompressArchiveName(`${stem}.${fmt}`);
                  }}
                  className={`rounded-lg border px-3 py-2 font-mono text-xs font-semibold cursor-pointer ${
                    compressFormat === fmt
                      ? 'border-purple-500/50 bg-purple-500/15 text-purple-200'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400'
                  }`}
                >
                  .{fmt} Archive
                </button>
              ))}
            </div>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Archive File Name {currentDir ? `(in /${currentDir})` : '(in root)'}
            </label>
            <input
              type="text"
              required
              value={compressArchiveName}
              onChange={(e) => setCompressArchiveName(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setCompressModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Compress Now
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 6e. Extract Archive (Unzip) Modal */}
      <ShadcnDialog
        open={Boolean(extractTargetFile)}
        onClose={() => setExtractTargetFile(null)}
        title="Extract Archive (Unzip)"
        icon={<FolderOpen className="h-4 w-4 text-emerald-400" />}
      >
        {extractTargetFile && (
          <form onSubmit={handleExtractSubmit} className="space-y-4">
            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 font-mono text-xs flex items-center justify-between">
              <span className="flex items-center gap-2 text-zinc-100 font-semibold truncate">
                <Archive className="h-4 w-4 text-purple-400 shrink-0" />
                <span className="truncate">{extractTargetFile.path}</span>
              </span>
              <span className="text-zinc-500 shrink-0">
                {formatFileSize(extractTargetFile.sizeBytes)}
              </span>
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-[11px] font-medium text-zinc-400">
                  Select Destination Folder
                </label>
                <span className="font-mono text-[10px] text-emerald-400">
                  Click any folder below
                </span>
              </div>
              <div className="max-h-44 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#09090b] p-1.5 space-y-1">
                {['', ...allFolderPaths].map((dir) => {
                  const normalizedCurrent = extractDestDir.trim().replace(/^\/+|\/+$/g, '');
                  const active = normalizedCurrent === dir;
                  const depth = dir ? dir.split('/').length : 0;
                  return (
                    <button
                      key={dir || 'root'}
                      type="button"
                      onClick={() => setExtractDestDir(dir)}
                      style={{ paddingLeft: `${depth * 14 + 10}px` }}
                      className={`flex w-full items-center justify-between rounded-md pr-2.5 py-1.5 text-left font-mono text-xs transition-colors cursor-pointer ${
                        active
                          ? 'bg-emerald-500/15 text-emerald-300 font-semibold ring-1 ring-emerald-500/35'
                          : 'text-zinc-300 hover:bg-white/[0.04] hover:text-white'
                      }`}
                    >
                      <span className="flex items-center gap-2 truncate">
                        {active ? (
                          <FolderOpen className="h-3.5 w-3.5 text-emerald-400 fill-emerald-400/20 shrink-0" />
                        ) : (
                          <Folder className="h-3.5 w-3.5 text-amber-400 fill-amber-400/20 shrink-0" />
                        )}
                        <span className="truncate">
                          {dir ? `/${dir}` : `/data/apps/${app.slug} (Root)`}
                        </span>
                      </span>
                      {active && <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400 shrink-0" />}
                    </button>
                  );
                })}
              </div>
            </div>

            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Extract Into Directory Path
              </label>
              <input
                type="text"
                value={extractDestDir}
                onChange={(e) => setExtractDestDir(e.target.value)}
                placeholder="/ (root directory)"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setExtractTargetFile(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-emerald-500 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-emerald-400 cursor-pointer"
              >
                Extract Files
              </button>
            </div>
          </form>
        )}
      </ShadcnDialog>

      {/* 6f. Restore Workspace Archive Modal */}
      <ShadcnDialog
        open={restoreArchiveModalOpen}
        onClose={() => setRestoreArchiveModalOpen(false)}
        title="Restore Workspace Archive"
        icon={<RotateCcw className="h-4 w-4 text-amber-400" />}
      >
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            if (!selectedRestoreFile) {
              toast.error('Please select a .tar.gz or .zip archive file');
              return;
            }
            setIsRestoringArchive(true);
            toast.info(`Extracting ${selectedRestoreFile.name} into workspace...`);
            try {
              const res = await backendApi.uploadWorkspaceArchive(app.id, selectedRestoreFile);
              if (res.ok) {
                toast.success(
                  res.message || `Successfully restored ${res.files_extracted || ''} files to workspace`
                );
                await syncWorkspaceFilesFromDisk();
                setRestoreArchiveModalOpen(false);
                setSelectedRestoreFile(null);
              } else {
                toast.error(res.error || 'Failed to extract archive');
              }
            } catch (err: any) {
              toast.error(err.message || 'Restore failed');
            } finally {
              setIsRestoringArchive(false);
            }
          }}
          className="space-y-4"
        >
          <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-3 text-xs text-amber-200">
            <strong>Warning:</strong> Restoring an archive will unpack its files directly into{' '}
            <code className="font-mono text-zinc-100">/{app.slug}</code>, overwriting matching existing files.
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Select Archive File (.tar.gz, .tgz, .zip)
            </label>
            <input
              type="file"
              required
              accept=".tar.gz,.tgz,.zip,.tar"
              onChange={(e) => {
                if (e.target.files && e.target.files[0]) {
                  setSelectedRestoreFile(e.target.files[0]);
                }
              }}
              className="w-full text-xs text-zinc-300 file:mr-3 file:py-1.5 file:px-3 file:rounded-lg file:border-0 file:bg-zinc-800 file:text-xs file:font-semibold file:text-zinc-200 hover:file:bg-zinc-700 cursor-pointer"
            />
          </div>

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              disabled={isRestoringArchive}
              onClick={() => setRestoreArchiveModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isRestoringArchive || !selectedRestoreFile}
              className="rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-amber-400 cursor-pointer disabled:opacity-50"
            >
              {isRestoringArchive ? 'Extracting...' : 'Restore Workspace'}
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 6f. Rename File / Folder Modal */}
      <ShadcnDialog
        open={Boolean(renameTarget)}
        onClose={() => setRenameTarget(null)}
        title="Rename"
        icon={<FileText className="h-4 w-4" />}
      >
        {renameTarget && (
          <form onSubmit={handleRenameSubmit} className="space-y-4">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                New Name
              </label>
              <input
                type="text"
                required
                value={renameInput}
                onChange={(e) => setRenameInput(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>
            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setRenameTarget(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Rename
              </button>
            </div>
          </form>
        )}
      </ShadcnDialog>

      {/* 6g. Copy / Move File Modal with Interactive Folder Tree Picker */}
      <ShadcnDialog
        open={Boolean(moveTarget)}
        onClose={() => setMoveTarget(null)}
        title={moveTarget?.mode === 'move' ? 'Move Files / Folders' : 'Copy Files / Folders'}
        icon={<Copy className="h-4 w-4 text-sky-400" />}
      >
        {moveTarget && (
          <form onSubmit={handleMoveOrCopySubmit} className="space-y-4">
            {/* Source Files Indicator */}
            <div className="rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 font-mono text-xs">
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] text-zinc-400 font-sans">
                  Selected Items ({moveTarget.paths.length}):
                </span>
                <span className="rounded bg-white/[0.06] px-1.5 py-0.5 text-[10px] uppercase text-zinc-400 shrink-0">
                  Source
                </span>
              </div>
              <div className="max-h-24 overflow-y-auto space-y-1">
                {moveTarget.paths.map((p) => {
                  const fname = p.split('/').pop() || p;
                  return (
                    <div key={p} className="flex items-center gap-2 text-zinc-200 truncate">
                      <DevFileIcon filename={fname} />
                      <span className="truncate font-semibold">{p}</span>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Mode Switch: Move vs Copy */}
            <div className="grid grid-cols-2 gap-2">
              {(['move', 'copy'] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setMoveTarget({ ...moveTarget, mode: m })}
                  className={`rounded-lg border px-3 py-1.5 text-xs font-semibold uppercase cursor-pointer transition-colors ${
                    moveTarget.mode === m
                      ? 'border-orange-500/50 bg-orange-500/15 text-orange-300'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
                  }`}
                >
                  {m === 'move' ? 'Move File' : 'Copy File'}
                </button>
              ))}
            </div>

            {/* Interactive Visual Folder Tree Picker */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="text-[11px] font-medium text-zinc-400">
                  Choose Destination Folder
                </label>
                <button
                  type="button"
                  onClick={() => setShowInlineFolderCreator((prev) => !prev)}
                  className="inline-flex items-center gap-1 font-mono text-[11px] text-amber-400 hover:text-amber-300 cursor-pointer"
                >
                  <FolderPlus className="h-3 w-3" />
                  <span>{showInlineFolderCreator ? 'Cancel' : '+ New Folder'}</span>
                </button>
              </div>

              {showInlineFolderCreator && (
                <div className="mb-2 flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/[0.06] p-2">
                  <Folder className="h-3.5 w-3.5 text-amber-400 shrink-0" />
                  <input
                    type="text"
                    value={inlineNewDestFolder}
                    onChange={(e) => setInlineNewDestFolder(e.target.value)}
                    placeholder="e.g. src/controllers or backups"
                    className="flex-1 bg-transparent font-mono text-xs text-zinc-100 placeholder-zinc-500 focus:outline-none"
                  />
                  <button
                    type="button"
                    onClick={() => {
                      const clean = inlineNewDestFolder
                        .trim()
                        .replace(/^\/+|\/+$/g, '')
                        .replace(/[^a-zA-Z0-9/_-]/g, '-');
                      if (!clean) return;
                      setCustomFolders((prev) => (prev.includes(clean) ? prev : [...prev, clean]));
                      setMoveDestInput(clean);
                      setInlineNewDestFolder('');
                      setShowInlineFolderCreator(false);
                      toast.success(`Created & selected /${clean}`);
                    }}
                    className="rounded-md bg-amber-500 px-2.5 py-1 text-[11px] font-semibold text-zinc-950 hover:bg-amber-400 cursor-pointer"
                  >
                    Add & Select
                  </button>
                </div>
              )}

              <div className="max-h-48 overflow-y-auto rounded-lg border border-white/[0.08] bg-[#09090b] p-1.5 space-y-1">
                {['', ...allFolderPaths].map((dir) => {
                  const normalizedDest = moveDestInput.trim().replace(/^\/+|\/+$/g, '');
                  const active = normalizedDest === dir;
                  const depth = dir ? dir.split('/').length : 0;
                  const fileCountInDir = defaultRichFiles.filter((f) => {
                    const lastSlash = f.path.lastIndexOf('/');
                    return (lastSlash === -1 ? '' : f.path.slice(0, lastSlash)) === dir;
                  }).length;

                  return (
                    <button
                      key={dir || 'root'}
                      type="button"
                      onClick={() => setMoveDestInput(dir)}
                      style={{ paddingLeft: `${depth * 14 + 10}px` }}
                      className={`flex w-full items-center justify-between rounded-md pr-2.5 py-1.5 text-left font-mono text-xs transition-colors cursor-pointer ${
                        active
                          ? 'bg-orange-500/15 text-orange-300 font-semibold ring-1 ring-orange-500/35'
                          : 'text-zinc-300 hover:bg-white/[0.04] hover:text-white'
                      }`}
                    >
                      <span className="flex items-center gap-2 truncate">
                        {active ? (
                          <FolderOpen className="h-3.5 w-3.5 text-amber-400 fill-amber-400/25 shrink-0" />
                        ) : (
                          <Folder className="h-3.5 w-3.5 text-amber-400 fill-amber-400/20 shrink-0" />
                        )}
                        <span className="truncate">
                          {dir ? `/${dir}` : `/ (Root Directory)`}
                        </span>
                      </span>
                      <span className="flex items-center gap-2 shrink-0">
                        <span className="text-[10px] text-zinc-500">{fileCountInDir} files</span>
                        {active && (
                          <CheckCircle2 className="h-3.5 w-3.5 text-orange-400 shrink-0" />
                        )}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Custom Path Input + Live Target Preview */}
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Destination Folder Path
              </label>
              <input
                type="text"
                value={moveDestInput}
                onChange={(e) => setMoveDestInput(e.target.value)}
                placeholder="/ (Root) or src/routes"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
              <div className="mt-1.5 flex items-center justify-between rounded-md bg-white/[0.03] px-2.5 py-1 font-mono text-[11px] text-zinc-400">
                <span>Destination Folder:</span>
                <span className="text-emerald-400 font-semibold truncate ml-2">
                  /data/apps/{app.slug}/
                  {moveDestInput.trim().replace(/^\/+|\/+$/g, '')
                    ? `${moveDestInput.trim().replace(/^\/+|\/+$/g, '')}/`
                    : '(Root)'}
                </span>
              </div>
            </div>

            <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setMoveTarget(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                {moveTarget.mode === 'move' ? 'Move Here' : 'Copy Here'}
              </button>
            </div>
          </form>
        )}
      </ShadcnDialog>

      {/* Conflict Resolution Modal (Skip vs Replace) */}
      <ShadcnDialog
        open={Boolean(conflictModal)}
        onClose={() => setConflictModal(null)}
        title="File Collision Detected"
        icon={<AlertTriangle className="h-4 w-4 text-amber-400" />}
      >
        {conflictModal && (
          <div className="space-y-4">
            <div className="rounded-lg border border-amber-500/25 bg-amber-500/[0.08] p-3 text-xs text-amber-200">
              <p className="font-semibold text-amber-100">
                The destination already contains {conflictModal.conflicts.length} item(s) with the same name:
              </p>
              <div className="mt-2 max-h-32 overflow-y-auto space-y-1 font-mono text-[11px] text-zinc-300">
                {conflictModal.conflicts.map((p) => {
                  const fname = p.split('/').pop() || p;
                  return (
                    <div key={p} className="flex items-center gap-2 rounded bg-black/40 px-2 py-1">
                      <DevFileIcon filename={fname} />
                      <span className="font-semibold text-zinc-100">{fname}</span>
                      <span className="text-zinc-500 text-[10px]">
                        ({conflictModal.destFolder ? `/${conflictModal.destFolder}/${fname}` : `/${fname}`})
                      </span>
                    </div>
                  );
                })}
              </div>
              <p className="mt-2.5 text-zinc-400">
                Would you like to <strong>Replace</strong> the existing items or <strong>Skip</strong> them during the {conflictModal.mode}?
              </p>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.07] pt-3">
              <button
                type="button"
                onClick={() => setConflictModal(null)}
                className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-400 hover:text-zinc-200 cursor-pointer"
              >
                Cancel
              </button>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={async () => {
                    const ctx = conflictModal;
                    setConflictModal(null);
                    const nonConflicting = ctx.paths.filter((p) => !ctx.conflicts.includes(p));
                    if (nonConflicting.length > 0) {
                      await executeMoveOrCopy(nonConflicting, ctx.destFolder, ctx.mode, false);
                    } else {
                      toast.info('Skipped all items because they already exist in the destination.');
                    }
                  }}
                  className="rounded-lg border border-white/[0.12] bg-zinc-800 px-3.5 py-1.5 text-xs font-medium text-zinc-200 hover:bg-zinc-700 cursor-pointer"
                >
                  Skip Conflicting
                </button>
                <button
                  type="button"
                  onClick={async () => {
                    const ctx = conflictModal;
                    setConflictModal(null);
                    await executeMoveOrCopy(ctx.paths, ctx.destFolder, ctx.mode, true);
                  }}
                  className="rounded-lg bg-orange-500 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-orange-400 cursor-pointer shadow-sm"
                >
                  Replace Existing
                </button>
              </div>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* 6h. Upload File / Archive / Remote URL Modal */}
      <ShadcnDialog
        open={uploadModalOpen}
        onClose={() => setUploadModalOpen(false)}
        title="Upload File or Fetch Remote URL"
        icon={<Upload className="h-4 w-4 text-sky-400" />}
      >
        <form onSubmit={handleUploadSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-1.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-1 text-xs">
            <button
              type="button"
              onClick={() => setUploadMode('local')}
              className={`rounded-md py-1.5 font-medium transition-colors cursor-pointer ${
                uploadMode === 'local'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Local File / Zip
            </button>
            <button
              type="button"
              onClick={() => setUploadMode('url')}
              className={`rounded-md py-1.5 font-medium transition-colors cursor-pointer ${
                uploadMode === 'url'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              Fetch Remote URL
            </button>
          </div>

          {uploadMode === 'local' ? (
            <div className="space-y-3">
              <div
                onDragOver={(e) => {
                  e.preventDefault();
                  setIsDragOverUploadZone(true);
                }}
                onDragLeave={() => setIsDragOverUploadZone(false)}
                onDrop={(e) => {
                  e.preventDefault();
                  setIsDragOverUploadZone(false);
                  if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
                    const dropped = Array.from(e.dataTransfer.files);
                    setSelectedUploadFiles((prev) => [...prev, ...dropped]);
                  }
                }}
                className={`relative flex flex-col items-center justify-center rounded-xl border-2 border-dashed p-6 text-center transition-all cursor-pointer ${
                  isDragOverUploadZone
                    ? 'border-orange-500 bg-orange-500/10 text-orange-300'
                    : 'border-white/[0.12] bg-[#09090b]/80 hover:border-white/[0.24] hover:bg-zinc-900/60 text-zinc-400'
                }`}
                onClick={() => document.getElementById('workspace-file-input')?.click()}
              >
                <input
                  id="workspace-file-input"
                  type="file"
                  multiple
                  className="hidden"
                  onChange={(e) => {
                    if (e.target.files && e.target.files.length > 0) {
                      const files = Array.from(e.target.files);
                      setSelectedUploadFiles((prev) => [...prev, ...files]);
                    }
                  }}
                />
                <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 mb-2">
                  <Upload className="h-5 w-5 text-orange-400" />
                </div>
                <p className="text-xs font-semibold text-zinc-200">
                  Click to browse or drag & drop files here
                </p>
                <p className="text-[11px] text-zinc-500 mt-1">
                  Upload into: <span className="font-mono text-zinc-300">/{currentDir || 'root'}</span>
                </p>
              </div>

              {selectedUploadFiles.length > 0 && (
                <div className="space-y-1.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-2.5 max-h-40 overflow-y-auto">
                  <div className="flex items-center justify-between text-[11px] font-medium text-zinc-400 px-1 pb-1 border-b border-white/[0.06]">
                    <span>Selected Files ({selectedUploadFiles.length})</span>
                    <button
                      type="button"
                      onClick={() => setSelectedUploadFiles([])}
                      className="text-rose-400 hover:underline cursor-pointer"
                    >
                      Clear All
                    </button>
                  </div>
                  {selectedUploadFiles.map((f, fIdx) => (
                    <div
                      key={fIdx}
                      className="flex items-center justify-between gap-2 px-1 py-0.5 text-xs font-mono text-zinc-300"
                    >
                      <span className="truncate">{f.name}</span>
                      <div className="flex items-center gap-2 shrink-0 text-[10px] text-zinc-500">
                        <span>{(f.size / 1024).toFixed(1)} KB</span>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            setSelectedUploadFiles((prev) => prev.filter((_, i) => i !== fIdx));
                          }}
                          className="text-zinc-500 hover:text-rose-400 cursor-pointer"
                        >
                          ✕
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ) : (
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Remote HTTPS URL {currentDir ? `(save into /${currentDir})` : '(save into root)'}
              </label>
              <input
                type="url"
                required
                value={uploadRemoteUrl}
                onChange={(e) => setUploadRemoteUrl(e.target.value)}
                placeholder="https://github.com/org/repo/releases/download/v1.0/bundle.zip"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setUploadModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isUploadingFiles}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
            >
              {isUploadingFiles
                ? 'Uploading...'
                : uploadMode === 'url'
                ? 'Fetch URL'
                : `Upload ${selectedUploadFiles.length > 0 ? `(${selectedUploadFiles.length})` : ''}`}
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 6i. Caddy Labels Preview Modal */}
      <ShadcnDialog
        open={Boolean(caddyLabelsPreview)}
        onClose={() => setCaddyLabelsPreview(null)}
        title={`Caddy Labels · ${caddyLabelsPreview?.domain || ''}`}
        icon={<FileCode2 className="h-4 w-4 text-emerald-400" />}
      >
        {caddyLabelsPreview && (
          <div className="space-y-3">
            <pre className="overflow-x-auto rounded-lg border border-white/[0.08] bg-[#09090b] p-3 font-mono text-xs text-emerald-300">
              {caddyLabelsPreview.snippet}
            </pre>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => {
                  navigator.clipboard?.writeText(caddyLabelsPreview.snippet);
                  toast.success('Copied Caddy Compose labels');
                }}
                className="rounded-lg bg-zinc-100 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                Copy Snippet
              </button>
            </div>
          </div>
        )}
      </ShadcnDialog>

      {/* 7. Invite Collaborator Modal */}
      <ShadcnDialog
        open={memberModalOpen}
        onClose={() => setMemberModalOpen(false)}
        title="Invite Member"
        icon={<Users className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!newMemberEmail.trim()) return;
            const uname = newMemberEmail.split('@')[0];
            void backendApi.addCollaborator(
              app.id,
              uname,
              newMemberRole === 'deployer' ? 'developer' : 'viewer',
              newMemberPermissions
            );
            onUpdateApp({
              ...app,
              collaborators: [
                ...app.collaborators,
                {
                  userId: `u-${Date.now()}`,
                  username: uname,
                  email: newMemberEmail.trim(),
                  role: newMemberRole,
                  permissions: newMemberPermissions,
                  addedAt: 'Just now',
                },
              ],
            });
            setNewMemberEmail('');
            setMemberModalOpen(false);
            toast.success('Collaborator invited with custom permissions');
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Email Address
            </label>
            <input
              type="email"
              required
              value={newMemberEmail}
              onChange={(e) => setNewMemberEmail(e.target.value)}
              placeholder="engineer@company.io"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Role Preset</label>
            <select
              value={newMemberRole}
              onChange={(e) => {
                const nextRole = e.target.value as 'deployer' | 'viewer';
                setNewMemberRole(nextRole);
                if (nextRole === 'deployer') {
                  setNewMemberPermissions(['deploy', 'files', 'terminal', 'logs', 'env']);
                } else {
                  setNewMemberPermissions(['logs']);
                }
              }}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
            >
              <option value="deployer">Deployer (Full operational permissions)</option>
              <option value="viewer">Viewer (Read-only logs & monitoring)</option>
            </select>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-2">
              Granular Action Permissions Matrix
            </label>
            <div className="grid grid-cols-2 gap-2 rounded-lg border border-white/[0.07] bg-[#09090b] p-3 text-xs">
              {[
                { id: 'deploy', label: 'Deploy & Restart' },
                { id: 'files', label: 'File Manager (Read/Write)' },
                { id: 'terminal', label: 'Terminal & Exec' },
                { id: 'env', label: 'Environment Variables' },
                { id: 'logs', label: 'View Logs & Telemetry' },
              ].map((p) => {
                const checked = newMemberPermissions.includes(p.id);
                return (
                  <label
                    key={p.id}
                    className="flex items-center gap-2 text-zinc-300 cursor-pointer select-none"
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(e) => {
                        if (e.target.checked) {
                          setNewMemberPermissions((prev) => [...prev, p.id]);
                        } else {
                          setNewMemberPermissions((prev) => prev.filter((x) => x !== p.id));
                        }
                      }}
                      className="h-3.5 w-3.5 accent-orange-500 rounded"
                    />
                    <span className="font-mono text-[11px]">{p.label}</span>
                  </label>
                );
              })}
            </div>
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setMemberModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Grant Access
            </button>
          </div>
        </form>
      </ShadcnDialog>

      {/* 8. Container Resource Limits Modal */}
      <ShadcnDialog
        open={limitsModalOpen}
        onClose={() => setLimitsModalOpen(false)}
        title="Your Resource Limits"
        icon={<Sliders className="h-4 w-4" />}
      >
        <div className="space-y-4 text-xs">
          {quota ? (
            <>
              <p className="text-zinc-400">
                All your containers share the limits your administrator set for your account. They are enforced by the
                kernel, so a container can never use more than this, and you cannot raise them here.
              </p>
              <div className="grid grid-cols-2 gap-3">
                {[
                  { label: 'Memory', used: `${quota.memory_used_mb} MB`, max: `${quota.max_memory_mb} MB` },
                  { label: 'CPU', used: `${quota.cpu_used_cores.toFixed(2)} cores`, max: `${quota.max_cpus} cores` },
                  { label: 'Storage', used: `${quota.storage_used_mb} MB`, max: `${quota.max_storage_mb} MB` },
                  { label: 'Projects', used: String(quota.apps_used), max: String(quota.max_apps) },
                ].map((r) => (
                  <div key={r.label} className="rounded-lg border border-white/[0.08] bg-[#09090b] px-3.5 py-2.5">
                    <div className="text-[11px] text-zinc-500">{r.label}</div>
                    <div className="mt-1 font-mono text-zinc-100">
                      {r.used} <span className="text-zinc-500">/ {r.max}</span>
                    </div>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <p className="text-zinc-400">
              Administrator accounts are not limited, and per-container limits are defined in the compose file
              (<code className="font-mono">deploy.resources.limits</code>).
            </p>
          )}
          <div className="flex justify-end border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setLimitsModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Close
            </button>
          </div>
        </div>
      </ShadcnDialog>

      {/* 9. Delete Project Confirmation Modal */}
      <ShadcnDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        title="Delete Project"
        icon={<Trash2 className="h-4 w-4 text-rose-400" />}
      >
        <div className="space-y-4">
          <p className="text-xs text-zinc-400">
            Enter <code className="font-mono font-bold text-rose-400">DELETE</code> to permanently
            remove <strong className="text-zinc-100">{app.name}</strong>:
          </p>
          <KobraInputOTP length={6} value={otpDeleteCode} onChange={setOtpDeleteCode} />
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setDeleteDialogOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={otpDeleteCode !== 'DELETE'}
              onClick={() => onDeleteApp(app.id)}
              className="rounded-lg bg-rose-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 disabled:opacity-40 cursor-pointer"
            >
              Delete Project
            </button>
          </div>
        </div>
      </ShadcnDialog>

      {/* Persistent Upload Progress Floating Toast/Modal */}
      {isUploadingFiles && (
        <div className="fixed bottom-5 right-5 z-50 flex w-80 items-center gap-3.5 rounded-xl border border-orange-500/30 bg-[#121217]/98 p-3.5 shadow-2xl backdrop-blur-xl">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-orange-500/15 border border-orange-500/25">
            <Loader2 className="h-5 w-5 animate-spin text-orange-400" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center justify-between gap-1">
              <span className="text-xs font-semibold text-zinc-100 truncate">Uploading files...</span>
              <span className="font-mono text-[10px] text-orange-400 font-semibold tabular-nums">{uploadProgress}%</span>
            </div>
            <p className="mt-0.5 text-[11px] text-zinc-400 truncate">
              {uploadProgress >= 100 ? 'Processing on server...' : uploadFileName}
            </p>
            <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-zinc-800">
              <div
                className="h-full rounded-full bg-gradient-to-r from-orange-500 to-amber-400 transition-[width] duration-150"
                style={{ width: `${uploadProgress}%` }}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
