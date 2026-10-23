// Core HTTP helpers and shared payload types for NextDeploy Go Backend API Client

export interface ApiResult<T = Record<string, unknown>> {
  ok: boolean;
  status?: number;
  data?: T;
  error?: string;
  offlineFallback?: boolean;
}

const DEFAULT_HEADERS: Record<string, string> = {
  Accept: 'application/json',
  'X-Requested-With': 'XMLHttpRequest',
  'X-NextDeploy-Client': 'spa',
};

export async function requestJson<T = Record<string, unknown>>(
  path: string,
  options: RequestInit = {}
): Promise<ApiResult<T>> {
  try {
    const headers: Record<string, string> = {
      ...DEFAULT_HEADERS,
      ...(options.headers as Record<string, string> | undefined),
    };
    if (typeof FormData !== 'undefined' && options.body instanceof FormData) {
      delete headers['Content-Type'];
    }
    const res = await fetch(path, {
      credentials: 'same-origin',
      ...options,
      headers,
    });
    const contentType = res.headers.get('content-type') || '';
    if (!contentType.includes('application/json')) {
      return { ok: res.ok, status: res.status, offlineFallback: true };
    }
    const json = (await res.json()) as T & { ok?: boolean; error?: string; message?: string };
    if (!res.ok || json.ok === false) {
      return {
        ok: false,
        status: res.status,
        data: json,
        error: json.error || json.message || `HTTP ${res.status}`,
      };
    }
    return { ok: true, status: res.status, data: json };
  } catch {
    return { ok: false, offlineFallback: true };
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
    return { ok: false, offlineFallback: true };
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
  enable_https?: boolean;
  enable_www?: boolean;
  serve_static?: boolean;
  static_path?: string;
  static_url_prefix?: string;
  serve_media?: boolean;
  media_path?: string;
  media_url_prefix?: string;
}

export interface BootstrapData {
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
  server_time?: string;
}
