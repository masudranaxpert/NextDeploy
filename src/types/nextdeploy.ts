export type NavigationSection =
  | 'overview'
  | 'monitor'
  | 'apps'
  | 'containers'
  | 'images'
  | 'volumes'
  | 'terminal'
  | 'git'
  | 'registries'
  | 'backup'
  | 'mcp'
  | 'cli-sessions'
  | 'migrate'
  | 'proxy'
  | 'settings'
  | 'audit-logs'
  | 'users';

export type AppWorkspaceTab =
  | 'overview'
  | 'deployments'
  | 'domains'
  | 'files'
  | 'git'
  | 'env'
  | 'logs'
  | 'terminal'
  | 'volumes'
  | 'backups'
  | 'collaborators';

export type AppSourceType = 'git' | 'zip' | 'compose';
export type AppStatus = 'running' | 'building' | 'stopped' | 'failed';

export interface ContainerService {
  id: string;
  name: string;
  service: string;
  image: string;
  state: 'running' | 'restarting' | 'exited';
  uptime: string;
  cpuPct: number;
  memUsedMB: number;
  memLimitMB: number;
  ports: string;
}

export interface DomainRoute {
  id: string;
  domain: string;
  targetService: string;
  targetPort: number;
  tlsProvider: 'LetsEncrypt' | 'ZeroSSL' | 'Internal';
  tlsStatus: 'active' | 'provisioning' | 'error';
  forceHttps: boolean;
  enableWww?: boolean;
  serveStatic?: boolean;
  staticPath?: string;
  staticUrlPrefix?: string;
  serveMedia?: boolean;
  mediaPath?: string;
  mediaUrlPrefix?: string;
  gzip: boolean;
  createdAt: string;
}

export interface ComposeFileNode {
  path: string;
  name: string;
  type: 'file' | 'dir';
  sizeBytes: number;
  updatedAt: string;
  language?: 'yaml' | 'dockerfile' | 'env' | 'json' | 'nginx' | 'sh' | 'markdown';
  content: string;
}

export interface EnvVarItem {
  id: string;
  key: string;
  value: string;
  isSecret: boolean;
  comment?: string;
}

export interface DeploymentRecord {
  id: string;
  commitSha: string;
  commitMessage: string;
  branch: string;
  trigger: 'git-webhook' | 'nd-cli' | 'mcp-agent' | 'manual-compose';
  author: string;
  status: 'ready' | 'building' | 'failed';
  durationSec: number;
  startedAt: string;
  buildSteps: {
    step?: string;
    name?: string;
    durationMs?: number;
    duration?: string;
    status: 'done' | 'running' | 'pending' | 'error';
    output?: string[];
    logs?: string[];
  }[];
}

export interface LogEntry {
  id: string;
  timestamp: string;
  container: string;
  level: 'info' | 'warn' | 'error' | 'debug';
  message: string;
}

export interface AppCollaborator {
  userId: string;
  username: string;
  email: string;
  role: 'owner' | 'deployer' | 'viewer';
  addedAt: string;
}

export interface AppVolumeMount {
  name: string;
  mountPath: string;
  mountPoint?: string;
  driver: string;
  sizeMB: number;
  lastBackupAt?: string;
}

export interface AppStack {
  id: string;
  name: string;
  slug: string;
  description: string;
  sourceType: AppSourceType;
  gitSync?: boolean;
  gitAuthMode?: 'public' | 'github_app' | 'gitlab_token';
  gitProviderId?: string;
  gitRepo?: string;
  gitBranch?: string;
  gitCommitSha?: string;
  gitCommitMessage?: string;
  webhookAutoDeploy: boolean;
  webhookSecret?: string;
  status: AppStatus;
  primaryDomain?: string;
  composeFilePath: string;
  owner: string;
  updatedAt: string;
  cpuPct: number;
  memMB: number;
  diskMB: number;
  reqPerMin: number;
  services: ContainerService[];
  domains: DomainRoute[];
  files: ComposeFileNode[];
  envVars: EnvVarItem[];
  deployments: DeploymentRecord[];
  logs: LogEntry[];
  volumes: AppVolumeMount[];
  collaborators: AppCollaborator[];
}

export interface DockerImageItem {
  id: string;
  repository: string;
  tag: string;
  sizeMB: number;
  created: string;
  inUseBy: string[];
}

export interface DockerVolumeItem {
  name: string;
  appSlug: string;
  mountPoint: string;
  driver: string;
  sizeMB: number;
  createdAt: string;
  filesCount: number;
  sampleFiles: { path: string; size: string; modified: string }[];
}

export interface GitProviderConnection {
  id: string;
  provider: 'GitHub' | 'GitLab';
  authMethod: 'GitHub App' | 'OAuth 2.0' | 'Fine-Grained PAT' | 'Deploy Token';
  accountName: string;
  instanceUrl: string;
  status: 'connected' | 'error';
  reposAccessible: number;
  webhookDeliveryRate: string;
  webhookUrl: string;
  lastSynced: string;
}

export interface SyncedGitRepository {
  id: string;
  provider: 'GitHub' | 'GitLab';
  fullName: string;
  defaultBranch: string;
  private: boolean;
  linkedAppSlug?: string;
  lastCommitSha: string;
  lastPushAt: string;
  autoDeploy: boolean;
}

export interface RegistryCredential {
  id: string;
  provider: 'GHCR' | 'DockerHub' | 'GitLab' | 'AWS ECR' | 'Custom';
  name: string;
  registryUrl: string;
  username: string;
  tokenPreview: string;
  scope: 'global' | 'personal';
  connectedApps: number;
  imagesCount: number;
  status: 'connected' | 'error';
  lastVerified: string;
}

export interface BackupDestination {
  id: string;
  name: string;
  provider: 'Cloudflare R2' | 'Google Drive';
  providerCode: 'r2' | 'gdrive';
  bucket: string;
  endpoint: string;
  region: string;
  accountId?: string;
  email?: string;
  displayName?: string;
  folderId?: string;
  infoError?: string;
  createdAt?: string;
  status: 'connected' | 'provisioning' | 'error';
  scheduleCron: string;
  retentionDays: number;
  usedGB: number;
  quotaGB: number;
  lastBackupAt: string;
}

export interface BackupScheduleItem {
  id: string;
  appId: string;
  appSlug: string;
  destinationId: string;
  destinationName: string;
  provider: 'Cloudflare R2' | 'Google Drive';
  destinationProvider?: 'r2' | 'gdrive';
  type: 'full' | 'volume' | 'app';
  volumeNames: string | string[];
  cron: string;
  retention: number;
  pauseContainers: boolean;
  enabled: boolean;
  lastRun?: string;
  lastRunAt?: string;
  nextRunAt?: string;
}

export interface BackupArchiveRecord {
  id: string;
  appId?: string;
  appSlug: string;
  destinationId?: string;
  destinationName: string;
  destinationProvider?: 'r2' | 'gdrive';
  provider?: 'Cloudflare R2' | 'Google Drive';
  archiveName: string;
  remotePath?: string;
  type?: 'full' | 'volume' | 'app';
  backupType?: 'full' | 'volume' | 'app';
  volumeName?: string;
  sizeMB: number;
  sizeBytes?: number;
  createdAt: string;
  status: 'completed' | 'building' | 'running' | 'failed';
  errorMessage?: string;
  includesVolumes: boolean;
  includesEnv: boolean;
  pauseContainers?: boolean;
  remoteMissing?: boolean;
  logOutput?: string;
}

export interface MigrationExportBundle {
  id: string;
  createdAt: string;
  selectedApps: string[];
  workspaceBytes?: number;
  volumeBytes?: number;
  estimatedSizeMB: number;
  actualSizeMB: number;
  status: 'ready' | 'running' | 'failed';
  token: string;
  downloadUrl?: string;
  migrateCmd?: string;
  expiresIn: string;
  progressLog: string[];
}

export interface McpToolItem {
  name: string;
  category: 'Workspace & Files' | 'Deploy & Compose' | 'Logs & Telemetry' | 'Domains & Env';
  description: string;
  signature: string;
  readOnly: boolean;
}

export interface CliSessionItem {
  id: string;
  tokenName: string;
  prefix: string;
  fullToken?: string;
  deviceHost: string;
  osArch: string;
  cliVersion: string;
  lastCommand: string;
  lastActiveAt: string;
  ipAddress: string;
}

export interface PanelUser {
  id: string;
  username: string;
  email: string;
  role: 'admin' | 'user';
  status?: 'active' | 'suspended';
  appsCount: number;
  maxApps: number;
  cpuQuotaCores: number;
  memUsedGB: number;
  memQuotaGB: number;
  maxMemoryMB?: number;
  diskUsedGB: number;
  diskQuotaGB: number;
  maxStorageMB?: number;
  allowDomainFileServer?: boolean;
  twoFactorEnabled: boolean;
  lastLoginAt: string;
}

export interface AuditLogEntry {
  id: string;
  timestamp: string;
  actor: string;
  actorRole: 'admin' | 'user' | 'mcp-agent' | 'nd-cli';
  action: string;
  resourceType?: string;
  target: string;
  details?: string;
  ip: string;
  userAgent?: string;
  status: 'success' | 'blocked';
}

export interface TelemetryPoint {
  time: string;
  cpu: number;
  memory: number;
  diskIo: number;
  netInMbps: number;
  netOutMbps: number;
  requests: number;
}
