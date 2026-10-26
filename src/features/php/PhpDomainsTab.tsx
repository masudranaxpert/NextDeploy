import React, { useContext, useEffect, useState } from 'react';
import { Check, Copy, ExternalLink, Globe, Plus, ShieldCheck, Star, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { ShieldNode } from '../../types/shield';
import { backendApi } from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';
import { OwnerContext } from '../../components/ui/OwnerBadge';
import { ShadcnSelect } from '../../components/ui/Primitives';
import { Card, EmptyState, Pill, PrimaryButton, inputCls } from './ui';

const DOMAIN_RE = /^(?=.{3,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i;

export const PhpDomainsTab: React.FC<{
  app: AppStack;
  onChanged: () => void | Promise<void>;
  onUpdateApp?: (updated: AppStack) => void;
}> = ({ app, onChanged, onUpdateApp }) => {
  const { isAdmin } = useContext(OwnerContext);
  const [domain, setDomain] = useState('');
  const [https, setHttps] = useState(true);
  const [www, setWww] = useState(false);
  const [shieldNodeId, setShieldNodeId] = useState<number | null>(null);
  const [shieldNodes, setShieldNodes] = useState<ShieldNode[]>([]);
  const [busy, setBusy] = useState('');
  const [copiedDomainId, setCopiedDomainId] = useState<number | null>(null);

  useEffect(() => {
    if (!isAdmin) return;
    backendApi.listShieldNodes().then((res) => {
      if (res.ok && res.nodes) setShieldNodes(res.nodes);
    }).catch(() => {});
  }, [isAdmin]);

  const add = async () => {
    const d = domain.trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/.*$/, '');
    if (!DOMAIN_RE.test(d)) {
      toast.error('Enter a valid domain such as example.com');
      return;
    }
    if (isAdmin && shieldNodeId) {
      const target = shieldNodes.find((s) => s.id === shieldNodeId);
      if (target && target.status !== 'connected') {
        toast.error(`Cannot route through '${target.name}': Node is ${target.status}. Only connected shield nodes can route traffic.`);
        return;
      }
    }
    setBusy('add');
    const res = await backendApi.domainCreate(app.id, {
      domain: d,
      service: 'php',
      port: 80,
      enable_https: https,
      enable_www: www,
      shield_node_id: isAdmin ? shieldNodeId : null,
      is_primary: app.domains.length === 0 || undefined,
    });
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Could not add the domain');
      return;
    }
    setDomain('');
    if (onUpdateApp && res.id) {
      const createdDomain = {
        id: String(res.id),
        domain: d,
        targetService: 'php',
        targetPort: 80,
        isPrimary: app.domains.length === 0,
        tlsProvider: 'LetsEncrypt' as const,
        tlsStatus: 'active' as const,
        forceHttps: https,
        enableWww: www,
        shield_node_id: isAdmin ? shieldNodeId : null,
        gzip: true,
        createdAt: 'Just now',
      };
      onUpdateApp({
        ...app,
        primaryDomain: app.primaryDomain || d,
        domains: [...app.domains, createdDomain],
      });
    }
    toast.success(`${d} added`);
    await onChanged();
  };

  const remove = async (id: string, name: string) => {
    if (!window.confirm(`Remove ${name} from this site?`)) return;
    setBusy('del' + id);
    const res = await backendApi.domainDelete(app.id, id);
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Could not remove the domain');
      return;
    }
    if (onUpdateApp) {
      onUpdateApp({
        ...app,
        domains: app.domains.filter((dom) => dom.id !== id),
      });
    }
    toast.success(`${name} removed`);
    await onChanged();
  };

  const makePrimary = async (id: string) => {
    setBusy('pri' + id);
    const res = await backendApi.domainSetPrimary(app.id, id);
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Could not change the primary domain');
      return;
    }
    if (onUpdateApp) {
      onUpdateApp({
        ...app,
        primaryDomain: app.domains.find((dom) => dom.id === id)?.domain || app.primaryDomain,
        domains: app.domains.map((dom) => ({
          ...dom,
          isPrimary: dom.id === id,
        })),
      });
    }
    await onChanged();
  };

  return (
    <div className="space-y-5">
      <Card title="Add domain">
        <div className="flex items-center gap-2">
          <input
            className={`${inputCls} min-w-0 flex-1`}
            placeholder="example.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && void add()}
          />
          <PrimaryButton busy={busy === 'add'} onClick={add} disabled={!domain.trim()}>
            <Plus className="h-3.5 w-3.5" /> Add
          </PrimaryButton>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-5 text-xs text-zinc-300">
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input type="checkbox" checked={https} onChange={(e) => setHttps(e.target.checked)} className="accent-orange-500" />
            HTTPS
          </label>
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input type="checkbox" checked={www} onChange={(e) => setWww(e.target.checked)} className="accent-orange-500" />
            Include www
          </label>
        </div>

        {/* Edge Shield Proxy (Admin Only) */}
        {isAdmin && (
          <div className="mt-3 rounded-lg border border-white/[0.08] bg-zinc-950/60 p-2.5 space-y-1.5">
            <div className="flex items-center justify-between text-xs font-medium text-zinc-300">
              <span className="inline-flex items-center gap-1.5">
                <ShieldCheck className="h-3.5 w-3.5 text-emerald-400" />
                Edge Shield Proxy
              </span>
              <span className="text-[11px] text-zinc-500 font-normal">Origin Masking</span>
            </div>
            <div className="pt-0.5">
              <ShadcnSelect
                value={shieldNodeId ?? ''}
                onChange={(val) => setShieldNodeId(val ? Number(val) : null)}
                options={[
                  { value: '', label: 'Direct Origin (Server IP)' },
                  ...shieldNodes.map((s) => ({
                    value: s.id,
                    label: s.name,
                    sublabel: s.ip,
                    disabled: s.status !== 'connected',
                    badge:
                      s.status !== 'connected'
                        ? {
                            text: s.status === 'pending' ? 'Pending Setup' : 'Offline',
                            tone: s.status === 'pending' ? ('warning' as const) : ('danger' as const),
                          }
                        : undefined,
                  })),
                ]}
              />
            </div>
          </div>
        )}
      </Card>

      <Card title="Domains" actions={<Pill>{app.domains.length}</Pill>}>
        {app.domains.length === 0 ? (
          <EmptyState icon={<Globe className="h-6 w-6" />} text="No domains yet." />
        ) : (
          <div className="space-y-2.5">
            {app.domains.map((d) => {
              const matchedShield = d.shield_node_id ? shieldNodes.find((s) => s.id === d.shield_node_id) : null;
              const isBroken = Boolean(matchedShield && matchedShield.status !== 'connected');

              return (
                <div
                  key={d.id}
                  className="rounded-xl border border-white/[0.08] bg-zinc-950/70 p-3 sm:p-3.5 transition-colors hover:border-white/[0.12] space-y-2.5"
                >
                  {/* Line 1: Domain Name (Full Width) + External link & Copy */}
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 flex-1 items-center gap-2">
                      <Globe className="h-4 w-4 shrink-0 text-emerald-400" />
                      <a
                        href={`${d.forceHttps ? 'https' : 'http'}://${d.domain}`}
                        target="_blank"
                        rel="noreferrer"
                        className="truncate font-mono text-xs font-semibold text-zinc-100 hover:text-orange-400 transition-colors"
                        title={d.domain}
                      >
                        {d.domain}
                      </a>
                      <a
                        href={`${d.forceHttps ? 'https' : 'http'}://${d.domain}`}
                        target="_blank"
                        rel="noreferrer"
                        className="shrink-0 text-zinc-500 hover:text-zinc-300 transition-colors"
                        title="Open in new tab"
                      >
                        <ExternalLink className="h-3 w-3" />
                      </a>
                      <button
                        type="button"
                        onClick={async () => {
                          const ok = await copyToClipboard(d.domain);
                          if (ok) {
                            setCopiedDomainId(d.id);
                            setTimeout(() => setCopiedDomainId((cur) => (cur === d.id ? null : cur)), 1500);
                          }
                        }}
                        className={`shrink-0 transition-colors cursor-pointer ${
                          copiedDomainId === d.id ? 'text-emerald-400' : 'text-zinc-500 hover:text-zinc-300'
                        }`}
                        title={copiedDomainId === d.id ? 'Copied!' : 'Copy domain'}
                      >
                        {copiedDomainId === d.id ? (
                          <Check className="h-3.5 w-3.5 text-emerald-400 animate-in zoom-in-75 duration-150" />
                        ) : (
                          <Copy className="h-3 w-3" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* Line 2: HTTPS / Primary / Delete in one row */}
                  <div className="flex items-center justify-between gap-2 pt-0.5">
                    <div className="flex flex-wrap items-center gap-1.5 text-xs">
                      <span
                        className={`inline-flex items-center rounded-md border px-2 py-0.5 text-[11px] font-semibold ${
                          d.forceHttps
                            ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-400'
                            : 'border-amber-500/25 bg-amber-500/10 text-amber-400'
                        }`}
                      >
                        {d.forceHttps ? 'HTTPS' : 'HTTP'}
                      </span>

                      {d.isPrimary && (
                        <span className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-semibold text-amber-300">
                          <Star className="h-2.5 w-2.5 fill-amber-300 text-amber-300" />
                          Primary
                        </span>
                      )}

                      {d.enableWww && (
                        <span className="inline-flex items-center rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-0.5 font-mono text-[11px] text-zinc-400">
                          +www
                        </span>
                      )}
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0">
                      {!d.isPrimary && (
                        <button
                          type="button"
                          disabled={busy === 'pri' + d.id}
                          onClick={() => makePrimary(d.id)}
                          title="Set as primary domain"
                          className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-300 hover:border-amber-500/40 hover:bg-amber-500/10 hover:text-amber-300 transition-colors cursor-pointer disabled:opacity-50"
                        >
                          {busy === 'pri' + d.id ? (
                            <span className="h-3 w-3 animate-spin rounded-full border border-zinc-400 border-t-transparent" />
                          ) : (
                            <Star className="h-3 w-3 text-amber-400" />
                          )}
                          <span>Make primary</span>
                        </button>
                      )}

                      <button
                        type="button"
                        onClick={() => remove(d.id, d.domain)}
                        disabled={busy === 'del' + d.id}
                        title="Remove domain"
                        className="inline-flex h-7 w-7 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-300 transition-colors cursor-pointer disabled:opacity-50"
                      >
                        {busy === 'del' + d.id ? (
                          <span className="h-3 w-3 animate-spin rounded-full border border-rose-400 border-t-transparent" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* Line 3: Dedicated Full-Width Route Selector (Admin Only) */}
                  {isAdmin && (
                    <div className="pt-0.5">
                      <ShadcnSelect
                        value={d.shield_node_id ?? ''}
                        disabled={busy === 'shield' + d.id}
                        tone={isBroken ? 'danger' : d.shield_node_id ? 'cyan' : 'default'}
                        title={
                          isBroken
                            ? `Warning: Shield node '${matchedShield?.name}' is ${matchedShield?.status}!`
                            : 'Change Route: Direct Origin or Edge Shield'
                        }
                        onChange={async (val) => {
                          const newId = val ? Number(val) : null;
                          if (newId) {
                            const target = shieldNodes.find((s) => s.id === newId);
                            if (target && target.status !== 'connected') {
                              toast.error(
                                `Cannot route through '${target.name}': Node is ${target.status}. Only connected shield nodes can route traffic.`
                              );
                              return;
                            }
                          }
                          setBusy('shield' + d.id);
                          const res = await backendApi.domainEdit(app.id, d.id, {
                            domain: d.domain,
                            service: 'php',
                            port: 80,
                            enable_https: d.forceHttps,
                            enable_www: d.enableWww,
                            shield_node_id: newId,
                          });
                          setBusy('');
                          if (!res.ok) {
                            toast.error(res.error || 'Failed to update route');
                            return;
                          }
                          const updated = app.domains.map((dom) =>
                            dom.id === d.id ? { ...dom, shield_node_id: newId } : dom
                          );
                          onUpdateApp?.({ ...app, domains: updated });
                          toast.success(`Updated ${d.domain} route`);
                          await onChanged();
                        }}
                        options={[
                          { value: '', label: 'Direct Origin (Server IP)' },
                          ...shieldNodes.map((s) => ({
                            value: s.id,
                            label: s.name,
                            sublabel: s.ip,
                            disabled: s.status !== 'connected' && s.id !== d.shield_node_id,
                            badge:
                              s.status !== 'connected'
                                ? {
                                    text: s.status === 'pending' ? 'Pending Setup' : 'Offline',
                                    tone: s.status === 'pending' ? ('warning' as const) : ('danger' as const),
                                  }
                                : undefined,
                          })),
                        ]}
                      />
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </Card>
    </div>
  );
};
