import React, { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import type { CaddyOptions } from '../../lib/api/caddyApi';
import { Card, ErrorBlock, Field, GhostButton, LoadingBlock, Pill, PrimaryButton, inputCls, textareaCls } from '../php/ui';

const PROFILES: Array<{ id: string; title: string; text: string }> = [
  { id: 'default', title: 'Default', text: "Caddy's own settings." },
  { id: 'balanced', title: 'Balanced', text: 'Shorter header wait, 2 minute keep-alive. A good start for most servers.' },
  { id: 'high', title: 'High traffic', text: 'Long keep-alive so busy clients reuse connections, quick header timeout.' },
  { id: 'strict', title: 'Strict', text: 'Short timeouts and small headers for a server open to the whole internet.' },
];

const FIELDS: Array<{ key: keyof CaddyOptions; label: string; placeholder: string }> = [
  { key: 'read_header', label: 'Header timeout', placeholder: 'default 1m' },
  { key: 'read_body_idle', label: 'Upload pause limit', placeholder: 'default 1m' },
  { key: 'idle', label: 'Keep-alive timeout', placeholder: 'default 5m' },
  { key: 'grace_period', label: 'Reload grace period', placeholder: 'default unlimited' },
  { key: 'max_header_size', label: 'Header size limit', placeholder: 'default 16KB' },
];

export const CaddyOptionsView: React.FC = () => {
  const [opts, setOpts] = useState<CaddyOptions | null>(null);
  const [profiles, setProfiles] = useState<Record<string, CaddyOptions>>({});
  const [cloudflare, setCloudflare] = useState<string[]>([]);
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [proxiesText, setProxiesText] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState<'' | 'save' | 'apply'>('');

  const load = async () => {
    const res = await backendApi.caddyOptions();
    if (!res.ok || !res.data) {
      setError(res.error || 'Could not load the options');
      return;
    }
    setError('');
    setOpts(res.data.options);
    setProfiles(res.data.profiles);
    setCloudflare(res.data.cloudflare_ranges);
    setLabels(res.data.labels);
    setProxiesText((res.data.options.trusted_proxies ?? []).join('\n'));
  };

  useEffect(() => {
    void load();
  }, []);

  if (error) return <ErrorBlock message={error} onRetry={load} />;
  if (!opts) return <LoadingBlock />;

  const set = <K extends keyof CaddyOptions>(k: K, v: CaddyOptions[K]) => setOpts({ ...opts, [k]: v, profile: 'custom' });
  const pick = (id: string) => {
    const p = profiles[id];
    if (!p) return;
    setOpts({ ...p, real_ip: opts.real_ip, trusted_proxies: opts.trusted_proxies, trusted_strict: opts.trusted_strict, profile: id });
  };

  const save = async (apply: boolean) => {
    if (apply && !window.confirm('Apply now? Caddy restarts and every site is unreachable for a few seconds.')) return;
    setBusy(apply ? 'apply' : 'save');
    const res = await backendApi.caddyOptionsSave(
      { ...opts, trusted_proxies: proxiesText.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean) },
      apply
    );
    setBusy('');
    if (!res.ok || !res.data) {
      toast.error(res.error || 'Could not save');
      return;
    }
    toast.success(apply ? 'Saved. Caddy is restarting with the new options…' : 'Saved. Click “Save and apply” when you are ready.');
    await load();
  };

  const labelEntries = Object.entries(labels);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold tracking-tight text-zinc-100">Server options</h1>
        <div className="flex items-center gap-2">
          <GhostButton busy={busy === 'save'} onClick={() => save(false)}>Save</GhostButton>
          <PrimaryButton busy={busy === 'apply'} onClick={() => save(true)}>Save and apply</PrimaryButton>
        </div>
      </div>

      <Card title="Profile">
        <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2 xl:grid-cols-4">
          {PROFILES.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => pick(p.id)}
              className={`cursor-pointer rounded-lg border p-3 text-left transition-colors ${
                opts.profile === p.id ? 'border-orange-500/50 bg-orange-500/10' : 'border-white/[0.08] bg-[#09090b] hover:bg-zinc-900'
              }`}
            >
              <div className="text-xs font-semibold text-zinc-100">{p.title}</div>
              <div className="mt-1 text-[11px] leading-snug text-zinc-400">{p.text}</div>
            </button>
          ))}
        </div>
      </Card>

      <Card title="Timeouts and limits" actions={opts.profile === 'custom' ? <Pill tone="orange">custom</Pill> : undefined}>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {FIELDS.map((f) => (
            <Field key={f.key} label={f.label}>
              <input
                className={`${inputCls} font-mono`}
                value={String(opts[f.key] ?? '')}
                placeholder={f.placeholder}
                onChange={(e) => set(f.key, e.target.value as never)}
              />
            </Field>
          ))}
        </div>
        <p className="mt-3 text-[11px] text-zinc-500">Durations look like 15s, 2m or 1h. Leave a field empty to keep Caddy's default.</p>
      </Card>

      <Card title="Real visitor IP">
        <div className="grid gap-2.5 sm:grid-cols-3">
          {(
            [
              { id: 'none', title: 'Direct', text: 'Visitors connect to this server straight away.' },
              { id: 'cloudflare', title: 'Behind Cloudflare', text: `Trust Cloudflare's ${cloudflare.length} ranges to read the visitor's address.` },
              { id: 'custom', title: 'Behind my own proxy', text: 'Trust the load balancer or CDN addresses you list.' },
            ] as const
          ).map((m) => (
            <button
              key={m.id}
              type="button"
              onClick={() => setOpts({ ...opts, real_ip: m.id })}
              className={`cursor-pointer rounded-lg border p-3 text-left transition-colors ${
                opts.real_ip === m.id ? 'border-orange-500/50 bg-orange-500/10' : 'border-white/[0.08] bg-[#09090b] hover:bg-zinc-900'
              }`}
            >
              <div className="text-xs font-semibold text-zinc-100">{m.title}</div>
              <div className="mt-1 text-[11px] leading-snug text-zinc-400">{m.text}</div>
            </button>
          ))}
        </div>
        {opts.real_ip === 'custom' && (
          <div className="mt-4">
            <Field label="Trusted proxy addresses">
              <textarea className={textareaCls} rows={4} value={proxiesText} onChange={(e) => setProxiesText(e.target.value)} placeholder={'10.0.0.5\n192.168.0.0/16'} />
            </Field>
          </div>
        )}
        {opts.real_ip !== 'none' && (
          <label className="mt-4 flex cursor-pointer items-center gap-2 text-xs text-zinc-300">
            <input type="checkbox" checked={opts.trusted_strict} onChange={(e) => setOpts({ ...opts, trusted_strict: e.target.checked })} className="accent-orange-500" />
            Strict: read forwarded headers from the right (use when several proxies are chained)
          </label>
        )}
        <p className="mt-3 text-[11px] text-zinc-500">
          Without this, sites behind a CDN see the CDN's address for every visitor, so IP block lists and logs do not match real people.
        </p>
      </Card>

      <details className="rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <summary className="cursor-pointer px-4 py-3 text-xs font-medium text-zinc-300">Labels written to Caddy ({labelEntries.length})</summary>
        <pre className="max-h-64 overflow-auto border-t border-white/[0.06] p-4 font-mono text-[11px] leading-relaxed text-zinc-400">
          {labelEntries.length ? labelEntries.map(([k, v]) => `${k}=${v}`).join('\n') : 'None: Caddy uses its defaults.'}
        </pre>
      </details>
    </div>
  );
};
