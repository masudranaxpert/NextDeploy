import { BootstrapData, DomainFormPayload, requestJson, requestText, resultFromResponse, toFormBody } from './http';

// Helper to encode file paths in Base64 for query parameters.
// This prevents external WAFs (like Cloudflare Managed Rules for WordPress / PHP)
// from inspecting queries and throwing 403 Forbidden on sensitive files like wp-config.php or .env.
const toPathB64Param = (path = ''): string => {
  if (!path) return '';
  try {
    const b64 = btoa(unescape(encodeURIComponent(path)));
    return `p_b64=${encodeURIComponent(b64)}`;
  } catch {
    return `path=${encodeURIComponent(path)}`;
  }
};

export const appsAndDockerApi = {
  // Bootstrap & Auth
  // The first call reuses the request index.html started before the scripts finished loading.
  bootstrap: async () => {
    const w = window as unknown as { __ndBoot?: Promise<Response | null> | null };
    const early = w.__ndBoot;
    w.__ndBoot = null;
    if (early) {
      try {
        const res = await early;
        if (res) return await resultFromResponse<BootstrapData>(res);
      } catch {
        // fall through to a normal request
      }
    }
    return requestJson<BootstrapData>('/api/panel/bootstrap');
  },
  setup: (username: string, password: string, confirm: string) =>
    requestJson<{ ok: boolean; error?: string }>('/setup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password, confirm }),
    }),
  login: (username: string, password: string, next = '/overview') =>
    requestJson<{ ok: boolean; error?: string }>('/login', {
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
  deleteApp: (appId: string) =>
    requestJson<{ ok: boolean; message?: string; error?: string }>(
      `/api/v1/apps/${encodeURIComponent(appId)}/delete`,
      { method: 'POST' }
    ),
  composeAction: (appId: string, action: 'up' | 'down' | 'restart' | 'redeploy', service?: string) =>
    requestJson<{ ok: boolean; job_id?: string; action?: string; error?: string }>(
      `/apps/${encodeURIComponent(appId)}/compose/${action}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: toFormBody({ service }),
      }
    ),
  deployProgress: (appId: string) =>
    requestJson<{
      ok: boolean;
      id: string;
      output: string;
      running: boolean;
      action: string;
    }>(`/apps/${encodeURIComponent(appId)}/partials/deploy-progress`),
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
  deployLogGet: (appId: string, logId: string | number) =>
    requestJson<{
      action: string;
      ok: boolean;
      output: string;
      created_label: string;
    }>(`/apps/${encodeURIComponent(appId)}/deploy-logs/${encodeURIComponent(String(logId))}`),
  deployLogDelete: (appId: string, logId: string | number) =>
    requestJson(
      `/apps/${encodeURIComponent(appId)}/deploy-logs/${encodeURIComponent(String(logId))}/delete`,
      { method: 'POST' }
    ),
  rollbackDeployment: (appId: string, depId: string | number) =>
    requestJson<{
      ok: boolean;
      active_slot?: string;
      pinned_slot?: string;
      warm?: boolean;
      message?: string;
      error?: string;
    }>(`/apps/${encodeURIComponent(appId)}/deployments/${encodeURIComponent(String(depId))}/rollback`, {
      method: 'POST',
    }),
  pinAppSlot: (appId: string, slot: string) =>
    requestJson<{
      ok: boolean;
      pinned_slot?: string;
      error?: string;
    }>(`/apps/${encodeURIComponent(appId)}/slots/pin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ slot }),
    }),
  promoteAppSlot: (appId: string) =>
    requestJson<{
      ok: boolean;
      active_slot?: string;
      pinned_slot?: string;
      message?: string;
      error?: string;
    }>(`/apps/${encodeURIComponent(appId)}/slots/promote`, {
      method: 'POST',
    }),
  saveAppEnv: (appId: string, envContent: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/env`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ env: envContent }),
    }),

  // Workspace File Manager
  filesList: (appId: string, path = '', recursive = true) => {
    const pParam = toPathB64Param(path);
    const q = pParam ? `${pParam}&` : '';
    return requestJson<
      Array<{
        name: string;
        rel_path: string;
        is_dir: boolean;
        size: number;
        mod_time: number;
        perms: string;
      }>
    >(`/api/v1/apps/${encodeURIComponent(appId)}/files?${q}recursive=${recursive ? 'true' : 'false'}`);
  },
  filesTree: (appId: string, path = '') => {
    const pParam = toPathB64Param(path);
    return requestJson(`/apps/${encodeURIComponent(appId)}/files/tree${pParam ? `?${pParam}` : ''}`);
  },
  fileBlob: (appId: string, path: string) =>
    requestJson<{
      path: string;
      name: string;
      size: number;
      text?: string;
      binary?: boolean;
      too_large?: boolean;
    }>(`/apps/${encodeURIComponent(appId)}/files/blob?${toPathB64Param(path)}`),
  fileSave: (appId: string, path: string, content: string) => {
    let pB64 = '';
    try {
      pB64 = btoa(unescape(encodeURIComponent(path)));
    } catch {}
    return requestJson(`/apps/${encodeURIComponent(appId)}/files/save?${toPathB64Param(path)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content, p_b64: pB64, path }),
    });
  },
  fileCreate: (appId: string, path: string, isDir = false) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/${isDir ? 'mkdir' : 'create'}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    }),
  fileDelete: (appId: string, path: string | string[]) => {
    const fd = new FormData();
    const arr = Array.isArray(path) ? path : [path];
    arr.forEach((p) => fd.append('paths', p));
    return requestJson<{ ok: boolean; message?: string }>(
      `/apps/${encodeURIComponent(appId)}/files/delete?format=json`,
      {
        method: 'POST',
        body: fd,
      }
    );
  },
  fileRename: (appId: string, oldPath: string, newPath: string) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/rename`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ old_path: oldPath, new_path: newPath }),
    }),
  fileMove: (appId: string, paths: string[], dest: string, overwrite = false) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/move`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths, dest, overwrite }),
    }),
  fileCopy: (appId: string, paths: string[], dest: string, overwrite = false) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/files/copy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths, dest, overwrite }),
    }),
  fileChmod: (appId: string, path: string, mode: string) =>
    requestJson<{ ok?: boolean; message?: string; mode?: string }>(
      `/apps/${encodeURIComponent(appId)}/files/chmod`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path, mode }),
      }
    ),
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
  fileUpload: (
    appId: string,
    files: File | File[],
    destDir = '',
    onProgress?: (pct: number) => void
  ) => {
    const fd = new FormData();
    fd.append('path', destDir);
    const fileList = Array.isArray(files) ? files : [files];
    fileList.forEach((f) => fd.append('file', f));
    // XHR is used because fetch cannot report upload progress.
    return new Promise<{ ok: boolean; message?: string }>((resolve) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `/apps/${encodeURIComponent(appId)}/files/upload`);
      xhr.setRequestHeader('Accept', 'application/json');
      xhr.setRequestHeader('X-Requested-With', 'XMLHttpRequest');
      xhr.setRequestHeader('X-NextDeploy-Client', 'spa');
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100));
      };
      xhr.onerror = () => resolve({ ok: false, message: 'Network error during upload' });
      xhr.onload = () => {
        let body: { message?: string; error?: string } = {};
        try {
          body = JSON.parse(xhr.responseText);
        } catch {
          // Non-JSON body: rely on status code only.
        }
        const ok = xhr.status >= 200 && xhr.status < 300;
        resolve({ ok, message: body.message || body.error || (ok ? undefined : `HTTP ${xhr.status}`) });
      };
      xhr.send(fd);
    });
  },
  uploadZip: (appId: string, file: File) => {
    const fd = new FormData();
    fd.append('archive', file);
    return requestJson<{ ok: boolean; message?: string }>(
      `/apps/${encodeURIComponent(appId)}/upload-zip`,
      { method: 'POST', body: fd }
    );
  },
  fileDownloadZipUrl: (appId: string, paths?: string[]) => {
    const q = paths && paths.length ? `?paths=${encodeURIComponent(paths.join(','))}` : '';
    return `/apps/${encodeURIComponent(appId)}/files/download-zip${q}`;
  },
  fileMkdir: (appId: string, path: string) =>
    requestJson<{ ok: boolean; message?: string }>(
      `/apps/${encodeURIComponent(appId)}/files/mkdir`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      }
    ),

  // Container & Image Lifecycle
  appContainerAction: (
    appId: string,
    containerName: string,
    action: 'start' | 'stop' | 'restart' | 'remove'
  ) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/containers/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ name: containerName, container: containerName }),
    }),
  globalContainerRestart: (containerName: string) =>
    requestJson('/containers/restart', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ name: containerName }),
    }),
  appExec: (appId: string, container: string, command: string) => {
    const fd = new FormData();
    fd.append('container', container);
    fd.append('command', command);
    return requestJson<{ ok: boolean; output: string }>(
      `/apps/${encodeURIComponent(appId)}/exec`,
      { method: 'POST', body: fd }
    );
  },
  appContainersRemoveSelected: (appId: string, containerNames: string[]) => {
    const body = new URLSearchParams();
    for (const name of containerNames) {
      body.append('name', name);
      body.append('containers', name);
    }
    return requestJson(`/apps/${encodeURIComponent(appId)}/containers/remove-selected`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });
  },
  globalContainerAction: (containerName: string, action: 'restart' | 'remove') =>
    requestJson(`/containers/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ name: containerName }),
    }),
  globalContainersRemoveSelected: (containerNames: string[]) => {
    const body = new URLSearchParams();
    for (const name of containerNames) {
      body.append('name', name);
    }
    return requestJson('/containers/remove-selected', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });
  },
  globalContainersPrune: () =>
    requestJson<{ ok: boolean; message?: string }>('/containers/prune', { method: 'POST' }),
  globalImageRemove: (idOrTag: string) =>
    requestJson(`/images/remove`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ id: idOrTag, image_id: idOrTag }),
    }),
  globalImagesPrune: () =>
    requestJson<{ ok: boolean; message?: string }>('/images/prune', { method: 'POST' }),
  globalVolumeRemove: (volumeName: string) =>
    requestJson('/volumes/remove', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({ name: volumeName }),
    }),

  // Domains & DNS Check
  domainCreate: (appId: string, payload: DomainFormPayload) =>
    requestJson(`/apps/${encodeURIComponent(appId)}/domains`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: toFormBody({
        domain: payload.domain,
        service: payload.service,
        port: payload.port,
        is_primary: payload.is_primary ? 'on' : '',
        enable_https: payload.enable_https !== false ? 'on' : '',
        enable_www: payload.enable_www ? 'on' : '',
        serve_static: payload.serve_static ? 'on' : '',
        static_path: payload.static_path || '',
        static_url_prefix: payload.static_url_prefix || '',
        serve_media: payload.serve_media ? 'on' : '',
        media_path: payload.media_path || '',
        media_url_prefix: payload.media_url_prefix || '',
        shield_node_id: payload.shield_node_id != null ? String(payload.shield_node_id) : '',
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
        is_primary: payload.is_primary ? 'on' : 'false',
        enable_https: payload.enable_https !== false ? 'on' : '',
        enable_www: payload.enable_www ? 'on' : '',
        serve_static: payload.serve_static ? 'on' : '',
        static_path: payload.static_path || '',
        static_url_prefix: payload.static_url_prefix || '',
        serve_media: payload.serve_media ? 'on' : '',
        media_path: payload.media_path || '',
        media_url_prefix: payload.media_url_prefix || '',
        shield_node_id: payload.shield_node_id != null ? String(payload.shield_node_id) : '',
      }),
    }),
  previewSave: (
    appId: string,
    cfg: { provider: string; base: string; service: string; port: number }
  ) =>
    requestJson<{ ok: boolean; host_base?: string; error?: string }>(
      `/apps/${encodeURIComponent(appId)}/preview`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(cfg),
      }
    ),
  domainSetPrimary: (appId: string, domainId: string | number) =>
    requestJson<{
      ok: boolean;
      primary_domain?: string;
      domains?: any[];
      error?: string;
    }>(`/apps/${encodeURIComponent(appId)}/domains/${domainId}/primary`, {
      method: 'POST',
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
  uploadWorkspaceArchive: async (appId: string, file: File) => {
    const fd = new FormData();
    fd.append('archive', file);
    const res = await fetch(`/api/v1/apps/${encodeURIComponent(appId)}/workspace/archive`, {
      method: 'POST',
      body: fd,
    });
    return res.json();
  },
  appLimitsGet: (appId: string) =>
    requestJson<AppLimitsInfo>(`/apps/${encodeURIComponent(appId)}/limits`),
  appLimitsSet: (appId: string, memoryMb: number, cpus: number) =>
    requestJson<{ ok: boolean; needs_redeploy?: boolean; error?: string }>(
      `/apps/${encodeURIComponent(appId)}/limits`,
      { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ memory_mb: memoryMb, cpus }) }
    ),
  metricsHistory: () =>
    requestJson<{ ok?: boolean; history?: any[] }>('/api/v1/metrics/history'),
};

/** A project's own memory/CPU ceiling, the most it may be given, and what it uses now. */
export interface AppLimitsInfo {
  ok: boolean;
  limits: { memory_mb: number; cpus: number };
  cap: { memory_mb: number; cpus: number; source: 'account' | 'server' };
  usage: { memory_mb: number; cpu_cores: number };
  /** False when this server cannot enforce group limits (the numbers are stored but not applied). */
  enforced: boolean;
}
