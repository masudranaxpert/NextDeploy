import React, { useMemo, useState } from 'react';
import {
  ArrowLeft,
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
  Sparkles,
  Filter,
  Pause,
  WrapText,
  Check,
  ChevronRight,
  Cloud,
  Calendar,
  KeyRound,
  Layers,
  X,
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
} from '../../lib/dockerFormatters';
import { XTermConsole } from '../../components/ui/XTermConsole';
import { CodeEditorBox, DevFileIcon, detectLanguageFromFilename } from '../../components/ui/CodeEditorBox';
import { backendApi } from '../../lib/backendApi';
import { ProjectDomainsTab } from './tabs/ProjectDomainsTab';
import { ProjectGitTab } from './tabs/ProjectGitTab';
import { ProjectVolumesTab } from './tabs/ProjectVolumesTab';
import { ProjectBackupsTab } from './tabs/ProjectBackupsTab';

interface AppWorkspaceProps {
  app: AppStack;
  onBack: () => void;
  onUpdateApp: (updated: AppStack) => void;
  onDeleteApp: (appId: string) => void;
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

export const AppWorkspace: React.FC<AppWorkspaceProps> = ({
  app,
  onBack,
  onUpdateApp,
  onDeleteApp,
}) => {
  const [activeTab, setActiveTab] = useState<AppWorkspaceTab>('overview');

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
  // 2. Deployments State (Production, Preview URLs, Rollback)
  // =========================================================================
  const [deployEnvFilter, setDeployEnvFilter] = useState<
    'all' | 'production' | 'preview' | 'rollback'
  >('all');
  const [buildLogsDeployment, setBuildLogsDeployment] = useState<DeploymentRecord | null>(null);
  const [previewDeploymentsEnabled, setPreviewDeploymentsEnabled] = useState(true);

  // Enhance deployments with Preview URLs & Environment metadata
  const enrichedDeployments = useMemo(() => {
    const extraPreview: (DeploymentRecord & {
      envType: 'production' | 'preview' | 'rollback';
      previewUrl: string;
      prLabel?: string;
    })[] = [
      ...app.deployments.map((d, idx) => ({
        ...d,
        envType:
          d.commitMessage.toLowerCase().includes('rollback')
            ? ('rollback' as const)
            : ('production' as const),
        previewUrl:
          idx === 0 && app.primaryDomain
            ? `https://${app.primaryDomain}`
            : `https://${d.id}-${app.slug}.preview.nextdeploy.sh`,
      })),
      {
        id: 'dep-pr48',
        commitSha: '9a21f0e',
        commitMessage: 'feat(payments): add webhook signature verification & retry queue',
        branch: 'feat/stripe-v3',
        trigger: 'git-webhook',
        author: 'masud',
        status: 'ready',
        durationSec: 24,
        startedAt: '35m ago',
        envType: 'preview',
        prLabel: 'PR #48',
        previewUrl: `https://pr-48-${app.slug}.preview.nextdeploy.sh`,
        buildSteps: [
          {
            name: 'Clone PR #48 (feat/stripe-v3)',
            duration: '2s',
            status: 'done',
            logs: ['Checked out 9a21f0e from feat/stripe-v3'],
          },
          {
            name: 'Build Isolated Preview Stack',
            duration: '19s',
            status: 'done',
            logs: [`docker compose -p ${app.slug}-pr48 up -d --build`],
          },
          {
            name: 'Provision Ephemeral Caddy Route',
            duration: '3s',
            status: 'done',
            logs: [`TLS route ready: https://pr-48-${app.slug}.preview.nextdeploy.sh`],
          },
        ],
      },
      {
        id: 'dep-pr45',
        commitSha: '4d19b8c',
        commitMessage: 'perf(cache): pipeline redis session lookups',
        branch: 'staging',
        trigger: 'git-webhook',
        author: 'nadia_ops',
        status: 'ready',
        durationSec: 21,
        startedAt: '3h ago',
        envType: 'preview',
        prLabel: 'Staging',
        previewUrl: `https://staging-${app.slug}.preview.nextdeploy.sh`,
        buildSteps: [],
      },
    ];
    return extraPreview.filter((d) =>
      deployEnvFilter === 'all' ? true : d.envType === deployEnvFilter
    );
  }, [app.deployments, app.primaryDomain, app.slug, deployEnvFilter]);

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
  const [customFolders, setCustomFolders] = useState<string[]>([
    'src',
    'src/routes',
    'config',
    'nginx',
    'scripts',
  ]);
  const [renamedPaths, setRenamedPaths] = useState<Record<string, string>>({});

  const defaultRichFiles: ComposeFileNode[] = useMemo(() => {
    const existingPaths = new Set(app.files.map((f) => f.path));
    const portNum = extractContainerPort(app.services[0]?.ports);
    const extras: ComposeFileNode[] = [
      {
        path: 'Dockerfile',
        name: 'Dockerfile',
        type: 'file',
        sizeBytes: 542,
        updatedAt: '2d ago',
        language: 'dockerfile',
        content: `# Syntax: docker/dockerfile:1.7\nFROM node:22-alpine AS base\nWORKDIR /app\n\nCOPY package.json pnpm-lock.yaml* ./\nRUN corepack enable && pnpm install --frozen-lockfile --prod\n\nCOPY ./src ./src\nCOPY ./config ./config\n\nENV NODE_ENV=production\nENV PORT=${portNum}\nEXPOSE ${portNum}\n\nHEALTHCHECK --interval=15s --timeout=3s \\\n  CMD wget -qO- http://127.0.0.1:${portNum}/health || exit 1\n\nUSER node\nCMD ["node", "src/server.js"]`,
      },
      {
        path: 'Caddyfile',
        name: 'Caddyfile',
        type: 'file',
        sizeBytes: 388,
        updatedAt: '1d ago',
        language: 'nginx',
        content: `${app.primaryDomain || `${app.slug}.nextdeploy.sh`} {\n  encode zstd gzip\n  tls {\n    protocols tls1.3\n  }\n  header {\n    Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"\n    X-Content-Type-Options "nosniff"\n    -Server\n  }\n  reverse_proxy ${app.services[0]?.service || 'app'}:${portNum} {\n    health_uri /health\n    health_interval 10s\n  }\n}`,
      },
      {
        path: 'package.json',
        name: 'package.json',
        type: 'file',
        sizeBytes: 462,
        updatedAt: '3d ago',
        language: 'json',
        content: `{\n  "name": "@nextdeploy/${app.slug}",\n  "version": "2.4.0",\n  "private": true,\n  "scripts": {\n    "start": "node dist/server.js",\n    "build": "tsup src/server.ts --format esm",\n    "db:migrate": "drizzle-kit migrate"\n  },\n  "dependencies": {\n    "fastify": "^5.1.0",\n    "ioredis": "^5.4.1",\n    "pg": "^8.13.1",\n    "pino": "^9.5.0"\n  }\n}`,
      },
      {
        path: 'release-bundle.zip',
        name: 'release-bundle.zip',
        type: 'file',
        sizeBytes: 18432,
        updatedAt: '1d ago',
        language: 'yaml',
        content: `# Binary Zip Archive: release-bundle.zip\n# Contains: public/index.html, public/robots.txt, config/staging.json`,
      },
      {
        path: 'src/server.ts',
        name: 'server.ts',
        type: 'file',
        sizeBytes: 690,
        updatedAt: '4h ago',
        language: 'yaml',
        content: `import Fastify from 'fastify';\nimport { registerHealthRoutes } from './routes/health';\n\nconst server = Fastify({ logger: true, trustProxy: true });\nregisterHealthRoutes(server);\n\nconst port = Number(process.env.PORT || ${portNum});\nserver.listen({ host: '0.0.0.0', port });`,
      },
      {
        path: 'src/db.ts',
        name: 'db.ts',
        type: 'file',
        sizeBytes: 480,
        updatedAt: '1d ago',
        language: 'yaml',
        content: `import pg from 'pg';\n\nexport const pool = new pg.Pool({\n  connectionString: process.env.DATABASE_URL,\n  max: 25,\n  idleTimeoutMillis: 30000,\n});`,
      },
      {
        path: 'src/worker.go',
        name: 'worker.go',
        type: 'file',
        sizeBytes: 615,
        updatedAt: '5h ago',
        language: 'yaml',
        content: `package main\n\nimport (\n  "context"\n  "fmt"\n  "time"\n)\n\ntype JobPayload struct {\n  ID        string\n  QueueName string\n  Attempts  int\n}\n\nfunc ProcessQueue(ctx context.Context, queue string) error {\n  fmt.Printf("[worker] listening on stream=%s port=${portNum}\\n", queue)\n  time.Sleep(250 * time.Millisecond)\n  return nil\n}`,
      },
      {
        path: 'src/telemetry.py',
        name: 'telemetry.py',
        type: 'file',
        sizeBytes: 584,
        updatedAt: '8h ago',
        language: 'yaml',
        content: `#!/usr/bin/env python3\nimport os\nimport time\nfrom typing import Dict, Optional\n\nSERVICE_SLUG = "${app.slug}"\nDEFAULT_PORT = ${portNum}\n\ndef collect_container_metrics(interval: float = 5.0) -> Dict[str, object]:\n    # Sample container memory & latency metrics\n    return {\n        "service": SERVICE_SLUG,\n        "port": DEFAULT_PORT,\n        "healthy": True,\n        "timestamp": int(time.time()),\n    }`,
      },
      {
        path: 'src/crypto.rs',
        name: 'crypto.rs',
        type: 'file',
        sizeBytes: 530,
        updatedAt: '2d ago',
        language: 'yaml',
        content: `use std::time::Duration;\n\n#[derive(Debug, Clone)]\npub struct TokenVerifier {\n    pub issuer: String,\n    pub ttl: Duration,\n}\n\nimpl TokenVerifier {\n    pub fn new(issuer: &str, ttl_secs: u64) -> Self {\n        Self {\n            issuer: issuer.to_string(),\n            ttl: Duration::from_secs(ttl_secs),\n        }\n    }\n}`,
      },
      {
        path: 'src/routes/health.ts',
        name: 'health.ts',
        type: 'file',
        sizeBytes: 512,
        updatedAt: '6h ago',
        language: 'yaml',
        content: `import type { FastifyInstance } from 'fastify';\n\nexport async function registerHealthRoutes(app: FastifyInstance) {\n  app.get('/health', async () => ({\n    status: 'ok',\n    service: '${app.slug}',\n    uptime: process.uptime(),\n  }));\n}`,
      },
      {
        path: 'config/production.json',
        name: 'production.json',
        type: 'file',
        sizeBytes: 345,
        updatedAt: '4d ago',
        language: 'json',
        content: `{\n  "service": "${app.slug}",\n  "port": ${portNum},\n  "rateLimit": {\n    "windowMs": 60000,\n    "maxRequests": 240\n  },\n  "metrics": true\n}`,
      },
      {
        path: 'config/schema.sql',
        name: 'schema.sql',
        type: 'file',
        sizeBytes: 490,
        updatedAt: '3d ago',
        language: 'yaml',
        content: `-- PostgreSQL schema for ${app.slug}\nCREATE TABLE IF NOT EXISTS deployments_audit (\n  id UUID PRIMARY KEY,\n  service_slug VARCHAR(120) NOT NULL,\n  commit_sha VARCHAR(40) NOT NULL,\n  created_at TIMESTAMPTZ DEFAULT NOW()\n);\n\nCREATE INDEX idx_deployments_service ON deployments_audit (service_slug);`,
      },
      {
        path: 'nginx/default.conf',
        name: 'default.conf',
        type: 'file',
        sizeBytes: 412,
        updatedAt: '5d ago',
        language: 'nginx',
        content: `upstream backend_upstream {\n  server 127.0.0.1:${portNum} keepalive=64;\n}\n\nserver {\n  listen 80;\n  client_max_body_size 50M;\n  location / {\n    proxy_pass http://backend_upstream;\n    proxy_http_version 1.1;\n  }\n}`,
      },
      {
        path: 'scripts/entrypoint.sh',
        name: 'entrypoint.sh',
        type: 'file',
        sizeBytes: 310,
        updatedAt: '6d ago',
        language: 'sh',
        content: `#!/usr/bin/env sh\nset -eu\n\necho "[nextdeploy] Waiting for database socket..."\necho "[nextdeploy] Running schema migrations for ${app.slug}..."\nexec "$@"`,
      },
      {
        path: 'scripts/backup-db.sh',
        name: 'backup-db.sh',
        type: 'file',
        sizeBytes: 295,
        updatedAt: '3d ago',
        language: 'sh',
        content: `#!/usr/bin/env sh\nset -eu\n\npg_dump "$DATABASE_URL" | gzip > "/backups/${app.slug}_\$(date +%F).sql.gz"`,
      },
      {
        path: 'scripts/webhook.php',
        name: 'webhook.php',
        type: 'file',
        sizeBytes: 428,
        updatedAt: '2d ago',
        language: 'yaml',
        content: `<?php\ndeclare(strict_types=1);\n\n$secret = getenv('WEBHOOK_SECRET') ?: 'nd_live_secret';\n$payload = file_get_contents('php://input');\n\nheader('Content-Type: application/json');\necho json_encode([\n    'verified' => true,\n    'service' => '${app.slug}',\n    'bytes' => strlen($payload),\n]);`,
      },
    ];
    return [...app.files, ...extras.filter((e) => !existingPaths.has(e.path))]
      .map((f) => {
        const mappedPath = renamedPaths[f.path] || f.path;
        return {
          ...f,
          path: mappedPath,
          name: mappedPath.split('/').pop() || mappedPath,
        };
      })
      .filter((f) => !deletedFilePaths.includes(f.path));
  }, [app.files, app.primaryDomain, app.services, app.slug, deletedFilePaths, renamedPaths]);

  const [currentDir, setCurrentDir] = useState<string>('');
  const [editingFilePath, setEditingFilePath] = useState<string | null>(null);
  const [openEditorTabs, setOpenEditorTabs] = useState<string[]>([]);
  const [expandedDirs, setExpandedDirs] = useState<Record<string, boolean>>({
    src: true,
    'src/routes': true,
    config: false,
    nginx: false,
    scripts: false,
  });
  const [mobileTreeOpen, setMobileTreeOpen] = useState(false);
  const [fileSearch, setFileSearch] = useState('');
  const [selectedEntries, setSelectedEntries] = useState<string[]>([]);
  const [filePermissions, setFilePermissions] = useState<Record<string, string>>({
    'scripts/entrypoint.sh': '-rwxr-xr-x (0755)',
    'scripts/backup-db.sh': '-rwxr-xr-x (0755)',
    '.env': '-rw------- (0600)',
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
  const [newFolderModalOpen, setNewFolderModalOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [renameTarget, setRenameTarget] = useState<{
    path: string;
    name: string;
    isFolder: boolean;
  } | null>(null);
  const [renameInput, setRenameInput] = useState('');
  const [moveTarget, setMoveTarget] = useState<{
    path: string;
    name: string;
    mode: 'move' | 'copy';
  } | null>(null);
  const [moveDestInput, setMoveDestInput] = useState('');
  const [inlineNewDestFolder, setInlineNewDestFolder] = useState('');
  const [showInlineFolderCreator, setShowInlineFolderCreator] = useState(false);
  const [compressModalOpen, setCompressModalOpen] = useState(false);
  const [compressItems, setCompressItems] = useState<string[]>([]);
  const [compressArchiveName, setCompressArchiveName] = useState('archive.zip');
  const [compressFormat, setCompressFormat] = useState<'zip' | 'tar.gz'>('zip');
  const [extractTargetFile, setExtractTargetFile] = useState<ComposeFileNode | null>(null);
  const [extractDestDir, setExtractDestDir] = useState('');
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [uploadFileName, setUploadFileName] = useState('assets-bundle.zip');

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

  const openFileInEditor = (file: ComposeFileNode) => {
    setContextMenu(null);
    if (isArchiveFile(file.name)) {
      setExtractTargetFile(file);
      setExtractDestDir(currentDir || '');
      return;
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

  const openChmodFor = (targetPath: string) => {
    setContextMenu(null);
    const currentPerm = filePermissions[targetPath] || '-rw-r--r-- (0644)';
    if (currentPerm.includes('0755')) {
      setChmodMatrix({
        ownerR: true,
        ownerW: true,
        ownerX: true,
        groupR: true,
        groupW: false,
        groupX: true,
        worldR: true,
        worldW: false,
        worldX: true,
      });
    } else if (currentPerm.includes('0600')) {
      setChmodMatrix({
        ownerR: true,
        ownerW: true,
        ownerX: false,
        groupR: false,
        groupW: false,
        groupX: false,
        worldR: false,
        worldW: false,
        worldX: false,
      });
    } else {
      setChmodMatrix({
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
    }
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
    const sym = `-${chmodMatrix.ownerR ? 'r' : '-'}${chmodMatrix.ownerW ? 'w' : '-'}${
      chmodMatrix.ownerX ? 'x' : '-'
    }${chmodMatrix.groupR ? 'r' : '-'}${chmodMatrix.groupW ? 'w' : '-'}${
      chmodMatrix.groupX ? 'x' : '-'
    }${chmodMatrix.worldR ? 'r' : '-'}${chmodMatrix.worldW ? 'w' : '-'}${
      chmodMatrix.worldX ? 'x' : '-'
    }`;
    return { octal: `0${o}${g}${w}`, symbolic: `${sym} (0${o}${g}${w})` };
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

  const detectedBranches: DetectedGitBranch[] = useMemo(
    () => [
      {
        name: 'main',
        sha: app.gitCommitSha?.slice(0, 7) || '8c4f19a',
        author: 'masud',
        updatedAt: '4m ago',
        isDefault: true,
        protected: true,
      },
      {
        name: 'production',
        sha: 'f42a90c',
        author: 'masud',
        updatedAt: '38m ago',
        protected: true,
      },
      {
        name: 'staging',
        sha: '4d19b8c',
        author: 'nadia_ops',
        updatedAt: '3h ago',
      },
      {
        name: 'develop',
        sha: '71e09b4',
        author: 'tanvir_dev',
        updatedAt: '6h ago',
      },
      {
        name: 'feat/stripe-v3',
        sha: '9a21f0e',
        author: 'masud',
        updatedAt: '35m ago',
        prNumber: 48,
      },
      {
        name: 'fix/redis-pool-timeout',
        sha: '2c88d10',
        author: 'masud',
        updatedAt: '1d ago',
        prNumber: 46,
      },
    ],
    [app.gitCommitSha]
  );

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

  const [projectBackups, setProjectBackups] = useState<ProjectBackupItem[]>([
    {
      id: 'snap-904',
      name: `${app.slug}_2025-05-14_06-00.nd-backup.tar.gz`,
      type: 'Scheduled',
      destination: 'Cloudflare R2 Primary (EU-Central)',
      provider: 'Cloudflare R2',
      sizeMB: 1420.5,
      createdAt: '2h ago',
      commitSha: app.gitCommitSha?.slice(0, 7) || '8c4f19a',
      includes: ['Compose', '.env (AES-256)', ...app.volumes.map((v) => v.name)],
      checksum: 'sha256:9f84c1e0b2',
      status: 'completed',
    },
    {
      id: 'snap-901',
      name: `${app.slug}_pre-deploy_dep-4920.nd-backup.tar.gz`,
      type: 'Pre-Deploy',
      destination: 'Cloudflare R2 Primary (EU-Central)',
      provider: 'Cloudflare R2',
      sizeMB: 1418.2,
      createdAt: '4h ago',
      commitSha: '3b10e4c',
      includes: ['Compose', '.env (AES-256)', ...app.volumes.map((v) => v.name)],
      checksum: 'sha256:4a12d9f8c7',
      status: 'completed',
    },
    {
      id: 'snap-894',
      name: `${app.slug}_vault-sync_weekly.nd-backup.tar.gz`,
      type: 'Manual',
      destination: 'Google Drive Team Vault',
      provider: 'Google Drive',
      sizeMB: 1390.0,
      createdAt: '2d ago',
      commitSha: '7c01a8b',
      includes: ['Compose', '.env (AES-256)'],
      checksum: 'sha256:8e31b4a021',
      status: 'completed',
    },
  ]);

  // =========================================================================
  // 7. Modern Logs Explorer State
  // =========================================================================
  const [logContainerFilter, setLogContainerFilter] = useState<string>('all');
  const [logLevelFilter, setLogLevelFilter] = useState<'all' | 'info' | 'warn' | 'error' | 'debug'>(
    'all'
  );
  const [logTimeRange, setLogTimeRange] = useState<'live' | '15m' | '1h' | '24h'>('live');
  const [logSearch, setLogSearch] = useState('');
  const [logLiveTail, setLogLiveTail] = useState(true);
  const [logShowTimestamps, setLogShowTimestamps] = useState(true);
  const [logWordWrap, setLogWordWrap] = useState(true);

  const syntheticLogs: LogEntry[] = useMemo(() => {
    const firstSvc = app.services[0]?.service || 'api';
    const secondSvc = app.services[1]?.service || firstSvc;
    const thirdSvc = app.services[2]?.service || firstSvc;
    const baseLogs: LogEntry[] = [
      ...app.logs,
      {
        id: 'lg-101',
        timestamp: '2025-05-14T18:20:04.112Z',
        container: firstSvc,
        level: 'info',
        message: `GET /api/v1/health 200 OK - 2.4ms (Caddy TLS 1.3 ingress)`,
      },
      {
        id: 'lg-102',
        timestamp: '2025-05-14T18:20:09.408Z',
        container: secondSvc,
        level: 'debug',
        message: `Connection pool heartbeat: active=6 idle=18 waiting=0 max=50`,
      },
      {
        id: 'lg-103',
        timestamp: '2025-05-14T18:20:15.892Z',
        container: firstSvc,
        level: 'info',
        message: `POST /api/v1/checkout/session 201 Created - 18.7ms [req_id=nd_991a04]`,
      },
      {
        id: 'lg-104',
        timestamp: '2025-05-14T18:20:22.301Z',
        container: thirdSvc,
        level: 'warn',
        message: `Rate limiter threshold reached for IP 185.199.108.153 (120 req/min) — throttling burst`,
      },
      {
        id: 'lg-105',
        timestamp: '2025-05-14T18:20:31.004Z',
        container: thirdSvc,
        level: 'info',
        message: `Job #8841 [invoice.pdf.render] completed in 142ms · uploaded to R2 bucket`,
      },
      {
        id: 'lg-106',
        timestamp: '2025-05-14T18:20:44.719Z',
        container: firstSvc,
        level: 'error',
        message: `Upstream webhook timeout after 3000ms on endpoint https://hooks.stripe.com/v1/events (retrying 1/3)`,
      },
      {
        id: 'lg-107',
        timestamp: '2025-05-14T18:20:46.105Z',
        container: firstSvc,
        level: 'info',
        message: `Retry 1/3 succeeded (200 OK in 94ms) — webhook event evt_3Q91kL processed`,
      },
    ];
    return baseLogs;
  }, [app.logs, app.services]);

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

  const appendSimulatedLog = () => {
    const svc = app.services[0]?.service || 'api';
    const nextLog: LogEntry = {
      id: `lg-${Date.now()}`,
      timestamp: new Date().toISOString(),
      container: svc,
      level: 'info',
      message: `GET /api/v1/status 200 OK - ${(Math.random() * 9 + 1.2).toFixed(1)}ms [tls=TLSv1.3]`,
    };
    onUpdateApp({
      ...app,
      logs: [...app.logs, nextLog],
    });
  };

  // =========================================================================
  // Other Modals State
  // =========================================================================
  const [volumeModalOpen, setVolumeModalOpen] = useState(false);
  const [newVolName, setNewVolName] = useState('');
  const [newVolMountPath, setNewVolMountPath] = useState('/var/lib/postgresql/data');

  const [memberModalOpen, setMemberModalOpen] = useState(false);
  const [newMemberEmail, setNewMemberEmail] = useState('');
  const [newMemberRole, setNewMemberRole] = useState<'deployer' | 'viewer'>('deployer');

  const [limitsModalOpen, setLimitsModalOpen] = useState(false);
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

  // =========================================================================
  // Actions & Handlers
  // =========================================================================
  const triggerDeploy = (options?: { noCache?: boolean; rollbackFrom?: DeploymentRecord }) => {
    void backendApi.composeAction(app.id, options?.noCache ? 'redeploy' : 'up');
    const newDepId = `dep-${Math.floor(5000 + Math.random() * 900)}`;
    const sha = options?.rollbackFrom
      ? options.rollbackFrom.commitSha
      : Math.random().toString(16).slice(2, 9);
    const msg = options?.rollbackFrom
      ? `Rollback to ${options.rollbackFrom.id} (${options.rollbackFrom.commitSha.slice(0, 7)})`
      : options?.noCache
        ? 'Clean rebuild (--no-cache) triggered from workspace'
        : 'Production compose deploy triggered';

    const nextDeployment: DeploymentRecord = {
      id: newDepId,
      commitSha: sha,
      commitMessage: msg,
      branch: options?.rollbackFrom?.branch || app.gitBranch || 'main',
      trigger: 'manual-compose',
      author: 'masud',
      status: 'ready',
      durationSec: options?.rollbackFrom ? 6 : 18,
      startedAt: 'Just now',
      buildSteps: [
        {
          name: 'Validate Compose & Secrets',
          duration: '1s',
          status: 'done',
          logs: ['Validated docker-compose.yml', `Injected ${app.envVars.length} variables`],
        },
        {
          name: options?.rollbackFrom ? 'Restore Container Image Digest' : 'Build & Start Services',
          duration: options?.rollbackFrom ? '3s' : '14s',
          status: 'done',
          logs: ['docker compose up -d --remove-orphans'],
        },
        {
          name: 'Switch Caddy HTTPS Upstream',
          duration: '2s',
          status: 'done',
          logs: [`Zero-downtime healthcheck passed for ${app.primaryDomain || app.slug}`],
        },
      ],
    };

    onUpdateApp({
      ...app,
      status: 'running',
      gitCommitSha: sha,
      updatedAt: 'Just now',
      deployments: [nextDeployment, ...app.deployments],
    });
    toast.success(
      options?.rollbackFrom
        ? `Rolled back production to ${options.rollbackFrom.id} (${sha.slice(0, 7)})`
        : `Deployed ${app.name} (${newDepId})`
    );
  };

  const toggleStopOrStart = () => {
    const nextStatus = app.status === 'stopped' ? 'running' : 'stopped';
    void backendApi.composeAction(app.id, nextStatus === 'running' ? 'up' : 'down');
    const nextServices: ContainerService[] = app.services.map((s) => ({
      ...s,
      state: nextStatus === 'running' ? 'running' : 'exited',
      cpuPct: nextStatus === 'running' ? 3.8 : 0,
    }));
    onUpdateApp({
      ...app,
      status: nextStatus,
      services: nextServices,
    });
    toast.success(nextStatus === 'running' ? `Started ${app.name}` : `Stopped ${app.name}`);
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
  const handleCreateFileSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const raw = newFileName.trim().replace(/^\/+/, '');
    if (!raw) return;
    const fullPath = !raw.includes('/') && currentDir ? `${currentDir}/${raw}` : raw;
    void backendApi.fileCreate(app.id, fullPath);
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

    const parentSlash = fullPath.lastIndexOf('/');
    if (parentSlash !== -1) {
      const parentDir = fullPath.slice(0, parentSlash);
      setCustomFolders((prev) => (prev.includes(parentDir) ? prev : [...prev, parentDir]));
    }

    const created: ComposeFileNode = {
      path: fullPath,
      name: fullPath.split('/').pop() || fullPath,
      type: 'file',
      sizeBytes: new Blob([starterContent]).size,
      updatedAt: 'Just now',
      language: lang,
      content: starterContent,
    };
    onUpdateApp({
      ...app,
      files: [...app.files, created],
    });
    setDeletedFilePaths((prev) => prev.filter((p) => p !== fullPath));
    setEditingFilePath(fullPath);
    setOpenEditorTabs((prev) => (prev.includes(fullPath) ? prev : [...prev, fullPath]));
    setNewFileName('');
    setNewFileModalOpen(false);
    toast.success(`Created ${fullPath}`);
  };

  // New Folder Submit
  const handleCreateFolderSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const raw = newFolderName
      .trim()
      .replace(/^\/+|\/+$/g, '')
      .replace(/[^a-zA-Z0-9/_-]/g, '-');
    if (!raw) return;
    const fullFolder = !raw.includes('/') && currentDir ? `${currentDir}/${raw}` : raw;
    void backendApi.fileCreate(app.id, fullFolder, true);
    setCustomFolders((prev) => (prev.includes(fullFolder) ? prev : [...prev, fullFolder]));
    setDeletedFilePaths((prev) => prev.filter((p) => p !== `dir:${fullFolder}`));
    setExpandedDirs((prev) => ({ ...prev, [fullFolder]: true }));
    setNewFolderName('');
    setNewFolderModalOpen(false);
    toast.success(`Created folder /${fullFolder}`);
  };

  // Compress (Zip / Tar.gz) Submit
  const handleCompressSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const cleanName = compressArchiveName.trim().replace(/^\/+/, '');
    if (!cleanName) return;
    const finalName =
      cleanName.endsWith('.zip') || cleanName.endsWith('.tar.gz')
        ? cleanName
        : `${cleanName}.${compressFormat}`;
    const fullPath = currentDir ? `${currentDir}/${finalName}` : finalName;
    if (compressItems[0]) {
      void backendApi.fileZip(app.id, compressItems[0].replace(/^dir:/, ''));
    }
    const archiveNode: ComposeFileNode = {
      path: fullPath,
      name: finalName,
      type: 'file',
      sizeBytes: 14820,
      updatedAt: 'Just now',
      language: 'yaml',
      content: `# Archive: ${finalName}\n# Compressed items: ${compressItems.join(', ')}\n`,
    };
    onUpdateApp({
      ...app,
      files: [...app.files.filter((f) => f.path !== fullPath), archiveNode],
    });
    setDeletedFilePaths((prev) => prev.filter((p) => p !== fullPath));
    setSelectedEntries([]);
    setCompressModalOpen(false);
    toast.success(`Compressed ${compressItems.length} item(s) into ${fullPath}`);
  };

  // Extract (Unzip) Submit
  const handleExtractSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!extractTargetFile) return;
    const targetDir = extractDestDir.trim().replace(/^\/+|\/+$/g, '');
    void backendApi.fileUnzip(app.id, extractTargetFile.path, targetDir);
    const baseArchive = extractTargetFile.name
      .replace(/\.tar\.gz$/i, '')
      .replace(/\.(zip|tgz|tar)$/i, '');
    const extractedDir = targetDir ? `${targetDir}/${baseArchive}` : baseArchive;

    const extractedFiles: ComposeFileNode[] = [
      {
        path: `${extractedDir}/manifest.json`,
        name: 'manifest.json',
        type: 'file',
        sizeBytes: 318,
        updatedAt: 'Just now',
        language: 'json',
        content: `{\n  "archive": "${extractTargetFile.name}",\n  "extractedAt": "${new Date().toISOString()}",\n  "integrity": "verified"\n}`,
      },
      {
        path: `${extractedDir}/deploy-hook.sh`,
        name: 'deploy-hook.sh',
        type: 'file',
        sizeBytes: 240,
        updatedAt: 'Just now',
        language: 'sh',
        content: `#!/usr/bin/env sh\necho "Running extracted hook from ${extractTargetFile.name}"\n`,
      },
    ];

    setCustomFolders((prev) => (prev.includes(extractedDir) ? prev : [...prev, extractedDir]));
    setExpandedDirs((prev) => ({ ...prev, [extractedDir]: true }));
    onUpdateApp({
      ...app,
      files: [...app.files, ...extractedFiles],
    });
    setExtractTargetFile(null);
    toast.success(`Extracted ${extractTargetFile.name} to /${extractedDir}`);
  };

  // Rename Submit
  const handleRenameSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!renameTarget || !renameInput.trim()) return;
    const cleanNewName = renameInput.trim().replace(/^\/+|\/+$/g, '');
    const parent = renameTarget.path.includes('/')
      ? renameTarget.path.slice(0, renameTarget.path.lastIndexOf('/'))
      : '';
    const nextPath = parent ? `${parent}/${cleanNewName}` : cleanNewName;
    void backendApi.fileRename(app.id, renameTarget.path, nextPath);

    if (renameTarget.isFolder) {
      setCustomFolders((prev) =>
        prev.map((d) => (d === renameTarget.path ? nextPath : d)).concat(nextPath)
      );
      toast.success(`Renamed folder to /${nextPath}`);
    } else {
      setRenamedPaths((prev) => ({ ...prev, [renameTarget.path]: nextPath }));
      toast.success(`Renamed ${renameTarget.name} → ${cleanNewName}`);
    }
    setRenameTarget(null);
  };

  // Copy / Move Submit
  const handleMoveOrCopySubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!moveTarget) return;
    const destFolder = moveDestInput.trim().replace(/^\/+|\/+$/g, '');
    const destPath = destFolder ? `${destFolder}/${moveTarget.name}` : moveTarget.name;
    if (moveTarget.mode === 'move') {
      void backendApi.fileMove(app.id, [moveTarget.path], destFolder);
    } else {
      void backendApi.fileCopy(app.id, [moveTarget.path], destFolder);
    }
    const sourceNode = defaultRichFiles.find((f) => f.path === moveTarget.path);
    if (sourceNode) {
      const cloned: ComposeFileNode = {
        ...sourceNode,
        path: destPath,
        name: destPath.split('/').pop() || destPath,
        updatedAt: 'Just now',
      };
      onUpdateApp({
        ...app,
        files: [...app.files, cloned],
      });
      if (moveTarget.mode === 'move') {
        setDeletedFilePaths((prev) => [...prev, moveTarget.path]);
      }
      if (destFolder) {
        setCustomFolders((prev) => (prev.includes(destFolder) ? prev : [...prev, destFolder]));
      }
      toast.success(
        moveTarget.mode === 'move'
          ? `Moved to /${destPath}`
          : `Copied to /${destPath}`
      );
    }
    setMoveTarget(null);
  };

  // Upload File or Remote URL Submit
  const handleUploadSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (uploadMode === 'url') {
      const rawUrl = uploadRemoteUrl.trim();
      if (!rawUrl) return;
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
        files: [...app.files, uploaded],
      });
      setUploadRemoteUrl('');
      setUploadModalOpen(false);
      toast.success(`Fetched ${inferredName} into /${currentDir || 'root'}`);
      return;
    }

    const clean = uploadFileName.trim().replace(/^\/+/, '');
    if (!clean) return;
    const fullPath = currentDir ? `${currentDir}/${clean}` : clean;
    const uploaded: ComposeFileNode = {
      path: fullPath,
      name: clean.split('/').pop() || clean,
      type: 'file',
      sizeBytes: clean.endsWith('.zip') ? 24576 : 1024,
      updatedAt: 'Just now',
      language: clean.endsWith('.json') ? 'json' : 'yaml',
      content: `# Uploaded file: ${clean}\n`,
    };
    onUpdateApp({
      ...app,
      files: [...app.files, uploaded],
    });
    setUploadModalOpen(false);
    toast.success(`Uploaded ${fullPath}`);
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

  // Tabs
  const tabs: { id: AppWorkspaceTab; label: string; count?: number }[] = [
    { id: 'overview', label: 'General' },
    { id: 'deployments', label: 'Deployments', count: app.deployments.length + 2 },
    { id: 'domains', label: 'Domains', count: app.domains.length },
    { id: 'env', label: 'Environment', count: app.envVars.length },
    { id: 'files', label: 'File Manager', count: defaultRichFiles.length },
    { id: 'git', label: 'Git' },
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
  const activeProductionDeployment = app.deployments[0];
  const productionUrl = app.primaryDomain
    ? `https://${app.primaryDomain}`
    : `https://${app.slug}.nextdeploy.sh`;
  const previewUrl = `https://pr-48-${app.slug}.preview.nextdeploy.sh`;

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
              {app.name.slice(0, 2)}
            </div>

            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="truncate text-base font-semibold tracking-tight text-zinc-100">
                  {app.name}
                </h1>
                <StatusBadge status={app.status} />
                <span className="rounded-md border border-emerald-500/25 bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-emerald-400">
                  production
                </span>
                {previewDeploymentsEnabled && (
                  <span className="rounded-md border border-violet-500/25 bg-violet-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-violet-300">
                    PR Previews
                  </span>
                )}
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-zinc-400">
                <a
                  href={productionUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 font-mono text-zinc-300 hover:text-orange-400 transition-colors"
                >
                  <Globe className="h-3 w-3 text-emerald-400" />
                  {productionUrl}
                  <ArrowUpRight className="h-3 w-3 opacity-60" />
                </a>
                {app.gitBranch && (
                  <button
                    type="button"
                    onClick={() => setGitModalOpen(true)}
                    className="inline-flex items-center gap-1 font-mono text-[11px] text-zinc-400 hover:text-orange-400 cursor-pointer"
                  >
                    <GitBranch className="h-3 w-3 text-orange-400" />
                    {app.gitBranch}
                    {app.gitCommitSha && <span>@{app.gitCommitSha.slice(0, 7)}</span>}
                  </button>
                )}
                <span className="font-mono text-[11px] text-zinc-500">
                  {runningContainersCount}/{app.services.length} containers · {app.cpuPct}% CPU ·{' '}
                  {app.memMB} MB
                </span>
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => triggerDeploy()}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-all cursor-pointer"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              Deploy
            </button>

            <button
              type="button"
              onClick={() => toast.success(`Restarted ${app.name} containers`)}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 transition-colors cursor-pointer"
            >
              <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
              Restart
            </button>

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
                  label: 'Container Limits',
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
                  variant: 'danger',
                  onClick: () => setDeleteDialogOpen(true),
                },
              ]}
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
          {/* Deploy Source Bar (Git source vs Manual files + Pause/Resume Git Sync + Switch Source) */}
          <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 px-4 py-3.5 flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2.5 min-w-0">
              {app.sourceType === 'git' ? (
                <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-1 text-xs font-medium text-emerald-300">
                  <GitBranch className="h-3.5 w-3.5" />
                  Git source
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5 rounded-full border border-orange-500/30 bg-orange-500/10 px-2.5 py-1 text-xs font-medium text-orange-300">
                  <Folder className="h-3.5 w-3.5" />
                  Manual files
                </span>
              )}

              {app.gitRepo && app.sourceType !== 'git' && (
                <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 font-mono text-[11px] font-medium text-amber-300">
                  Git sync paused · {app.gitBranch || 'main'}
                </span>
              )}

              <span className="font-mono text-xs text-zinc-400 truncate">
                {app.sourceType === 'git'
                  ? `${app.gitRepo || `https://github.com/masudranaxpert/${app.slug}.git`} (${app.gitBranch || 'main'})`
                  : `Workspace directory (/apps/${app.id}) · ${app.composeFilePath || 'docker-compose.yml'}`}
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {app.gitRepo && (
                <button
                  type="button"
                  onClick={() => {
                    setForceGitSyncResume(false);
                    setGitSyncModalOpen(true);
                  }}
                  className={`inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium cursor-pointer ${
                    app.sourceType === 'git'
                      ? 'border-white/[0.08] bg-zinc-900 text-zinc-200 hover:bg-zinc-800'
                      : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20'
                  }`}
                >
                  {app.sourceType === 'git' ? (
                    <>
                      <Pause className="h-3.5 w-3.5 text-amber-400" />
                      Pause Git sync
                    </>
                  ) : (
                    <>
                      <Play className="h-3.5 w-3.5 text-emerald-400" />
                      Resume Git sync
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
                Switch source
              </button>
            </div>
          </div>

          {/* Compose Services Grid */}
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-400">
                  Services ({app.services.length})
                </h2>
              </div>
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={async () => {
                    const res = await backendApi.composeView(app.id);
                    const fallbackYaml =
                      app.files.find((f) => f.path.includes('docker-compose'))?.content ||
                      `services:\n  ${app.services[0]?.service || 'app'}:\n    image: ${app.services[0]?.image || 'node:22-alpine'}\n    labels:\n      caddy: ${app.primaryDomain || `${app.slug}.nextdeploy.sh`}\n      caddy.reverse_proxy: "{{upstreams ${extractContainerPort(app.services[0]?.ports)}}}"\nnetworks:\n  nextdeploy_net:\n    external: true\n`;
                    setMergedComposeYaml(res.yaml || fallbackYaml);
                    setMergedComposeModalOpen(true);
                  }}
                  className="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-400 hover:text-emerald-300 cursor-pointer"
                >
                  <Layers className="h-3.5 w-3.5" />
                  Merged Caddy Compose
                </button>
                <button
                  type="button"
                  onClick={() => setActiveTab('files')}
                  className="inline-flex items-center gap-1.5 text-xs font-medium text-orange-400 hover:text-orange-300 cursor-pointer"
                >
                  <FileCode2 className="h-3.5 w-3.5" />
                  Open File Manager
                </button>
              </div>
            </div>

            <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
              {app.services.map((svc) => {
                const imgInfo = formatCompactImage(svc.image);
                const shortPort = formatCompactPort(svc.ports);
                const memPct = Math.min(Math.round((svc.memUsedMB / svc.memLimitMB) * 100), 100);

                return (
                  <div
                    key={svc.id}
                    className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)] flex flex-col justify-between gap-3.5"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex items-center gap-3 min-w-0">
                        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
                          <Box className="h-4 w-4 text-orange-400" />
                        </div>
                        <div className="min-w-0">
                          <div className="flex items-center gap-2">
                            <span className="font-mono text-xs font-semibold text-zinc-100 truncate">
                              {svc.service}
                            </span>
                            <StatusBadge status={svc.state} />
                          </div>
                          <div className="mt-0.5 flex items-center gap-2 font-mono text-[11px] text-zinc-400">
                            <span className="truncate">
                              {imgInfo.shortName}
                              <span className="text-orange-400">:{imgInfo.tagPart}</span>
                            </span>
                            <span className="text-zinc-600">·</span>
                            <span className="inline-flex items-center gap-1 text-zinc-300">
                              <Network className="h-3 w-3 text-emerald-400" />
                              {shortPort}
                            </span>
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-1">
                        <button
                          type="button"
                          onClick={() => {
                            setLogContainerFilter(svc.service);
                            setActiveTab('logs');
                          }}
                          className="rounded-lg border border-white/[0.07] bg-zinc-900 px-2 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
                        >
                          Logs
                        </button>
                        <button
                          type="button"
                          onClick={() => setActiveTab('terminal')}
                          className="rounded-lg border border-white/[0.07] bg-zinc-900 px-2 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
                        >
                          Shell
                        </button>
                        <ActionMenu
                          items={[
                            {
                              label: 'Restart Container',
                              icon: <RotateCw className="h-3.5 w-3.5" />,
                              onClick: () => toast.success(`Restarted ${svc.name}`),
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
                                navigator.clipboard?.writeText(svc.id);
                                toast.success(`Copied ${svc.id}`);
                              },
                            },
                          ]}
                        />
                      </div>
                    </div>

                    {/* CPU & Memory Mini Bars */}
                    <div className="grid grid-cols-2 gap-3 border-t border-white/[0.05] pt-3 font-mono text-[11px]">
                      <div>
                        <div className="flex justify-between text-zinc-400 mb-1">
                          <span>CPU</span>
                          <span className="text-zinc-200">{svc.cpuPct}%</span>
                        </div>
                        <div className="h-1 w-full rounded-full bg-zinc-900 overflow-hidden">
                          <div
                            className="h-full bg-orange-500 rounded-full"
                            style={{ width: `${Math.min(svc.cpuPct * 3, 100)}%` }}
                          />
                        </div>
                      </div>
                      <div>
                        <div className="flex justify-between text-zinc-400 mb-1">
                          <span>Memory</span>
                          <span className="text-zinc-200">
                            {svc.memUsedMB} / {svc.memLimitMB} MB
                          </span>
                        </div>
                        <div className="h-1 w-full rounded-full bg-zinc-900 overflow-hidden">
                          <div
                            className="h-full bg-emerald-500 rounded-full"
                            style={{ width: `${memPct}%` }}
                          />
                        </div>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Row 2: Source Pipeline + Container Resource Limits & Styled Delete Button */}
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
            <div className="lg:col-span-2 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 flex flex-col justify-between gap-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <GitBranch className="h-4 w-4 text-orange-400" />
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-300">
                    Git Source & Environments
                  </h3>
                </div>
                <button
                  type="button"
                  onClick={() => setGitModalOpen(true)}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                >
                  <GitBranch className="h-3 w-3 text-orange-400" />
                  Change Branch / Repo
                </button>
              </div>

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
                      Auto-Deploy
                    </span>
                    <span className="mt-1 font-mono text-xs text-zinc-200 block">
                      {app.webhookAutoDeploy ? 'On Git Push' : 'Manual'}
                    </span>
                  </div>
                  <ShadcnSwitch
                    checked={Boolean(app.webhookAutoDeploy)}
                    onChange={(next) => {
                      onUpdateApp({ ...app, webhookAutoDeploy: next });
                      toast.success(next ? 'Auto-deploy enabled' : 'Auto-deploy paused');
                    }}
                  />
                </div>
              </div>

              {/* Production & Preview URLs Bar */}
              <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-3 text-xs">
                <div className="flex flex-wrap items-center gap-3 font-mono text-[11px]">
                  <a
                    href={productionUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 text-emerald-400 hover:underline"
                  >
                    <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                    Prod: {productionUrl.replace('https://', '')}
                    <ArrowUpRight className="h-3 w-3" />
                  </a>
                  <span className="text-zinc-700">|</span>
                  <a
                    href={previewUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 text-violet-300 hover:underline"
                  >
                    <span className="h-1.5 w-1.5 rounded-full bg-violet-400" />
                    Preview: pr-48-{app.slug}.preview.nextdeploy.sh
                    <ArrowUpRight className="h-3 w-3" />
                  </a>
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
                      <span className="text-zinc-100 font-semibold">{cpuQuota} vCPU</span>
                      <span className="text-[10px] text-emerald-400">{app.cpuPct}% used</span>
                    </div>
                  </div>
                  <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-2.5">
                    <span className="text-[10px] text-zinc-500 block">Max Memory</span>
                    <div className="mt-0.5 flex items-baseline justify-between">
                      <span className="text-zinc-100 font-semibold">{memQuota} MB</span>
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
              app.sourceType === 'git'
                ? 'Switch to Manual Workspace Files'
                : 'Switch to Git Repository Source'
            }
            icon={<RefreshCw className="h-4 w-4 text-orange-400" />}
          >
            <div className="space-y-4">
              {app.sourceType === 'git' ? (
                <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-200 leading-relaxed">
                  Switching to <strong>Manual files</strong> disconnects the Git repository config and uses the editable workspace directory for future deployments. (To keep the repo saved while editing files, use <strong>Pause Git sync</strong> instead.)
                </div>
              ) : (
                <div className="rounded-lg border border-orange-500/30 bg-orange-500/10 p-3 text-xs text-orange-200 leading-relaxed">
                  Switching to <strong>Git source</strong> clears uploaded workspace files (except panel <code className="font-mono">.env</code>) so your Git repository checkout is the single deploy source.
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
                    const targetSource = app.sourceType === 'git' ? 'files' : 'git';
                    const res = await backendApi.switchAppSource(app.id, targetSource);
                    if (!res.ok && !res.offlineFallback) {
                      toast.error(res.error || 'Failed to switch deploy source');
                      return;
                    }
                    onUpdateApp({
                      ...app,
                      sourceType: targetSource === 'git' ? 'git' : 'compose',
                      gitSync: targetSource === 'git',
                    });
                    setSwitchSourceModalOpen(false);
                    if (targetSource === 'git') {
                      setActiveTab('git');
                      toast.success('Switched to Git source');
                    } else {
                      setActiveTab('files');
                      toast.success('Switched to Manual workspace files');
                    }
                  }}
                  className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
                >
                  {app.sourceType === 'git' ? 'Switch to Manual Files' : 'Switch to Git Source'}
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
      {/* TAB 2: DEPLOYMENTS (Dokploy / Vercel — Prod, Preview URLs, Rollback)  */}
      {/* ===================================================================== */}
      {activeTab === 'deployments' && (
        <div className="space-y-4">
          {/* Top Bar */}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
                {(
                  [
                    { id: 'all', label: 'All Releases' },
                    { id: 'production', label: 'Production' },
                    { id: 'preview', label: 'Preview URLs' },
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
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => {
                  void backendApi.clearDeployLogs(app.id);
                  onUpdateApp({
                    ...app,
                    deployments: app.deployments.slice(0, 1),
                  });
                  toast.success('Cleared deployment build logs history');
                }}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
              >
                <Trash2 className="h-3.5 w-3.5 text-zinc-400" />
                Clear History
              </button>
              <button
                type="button"
                onClick={() => triggerDeploy({ noCache: true })}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
              >
                <RefreshCw className="h-3.5 w-3.5 text-orange-400" />
                Redeploy (No Cache)
              </button>
              <button
                type="button"
                onClick={() => triggerDeploy()}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
              >
                <Play className="h-3.5 w-3.5 fill-current" />
                Deploy Now
              </button>
            </div>
          </div>

          {/* Active Production Release Card (Vercel / Dokploy Style) */}
          {activeProductionDeployment && (
            <div className="rounded-xl border border-emerald-500/25 bg-gradient-to-r from-emerald-500/[0.06] via-[#111115] to-[#111115] p-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-start gap-3.5 min-w-0">
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-emerald-500/30 bg-emerald-500/15">
                  <CheckCircle2 className="h-4 w-4 text-emerald-400" />
                </div>
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="rounded-md bg-emerald-500/15 px-2 py-0.5 font-mono text-[10px] font-semibold uppercase text-emerald-400 ring-1 ring-emerald-500/30">
                      Active Production
                    </span>
                    <span className="font-mono text-xs font-semibold text-zinc-100">
                      {activeProductionDeployment.id}
                    </span>
                    <span className="rounded bg-white/[0.06] px-1.5 py-0.5 font-mono text-[10px] text-orange-400">
                      {activeProductionDeployment.branch}@
                      {activeProductionDeployment.commitSha.slice(0, 7)}
                    </span>
                    <span className="text-xs text-zinc-300 truncate">
                      {activeProductionDeployment.commitMessage}
                    </span>
                  </div>

                  <div className="mt-1.5 flex flex-wrap items-center gap-3 font-mono text-[11px] text-zinc-400">
                    <a
                      href={productionUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-emerald-400 hover:underline"
                    >
                      <Globe className="h-3 w-3" />
                      {productionUrl}
                      <ArrowUpRight className="h-3 w-3" />
                    </a>
                    <span>·</span>
                    <span>Built in {activeProductionDeployment.durationSec}s</span>
                    <span>·</span>
                    <span>{activeProductionDeployment.startedAt}</span>
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-2 shrink-0">
                <button
                  type="button"
                  onClick={() => setBuildLogsDeployment(activeProductionDeployment)}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
                >
                  <TerminalIcon className="h-3.5 w-3.5 text-orange-400" />
                  Build Logs
                </button>
              </div>
            </div>
          )}

          {/* Deployments Table with Production / Preview URLs / Instant Rollback */}
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
            <table className="w-full table-fixed text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
                  <th className="w-[36%] px-4 py-2.5 font-medium">Release & Commit</th>
                  <th className="w-[18%] px-4 py-2.5 font-medium">Environment</th>
                  <th className="w-[26%] px-4 py-2.5 font-medium">Preview / Live URL</th>
                  <th className="w-[14%] px-4 py-2.5 font-medium text-right">Duration</th>
                  <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {enrichedDeployments.map((dep, index) => {
                  const isCurrentProd = index === 0 && dep.envType !== 'preview';
                  return (
                    <tr
                      key={dep.id}
                      className="hover:bg-white/[0.02] transition-colors"
                    >
                      {/* Column 1: Release + Commit */}
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2 min-w-0">
                          <StatusBadge status={dep.status} />
                          <span className="font-mono font-semibold text-zinc-100">{dep.id}</span>
                          <span className="rounded bg-white/[0.05] px-1.5 py-0.2 font-mono text-[10px] text-orange-400 shrink-0">
                            {dep.branch}@{dep.commitSha.slice(0, 7)}
                          </span>
                        </div>
                        <div className="mt-1 truncate text-[11px] text-zinc-400">
                          {dep.commitMessage}
                        </div>
                      </td>

                      {/* Column 2: Environment (Production / Preview PR / Rollback) */}
                      <td className="px-4 py-3">
                        {dep.envType === 'preview' ? (
                          <span className="inline-flex items-center gap-1 rounded-md border border-violet-500/25 bg-violet-500/10 px-2 py-0.5 font-mono text-[10px] font-medium text-violet-300">
                            <Sparkles className="h-2.5 w-2.5" />
                            {dep.prLabel || 'Preview'}
                          </span>
                        ) : dep.envType === 'rollback' ? (
                          <span className="inline-flex items-center gap-1 rounded-md border border-amber-500/25 bg-amber-500/10 px-2 py-0.5 font-mono text-[10px] font-medium text-amber-300">
                            <RotateCcw className="h-2.5 w-2.5" />
                            Rollback
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 rounded-md border border-emerald-500/25 bg-emerald-500/10 px-2 py-0.5 font-mono text-[10px] font-medium text-emerald-400">
                            Production
                            {isCurrentProd && ' · Live'}
                          </span>
                        )}
                      </td>

                      {/* Column 3: Clickable Preview / Production URL */}
                      <td className="px-4 py-3">
                        <a
                          href={dep.previewUrl}
                          target="_blank"
                          rel="noreferrer"
                          className="inline-flex items-center gap-1 truncate font-mono text-[11px] text-zinc-300 hover:text-orange-400 transition-colors max-w-full"
                        >
                          <Globe className="h-3 w-3 text-emerald-400 shrink-0" />
                          <span className="truncate">
                            {dep.previewUrl.replace('https://', '')}
                          </span>
                          <ArrowUpRight className="h-3 w-3 shrink-0 opacity-60" />
                        </a>
                      </td>

                      {/* Column 4: Duration */}
                      <td className="px-4 py-3 text-right font-mono tabular-nums text-[11px] text-zinc-400">
                        <div>{dep.durationSec}s</div>
                        <div className="text-[10px] text-zinc-500">{dep.startedAt}</div>
                      </td>

                      {/* Column 5: 3-Dot Action Menu */}
                      <td className="px-3 py-3 text-right">
                        <ActionMenu
                          items={[
                            {
                              label: 'View Build Logs',
                              icon: <TerminalIcon className="h-3.5 w-3.5" />,
                              onClick: () => setBuildLogsDeployment(dep),
                            },
                            ...(!isCurrentProd
                              ? [
                                  {
                                    label: 'Rollback to Release',
                                    icon: <RotateCcw className="h-3.5 w-3.5" />,
                                    onClick: () => triggerDeploy({ rollbackFrom: dep }),
                                  },
                                ]
                              : []),
                            {
                              label: 'Redeploy Commit',
                              icon: <RefreshCw className="h-3.5 w-3.5" />,
                              onClick: () => triggerDeploy(),
                            },
                            {
                              label: 'Open Release URL',
                              icon: <ExternalLink className="h-3.5 w-3.5" />,
                              onClick: () => window.open(dep.previewUrl, '_blank'),
                            },
                            {
                              label: 'Copy Commit SHA',
                              icon: <Copy className="h-3.5 w-3.5" />,
                              onClick: () => {
                                navigator.clipboard?.writeText(dep.commitSha);
                                toast.success(`Copied SHA ${dep.commitSha.slice(0, 7)}`);
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
      )}

      {/* ===================================================================== */}
      {/* TAB 3: DOMAINS (Clean Header + Auto-Port Detection + Preview Routing) */}
      {/* ===================================================================== */}
      {activeTab === 'domains' && (
        <ProjectDomainsTab app={app} onUpdateApp={onUpdateApp} />
      )}

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
                  onClick={() => {
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
                    onUpdateApp({ ...app, envVars: parsed });
                    setEnvRawMode(false);
                    toast.success(`Synced ${parsed.length} variables`);
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
                  onClick={() => {
                    const rawEnv = app.envVars.map((e) => `${e.key}=${e.value}`).join('\n');
                    void backendApi.saveAppEnv(app.id, rawEnv);
                    toast.success(`Saved ${app.envVars.length} variables to .env`);
                  }}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-all cursor-pointer"
                >
                  Save & Redeploy
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
          className="relative overflow-hidden rounded-xl border border-white/[0.08] bg-[#111115] shadow-xl select-none"
          onClick={() => {
            if (contextMenu) setContextMenu(null);
          }}
        >
          {/* 1. TOP CPANEL-STYLE FILE MANAGER ACTION TOOLBAR */}
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#0c0c0f] px-3.5 py-2">
            <div className="flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                onClick={() => {
                  setNewFileName('');
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

              {/* Bulk Delete when items are checked */}
              {selectedEntries.length > 0 && (
                <>
                  <div className="mx-0.5 h-4 w-px bg-white/[0.07]" />
                  <button
                    type="button"
                    onClick={() => {
                      setDeletedFilePaths((prev) => [...prev, ...selectedEntries]);
                      toast.success(`Deleted ${selectedEntries.length} selected item(s)`);
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
            <div className="lg:col-span-9 flex flex-col justify-between bg-[#111115] min-w-0">
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
                          <td className="px-3 py-2.5 text-[11px] text-zinc-500">0755</td>
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
                                onClick={() => openChmodFor(dirPath)}
                                className="hover:text-emerald-400 hover:underline cursor-pointer"
                              >
                                {filePermissions[dirPath] || 'drwxr-xr-x (0755)'}
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
                                    label: 'Permissions (chmod)',
                                    icon: <KeyRound className="h-3.5 w-3.5" />,
                                    onClick: () => openChmodFor(dirPath),
                                  },
                                  {
                                    label: 'Delete Folder',
                                    icon: <Trash2 className="h-3.5 w-3.5" />,
                                    variant: 'danger',
                                    onClick: () => {
                                      setDeletedFilePaths((prev) => [...prev, `dir:${dirPath}`]);
                                      toast.success(`Deleted folder /${dirPath}`);
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
                                onClick={() => openChmodFor(file.path)}
                                className="hover:underline cursor-pointer"
                              >
                                {filePermissions[file.path] || '-rw-r--r-- (0644)'}
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
                                    onClick: () => {
                                      setMoveTarget({
                                        path: file.path,
                                        name: file.name,
                                        mode: 'move',
                                      });
                                      setMoveDestInput(currentDir || 'config');
                                      setShowInlineFolderCreator(false);
                                    },
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
                                    onClick: () => toast.success(`Downloaded ${file.name}`),
                                  },
                                  {
                                    label: 'Delete',
                                    icon: <Trash2 className="h-3.5 w-3.5" />,
                                    variant: 'danger',
                                    onClick: () => {
                                      setDeletedFilePaths((prev) => [...prev, file.path]);
                                      toast.success(`Deleted ${file.path}`);
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
                        onClick={() => openChmodFor(selectedFile.path)}
                        className="hidden md:inline-block font-mono text-[11px] text-emerald-400 hover:underline cursor-pointer px-1.5"
                      >
                        {filePermissions[selectedFile.path] || '-rw-r--r-- (0644)'}
                      </button>
                    </div>
                  </div>

                  <CodeEditorBox
                    filename={selectedFile.path}
                    language={selectedFile.language}
                    value={selectedFile.content}
                    onChange={handleSaveFileContent}
                    onSave={() => {
                      void backendApi.fileSave(app.id, selectedFile.path, selectedFile.content);
                      toast.success(`Saved ${selectedFile.path}`);
                    }}
                    permissions={filePermissions[selectedFile.path] || '-rw-r--r-- (0644)'}
                    height={420}
                    actions={
                      <button
                        type="button"
                        onClick={() => {
                          void backendApi.fileSave(app.id, selectedFile.path, selectedFile.content);
                          void backendApi.composeAction(app.id, 'restart');
                          toast.success(`Saved ${selectedFile.path} & restarted compose services`);
                        }}
                        className="inline-flex h-7 items-center gap-1 rounded-lg border border-orange-500/30 bg-orange-500/15 px-2.5 text-[11px] font-semibold text-orange-300 hover:bg-orange-500/25 cursor-pointer"
                      >
                        <RotateCw className="h-3 w-3" />
                        <span className="hidden sm:inline">Save & Restart</span>
                        <span className="sm:hidden">Restart</span>
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
                top: Math.min(contextMenu.y, window.innerHeight - 310),
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
                  <button
                    type="button"
                    onClick={() => navigateToFolder(contextMenu.targetPath)}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                  >
                    <FolderOpen className="h-3.5 w-3.5 text-amber-400" />
                    <span>Open Folder</span>
                  </button>
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

                {!contextMenu.isFolder && (
                  <button
                    type="button"
                    onClick={() => {
                      setMoveTarget({
                        path: contextMenu.targetPath,
                        name: contextMenu.targetName,
                        mode: 'move',
                      });
                      setMoveDestInput(currentDir || 'config');
                      setContextMenu(null);
                    }}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
                  >
                    <Copy className="h-3.5 w-3.5 text-sky-400" />
                    <span>Copy / Move to...</span>
                  </button>
                )}

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
                      toast.success(`Downloaded ${contextMenu.targetName}`);
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
                    if (contextMenu.isFolder) {
                      setDeletedFilePaths((prev) => [...prev, `dir:${contextMenu.targetPath}`]);
                      toast.success(`Deleted folder /${contextMenu.targetPath}`);
                    } else {
                      setDeletedFilePaths((prev) => [...prev, contextMenu.targetPath]);
                      toast.success(`Deleted ${contextMenu.targetPath}`);
                    }
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
                onClick={appendSimulatedLog}
                className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
              >
                + Event
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
              {filteredLogs.map((l) => (
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
              ))}
            </div>
          </div>
        </div>
      )}

      {/* ===================================================================== */}
      {/* TAB 10: TERMINAL                                                      */}
      {/* ===================================================================== */}
      {activeTab === 'terminal' && (
        <XTermConsole
          hostLabel={`root@${app.slug}`}
          promptPrefix={`root@${app.slug}:/app# `}
          presets={['docker compose ps', 'env', 'ls -la', 'top -b -n 1 | head -n 12', 'clear']}
          height={390}
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
                      <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-200 uppercase">
                        {c.role}
                      </span>
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
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Domain</label>
            <input
              type="text"
              required
              value={newDomainName}
              onChange={(e) => setNewDomainName(e.target.value)}
              placeholder={`api.${app.slug}.com`}
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

            <div className="rounded-xl border border-white/[0.07] bg-[#070709] p-4 space-y-2 leading-relaxed">
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
                const fileBase = (newFileName.split('/').pop() || 'index.ts').trim();
                const currentFileDir = newFileName.includes('/')
                  ? newFileName.slice(0, newFileName.lastIndexOf('/'))
                  : currentDir;
                const active = currentFileDir === dir;
                return (
                  <button
                    key={dir || 'root'}
                    type="button"
                    onClick={() => setNewFileName(dir ? `${dir}/${fileBase}` : fileBase)}
                    className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 font-mono text-[11px] transition-colors cursor-pointer ${
                      active
                        ? 'border-amber-500/50 bg-amber-500/15 text-amber-300 font-semibold'
                        : 'border-white/[0.06] bg-zinc-900/80 text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    <Folder className="h-3 w-3 text-amber-400 fill-amber-400/20" />
                    <span>{dir ? `/${dir}` : '/ (root)'}</span>
                  </button>
                );
              })}
            </div>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              File Name & Path
            </label>
            <input
              type="text"
              required
              value={newFileName}
              onChange={(e) => setNewFileName(e.target.value)}
              placeholder={currentDir ? `${currentDir}/worker.ts` : 'src/worker.ts'}
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
                const leaf = (newFolderName.split('/').pop() || '').trim();
                const parentPart = newFolderName.includes('/')
                  ? newFolderName.slice(0, newFolderName.lastIndexOf('/'))
                  : currentDir;
                const active = parentPart === dir;
                return (
                  <button
                    key={dir || 'root'}
                    type="button"
                    onClick={() =>
                      setNewFolderName(dir ? `${dir}/${leaf || 'new-folder'}` : leaf || 'new-folder')
                    }
                    className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 font-mono text-[11px] transition-colors cursor-pointer ${
                      active
                        ? 'border-amber-500/50 bg-amber-500/15 text-amber-300 font-semibold'
                        : 'border-white/[0.06] bg-zinc-900/80 text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    <Folder className="h-3 w-3 text-amber-400 fill-amber-400/20" />
                    <span>{dir ? `/${dir}` : '/ (root)'}</span>
                  </button>
                );
              })}
            </div>
          </div>

          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Folder Path
            </label>
            <input
              type="text"
              required
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder={currentDir ? `${currentDir}/migrations` : 'migrations or src/middleware'}
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
            onSubmit={(e) => {
              e.preventDefault();
              setFilePermissions((prev) => ({
                ...prev,
                [chmodModalFile]: computedOctal.symbolic,
              }));
              toast.success(
                `Updated permissions for ${chmodModalFile} to ${computedOctal.octal}`
              );
              setChmodModalFile(null);
            }}
            className="space-y-4"
          >
            <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 font-mono text-xs">
              <span className="text-zinc-300 truncate">{chmodModalFile}</span>
              <span className="rounded bg-emerald-500/15 px-2 py-0.5 text-emerald-300 font-semibold">
                {computedOctal.symbolic}
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
        title="Copy / Move File"
        icon={<Copy className="h-4 w-4 text-sky-400" />}
      >
        {moveTarget && (
          <form onSubmit={handleMoveOrCopySubmit} className="space-y-4">
            {/* Source File Indicator */}
            <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 font-mono text-xs">
              <span className="flex items-center gap-2 text-zinc-200 truncate">
                <DevFileIcon filename={moveTarget.name} />
                <span className="truncate font-semibold">{moveTarget.path}</span>
              </span>
              <span className="rounded bg-white/[0.06] px-1.5 py-0.5 text-[10px] uppercase text-zinc-400 shrink-0">
                Source
              </span>
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
                <span>Resulting Path:</span>
                <span className="text-emerald-400 font-semibold truncate ml-2">
                  /data/apps/{app.slug}/
                  {moveDestInput.trim().replace(/^\/+|\/+$/g, '')
                    ? `${moveDestInput.trim().replace(/^\/+|\/+$/g, '')}/${moveTarget.name}`
                    : moveTarget.name}
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
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                File Name {currentDir ? `(into /${currentDir})` : '(into root)'}
              </label>
              <input
                type="text"
                required
                value={uploadFileName}
                onChange={(e) => setUploadFileName(e.target.value)}
                placeholder="release-assets.zip or .env.production"
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
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
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              {uploadMode === 'url' ? 'Fetch URL' : 'Upload'}
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
              newMemberRole === 'deployer' ? 'developer' : 'viewer'
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
                  addedAt: 'Just now',
                },
              ],
            });
            setNewMemberEmail('');
            setMemberModalOpen(false);
            toast.success('Collaborator invited');
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
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">Role</label>
            <select
              value={newMemberRole}
              onChange={(e) => setNewMemberRole(e.target.value as 'deployer' | 'viewer')}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs text-zinc-100"
            >
              <option value="deployer">Deployer (Deploy, Restart, Logs, Env)</option>
              <option value="viewer">Viewer (Read-only Metrics & Logs)</option>
            </select>
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
        title="Container Resource Limits"
        icon={<Sliders className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setLimitsModalOpen(false);
            toast.success(`Updated container limits (${cpuQuota} vCPU / ${memQuota} MB)`);
          }}
          className="space-y-4"
        >
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Max CPU (vCPU)
              </label>
              <input
                type="text"
                value={cpuQuota}
                onChange={(e) => setCpuQuota(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
            <div>
              <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
                Max Memory (MB)
              </label>
              <input
                type="text"
                value={memQuota}
                onChange={(e) => setMemQuota(e.target.value)}
                className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
              />
            </div>
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setLimitsModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Save Limits
            </button>
          </div>
        </form>
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
    </div>
  );
};
