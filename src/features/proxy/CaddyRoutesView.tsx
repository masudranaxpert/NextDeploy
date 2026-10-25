import React, { useEffect, useState } from 'react';
import { ArrowRightLeft, ExternalLink, Pencil, Plus, Trash2, Waypoints } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../lib/backendApi';
import type { ProxyRoute } from '../../lib/api/caddyApi';
import { ShadcnDialog, ShadcnSwitch } from '../../components/ui/Primitives';
import { Card, EmptyState, ErrorBlock, Field, GhostButton, LoadingBlock, Pill, PrimaryButton, inputCls, selectCls } from '../php/ui';

const blank: Partial<ProxyRoute> = {
  kind: 'proxy', domain: '', target: '', redirect_code: 301, keep_path: true,
  enable_https: true, enable_www: false, skip_tls_verify: false, enabled: true, note: '',
};

export const CaddyRoutesView: React.FC = () => {
  const [routes, setRoutes] = useState<ProxyRoute[] | null>(null);
  const [error, setError] = useState('');
  const [form, setForm] = useState<Partial<ProxyRoute> | null>(null);
  const [saving, setSaving] = useState(false);

  const load = async () => {
    const res = await backendApi.caddyRoutes();
    if (!res.ok || !res.data) {
      setError(res.error || 'Could not load the routes');
      return;
    }
    setError('');
    setRoutes(res.data.routes ?? []);
  };

  useEffect(() => {
    void load();
  }, []);

  if (error) return <ErrorBlock message={error} onRetry={load} />;
  if (!routes) return <LoadingBlock />;

  const save = async () => {
    if (!form) return;
    setSaving(true);
    const res = await backendApi.caddyRouteSave(form);
    setSaving(false);
    if (!res.ok) {
      toast.error(res.error || 'Could not save the route');
      return;
    }
    toast.success(form.id ? 'Route updated' : 'Route added');
    setForm(null);
    await load();
  };

  const toggle = async (r: ProxyRoute, enabled: boolean) => {
    setRoutes(routes.map((x) => (x.id === r.id ? { ...x, enabled } : x)));
    const res = await backendApi.caddyRouteToggle(r.id, enabled);
    if (!res.ok) toast.error(res.error || 'Could not change the route');
    await load();
  };

  const remove = async (r: ProxyRoute) => {
    if (!window.confirm(`Delete the route for ${r.domain}?`)) return;
    const res = await backendApi.caddyRouteDelete(r.id);
    if (!res.ok) toast.error(res.error || 'Could not delete the route');
    await load();
  };

  const set = (patch: Partial<ProxyRoute>) => setForm({ ...form, ...patch });
  const isProxy = (form?.kind ?? 'proxy') === 'proxy';

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold tracking-tight text-zinc-100">Routes and redirects</h1>
        <PrimaryButton onClick={() => setForm({ ...blank })}>
          <Plus className="h-3.5 w-3.5" /> Add route
        </PrimaryButton>
      </div>

      <Card title="Routes" actions={<Pill>{routes.length}</Pill>}>
        {routes.length === 0 ? (
          <EmptyState icon={<Waypoints className="h-6 w-6" />} text="No routes yet." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="text-[11px] text-zinc-500">
                <tr>
                  <th className="py-2 pr-4 font-medium">Domain</th>
                  <th className="py-2 pr-4 font-medium">Type</th>
                  <th className="py-2 pr-4 font-medium">Target</th>
                  <th className="py-2 pr-4 font-medium">On</th>
                  <th className="py-2 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {routes.map((r) => (
                  <tr key={r.id}>
                    <td className="py-2.5 pr-4">
                      <a
                        href={`${r.enable_https ? 'https' : 'http'}://${r.domain}`}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 font-mono text-zinc-100 hover:text-orange-300"
                      >
                        {r.domain}
                        <ExternalLink className="h-3 w-3 opacity-60" />
                      </a>
                      {r.note && <div className="mt-0.5 text-[11px] text-zinc-500">{r.note}</div>}
                    </td>
                    <td className="py-2.5 pr-4">
                      {r.kind === 'proxy' ? <Pill tone="violet">proxy</Pill> : <Pill tone="amber">redirect {r.redirect_code}</Pill>}
                    </td>
                    <td className="max-w-[18rem] truncate py-2.5 pr-4 font-mono text-zinc-300" title={r.target}>
                      {r.target}
                      {r.kind === 'redirect' && r.keep_path && <span className="text-zinc-500"> + path</span>}
                    </td>
                    <td className="py-2.5 pr-4">
                      <ShadcnSwitch checked={r.enabled} onChange={(v) => void toggle(r, v)} />
                    </td>
                    <td className="py-2.5">
                      <div className="flex items-center justify-end gap-1.5">
                        <GhostButton title="Edit" onClick={() => setForm({ ...r })}>
                          <Pencil className="h-3.5 w-3.5" />
                        </GhostButton>
                        <GhostButton tone="danger" title="Delete" onClick={() => void remove(r)}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </GhostButton>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <ShadcnDialog
        open={form !== null}
        onClose={() => !saving && setForm(null)}
        title={form?.id ? 'Edit route' : 'Add route'}
        icon={<ArrowRightLeft className="h-4 w-4" />}
        maxWidth="lg"
        footer={
          <>
            <GhostButton onClick={() => setForm(null)} disabled={saving}>Cancel</GhostButton>
            <PrimaryButton busy={saving} onClick={save} disabled={!form?.domain?.trim() || !form?.target?.trim()}>
              {form?.id ? 'Save' : 'Add route'}
            </PrimaryButton>
          </>
        }
      >
        {form && (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-2">
              {(
                [
                  { id: 'proxy', title: 'Reverse proxy', text: 'Show another server under this domain.' },
                  { id: 'redirect', title: 'Redirect', text: 'Send visitors to a different address.' },
                ] as const
              ).map((k) => (
                <button
                  key={k.id}
                  type="button"
                  onClick={() => set({ kind: k.id })}
                  className={`cursor-pointer rounded-lg border p-3 text-left transition-colors ${
                    form.kind === k.id ? 'border-orange-500/50 bg-orange-500/10' : 'border-white/[0.08] bg-[#09090b] hover:bg-zinc-900'
                  }`}
                >
                  <div className="text-xs font-semibold text-zinc-100">{k.title}</div>
                  <div className="mt-1 text-[11px] leading-snug text-zinc-400">{k.text}</div>
                </button>
              ))}
            </div>
            <Field label="Domain">
              <input className={inputCls} value={form.domain ?? ''} onChange={(e) => set({ domain: e.target.value })} placeholder="app.example.com" autoFocus />
            </Field>
            <Field label={isProxy ? 'Forward to' : 'Redirect to'}>
              <input
                className={`${inputCls} font-mono`}
                value={form.target ?? ''}
                onChange={(e) => set({ target: e.target.value })}
                placeholder={isProxy ? 'http://10.0.0.5:8080' : 'https://example.com'}
              />
            </Field>
            {!isProxy && (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Type">
                  <select className={selectCls} value={form.redirect_code ?? 301} onChange={(e) => set({ redirect_code: Number(e.target.value) })}>
                    <option value={301}>301 Moved permanently</option>
                    <option value={302}>302 Found (temporary)</option>
                    <option value={307}>307 Temporary, keep method</option>
                    <option value={308}>308 Permanent, keep method</option>
                  </select>
                </Field>
                <label className="flex cursor-pointer items-end gap-2 pb-2 text-xs text-zinc-300">
                  <input type="checkbox" checked={Boolean(form.keep_path)} onChange={(e) => set({ keep_path: e.target.checked })} className="accent-orange-500" />
                  Keep the page path and query
                </label>
              </div>
            )}
            <div className="flex flex-wrap gap-x-6 gap-y-2 text-xs text-zinc-300">
              <label className="inline-flex cursor-pointer items-center gap-2">
                <input type="checkbox" checked={form.enable_https ?? true} onChange={(e) => set({ enable_https: e.target.checked })} className="accent-orange-500" />
                HTTPS certificate
              </label>
              <label className="inline-flex cursor-pointer items-center gap-2">
                <input type="checkbox" checked={Boolean(form.enable_www)} onChange={(e) => set({ enable_www: e.target.checked })} className="accent-orange-500" />
                Include www
              </label>
              {isProxy && (form.target ?? '').startsWith('https://') && (
                <label className="inline-flex cursor-pointer items-center gap-2">
                  <input type="checkbox" checked={Boolean(form.skip_tls_verify)} onChange={(e) => set({ skip_tls_verify: e.target.checked })} className="accent-orange-500" />
                  Target has a self-signed certificate
                </label>
              )}
            </div>
            <Field label="Note">
              <input className={inputCls} value={form.note ?? ''} onChange={(e) => set({ note: e.target.value })} placeholder="Optional" maxLength={200} />
            </Field>
          </div>
        )}
      </ShadcnDialog>
    </div>
  );
};
