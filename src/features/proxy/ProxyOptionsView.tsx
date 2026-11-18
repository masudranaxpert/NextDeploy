import React, { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import {
  normalizeProxyEngine,
  resolveCapabilities,
  type ProxyEngineName,
  type ProxyOptions,
} from '../../lib/api/proxyApi';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { ProxyEngineBadge } from '../../components/proxy/ProxyEngineBadge';
import { formatProxyEngineLabel } from '../../components/shield/shieldProxyDisplay';
import {
  Card,
  ErrorBlock,
  Field,
  GhostButton,
  LoadingBlock,
  PrimaryButton,
  inputCls,
  textareaCls,
} from '../php/ui';
import { OPTION_FIELDS, OPTION_PROFILES, fieldHint } from './components/proxyOptionsMeta';

export const ProxyOptionsView: React.FC = () => {
  const [opts, setOpts] = useState<ProxyOptions | null>(null);
  const [profiles, setProfiles] = useState<Record<string, ProxyOptions>>({});
  const [cfRanges, setCfRanges] = useState<string[]>([]);
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [renderedConfig, setRenderedConfig] = useState<string>('');
  const [engine, setEngine] = useState<ProxyEngineName>('caddy');
  const [capsRaw, setCapsRaw] = useState<Record<string, boolean> | undefined>(undefined);
  const [proxiesText, setProxiesText] = useState('');
  const [busy, setBusy] = useState<'save' | 'apply' | ''>('');
  const [confirmApplyOpen, setConfirmApplyOpen] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setError('');
    const res = await backendApi.proxyOptions();
    if (!res.ok || !res.data?.options) {
      setError(res.error || 'Could not load proxy options.');
      return;
    }
    const d = res.data;
    setOpts(d.options);
    setProfiles(d.profiles || {});
    setCfRanges(d.cloudflare_ranges || []);
    setLabels(d.labels || {});
    setRenderedConfig(d.rendered_config || '');
    setProxiesText((d.options.trusted_proxies ?? []).join('\n'));
    if (d.engine) {
      setEngine(normalizeProxyEngine(d.engine));
      if (d.capabilities) setCapsRaw(d.capabilities as Record<string, boolean>);
    } else {
      const eng = await backendApi.proxyEngineStatus();
      if (eng.ok) {
        setEngine(normalizeProxyEngine(eng.engine || eng.data?.engine));
        if (eng.capabilities || eng.data?.capabilities) {
          setCapsRaw((eng.capabilities || eng.data?.capabilities) as Record<string, boolean>);
        }
      }
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (error) return <ErrorBlock message={error} onRetry={load} />;
  if (!opts) return <LoadingBlock label="Loading proxy options…" />;

  const caps = resolveCapabilities(engine, capsRaw);
  const engineLabel = formatProxyEngineLabel(engine);
  const set = <K extends keyof ProxyOptions>(k: K, v: ProxyOptions[K]) =>
    setOpts({ ...opts, [k]: v, profile: 'custom' });

  const pick = (id: string) => {
    const p = profiles[id];
    if (!p) return;
    setOpts({
      ...p,
      real_ip: opts.real_ip,
      trusted_proxies: opts.trusted_proxies,
      trusted_strict: opts.trusted_strict,
      shield_proxies: opts.shield_proxies,
      direct_cloudflare: opts.direct_cloudflare,
    });
  };

  const save = async (apply: boolean) => {
    setBusy(apply ? 'apply' : 'save');
    try {
      const res = await backendApi.proxyOptionsSave(
        {
          ...opts,
          trusted_proxies: proxiesText
            .split(/[\s,]+/)
            .map((x) => x.trim())
            .filter(Boolean),
        },
        apply
      );
      if (!res.ok) {
        toast.error(res.error || 'Could not save');
        return;
      }
      toast.success(
        apply
          ? `Saved. ${engineLabel} is applying the updated options…`
          : 'Saved. Click "Save and apply" when ready.'
      );
      await load();
    } finally {
      setBusy('');
    }
  };

  const labelEntries = Object.entries(labels);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <h1 className="text-lg font-semibold tracking-tight text-zinc-100">Proxy server options</h1>
          <ProxyEngineBadge engine={engine} experimental={caps.experimental} size="sm" />
        </div>
        <div className="flex items-center gap-2">
          <GhostButton busy={busy === 'save'} onClick={() => void save(false)}>
            Save
          </GhostButton>
          <PrimaryButton busy={busy === 'apply'} onClick={() => setConfirmApplyOpen(true)}>
            Save and apply
          </PrimaryButton>
        </div>
      </div>

      <Card title="Profile" description="Pick a preset or tune individual timeout and buffer values below.">
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {OPTION_PROFILES.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => pick(p.id)}
              className={`min-h-[44px] rounded-lg border px-3 py-2.5 text-left transition cursor-pointer ${
                opts.profile === p.id
                  ? 'border-orange-500/50 bg-orange-500/10'
                  : 'border-white/[0.08] bg-[#09090b] hover:bg-zinc-900'
              }`}
            >
              <div className="text-xs font-medium text-zinc-100">{p.title}</div>
              <div className="mt-0.5 text-[11px] text-zinc-400">{p.text}</div>
            </button>
          ))}
        </div>
      </Card>

      <Card title="Timeouts and limits" description={`Per-engine capability checks run automatically for ${engineLabel}.`}>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {OPTION_FIELDS.map((f) => {
            const { disabled, note } = fieldHint(f, engine, caps);
            return (
              <div key={f.key} className={disabled ? 'opacity-60' : ''}>
                <Field label={f.label}>
                  <input
                    className={inputCls}
                    disabled={disabled}
                    value={(opts[f.key] as string) ?? ''}
                    placeholder={f.placeholder}
                    onChange={(e) => set(f.key, e.target.value as never)}
                  />
                </Field>
                {note && <p className="mt-1 text-[11px] text-amber-200">{note}</p>}
              </div>
            );
          })}
        </div>
        <p className="mt-3 text-[11px] text-zinc-400">
          Durations look like 15s, 2m, or 1h. Leave a field empty to keep {engineLabel}&apos;s default.
        </p>
      </Card>

      <Card
        title="Real visitor IP (trusted proxies)"
        description="Who can pass X-Forwarded-For so apps and Shield routing see real client IPs."
      >
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          {(
            [
              ['none', 'Off', 'Direct traffic only'],
              ['cloudflare', 'Cloudflare', `${cfRanges.length} edge IP ranges`],
              ['custom', 'Custom', 'List IPs / CIDRs manually'],
            ] as const
          ).map(([id, title, sub]) => (
            <button
              key={id}
              type="button"
              onClick={() => setOpts({ ...opts, real_ip: id })}
              className={`min-h-[44px] rounded-lg border px-3 py-2.5 text-left transition cursor-pointer ${
                opts.real_ip === id
                  ? 'border-orange-500/50 bg-orange-500/10'
                  : 'border-white/[0.08] bg-[#09090b] hover:bg-zinc-900'
              }`}
            >
              <div className="text-xs font-medium text-zinc-100">{title}</div>
              <div className="mt-0.5 text-[11px] text-zinc-400">{sub}</div>
            </button>
          ))}
        </div>

        {opts.real_ip === 'custom' && (
          <div className="mt-3">
            <Field label="Trusted proxy addresses (one per line or comma-separated)">
              <textarea
                className={textareaCls}
                rows={4}
                value={proxiesText}
                onChange={(e) => setProxiesText(e.target.value)}
                placeholder={'10.0.0.5\n192.168.0.0/16'}
              />
            </Field>
          </div>
        )}

        {opts.real_ip !== 'none' && (
          <div className="mt-3 space-y-1">
            <label
              className={`flex items-center gap-2 text-xs ${
                caps.trusted_strict ? 'text-zinc-300 cursor-pointer' : 'text-zinc-400 cursor-not-allowed opacity-70'
              }`}
            >
              <input
                type="checkbox"
                disabled={!caps.trusted_strict}
                checked={opts.trusted_strict}
                onChange={(e) => setOpts({ ...opts, trusted_strict: e.target.checked })}
                className="accent-orange-500"
              />
              Strict mode: reject forwarded headers from unlisted sources
            </label>
            {!caps.trusted_strict && (
              <p className="text-[11px] text-amber-200 pl-6">
                Strict header stripping is unavailable on {engineLabel}.
              </p>
            )}
          </div>
        )}

        {((opts.shield_proxies?.length ?? 0) > 0 || opts.direct_cloudflare) && (
          <div className="mt-3 space-y-2 rounded-lg border border-emerald-500/20 bg-emerald-500/5 p-3 text-xs text-emerald-200">
            {(opts.shield_proxies?.length ?? 0) > 0 && (
              <div>
                Connected Shield proxies trusted automatically: {(opts.shield_proxies ?? []).join(', ')}
              </div>
            )}
            {opts.direct_cloudflare && (
              <div>Direct Cloudflare Real IP is active on one or more direct domains.</div>
            )}
          </div>
        )}
      </Card>

      <details className="rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <summary className="cursor-pointer px-4 py-3 text-xs font-medium text-zinc-300">
          {renderedConfig
            ? `Generated global config (${engineLabel})`
            : `Generated global proxy options (${labelEntries.length})`}
        </summary>
        <pre className="max-h-64 overflow-x-auto border-t border-white/[0.06] px-4 py-3 font-mono text-[11px] text-zinc-400">
          {renderedConfig
            ? renderedConfig
            : labelEntries.length
            ? labelEntries.map(([k, v]) => `${k}=${v}`).join('\n')
            : `None: ${engineLabel} uses its defaults.`}
        </pre>
      </details>

      <ShadcnDialog
        open={confirmApplyOpen}
        onClose={() => setConfirmApplyOpen(false)}
        title={`Apply options to ${engineLabel}?`}
        description="Applying global timeouts or trusted proxies reloads or restarts the active reverse proxy and may briefly interrupt live connections."
        footer={
          <div className="flex justify-end gap-2">
            <GhostButton onClick={() => setConfirmApplyOpen(false)}>Cancel</GhostButton>
            <PrimaryButton
              busy={busy === 'apply'}
              onClick={() => {
                setConfirmApplyOpen(false);
                void save(true);
              }}
            >
              Apply Now
            </PrimaryButton>
          </div>
        }
      >
        <p className="text-xs text-zinc-300">
          Engine: <strong className="text-zinc-100">{engineLabel}</strong>. Profile:{' '}
          <strong className="font-mono text-zinc-100">{opts.profile}</strong>.
        </p>
      </ShadcnDialog>
    </div>
  );
};
