import React, { useState } from 'react';
import { ExternalLink, Globe, Plus, Star, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi, getServerPublicIp } from '../../lib/backendApi';
import { Card, Field, GhostButton, Pill, PrimaryButton, inputCls } from './ui';

const DOMAIN_RE = /^(?=.{3,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i;

export const PhpDomainsTab: React.FC<{ app: AppStack; onChanged: () => void | Promise<void> }> = ({ app, onChanged }) => {
  const [domain, setDomain] = useState('');
  const [https, setHttps] = useState(true);
  const [www, setWww] = useState(false);
  const [busy, setBusy] = useState('');
  const serverIp = getServerPublicIp();

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
      <Card title="Domains" description="Point a domain's DNS A record to this server, add it here, and HTTPS is issued automatically.">
        {app.domains.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <Globe className="h-6 w-6 text-zinc-600" />
            <p className="text-xs text-zinc-400">No domain yet. Add one below to publish this site.</p>
          </div>
        ) : (
          <ul className="divide-y divide-white/[0.05]">
            {app.domains.map((d) => (
              <li key={d.id} className="flex flex-wrap items-center justify-between gap-3 py-2.5">
                <div className="flex min-w-0 items-center gap-2">
                  <Globe className="h-3.5 w-3.5 shrink-0 text-emerald-400" />
                  <a
                    href={`${d.forceHttps ? 'https' : 'http'}://${d.domain}`}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex min-w-0 items-center gap-1 truncate font-mono text-xs text-zinc-100 hover:text-orange-300"
                  >
                    {d.domain}
                    <ExternalLink className="h-3 w-3 shrink-0 opacity-60" />
                  </a>
                  {d.isPrimary && <Pill tone="orange">primary</Pill>}
                  {d.forceHttps ? <Pill tone="green">HTTPS</Pill> : <Pill tone="amber">HTTP only</Pill>}
                  {d.enableWww && <Pill>+ www</Pill>}
                </div>
                <div className="flex items-center gap-1.5">
                  {!d.isPrimary && (
                    <GhostButton busy={busy === 'pri' + d.id} onClick={() => makePrimary(d.id)}>
                      <Star className="h-3.5 w-3.5" /> Make primary
                    </GhostButton>
                  )}
                  <GhostButton tone="danger" busy={busy === 'del' + d.id} onClick={() => remove(d.id, d.domain)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </GhostButton>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <Card title="Add a domain">
        <div className="grid gap-4 sm:grid-cols-[1fr_auto] sm:items-end">
          <Field
            label="Domain name"
            hint={serverIp ? `DNS: create an A record pointing to ${serverIp}.` : 'DNS: create an A record pointing to this server.'}
          >
            <input
              className={inputCls}
              placeholder="example.com"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && void add()}
            />
          </Field>
          <PrimaryButton busy={busy === 'add'} onClick={add}>
            <Plus className="h-3.5 w-3.5" /> Add domain
          </PrimaryButton>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-5 text-xs text-zinc-300">
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input type="checkbox" checked={https} onChange={(e) => setHttps(e.target.checked)} className="accent-orange-500" />
            Enable HTTPS (Let's Encrypt)
          </label>
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input type="checkbox" checked={www} onChange={(e) => setWww(e.target.checked)} className="accent-orange-500" />
            Also serve www.
          </label>
        </div>
      </Card>
    </div>
  );
};
