import React, { useEffect, useMemo, useState } from 'react';
import {
  RotateCw,
  HardDrive,
  Globe,
  ExternalLink,
  FileCode2,
  Lock,
  Terminal,
  Play,
  Square,
  CheckCircle2,
  Copy,
  Download,
  Zap,
  Server,
  Search,
  AlertTriangle,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { ShadcnSwitch, StatusBadge } from '../../components/ui/Primitives';
import { CodeEditorBox } from '../../components/ui/CodeEditorBox';
import { backendApi } from '../../lib/backendApi';

interface CaddyLogLine {
  raw: string;
  ts: string;
  msg: string;
  level: 'error' | 'warn' | 'info' | 'default';
}

function parseCaddyLogLine(line: string): CaddyLogLine {
  const raw = String(line || '');
  const trimmed = raw.trim();
  const braceIdx = trimmed.indexOf('{');
  if (braceIdx >= 0) {
    try {
      const obj = JSON.parse(trimmed.slice(braceIdx)) as {
        level?: string;
        msg?: string;
        message?: string;
        logger?: string;
        address?: string;
        error?: string;
        ts?: number | string;
      };
      const lvlRaw = String(obj.level || 'info').toLowerCase();
      const level: CaddyLogLine['level'] =
        lvlRaw === 'error' || lvlRaw === 'fatal' || lvlRaw === 'panic'
          ? 'error'
          : lvlRaw === 'warn' || lvlRaw === 'warning'
            ? 'warn'
            : lvlRaw === 'info'
              ? 'info'
              : 'default';
      let msg = obj.msg || obj.message || trimmed;
      const meta: string[] = [];
      if (obj.logger) meta.push(`logger=${obj.logger}`);
      if (obj.address) meta.push(`address=${obj.address}`);
      if (obj.error) meta.push(`error=${obj.error}`);
      if (meta.length > 0) msg += ` [${meta.join(' ')}]`;
      const ts =
        typeof obj.ts === 'number'
          ? new Date(obj.ts * 1000).toISOString()
          : typeof obj.ts === 'string'
            ? obj.ts
            : '';
      return { raw, ts, msg, level };
    } catch {
      // fall through
    }
  }
  const lower = raw.toLowerCase();
  const level: CaddyLogLine['level'] = /\b(error|fatal|panic|exception)\b/.test(lower)
    ? 'error'
    : /\b(warn|warning)\b/.test(lower)
      ? 'warn'
      : /\b(info|started|listening|serving|certificate|tls|acme|issued)\b/.test(lower)
        ? 'info'
        : 'default';
  return { raw, ts: '', msg: raw, level };
}

export const CaddyProxyView: React.FC<{
  apps: AppStack[];
  caddyInfo?: {
    up: boolean;
    status: string;
    config?: Record<string, string>;
  } | null;
  onRefreshCaddy?: () => Promise<void>;
}> = ({ apps, caddyInfo, onRefreshCaddy }) => {
  // 1. Panel Domain card (POST /nextdeploy/panel + GET /nextdeploy/apply-status)
  const [panelDomain, setPanelDomain] = useState('panel.nextdeploy.sh');
  const [panelEnableHttps, setPanelEnableHttps] = useState(true);
  const [panelEnableWww, setPanelEnableWww] = useState(false);
  const [applyBanner, setApplyBanner] = useState<{
    active: boolean;
    done: boolean;
    succeeded: boolean;
    status: string;
  } | null>(null);

  // 2. Caddy Shared Volumes card (POST /nextdeploy/shared-volumes)
  const [sharedMountPrefix, setSharedMountPrefix] = useState('/mnt/shared');
  const allDockerVolumes = useMemo(() => {
    const set = new Set<string>([
      'commerce_core_staticfiles',
      'commerce_core_media',
      'studio_web_assets',
      'postgres_primary_data',
      'redis_session_data',
    ]);
    apps.forEach((a) => a.volumes.forEach((v) => set.add(v.name)));
    return Array.from(set);
  }, [apps]);
  const [selectedSharedVolumes, setSelectedSharedVolumes] = useState<string[]>([
    'commerce_core_staticfiles',
    'commerce_core_media',
  ]);
  const [volumeFilter, setVolumeFilter] = useState('');

  // 3. Proxy Control card (POST /caddy/config & POST /caddy/container)
  const [caddyAdminApi, setCaddyAdminApi] = useState(
    caddyInfo?.config?.admin_api || 'http://caddy:2019'
  );
  const [acmeEmail, setAcmeEmail] = useState(
    caddyInfo?.config?.email || ''
  );
  const [caddyImage, setCaddyImage] = useState(
    caddyInfo?.config?.caddy_image || 'lucaslorentz/caddy-docker-proxy:ci-alpine'
  );
  const [caddyContainerRunning, setCaddyContainerRunning] = useState(
    caddyInfo?.up !== false
  );

  useEffect(() => {
    if (caddyInfo) {
      if (caddyInfo.config?.admin_api) setCaddyAdminApi(caddyInfo.config.admin_api);
      if (caddyInfo.config?.email) setAcmeEmail(caddyInfo.config.email);
      if (caddyInfo.config?.caddy_image) setCaddyImage(caddyInfo.config.caddy_image);
      setCaddyContainerRunning(caddyInfo.up !== false);
    }
  }, [caddyInfo]);

  // 4. Root Stack Compose & Live Config Tab
  const [codeTab, setCodeTab] = useState<'root_compose' | 'live_config'>('root_compose');

  // 5. Caddy Container Logs (GET /caddy/logs?tail=300)
  const [logLines, setLogLines] = useState<string[]>([
    '{"level":"info","ts":1747240800,"logger":"admin","msg":"admin endpoint started","address":"tcp/0.0.0.0:2019"}',
    '{"level":"info","ts":1747240801,"logger":"tls.cache.maintenance","msg":"started background certificate maintenance"}',
    '{"level":"info","ts":1747240802,"logger":"http.auto_https","msg":"enabling automatic HTTP->HTTPS redirects","server_name":"srv0"}',
    '{"level":"info","ts":1747240804,"logger":"docker-proxy","msg":"Running caddy proxy server"}',
    '{"level":"warn","ts":1747240812,"logger":"tls","msg":"stapling OCSP: no OCSP stapling for [staging.nextdeploy.sh]"}',
  ]);
  const [logLevelFilter, setLogLevelFilter] = useState<'all' | 'error' | 'warn' | 'info'>('all');
  const [showTimestamps, setShowTimestamps] = useState(true);
  const [isLoadingLogs, setIsLoadingLogs] = useState(false);

  const loadCaddyLogs = async () => {
    setIsLoadingLogs(true);
    const res = await backendApi.caddyLogs(300);
    setIsLoadingLogs(false);
    if (res.ok && Array.isArray(res.lines) && res.lines.length > 0) {
      setLogLines(res.lines);
    }
  };

  useEffect(() => {
    void loadCaddyLogs();
  }, []);

  const pollApplyStatus = () => {
    setApplyBanner({
      active: true,
      done: false,
      succeeded: false,
      status: 'Applying Root Stack Compose & reloading Caddy proxy...',
    });
    let attempts = 0;
    const timer = setInterval(async () => {
      attempts += 1;
      const res = await backendApi.panelApplyStatus();
      if (res.ok && res.data) {
        const done = Boolean(res.data.done || res.data.running === false);
        const succeeded = res.data.succeeded !== false;
        setApplyBanner({
          active: !done,
          done,
          succeeded,
          status:
            res.data.status ||
            (done ? 'Root stack compose applied.' : 'Updating root stack...'),
        });
        if (done || attempts >= 8) {
          clearInterval(timer);
        }
      } else if (attempts >= 2) {
        setApplyBanner({
          active: false,
          done: true,
          succeeded: true,
          status: 'Applied successfully.',
        });
        clearInterval(timer);
      }
    }, 1000);
  };

  const handleSavePanelDomain = async (e: React.FormEvent) => {
    e.preventDefault();
    await backendApi.savePanelConfig({
      panel_domain: panelDomain.trim(),
      panel_enable_https: panelEnableHttps,
      panel_enable_www: panelEnableWww,
    });
    pollApplyStatus();
    toast.success(`Saved Panel Domain (${panelDomain.trim() || 'cleared'})`);
  };

  const handleSaveSharedVolumes = async (e: React.FormEvent) => {
    e.preventDefault();
    await backendApi.saveSharedVolumes(
      selectedSharedVolumes,
      sharedMountPrefix.trim() || '/mnt/shared'
    );
    pollApplyStatus();
    toast.success(
      `Mounted ${selectedSharedVolumes.length} shared volume(s) under ${sharedMountPrefix.trim() || '/mnt/shared'}`
    );
  };

  const handleSaveProxySettings = async (e: React.FormEvent) => {
    e.preventDefault();
    await backendApi.caddySaveConfig({
      admin_api: caddyAdminApi.trim(),
      email: acmeEmail.trim(),
      caddy_image: caddyImage.trim(),
    });
    toast.success('Saved Caddy proxy configuration');
  };

  const handleContainerLifecycle = async (
    action: 'start' | 'force_start' | 'restart' | 'stop'
  ) => {
    await backendApi.caddyContainerAction(action);
    if (action === 'stop') {
      setCaddyContainerRunning(false);
      toast.success('Stopped Caddy container');
    } else {
      setCaddyContainerRunning(true);
      toast.success(
        action === 'force_start'
          ? 'Freed ports 80/443 and started Caddy container'
          : action === 'restart'
            ? 'Restarted Caddy container'
            : 'Started Caddy container'
      );
    }
  };

  const rootComposeYaml = useMemo(() => {
    const sharedVolLines = selectedSharedVolumes
      .map(
        (v) =>
          `      - nd_shared_${v}:${(sharedMountPrefix || '/mnt/shared').replace(/\/+$/, '')}/${v}:ro`
      )
      .join('\n');
    const externalVolDefs = selectedSharedVolumes
      .map((v) => `  nd_shared_${v}:\n    external: true\n    name: ${v}`)
      .join('\n');
    const hostList = panelDomain.trim()
      ? panelEnableWww
        ? `${panelDomain.trim()} www.${panelDomain.trim()}`
        : panelDomain.trim()
      : ':80';

    return `services:
  caddy:
    image: ${caddyImage || 'lucaslorentz/caddy-docker-proxy:ci-alpine'}
    container_name: nextdeploy_caddy
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    environment:
      - CADDY_INGRESS_NETWORKS=nextdeploy_net
      - CADDY_DOCKER_CADDYFILE_PATH=/etc/caddy/Caddyfile
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - caddy_data:/data
      - caddy_config:/config
${sharedVolLines ? sharedVolLines + '\n' : ''}    labels:
      caddy.email: "${acmeEmail || 'admin@example.com'}"
      caddy_0: "${hostList}"
      caddy_0.reverse_proxy: "nextdeploy_panel:3000"
      ${!panelEnableHttps ? 'caddy_0.tls: "internal"' : 'caddy_0.encode: "zstd gzip"'}
networks:
  default:
    name: nextdeploy_net
    external: true
volumes:
  caddy_data:
  caddy_config:
${externalVolDefs}`;
  }, [
    acmeEmail,
    caddyImage,
    panelDomain,
    panelEnableHttps,
    panelEnableWww,
    selectedSharedVolumes,
    sharedMountPrefix,
  ]);

  const liveConfigJson = useMemo(() => {
    const routes = apps.flatMap((a) =>
      a.domains.map((d) => ({
        match: [{ host: [d.domain] }],
        handle: [
          {
            handler: 'reverse_proxy',
            upstreams: [{ dial: `${a.slug}-${d.targetService}:${d.targetPort}` }],
          },
        ],
        terminal: true,
      }))
    );
    return JSON.stringify(
      {
        admin: { listen: caddyAdminApi },
        apps: {
          http: {
            servers: {
              srv0: {
                listen: [':443', ':80'],
                routes,
              },
            },
          },
          tls: {
            automation: {
              policies: [{ issuers: [{ module: 'acme', email: acmeEmail }] }],
            },
          },
        },
      },
      null,
      2
    );
  }, [acmeEmail, apps, caddyAdminApi]);

  const parsedLogs = useMemo(
    () =>
      logLines.map(parseCaddyLogLine).filter((line) => {
        if (logLevelFilter === 'all') return true;
        return line.level === logLevelFilter;
      }),
    [logLines, logLevelFilter]
  );

  const filteredVolumes = allDockerVolumes.filter(
    (v) => !volumeFilter.trim() || v.toLowerCase().includes(volumeFilter.trim().toLowerCase())
  );

  const toggleVolumeSelection = (volName: string) => {
    setSelectedSharedVolumes((prev) =>
      prev.includes(volName) ? prev.filter((item) => item !== volName) : [...prev, volName]
    );
  };

  const downloadCaddyLogs = () => {
    const content = parsedLogs.map((l) => l.raw).join('\n');
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'caddy-logs.txt';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setTimeout(() => URL.revokeObjectURL(url), 100);
  };

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Globe className="h-4 w-4 text-orange-400" />
          </div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-semibold tracking-tight text-zinc-100">
              NextDeploy &amp; Caddy Proxy
            </h1>
            <StatusBadge
              status={caddyContainerRunning ? 'running' : 'stopped'}
              label={caddyContainerRunning ? 'nextdeploy_caddy running' : 'stopped'}
            />
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => void handleContainerLifecycle('start')}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 cursor-pointer"
          >
            <Play className="h-3.5 w-3.5" />
            Start
          </button>
          <button
            type="button"
            onClick={() => void handleContainerLifecycle('force_start')}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-300 hover:bg-amber-500/20 cursor-pointer"
            title="Stop conflicting host nginx/apache on ports 80/443 and start Caddy"
          >
            <Zap className="h-3.5 w-3.5" />
            Force Start (80/443)
          </button>
          <button
            type="button"
            onClick={() => void handleContainerLifecycle('restart')}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
            Restart
          </button>
          <button
            type="button"
            onClick={() => void handleContainerLifecycle('stop')}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-rose-500/25 bg-rose-500/10 px-3 text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
          >
            <Square className="h-3.5 w-3.5" />
            Stop
          </button>
        </div>
      </div>

      {/* Live Root Stack Apply Progress Banner (/nextdeploy/apply-status) */}
      {applyBanner && (
        <div
          className={`flex items-center justify-between gap-3 rounded-xl border px-4 py-3 text-xs ${
            applyBanner.active
              ? 'border-orange-500/30 bg-orange-500/10 text-orange-200'
              : applyBanner.succeeded
                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200'
                : 'border-rose-500/30 bg-rose-500/10 text-rose-200'
          }`}
        >
          <div className="flex items-center gap-2 font-mono">
            {applyBanner.active ? (
              <RotateCw className="h-3.5 w-3.5 animate-spin text-orange-400 shrink-0" />
            ) : (
              <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
            )}
            <span>{applyBanner.status}</span>
          </div>
          {applyBanner.done && (
            <button
              type="button"
              onClick={() => setApplyBanner(null)}
              className="text-[11px] underline opacity-80 hover:opacity-100 cursor-pointer"
            >
              Dismiss
            </button>
          )}
        </div>
      )}

      {/* Admin API Unreachable & Network Diagnostic Banner */}
      {caddyInfo && !caddyInfo.up && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-rose-500/30 bg-rose-500/10 p-4 text-xs text-rose-200">
          <div className="flex items-start gap-3">
            <AlertTriangle className="h-5 w-5 text-rose-400 shrink-0 mt-0.5" />
            <div>
              <div className="font-semibold text-rose-100">
                Caddy Admin API Unreachable ({caddyAdminApi})
              </div>
              <div className="mt-1 font-mono text-[11px] text-rose-300/80">{caddyInfo.status}</div>
              <div className="mt-1.5 text-zinc-400">
                This occurs when the Caddy container is running but disconnected from the NextDeploy network. Automatic network healing runs in the background.
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={async () => {
                toast.loading('Reconnecting Caddy to NextDeploy network...');
                await backendApi.caddyContainerAction('reconcile_network');
                toast.dismiss();
                toast.success('Network reconnected. Refreshing status...');
                if (onRefreshCaddy) await onRefreshCaddy();
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-rose-500/20 px-3 text-xs font-semibold text-rose-200 hover:bg-rose-500/30 cursor-pointer"
            >
              <Zap className="h-3.5 w-3.5" />
              Auto-Connect Network
            </button>
            <button
              type="button"
              onClick={async () => {
                await handleContainerLifecycle('start');
                if (onRefreshCaddy) await onRefreshCaddy();
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.1] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
            >
              <RotateCw className="h-3.5 w-3.5" />
              Restart Proxy
            </button>
          </div>
        </div>
      )}

      {/* Row 1: Panel Domain Card + Caddy Shared Volumes Card */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Card 1: Panel Domain (POST /nextdeploy/panel) */}
        <form
          onSubmit={handleSavePanelDomain}
          className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 flex flex-col justify-between gap-4"
        >
          <div className="space-y-4">
            <div className="flex items-center justify-between gap-2 border-b border-white/[0.06] pb-3">
              <div className="flex items-center gap-2.5">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-orange-500/10 ring-1 ring-orange-500/20">
                  <Lock className="h-4 w-4 text-orange-400" />
                </div>
                <div>
                  <h2 className="text-sm font-semibold text-zinc-100">Panel Domain</h2>
                  <p className="font-mono text-[11px] text-zinc-500">POST /nextdeploy/panel</p>
                </div>
              </div>
              {panelDomain.trim() && (
                <a
                  href={`${panelEnableHttps ? 'https' : 'http'}://${panelDomain.trim()}`}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 font-mono text-xs text-emerald-400 hover:underline"
                >
                  {panelDomain.trim()}
                  <ExternalLink className="h-3 w-3" />
                </a>
              )}
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                Domain (leave empty to unbind)
              </label>
              <input
                type="text"
                value={panelDomain}
                onChange={(e) => setPanelDomain(e.target.value)}
                placeholder="panel.example.com"
                className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>

            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
              <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2.5">
                <div>
                  <div className="text-xs font-medium text-zinc-200">Automatic HTTPS</div>
                  <div className="font-mono text-[10px] text-zinc-500">panel_enable_https</div>
                </div>
                <ShadcnSwitch checked={panelEnableHttps} onChange={setPanelEnableHttps} />
              </div>

              <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2.5">
                <div>
                  <div className="text-xs font-medium text-zinc-200">Also route www</div>
                  <div className="font-mono text-[10px] text-zinc-500">panel_enable_www</div>
                </div>
                <ShadcnSwitch checked={panelEnableWww} onChange={setPanelEnableWww} />
              </div>
            </div>
          </div>

          <div className="flex justify-end border-t border-white/[0.06] pt-3">
            <button
              type="submit"
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              Save Panel Domain
            </button>
          </div>
        </form>

        {/* Card 2: Caddy Shared Volumes (POST /nextdeploy/shared-volumes) */}
        <form
          onSubmit={handleSaveSharedVolumes}
          className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 flex flex-col justify-between gap-4"
        >
          <div className="space-y-3.5">
            <div className="flex items-center justify-between gap-2 border-b border-white/[0.06] pb-3">
              <div className="flex items-center gap-2.5">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-sky-500/10 ring-1 ring-sky-500/20">
                  <HardDrive className="h-4 w-4 text-sky-400" />
                </div>
                <div>
                  <h2 className="text-sm font-semibold text-zinc-100">Caddy Shared Volumes</h2>
                  <p className="font-mono text-[11px] text-zinc-500">
                    {selectedSharedVolumes.length} of {allDockerVolumes.length} mounted read-only
                  </p>
                </div>
              </div>
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
                <input
                  type="text"
                  value={volumeFilter}
                  onChange={(e) => setVolumeFilter(e.target.value)}
                  placeholder="Filter volumes..."
                  className="h-7 w-36 rounded-lg border border-white/[0.08] bg-[#09090b] pl-7 pr-2 text-[11px] text-zinc-200 focus:border-orange-500 focus:outline-none"
                />
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs font-medium text-zinc-400">
                Mount prefix (caddy_shared_mount_prefix)
              </label>
              <input
                type="text"
                value={sharedMountPrefix}
                onChange={(e) => setSharedMountPrefix(e.target.value)}
                placeholder="/mnt/shared"
                className="h-8 w-full max-w-xs rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
              />
            </div>

            <div className="max-h-36 overflow-y-auto rounded-lg border border-white/[0.06] bg-[#09090b] p-2">
              <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                {filteredVolumes.map((vol) => {
                  const checked = selectedSharedVolumes.includes(vol);
                  return (
                    <label
                      key={vol}
                      className={`flex cursor-pointer items-center gap-2 rounded-md px-2.5 py-1.5 font-mono text-[11px] transition-colors ${
                        checked
                          ? 'bg-orange-500/10 text-orange-300 ring-1 ring-orange-500/25'
                          : 'text-zinc-300 hover:bg-white/[0.04]'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleVolumeSelection(vol)}
                        className="rounded border-zinc-700 accent-orange-500"
                      />
                      <span className="truncate">{vol}</span>
                    </label>
                  );
                })}
              </div>
            </div>
          </div>

          <div className="flex justify-end border-t border-white/[0.06] pt-3">
            <button
              type="submit"
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              Save Caddy Volumes
            </button>
          </div>
        </form>
      </div>

      {/* Row 2: Proxy Control Settings (POST /caddy/config) */}
      <form
        onSubmit={handleSaveProxySettings}
        className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-5 space-y-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.06] pb-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-zinc-900 ring-1 ring-white/[0.08]">
              <Server className="h-4 w-4 text-amber-400" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-zinc-100">Proxy Control</h2>
              <p className="font-mono text-[11px] text-zinc-500">POST /caddy/config</p>
            </div>
          </div>
          <button
            type="submit"
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-4 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <CheckCircle2 className="h-3.5 w-3.5" />
            Save Proxy Settings
          </button>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div>
            <label className="mb-1.5 block text-xs font-medium text-zinc-400">
              Admin API URL (admin_api)
            </label>
            <input
              type="text"
              value={caddyAdminApi}
              onChange={(e) => setCaddyAdminApi(e.target.value)}
              placeholder="http://caddy:2019"
              className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div>
            <label className="mb-1.5 block text-xs font-medium text-zinc-400">
              ACME Email (email)
            </label>
            <input
              type="email"
              value={acmeEmail}
              onChange={(e) => setAcmeEmail(e.target.value)}
              placeholder="you@example.com"
              className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
          <div>
            <label className="mb-1.5 block text-xs font-medium text-zinc-400">
              Caddy Image (caddy_image)
            </label>
            <input
              type="text"
              value={caddyImage}
              onChange={(e) => setCaddyImage(e.target.value)}
              placeholder="lucaslorentz/caddy-docker-proxy:ci-alpine"
              className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
        </div>
      </form>

      {/* Row 3: Root Stack Compose & Live Running Config Viewer */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/50 px-4 py-3">
          <div className="flex items-center gap-1 rounded-lg border border-white/[0.08] bg-[#09090b] p-0.5">
            <button
              type="button"
              onClick={() => setCodeTab('root_compose')}
              className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                codeTab === 'root_compose'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <FileCode2 className="h-3.5 w-3.5 text-orange-400" />
              Root Stack Compose
            </button>
            <button
              type="button"
              onClick={() => setCodeTab('live_config')}
              className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium cursor-pointer ${
                codeTab === 'live_config'
                  ? 'bg-zinc-800 text-zinc-100'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Globe className="h-3.5 w-3.5 text-emerald-400" />
              Live Running Config
            </button>
          </div>

          <button
            type="button"
            onClick={() => {
              const text = codeTab === 'root_compose' ? rootComposeYaml : liveConfigJson;
              navigator.clipboard?.writeText(text);
              toast.success('Copied to clipboard');
            }}
            className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
          >
            <Copy className="h-3 w-3" />
            Copy
          </button>
        </div>

        <div className="p-3">
          <CodeEditorBox
            filename={codeTab === 'root_compose' ? 'docker-compose.yml' : 'caddy-live-config.json'}
            language={codeTab === 'root_compose' ? 'yaml' : 'json'}
            value={codeTab === 'root_compose' ? rootComposeYaml : liveConfigJson}
            onChange={() => {}}
            readOnly
            height={250}
          />
        </div>
      </div>

      {/* Row 4: Caddy Container Logs (GET /caddy/logs?tail=300) */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/50 px-4 py-3">
          <div className="flex items-center gap-2">
            <Terminal className="h-4 w-4 text-orange-400" />
            <h2 className="text-xs font-semibold uppercase tracking-wider text-zinc-200">
              Caddy Container Logs
            </h2>
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            {(
              [
                { id: 'all', label: 'All' },
                { id: 'error', label: 'Error' },
                { id: 'warn', label: 'Warn' },
                { id: 'info', label: 'Info' },
              ] as const
            ).map((f) => (
              <button
                key={f.id}
                type="button"
                onClick={() => setLogLevelFilter(f.id)}
                className={`rounded-md px-2.5 py-1 text-[11px] font-medium cursor-pointer ${
                  logLevelFilter === f.id
                    ? 'bg-orange-500/20 text-orange-300 ring-1 ring-orange-500/30'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {f.label}
              </button>
            ))}

            <button
              type="button"
              onClick={() => setShowTimestamps((v) => !v)}
              className="rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
            >
              {showTimestamps ? 'Hide TS' : 'Show TS'}
            </button>

            <button
              type="button"
              onClick={downloadCaddyLogs}
              className="inline-flex items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
            >
              <Download className="h-3 w-3" />
              Download
            </button>

            <button
              type="button"
              onClick={() => void loadCaddyLogs()}
              className="inline-flex items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
            >
              <RotateCw className={`h-3 w-3 ${isLoadingLogs ? 'animate-spin' : ''}`} />
              Refresh
            </button>
          </div>
        </div>

        <div className="max-h-72 overflow-y-auto bg-[#070709] p-3.5 font-mono text-xs space-y-1">
          {parsedLogs.length === 0 ? (
            <div className="text-zinc-500">No log output matching filter.</div>
          ) : (
            parsedLogs.map((l, idx) => (
              <div key={idx} className="flex items-start gap-2 leading-relaxed">
                {showTimestamps && l.ts && (
                  <span className="select-none text-zinc-500 shrink-0">{l.ts}</span>
                )}
                {l.level === 'error' && (
                  <span className="rounded bg-rose-500/20 px-1.5 py-0.2 text-[10px] font-semibold text-rose-300 shrink-0">
                    ERROR
                  </span>
                )}
                {l.level === 'warn' && (
                  <span className="rounded bg-amber-500/20 px-1.5 py-0.2 text-[10px] font-semibold text-amber-300 shrink-0">
                    WARN
                  </span>
                )}
                {l.level === 'info' && (
                  <span className="rounded bg-orange-500/20 px-1.5 py-0.2 text-[10px] font-semibold text-orange-300 shrink-0">
                    INFO
                  </span>
                )}
                <span className="text-zinc-200 break-all">{l.msg}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
};
