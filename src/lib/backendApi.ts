// Unified NextDeploy Go Backend API Client
// Calls real Fiber endpoints in main.go / internal/handlers/* when running on the Go server,
// and falls back gracefully in standalone preview environments.

export interface ApiResult<T = Record<string, unknown>> {
  ok: boolean;
  data?: T;
  error?: string;
  offlineFallback?: boolean;
}

const DEFAULT_HEADERS: Record<string, string> = {
  Accept: 'application/json',
  'X-Requested-With': 'XMLHttpRequest',
  'X-NextDeploy-Client': 'spa',
};

async function requestJson<T = Record<string, unknown>>(
  path: string,
  options: RequestInit = {}
): Promise<ApiResult<T>> {
  try {
    const headers: Record<string, string> = {
      ...DEFAULT_HEADERS,
      ...(options.headers as Record<string, string> | undefined),
    };
    const res = await fetch(path, {
      credentials: 'same-origin',
      ...options,
      headers,
    });
    const contentType = res.headers.get('content-type') || '';
    if (!contentType.includes('application/json')) {
      return { ok: res.ok, offlineFallback: true };
    }
    const json = (await res.json()) as T & { ok?: boolean; error?: string; message?: string };
    if (!res.ok || json.ok === false) {
      return {
        ok: false,
        data: json,
        error: json.error || json.message || `HTTP ${res.status}`,
      };
    }
    return { ok: true, data: json };
  } catch {
    return { ok: false, offlineFallback: true };
  }
}

function toFormBody(params: Record<string, string | number | boolean | undefined>): URLSearchParams {
  const body = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined) {
      body.set(k, String(v));
    }
  }
  return body;
}

export const backendApi = {
  // Bootstrap & Auth
  bootstrap: () => requestJson('/api/panel/bootstrap'),
  login: (username: string, password: string, next = '/overview') =>
    requestJson('/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password, next }),
    }),
  setupAdmin: (username: string, password: string, confirm: string) =>
    requestJson('/setup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password, confirm }),
    }),
  logout: () => requestJson('/logout', { method: 'POST' }),

  // Apps & Compose Lifecycle
  createApp: (name: string) =>
    requestJson('/apps', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ name }),
    }),
  deleteApp: (appId: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/delete`, { method: 'POST' }),
  composeAction: (appId: string, action: 'up' | 'down' | 'restart' | 'redeploy') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/compose/${action}`, { method: 'POST' }),
  saveComposeFile: (appId: string, composeFile: string, content: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/compose-file`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ compose_file: composeFile, compose_content: content }),
    }),
  saveAppEnv: (appId: string, envContent: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/env`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ env: envContent }),
    }),

  // Workspace File Manager
  filesTree: (appId: string, path = '') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/tree?path=${encodeURIComponent(path)}`),
  fileBlob: (appId: string, path: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/blob?path=${encodeURIComponent(path)}`),
  fileSave: (appId: string, path: string, content: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/save?path=${encodeURIComponent(path)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    }),
  fileCreate: (appId: string, path: string, isDir: boolean) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/${isDir ? 'mkdir' : 'create'}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    }),
  fileDelete: (appId: string, path: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ path }),
    }),
  fileRename: (appId: string, oldPath: string, newName: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/rename`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ old_path: oldPath, new_name: newName }),
    }),
  fileMove: (appId: string, paths: string[], dest: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/move`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths, dest }),
    }),
  fileCopy: (appId: string, paths: string[], dest: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/copy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths, dest }),
    }),
  fileZip: (appId: string, paths: string[], name: string, dest = '') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/zip`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths, name, dest }),
    }),
  fileUnzip: (appId: string, path: string, dest = '') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/unzip`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, dest }),
    }),
  fileUrlUpload: (appId: string, url: string, dest = '') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/url-upload`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, dest }),
    }),

  // Domains & DNS Check
  domainCreate: (
    appId: string,
    payload: {
      domain: string;
      service: string;
      port: number;
      enable_https: boolean;
      enable_www: boolean;
    }
  ) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/domains`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        domain: payload.domain,
        service: payload.service,
        port: payload.port,
        enable_https: payload.enable_https ? '1' : '0',
        enable_www: payload.enable_www ? '1' : '0',
      }),
    }),
  domainDelete: (appId: string, domainId: string | number) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/domains/${domainId}/delete`, {
      method: 'POST',
    }),
  domainDnsCheck: (appId: string, domainId: string | number) =>
    requestJson<{
      ok: boolean;
      domain: string;
      ips: string[];
      cloudflare: boolean;
      internalTLS: boolean;
    }>(`/apps/${encodeURIComponent(appId)}/domains/${domainId}/dns-check`),
  domainLabels: (appId: string, domainId: string | number) =>
    requestJson<{ ok: boolean; yaml: string }>(
      `/apps/${encodeURIComponent(appId)}/domains/${domainId}/labels`
    ),

  // Git Source & Providers
  switchAppSource: (appId: string, sourceType: 'git' | 'files') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/switch-source`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ source_type: sourceType }),
    }),
  toggleGitSync: (appId: string, paused: boolean) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/git-sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ paused: paused ? '1' : '0' }),
    }),
  gitSyncNow: (appId: string, force = false) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/git/sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ force: force ? '1' : '0' }),
    }),

  // Collaborators & Ownership Transfer
  addCollaborator: (appId: string, username: string, role: 'developer' | 'viewer') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/collaborators`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ username, role }),
    }),
  removeCollaborator: (appId: string, userId: number | string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/collaborators/${userId}/delete`, {
      method: 'POST',
    }),
  transferOwnership: (appId: string, username: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/transfer`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ username }),
    }),

  // Backups
  triggerManualBackup: (appId: string, destId: string | number, type = 'full', volumeName = '') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/${destId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ type, volume_name: volumeName }),
    }),
  restoreBackup: (appId: string, historyId: string | number) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/restore/${historyId}`, {
      method: 'POST',
    }),

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
};
