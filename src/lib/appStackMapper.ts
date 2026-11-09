import { AppStack, MigrationExportBundle } from '../types/nextdeploy';

export function mapMigrateExports(raw: any[]): MigrationExportBundle[] {
  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  if (!Array.isArray(raw)) return [];
  return raw.map((me: any) => {
    const id = String(me.id ?? me.ID ?? '');
    const token = me.token ?? me.Token ?? '';
    const rawApps = me.apps ?? me.AppIDsJSON ?? [];
    let selectedApps: string[] = [];
    if (Array.isArray(rawApps)) {
      selectedApps = rawApps.map(String);
    } else if (typeof rawApps === 'string') {
      try {
        const parsed = JSON.parse(rawApps);
        selectedApps = Array.isArray(parsed) ? parsed.map(String) : rawApps.split(',');
      } catch {
        selectedApps = rawApps.split(',').filter(Boolean);
      }
    }
    const sizeBytes = Number(me.size_bytes ?? me.SizeBytes ?? 0);
    const status = (me.status ?? me.Status ?? 'ready').toLowerCase();
    const createdAt = me.created_at ?? me.CreatedAt ?? 'Recently';
    const expiresAt = me.expires_at ?? me.ExpiresAt;
    const downloadUrl =
      me.download_url ||
      (token ? `${origin}/migrate/download/${token}` : '');
    const migrateCmd =
      me.migrate_cmd ||
      (downloadUrl
        ? `curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main-v2/migrate.sh | sudo bash -s -- --url "${downloadUrl}"`
        : '');
    const logRaw = me.progress_log ?? me.ProgressLog ?? me.log ?? [];
    const progressLog = Array.isArray(logRaw)
      ? logRaw
      : typeof logRaw === 'string'
      ? logRaw.split('\n').filter(Boolean)
      : [];

    let expiresIn = '3h';
    if (expiresAt) {
      const diffMs = new Date(expiresAt).getTime() - Date.now();
      const mins = Math.max(0, Math.round(diffMs / (1000 * 60)));
      expiresIn = mins > 60 ? `${Math.floor(mins / 60)}h ${mins % 60}m` : `${mins}m`;
    }

    return {
      id,
      createdAt,
      selectedApps,
      estimatedSizeMB: Math.round(sizeBytes / (1024 * 1024)),
      actualSizeMB: Math.round(sizeBytes / (1024 * 1024)),
      workspaceBytes: 0,
      volumeBytes: sizeBytes,
      status: status as any,
      token,
      expiresIn,
      downloadUrl,
      migrateCmd,
      progressLog,
      error: me.error ?? me.Error ?? '',
    } as MigrationExportBundle;
  });
}

export function mapRawAppToStack(
  a: any,
  liveContainers: any[] = [],
  liveImages: any[] = []
): AppStack {
  const idNorm = (a.id || '').toLowerCase();
  const idUnderscore = idNorm.replace(/-/g, '_');
  const nameNorm = (a.name || '').toLowerCase();

  const appContainers = liveContainers.filter((c: any) => {
    const cp = (c.ComposeProject || '').toLowerCase();
    const rawName = String(c.Name || '').toLowerCase().replace(/^\//, '');
    return (
      (cp && (
        cp === idNorm ||
        cp === idUnderscore ||
        cp === nameNorm ||
        cp.startsWith(`${idNorm}_`) ||
        cp.startsWith(`${idUnderscore}_`) ||
        cp.startsWith(`${nameNorm}_`) ||
        cp.startsWith(`${idNorm}-`) ||
        cp.startsWith(`${idUnderscore}-`) ||
        cp.startsWith(`${nameNorm}-`)
      )) ||
      rawName === `nd-phpr-${idNorm}` ||
      rawName === `nd-phpr-${idUnderscore}` ||
      rawName === `nd-phpr-${nameNorm}`
    );
  });

  const checkSlotAvailable = (slot: 'blue' | 'green'): boolean => {
    const prefixes = [`${idNorm}-${slot}`, `${nameNorm}-${slot}`, `${idUnderscore}-${slot}`];
    const hasContainer = appContainers.some((c: any) => {
      const cp = (c.ComposeProject || '').toLowerCase();
      const cname = (c.Name || '').toLowerCase();
      return prefixes.some((p) => cp === p || cname.startsWith(`${p}-`));
    });
    if (hasContainer) return true;
    return liveImages.some((img: any) => {
      const tags: string[] = Array.isArray(img.RepoTags) ? img.RepoTags : img.Tags ? [img.Tags] : [];
      return tags.some((t) => {
        const tl = String(t || '').toLowerCase();
        return prefixes.some((p) => tl.startsWith(`${p}-`) || tl.startsWith(`${p}:`) || tl.startsWith(`${p}/`));
      });
    });
  };

  const slotImages = {
    blue: checkSlotAvailable('blue'),
    green: checkSlotAvailable('green'),
  };

  const hasRunningContainer = appContainers.some(
    (c: any) => (c.State || '').toLowerCase() === 'running'
  );
  const latestDeploy = Array.isArray(a.deploy_logs) ? a.deploy_logs[0] : null;
  const computedStatus: AppStack['status'] = a.php
    ? a.php.state === 'running'
      ? 'running'
      : 'stopped'
    : hasRunningContainer
    ? 'running'
    : latestDeploy && latestDeploy.ok === false
    ? 'failed'
    : 'stopped';

  return {
    id: a.id,
    php: a.php
      ? {
          siteType: a.php.site_type,
          phpVersion: a.php.php_version || '',
          rootDir: a.php.root_dir || '',
          uid: a.php.uid || 0,
        }
      : undefined,
    name: a.name,
    slug: a.name,
    description: a.description || '',
    sourceType: (a.source_type || 'compose') as any,
    gitRepo: a.git_config?.repo_url || a.git_repo || '',
    gitBranch: a.git_config?.branch || a.git_branch || 'main',
    gitCommitSha: a.git_config?.last_commit_sha || a.active_slot || 'main',
    gitCommitMessage: a.git_config?.last_commit_msg || 'Production build',
    webhookAutoDeploy: a.git_config?.auto_deploy ?? false,
    webhookSecret: a.git_config?.webhook_secret,
    status: computedStatus,
    activeSlot: a.active_slot,
    standbySlot: a.standby_slot,
    pinnedSlot: a.pinned_slot || '',
    slotImages,
    primaryDomain: (() => {
      const doms = Array.isArray(a.domains) ? a.domains : [];
      const pinned = doms.find((d: any) => d.is_primary);
      if (pinned?.domain) return pinned.domain;
      const custom = doms.find(
        (d: any) =>
          d.domain &&
          !String(d.domain).toLowerCase().endsWith('.sslip.io') &&
          !String(d.domain).toLowerCase().endsWith('.nip.io')
      );
      if (custom?.domain) return custom.domain;
      return doms[0]?.domain || '';
    })(),
    preview: a.preview,
    previewHostBase: a.preview_host_base || '',
    ports: Array.isArray(a.ports) ? a.ports : [],
    composeFilePath: 'docker-compose.yml',
    owner: a.owner_name || '',
    ownerId: a.owner_id,
    ownerRole: a.owner_role,
    updatedAt: a.updated_at || 'Recently',
    cpuPct: Math.round((a.usage?.cpu_percent || 0) * 100) / 100,
    memMB: a.usage?.memory_mb || 0,
    diskMB: a.disk_mb || 0,
    reqPerMin: 0,
    composeServices: Array.isArray(a.services) ? a.services : [],
    services:
      appContainers.length > 0
        ? appContainers.map((c: any, sIdx: number) => ({
            id: c.ID || `svc-${a.id}-${sIdx}`,
            name: c.Name || `${a.name}_${sIdx}`,
            service: c.ComposeService || (Array.isArray(a.services) && a.services[sIdx]) || 'app',
            image: c.Image || `${a.name}:latest`,
            state: ((c.State || '').toLowerCase() === 'running'
              ? 'running'
              : 'exited') as any,
            uptime: c.Status || 'Active',
            cpuPct: Math.round((c.cpu_percent || 0) * 100) / 100,
            memUsedMB: c.memory_mb || 0,
            memLimitMB: 1024,
            ports: c.Ports || '',
          }))
        : [],
    domains: Array.isArray(a.domains)
      ? a.domains.map((d: any) => ({
          id: String(d.id),
          domain: d.domain,
          targetService: d.service,
          targetPort: d.port,
          isPrimary: Boolean(d.is_primary),
          tlsProvider: 'LetsEncrypt' as const,
          tlsStatus: 'active' as const,
          forceHttps: d.enable_https ?? true,
          enableWww: d.enable_www ?? false,
          serveStatic: d.serve_static ?? false,
          staticPath: d.static_path,
          staticUrlPrefix: d.static_url_prefix,
          serveMedia: d.serve_media ?? false,
          mediaPath: d.media_path,
          mediaUrlPrefix: d.media_url_prefix,
          shield_node_id: d.shield_node_id != null ? Number(d.shield_node_id) : null,
          gzip: true,
          createdAt: d.created_at || 'Active',
        }))
      : [],
    files: [], // loaded from server when file manager opens
    envVars: (a.env_content || '')
      .split('\n')
      .filter((l: string) => l.includes('='))
      .map((l: string, evIdx: number) => {
        const parts = l.split('=');
        return {
          id: `ev-${evIdx}`,
          key: parts[0].trim(),
          value: parts.slice(1).join('=').trim(),
          isSecret: /key|secret|pass|token/i.test(parts[0]),
        };
      }),
    deployments: Array.isArray(a.deploy_logs)
      ? a.deploy_logs.map((dl: any) => ({
          id: `dep-${dl.id}`,
          commitSha: dl.commit_sha || 'latest',
          commitMessage: dl.action || 'Deploy',
          branch: 'main',
          trigger: 'manual-compose' as const,
          author: 'admin',
          status: dl.ok ? ('ready' as const) : ('failed' as const),
          durationSec: 15,
          startedAt: dl.created_at,
          slot: dl.slot,
          rawOutput: dl.output || '',
          buildSteps: [],
        }))
      : [],
    logs: [],
    volumes: Array.isArray(a.volumes)
      ? a.volumes.map((v: string) => ({
          name: v,
          mountPath: `/var/lib/${v}`,
          driver: 'local',
          sizeMB: 0,
        }))
      : [],
    collaborators: Array.isArray(a.collaborators)
      ? a.collaborators.map((c: any) => {
          let perms: string[] = [];
          try {
            if (Array.isArray(c.permissions)) perms = c.permissions;
            else if (typeof c.permissions === 'string' && c.permissions.startsWith('['))
              perms = JSON.parse(c.permissions);
            else if (typeof c.Permissions === 'string' && c.Permissions.startsWith('['))
              perms = JSON.parse(c.Permissions);
          } catch {
            perms = [];
          }
          return {
            userId: String(c.user_id || c.UserID || ''),
            username: c.username || c.Username || `user_${c.user_id || c.UserID}`,
            email: c.email || '',
            role: c.role || c.Role || 'viewer',
            permissions: perms,
            addedAt: c.created_at || c.CreatedAt || 'Active',
          };
        })
      : [],
    hasSchedule: Array.isArray(a.backup_schedules) && a.backup_schedules.some((sc: any) => sc.enabled),
    backupSchedules: Array.isArray(a.backup_schedules)
      ? a.backup_schedules.map((sc: any) => ({
          id: sc.id,
          destinationId: sc.destination_id,
          cronExpression: sc.cron_schedule || sc.cron_expression || '0 3 * * *',
          retentionCount: sc.retention_count || 7,
          backupType: sc.backup_type || 'app',
          enabled: Boolean(sc.enabled),
        }))
      : [],
  };
}
