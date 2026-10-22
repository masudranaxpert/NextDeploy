import {
  AppStack,
  AuditLogEntry,
  BackupArchiveRecord,
  BackupDestination,
  CliSessionItem,
  DockerImageItem,
  DockerVolumeItem,
  GitProviderConnection,
  McpToolItem,
  MigrationExportBundle,
  PanelUser,
  RegistryCredential,
  SyncedGitRepository,
  TelemetryPoint,
} from '../types/nextdeploy';

export const INITIAL_TELEMETRY: TelemetryPoint[] = [
  { time: '18:00', cpu: 18, memory: 42, diskIo: 12, netInMbps: 24, netOutMbps: 19, requests: 1240 },
  { time: '18:02', cpu: 22, memory: 43, diskIo: 15, netInMbps: 31, netOutMbps: 24, requests: 1410 },
  { time: '18:04', cpu: 19, memory: 43, diskIo: 11, netInMbps: 28, netOutMbps: 22, requests: 1380 },
  { time: '18:06', cpu: 34, memory: 47, diskIo: 38, netInMbps: 54, netOutMbps: 41, requests: 2190 },
  { time: '18:08', cpu: 48, memory: 52, diskIo: 62, netInMbps: 72, netOutMbps: 58, requests: 2840 },
  { time: '18:10', cpu: 29, memory: 49, diskIo: 24, netInMbps: 46, netOutMbps: 39, requests: 1920 },
  { time: '18:12', cpu: 24, memory: 48, diskIo: 18, netInMbps: 38, netOutMbps: 31, requests: 1690 },
  { time: '18:14', cpu: 21, memory: 47, diskIo: 14, netInMbps: 33, netOutMbps: 27, requests: 1540 },
  { time: '18:16', cpu: 26, memory: 48, diskIo: 19, netInMbps: 42, netOutMbps: 35, requests: 1810 },
  { time: '18:18', cpu: 39, memory: 51, diskIo: 44, netInMbps: 61, netOutMbps: 49, requests: 2460 },
  { time: '18:20', cpu: 31, memory: 50, diskIo: 29, netInMbps: 49, netOutMbps: 40, requests: 2080 },
  { time: '18:22', cpu: 25, memory: 48, diskIo: 17, netInMbps: 39, netOutMbps: 33, requests: 1760 },
  { time: '18:24', cpu: 23, memory: 48, diskIo: 16, netInMbps: 37, netOutMbps: 30, requests: 1710 },
];

export const INITIAL_APPS: AppStack[] = [
  {
    id: 'app-9f81c2a0',
    name: 'commerce-core-api',
    slug: 'commerce-core-api',
    description: 'Go 1.24 + PostgreSQL 16 + Redis 7 production stack',
    sourceType: 'git',
    gitRepo: 'github.com/masudranaxpert/commerce-core-api',
    gitBranch: 'main',
    gitCommitSha: '8c4f19a',
    gitCommitMessage: 'perf(checkout): batch inventory reservation lock in redis',
    webhookAutoDeploy: true,
    status: 'running',
    primaryDomain: 'api.nextdeploy.cloud',
    composeFilePath: 'docker-compose.yml',
    owner: 'masud',
    updatedAt: '4m ago',
    cpuPct: 14.2,
    memMB: 842,
    diskMB: 4120,
    reqPerMin: 1480,
    services: [
      {
        id: 'c-901a8b12',
        name: 'commerce-core-api-web-1',
        service: 'web',
        image: 'ghcr.io/masudranaxpert/commerce-api:8c4f19a',
        state: 'running',
        uptime: '6d 14h',
        cpuPct: 9.4,
        memUsedMB: 312,
        memLimitMB: 1024,
        ports: '3000/tcp -> caddy:443',
      },
      {
        id: 'c-901a8b13',
        name: 'commerce-core-api-postgres-1',
        service: 'postgres',
        image: 'postgres:16.4-alpine',
        state: 'running',
        uptime: '19d 04h',
        cpuPct: 3.6,
        memUsedMB: 428,
        memLimitMB: 2048,
        ports: '5432/tcp (internal)',
      },
      {
        id: 'c-901a8b14',
        name: 'commerce-core-api-redis-1',
        service: 'redis',
        image: 'redis:7.4-alpine',
        state: 'running',
        uptime: '19d 04h',
        cpuPct: 1.2,
        memUsedMB: 102,
        memLimitMB: 512,
        ports: '6379/tcp (internal)',
      },
    ],
    domains: [
      {
        id: 'dom-1',
        domain: 'api.nextdeploy.cloud',
        targetService: 'web',
        targetPort: 3000,
        tlsProvider: 'LetsEncrypt',
        tlsStatus: 'active',
        forceHttps: true,
        gzip: true,
        createdAt: '2026-09-14',
      },
      {
        id: 'dom-2',
        domain: 'webhooks.nextdeploy.cloud',
        targetService: 'web',
        targetPort: 3000,
        tlsProvider: 'LetsEncrypt',
        tlsStatus: 'active',
        forceHttps: true,
        gzip: true,
        createdAt: '2026-09-18',
      },
    ],
    files: [
      {
        path: 'docker-compose.yml',
        name: 'docker-compose.yml',
        type: 'file',
        sizeBytes: 1420,
        updatedAt: '4m ago',
        language: 'yaml',
        content: `services:
  web:
    build:
      context: .
      dockerfile: Dockerfile
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_started
    networks:
      - nextdeploy_net
      - internal

  postgres:
    image: postgres:16.4-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: \${DB_USER}
      POSTGRES_PASSWORD: \${DB_PASSWORD}
      POSTGRES_DB: commerce_prod
    volumes:
      - pg_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U \${DB_USER}"]
      interval: 5s
      timeout: 3s
      retries: 5
    networks:
      - internal

  redis:
    image: redis:7.4-alpine
    command: redis-server --appendonly yes
    volumes:
      - redis_data:/data
    networks:
      - internal

volumes:
  pg_data:
  redis_data:

networks:
  nextdeploy_net:
    external: true
  internal:
    driver: bridge`,
      },
      {
        path: '.nextdeploy.generated.compose.yml',
        name: '.nextdeploy.generated.compose.yml',
        type: 'file',
        sizeBytes: 618,
        updatedAt: '4m ago',
        language: 'yaml',
        content: `# Auto-generated by NextDeploy Caddy Route Engine
services:
  web:
    labels:
      caddy_0: api.nextdeploy.cloud
      caddy_0.reverse_proxy: "{{upstreams 3000}}"
      caddy_0.encode: "zstd gzip"
      caddy_0.tls.protocols: "tls1.2 tls1.3"
      caddy_1: webhooks.nextdeploy.cloud
      caddy_1.reverse_proxy: "{{upstreams 3000}}"
      nextdeploy.app.id: "app-9f81c2a0"
      nextdeploy.app.revision: "8c4f19a"`,
      },
      {
        path: 'Dockerfile',
        name: 'Dockerfile',
        type: 'file',
        sizeBytes: 512,
        updatedAt: '2d ago',
        language: 'dockerfile',
        content: `FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/commerce-api ./cmd/server

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /bin/commerce-api /bin/commerce-api
EXPOSE 3000
ENTRYPOINT ["/bin/commerce-api"]`,
      },
    ],
    envVars: [
      { id: 'e1', key: 'PORT', value: '3000', isSecret: false },
      { id: 'e2', key: 'GO_ENV', value: 'production', isSecret: false },
      { id: 'e3', key: 'DB_USER', value: 'nd_commerce', isSecret: false },
      { id: 'e4', key: 'DB_PASSWORD', value: 'pg_live_99a8b7c6d5e4f321', isSecret: true },
      { id: 'e5', key: 'REDIS_URL', value: 'redis://redis:6379/0', isSecret: false },
      { id: 'e6', key: 'STRIPE_WEBHOOK_SECRET', value: 'whsec_8f9102ab773c9102d11e', isSecret: true },
    ],
    deployments: [
      {
        id: 'dep-4920',
        commitSha: '8c4f19a',
        commitMessage: 'perf(checkout): batch inventory reservation lock in redis',
        branch: 'main',
        trigger: 'git-webhook',
        author: 'masudranaxpert',
        status: 'ready',
        durationSec: 28,
        startedAt: '4m ago',
        buildSteps: [],
      },
      {
        id: 'dep-4919',
        commitSha: '3b10e4c',
        commitMessage: 'feat(orders): add idempotency key header middleware',
        branch: 'main',
        trigger: 'nd-cli',
        author: 'masudranaxpert',
        status: 'ready',
        durationSec: 31,
        startedAt: '5h ago',
        buildSteps: [],
      },
      {
        id: 'dep-4918',
        commitSha: 'a19d02f',
        commitMessage: 'fix(migrations): add partial index on active carts',
        branch: 'main',
        trigger: 'mcp-agent',
        author: 'claude-code-mcp',
        status: 'ready',
        durationSec: 24,
        startedAt: '1d ago',
        buildSteps: [],
      },
    ],
    logs: [
      { id: 'l1', timestamp: '2026-10-19T18:24:11.104Z', container: 'web', level: 'info', message: 'server listening on :3000 (go1.24.2 linux/amd64)' },
      { id: 'l2', timestamp: '2026-10-19T18:24:14.882Z', container: 'postgres', level: 'info', message: 'database system is ready to accept connections' },
      { id: 'l3', timestamp: '2026-10-19T18:24:22.419Z', container: 'web', level: 'info', message: 'POST /v1/checkout/reserve 200 OK duration=3.8ms lock_key=sku_9941' },
      { id: 'l4', timestamp: '2026-10-19T18:24:31.005Z', container: 'redis', level: 'debug', message: 'DB 0: 14,820 keys (412 volatile) in 16384 slots HT' },
      { id: 'l5', timestamp: '2026-10-19T18:24:44.712Z', container: 'web', level: 'warn', message: 'rate_limiter: client 185.220.101.42 approaching burst threshold (88/100 req/10s)' },
      { id: 'l6', timestamp: '2026-10-19T18:24:51.390Z', container: 'web', level: 'info', message: 'GET /healthz 200 OK duration=0.4ms caddy_probe=true' },
      { id: 'l7', timestamp: '2026-10-19T18:24:55.901Z', container: 'web', level: 'error', message: 'webhook_dispatch: upstream timeout delivering event evt_3Q91a (retrying in 5s)' },
    ],
    volumes: [
      { name: 'commerce-core-api_pg_data', mountPath: '/var/lib/postgresql/data', mountPoint: '/var/lib/postgresql/data', driver: 'local', sizeMB: 3840, lastBackupAt: '2h ago' },
      { name: 'commerce-core-api_redis_data', mountPath: '/data', mountPoint: '/data', driver: 'local', sizeMB: 280, lastBackupAt: '2h ago' },
    ],
    collaborators: [
      { userId: 'u1', username: 'masud', email: 'masud132213@gmail.com', role: 'owner', addedAt: '2026-08-01' },
      { userId: 'u2', username: 'arina.sre', email: 'arina@nextdeploy.cloud', role: 'deployer', addedAt: '2026-09-12' },
    ],
  },
  {
    id: 'app-7d24e8b1',
    name: 'studio-web-dashboard',
    slug: 'studio-web-dashboard',
    description: 'Next.js 15 standalone SSR storefront & customer portal',
    sourceType: 'git',
    gitRepo: 'github.com/masudranaxpert/studio-web-dashboard',
    gitBranch: 'production',
    gitCommitSha: 'f42a90c',
    gitCommitMessage: 'feat(billing): integrate instant SEPA & invoice PDF stream',
    webhookAutoDeploy: true,
    status: 'running',
    primaryDomain: 'app.nextdeploy.cloud',
    composeFilePath: 'docker-compose.yml',
    owner: 'masud',
    updatedAt: '38m ago',
    cpuPct: 8.6,
    memMB: 518,
    diskMB: 1290,
    reqPerMin: 940,
    services: [
      {
        id: 'c-771b201',
        name: 'studio-web-dashboard-app-1',
        service: 'app',
        image: 'ghcr.io/masudranaxpert/studio-web:f42a90c',
        state: 'running',
        uptime: '12d 09h',
        cpuPct: 8.6,
        memUsedMB: 518,
        memLimitMB: 1024,
        ports: '3000/tcp -> caddy:443',
      },
    ],
    domains: [
      {
        id: 'dom-3',
        domain: 'app.nextdeploy.cloud',
        targetService: 'app',
        targetPort: 3000,
        tlsProvider: 'LetsEncrypt',
        tlsStatus: 'active',
        forceHttps: true,
        gzip: true,
        createdAt: '2026-09-02',
      },
    ],
    files: [
      {
        path: 'docker-compose.yml',
        name: 'docker-compose.yml',
        type: 'file',
        sizeBytes: 640,
        updatedAt: '38m ago',
        language: 'yaml',
        content: `services:
  app:
    build: .
    restart: always
    env_file: .env
    networks:
      - nextdeploy_net

networks:
  nextdeploy_net:
    external: true`,
      },
    ],
    envVars: [
      { id: 'e21', key: 'NODE_ENV', value: 'production', isSecret: false },
      { id: 'e22', key: 'NEXT_PUBLIC_API_URL', value: 'https://api.nextdeploy.cloud', isSecret: false },
      { id: 'e23', key: 'AUTH_SECRET', value: 'nd_auth_secret_883190f2a1', isSecret: true },
    ],
    deployments: [
      {
        id: 'dep-4915',
        commitSha: 'f42a90c',
        commitMessage: 'feat(billing): integrate instant SEPA & invoice PDF stream',
        branch: 'production',
        trigger: 'git-webhook',
        author: 'masudranaxpert',
        status: 'ready',
        durationSec: 42,
        startedAt: '38m ago',
        buildSteps: [],
      },
    ],
    logs: [
      { id: 'l21', timestamp: '2026-10-19T18:22:10.004Z', container: 'app', level: 'info', message: '▲ Next.js 15.1.0 — Ready in 148ms on 0.0.0.0:3000' },
      { id: 'l22', timestamp: '2026-10-19T18:24:01.219Z', container: 'app', level: 'info', message: 'SSR /dashboard/deployments rendered in 11.2ms (cache: HIT)' },
    ],
    volumes: [],
    collaborators: [
      { userId: 'u1', username: 'masud', email: 'masud132213@gmail.com', role: 'owner', addedAt: '2026-08-01' },
    ],
  },
  {
    id: 'app-3c90a1f4',
    name: 'plausible-analytics',
    slug: 'plausible-analytics',
    description: 'Self-hosted ClickHouse + PostgreSQL telemetry stack',
    sourceType: 'compose',
    gitBranch: 'v2.1.4',
    gitCommitSha: 'compose-zip',
    gitCommitMessage: 'Uploaded compose workspace bundle via nd push',
    webhookAutoDeploy: false,
    status: 'running',
    primaryDomain: 'stats.nextdeploy.cloud',
    composeFilePath: 'docker-compose.yml',
    owner: 'masud',
    updatedAt: '2d ago',
    cpuPct: 5.8,
    memMB: 1420,
    diskMB: 8940,
    reqPerMin: 620,
    services: [
      {
        id: 'c-551a1',
        name: 'plausible-analytics-plausible-1',
        service: 'plausible',
        image: 'ghcr.io/plausible/community-edition:v2.1',
        state: 'running',
        uptime: '24d 11h',
        cpuPct: 3.1,
        memUsedMB: 410,
        memLimitMB: 1024,
        ports: '8000/tcp -> caddy:443',
      },
      {
        id: 'c-551a2',
        name: 'plausible-analytics-clickhouse-1',
        service: 'events_db',
        image: 'clickhouse/clickhouse-server:24.3-alpine',
        state: 'running',
        uptime: '24d 11h',
        cpuPct: 2.7,
        memUsedMB: 1010,
        memLimitMB: 2048,
        ports: '8123/tcp (internal)',
      },
    ],
    domains: [
      {
        id: 'dom-4',
        domain: 'stats.nextdeploy.cloud',
        targetService: 'plausible',
        targetPort: 8000,
        tlsProvider: 'ZeroSSL',
        tlsStatus: 'active',
        forceHttps: true,
        gzip: true,
        createdAt: '2026-08-19',
      },
    ],
    files: [
      {
        path: 'docker-compose.yml',
        name: 'docker-compose.yml',
        type: 'file',
        sizeBytes: 980,
        updatedAt: '2d ago',
        language: 'yaml',
        content: `services:
  plausible:
    image: ghcr.io/plausible/community-edition:v2.1
    restart: always
    depends_on:
      - events_db
    volumes:
      - plausible_data:/var/lib/plausible
    networks:
      - nextdeploy_net

  events_db:
    image: clickhouse/clickhouse-server:24.3-alpine
    restart: always
    volumes:
      - event_data:/var/lib/clickhouse
    networks:
      - nextdeploy_net

volumes:
  plausible_data:
  event_data:

networks:
  nextdeploy_net:
    external: true`,
      },
    ],
    envVars: [
      { id: 'e31', key: 'BASE_URL', value: 'https://stats.nextdeploy.cloud', isSecret: false },
      { id: 'e32', key: 'SECRET_KEY_BASE', value: 'ch_sec_9920184a7b6c5d4e3f2a1', isSecret: true },
    ],
    deployments: [],
    logs: [
      { id: 'l31', timestamp: '2026-10-19T18:20:11.000Z', container: 'events_db', level: 'info', message: 'ClickHouse 24.3.2.23 merged 4 parts in events_local (142,900 rows)' },
    ],
    volumes: [
      { name: 'plausible-analytics_event_data', mountPath: '/var/lib/clickhouse', mountPoint: '/var/lib/clickhouse', driver: 'local', sizeMB: 8420, lastBackupAt: '6h ago' },
    ],
    collaborators: [
      { userId: 'u1', username: 'masud', email: 'masud132213@gmail.com', role: 'owner', addedAt: '2026-08-19' },
    ],
  },
  {
    id: 'app-6b12f0e9',
    name: 'temporal-worker-queue',
    slug: 'temporal-worker-queue',
    description: 'Async video transcoding & PDF generation worker fleet',
    sourceType: 'git',
    gitRepo: 'gitlab.com/nextdeploy-internal/temporal-worker-queue',
    gitBranch: 'main',
    gitCommitSha: '19d04e2',
    gitCommitMessage: 'chore(ffmpeg): bump worker concurrency limit to 8 threads',
    webhookAutoDeploy: true,
    status: 'building',
    primaryDomain: 'workers.internal.nextdeploy.cloud',
    composeFilePath: 'docker-compose.yml',
    owner: 'arina.sre',
    updatedAt: 'Just now',
    cpuPct: 38.4,
    memMB: 1120,
    diskMB: 2150,
    reqPerMin: 310,
    services: [
      {
        id: 'c-661b1',
        name: 'temporal-worker-queue-worker-1',
        service: 'worker',
        image: 'registry.gitlab.com/nextdeploy-internal/temporal-worker:19d04e2',
        state: 'running',
        uptime: '2m 14s',
        cpuPct: 38.4,
        memUsedMB: 1120,
        memLimitMB: 2048,
        ports: '9090/tcp (metrics)',
      },
    ],
    domains: [],
    files: [
      {
        path: 'docker-compose.yml',
        name: 'docker-compose.yml',
        type: 'file',
        sizeBytes: 420,
        updatedAt: 'Just now',
        language: 'yaml',
        content: `services:
  worker:
    build: .
    restart: unless-stopped
    env_file: .env`,
      },
    ],
    envVars: [
      { id: 'e41', key: 'TEMPORAL_HOST', value: 'temporal.nextdeploy.cloud:7233', isSecret: false },
      { id: 'e42', key: 'WORKER_CONCURRENCY', value: '8', isSecret: false },
    ],
    deployments: [],
    logs: [
      { id: 'l41', timestamp: '2026-10-19T18:24:49.102Z', container: 'worker', level: 'info', message: 'Temporal worker polling task_queue=media-transcode-v2 (concurrency=8)' },
    ],
    volumes: [],
    collaborators: [
      { userId: 'u2', username: 'arina.sre', email: 'arina@nextdeploy.cloud', role: 'owner', addedAt: '2026-09-20' },
    ],
  },
];

export const INITIAL_IMAGES: DockerImageItem[] = [
  { id: 'sha256:4d91a8b20019', repository: 'ghcr.io/masudranaxpert/commerce-api', tag: '8c4f19a', sizeMB: 48.2, created: '4m ago', inUseBy: ['commerce-core-api-web-1'] },
  { id: 'sha256:9f02e1a44120', repository: 'postgres', tag: '16.4-alpine', sizeMB: 244.6, created: '19d ago', inUseBy: ['commerce-core-api-postgres-1'] },
  { id: 'sha256:7b18c0e33104', repository: 'redis', tag: '7.4-alpine', sizeMB: 41.8, created: '19d ago', inUseBy: ['commerce-core-api-redis-1'] },
  { id: 'sha256:1a89f3c77821', repository: 'ghcr.io/masudranaxpert/studio-web', tag: 'f42a90c', sizeMB: 186.4, created: '38m ago', inUseBy: ['studio-web-dashboard-app-1'] },
  { id: 'sha256:6c44d9e88102', repository: 'clickhouse/clickhouse-server', tag: '24.3-alpine', sizeMB: 712.0, created: '24d ago', inUseBy: ['plausible-analytics-clickhouse-1'] },
  { id: 'sha256:0e77b2d19044', repository: 'ghcr.io/masudranaxpert/commerce-api', tag: '3b10e4c', sizeMB: 48.0, created: '5h ago', inUseBy: [] },
  { id: 'sha256:8d12a4f00912', repository: 'ghcr.io/masudranaxpert/studio-web', tag: '91b02e1', sizeMB: 184.9, created: '4d ago', inUseBy: [] },
];

export const INITIAL_VOLUMES: DockerVolumeItem[] = [
  {
    name: 'commerce-core-api_pg_data',
    appSlug: 'commerce-core-api',
    mountPoint: '/var/lib/docker/volumes/commerce-core-api_pg_data/_data',
    driver: 'local',
    sizeMB: 3840,
    createdAt: '2026-08-01',
    filesCount: 1428,
    sampleFiles: [
      { path: 'PG_VERSION', size: '3 B', modified: '2026-08-01 09:14' },
      { path: 'postgresql.conf', size: '29.4 KB', modified: '2026-09-12 14:20' },
      { path: 'pg_hba.conf', size: '5.1 KB', modified: '2026-08-01 09:14' },
      { path: 'base/16384/2619', size: '1.84 GB', modified: '2026-10-19 18:24' },
      { path: 'pg_wal/000000010000000A0000004F', size: '16.0 MB', modified: '2026-10-19 18:23' },
    ],
  },
  {
    name: 'commerce-core-api_redis_data',
    appSlug: 'commerce-core-api',
    mountPoint: '/var/lib/docker/volumes/commerce-core-api_redis_data/_data',
    driver: 'local',
    sizeMB: 280,
    createdAt: '2026-08-01',
    filesCount: 14,
    sampleFiles: [
      { path: 'appendonlydir/appendonly.aof.1.base.rdb', size: '214.2 MB', modified: '2026-10-19 18:00' },
      { path: 'appendonlydir/appendonly.aof.1.incr.aof', size: '65.8 MB', modified: '2026-10-19 18:24' },
      { path: 'dump.rdb', size: '198.4 MB', modified: '2026-10-19 17:45' },
    ],
  },
  {
    name: 'plausible-analytics_event_data',
    appSlug: 'plausible-analytics',
    mountPoint: '/var/lib/docker/volumes/plausible-analytics_event_data/_data',
    driver: 'local',
    sizeMB: 8420,
    createdAt: '2026-08-19',
    filesCount: 4892,
    sampleFiles: [
      { path: 'store/91a/91a4f8b2-0012/202610_1_44_2/data.bin', size: '4.12 GB', modified: '2026-10-19 18:20' },
      { path: 'metadata/plausible_events_db.sql', size: '4.8 KB', modified: '2026-08-19 11:02' },
    ],
  },
];

export const INITIAL_GIT_PROVIDERS: GitProviderConnection[] = [
  {
    id: 'git-1',
    provider: 'GitHub',
    authMethod: 'GitHub App',
    accountName: 'masudranaxpert',
    instanceUrl: 'https://github.com',
    status: 'connected',
    reposAccessible: 28,
    webhookDeliveryRate: '99.98%',
    webhookUrl: 'https://panel.nextdeploy.sh/api/v1/webhooks/github',
    lastSynced: '2m ago',
  },
  {
    id: 'git-2',
    provider: 'GitLab',
    authMethod: 'OAuth 2.0',
    accountName: 'nextdeploy-internal',
    instanceUrl: 'https://gitlab.com',
    status: 'connected',
    reposAccessible: 14,
    webhookDeliveryRate: '100%',
    webhookUrl: 'https://panel.nextdeploy.sh/api/v1/webhooks/gitlab',
    lastSynced: '18m ago',
  },
  {
    id: 'git-3',
    provider: 'GitLab',
    authMethod: 'Deploy Token',
    accountName: 'git.corp.nextdeploy.io',
    instanceUrl: 'https://git.corp.nextdeploy.io',
    status: 'connected',
    reposAccessible: 9,
    webhookDeliveryRate: '100%',
    webhookUrl: 'https://panel.nextdeploy.sh/api/v1/webhooks/gitlab-self',
    lastSynced: '1h ago',
  },
];

export const INITIAL_SYNCED_REPOS: SyncedGitRepository[] = [
  {
    id: 'repo-1',
    provider: 'GitHub',
    fullName: 'masudranaxpert/commerce-core-api',
    defaultBranch: 'main',
    private: true,
    linkedAppSlug: 'commerce-core-api',
    lastCommitSha: '8c4f19a',
    lastPushAt: '4m ago',
    autoDeploy: true,
  },
  {
    id: 'repo-2',
    provider: 'GitHub',
    fullName: 'masudranaxpert/studio-web-dashboard',
    defaultBranch: 'production',
    private: true,
    linkedAppSlug: 'studio-web-dashboard',
    lastCommitSha: 'f42a90c',
    lastPushAt: '38m ago',
    autoDeploy: true,
  },
  {
    id: 'repo-3',
    provider: 'GitLab',
    fullName: 'nextdeploy-internal/temporal-worker-queue',
    defaultBranch: 'main',
    private: true,
    linkedAppSlug: 'temporal-worker-queue',
    lastCommitSha: '19d04e2',
    lastPushAt: '12m ago',
    autoDeploy: true,
  },
  {
    id: 'repo-4',
    provider: 'GitLab',
    fullName: 'nextdeploy-internal/billing-ledger-service',
    defaultBranch: 'main',
    private: true,
    lastCommitSha: '74b109c',
    lastPushAt: '3h ago',
    autoDeploy: false,
  },
  {
    id: 'repo-5',
    provider: 'GitHub',
    fullName: 'masudranaxpert/edge-auth-proxy',
    defaultBranch: 'main',
    private: false,
    lastCommitSha: 'e019c4b',
    lastPushAt: '1d ago',
    autoDeploy: false,
  },
];

export const INITIAL_REGISTRIES: RegistryCredential[] = [
  {
    id: 'reg-1',
    provider: 'GHCR',
    name: 'GitHub Container Registry',
    registryUrl: 'ghcr.io',
    username: 'masudranaxpert',
    tokenPreview: 'ghp_91a8••••',
    scope: 'global',
    connectedApps: 3,
    imagesCount: 4,
    status: 'connected',
    lastVerified: '10m ago',
  },
  {
    id: 'reg-2',
    provider: 'DockerHub',
    name: 'Docker Hub Production',
    registryUrl: 'index.docker.io/v1/',
    username: 'nextdeployops',
    tokenPreview: 'dckr_pat_4f2••••',
    scope: 'global',
    connectedApps: 2,
    imagesCount: 3,
    status: 'connected',
    lastVerified: '2h ago',
  },
  {
    id: 'reg-3',
    provider: 'GitLab',
    name: 'GitLab Container Registry',
    registryUrl: 'registry.gitlab.com',
    username: 'gitlab+deploy-token-88',
    tokenPreview: 'gldt-89c1••••',
    scope: 'global',
    connectedApps: 1,
    imagesCount: 2,
    status: 'connected',
    lastVerified: '18m ago',
  },
  {
    id: 'reg-4',
    provider: 'AWS ECR',
    name: 'AWS ECR (eu-central-1)',
    registryUrl: '849201948201.dkr.ecr.eu-central-1.amazonaws.com',
    username: 'AWS',
    tokenPreview: 'AKIAIOSF••••',
    scope: 'project',
    connectedApps: 1,
    imagesCount: 1,
    status: 'connected',
    lastVerified: '6h ago',
  },
];

export const INITIAL_BACKUP_DESTINATIONS: BackupDestination[] = [
  {
    id: 'bdest-1',
    name: 'Cloudflare R2 Primary (EU-Central)',
    provider: 'Cloudflare R2',
    bucket: 's3://nextdeploy-prod-backups',
    endpoint: 'https://8819a2f0c1.r2.cloudflarestorage.com',
    region: 'auto',
    status: 'connected',
    scheduleCron: '0 */6 * * *',
    retentionDays: 30,
    usedGB: 42.8,
    quotaGB: 250,
    lastBackupAt: '2h ago',
  },
  {
    id: 'bdest-2',
    name: 'AWS S3 Offsite Vault (ap-southeast-1)',
    provider: 'AWS S3',
    bucket: 's3://nd-disaster-recovery-sg',
    endpoint: 'https://s3.ap-southeast-1.amazonaws.com',
    region: 'ap-southeast-1',
    status: 'connected',
    scheduleCron: '0 2 * * *',
    retentionDays: 90,
    usedGB: 118.4,
    quotaGB: 500,
    lastBackupAt: '14h ago',
  },
];

export const INITIAL_BACKUP_ARCHIVES: BackupArchiveRecord[] = [
  {
    id: 'bk-9941',
    appSlug: 'commerce-core-api',
    destinationName: 'Cloudflare R2 Primary (EU-Central)',
    archiveName: 'commerce-core-api_2026-10-19_1800.nd-backup.tar.gz',
    sizeMB: 1420.5,
    createdAt: '2h ago',
    status: 'completed',
    includesVolumes: true,
    includesEnv: true,
  },
  {
    id: 'bk-9940',
    appSlug: 'plausible-analytics',
    destinationName: 'Cloudflare R2 Primary (EU-Central)',
    archiveName: 'plausible-analytics_2026-10-19_1200.nd-backup.tar.gz',
    sizeMB: 3180.2,
    createdAt: '6h ago',
    status: 'completed',
    includesVolumes: true,
    includesEnv: true,
  },
  {
    id: 'bk-9939',
    appSlug: 'studio-web-dashboard',
    destinationName: 'AWS S3 Offsite Vault (ap-southeast-1)',
    archiveName: 'studio-web-dashboard_2026-10-19_0200.nd-backup.tar.gz',
    sizeMB: 215.8,
    createdAt: '16h ago',
    status: 'completed',
    includesVolumes: false,
    includesEnv: true,
  },
];

export const INITIAL_MIGRATION_BUNDLES: MigrationExportBundle[] = [
  {
    id: 'mig-204',
    createdAt: 'Yesterday at 21:14 UTC',
    selectedApps: ['commerce-core-api', 'studio-web-dashboard'],
    estimatedSizeMB: 4650,
    actualSizeMB: 1940,
    status: 'ready',
    token: 'ndm_live_8f91a2b3c4d5e6f709182736',
    expiresIn: '2h 41m',
    progressLog: [
      '[21:14:02] Starting NextDeploy VPS-to-VPS export #mig-204',
      '[21:14:03] Quiescing stack commerce-core-api...',
      '[21:14:08] Archiving volume commerce-core-api_pg_data (zstd -> 1.38 GB)...',
      '[21:14:38] Bundle ready: /data/migrations/mig-204.nd-migrate (1.94 GB)',
    ],
  },
];

export const MCP_TOOLS_CATALOG: McpToolItem[] = [
  { name: 'apps_list', category: 'Deploy & Compose', description: 'List all Compose stacks, status, domains, and resource usage.', signature: 'apps_list()', readOnly: true },
  { name: 'app_inspect', category: 'Deploy & Compose', description: 'Get full stack details including services, domains, and Git commit.', signature: 'app_inspect(app_id: string)', readOnly: true },
  { name: 'deploy', category: 'Deploy & Compose', description: 'Trigger a zero-downtime docker compose build & rollout for an app.', signature: 'deploy(app_id: string, no_cache?: boolean)', readOnly: false },
  { name: 'compose_action', category: 'Deploy & Compose', description: 'Run start, stop, restart, pull, or down on an application stack.', signature: 'compose_action(app_id: string, action: "up"|"stop"|"restart"|"pull")', readOnly: false },
  { name: 'file_tree', category: 'Workspace & Files', description: 'List files and directories inside an app workspace.', signature: 'file_tree(app_id: string, path?: string)', readOnly: true },
  { name: 'file_read', category: 'Workspace & Files', description: 'Read contents of docker-compose.yml, Dockerfile, or source files.', signature: 'file_read(app_id: string, path: string)', readOnly: true },
  { name: 'file_write', category: 'Workspace & Files', description: 'Write or patch workspace files directly from Cursor / Claude Code.', signature: 'file_write(app_id: string, path: string, content: string)', readOnly: false },
  { name: 'file_delete', category: 'Workspace & Files', description: 'Remove an unused file or directory from the app workspace.', signature: 'file_delete(app_id: string, path: string)', readOnly: false },
  { name: 'container_logs', category: 'Logs & Telemetry', description: 'Fetch or tail structured container logs with level & line filters.', signature: 'container_logs(app_id: string, service?: string, tail?: number)', readOnly: true },
  { name: 'monitor_snapshot', category: 'Logs & Telemetry', description: 'Retrieve host CPU, RAM, Disk, and per-container resource metrics.', signature: 'monitor_snapshot()', readOnly: true },
  { name: 'env_list', category: 'Domains & Env', description: 'List environment variable keys and non-secret values for an app.', signature: 'env_list(app_id: string)', readOnly: true },
  { name: 'env_set', category: 'Domains & Env', description: 'Upsert one or more .env variables and optionally restart services.', signature: 'env_set(app_id: string, vars: Record<string, string>)', readOnly: false },
  { name: 'domain_add', category: 'Domains & Env', description: 'Bind a domain to a container port and provision Caddy HTTPS automatically.', signature: 'domain_add(app_id: string, domain: string, service: string, port: number)', readOnly: false },
  { name: 'domain_remove', category: 'Domains & Env', description: 'Detach a custom domain and regenerate Caddy reverse proxy labels.', signature: 'domain_remove(app_id: string, domain: string)', readOnly: false },
  { name: 'git_sync', category: 'Workspace & Files', description: 'Pull latest commit from configured Git remote and diff workspace.', signature: 'git_sync(app_id: string, branch?: string)', readOnly: false },
  { name: 'volume_backup', category: 'Deploy & Compose', description: 'Create a compressed .tar.gz volume snapshot or push to rclone S3.', signature: 'volume_backup(app_id: string, volume_name: string)', readOnly: false },
];

export const INITIAL_CLI_SESSIONS: CliSessionItem[] = [
  {
    id: 'cli-1',
    tokenName: 'Masud MacBook Pro M4 Max',
    prefix: 'nd_cli_94a8...',
    deviceHost: 'masud-mbp.local',
    osArch: 'darwin/arm64',
    cliVersion: 'nd v1.8.4',
    lastCommand: 'nd push --app commerce-core-api --deploy',
    lastActiveAt: '4m ago',
    ipAddress: '103.148.22.91',
  },
  {
    id: 'cli-2',
    tokenName: 'GitHub Actions Release Runner',
    prefix: 'nd_cli_31f0...',
    deviceHost: 'gha-runner-ubuntu-24',
    osArch: 'linux/amd64',
    cliVersion: 'nd v1.8.4',
    lastCommand: 'nd diff --app studio-web-dashboard',
    lastActiveAt: '38m ago',
    ipAddress: '140.82.115.44',
  },
  {
    id: 'cli-3',
    tokenName: 'Arina ThinkPad X1 Carbon (Arch)',
    prefix: 'nd_cli_77c2...',
    deviceHost: 'arina-arch-workstation',
    osArch: 'linux/amd64',
    cliVersion: 'nd v1.8.3',
    lastCommand: 'nd logs temporal-worker-queue --follow',
    lastActiveAt: '2h ago',
    ipAddress: '185.199.108.15',
  },
  {
    id: 'cli-4',
    tokenName: 'GitLab CI Runner (eu-central)',
    prefix: 'nd_cli_59d1...',
    deviceHost: 'gitlab-runner-k8s-04',
    osArch: 'linux/amd64',
    cliVersion: 'nd v1.8.4',
    lastCommand: 'nd deploy --app temporal-worker-queue',
    lastActiveAt: '12m ago',
    ipAddress: '35.246.190.12',
  },
];

export const INITIAL_USERS: PanelUser[] = [
  {
    id: 'u1',
    username: 'masud',
    email: 'masud132213@gmail.com',
    role: 'admin',
    appsCount: 3,
    maxApps: 50,
    cpuQuotaCores: 16,
    memUsedGB: 2.8,
    memQuotaGB: 32,
    diskUsedGB: 14.3,
    diskQuotaGB: 500,
    twoFactorEnabled: true,
    lastLoginAt: 'Active now',
  },
  {
    id: 'u2',
    username: 'arina.sre',
    email: 'arina@nextdeploy.cloud',
    role: 'admin',
    appsCount: 1,
    maxApps: 25,
    cpuQuotaCores: 8,
    memUsedGB: 1.1,
    memQuotaGB: 16,
    diskUsedGB: 2.1,
    diskQuotaGB: 200,
    twoFactorEnabled: true,
    lastLoginAt: '18m ago',
  },
  {
    id: 'u3',
    username: 'dev.tanvir',
    email: 'tanvir@nextdeploy.cloud',
    role: 'user',
    appsCount: 2,
    maxApps: 5,
    cpuQuotaCores: 4,
    memUsedGB: 1.4,
    memQuotaGB: 8,
    diskUsedGB: 6.4,
    diskQuotaGB: 50,
    twoFactorEnabled: false,
    lastLoginAt: '1d ago',
  },
];

export const INITIAL_AUDIT_LOGS: AuditLogEntry[] = [
  {
    id: 'aud-1',
    timestamp: '2026-10-19 18:20:42',
    actor: 'masud',
    actorRole: 'admin',
    action: 'compose.deploy',
    target: 'commerce-core-api (8c4f19a)',
    ip: '103.148.22.91',
    status: 'success',
  },
  {
    id: 'aud-2',
    timestamp: '2026-10-19 18:11:09',
    actor: 'claude-code-mcp',
    actorRole: 'mcp-agent',
    action: 'mcp.file_write',
    target: 'commerce-core-api/docker-compose.yml',
    ip: '127.0.0.1 (SSE)',
    status: 'success',
  },
  {
    id: 'aud-3',
    timestamp: '2026-10-19 17:46:19',
    actor: 'masud',
    actorRole: 'nd-cli',
    action: 'cli.push_archive',
    target: 'studio-web-dashboard (14.2 MB)',
    ip: '103.148.22.91',
    status: 'success',
  },
  {
    id: 'aud-4',
    timestamp: '2026-10-19 16:00:01',
    actor: 'system.cron',
    actorRole: 'admin',
    action: 'backup.rclone_sync',
    target: 'commerce-core-api -> Cloudflare R2',
    ip: 'internal',
    status: 'success',
  },
  {
    id: 'aud-5',
    timestamp: '2026-10-19 14:12:55',
    actor: 'unknown',
    actorRole: 'user',
    action: 'auth.login_attempt',
    target: 'admin account (invalid OTP)',
    ip: '45.155.205.233',
    status: 'blocked',
  },
];
