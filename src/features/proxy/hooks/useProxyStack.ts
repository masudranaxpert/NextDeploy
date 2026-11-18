import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { backendApi, getServerPublicIp } from '../../../lib/backendApi';
import { normalizeProxyEngine, resolveCapabilities } from '../../../lib/api/proxyApi';
import type { ProxyBootstrapInfo } from '../../../lib/api/http';
import type { ApplyBannerState } from '../components/ProxyHeaderBar';
import type { ProxyContainerActionType } from '../components/ProxyControlCard';

export function useProxyStack(
  apps: AppStack[],
  dockerVolumes: string[],
  caddyInfo?: ProxyBootstrapInfo | null,
  onRefreshCaddy?: () => Promise<void>
) {
  const [panelDomain, setPanelDomain] = useState<string>(caddyInfo?.panel_domain ?? '');
  const [panelEnableHttps, setPanelEnableHttps] = useState<boolean>(caddyInfo?.panel_enable_https ?? true);
  const [panelEnableWww, setPanelEnableWww] = useState<boolean>(caddyInfo?.panel_enable_www ?? false);
  const [sharedMountPrefix, setSharedMountPrefix] = useState<string>(caddyInfo?.shared_mount_prefix ?? '/mnt/shared');
  const [selectedSharedVolumes, setSelectedSharedVolumes] = useState<string[]>(
    Array.isArray(caddyInfo?.shared_volumes) ? caddyInfo.shared_volumes.filter(Boolean) : []
  );

  const [engine, setEngine] = useState<string>(caddyInfo?.engine || 'caddy');
  const [version, setVersion] = useState<string>(caddyInfo?.version || '');
  const [experimental, setExperimental] = useState<boolean>(Boolean(caddyInfo?.experimental));
  const [running, setRunning] = useState<boolean>(caddyInfo?.running ?? caddyInfo?.up ?? false);
  const [capabilitiesRaw, setCapabilitiesRaw] = useState<Record<string, boolean> | undefined>(
    caddyInfo?.capabilities
  );
  const [lastApply, setLastApply] = useState<{ generation?: string; status?: string; error?: string } | null>(null);

  const [adminApi, setAdminApi] = useState<string>(caddyInfo?.config?.admin_api || 'off');
  const [acmeEmail, setAcmeEmail] = useState<string>(caddyInfo?.config?.email || '');
  const [proxyImage, setProxyImage] = useState<string>(
    caddyInfo?.config?.proxy_image ||
      caddyInfo?.config?.caddy_image ||
      'lucaslorentz/caddy-docker-proxy:2.9-alpine'
  );

  const [liveConfig, setLiveConfig] = useState('');
  const [rootCompose, setRootCompose] = useState('');
  const [overviewLoading, setOverviewLoading] = useState(true);
  const [overviewError, setOverviewError] = useState<string | null>(null);

  const [logLines, setLogLines] = useState<string[]>([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsError, setLogsError] = useState<string | null>(null);

  const [savingPanel, setSavingPanel] = useState(false);
  const [savingVolumes, setSavingVolumes] = useState(false);
  const [savingSettings, setSavingSettings] = useState(false);
  const [actionPending, setActionPending] = useState<ProxyContainerActionType | null>(null);
  const [applyBanner, setApplyBanner] = useState<ApplyBannerState>({ status: 'idle', message: '' });
  const pollTimerRef = useRef<number | null>(null);

  const serverIp = useMemo(() => {
    const cached = getServerPublicIp();
    if (cached) return cached;
    return typeof window !== 'undefined' ? window.location.hostname : '';
  }, []);

  const allDockerVolumes = useMemo(() => {
    const set = new Set<string>(dockerVolumes.filter(Boolean));
    for (const a of apps) {
      for (const v of a.volumes || []) {
        const name = typeof v === 'string' ? v : v?.name;
        if (name) set.add(name);
      }
    }
    return Array.from(set).sort();
  }, [apps, dockerVolumes]);

  useEffect(() => {
    if (!caddyInfo) return;
    if (typeof caddyInfo.panel_domain === 'string') setPanelDomain(caddyInfo.panel_domain);
    if (typeof caddyInfo.panel_enable_https === 'boolean') setPanelEnableHttps(caddyInfo.panel_enable_https);
    if (typeof caddyInfo.panel_enable_www === 'boolean') setPanelEnableWww(caddyInfo.panel_enable_www);
    if (typeof caddyInfo.shared_mount_prefix === 'string' && caddyInfo.shared_mount_prefix) {
      setSharedMountPrefix(caddyInfo.shared_mount_prefix);
    }
    if (Array.isArray(caddyInfo.shared_volumes)) {
      setSelectedSharedVolumes(caddyInfo.shared_volumes.filter(Boolean));
    }
    if (caddyInfo.engine) setEngine(caddyInfo.engine);
    if (typeof caddyInfo.running === 'boolean' || typeof caddyInfo.up === 'boolean') {
      setRunning(Boolean(caddyInfo.running ?? caddyInfo.up));
    }
    if (caddyInfo.config?.admin_api) setAdminApi(caddyInfo.config.admin_api);
    if (caddyInfo.config?.email) setAcmeEmail(caddyInfo.config.email);
    if (caddyInfo.config?.proxy_image || caddyInfo.config?.caddy_image) {
      setProxyImage(caddyInfo.config.proxy_image || caddyInfo.config.caddy_image || '');
    }
  }, [caddyInfo]);

  const loadOverview = useCallback(async () => {
    setOverviewLoading(true);
    setOverviewError(null);
    const res = await backendApi.proxyOverview();
    if (res.ok && res.data) {
      const ov = res.data;
      if (ov.engine) setEngine(ov.engine);
      if (ov.version) setVersion(ov.version);
      if (typeof ov.experimental === 'boolean') setExperimental(ov.experimental);
      if (typeof ov.running === 'boolean' || typeof ov.up === 'boolean') {
        setRunning(Boolean(ov.running ?? ov.up));
      }
      if (ov.capabilities) setCapabilitiesRaw(ov.capabilities as Record<string, boolean>);
      if (ov.last_apply !== undefined) setLastApply(ov.last_apply);
      if (typeof ov.live_config === 'string') setLiveConfig(ov.live_config);
      if (typeof ov.root_compose === 'string') setRootCompose(ov.root_compose);
      if (typeof ov.panel_domain === 'string') setPanelDomain(ov.panel_domain);
      if (typeof ov.panel_enable_https === 'boolean') setPanelEnableHttps(ov.panel_enable_https);
      if (typeof ov.panel_enable_www === 'boolean') setPanelEnableWww(ov.panel_enable_www);
      if (typeof ov.shared_mount_prefix === 'string' && ov.shared_mount_prefix) {
        setSharedMountPrefix(ov.shared_mount_prefix);
      }
      if (Array.isArray(ov.shared_volumes)) setSelectedSharedVolumes(ov.shared_volumes.filter(Boolean));
      if (ov.config?.email) setAcmeEmail(ov.config.email);
      if (ov.image || ov.config?.proxy_image || ov.config?.caddy_image) {
        setProxyImage(ov.image || ov.config?.proxy_image || ov.config?.caddy_image || '');
      }
      if (ov.config?.admin_api) setAdminApi(ov.config.admin_api);
    } else {
      setOverviewError(res.error || 'Failed to load proxy overview.');
    }
    setOverviewLoading(false);
  }, []);

  const loadLogs = useCallback(async () => {
    setLogsLoading(true);
    setLogsError(null);
    const res = await backendApi.proxyLogs(300);
    if (res.ok && res.lines) {
      setLogLines(res.lines);
    } else if (res.ok) {
      setLogLines([]);
    } else {
      setLogsError(res.error || 'Failed to fetch reverse proxy logs.');
    }
    setLogsLoading(false);
  }, []);

  useEffect(() => {
    void loadOverview();
    void loadLogs();
    return () => {
      if (pollTimerRef.current) window.clearInterval(pollTimerRef.current);
    };
  }, [loadOverview, loadLogs]);

  const pollApplyStatus = () => {
    if (pollTimerRef.current) window.clearInterval(pollTimerRef.current);
    setApplyBanner({ status: 'running', message: 'Applying reverse proxy stack changes...' });
    let ticks = 0;
    pollTimerRef.current = window.setInterval(async () => {
      ticks += 1;
      const res = await backendApi.panelApplyStatus();
      if (res.ok && res.data) {
        const done = Boolean(res.data.done || res.data.running === false);
        if (done) {
          if (pollTimerRef.current) window.clearInterval(pollTimerRef.current);
          pollTimerRef.current = null;
          const ok = res.data.succeeded !== false;
          setApplyBanner({
            status: ok ? 'success' : 'error',
            message: res.data.message || res.data.status || (ok ? 'Changes applied.' : 'Apply failed.'),
            updatedAt: res.data.updated_at,
          });
          void loadOverview();
          void loadLogs();
          return;
        }
      }
      if (ticks > 30 && pollTimerRef.current) {
        window.clearInterval(pollTimerRef.current);
        pollTimerRef.current = null;
      }
    }, 1500);
  };

  const handleSavePanel = async (e: React.FormEvent) => {
    e.preventDefault();
    setSavingPanel(true);
    const res = await backendApi.savePanelConfig({
      panel_domain: panelDomain.trim(),
      panel_enable_https: panelEnableHttps,
      panel_enable_www: panelEnableWww,
    });
    setSavingPanel(false);
    if (res.ok) {
      toast.success('Panel domain update queued');
      pollApplyStatus();
    } else {
      toast.error(res.error || 'Failed to save panel domain');
    }
  };

  const handleSaveVolumes = async (e: React.FormEvent) => {
    e.preventDefault();
    setSavingVolumes(true);
    const res = await backendApi.saveSharedVolumes({
      caddy_shared_mount_prefix: sharedMountPrefix.trim() || '/mnt/shared',
      caddy_shared_volumes: selectedSharedVolumes,
    });
    setSavingVolumes(false);
    if (res.ok) {
      toast.success('Shared volume mounts updated');
      pollApplyStatus();
    } else {
      toast.error(res.error || 'Failed to save shared volumes');
    }
  };

  const handleSaveSettings = async (e: React.FormEvent) => {
    e.preventDefault();
    setSavingSettings(true);
    const res = await backendApi.proxySaveConfig({
      admin_api: adminApi,
      email: acmeEmail.trim(),
      proxy_image: proxyImage.trim(),
      caddy_image: proxyImage.trim(),
    });
    setSavingSettings(false);
    if (res.ok) {
      toast.success('Reverse proxy settings saved');
      void loadOverview();
    } else {
      toast.error(res.error || 'Failed to save reverse proxy settings');
    }
  };

  const handleLifecycle = async (action: ProxyContainerActionType) => {
    setActionPending(action);
    const res = await backendApi.proxyContainerAction(action);
    setActionPending(null);
    if (res.ok) {
      toast.success(`Proxy action "${action}" completed`);
      void loadOverview();
      void loadLogs();
    } else {
      toast.error(res.error || `Failed to run "${action}"`);
    }
  };

  const normalizedEngine = normalizeProxyEngine(engine);
  const capabilities = resolveCapabilities(normalizedEngine, capabilitiesRaw);

  return {
    panelDomain,
    setPanelDomain,
    panelEnableHttps,
    setPanelEnableHttps,
    panelEnableWww,
    setPanelEnableWww,
    sharedMountPrefix,
    setSharedMountPrefix,
    selectedSharedVolumes,
    setSelectedSharedVolumes,
    normalizedEngine,
    version,
    experimental: experimental || capabilities.experimental,
    running,
    capabilities,
    lastApply,
    adminApi,
    setAdminApi,
    acmeEmail,
    setAcmeEmail,
    proxyImage,
    setProxyImage,
    liveConfig,
    rootCompose,
    overviewLoading,
    overviewError,
    logLines,
    logsLoading,
    logsError,
    savingPanel,
    savingVolumes,
    savingSettings,
    actionPending,
    applyBanner,
    serverIp,
    allDockerVolumes,
    loadOverview,
    loadLogs,
    handleSavePanel,
    handleSaveVolumes,
    handleSaveSettings,
    handleLifecycle,
  };
}
