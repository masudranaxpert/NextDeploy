import React, { useState } from 'react';
import { ExternalLink, Globe, Plus, Star, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { Card, EmptyState, GhostButton, Pill, PrimaryButton, inputCls } from './ui';

const DOMAIN_RE = /^(?=.{3,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i;

export const PhpDomainsTab: React.FC<{ app: AppStack; onChanged: () => void | Promise<void> }> = ({ app, onChanged }) => {
  const [domain, setDomain] = useState('');
  const [https, setHttps] = useState(true);
  const [www, setWww] = useState(false);
  const [busy, setBusy] = useState('');

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
      is_primary: app.domains.length === 0 || undefined,
    });
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Could not add the domain');
      return;
    }
    setDomain('');
    toast.success(`${d} added`);
    await onChanged();
  };

  const remove = async (id: string, name: string) => {
    if (!window.confirm(`Remove ${name} from this site?`)) return;
    setBusy('del' + id);
    const res = await backendApi.domainDelete(app.id, id);
    setBusy('');
    if (!res.ok) toast.error(res.error || 'Could not remove the domain');
    else toast.success(`${name} removed`);
    await onChanged();
  };

  const makePrimary = async (id: string) => {
    setBusy('pri' + id);
    const res = await backendApi.domainSetPrimary(app.id, id);
    setBusy('');
    if (!res.ok) toast.error(res.error || 'Could not change the primary domain');
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
      </Card>

      <Card title="Domains" actions={<Pill>{app.domains.length}</Pill>}>
        {app.domains.length === 0 ? (
          <EmptyState icon={<Globe className="h-6 w-6" />} text="No domains yet." />
        ) : (
          <ul>
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
                  <div className="flex flex-wrap items-center gap-1.5">
                    {d.isPrimary && <Pill tone="orange">primary</Pill>}
                    {d.forceHttps ? <Pill tone="green">HTTPS</Pill> : <Pill tone="amber">HTTP</Pill>}
                    {d.enableWww && <Pill>www</Pill>}
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
