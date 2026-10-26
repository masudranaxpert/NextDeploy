import React, { useContext, useEffect, useState } from 'react';
import { ExternalLink, Globe, Plus, ShieldCheck, Star, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { ShieldNode } from '../../types/shield';
import { backendApi } from '../../lib/backendApi';
import { OwnerContext } from '../../components/ui/OwnerBadge';
import { Card, EmptyState, GhostButton, Pill, PrimaryButton, inputCls } from './ui';

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
          <div className="mt-3 rounded-lg border border-white/[0.08] bg-zinc-950/60 p-2.5">
            <div className="flex items-center justify-between text-xs font-medium text-zinc-300">
              <span className="inline-flex items-center gap-1.5">
                <ShieldCheck className="h-3.5 w-3.5 text-emerald-400" />
                Edge Shield Proxy
              </span>
              <span className="text-[11px] text-zinc-500 font-normal">Origin Masking</span>
            </div>
            <select
              value={shieldNodeId ?? ''}
              onChange={(e) => setShieldNodeId(e.target.value ? Number(e.target.value) : null)}
              className="w-full mt-1.5 h-8 px-2.5 rounded-md bg-[#09090b] border border-white/[0.08] text-xs text-zinc-200 focus:outline-none focus:border-emerald-500 cursor-pointer"
            >
              <option value="">Direct Origin (Server IP)</option>
              {shieldNodes.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} ({s.ip})
                </option>
              ))}
            </select>
          </div>
        )}
      </Card>

      <Card title="Domains" actions={<Pill>{app.domains.length}</Pill>}>
        {app.domains.length === 0 ? (
          <EmptyState icon={<Globe className="h-6 w-6" />} text="No domains yet." />
        ) : (
          <ul className="divide-y divide-white/[0.06]">
            {app.domains.map((d) => (
              <li key={d.id} className="py-3">
                <a
                  href={`${d.forceHttps ? 'https' : 'http'}://${d.domain}`}
                  target="_blank"
                  rel="noreferrer"
                  className="flex min-w-0 items-center gap-1.5 font-mono text-xs text-zinc-100 hover:text-orange-300"
                >
                  <Globe className="h-3.5 w-3.5 shrink-0 text-emerald-400" />
                  <span className="truncate">{d.domain}</span>
                  <ExternalLink className="h-3 w-3 shrink-0 opacity-60" />
                </a>
                <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-1.5 text-xs">
                    {d.isPrimary && <Pill tone="orange">primary</Pill>}
                    {d.forceHttps ? <Pill tone="green">HTTPS</Pill> : <Pill tone="amber">HTTP</Pill>}
                    {d.enableWww && <Pill>www</Pill>}

                    {/* Shield Routing status for admin */}
                    {isAdmin && (
                      <select
                        value={d.shield_node_id ?? ''}
                        disabled={busy === 'shield' + d.id}
                        onChange={async (e) => {
                          const val = e.target.value;
                          const newId = val ? Number(val) : null;
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
                          toast.success(`Updated ${d.domain} shield route`);
                          await onChanged();
                        }}
                        className={`h-6 rounded-md border px-2 text-[11px] font-medium transition-colors cursor-pointer focus:outline-none ${
                          d.shield_node_id
                            ? 'border-cyan-500/30 bg-cyan-500/10 text-cyan-300 focus:border-cyan-400'
                            : 'border-white/[0.08] bg-zinc-900 text-zinc-400 hover:text-zinc-200 focus:border-zinc-700'
                        }`}
                        title="Change Route: Direct Origin or Edge Shield"
                      >
                        <option value="">Direct Origin</option>
                        {shieldNodes.map((s) => (
                          <option key={s.id} value={s.id}>
                            Shield: {s.name} ({s.ip})
                          </option>
                        ))}
                      </select>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    {!d.isPrimary && (
                      <GhostButton busy={busy === 'pri' + d.id} onClick={() => makePrimary(d.id)} title="Make primary">
                        <Star className="h-3.5 w-3.5" /> Make primary
                      </GhostButton>
                    )}
                    <button
                      type="button"
                      onClick={() => remove(d.id, d.domain)}
                      disabled={busy === 'del' + d.id}
                      title="Remove"
                      className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-rose-500/30 bg-rose-500/10 text-rose-300 transition-colors hover:bg-rose-500/20 cursor-pointer disabled:opacity-50"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
};
