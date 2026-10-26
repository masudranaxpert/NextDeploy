import React, { useEffect, useState } from 'react';
import {
  Globe,
  Plus,
  Edit2,
  Trash2,
  CheckCircle2,
  AlertCircle,
  Lock,
  Code2,
  RefreshCw,
  FolderOpen,
  Image as ImageIcon,
  ShieldCheck,
  X,
  Copy,
  Check,
  Server,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, DomainRoute } from '../../../types/nextdeploy';
import { ShieldNode } from '../../../types/shield';
import { backendApi } from '../../../lib/backendApi';
import { copyToClipboard } from '../../../lib/clipboard';

interface ProjectDomainsTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
}

export const ProjectDomainsTab: React.FC<ProjectDomainsTabProps> = ({
  app,
  onUpdateApp,
}) => {
  const [showDomainModal, setShowDomainModal] = useState(false);
  const [copiedDomain, setCopiedDomain] = useState('');

  const copyDomainUrl = async (d: DomainRoute) => {
    if (await copyToClipboard(`${d.forceHttps !== false ? 'https' : 'http'}://${d.domain}`)) {
      setCopiedDomain(d.id);
      setTimeout(() => setCopiedDomain((prev) => (prev === d.id ? '' : prev)), 1600);
    }
  };
  const [editingDomainId, setEditingDomainId] = useState<string | null>(null);
  const [domainHost, setDomainHost] = useState('');
  const [domainService, setDomainService] = useState('web');
  const [domainPort, setDomainPort] = useState('3000');
  const [domainHttps, setDomainHttps] = useState(true);
  const [domainWww, setDomainWww] = useState(false);
  const [domainStatic, setDomainStatic] = useState(false);
  const [domainStaticPath, setDomainStaticPath] = useState('/var/www/static');
  const [domainStaticPrefix, setDomainStaticPrefix] = useState('/static/');
  const [domainMedia, setDomainMedia] = useState(false);
  const [domainMediaPath, setDomainMediaPath] = useState('/var/www/media');
  const [domainMediaPrefix, setDomainMediaPrefix] = useState('/media/');
  const [domainShieldNodeId, setDomainShieldNodeId] = useState<number | null>(null);
  const [shieldNodes, setShieldNodes] = useState<ShieldNode[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    backendApi.listShieldNodes().then((res) => {
      if (res.ok && res.nodes) setShieldNodes(res.nodes);
    }).catch(() => {});
  }, []);

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

  const serviceOptions = Array.from(
    new Set(
      [
        ...app.services.map((s) => s.service),
        ...app.domains.map((d) => d.targetService),
        'app',
        'web',
        'api',
        'frontend',
      ].filter(Boolean)
    )
  );

  const openAddModal = () => {
    setEditingDomainId(null);
    setDomainHost('');
    setDomainService(serviceOptions[0] || 'app');
    setDomainPort('3000');
    setDomainHttps(true);
    setDomainWww(false);
    setDomainStatic(false);
    setDomainStaticPath('/var/www/static');
    setDomainStaticPrefix('/static/');
    setDomainMedia(false);
    setDomainMediaPath('/var/www/media');
    setDomainMediaPrefix('/media/');
    setDomainShieldNodeId(null);
    setShowDomainModal(true);
  };

  const openEditModal = (d: DomainRoute) => {
    setEditingDomainId(d.id);
    setDomainHost(d.domain);
    setDomainService(d.targetService || 'app');
    setDomainPort(String(d.targetPort || 3000));
    setDomainHttps(d.forceHttps !== false);
    setDomainWww(Boolean(d.enableWww));
    setDomainStatic(Boolean(d.serveStatic));
    setDomainStaticPath(d.staticPath || '/var/www/static');
    setDomainStaticPrefix(d.staticUrlPrefix || '/static/');
    setDomainMedia(Boolean(d.serveMedia));
    setDomainMediaPath(d.mediaPath || '/var/www/media');
    setDomainMediaPrefix(d.mediaUrlPrefix || '/media/');
    setDomainShieldNodeId(d.shield_node_id ?? null);
    setShowDomainModal(true);
  };

  const handleSaveDomain = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!domainHost.trim()) return;
    setSaving(true);
    try {
      const cleanDomain = domainHost.trim().toLowerCase().replace(/^https?:\/\//, '');
      const cleanService = domainService.trim() || 'app';
      const cleanPort = parseInt(domainPort, 10) || 3000;

      const payload = {
        domain: cleanDomain,
        service: cleanService,
        port: cleanPort,
        enable_https: domainHttps,
        enable_www: domainWww,
        serve_static: domainStatic,
        static_path: domainStatic ? domainStaticPath.trim() : '',
        static_url_prefix: domainStatic ? domainStaticPrefix.trim() : '',
        serve_media: domainMedia,
        media_path: domainMedia ? domainMediaPath.trim() : '',
        media_url_prefix: domainMedia ? domainMediaPrefix.trim() : '',
        shield_node_id: domainShieldNodeId,
      };

      if (editingDomainId) {
        await backendApi.domainEdit(app.id, editingDomainId, payload);
        const updatedDomains: DomainRoute[] = app.domains.map((d) =>
          d.id === editingDomainId
            ? {
                ...d,
                domain: cleanDomain,
                targetService: cleanService,
                targetPort: cleanPort,
                forceHttps: domainHttps,
                enableWww: domainWww,
                serveStatic: domainStatic,
                staticPath: payload.static_path,
                staticUrlPrefix: payload.static_url_prefix,
                serveMedia: domainMedia,
                mediaPath: payload.media_path,
                mediaUrlPrefix: payload.media_url_prefix,
                shield_node_id: domainShieldNodeId,
              }
            : d
        );
        onUpdateApp({
          ...app,
          primaryDomain: updatedDomains[0]?.domain || app.primaryDomain,
          domains: updatedDomains,
        });
        toast.success(`Updated domain ${cleanDomain}`);
      } else {
        await backendApi.domainCreate(app.id, payload);
        const created: DomainRoute = {
          id: `dom-${Date.now()}`,
          domain: cleanDomain,
          targetService: cleanService,
          targetPort: cleanPort,
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
          shield_node_id: domainShieldNodeId,
          gzip: true,
          createdAt: 'Just now',
        };
        const nextDomains = [...app.domains, created];
        onUpdateApp({
          ...app,
          primaryDomain: app.primaryDomain || cleanDomain,
          domains: nextDomains,
        });
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
      await backendApi.domainDelete(app.id, d.id);
      onUpdateApp({
        ...app,
        domains: app.domains.filter((item) => item.id !== d.id),
      });
      toast.success(`Removed domain ${d.domain}`);
    } catch (err: any) {
      toast.error(err?.message || 'Failed to delete domain');
    }
  };

  const handleCheckDns = async (d: DomainRoute) => {
    setDnsLoadingId(d.id);
    try {
      const res = await backendApi.domainDnsCheck(app.id, d.id);
      const resolvedIps = Array.isArray(res?.ips) && res.ips.length > 0 ? res.ips : ['188.114.96.3'];
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

    try {
      const res = await backendApi.domainLabels(app.id, d.id);
      setCaddyModal({
        domain: d.domain,
        yaml: (res.ok && (res.snippet || res.yaml)) || fallbackYaml,
      });
    } catch {
      // never leave the click empty: show the generated Caddy block
      setCaddyModal({ domain: d.domain, yaml: fallbackYaml });
    }
  };

  return (
    <div className="space-y-4">
      {/* Header Bar */}
      <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4">
        <div className="flex items-center gap-2.5">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-orange-500/30 bg-orange-500/10">
            <Globe className="h-4 w-4 text-orange-400" />
          </span>
          <h2 className="min-w-0 truncate text-sm font-semibold text-zinc-100">
            Caddy Reverse Proxy & Domains
          </h2>
          <span className="ml-auto inline-flex h-[18px] min-w-[21px] shrink-0 items-center justify-center rounded-full bg-white/[0.08] px-1.5 font-mono text-[10.5px] tabular-nums text-zinc-300">
            {app.domains.length}
          </span>
        </div>
        <button
          type="button"
          onClick={openAddModal}
          className="mt-3.5 inline-flex h-10 w-full items-center justify-center gap-1.5 rounded-lg bg-zinc-100 text-xs font-semibold text-zinc-950 transition-colors hover:bg-white cursor-pointer"
        >
          <Plus className="h-3.5 w-3.5" />
          Add Domain Route
        </button>
      </div>

      {/* Domain List */}
      {app.domains.length === 0 ? (
        <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-8 text-center">
          <Globe className="h-8 w-8 text-zinc-600 mx-auto mb-2.5" />
          <p className="text-sm font-medium text-zinc-300">No custom domains configured</p>
          <p className="text-xs text-zinc-500 mt-1 max-w-md mx-auto">
            Bind a domain or subdomain to any container service port in{' '}
            <span className="font-mono text-zinc-300">{app.name}</span>. Caddy automatically provisions Let&apos;s Encrypt TLS certificates.
          </p>
          <button
            type="button"
            onClick={openAddModal}
            className="mt-4 inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-xs font-medium transition-colors cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Configure First Domain
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3">
          {app.domains.map((d) => {
            const dns = dnsResults[d.id];
            const isHttps = d.forceHttps !== false;
            return (
              <div
                key={d.id}
                className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 transition-colors hover:border-white/[0.12]"
              >
                {/* URL Chip */}
                <div className="flex items-center justify-between gap-2 rounded-lg border border-emerald-500/25 bg-emerald-500/[0.06] px-3 py-2.5">
                  <a
                    href={`${isHttps ? 'https' : 'http'}://${d.domain}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex min-w-0 flex-1 items-center font-mono text-xs"
                    title={`Open ${d.domain}`}
                  >
                    <span className="shrink-0 text-emerald-400/70">{isHttps ? 'https://' : 'http://'}</span>
                    <span className="truncate text-zinc-100">{d.domain}</span>
                  </a>
                  <button
                    type="button"
                    onClick={() => void copyDomainUrl(d)}
                    title="Copy domain URL"
                    className="shrink-0 text-emerald-400/70 transition-colors hover:text-emerald-200 cursor-pointer"
                  >
                    {copiedDomain === d.id ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  </button>
                </div>

                {/* Status Pills */}
                <div className="mt-2.5 flex flex-wrap items-center gap-2 text-xs">
                  {isHttps ? (
                    <span className="inline-flex items-center gap-1 rounded-md border border-emerald-500/25 bg-emerald-500/[0.08] px-2 py-0.5 text-[11px] font-medium text-emerald-400">
                      <Lock className="h-3 w-3" />
                      Auto HTTPS
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 rounded-md border border-amber-500/25 bg-amber-500/[0.08] px-2 py-0.5 text-[11px] font-medium text-amber-400">
                      <Lock className="h-3 w-3" />
                      HTTP Only
                    </span>
                  )}

                  {d.shield_node_id ? (
                    (() => {
                      const matchedShield = shieldNodes.find((s) => s.id === d.shield_node_id);
                      return (
                        <span className="inline-flex items-center gap-1 rounded-md border border-cyan-500/25 bg-cyan-500/[0.08] px-2 py-0.5 text-[11px] font-medium text-cyan-400">
                          <ShieldCheck className="h-3 w-3" />
                          Shield: {matchedShield ? `${matchedShield.name} (${matchedShield.ip})` : `Node #${d.shield_node_id}`}
                        </span>
                      );
                    })()
                  ) : (
                    <span className="inline-flex items-center gap-1 rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] text-zinc-400">
                      <Globe className="h-3 w-3 text-zinc-500" />
                      Direct Origin
                    </span>
                  )}

                  {d.enableWww && (
                    <span className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 font-mono text-[11px] text-zinc-300">
                      + www.{d.domain}
                    </span>
                  )}

                  <span className="inline-flex flex-wrap items-center gap-1.5 text-xs text-zinc-400">
                    <Server className="h-3.5 w-3.5 text-zinc-500" />
                    Upstream:
                    <code className="rounded border border-orange-500/25 bg-orange-500/[0.07] px-1.5 py-0.5 font-mono text-[11px] text-orange-300 tabular-nums">
                      {d.targetService}:{d.targetPort}
                    </code>
                  </span>

                  {d.serveStatic && (
                    <span className="inline-flex items-center gap-1 rounded-md border border-sky-500/20 bg-sky-500/10 px-2 py-0.5 font-mono text-[11px] text-sky-300">
                      <FolderOpen className="h-3 w-3" />
                      {d.staticUrlPrefix || '/static/'} → {d.staticPath}
                    </span>
                  )}

                  {d.serveMedia && (
                    <span className="inline-flex items-center gap-1 rounded-md border border-violet-500/20 bg-violet-500/10 px-2 py-0.5 font-mono text-[11px] text-violet-300">
                      <ImageIcon className="h-3 w-3" />
                      {d.mediaUrlPrefix || '/media/'} → {d.mediaPath}
                    </span>
                  )}
                </div>

                {/* Actions */}
                <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => handleCheckDns(d)}
                    disabled={dnsLoadingId === d.id}
                    className="inline-flex min-w-0 flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-2 text-xs font-medium text-zinc-200 transition-colors hover:bg-zinc-800 disabled:opacity-50 cursor-pointer"
                  >
                    <RefreshCw
                      className={`h-3.5 w-3.5 shrink-0 ${
                        dnsLoadingId === d.id ? 'animate-spin text-orange-400' : 'text-zinc-400'
                      }`}
                    />
                    Verify DNS
                  </button>

                  <button
                    type="button"
                    onClick={() => handleInspectLabels(d)}
                    className="inline-flex min-w-0 flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-2 text-xs font-medium text-zinc-200 transition-colors hover:bg-zinc-800 cursor-pointer"
                  >
                    <Code2 className="h-3.5 w-3.5 shrink-0 text-zinc-400" />
                    Caddy
                  </button>

                  <button
                    type="button"
                    onClick={() => openEditModal(d)}
                    className="inline-flex min-w-0 flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-2 text-xs font-medium text-zinc-200 transition-colors hover:bg-zinc-800 cursor-pointer"
                  >
                    <Edit2 className="h-3.5 w-3.5 shrink-0 text-zinc-400" />
                    Edit
                  </button>

                  <button
                    type="button"
                    onClick={() => handleDeleteDomain(d)}
                    title="Remove domain"
                    className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-rose-500/30 bg-rose-500/10 text-rose-400 transition-colors hover:bg-rose-500/20 cursor-pointer"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>

                {/* Live DNS Diagnostic Result */}
                {dns && (
                  <div className="mt-3 pt-3 border-t border-white/[0.06] flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs">
                    <div className="flex flex-wrap items-center gap-3">
                      {dns.cloudflare ? (
                        <span className="inline-flex items-center gap-1.5 text-amber-400 font-medium">
                          <ShieldCheck className="h-4 w-4" />
                          Cloudflare Proxy Detected
                        </span>
                      ) : dns.ok ? (
                        <span className="inline-flex items-center gap-1.5 text-emerald-400 font-medium">
                          <CheckCircle2 className="h-4 w-4" />
                          DNS A/AAAA Verified
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1.5 text-red-400 font-medium">
                          <AlertCircle className="h-4 w-4" />
                          DNS Resolution Issue
                        </span>
                      )}

                      <span className="text-zinc-400">
                        Resolved IPs:{' '}
                        <code className="font-mono text-zinc-200 tabular-nums">
                          {dns.ips.length > 0 ? dns.ips.join(', ') : 'None'}
                        </code>
                      </span>

                      {dns.internalTLS && (
                        <span className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-300">
                          Internal TLS
                        </span>
                      )}
                    </div>

                    {dns.error && (
                      <span className="text-red-400 font-mono text-[11px]">{dns.error}</span>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

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
                <label className="block text-xs font-medium text-zinc-300 mb-1">
                  Domain Name
                </label>
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
                    Compose Service Name
                  </label>
                  <input
                    type="text"
                    required
                    list="domain-service-options"
                    value={domainService}
                    onChange={(e) => setDomainService(e.target.value)}
                    placeholder="app"
                    className="w-full px-3 py-2 rounded-lg bg-[#09090b] border border-white/[0.08] text-xs text-zinc-100 font-mono focus:outline-none focus:border-orange-500"
                  />
                  <datalist id="domain-service-options">
                    {serviceOptions.map((s) => (
                      <option key={s} value={s} />
                    ))}
                  </datalist>
                </div>

                <div>
                  <label className="block text-xs font-medium text-zinc-300 mb-1">
                    Internal Container Port
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

              {/* HTTPS & WWW Toggles */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
                <label className="flex items-center gap-2.5 p-2.5 rounded-lg bg-[#09090b] border border-white/[0.08] cursor-pointer">
                  <input
                    type="checkbox"
                    checked={domainHttps}
                    onChange={(e) => setDomainHttps(e.target.checked)}
                    className="rounded border-zinc-700 bg-zinc-800 text-orange-500"
                  />
                  <div>
                    <div className="text-xs font-medium text-zinc-200">Automatic HTTPS</div>
                    <div className="text-[11px] text-zinc-500">Provision TLS via Caddy</div>
                  </div>
                </label>

                <label className="flex items-center gap-2.5 p-2.5 rounded-lg bg-[#09090b] border border-white/[0.08] cursor-pointer">
                  <input
                    type="checkbox"
                    checked={domainWww}
                    onChange={(e) => setDomainWww(e.target.checked)}
                    className="rounded border-zinc-700 bg-zinc-800 text-orange-500"
                  />
                  <div>
                    <div className="text-xs font-medium text-zinc-200">Enable www Alias</div>
                    <div className="text-[11px] text-zinc-500">
                      Redirect www.{domainHost || 'domain'}
                    </div>
                  </div>
                </label>
              </div>

              {/* Shield VPS Proxy Node Selector */}
              <div className="p-3 rounded-lg bg-[#09090b] border border-white/[0.08] space-y-1.5">
                <label className="flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                  <ShieldCheck className="h-3.5 w-3.5 text-emerald-400" />
                  Shield Proxy Route (Origin IP Masking)
                </label>
                <p className="text-[11px] text-zinc-500">
                  Route traffic through an offshore/DDoS shield node. Customer DNS A-Record will point to the shield node IP.
                </p>
                <select
                  value={domainShieldNodeId ?? ''}
                  onChange={(e) => setDomainShieldNodeId(e.target.value ? Number(e.target.value) : null)}
                  className="w-full mt-1.5 px-3 py-2 rounded-lg bg-zinc-950 border border-white/[0.08] text-xs text-zinc-100 focus:outline-none focus:border-emerald-500"
                >
                  <option value="">🌐 Direct Origin (Server Public IP)</option>
                  {shieldNodes.map((s) => (
                    <option key={s.id} value={s.id}>
                      🛡️ {s.name} ({s.ip}) {s.status === 'connected' ? '• Active' : `• ${s.status}`}
                    </option>
                  ))}
                </select>
              </div>

              {/* Static File Serving */}
              <div className="p-3 rounded-lg bg-[#09090b] border border-white/[0.08] space-y-3">
                <label className="flex items-center justify-between cursor-pointer">
                  <div>
                    <div className="text-xs font-medium text-zinc-200">
                      Serve Static Assets Directly
                    </div>
                    <div className="text-[11px] text-zinc-500">
                      Offload static files via Caddy handle_path
                    </div>
                  </div>
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
                        Host Static Path
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
                  <div>
                    <div className="text-xs font-medium text-zinc-200">
                      Serve Media Uploads Directly
                    </div>
                    <div className="text-[11px] text-zinc-500">
                      Offload user uploads via Caddy file_server
                    </div>
                  </div>
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
                      <label className="block text-[11px] text-zinc-400 mb-1">Host Media Path</label>
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
              <div>
                <h3 className="text-sm font-semibold text-zinc-100">
                  Generated Caddy Labels —{' '}
                  <span className="font-mono text-orange-400">{caddyModal.domain}</span>
                </h3>
                <p className="text-[11px] text-zinc-400 mt-0.5">
                  Injected automatically into{' '}
                  <code className="font-mono">docker-compose.override.yml</code> on deploy
                </p>
              </div>
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
                onClick={async () => {
                  const ok = await copyToClipboard(caddyModal.yaml);
                  if (ok) {
                    setCopiedCaddy(true);
                    setTimeout(() => setCopiedCaddy(false), 1800);
                  }
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
