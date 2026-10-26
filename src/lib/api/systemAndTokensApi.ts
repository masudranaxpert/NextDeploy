import { requestJson, toFormBody } from './http';

export const systemAndTokensApi = {
  // Caddy & NextDeploy Root Stack
  caddySaveConfig: (
    payload:
      | string
      | {
          admin_api: string;
          email: string;
          caddy_image: string;
        },
    _mode?: string
  ) => {
    if (typeof payload === 'string') {
      return requestJson('/caddy/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: toFormBody({
          admin_api: 'http://127.0.0.1:2019',
          email: 'masud132213@gmail.com',
          caddy_image: 'lucaslorentz/caddy-docker-proxy:2.9-alpine',
        }),
      });
    }
    return requestJson('/caddy/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody(payload),
    });
  },
  caddyContainerAction: (action: 'start' | 'force_start' | 'restart' | 'stop' | 'reconcile_network') =>
    requestJson('/caddy/container', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ action }),
    }),
  caddyLogs: async (tail = 300) => {
    const res = await requestJson<{ ok: boolean; lines: string[] }>(`/caddy/logs?tail=${tail}`);
    return {
      ok: res.ok,
      lines: res.data?.lines,
      logs: res.data?.lines ? res.data.lines.join('\n') : undefined,
    };
  },
  caddyOverview: () =>
    requestJson<{
      ok: boolean;
      live_config: string;
      root_compose: string;
      panel_domain: string;
      panel_enable_https: boolean;
      panel_enable_www: boolean;
      shared_mount_prefix: string;
      shared_volumes: string[];
    }>('/caddy/overview'),
  savePanelConfig: (
    payloadOrDomain:
      | string
      | {
          panel_domain: string;
          panel_enable_https: boolean;
          panel_enable_www: boolean;
        },
    enableHttps?: boolean,
    enableWww?: boolean
  ) => {
    const payload =
      typeof payloadOrDomain === 'string'
        ? {
            panel_domain: payloadOrDomain,
            panel_enable_https: enableHttps !== undefined ? enableHttps : true,
            panel_enable_www: enableWww !== undefined ? enableWww : false,
          }
        : payloadOrDomain;
    return requestJson('/nextdeploy/panel', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        panel_domain: payload.panel_domain,
        panel_enable_https: payload.panel_enable_https ? 'on' : '',
        panel_enable_www: payload.panel_enable_www ? 'on' : '',
      }),
    });
  },
  panelApplyStatus: () =>
    requestJson<{
      running?: boolean;
      done?: boolean;
      succeeded?: boolean;
      status: string;
      message?: string;
      log?: string;
      updated_at?: string;
    }>('/nextdeploy/apply-status'),
  saveSharedVolumes: (
    payloadOrVolumes:
      | string
      | string[]
      | {
          caddy_shared_mount_prefix: string;
          caddy_shared_volumes: string | string[];
        },
    mountPrefix = '/mnt/shared'
  ) => {
    const body = new URLSearchParams();
    if (Array.isArray(payloadOrVolumes)) {
      body.set('caddy_shared_mount_prefix', mountPrefix || '/mnt/shared');
      payloadOrVolumes.forEach((v) => {
        if (v.trim()) body.append('caddy_shared_volumes', v.trim());
      });
    } else if (typeof payloadOrVolumes === 'string') {
      body.set('caddy_shared_mount_prefix', mountPrefix || '/mnt/shared');
      payloadOrVolumes
        .split(/[\n,]/)
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((v) => body.append('caddy_shared_volumes', v));
    } else {
      body.set(
        'caddy_shared_mount_prefix',
        payloadOrVolumes.caddy_shared_mount_prefix || '/mnt/shared'
      );
      const vols = Array.isArray(payloadOrVolumes.caddy_shared_volumes)
        ? payloadOrVolumes.caddy_shared_volumes
        : payloadOrVolumes.caddy_shared_volumes.split(/[\n,]/);
      vols
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((v) => body.append('caddy_shared_volumes', v));
    }
    return requestJson('/nextdeploy/shared-volumes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body,
    });
  },

  // MCP & CLI Tokens
  createApiToken: (payload: {
    name: string;
    kind: 'mcp' | 'cli';
    allow_env_reveal?: boolean;
    allow_server_exec?: boolean;
    allow_container_exec?: boolean;
    allow_app_delete?: boolean;
  }) =>
    requestJson<{ ok: boolean; id: number; token: string; name: string }>(
      payload.kind === 'cli' ? '/cli-sessions/tokens' : '/mcp-docs/tokens',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: toFormBody({
          name: payload.name,
          kind: payload.kind,
          allow_env_reveal: payload.allow_env_reveal ? '1' : '0',
          allow_server_exec: payload.allow_server_exec ? '1' : '0',
          allow_container_exec: payload.allow_container_exec ? '1' : '0',
          allow_app_delete: payload.allow_app_delete ? '1' : '0',
        }),
      }
    ),
  createCliToken: (name: string) =>
    requestJson<{ ok: boolean; id: number; token: string; name: string }>('/cli-sessions/tokens', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        name,
        kind: 'cli',
        allow_env_reveal: '1',
        allow_container_exec: '1',
        allow_server_exec: '0',
        allow_app_delete: '0',
      }),
    }),
  deleteCliSession: (tokenId: string | number) =>
    requestJson(`/cli-sessions/tokens/${tokenId}/delete`, {
      method: 'POST',
    }),
  deleteMcpToken: (tokenId: string | number) =>
    requestJson(`/mcp-docs/tokens/${tokenId}/delete`, { method: 'POST' }),
  toggleMcpStatus: () =>
    requestJson<{ ok: boolean; enabled: boolean }>('/mcp-docs/toggle-status', {
      method: 'POST',
    }),
  toggleApiTokenPerm: (
    tokenId: string | number,
    perm:
      | 'reveal'
      | 'container-exec'
      | 'exec'
      | 'app-delete'
      | 'allow_env_reveal'
      | 'allow_container_exec'
      | 'allow_server_exec'
      | 'allow_app_delete',
    kind: 'mcp' | 'cli' = 'cli'
  ) => {
    const permMap: Record<string, string> = {
      allow_env_reveal: 'reveal',
      allow_container_exec: 'container-exec',
      allow_server_exec: 'exec',
      allow_app_delete: 'app-delete',
    };
    const routeSuffix = permMap[perm] || perm;
    return requestJson(
      `${kind === 'cli' ? '/cli-sessions' : '/mcp-docs'}/tokens/${tokenId}/toggle-${routeSuffix}`,
      { method: 'POST' }
    );
  },

  // Users, Quotas, Audit Logs & System Settings
  createUser: (payload: { username: string; password: string; role: 'admin' | 'user' }) =>
    requestJson<{ ok: boolean; id: number; username: string; role: 'admin' | 'user' }>('/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody(payload),
    }),
  deleteUser: (userId: string | number) =>
    requestJson(`/users/${userId}/delete`, {
      method: 'POST',
    }),
  changeUserPassword: (userId: string | number, password: string, confirm: string) =>
    requestJson(`/users/${userId}/password`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ password, confirm }),
    }),
  changeUserRole: (userId: string | number, role: 'admin' | 'user') =>
    requestJson(`/users/${userId}/role`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ role }),
    }),
  changeUserStatus: (userId: string | number, status: 'active' | 'suspended') =>
    requestJson(`/users/${userId}/status`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ status }),
    }),
  updateUserLimits: (
    userId: string | number,
    payload: {
      max_apps: number;
      max_memory_mb: number;
      max_cpus: number;
      max_storage_mb: number;
      bandwidth_limit_mbps?: number;
      allow_domain_file_server: boolean;
      default_shield_id?: number | null;
    }
  ) =>
    requestJson(`/users/${userId}/limits`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        max_apps: payload.max_apps,
        max_memory_mb: payload.max_memory_mb,
        max_cpus: payload.max_cpus,
        max_storage_mb: payload.max_storage_mb,
        bandwidth_limit_mbps: payload.bandwidth_limit_mbps ?? 0,
        allow_domain_file_server: payload.allow_domain_file_server ? 'on' : '',
        default_shield_id: payload.default_shield_id ?? '',
      }),
    }),
  auditLogsList: (page = 1, perPage = 10) =>
    requestJson<{
      ok: boolean;
      logs: Array<{
        id: number;
        user_id?: number;
        username: string;
        action: string;
        resource_type: string;
        resource_id: string;
        details: string;
        ip_address: string;
        user_agent: string;
        created_at: string;
      }>;
      total: number;
      page: number;
      per_page: number;
      total_pages: number;
    }>(`/api/audit-logs?format=json&page=${page}&per_page=${perPage}`),
  getSettings: () =>
    requestJson<{
      ok: boolean;
      settings: Record<string, string>;
      cleanup_enabled?: boolean;
      cleanup_interval?: string;
      cleanup_include_build_cache?: boolean;
      cleanup_last_run?: string;
      cleanup_last_log?: string;
      storage_overselling_enabled?: boolean;
    }>('/settings?format=json'),
  saveSettings: (payload: {
    cleanup_enabled?: boolean;
    cleanup_interval?: string;
    cleanup_include_build_cache?: boolean;
    storage_overselling_enabled?: boolean;
  }) =>
    requestJson<{
      ok: boolean;
      error?: string;
      overcommitted?: boolean;
      allocated_mb?: number;
      host_disk_mb?: number;
      excess_mb?: number;
      users?: Array<{ id: number; username: string; max_storage_mb: number; unlimited: boolean }>;
      cleanup_enabled?: boolean;
      cleanup_interval?: string;
      cleanup_include_build_cache?: boolean;
      storage_overselling_enabled?: boolean;
    }>('/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }),
  runDockerCleanup: () =>
    requestJson<{ ok: boolean; log?: string }>('/settings/cleanup/run', { method: 'POST' }),
  getTmpCleanupInfo: async () => {
    const res = await requestJson<{
      count: number;
      bytes: number;
      human: string;
      last_run: string;
      last_log: string;
    }>('/settings/tmp/info');
    return {
      ok: res.ok,
      count: res.data?.count,
      bytes: res.data?.bytes,
      human: res.data?.human,
      total_size: res.data?.human,
      last_run: res.data?.last_run,
      last_log: res.data?.last_log,
    };
  },
  runTmpCleanup: () =>
    requestJson<{ ok: boolean; removed: number; freed: number; message: string }>(
      '/settings/tmp/clean',
      { method: 'POST' }
    ),
};
