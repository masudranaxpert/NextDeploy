import React, { useState, useMemo, useEffect } from 'react';
import {
  Globe,
  Plus,
  Edit2,
  Trash2,
  CheckCircle2,
  AlertCircle,
  Lock,
  ExternalLink,
  Code2,
  RefreshCw,
  FolderOpen,
  Image as ImageIcon,
  ShieldCheck,
  X,
  Copy,
  Check,
  Server,
  Pin,
  Layers,
  Network,
  Eye,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, DomainRoute, PreviewConfig } from '../../../types/nextdeploy';
import { backendApi, formatSlotPreviewDomain, formatWildcardDomain } from '../../../lib/backendApi';
import { ActionMenu, ActionMenuItem } from '../../../components/ui/Primitives';

interface ProjectDomainsTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
}

const isWildcardDomain = (domain: string): boolean => {
  const d = (domain || '').trim().toLowerCase();
  return d.endsWith('.sslip.io') || d.endsWith('.nip.io');
};

const detectPortFromServiceString = (portsStr?: string): number | null => {
  if (!portsStr) return null;
  const arrowMatch = portsStr.match(/->(\d+)/);
  if (arrowMatch) return parseInt(arrowMatch[1], 10);
  const numMatch = portsStr.match(/(\d+)\/(?:tcp|udp)/i) || portsStr.match(/\b(\d{2,5})\b/);
  if (numMatch) return parseInt(numMatch[1], 10);
  return null;
};

const mapBackendDomain = (d: any): DomainRoute => ({
  id: String(d.id),
  domain: d.domain,
  targetService: d.service,
  targetPort: d.port || 3000,
  isPrimary: Boolean(d.is_primary),
  tlsProvider: 'LetsEncrypt',
  tlsStatus: 'active',
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
});

const resolvePrimaryDomainName = (domains: DomainRoute[], fallback = ''): string => {
  const pinned = domains.find((d) => d.isPrimary);
  if (pinned?.domain) return pinned.domain;
  const custom = domains.find((d) => d.domain && !isWildcardDomain(d.domain));
  if (custom?.domain) return custom.domain;
  return domains[0]?.domain || fallback;
};

const extractComposeServices = (app: AppStack): { name: string; port: number | null }[] => {
  const map = new Map<string, number | null>();

  // 1. Running / active container services
  for (const s of app.services || []) {
    if (s.service) {
      const port = detectPortFromServiceString(s.ports);
      map.set(s.service, port);
    }
  }

  // 2. Exposed ports
  for (const p of app.ports || []) {
    if (p.service) {
      if (!map.has(p.service) || map.get(p.service) === null) {
        map.set(p.service, p.port || null);
      }
    }
  }

  // 3. Compose file in workspace
  const composeNode = (app.files || []).find((f) =>
    f.type === 'file' && /(?:docker-compose|\bcompose)\.ya?ml$/i.test(f.path)
  );
  if (composeNode?.content) {
    const lines = composeNode.content.split('\n');
    let inServices = false;
    let currentSvc: string | null = null;

    for (const rawLine of lines) {
      const trimmed = rawLine.trim();
      if (!trimmed || trimmed.startsWith('#')) continue;

      if (/^services:\s*$/i.test(rawLine.trimEnd())) {
        inServices = true;
        currentSvc = null;
        continue;
      }
      if (inServices && /^[a-zA-Z0-9_-]+:\s*$/.test(rawLine.trimEnd()) && !/^services:\s*$/i.test(rawLine.trimEnd())) {
        inServices = false;
        currentSvc = null;
        continue;
      }
      if (inServices) {
        const svcMatch = rawLine.match(/^ {2}([a-zA-Z0-9_-]+):\s*$/);
        if (svcMatch) {
          currentSvc = svcMatch[1];
          if (!map.has(currentSvc)) {
            map.set(currentSvc, null);
          }
          continue;
        }

        if (currentSvc && /^\s*-\s*["']?(?:\d+:)?\d+/.test(rawLine)) {
          const m = rawLine.match(/^\s*-\s*["']?(?:(\d+):)?(\d+)(?:\/(?:tcp|udp))?["']?/);
          if (m) {
            const containerPort = parseInt(m[2] || m[1], 10);
            if (containerPort && (!map.has(currentSvc) || map.get(currentSvc) === null)) {
              map.set(currentSvc, containerPort);
            }
          }
        }
      }
    }
  }

  // 4. Existing domain routes
  for (const d of app.domains || []) {
    if (d.targetService) {
      if (!map.has(d.targetService) || map.get(d.targetService) === null) {
        map.set(d.targetService, d.targetPort || null);
      }
    }
  }

  if (map.size === 0) {
    map.set('app', 3000);
  }

  return Array.from(map.entries()).map(([name, port]) => ({ name, port }));
};

export const ProjectDomainsTab: React.FC<ProjectDomainsTabProps> = ({
  app,
  onUpdateApp,
}) => {
  const detectedServices = useMemo(() => extractComposeServices(app), [app]);
  const [showDomainModal, setShowDomainModal] = useState(false);
  const [editingDomainId, setEditingDomainId] = useState<string | null>(null);
  const [domainHost, setDomainHost] = useState('');
  const [domainService, setDomainService] = useState('app');
  const [isCustomDomainService, setIsCustomDomainService] = useState(false);
  const [domainPort, setDomainPort] = useState('3000');
  const [domainIsPrimary, setDomainIsPrimary] = useState(false);
  const [domainHttps, setDomainHttps] = useState(true);
  const [domainWww, setDomainWww] = useState(false);
  const [domainStatic, setDomainStatic] = useState(false);
  const [domainStaticPath, setDomainStaticPath] = useState('/var/www/static');
  const [domainStaticPrefix, setDomainStaticPrefix] = useState('/static/');
  const [domainMedia, setDomainMedia] = useState(false);
  const [domainMediaPath, setDomainMediaPath] = useState('/var/www/media');
  const [domainMediaPrefix, setDomainMediaPrefix] = useState('/media/');
  const [saving, setSaving] = useState(false);
  const [tab, setTab] = useState<'domains' | 'preview' | 'ports'>('domains');
  const [pvProvider, setPvProvider] = useState<PreviewConfig['provider']>(app.preview?.provider || '');
  const [pvBase, setPvBase] = useState(app.preview?.base || '');
  const [pvService, setPvService] = useState(app.preview?.service || '');
  const [isCustomPvService, setIsCustomPvService] = useState(false);
  const [pvPort, setPvPort] = useState(String(app.preview?.port || 3000));
  const [pvSaving, setPvSaving] = useState(false);
  const [pinningId, setPinningId] = useState<string | null>(null);

  useEffect(() => {
    if (!pvService && detectedServices[0]) {
      setPvService(detectedServices[0].name);
      if (detectedServices[0].port) {
        setPvPort(String(detectedServices[0].port));
      }
    }
  }, [detectedServices, pvService]);

  // DNS check state per domain ID
  const [dnsLoadingId, setDnsLoadingId] = useState<string | null>(null);
  const [dnsResults, setDnsResults] = useState<
    Record<
      string,
      {
        domain: string;
        ips: string[];
        cloudflare: boolean;
        internalTLS: boolean;
        ok: boolean;
        error?: string;
      }
    >
  >({});

  // Caddy Labels modal state
  const [caddyModal, setCaddyModal] = useState<{ domain: string; yaml: string } | null>(null);
  const [copiedCaddy, setCopiedCaddy] = useState(false);

  const detectServicePort = (serviceName: string): number | null => {
    const match = detectedServices.find((s) => s.name === serviceName);
    if (match?.port) return match.port;
    const svc = app.services.find((s) => s.service === serviceName) || app.services[0];
    return detectPortFromServiceString(svc?.ports);
  };

  const activePrimaryDomain = resolvePrimaryDomainName(app.domains, app.primaryDomain || '');
  const standbySlotLower = (app.activeSlot || 'blue').toLowerCase() === 'green' ? 'blue' : 'green';

  const applyBackendDomainsResponse = (rawDomains: any[] | undefined, fallbackList: DomainRoute[]) => {
    const nextDomains = Array.isArray(rawDomains)
      ? rawDomains.map(mapBackendDomain)
      : fallbackList;
    onUpdateApp({
      ...app,
      domains: nextDomains,
      primaryDomain: resolvePrimaryDomainName(nextDomains, ''),
    });
  };

  const handleSetPrimary = async (d: DomainRoute) => {
    setPinningId(d.id);
    try {
      const res = await backendApi.domainSetPrimary(app.id, d.id);
      if (!res.ok) {
        toast.error(res.error || 'Failed to set primary domain');
        return;
      }
      const fallback = app.domains.map((item) => ({
        ...item,
        isPrimary: item.id === d.id,
      }));
      applyBackendDomainsResponse(res.data?.domains, fallback);
      toast.success(`Pinned ${d.domain} as Primary Production URL`);
    } catch (err: any) {
      toast.error(err?.message || 'Failed to set primary domain');
    } finally {
      setPinningId(null);
    }
  };

  const handleSavePreview = async () => {
    setPvSaving(true);
    try {
      const res = await backendApi.previewSave(app.id, {
        provider: pvProvider,
        base: pvBase,
        service: pvService.trim(),
        port: parseInt(pvPort, 10) || 3000,
      });
      if (!res.ok) {
        toast.error(res.error || 'Failed to save Preview URL');
        return;
      }
      onUpdateApp({
        ...app,
        preview: {
          provider: pvProvider,
          base: pvBase,
          service: pvService.trim(),
          port: parseInt(pvPort, 10) || 3000,
        },
        previewHostBase: res.data?.host_base || '',
      });
      toast.success(pvProvider ? 'Preview URL saved' : 'Preview URL disabled');
    } finally {
      setPvSaving(false);
    }
  };

  const openAddModal = () => {
    const firstSvc = detectedServices[0];
    const defaultSvc = firstSvc?.name || 'app';
    const detectedPort = firstSvc?.port || 3000;
    const hasCustomDomain = app.domains.some((d) => !isWildcardDomain(d.domain));
    setIsCustomDomainService(false);
    setEditingDomainId(null);
    setDomainHost('');
    setDomainService(defaultSvc);
    setDomainPort(String(detectedPort));
    setDomainIsPrimary(!hasCustomDomain);
    setDomainHttps(true);
    setDomainWww(false);
    setDomainStatic(false);
    setDomainStaticPath('/var/www/static');
    setDomainStaticPrefix('/static/');
    setDomainMedia(false);
    setDomainMediaPath('/var/www/media');
    setDomainMediaPrefix('/media/');
    setShowDomainModal(true);
  };

  const openEditModal = (d: DomainRoute) => {
    const isCustom = !detectedServices.some((s) => s.name === d.targetService);
    setIsCustomDomainService(isCustom);
    setEditingDomainId(d.id);
    setDomainHost(d.domain);
    setDomainService(d.targetService || 'app');
    setDomainPort(String(d.targetPort || 3000));
    setDomainIsPrimary(Boolean(d.isPrimary) || d.domain === activePrimaryDomain);
    setDomainHttps(d.forceHttps !== false);
    setDomainWww(Boolean(d.enableWww));
    setDomainStatic(Boolean(d.serveStatic));
    setDomainStaticPath(d.staticPath || '/var/www/static');
    setDomainStaticPrefix(d.staticUrlPrefix || '/static/');
    setDomainMedia(Boolean(d.serveMedia));
    setDomainMediaPath(d.mediaPath || '/var/www/media');
    setDomainMediaPrefix(d.mediaUrlPrefix || '/media/');
    setShowDomainModal(true);
  };

  const handleSaveDomain = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!domainHost.trim()) return;
    setSaving(true);
    try {
      const cleanDomain = domainHost.trim().toLowerCase().replace(/^https?:\/\//, '');
      const cleanService = domainService.trim() || 'app';
      const detected = detectServicePort(cleanService);
      const cleanPort = parseInt(domainPort, 10) || detected || 3000;

      const payload = {
        domain: cleanDomain,
        service: cleanService,
        port: cleanPort,
        is_primary: domainIsPrimary,
        enable_https: domainHttps,
        enable_www: domainWww,
        serve_static: domainStatic,
        static_path: domainStatic ? domainStaticPath.trim() : '',
        static_url_prefix: domainStatic ? domainStaticPrefix.trim() : '',
        serve_media: domainMedia,
        media_path: domainMedia ? domainMediaPath.trim() : '',
        media_url_prefix: domainMedia ? domainMediaPrefix.trim() : '',
      };

      if (editingDomainId) {
        const res = await backendApi.domainEdit(app.id, editingDomainId, payload);
        if (!res.ok) {
          toast.error(res.error || 'Failed to update domain');
          return;
        }
        const fallback: DomainRoute[] = app.domains.map((d) =>
          d.id === editingDomainId
            ? {
                ...d,
                domain: cleanDomain,
                targetService: cleanService,
                targetPort: cleanPort,
                isPrimary: domainIsPrimary,
                forceHttps: domainHttps,
                enableWww: domainWww,
                serveStatic: domainStatic,
                staticPath: payload.static_path,
                staticUrlPrefix: payload.static_url_prefix,
                serveMedia: domainMedia,
                mediaPath: payload.media_path,
                mediaUrlPrefix: payload.media_url_prefix,
              }
            : domainIsPrimary
            ? { ...d, isPrimary: false }
            : d
        );
        applyBackendDomainsResponse((res.data as any)?.domains, fallback);
        toast.success(`Updated domain ${cleanDomain}`);
      } else {
        const res = await backendApi.domainCreate(app.id, payload);
        if (!res.ok) {
          toast.error(res.error || 'Failed to add domain');
          return;
        }
        const created: DomainRoute = {
          id: String((res.data as any)?.id || `dom-${Date.now()}`),
          domain: cleanDomain,
          targetService: cleanService,
          targetPort: cleanPort,
          isPrimary: domainIsPrimary,
          tlsProvider: 'LetsEncrypt',
          tlsStatus: 'active',
          forceHttps: domainHttps,
          enableWww: domainWww,
          serveStatic: domainStatic,
          staticPath: payload.static_path,
          staticUrlPrefix: payload.static_url_prefix,
          serveMedia: domainMedia,
          mediaPath: payload.media_path,
          mediaUrlPrefix: payload.media_url_prefix,
          gzip: true,
          createdAt: 'Just now',
        };
        const baseList = domainIsPrimary
          ? app.domains.map((d) => ({ ...d, isPrimary: false }))
          : app.domains;
        applyBackendDomainsResponse((res.data as any)?.domains, [...baseList, created]);
        toast.success(`Added domain ${cleanDomain}`);
      }
      setShowDomainModal(false);
    } catch (err: any) {
      toast.error(err?.message || 'Failed to save domain configuration');
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteDomain = async (d: DomainRoute) => {
    try {
      const res = await backendApi.domainDelete(app.id, d.id);
      const fallback = app.domains.filter((item) => item.id !== d.id);
      applyBackendDomainsResponse((res.data as any)?.domains, fallback);
      toast.success(`Removed domain ${d.domain}`);
    } catch (err: any) {
      toast.error(err?.message || 'Failed to delete domain');
    }
  };

  const handleCheckDns = async (d: DomainRoute) => {
    setDnsLoadingId(d.id);
    try {
      const res = await backendApi.domainDnsCheck(app.id, d.id);
      const resolvedIps = Array.isArray(res?.ips) && res.ips.length > 0 ? res.ips : [];
      setDnsResults((prev) => ({
        ...prev,
        [d.id]: {
          domain: res?.domain || d.domain,
          ips: resolvedIps,
          cloudflare: Boolean(res?.cloudflare),
          internalTLS: Boolean(res?.internalTLS),
          ok: res?.ok !== false,
          error: res?.error,
        },
      }));
      toast.success(`DNS checked for ${d.domain}`);
    } catch (err: any) {
      toast.error(err?.message || 'DNS verification failed');
    } finally {
      setDnsLoadingId(null);
    }
  };

  const handleInspectLabels = async (d: DomainRoute) => {
    try {
      const res = await backendApi.domainLabels(app.id, d.id);
      const fallbackYaml = [
        `# Caddy Docker proxy labels for ${d.domain}`,
        `services:`,
        `  ${d.targetService}:`,
        `    labels:`,
        `      caddy: "${d.forceHttps !== false ? '' : 'http://'}${d.domain}${d.enableWww ? `, www.${d.domain}` : ''}"`,
        `      caddy.reverse_proxy: "{{upstreams ${d.targetPort}}}"`,
        ...(d.serveStatic
          ? [
              `      caddy.handle_path_0: "${d.staticUrlPrefix || '/static/*'}*"`,
              `      caddy.handle_path_0.root: "* ${d.staticPath || '/var/www/static'}"`,
              `      caddy.handle_path_0.file_server: ""`,
            ]
          : []),
        ...(d.serveMedia
          ? [
              `      caddy.handle_path_1: "${d.mediaUrlPrefix || '/media/*'}*"`,
              `      caddy.handle_path_1.root: "* ${d.mediaPath || '/var/www/media'}"`,
              `      caddy.handle_path_1.file_server: ""`,
            ]
          : []),
      ].join('\n');

      setCaddyModal({
        domain: d.domain,
        yaml: res?.snippet || res?.yaml || fallbackYaml,
      });
    } catch (err: any) {
      toast.error(err?.message || 'Failed to load Caddy labels');
    }
  };

  return (
    <div className="space-y-4">
      {/* Header Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
        <div className="flex items-center gap-2">
          <Globe className="h-4 w-4 text-orange-400" />
          <h2 className="text-sm font-semibold text-zinc-100">
            Domains
          </h2>
          <span className="rounded-md bg-white/[0.06] px-2 py-0.5 text-[11px] font-medium text-zinc-400 tabular-nums">
            {app.domains.length}
          </span>
        </div>
        <button
          type="button"
          onClick={openAddModal}
          className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer shrink-0"
        >
          <Plus className="h-3.5 w-3.5" />
          Add Domain
        </button>
      </div>

      {/* Section switcher */}
      <div className="inline-flex items-center gap-0.5 rounded-lg border border-white/[0.07] bg-[#09090b] p-0.5">
        {(
          [
            { id: 'domains', label: 'Domains', icon: Globe },
            { id: 'preview', label: 'Preview URL', icon: Eye },
            { id: 'ports', label: 'Exposed Ports', icon: Network },
          ] as const
        ).map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors cursor-pointer ${
              tab === t.id ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <t.icon className="h-3.5 w-3.5" />
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'preview' && (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-1">Domain source</label>
              <select
                value={pvProvider}
                onChange={(e) => {
                  const val = e.target.value as PreviewConfig['provider'];
                  setPvProvider(val);
                  if (val !== '' && !pvService && detectedServices[0]) {
                    setPvService(detectedServices[0].name);
                    if (detectedServices[0].port) {
                      setPvPort(String(detectedServices[0].port));
                    }
                  }
                }}
                className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500"
              >
                <option value="">Disabled</option>
                <option value="sslip.io">sslip.io</option>
                <option value="nip.io">nip.io</option>
                <option value="custom">My own domain</option>
              </select>
            </div>

            {pvProvider === 'custom' && (
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1">Preview base domain</label>
                <input
                  type="text"
                  value={pvBase}
                  onChange={(e) => setPvBase(e.target.value)}
                  placeholder="preview.example.com"
                  className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500"
                />
              </div>
            )}

            {pvProvider !== '' && (
              <>
                <div>
                  <label className="block text-xs font-medium text-zinc-300 mb-1">Compose service</label>
                  <select
                    value={
                      isCustomPvService
                        ? '__custom__'
                        : detectedServices.some((s) => s.name === pvService)
                        ? pvService
                        : '__custom__'
                    }
                    onChange={(e) => {
                      const val = e.target.value;
                      if (val === '__custom__') {
                        setIsCustomPvService(true);
                      } else {
                        setIsCustomPvService(false);
                        setPvService(val);
                        const match = detectedServices.find((s) => s.name === val);
                        if (match?.port) setPvPort(String(match.port));
                      }
                    }}
                    className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500 cursor-pointer"
                  >
                    {detectedServices.map((s) => (
                      <option key={s.name} value={s.name}>
                        {s.name} {s.port ? `(:${s.port})` : ''}
                      </option>
                    ))}
                    <option value="__custom__">+ Custom service name</option>
                  </select>
                  {isCustomPvService && (
                    <input
                      type="text"
                      value={pvService}
                      onChange={(e) => setPvService(e.target.value)}
                      placeholder="Service name"
                      className="mt-2 w-full px-3 py-1.5 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500"
                    />
                  )}
                </div>
                <div>
                  <label className="block text-xs font-medium text-zinc-300 mb-1">Container port</label>
                  <input
                    type="number"
                    min={1}
                    max={65535}
                    value={pvPort}
                    onChange={(e) => setPvPort(e.target.value)}
                    className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono tabular-nums focus:outline-none focus:border-orange-500"
                  />
                </div>
              </>
            )}
          </div>

          {pvProvider !== '' && (
            <div className="rounded-lg border border-white/[0.06] bg-[#09090b] px-3 py-2 text-xs flex items-center justify-between gap-2">
              <span className="text-zinc-400">Standby ({standbySlotLower.toUpperCase()})</span>
              <span className="font-mono text-violet-300 truncate">
                {formatSlotPreviewDomain(
                  pvProvider === 'custom'
                    ? pvBase.trim().toLowerCase() || 'preview.example.com'
                    : formatWildcardDomain(app.slug, pvProvider),
                  standbySlotLower
                )}
              </span>
            </div>
          )}

          <div className="flex justify-end">
            <button
              type="button"
              disabled={pvSaving}
              onClick={handleSavePreview}
              className="px-4 py-1.5 rounded-lg bg-zinc-100 hover:bg-white text-zinc-950 text-xs font-semibold disabled:opacity-50 cursor-pointer"
            >
              {pvSaving ? 'Saving...' : 'Save Preview URL'}
            </button>
          </div>
        </div>
      )}

      {tab === 'ports' && (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 overflow-hidden">
          {(app.ports || []).length === 0 ? (
            <div className="p-8 text-center text-xs text-zinc-500">No exposed ports</div>
          ) : (
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="border-b border-white/[0.07] bg-[#09090b]/80 text-[11px] uppercase tracking-wider text-zinc-400">
                  <th className="px-4 py-2.5 font-medium">Host port</th>
                  <th className="px-4 py-2.5 font-medium">Bind address</th>
                  <th className="px-4 py-2.5 font-medium">Service</th>
                  <th className="px-4 py-2.5 font-medium">Protocol</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05] font-mono">
                {(app.ports || []).map((pt) => (
                  <tr key={`${pt.bind_ip}:${pt.port}`}>
                    <td className="px-4 py-2.5 text-emerald-400 tabular-nums">:{pt.port}</td>
                    <td className="px-4 py-2.5 text-zinc-300">{pt.bind_ip}</td>
                    <td className="px-4 py-2.5 text-zinc-200">{pt.service}</td>
                    <td className="px-4 py-2.5 text-zinc-400 uppercase">{pt.protocol}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {tab === 'domains' && (<>
      {/* Domain List */}
      {app.domains.length === 0 ? (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-8 text-center">
          <Globe className="h-8 w-8 text-zinc-600 mx-auto mb-2.5" />
          <p className="text-sm font-medium text-zinc-300">No domains</p>
          <button
            type="button"
            onClick={openAddModal}
            className="mt-4 inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-xs font-medium transition-colors cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Add Domain
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-2.5">
          {app.domains.map((d) => {
            const dns = dnsResults[d.id];
            const isHttps = d.forceHttps !== false;
            const isPrimary = d.domain === activePrimaryDomain;

            const menuItems: ActionMenuItem[] = [
              ...(!isPrimary
                ? [
                    {
                      label: pinningId === d.id ? 'Setting Primary...' : 'Set as Primary',
                      icon: <Pin className="h-3.5 w-3.5" />,
                      onClick: () => handleSetPrimary(d),
                      disabled: pinningId === d.id,
                    },
                  ]
                : []),
              {
                label: dnsLoadingId === d.id ? 'Verifying DNS...' : 'Verify DNS',
                icon: <RefreshCw className={`h-3.5 w-3.5 ${dnsLoadingId === d.id ? 'animate-spin' : ''}`} />,
                onClick: () => handleCheckDns(d),
                disabled: dnsLoadingId === d.id,
              },
              {
                label: 'Caddy YAML',
                icon: <Code2 className="h-3.5 w-3.5" />,
                onClick: () => handleInspectLabels(d),
              },
              {
                label: 'Remove',
                icon: <Trash2 className="h-3.5 w-3.5" />,
                onClick: () => handleDeleteDomain(d),
                variant: 'danger',
              },
            ];

            return (
              <div
                key={d.id}
                className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-3.5 transition-colors hover:border-white/[0.12]"
              >
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    {/* Domain + Lock + Primary / Alias badges */}
                    <div className="flex items-center gap-2 min-w-0 flex-wrap">
                      <Lock
                        className={`h-3.5 w-3.5 shrink-0 ${
                          isHttps ? 'text-emerald-400' : 'text-amber-400'
                        }`}
                      />
                      <a
                        href={`${isHttps ? 'https' : 'http'}://${d.domain}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="group font-mono text-xs font-semibold text-zinc-100 hover:text-orange-400 hover:underline underline-offset-4 inline-flex items-center gap-1.5 truncate transition-colors"
                      >
                        {d.domain}
                        <ExternalLink className="h-3 w-3 text-zinc-500 group-hover:text-orange-400 transition-colors shrink-0" />
                      </a>

                      {isPrimary && (
                        <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-400 border border-emerald-500/20">
                          Primary
                        </span>
                      )}

                      {d.enableWww && (
                        <span className="text-[10px] font-mono text-zinc-400 bg-white/[0.04] px-1.5 py-0.5 rounded border border-white/[0.06]">
                          +www
                        </span>
                      )}
                    </div>

                    {/* Upstream & Static/Media Routing Badges */}
                    <div className="flex flex-wrap items-center gap-2 pt-0.5 text-xs">
                      <span className="inline-flex items-center gap-1.5 rounded-md bg-white/[0.04] border border-white/[0.07] px-2 py-0.5 text-[11px] font-mono text-zinc-300">
                        <Server className="h-3 w-3 text-zinc-500" />
                        Upstream:{' '}
                        <span className="text-orange-300 font-medium tabular-nums">
                          {d.targetService}:{d.targetPort}
                        </span>
                      </span>

                      {d.serveStatic && (
                        <span className="inline-flex items-center gap-1 text-[11px] text-sky-300 bg-sky-500/10 border border-sky-500/20 px-2 py-0.5 rounded-md font-mono">
                          <FolderOpen className="h-3 w-3" />
                          {d.staticUrlPrefix || '/static/'} → {d.staticPath}
                        </span>
                      )}

                      {d.serveMedia && (
                        <span className="inline-flex items-center gap-1 text-[11px] text-violet-300 bg-violet-500/10 border border-violet-500/20 px-2 py-0.5 rounded-md font-mono">
                          <ImageIcon className="h-3 w-3" />
                          {d.mediaUrlPrefix || '/media/'} → {d.mediaPath}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Actions: Edit button + 3-dot dropdown */}
                  <div className="flex items-center gap-1.5 shrink-0">
                    <button
                      type="button"
                      onClick={() => openEditModal(d)}
                      className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-200 hover:bg-zinc-800 hover:text-white transition-colors cursor-pointer"
                    >
                      <Edit2 className="h-3 w-3 text-zinc-400" />
                      Edit
                    </button>
                    <ActionMenu items={menuItems} />
                  </div>
                </div>

                {/* DNS Diagnostics (only if checked) */}
                {dns && (
                  <div className="mt-2.5 pt-2.5 border-t border-white/[0.05] flex items-center justify-between gap-2 text-[11px]">
                    <div className="flex items-center gap-2">
                      {dns.cloudflare ? (
                        <span className="inline-flex items-center gap-1 text-amber-400">
                          <ShieldCheck className="h-3.5 w-3.5" />
                          Cloudflare Proxy
                        </span>
                      ) : dns.ok ? (
                        <span className="inline-flex items-center gap-1 text-emerald-400">
                          <CheckCircle2 className="h-3.5 w-3.5" />
                          DNS Verified
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 text-rose-400">
                          <AlertCircle className="h-3.5 w-3.5" />
                          DNS Issue
                        </span>
                      )}
                      <span className="font-mono text-zinc-400">
                        ({dns.ips.length > 0 ? dns.ips.join(', ') : 'no IP'})
                      </span>
                    </div>
                    {dns.error && <span className="text-rose-400 font-mono">{dns.error}</span>}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      </>)}

      {/* Create / Edit Domain Modal */}
      {showDomainModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 overflow-y-auto">
          <div className="bg-[#111115] border border-white/[0.08] rounded-xl w-full max-w-lg p-5 shadow-2xl space-y-4 my-8">
            <div className="flex items-center justify-between border-b border-white/[0.07] pb-3">
              <h3 className="text-sm font-semibold text-zinc-100">
                {editingDomainId ? 'Edit Domain Route' : 'Add Custom Domain Route'}
              </h3>
              <button
                type="button"
                onClick={() => setShowDomainModal(false)}
                className="text-zinc-500 hover:text-zinc-300 cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <form onSubmit={handleSaveDomain} className="space-y-4">
              <div>
                <div className="flex items-center justify-between mb-1">
                  <label className="block text-xs font-medium text-zinc-300">
                    Domain Name
                  </label>
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() =>
                        setDomainHost(formatWildcardDomain(app.slug, 'sslip.io'))
                      }
                      className="rounded border border-emerald-500/30 bg-emerald-500/10 px-2 py-0.5 font-mono text-[10px] text-emerald-400 hover:bg-emerald-500/20 cursor-pointer"
                    >
                      + sslip.io
                    </button>
                    <button
                      type="button"
                      onClick={() =>
                        setDomainHost(formatWildcardDomain(app.slug, 'nip.io'))
                      }
                      className="rounded border border-violet-500/30 bg-violet-500/10 px-2 py-0.5 font-mono text-[10px] text-violet-300 hover:bg-violet-500/20 cursor-pointer"
                    >
                      + nip.io
                    </button>
                  </div>
                </div>
                <input
                  type="text"
                  required
                  placeholder="app.example.com"
                  value={domainHost}
                  onChange={(e) => setDomainHost(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500"
                />
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-zinc-300 mb-1">
                    Compose service
                  </label>
                  <select
                    value={
                      isCustomDomainService
                        ? '__custom__'
                        : detectedServices.some((s) => s.name === domainService)
                        ? domainService
                        : '__custom__'
                    }
                    onChange={(e) => {
                      const val = e.target.value;
                      if (val === '__custom__') {
                        setIsCustomDomainService(true);
                      } else {
                        setIsCustomDomainService(false);
                        setDomainService(val);
                        const match = detectedServices.find((s) => s.name === val);
                        if (match?.port) {
                          setDomainPort(String(match.port));
                        }
                      }
                    }}
                    className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500 cursor-pointer"
                  >
                    {detectedServices.map((s) => (
                      <option key={s.name} value={s.name}>
                        {s.name} {s.port ? `(:${s.port})` : ''}
                      </option>
                    ))}
                    <option value="__custom__">+ Custom service name</option>
                  </select>
                  {isCustomDomainService && (
                    <input
                      type="text"
                      required
                      placeholder="e.g. web"
                      value={domainService}
                      onChange={(e) => {
                        const val = e.target.value;
                        setDomainService(val);
                        const match = detectedServices.find((s) => s.name === val);
                        if (match?.port) setDomainPort(String(match.port));
                      }}
                      className="mt-2 w-full px-3 py-1.5 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500"
                    />
                  )}
                </div>

                <div>
                  <label className="block text-xs font-medium text-zinc-300 mb-1">
                    Container Port
                  </label>
                  <input
                    type="number"
                    required
                    min={1}
                    max={65535}
                    value={domainPort}
                    onChange={(e) => setDomainPort(e.target.value)}
                    className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono tabular-nums focus:outline-none focus:border-orange-500"
                  />
                </div>
              </div>

              {/* Primary URL & HTTPS & WWW Toggles */}
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5 pt-1">
                <label className="flex items-center gap-2.5 p-2.5 rounded-lg bg-[#09090b] border border-white/[0.08] cursor-pointer">
                  <input
                    type="checkbox"
                    checked={domainIsPrimary}
                    onChange={(e) => setDomainIsPrimary(e.target.checked)}
                    className="rounded border-zinc-700 bg-zinc-800 text-orange-500"
                  />
                  <span className="text-xs font-medium text-zinc-200">Primary</span>
                </label>

                <label className="flex items-center gap-2.5 p-2.5 rounded-lg bg-[#09090b] border border-white/[0.08] cursor-pointer">
                  <input
                    type="checkbox"
                    checked={domainHttps}
                    onChange={(e) => setDomainHttps(e.target.checked)}
                    className="rounded border-zinc-700 bg-zinc-800 text-orange-500"
                  />
                  <span className="text-xs font-medium text-zinc-200">Auto HTTPS</span>
                </label>

                <label className="flex items-center gap-2.5 p-2.5 rounded-lg bg-[#09090b] border border-white/[0.08] cursor-pointer">
                  <input
                    type="checkbox"
                    checked={domainWww}
                    onChange={(e) => setDomainWww(e.target.checked)}
                    className="rounded border-zinc-700 bg-zinc-800 text-orange-500"
                  />
                  <span className="text-xs font-medium text-zinc-200">www redirect</span>
                </label>
              </div>

              {/* Static File Serving */}
              <div className="p-3 rounded-lg bg-[#09090b] border border-white/[0.08] space-y-3">
                <label className="flex items-center justify-between cursor-pointer">
                  <span className="text-xs font-medium text-zinc-200">Static files</span>
                  <input
                    type="checkbox"
                    checked={domainStatic}
                    onChange={(e) => setDomainStatic(e.target.checked)}
                    className="rounded border-zinc-700 bg-zinc-800 text-orange-500"
                  />
                </label>

                {domainStatic && (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5 pt-1">
                    <div>
                      <label className="block text-[11px] text-zinc-400 mb-1">URL Prefix</label>
                      <input
                        type="text"
                        value={domainStaticPrefix}
                        onChange={(e) => setDomainStaticPrefix(e.target.value)}
                        placeholder="/static/"
                        className="w-full px-2.5 py-1.5 rounded bg-zinc-950 border border-white/[0.08] text-xs text-zinc-200 font-mono"
                      />
                    </div>
                    <div>
                      <label className="block text-[11px] text-zinc-400 mb-1">
                        Host Path
                      </label>
                      <input
                        type="text"
                        value={domainStaticPath}
                        onChange={(e) => setDomainStaticPath(e.target.value)}
                        placeholder="/var/www/static"
                        className="w-full px-2.5 py-1.5 rounded bg-zinc-950 border border-white/[0.08] text-xs text-zinc-200 font-mono"
                      />
                    </div>
                  </div>
                )}
              </div>

              {/* Media File Serving */}
              <div className="p-3 rounded-lg bg-[#09090b] border border-white/[0.08] space-y-3">
                <label className="flex items-center justify-between cursor-pointer">
                  <span className="text-xs font-medium text-zinc-200">Media files</span>
                  <input
                    type="checkbox"
                    checked={domainMedia}
                    onChange={(e) => setDomainMedia(e.target.checked)}
                    className="rounded border-zinc-700 bg-zinc-800 text-orange-500"
                  />
                </label>

                {domainMedia && (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5 pt-1">
                    <div>
                      <label className="block text-[11px] text-zinc-400 mb-1">URL Prefix</label>
                      <input
                        type="text"
                        value={domainMediaPrefix}
                        onChange={(e) => setDomainMediaPrefix(e.target.value)}
                        placeholder="/media/"
                        className="w-full px-2.5 py-1.5 rounded bg-zinc-950 border border-white/[0.08] text-xs text-zinc-200 font-mono"
                      />
                    </div>
                    <div>
                      <label className="block text-[11px] text-zinc-400 mb-1">Host Path</label>
                      <input
                        type="text"
                        value={domainMediaPath}
                        onChange={(e) => setDomainMediaPath(e.target.value)}
                        placeholder="/var/www/media"
                        className="w-full px-2.5 py-1.5 rounded bg-zinc-950 border border-white/[0.08] text-xs text-zinc-200 font-mono"
                      />
                    </div>
                  </div>
                )}
              </div>

              <div className="flex items-center justify-end gap-2 pt-2 border-t border-white/[0.07]">
                <button
                  type="button"
                  onClick={() => setShowDomainModal(false)}
                  className="px-3.5 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-300 text-xs font-medium cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={saving}
                  className="px-4 py-1.5 rounded-lg bg-zinc-100 hover:bg-white text-zinc-950 text-xs font-semibold disabled:opacity-50 cursor-pointer"
                >
                  {saving ? 'Saving...' : editingDomainId ? 'Update Route' : 'Add Domain'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Caddy Labels YAML Modal */}
      {caddyModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4">
          <div className="bg-[#111115] border border-white/[0.08] rounded-xl w-full max-w-xl p-5 shadow-2xl space-y-4">
            <div className="flex items-center justify-between border-b border-white/[0.07] pb-3">
              <h3 className="text-sm font-semibold text-zinc-100">
                Caddy Labels ·{' '}
                <span className="font-mono text-orange-400">{caddyModal.domain}</span>
              </h3>
              <button
                type="button"
                onClick={() => setCaddyModal(null)}
                className="text-zinc-500 hover:text-zinc-300 cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <pre className="bg-[#09090b] border border-white/[0.08] rounded-lg p-3.5 text-xs font-mono text-emerald-300 overflow-x-auto max-h-80">
              {caddyModal.yaml}
            </pre>

            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => {
                  navigator.clipboard?.writeText(caddyModal.yaml);
                  setCopiedCaddy(true);
                  setTimeout(() => setCopiedCaddy(false), 1800);
                }}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-xs font-medium cursor-pointer"
              >
                {copiedCaddy ? (
                  <Check className="h-3.5 w-3.5 text-emerald-400" />
                ) : (
                  <Copy className="h-3.5 w-3.5" />
                )}
                {copiedCaddy ? 'Copied' : 'Copy YAML'}
              </button>
              <button
                type="button"
                onClick={() => setCaddyModal(null)}
                className="px-3.5 py-1.5 rounded-lg bg-zinc-100 hover:bg-white text-zinc-950 text-xs font-semibold cursor-pointer"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
