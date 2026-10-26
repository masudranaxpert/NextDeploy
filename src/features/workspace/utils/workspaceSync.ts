import type {
  AppStack,
  ContainerService,
  DeploymentRecord,
  DomainRoute,
} from '../../../types/nextdeploy';

/**
 * Maps raw backend bootstrap data into a strongly-typed AppStack update.
 */
export function mapBootstrapApp(app: AppStack, bootData: any): Partial<AppStack> | null {
  if (!bootData?.apps) return null;
  const fresh: any = bootData.apps.find((a: any) => a.id === app.id);
  if (!fresh) return null;

  const liveContainers: any[] = Array.isArray(bootData.containers) ? bootData.containers : [];
  const liveImages: any[] = Array.isArray(bootData.images) ? bootData.images : [];
  const idNorm = (fresh.id || '').toLowerCase();
  const idUnderscore = idNorm.replace(/-/g, '_');
  const nameNorm = (fresh.name || '').toLowerCase();

  const appContainers = liveContainers.filter((c: any) => {
    const cp = (c.ComposeProject || '').toLowerCase();
    return (
      cp === idNorm ||
      cp === idUnderscore ||
      cp === nameNorm ||
      cp.startsWith(`${idNorm}_`) ||
      cp.startsWith(`${idUnderscore}_`) ||
      cp.startsWith(`${nameNorm}_`) ||
      cp.startsWith(`${idNorm}-`) ||
      cp.startsWith(`${idUnderscore}-`) ||
      cp.startsWith(`${nameNorm}-`)
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
  const latestDeploy = Array.isArray(fresh.deploy_logs) ? fresh.deploy_logs[0] : null;
  const computedStatus: AppStack['status'] = hasRunningContainer
    ? 'running'
    : latestDeploy && latestDeploy.ok === false
    ? 'failed'
    : 'stopped';

  const mappedServices: ContainerService[] =
    appContainers.length > 0
      ? appContainers.map((c: any, sIdx: number) => ({
          id: c.ID || `svc-${fresh.id}-${sIdx}`,
          name: c.Name || `${fresh.name}_${sIdx}`,
          service: c.ComposeService || (Array.isArray(fresh.services) && fresh.services[sIdx]) || 'app',
          image: c.Image || `${fresh.name}:latest`,
          state: ((c.State || '').toLowerCase() === 'running'
            ? 'running'
            : 'exited') as any,
          uptime: c.Status || 'Active',
          cpuPct: 0,
          memUsedMB: 0,
          memLimitMB: 1024,
          ports: c.Ports || '',
        }))
      : Array.isArray(fresh.services)
      ? fresh.services.map((s: string, sIdx: number) => ({
          id: `svc-${fresh.id}-${sIdx}`,
          name: `${fresh.name}_${s}_1`,
          service: s,
          image: `${fresh.name}:latest`,
          state: 'exited' as const,
          uptime: 'Stopped',
          cpuPct: 0,
          memUsedMB: 0,
          memLimitMB: 1024,
          ports: '',
        }))
      : [];

  const mappedDeployments: DeploymentRecord[] = Array.isArray(fresh.deploy_logs)
    ? fresh.deploy_logs.map((dl: any) => ({
        id: `dep-${dl.id}`,
        commitSha: dl.commit_sha || 'latest',
        commitMessage: dl.action || 'Deploy',
        branch: fresh.git_config?.branch || app.gitBranch || 'main',
        trigger: 'manual-compose' as const,
        author: 'admin',
        status: dl.ok ? ('ready' as const) : ('failed' as const),
        durationSec: 15,
        startedAt: dl.created_at,
        slot: dl.slot,
        rawOutput: dl.output || '',
        buildSteps: [],
      }))
    : app.deployments;

  const mappedDomains: DomainRoute[] = Array.isArray(fresh.domains)
    ? fresh.domains.map((d: any) => ({
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
        gzip: true,
        createdAt: d.created_at || 'Active',
      }))
    : app.domains;

  const resolvedPrimaryDomain = (() => {
    const pinned = mappedDomains.find((d) => d.isPrimary);
    if (pinned?.domain) return pinned.domain;
    const custom = mappedDomains.find(
      (d) =>
        d.domain &&
        !d.domain.toLowerCase().endsWith('.sslip.io') &&
        !d.domain.toLowerCase().endsWith('.nip.io')
    );
    if (custom?.domain) return custom.domain;
    return mappedDomains[0]?.domain || '';
  })();

  return {
    status: computedStatus,
    activeSlot: fresh.active_slot || app.activeSlot,
    standbySlot: fresh.standby_slot ?? app.standbySlot,
    pinnedSlot: fresh.pinned_slot ?? '',
    slotImages,
    domains: mappedDomains,
    primaryDomain: resolvedPrimaryDomain,
    preview: fresh.preview,
    previewHostBase: fresh.preview_host_base || '',
    ports: Array.isArray(fresh.ports) ? fresh.ports : [],
    services: mappedServices,
    deployments: mappedDeployments,
    updatedAt: fresh.updated_at || 'Just now',
  };
}
