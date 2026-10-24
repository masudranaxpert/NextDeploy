import { requestJson, toFormBody } from './http';

export const gitAndRegistryApi = {
  gitProvidersList: () =>
    requestJson<{
      ok: boolean;
      providers: Array<{
        id: number | string;
        name: string;
        provider: 'github' | 'gitlab';
        token?: string;
        notes?: string;
        accountName?: string;
        authMethod?: string;
      }>;
    }>(`/api/v1/git/providers`),
  createProvider: (payload: { name: string; provider: 'github' | 'gitlab'; token: string; notes?: string }) =>
    requestJson<{ ok: boolean; id: number; name: string; provider: string }>(`/api/v1/git/providers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }),
  webhookDeliveries: (limit = 10) =>
    requestJson<
      Array<{
        app_id: string;
        delivery_id: string;
        provider: string;
        repo: string;
        event: string;
        sha: string;
        status: string;
        latency: string;
        created_at: string;
      }>
    >(`/api/v1/git/webhooks?limit=${limit}`),
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
  publicGitRemoteBranches: (appId: string, url: string) =>
    requestJson<{
      ok?: boolean;
      branches?: string[];
      error?: string;
    }>(`/apps/${encodeURIComponent(appId)}/git/remote-branches?url=${encodeURIComponent(url)}`),
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
      installation_id?: string;
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
  addCollaborator: (
    appId: string,
    username: string,
    role: 'developer' | 'viewer' | 'deployer',
    permissions: string[] = []
  ) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/collaborators`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username,
        role: role === 'deployer' ? 'developer' : role,
        permissions,
      }),
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
};
