import React, { useState } from 'react';
import { Check, Copy, ExternalLink, Globe, Plus, ShieldCheck, Star, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi, getServerPublicIp } from '../../lib/backendApi';
import { Pill } from './ui';

const DOMAIN_RE = /^(?=.{3,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i;

export const PhpDomainsTab: React.FC<{ app: AppStack; onChanged: () => void | Promise<void> }> = ({
  app,
  onChanged,
}) => {
  const [domain, setDomain] = useState('');
  const [https, setHttps] = useState(true);
  const [www, setWww] = useState(false);
  const [busy, setBusy] = useState('');
  const [copiedIp, setCopiedIp] = useState(false);
  const serverIp = getServerPublicIp();

  const handleCopyIp = (e: React.MouseEvent) => {
    e.preventDefault();
    if (!serverIp) return;
    navigator.clipboard?.writeText(serverIp);
    setCopiedIp(true);
    toast.success(`Copied IP: ${serverIp}`);
    setTimeout(() => setCopiedIp(false), 2000);
  };

  const add = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    const d = domain.trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/.*$/, '');
    if (!DOMAIN_RE.test(d)) {
      toast.error('Enter a valid domain name (e.g. example.com)');
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
      toast.error(res.error || 'Failed to add domain');
      return;
    }
    setDomain('');
    toast.success(`Domain ${d} connected`);
    await onChanged();
  };

  const remove = async (id: string, name: string) => {
    if (!window.confirm(`Are you sure you want to remove ${name}?`)) return;
    setBusy('del' + id);
    const res = await backendApi.domainDelete(app.id, id);
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Failed to remove domain');
    } else {
      toast.success(`Domain ${name} removed`);
    }
    await onChanged();
  };

  const makePrimary = async (id: string) => {
    setBusy('pri' + id);
    const res = await backendApi.domainSetPrimary(app.id, id);
    setBusy('');
    if (!res.ok) {
      toast.error(res.error || 'Failed to set primary domain');
    } else {
      toast.success('Primary domain updated');
    }
    await onChanged();
  };

  return (
    <div className="space-y-6">
      {/* Existing Domains Card */}
      <div className="rounded-xl border border-white/[0.08] bg-[#111115] p-5 shadow-sm">
        <div className="flex items-center justify-between pb-4 border-b border-white/[0.06]">
          <div>
            <h3 className="text-sm font-semibold text-zinc-100">Connected Domains</h3>
            <p className="mt-0.5 text-xs text-zinc-400">
              Domains mapped to this website with automatic SSL certificates
            </p>
          </div>
          <span className="rounded-full bg-white/[0.06] px-2.5 py-0.5 font-mono text-xs font-medium text-zinc-300">
            {app.domains.length}
          </span>
        </div>

        {app.domains.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-10 text-center">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-white/[0.08] bg-white/[0.02] text-zinc-500 mb-2.5">
              <Globe className="h-5 w-5" />
            </div>
            <p className="text-xs font-medium text-zinc-300">No domains added yet</p>
            <p className="text-[11px] text-zinc-500 mt-0.5">
              Add your domain below to make this site accessible on the web.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-white/[0.05]">
            {app.domains.map((d) => (
              <div
                key={d.id}
                className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 py-3.5 first:pt-4 last:pb-1"
              >
                <div className="flex min-w-0 items-center gap-3">
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-emerald-500/20 bg-emerald-500/10 text-emerald-400">
                    <Globe className="h-4 w-4" />
                  </div>
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <a
                        href={`${d.forceHttps ? 'https' : 'http'}://${d.domain}`}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 font-mono text-xs font-semibold text-zinc-100 hover:text-orange-400 transition-colors truncate"
                      >
                        <span>{d.domain}</span>
                        <ExternalLink className="h-3 w-3 shrink-0 opacity-60" />
                      </a>
                      {d.isPrimary && (
                        <span className="rounded-md border border-orange-500/30 bg-orange-500/10 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-orange-400">
                          Primary
                        </span>
                      )}
                      {d.forceHttps ? (
                        <span className="inline-flex items-center gap-1 rounded-md border border-emerald-500/25 bg-emerald-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-emerald-400">
                          <ShieldCheck className="h-2.5 w-2.5" />
                          HTTPS
                        </span>
                      ) : (
                        <span className="rounded-md border border-amber-500/25 bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-amber-400">
                          HTTP
                        </span>
                      )}
                      {d.enableWww && (
                        <span className="rounded-md border border-white/[0.08] bg-white/[0.04] px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">
                          +www
                        </span>
                      )}
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2 self-end sm:self-center shrink-0">
                  {!d.isPrimary && (
                    <button
                      type="button"
                      disabled={busy === 'pri' + d.id}
                      onClick={() => makePrimary(d.id)}
                      className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-300 hover:border-white/[0.16] hover:bg-zinc-800 transition-colors cursor-pointer"
                    >
                      <Star className="h-3 w-3 text-amber-400" />
                      <span>Make Primary</span>
                    </button>
                  )}
                  <button
                    type="button"
                    disabled={busy === 'del' + d.id}
                    onClick={() => remove(d.id, d.domain)}
                    className="flex h-7 w-7 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-400 transition-colors cursor-pointer"
                    title="Remove domain"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Add Domain Form Card */}
      <div className="rounded-xl border border-white/[0.08] bg-[#111115] p-5 shadow-sm">
        <div className="mb-4">
          <h3 className="text-sm font-semibold text-zinc-100">Add Domain</h3>
          <p className="mt-0.5 text-xs text-zinc-400">
            Connect a custom domain or subdomain to this site
          </p>
        </div>

        <form onSubmit={add} className="space-y-4">
          <div>
            <div className="flex items-center justify-between gap-2 mb-1.5">
              <label className="text-xs font-medium text-zinc-300">Domain Name</label>
              {serverIp && (
                <button
                  type="button"
                  onClick={handleCopyIp}
                  className="inline-flex items-center gap-1 rounded border border-white/[0.07] bg-[#09090b] px-2 py-0.5 font-mono text-[10px] text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
                  title="Click to copy IP for DNS A Record"
                >
                  <span>A Record:</span>
                  <span className="font-semibold text-orange-400">{serverIp}</span>
                  {copiedIp ? (
                    <Check className="h-2.5 w-2.5 text-emerald-400 ml-0.5" />
                  ) : (
                    <Copy className="h-2.5 w-2.5 text-zinc-500 ml-0.5" />
                  )}
                </button>
              )}
            </div>
            <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-2.5">
              <div className="relative flex-1">
                <input
                  type="text"
                  value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                  placeholder="example.com or sub.example.com"
                  className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 text-xs font-mono text-zinc-100 placeholder-zinc-500 focus:border-orange-500/60 focus:outline-none transition-colors"
                />
              </div>
              <button
                type="submit"
                disabled={busy === 'add' || !domain.trim()}
                className="inline-flex h-9 items-center justify-center gap-1.5 rounded-lg bg-orange-600 px-4 text-xs font-semibold text-white hover:bg-orange-500 disabled:opacity-50 transition-all cursor-pointer shadow-xs whitespace-nowrap"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>{busy === 'add' ? 'Connecting...' : 'Add Domain'}</span>
              </button>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-5 pt-3 border-t border-white/[0.06] text-xs text-zinc-300">
            <label className="inline-flex cursor-pointer items-center gap-2 select-none">
              <input
                type="checkbox"
                checked={https}
                onChange={(e) => setHttps(e.target.checked)}
                className="h-3.5 w-3.5 rounded border-zinc-700 bg-zinc-900 accent-orange-500 cursor-pointer"
              />
              <span>Automatic SSL Certificate (HTTPS)</span>
            </label>

            <label className="inline-flex cursor-pointer items-center gap-2 select-none">
              <input
                type="checkbox"
                checked={www}
                onChange={(e) => setWww(e.target.checked)}
                className="h-3.5 w-3.5 rounded border-zinc-700 bg-zinc-900 accent-orange-500 cursor-pointer"
              />
              <span>Redirect www to root domain</span>
            </label>
          </div>
        </form>
      </div>
    </div>
  );
};
