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

async function requestText(path: string): Promise<ApiResult<string>> {
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
      return { ok: false, error: text || `HTTP ${res.status}` };
    }
    return { ok: true, data: text };
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

export const backendApi = {
  // Bootstrap & Auth
  bootstrap: () =>
    requestJson<{
      ok?: boolean;
      cli_sessions?: Array<{
        id: string;
        name?: string;
        token_prefix?: string;
        hostname?: string;
        os?: string;
        arch?: string;
        last_used_at?: string;
        created_at?: string;
        ip?: string;
      }>;
    }>('/api/panel/bootstrap'),
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
  createApp: (
    payload:
      | string
      | {
          name: string;
          description?: string;
          sourceType?: string;
          gitRepo?: string;
          gitBranch?: string;
          composeContent?: string;
        }
  ) => {
    const name = typeof payload === 'string' ? payload : payload.name;
    return requestJson('/apps', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ name }),
    });
  },
  deleteApp: (appId: string, confirmName?: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ confirm_name: confirmName || appId }),
    }),
  composeAction: (appId: string, action: 'up' | 'down' | 'restart' | 'redeploy') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/compose/${action}`, { method: 'POST' }),
  saveComposeFile: (appId: string, composeFile: string, content?: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/compose-file`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ compose_file: composeFile, compose_content: content }),
    }),
  composeView: async (appId: string) => {
    const res = await requestText(`/apps/${encodeURIComponent(appId)}/compose-view`);
    return { ...res, yaml: res.data };
  },
  clearDeployLogs: (appId: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/deploy-logs/clear`, { method: 'POST' }),
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
  fileCreate: (appId: string, path: string, isDir = false) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/${isDir ? 'mkdir' : 'create'}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    }),
  fileDelete: (appId: string, path: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/delete?format=json`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ paths: path }),
    }),
  fileRename: (appId: string, oldPath: string, newPath: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/rename`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ old_path: oldPath, new_path: newPath }),
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
  fileZip: (appId: string, paths: string | string[], name = 'archive.zip', dest = '') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/zip`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        paths: Array.isArray(paths) ? paths : [paths],
        name,
        dest,
      }),
    }),
  fileUnzip: (appId: string, path: string, dest = '') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/unzip`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, dest }),
    }),
  fileUrlUpload: async (appId: string, url: string, dest = '') => {
    const res = await requestJson<{ file?: string; bytes?: number }>(
      `/apps/${encodeURIComponent(appId)}/files/url-upload`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url, dest }),
      }
    );
    return { ...res, file: res.data?.file, bytes: res.data?.bytes };
  },

  // Domains & DNS Check
  domainCreate: (appId: string, payload: DomainFormPayload) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/domains`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        domain: payload.domain,
        service: payload.service,
        port: payload.port,
        enable_https: payload.enable_https !== false ? 'on' : '',
        enable_www: payload.enable_www ? 'on' : '',
        serve_static: payload.serve_static ? 'on' : '',
        static_path: payload.static_path || '',
        static_url_prefix: payload.static_url_prefix || '',
        serve_media: payload.serve_media ? 'on' : '',
        media_path: payload.media_path || '',
        media_url_prefix: payload.media_url_prefix || '',
      }),
    }),
  domainEdit: (appId: string, domainId: string | number, payload: DomainFormPayload) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/domains/${domainId}/edit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        domain: payload.domain,
        service: payload.service,
        port: payload.port,
        enable_https: payload.enable_https !== false ? 'on' : '',
        enable_www: payload.enable_www ? 'on' : '',
        serve_static: payload.serve_static ? 'on' : '',
        static_path: payload.static_path || '',
        static_url_prefix: payload.static_url_prefix || '',
        serve_media: payload.serve_media ? 'on' : '',
        media_path: payload.media_path || '',
        media_url_prefix: payload.media_url_prefix || '',
      }),
    }),
  domainDelete: (appId: string, domainId: string | number) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/domains/${domainId}/delete`, {
      method: 'POST',
    }),
  domainDnsCheck: async (appId: string, domainId: string | number) => {
    const res = await requestJson<{
      ok: boolean;
      domain: string;
      ips: string[];
      cloudflare: boolean;
      internalTLS: boolean;
      error?: string;
    }>(`/apps/${encodeURIComponent(appId)}/domains/${domainId}/dns-check`);
    return {
      ...res,
      domain: res.data?.domain,
      ips: res.data?.ips,
      cloudflare: res.data?.cloudflare,
      internalTLS: res.data?.internalTLS,
    };
  },
  domainLabels: async (appId: string, domainId: string | number) => {
    const res = await requestJson<{
      ok?: boolean;
      domain?: string;
      port?: number;
      labels?: Record<string, string>;
      yaml?: string;
      snippet?: string;
    }>(`/apps/${encodeURIComponent(appId)}/domains/${domainId}/labels`);
    return {
      ...res,
      domain: res.data?.domain,
      port: res.data?.port,
      labels: res.data?.labels,
      yaml: res.data?.yaml,
      snippet: res.data?.yaml || res.data?.snippet,
    };
  },

  // Git Source & Providers (GitHub App + GitLab OAuth 2.0 + Git Sync)
  switchAppSource: (appId: string, sourceType: 'git' | 'files') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/switch-source`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ source_type: sourceType }),
    }),
  switchSourceMode: (appId: string, sourceType: 'git' | 'files' | 'upload') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/switch-source`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ source_type: sourceType === 'git' ? 'git' : 'files' }),
    }),
  saveAppGit: (
    appId: string,
    payload: {
      auth_mode: 'public' | 'github_app' | 'gitlab_token';
      git_provider_id?: string | number;
      repo_url: string;
      branch: string;
      auto_deploy: boolean;
    }
  ) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/git`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        auth_mode: payload.auth_mode,
        git_provider_id: payload.git_provider_id ?? '',
        repo_url: payload.repo_url,
        branch: payload.branch,
        auto_deploy: payload.auto_deploy ? 'on' : '',
      }),
    }),
  deleteAppGit: (appId: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/git/delete`, {
      method: 'POST',
    }),
  gitProviderRepos: (appId: string, providerId: string | number) =>
    requestJson<{
      ok?: boolean;
      repos?: Array<{
        full_name: string;
        clone_url: string;
        html_url: string;
        private: boolean;
        default_branch: string;
      }>;
    }>(`/apps/${encodeURIComponent(appId)}/git/providers/${providerId}/repos`),
  gitProviderBranches: (appId: string, providerId: string | number, repoFullName: string) =>
    requestJson<{
      ok?: boolean;
      branches?: string[];
    }>(
      `/apps/${encodeURIComponent(appId)}/git/providers/${providerId}/branches?repo=${encodeURIComponent(repoFullName)}`
    ),
  setGitSync: (appId: string, enabled: boolean, force = false) =>
    requestJson<{
      ok?: boolean;
      git_sync?: boolean;
      warning?: string;
      confirm_needed?: boolean;
      result?: {
        changed?: boolean;
        files_copied?: number;
        files_kept_newer_in_workspace?: number;
        files_unchanged?: number;
        notes?: string[];
        next_step?: string;
      };
    }>(`/apps/${encodeURIComponent(appId)}/git-sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        enabled: enabled ? '1' : '0',
        force: force ? '1' : '0',
      }),
    }),
  toggleGitSync: (appId: string, paused: boolean, force = false) =>
    requestJson<{
      ok?: boolean;
      git_sync?: boolean;
      warning?: string;
      confirm_needed?: boolean;
    }>(`/apps/${encodeURIComponent(appId)}/git-sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        enabled: !paused ? '1' : '0',
        force: force ? '1' : '0',
      }),
    }),
  toggleGitAutoSync: (appId: string, paused: boolean) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/git-sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        enabled: !paused ? '1' : '0',
        force: '0',
      }),
    }),
  gitTree: (appId: string, path = '') =>
    requestJson<{
      ok?: boolean;
      path?: string;
      entries?: Array<{
        name: string;
        path: string;
        is_dir: boolean;
        size: number;
        mod_time?: string;
      }>;
    }>(`/apps/${encodeURIComponent(appId)}/git/tree?path=${encodeURIComponent(path)}`),
  gitBlob: (appId: string, path: string) =>
    requestJson<{
      ok?: boolean;
      path?: string;
      content?: string;
      binary?: boolean;
    }>(`/apps/${encodeURIComponent(appId)}/git/blob?path=${encodeURIComponent(path)}`),
  gitSyncNow: (appId: string, force = false) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/git/sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ force: force ? '1' : '0' }),
    }),
  startGitHubApp: (appName = '') =>
    requestJson('/git/github/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ app_name: appName }),
    }),
  refreshGitHubInstall: (providerId: string | number) =>
    requestJson(`/git/${providerId}/github/refresh-installation`, {
      method: 'POST',
    }),
  startGitLabOAuth: (clientId: string, clientSecret: string) =>
    requestJson('/git/gitlab/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ client_id: clientId, client_secret: clientSecret }),
    }),
  updateGitProvider: (
    providerId: string | number,
    payload: {
      github_app_id?: string;
      github_installation_id?: string;
      github_private_key?: string;
      github_webhook_secret?: string;
      client_id?: string;
      client_secret?: string;
    }
  ) =>
    requestJson(`/git/${providerId}/update`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody(payload),
    }),
  deleteGitProvider: (providerId: string | number) =>
    requestJson(`/git/${providerId}/delete`, {
      method: 'POST',
    }),

  // Container Registries (Global vs Personal Scope — POST /registries)
  addRegistry: (payload: {
    name: string;
    server: string;
    username: string;
    password: string;
    is_global: boolean;
  }) =>
    requestJson<{
      ok: boolean;
      id: number;
      name: string;
      server: string;
      username: string;
      scope: 'global' | 'personal';
    }>('/registries', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        name: payload.name,
        server_address: payload.server,
        server: payload.server,
        username: payload.username,
        password: payload.password,
        is_global: payload.is_global ? 'on' : '',
      }),
    }),
  deleteRegistry: (registryId: string | number) =>
    requestJson(`/registries/${registryId}/delete`, {
      method: 'POST',
    }),

  // Collaborators & Ownership Transfer
  addCollaborator: (appId: string, username: string, role: 'developer' | 'viewer' | 'deployer') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/collaborators`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ username, role: role === 'deployer' ? 'developer' : role }),
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

  // Backups & Rclone Destinations (Google Drive OAuth 2.0 + Cloudflare R2)
  backupDestinationsList: () =>
    requestJson<{
      destinations: Array<{
        id?: number;
        ID?: number;
        name?: string;
        Name?: string;
        provider?: 'gdrive' | 'r2';
        Provider?: 'gdrive' | 'r2';
        config?: string;
        Config?: string;
        created_at?: string;
        CreatedAt?: string;
        account_id?: string;
        bucket?: string;
        folder_id?: string;
        email?: string;
        display_name?: string;
        info_error?: string;
        storage_limit?: number;
        storage_used?: number;
        usage?: { Total?: number; Used?: number; Free?: number };
      }>;
    }>('/backup/destinations'),
  backupGDriveAuthUrl: async (clientId: string, clientSecret: string, state = '') => {
    const res = await requestJson<{
      auth_url?: string;
      redirect_url?: string;
      url?: string;
      redirect_uri?: string;
    }>(
      `/backup/gdrive/auth-url?client_id=${encodeURIComponent(clientId)}&client_secret=${encodeURIComponent(clientSecret)}&state=${encodeURIComponent(state)}`
    );
    return {
      ...res,
      auth_url: res.data?.auth_url || res.data?.url,
      redirect_url: res.data?.redirect_url || res.data?.redirect_uri,
    };
  },
  backupDestinationCreate: (
    payload:
      | {
          provider: 'gdrive';
          name: string;
          token?: string;
          folder_id?: string;
          client_id?: string;
          client_secret?: string;
          auth_mode?: 'rclone_token' | 'custom_oauth';
        }
      | {
          provider: 'r2';
          name: string;
          account_id: string;
          access_key_id: string;
          secret_access_key: string;
          bucket: string;
        }
  ) =>
    requestJson<{ id?: number; message?: string; auth_url?: string }>('/backup/destinations', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody(payload),
    }),
  backupDestinationDelete: (destId: string | number) =>
    requestJson(`/backup/destinations/${destId}/delete`, {
      method: 'POST',
    }),
  triggerManualBackup: (
    appId: string,
    destId: string | number,
    type: 'full' | 'volume' | 'app' = 'full',
    volumeNames = '',
    pauseContainers = false
  ) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/${destId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        type,
        volume_name: volumeNames,
        volume_names: volumeNames,
        pause_containers: pauseContainers ? 'true' : 'false',
      }),
    }),
  backupNow: (appId: string, scope: 'full' | 'volume' | 'app' = 'full') =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/1`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ type: scope }),
    }),
  backupDelete: (appId: string, historyId: string | number) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/history/${historyId}/delete`, {
      method: 'POST',
    }),
  backupSchedulesList: (appId: string) =>
    requestJson<{
      schedules: Array<{
        id?: number;
        ID?: number;
        app_id?: string;
        AppID?: number;
        destination_id?: number;
        DestinationID?: number;
        destination_name?: string;
        destination_provider?: string;
        type?: 'full' | 'volume' | 'app';
        BackupType?: 'full' | 'volume' | 'app';
        volume_name?: string;
        VolumeNames?: string;
        cron?: string;
        CronExpression?: string;
        retention?: number;
        RetentionCount?: number;
        pause_containers?: boolean;
        PauseContainers?: boolean;
        enabled?: boolean;
        Enabled?: boolean;
        last_run?: string;
        LastRun?: string;
        next_run_at?: string;
        NextRunAt?: string;
      }>;
    }>(`/apps/${encodeURIComponent(appId)}/backup/schedules`),
  backupScheduleCreate: (
    appId: string,
    destId: string | number,
    payload: {
      type: 'full' | 'volume' | 'app';
      volume_names?: string;
      cron: string;
      retention: number;
      pause_containers: boolean;
    }
  ) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/schedule/${destId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        type: payload.type,
        volume_names: payload.volume_names || '',
        cron: payload.cron,
        retention: payload.retention,
        pause_containers: payload.pause_containers ? 'true' : 'false',
      }),
    }),
  backupScheduleUpdate: (
    appId: string,
    scheduleId: string | number,
    payload: {
      destination?: string | number;
      type: 'full' | 'volume' | 'app';
      volume_names?: string;
      cron: string;
      retention: number;
      pause_containers: boolean;
    }
  ) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/schedule/${scheduleId}/edit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        destination: payload.destination,
        type: payload.type,
        volume_names: payload.volume_names || '',
        cron: payload.cron,
        retention: payload.retention,
        pause_containers: payload.pause_containers ? 'true' : 'false',
      }),
    }),
  backupScheduleToggle: (appId: string, scheduleId: string | number, enabled: boolean) =>
    requestJson(
      `/apps/${encodeURIComponent(appId)}/backup/schedule/${scheduleId}/toggle?enabled=${enabled ? 'true' : 'false'}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: toFormBody({ enabled: enabled ? 'true' : 'false' }),
      }
    ),
  backupScheduleDelete: (appId: string, scheduleId: string | number) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/backup/schedule/${scheduleId}/delete`, {
      method: 'POST',
    }),
  backupHistoryList: (appId: string, page = 1, perPage = 5) =>
    requestJson<{
      history: Array<{
        id?: number;
        ID?: number;
        app_id?: string;
        AppID?: number;
        destination_id?: number;
        DestinationID?: number;
        destination_name?: string;
        destination_provider?: string;
        type?: string;
        BackupType?: string;
        volume_name?: string;
        VolumeName?: string;
        status?: string;
        Status?: string;
        error_message?: string;
        ErrorMessage?: string;
        size?: number;
        SizeBytes?: number;
        started_at?: string;
        CreatedAt?: string;
        file_path?: string;
        RemotePath?: string;
        remote_missing?: boolean;
        RemoteMissing?: boolean;
      }>;
      total: number;
      page: number;
      per_page?: number;
      limit?: number;
      total_pages: number;
    }>(
      `/apps/${encodeURIComponent(appId)}/backup/history?page=${page}&per_page=${perPage}&limit=${perPage}`
    ),
  restoreBackup: (appId: string, historyId: string | number) =>
    requestJson<{ ok: boolean; job_id?: string; status?: string }>(
      `/apps/${encodeURIComponent(appId)}/backup/restore/${historyId}`,
      { method: 'POST' }
    ),
  backupRestoreStatus: (appId: string) =>
    requestJson<{
      ok?: boolean;
      status: 'idle' | 'running' | 'completed' | 'failed' | string;
      log?: string;
      error?: string;
    }>(`/apps/${encodeURIComponent(appId)}/backup/restore-status`),
  backupLog: async (appId: string, historyId: string | number) => {
    const res = await requestJson<{ status?: string; error_message?: string; log: string }>(
      `/apps/${encodeURIComponent(appId)}/backup/history/${historyId}/log`
    );
    return {
      ok: res.ok,
      status: res.data?.status,
      error_message: res.data?.error_message,
      log: res.data?.log,
    };
  },
  backupDriveLink: async (appId: string, historyId: string | number) => {
    const res = await requestJson<{ url: string; warning?: string; error?: string }>(
      `/apps/${encodeURIComponent(appId)}/backup/history/${historyId}/drivelink`
    );
    return { ok: res.ok, url: res.data?.url, warning: res.data?.warning, error: res.error };
  },

  // Server Migration (.nd-migrate Bundle Export & Estimate)
  migrateEstimate: (appIds: string[]) =>
    requestJson<{
      workspace_bytes: number;
      volume_bytes: number;
      total_bytes: number;
      total_human: string;
      app_count: number;
      volume_count: number;
    }>(`/migrate/estimate?apps=${encodeURIComponent(appIds.join(','))}`),
  migrateExportStart: (appIds: string[]) =>
    requestJson<{
      ok: boolean;
      id: number;
      status: string;
      app_count: number;
      download_url: string;
      migrate_cmd: string;
      expires_at: string;
    }>('/migrate/export', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ app_ids: appIds.join(',') }),
    }),
  migrateExportStatus: (exportId: string | number) =>
    requestJson<{
      id: number;
      status: string;
      size_bytes: number;
      size_human: string;
      app_count: number;
      volume_count: number;
      log: string;
      error_msg: string;
      expires_at: string;
      expired: boolean;
      consumed: boolean;
      download_url: string;
      migrate_cmd: string;
    }>(`/migrate/exports/${exportId}/status`),
  migrateExportDelete: (exportId: string | number) =>
    requestJson(`/migrate/exports/${exportId}/delete?ajax=1`, {
      method: 'POST',
    }),

  // Volumes Browse, Download & Multipart Archive Restore
  volumeBrowse: (name: string, path = '') =>
    requestJson<{
      ok: boolean;
      volume: string;
      path: string;
      parent_path: string;
      entries: Array<{ name: string; is_dir: boolean; rel_path: string }>;
      error?: string;
    }>(
      `/volumes/browse?format=json&name=${encodeURIComponent(name)}&path=${encodeURIComponent(path)}`
    ),
  volumeRestoreUpload: async (volumeName: string, archiveFile: File) => {
    try {
      const fd = new FormData();
      fd.append('volume_name', volumeName);
      fd.append('archive', archiveFile);
      const res = await fetch('/volumes/restore', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'X-Requested-With': 'XMLHttpRequest',
          'X-NextDeploy-Client': 'spa',
        },
        body: fd,
      });
      const contentType = res.headers.get('content-type') || '';
      if (!contentType.includes('application/json')) {
        return { ok: false, offlineFallback: true };
      }
      const json = (await res.json()) as { job_id?: string; error?: string };
      if (!res.ok || json.error) {
        return { ok: false, error: json.error || `HTTP ${res.status}` };
      }
      return { ok: true, job_id: json.job_id };
    } catch {
      return { ok: false, offlineFallback: true };
    }
  },
  volumeRestoreStatus: (jobId: string) =>
    requestJson<{
      ok?: boolean;
      status: 'running' | 'done' | 'error' | string;
      error?: string;
      extract_percent?: number;
      volume_name?: string;
    }>(`/volumes/restore/status?id=${encodeURIComponent(jobId)}&job=${encodeURIComponent(jobId)}`),

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
  caddyContainerAction: (action: 'start' | 'force_start' | 'restart' | 'stop') =>
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
      allow_domain_file_server: boolean;
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
        allow_domain_file_server: payload.allow_domain_file_server ? 'on' : '',
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
    }>(`/settings/audit-logs?format=json&page=${page}&per_page=${perPage}`),
  saveSettings: (payload: {
    cleanup_enabled: boolean;
    cleanup_interval: string;
    cleanup_include_build_cache: boolean;
  }) =>
    requestJson('/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        cleanup_enabled: payload.cleanup_enabled ? 'on' : '',
        cleanup_interval: payload.cleanup_interval,
        cleanup_include_build_cache: payload.cleanup_include_build_cache ? 'on' : '',
      }),
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
