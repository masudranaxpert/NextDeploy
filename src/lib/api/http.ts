import { toast } from 'sonner';

// Core HTTP helpers and shared payload types for NextDeploy Go Backend API Client

export type ApiResult<T = Record<string, unknown>> = {
  ok: boolean;
  status?: number;
  data?: T;
  error?: string;
  message?: string;
  offlineFallback?: boolean;
} & (T extends Record<string, unknown> ? Partial<T> : Record<string, unknown>);

const DEFAULT_HEADERS: Record<string, string> = {
  Accept: 'application/json',
  'Content-Type': 'application/json',
  'X-Requested-With': 'XMLHttpRequest',
  'X-NextDeploy-Client': 'spa',
};

/** Turns a fetch Response into the ApiResult shape used everywhere in the client. */
export async function resultFromResponse<T = Record<string, unknown>>(res: Response): Promise<ApiResult<T>> {
  if (res.status === 401 && typeof window !== 'undefined') {
    // the session ended (or never existed): the app shows the sign-in screen instead of failing quietly
    window.dispatchEvent(new Event('nd:unauthorized'));
  }
  const contentType = res.headers.get('content-type') || '';
  if (!contentType.includes('application/json')) {
    if (!res.ok) {
      const errText = await res.text().catch(() => '');
      return {
        ok: false,
        status: res.status,
        error: errText.trim().slice(0, 300) || `HTTP ${res.status}`,
      } as ApiResult<T>;
    }
    return { ok: true, status: res.status } as ApiResult<T>;
  }
  const json = (await res.json()) as T & { ok?: boolean; error?: string; message?: string };
  const spread =
    typeof json === 'object' && json !== null && !Array.isArray(json)
      ? (json as Record<string, unknown>)
      : {};
  if (!res.ok || json.ok === false) {
    return {
      ...spread,
      ok: false,
      status: res.status,
      data: json,
      error: json.error || json.message || `HTTP ${res.status}`,
    } as ApiResult<T>;
  }
  return {
    ...spread,
    ok: true,
    status: res.status,
    data: json,
  } as ApiResult<T>;
}

// A change that takes more than a moment shows a "working" toast until it finishes, so the user
// is never left wondering whether the click did anything. The caller still shows the result.
function progressLabel(path: string): string {
  const p = path.toLowerCase();
  const table: Array<[RegExp, string]> = [
    [/upload|archive/, 'Uploading…'],
    [/import/, 'Importing…'],
    [/export/, 'Exporting…'],
    [/restore/, 'Restoring…'],
    [/backup/, 'Backing up…'],
    [/deploy|rollback/, 'Deploying…'],
    [/install|pull|build/, 'Installing…'],
    [/sync|refresh/, 'Syncing…'],
    [/delete|remove|prune/, 'Removing…'],
    [/restart|start|stop|apply|action|container/, 'Applying…'],
    [/save|update|settings|config|create|add|password|limits|security|domains|env/, 'Saving…'],
  ];
  for (const [re, label] of table) if (re.test(p)) return label;
  return 'Working…';
}

const QUIET_PATHS = /^\/(login|logout|setup)\b|\/status|\/heartbeat|\/sessions\b/;

export async function requestJson<T = Record<string, unknown>>(
  path: string,
  options: RequestInit = {}
): Promise<ApiResult<T>> {
  const method = (options.method || 'GET').toUpperCase();
  let timer: number | undefined;
  let toastId: string | number | undefined;
  if (method !== 'GET' && typeof window !== 'undefined' && !QUIET_PATHS.test(path)) {
    timer = window.setTimeout(() => {
      toastId = toast.loading(progressLabel(path));
    }, 700);
  }
  try {
    const headers: Record<string, string> = {
      ...DEFAULT_HEADERS,
      ...(options.headers as Record<string, string> | undefined),
    };
    if (typeof FormData !== 'undefined' && options.body instanceof FormData) {
      delete headers['Content-Type'];
    }
    if (!options.body && method === 'GET') {
      delete headers['Content-Type'];
    }
    const res = await fetch(path, {
      credentials: 'same-origin',
      cache: 'no-store',
      ...options,
      headers,
    });
    return await resultFromResponse<T>(res);
  } catch {
    return { ok: false, error: 'Cannot reach the server' } as ApiResult<T>;
  } finally {
    if (timer !== undefined) window.clearTimeout(timer);
    if (toastId !== undefined) toast.dismiss(toastId);
  }
}

export async function requestText(path: string): Promise<ApiResult<string>> {
  try {
    const res = await fetch(path, {
      credentials: 'same-origin',
      headers: {
        'X-Requested-With': 'XMLHttpRequest',
        'X-NextDeploy-Client': 'spa',
      },
    });
    const text = await res.text();
    if (!res.ok) {
      return { ok: false, status: res.status, error: text || `HTTP ${res.status}` };
    }
    return { ok: true, status: res.status, data: text };
  } catch {
    return { ok: false, error: 'Cannot reach the server' };
  }
}

export function toFormBody(
  params: Record<string, string | number | boolean | undefined>
): URLSearchParams {
  const body = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined) {
      body.set(k, String(v));
    }
  }
  return body;
}

export interface DomainFormPayload {
  domain: string;
  service: string;
  port: number;
  is_primary?: boolean;
  enable_https?: boolean;
  enable_www?: boolean;
  serve_static?: boolean;
  static_path?: string;
  static_url_prefix?: string;
  serve_media?: boolean;
  media_path?: string;
  media_url_prefix?: string;
  shield_node_id?: number | null;
}

export interface BootstrapData {
  [key: string]: any;
  ok?: boolean;
  needs_setup?: boolean;
  user?: {
    id: number;
    username: string;
    role: string;
    status: string;
    max_apps: number;
    max_memory_mb: number;
    max_cpus: number;
    max_storage_mb: number;
    allow_domain_file_server: boolean;
    storage_used_bytes?: number;
    storage_used_human?: string;
  };
  sys?: {
    [key: string]: any;
    os?: string;
    arch?: string;
    hostname?: string;
    docker_version?: string;
    cpus?: number;
    memory_total_bytes?: number;
    memory_used_bytes?: number;
    disk_total_bytes?: number;
    disk_used_bytes?: number;
    load_1?: number;
    uptime_seconds?: number;
  };
  apps?: Array<{
    id: string;
    name: string;
    source_type?: string;
    git_repo?: string;
    git_branch?: string;
    status?: string;
    active_slot?: string;
    standby_slot?: string;
    created_at?: string;
    updated_at?: string;
    domains?: Array<{
      id: number;
      domain: string;
      service: string;
      port: number;
      enable_https: boolean;
      enable_www: boolean;
    }>;
    services?: string[];
    git_config?: {
      repo_url?: string;
      branch?: string;
      auth_mode?: string;
      git_provider_id?: number;
      webhook_secret?: string;
      auto_deploy?: boolean;
      git_sync?: boolean;
      last_commit_sha?: string;
      last_commit_msg?: string;
    };
    deploy_logs?: Array<{
      id: number;
      action: string;
      ok: boolean;
      output: string;
      slot?: string;
      commit_sha?: string;
      image_digest?: string;
      env_snapshot?: string;
      created_at: string;
    }>;
    collaborators?: Array<{
      app_id: string;
      user_id: number;
      role: string;
      created_at: string;
    }>;
    volumes?: string[];
    backup_history?: Array<{
      id: number;
      archive_name: string;
      backup_type: string;
      size_bytes: number;
      status: string;
      created_at: string;
    }>;
    backup_schedules?: Array<{
      id: number;
      destination_id: number;
      cron_expression: string;
      retention_count: number;
      backup_type: string;
      enabled: boolean;
    }>;
    env_content?: string;
  }>;
  containers?: Array<{
    id: string;
    names: string[];
    image: string;
    state: string;
    status: string;
    ports: Array<{ ip?: string; private_port: number; public_port?: number; type?: string }>;
    created: number;
  }>;
  images?: Array<{
    id: string;
    repo_tags: string[];
    size: number;
    created: number;
  }>;
  volumes?: string[];
  git_providers?: Array<{
    id: number;
    name: string;
    provider: string;
    token: string;
    notes?: string;
  }>;
  registries?: Array<{
    id: number;
    name: string;
    url: string;
    username: string;
  }>;
  backup_destinations?: Array<{
    id: number;
    name: string;
    provider: string;
    bucket?: string;
  }>;
  cli_sessions?: Array<{
    id: string;
    name?: string;
    token_prefix?: string;
    token?: string;
    hostname?: string;
    os?: string;
    arch?: string;
    last_used_at?: string;
    created_at?: string;
    ip?: string;
  }>;
  users?: Array<{
    id: number;
    username: string;
    role: string;
    status: string;
    max_apps: number;
    max_memory_mb: number;
    max_cpus: number;
    max_storage_mb: number;
    allow_domain_file_server: boolean;
    created_at: string;
  }>;
  settings?: Record<string, string>;
  caddy?: {
    up: boolean;
    status: string;
    config?: Record<string, string>;
  };
  mcp_enabled?: boolean;
  public_ip?: string;
  origin_ip?: string;
  shield_nodes?: Array<any>;
  server_time?: string;
}

let cachedServerPublicIp = '';

export function setServerPublicIp(ip?: string): void {
  if (ip && /^\d{1,3}(\.\d{1,3}){3}$/.test(ip.trim())) {
    cachedServerPublicIp = ip.trim();
  }
}

export function getServerPublicIp(): string {
  if (cachedServerPublicIp) {
    return cachedServerPublicIp;
  }
  if (typeof window !== 'undefined') {
    const host = window.location.hostname;
    if (/^\d{1,3}(\.\d{1,3}){3}$/.test(host)) {
      return host;
    }
  }
  return '127.0.0.1';
}

export function formatWildcardDomain(
  slug: string,
  provider: 'sslip.io' | 'nip.io' = 'sslip.io',
  ipOverride?: string
): string {
  let cleanSlug =
    slug
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9-]+/g, '-')
      .replace(/^-|-$/g, '') || 'app';
  if (/[0-9]$/.test(cleanSlug)) {
    cleanSlug = `${cleanSlug}-app`;
  }
  const dashedIp = (ipOverride || getServerPublicIp()).replace(/\./g, '-');
  return `${cleanSlug}-${dashedIp}.${provider}`;
}

export function formatSlotPreviewDomain(
  domain: string,
  slot: string = 'green'
): string {
  const cleanDom = domain.trim().toLowerCase().replace(/^https?:\/\//, '');
  const cleanSlot = slot.trim().toLowerCase() || 'green';
  if (!cleanDom) return '';
  if (cleanDom.startsWith(`${cleanSlot}-`) || cleanDom.startsWith(`${cleanSlot}.`)) {
    return cleanDom;
  }
  if (cleanDom.endsWith('.sslip.io') || cleanDom.endsWith('.nip.io')) {
    return `${cleanSlot}-${cleanDom}`;
  }
  return `${cleanSlot}.${cleanDom}`;
}
