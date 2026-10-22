import { requestJson, toFormBody } from './http';

export const backupsAndMigrateApi = {
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
};
