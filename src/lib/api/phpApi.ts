import { requestJson } from './http';
import { notifyMutation } from '../dataSync';

const json = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

const site = (appId: string, rest = '') => `/apps/${encodeURIComponent(appId)}/php${rest}`;

export interface PhpBuildState {
  version: string;
  running: boolean;
  error?: string;
  log: string;
}

export interface PhpVersionRow {
  version: string;
  installed: boolean;
  sites: number;
  build?: PhpBuildState;
  status?: string;
  status_label?: string;
  has_cve?: boolean;
  cvss?: string;
  cves?: string[];
  cve_warning?: string;
  eol_date?: string;
  patched_in?: string;
  is_default?: boolean;
}

export interface PhpContainerRow {
  name: string;
  image: string;
  state: string;
  status: string;
  kind: 'mysql' | 'php' | 'route' | 'pma';
  version?: string;
  owner_id?: string;
}

export interface PhpSecurity {
  blocked_ips: string[];
  allow_only: string[];
  basic_auth_user: string;
  has_password: boolean;
  block_xmlrpc: boolean;
  block_upload_php: boolean;
  block_scanners: boolean;
  hsts: boolean;
  frame_protection: boolean;
}

export interface PhpInstanceData {
  ok: boolean;
  enabled: boolean;
  mysql: {
    enabled: boolean;
    exists: boolean;
    running: boolean;
    ready: boolean;
    image: string;
    memory_mb: number;
    host_memory_mb: number;
    buffer_pool_mb: number;
    max_connections: number;
    query_seconds: number;
    idle_seconds: number;
    query_limit_supported: boolean;
  };
  mysql_task: { running: boolean; error?: string };
  pma: { enabled: boolean; exists: boolean; running: boolean; image: string };
  pma_task: { running: boolean; error?: string };
  redis?: {
    enabled: boolean;
    exists: boolean;
    running: boolean;
    image: string;
    host: string;
    port: number;
    version?: string;
    used_memory_human?: string;
    connected_clients?: number;
    uptime_seconds?: number;
  };
  versions: PhpVersionRow[];
  containers: PhpContainerRow[];
  min_cron_interval_minutes: number;
  user_limits: Array<{ user_id: number; username: string; role: string; max_databases: number; max_cron_jobs: number }>;
}

export interface PhpRoutingRule {
  id: string;
  kind: 'rewrite' | 'redir' | 'block';
  match_type: 'exact' | 'prefix' | 'regex';
  pattern: string;
  target?: string;
  status?: number;
  enabled: boolean;
  unless_exists?: boolean;
  require_target_file?: boolean;
  preserve_query?: boolean;
  note?: string;
}

export interface PhpSiteRouting {
  fallback_mode?: '' | 'default' | 'strict_404' | 'custom';
  fallback_entry?: string;
  rules?: PhpRoutingRule[];
}

export interface HtaccessDiagnostic {
  file: string;
  line: number;
  severity: 'converted' | 'info' | 'warning' | 'unknown';
  directive: string;
  message: string;
}

export interface HtaccessConvertResult {
  rules: PhpRoutingRule[];
  fallback_mode?: '' | 'default' | 'strict_404' | 'custom';
  fallback_entry?: string;
  diagnostics: HtaccessDiagnostic[];
  files_scanned?: number;
  files?: string[];
  summary?: {
    converted: number;
    skipped: number;
    warnings: number;
  };
}

export interface PhpSettings {
  memory_limit: string;
  max_execution_time: number;
  max_input_time: number;
  max_input_vars: number;
  post_max_size: string;
  upload_max_filesize: string;
  max_children: number;
  process_manager?: 'dynamic' | 'ondemand';
  allow_shell_functions?: boolean;
  disable_functions?: string;
  additional: string;
  stopped?: boolean;
  routing?: PhpSiteRouting;
}

export interface PhpStatusData {
  ok: boolean;
  enabled: boolean;
  default_version: string;
  installed_versions: string[];
  mysql: { enabled: boolean; running: boolean; ready: boolean };
  limits: { max_databases: number; used_databases: number; max_cron_jobs: number; used_cron_jobs: number; unlimited: boolean };
  min_cron_interval_minutes: number;
  choices: {
    memory_limit: string[];
    sizes: string[];
    max_children: number;
    times: number[];
    max_input_vars: number[];
    supported_versions: string[];
  };
}

export interface PhpDatabaseRow {
  id: number;
  db_name: string;
  size_bytes: number;
  users: Array<{ user_id: number; user: string; access: 'all' | 'read' }>;
}

export interface PhpDbUserRow {
  id: number;
  user_name: string;
  grants: Array<{ db_id: number; access: 'all' | 'read' }>;
}

export interface PhpCronRow {
  id: number;
  schedule: string;
  command: string;
  enabled: boolean;
  last_run_at: string;
  last_status: string;
  last_output: string;
}

export interface PhpProvision {
  status: 'running' | 'done' | 'failed';
  log: string;
  error?: string;
  secrets?: Record<string, string>;
}

export interface PhpSiteDetail {
  ok: boolean;
  id: string;
  site: { site_type: 'php' | 'wordpress' | 'static'; php_version: string; root_dir: string; uid: number };
  settings: PhpSettings;
  doc_root: string;
  runtime: { container: string; state: string };
  mysql: { enabled: boolean; running: boolean; ready: boolean; host: string; port: number };
  pma: { enabled: boolean };
  databases: PhpDatabaseRow[];
  db_users: PhpDbUserRow[];
  cron_jobs: PhpCronRow[];
  provision?: PhpProvision | null;
  installed_versions: string[];
  min_cron_interval_minutes: number;
  wp_cache_plugins?: Array<{
    slug: string;
    name: string;
    installed: boolean;
    has_cache: boolean;
    cache_path: string;
  }>;
  limits: {
    max_memory_mb: number;
    max_storage_mb: number;
    max_children: number;
    max_databases: number;
    used_databases: number;
    max_cron_jobs: number;
    used_cron_jobs: number;
    unlimited: boolean;
  };
}

export const phpApi = {
  // ---- any user ----
  phpStatus: () => requestJson<PhpStatusData>('/api/php/status'),
  phpCreateSite: async (body: {
    name: string;
    type: 'php' | 'wordpress' | 'static';
    php_version?: string;
    title?: string;
    wp_admin_user?: string;
    wp_admin_email?: string;
  }) => {
    const res = await requestJson<{ ok: boolean; id: string; domain: string; provisioning?: boolean; error?: string }>(
      '/api/php/sites',
      json(body)
    );
    if (res?.ok) notifyMutation();
    return res;
  },

  // ---- admin: instance ----
  phpInstance: () => requestJson<PhpInstanceData>('/api/php/instance'),
  phpSetModule: (enabled: boolean) => requestJson('/api/php/instance/module', json({ enabled })),
  phpMysqlQueryLimit: (seconds: number) =>
    requestJson('/api/php/instance/mysql', json({ action: 'limits', query_seconds: seconds })),
  phpMysql: async (action: 'enable' | 'disable' | 'restart', memoryMb?: number) => {
    const res = await requestJson('/api/php/instance/mysql', json({ action, memory_mb: memoryMb }));
    if (res?.ok) notifyMutation();
    return res;
  },
  phpSecurity: (appId: string) => requestJson<{ ok: boolean; security: PhpSecurity; client_ip: string }>(site(appId, '/security')),
  phpSecuritySave: (appId: string, body: Partial<PhpSecurity> & { basic_auth_password?: string; remove_password?: boolean }) =>
    requestJson<{ ok: boolean; security: PhpSecurity }>(site(appId, '/security'), json(body)),
  phpPma: (action: 'enable' | 'disable' | 'restart') => requestJson('/api/php/instance/pma', json({ action })),
  phpRedis: (action: 'enable' | 'disable' | 'restart') =>
    requestJson<{ ok: boolean; redis?: PhpInstanceData['redis'] }>('/api/php/instance/redis', json({ action })),
  phpPmaOpenAll: () => requestJson<{ ok: boolean; url: string }>('/api/php/instance/pma/open', json({})),
  phpPmaOpen: (appId: string) => requestJson<{ ok: boolean; url: string }>(site(appId, "/pma/open"), json({})),
  phpVersion: async (version: string, action: 'install' | 'remove') => {
    const res = await requestJson('/api/php/instance/versions', json({ version, action }));
    if (res?.ok) notifyMutation();
    return res;
  },
  phpRestartContainer: async (name: string) => {
    const res = await requestJson('/api/php/instance/containers/restart', json({ name }));
    if (res?.ok) notifyMutation();
    return res;
  },
  phpInstanceSettings: (minCronIntervalMinutes: number) =>
    requestJson('/api/php/instance/settings', json({ min_cron_interval_minutes: minCronIntervalMinutes })),
  phpUserLimits: (userId: number, maxDatabases: number, maxCronJobs: number) =>
    requestJson('/api/php/instance/user-limits', json({ user_id: userId, max_databases: maxDatabases, max_cron_jobs: maxCronJobs })),

  // ---- per site ----
  phpSiteDetail: (appId: string) => requestJson<PhpSiteDetail>(site(appId)),
  phpSiteSettings: (appId: string, body: Partial<PhpSettings> & { php_version?: string; root_dir?: string }) =>
    requestJson(site(appId, '/settings'), json(body)),
  phpSiteRestart: (appId: string) => requestJson(site(appId, '/restart'), { method: 'POST' }),
  phpSiteStop: async (appId: string) => {
    const res = await requestJson<{ ok: boolean; status: string }>(site(appId, '/stop'), { method: 'POST' });
    if (res?.ok) notifyMutation();
    return res;
  },
  phpSiteStart: async (appId: string) => {
    const res = await requestJson<{ ok: boolean; status: string }>(site(appId, '/start'), { method: 'POST' });
    if (res?.ok) notifyMutation();
    return res;
  },
  phpRoutingGet: (appId: string) => requestJson<{ ok: boolean; routing: PhpSiteRouting; stopped: boolean }>(site(appId, '/routing')),
  phpRoutingSave: async (appId: string, routing: PhpSiteRouting) => {
    const res = await requestJson<{ ok: boolean; routing: PhpSiteRouting }>(site(appId, '/routing'), json(routing));
    if (res?.ok) notifyMutation();
    return res;
  },
  phpConvertHtaccess: (appId: string, content: string, filePath = '.htaccess') =>
    requestJson<{ ok: boolean; result: HtaccessConvertResult }>(site(appId, '/routing/convert-htaccess'), json({ content, file_path: filePath })),
  phpScanHtaccess: async (appId: string, opts?: { apply?: boolean; mode?: 'replace' | 'append' }) => {
    const res = await requestJson<{ ok: boolean; result: HtaccessConvertResult; routing?: PhpSiteRouting }>(
      site(appId, '/routing/scan-htaccess'),
      json({ apply: Boolean(opts?.apply), mode: opts?.mode || 'replace' })
    );
    if (res?.ok && opts?.apply) notifyMutation();
    return res;
  },
  phpFixPermissions: (appId: string) => requestJson(site(appId, '/fix-permissions'), { method: 'POST' }),
  phpProvision: (appId: string, consume = false) =>
    requestJson<{ ok: boolean; provision: PhpProvision | null }>(site(appId, `/provision${consume ? '?consume=1' : ''}`)),
  phpLogs: (appId: string, source: 'php' | 'runtime', lines = 200) =>
    requestJson<{ ok: boolean; text: string }>(site(appId, `/logs?source=${source}&lines=${lines}`)),

  phpDbCreate: (appId: string, body: { name: string }) =>
    requestJson<{ ok: boolean; id: number; db_name: string }>(site(appId, '/databases'), json(body)),
  phpDbDelete: (appId: string, dbId: number) =>
    requestJson(site(appId, `/databases/${dbId}/delete`), { method: 'POST' }),
  phpDbUserCreate: (appId: string, body: { name: string; password?: string; grants: Array<{ db_id: number; access: 'all' | 'read' }> }) =>
    requestJson<{ ok: boolean; id: number; user: string; password: string; host: string; port: number }>(
      site(appId, '/db-users'),
      json(body)
    ),
  phpDbUserGrants: (appId: string, userId: number, grants: Array<{ db_id: number; access: 'all' | 'read' }>) =>
    requestJson(site(appId, `/db-users/${userId}/grants`), json({ grants })),
  phpDbUserPassword: (appId: string, userId: number, password?: string) =>
    requestJson<{ ok: boolean; password: string }>(site(appId, `/db-users/${userId}/password`), json({ password })),
  phpDbUserDelete: (appId: string, userId: number) =>
    requestJson(site(appId, `/db-users/${userId}/delete`), { method: 'POST' }),
  phpDbExportUrl: (appId: string, dbId: number) => site(appId, `/databases/${dbId}/export`),
  phpDbImport: (appId: string, dbId: number, file: File) => {
    const fd = new FormData();
    fd.append('file', file);
    return requestJson(site(appId, `/databases/${dbId}/import`), { method: 'POST', body: fd });
  },

  phpCronCreate: (appId: string, body: { schedule: string; command: string; enabled?: boolean }) =>
    requestJson<{ ok: boolean; id: number }>(site(appId, '/cron'), json(body)),
  phpCronUpdate: (appId: string, jobId: number, body: { schedule?: string; command?: string; enabled?: boolean }) =>
    requestJson(site(appId, `/cron/${jobId}`), json(body)),
  phpCronDelete: (appId: string, jobId: number) =>
    requestJson(site(appId, `/cron/${jobId}/delete`), { method: 'POST' }),
  phpCronRun: (appId: string, jobId: number) =>
    requestJson<{ ok: boolean; status: string; output: string }>(site(appId, `/cron/${jobId}/run`), { method: 'POST' }),
  phpPurgeCache: (appId: string) =>
    requestJson<{ ok: boolean; message: string }>(site(appId, '/purge-cache'), { method: 'POST' }),
};
