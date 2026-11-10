import React, { Suspense, lazy, useState } from 'react';
import { ArrowDown, ArrowUp, Plus, Route, ShieldAlert, Trash2, Wand2 } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../../lib/backendApi';
import type { PhpRoutingRule, PhpSiteRouting } from '../../../lib/api/phpApi';
import { Card, Field, GhostButton, Pill, PrimaryButton, inputCls, selectCls } from '../ui';

const HtaccessImportModal = lazy(() =>
  import('./HtaccessImportModal').then((m) => ({ default: m.HtaccessImportModal }))
);

interface Props {
  appId: string;
  isStatic: boolean;
  value: PhpSiteRouting;
  onChange: (next: PhpSiteRouting) => void;
  onSaved?: () => void | Promise<void>;
}

const makeRuleId = () => `r-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;

const DEFAULT_RULE = (): PhpRoutingRule => ({
  id: makeRuleId(),
  kind: 'rewrite',
  match_type: 'prefix',
  pattern: '/api/',
  target: '/api.php',
  status: 0,
  enabled: true,
  note: '',
});

export const PhpRoutingSection: React.FC<Props> = ({ appId, isStatic, value, onChange, onSaved }) => {
  const [importOpen, setImportOpen] = useState(false);
  const [savingRouting, setSavingRouting] = useState(false);

  const rules = value.rules ?? [];
  const fallbackMode = value.fallback_mode || 'default';

  const setRouting = (updater: (prev: PhpSiteRouting) => PhpSiteRouting) => {
    onChange(updater(value));
  };

  const updateRule = (idx: number, patch: Partial<PhpRoutingRule>) => {
    setRouting((prev) => ({
      ...prev,
      rules: (prev.rules ?? []).map((r, i) => (i === idx ? { ...r, ...patch } : r)),
    }));
  };

  const moveRule = (idx: number, dir: -1 | 1) => {
    const nextIdx = idx + dir;
    if (nextIdx < 0 || nextIdx >= rules.length) return;
    setRouting((prev) => {
      const copy = [...(prev.rules ?? [])];
      const [item] = copy.splice(idx, 1);
      copy.splice(nextIdx, 0, item);
      return { ...prev, rules: copy };
    });
  };

  const removeRule = (idx: number) => {
    setRouting((prev) => ({
      ...prev,
      rules: (prev.rules ?? []).filter((_, i) => i !== idx),
    }));
  };

  const addPreset = (preset: PhpRoutingRule) => {
    setRouting((prev) => ({
      ...prev,
      rules: [...(prev.rules ?? []), { ...preset, id: makeRuleId() }],
    }));
  };

  const handleImportApply = (
    imported: PhpRoutingRule[],
    mode: 'append' | 'replace',
    suggestedFallback?: PhpSiteRouting['fallback_mode'],
    suggestedEntry?: string
  ) => {
    setRouting((prev) => {
      const existing = mode === 'replace' ? [] : prev.rules ?? [];
      const seen = new Set(existing.map((r) => `${r.kind}|${r.match_type}|${r.pattern}|${r.target || ''}`));
      const merged = [...existing];
      for (const r of imported) {
        const key = `${r.kind}|${r.match_type}|${r.pattern}|${r.target || ''}`;
        if (!seen.has(key)) {
          seen.add(key);
          merged.push({ ...r, id: r.id || makeRuleId() });
        }
      }
      const next: PhpSiteRouting = { ...prev, rules: merged };
      if (suggestedFallback && (!prev.fallback_mode || prev.fallback_mode === 'default' || mode === 'replace')) {
        next.fallback_mode = suggestedFallback;
        next.fallback_entry = suggestedEntry || '';
      }
      return next;
    });
    toast.success(`Imported ${imported.length} rule(s). Click "Save rules" to apply to Caddy.`);
  };

  const saveRoutingOnly = async () => {
    setSavingRouting(true);
    const res = await backendApi.phpRoutingSave(appId, value);
    setSavingRouting(false);
    if (!res.ok) {
      toast.error(res.error || 'Could not save URL routing rules');
      return;
    }
    toast.success('Caddy routing rules saved and synced');
    if (onSaved) await onSaved();
  };

  return (
    <Card
      title="URL Routing & .htaccess Converter"
      description="Native Caddy rewrites, redirects, and path blocking. WordPress permalinks and Laravel front-controllers already work without extra rules."
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <GhostButton onClick={() => setImportOpen(true)}>
            <Wand2 className="h-3.5 w-3.5 text-orange-400" /> Import .htaccess
          </GhostButton>
          <PrimaryButton busy={savingRouting} onClick={saveRoutingOnly}>
            Save rules
          </PrimaryButton>
        </div>
      }
    >
      <div className="space-y-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Missing File Fallback (try_files)"
            hint="Default preserves index.php fallback so WordPress/Laravel work out of the box."
          >
            <select
              className={selectCls}
              value={fallbackMode}
              onChange={(e) => {
                const mode = e.target.value as PhpSiteRouting['fallback_mode'];
                setRouting((prev) => ({
                  ...prev,
                  fallback_mode: mode === 'default' ? '' : mode,
                  fallback_entry: mode === 'custom' ? prev.fallback_entry || 'index.html' : '',
                }));
              }}
            >
              <option value="default">
                {isStatic ? 'Standard 404 (Static default)' : 'Front-controller index.php (WP / Laravel default)'}
              </option>
              <option value="strict_404">Strict 404 for missing files (=404)</option>
              <option value="custom">Custom entry file (e.g. app.php or index.html SPA)</option>
            </select>
          </Field>

          {fallbackMode === 'custom' && (
            <Field label="Custom Fallback Entry File" hint="Relative path inside document root">
              <input
                className={inputCls}
                value={value.fallback_entry || ''}
                onChange={(e) => setRouting((prev) => ({ ...prev, fallback_entry: e.target.value }))}
                placeholder={isStatic ? 'index.html' : 'app.php'}
              />
            </Field>
          )}
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-4">
          <div className="flex items-center gap-2">
            <Route className="h-4 w-4 text-zinc-400" />
            <span className="text-xs font-medium text-zinc-200">Ordered Rules</span>
            <Pill tone="zinc">{rules.filter((r) => r.enabled).length} active</Pill>
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            <button
              type="button"
              onClick={() =>
                addPreset({
                  id: '',
                  kind: 'rewrite',
                  match_type: 'regex',
                  pattern: '^/([^.]+)$',
                  target: '/$1.php',
                  enabled: true,
                  unless_exists: true,
                  preserve_query: true,
                  note: 'Hide .php extension',
                })
              }
              className="rounded-md border border-white/[0.08] bg-white/[0.03] px-2.5 py-1 text-[11px] text-zinc-300 transition hover:bg-white/[0.07] cursor-pointer"
            >
              + Hide .php extension
            </button>
            <button
              type="button"
              onClick={() =>
                addPreset({
                  id: '',
                  kind: 'block',
                  match_type: 'prefix',
                  pattern: '/.git/',
                  status: 404,
                  enabled: true,
                  note: 'Hide VCS',
                })
              }
              className="rounded-md border border-white/[0.08] bg-white/[0.03] px-2.5 py-1 text-[11px] text-zinc-300 transition hover:bg-white/[0.07] cursor-pointer"
            >
              + Block dot-dirs
            </button>
            <button
              type="button"
              onClick={() =>
                addPreset({
                  id: '',
                  kind: 'redir',
                  match_type: 'exact',
                  pattern: '/old-path',
                  target: '/new-path',
                  status: 301,
                  enabled: true,
                  note: '301 SEO Redirect',
                })
              }
              className="rounded-md border border-white/[0.08] bg-white/[0.03] px-2.5 py-1 text-[11px] text-zinc-300 transition hover:bg-white/[0.07] cursor-pointer"
            >
              + 301 Redirect
            </button>
            <button
              type="button"
              onClick={() => addPreset(DEFAULT_RULE())}
              className="inline-flex items-center gap-1 rounded-md border border-orange-500/30 bg-orange-500/10 px-2.5 py-1 text-[11px] font-medium text-orange-300 transition hover:bg-orange-500/20 cursor-pointer"
            >
              <Plus className="h-3 w-3" /> Add Rule
            </button>
          </div>
        </div>

        {rules.length === 0 ? (
          <div className="rounded-lg border border-dashed border-white/[0.08] bg-[#09090b]/60 px-4 py-6 text-center text-xs text-zinc-500">
            No custom rules configured. Click <span className="text-zinc-300">Import .htaccess</span> to scan{' '}
            <code className="font-mono text-zinc-400">htdocs/</code> automatically, or add a rule above.
          </div>
        ) : (
          <div className="space-y-2.5">
            {rules.map((rule, idx) => (
              <div
                key={rule.id || idx}
                className={`rounded-lg border p-3 transition ${
                  rule.enabled ? 'border-white/[0.08] bg-[#09090b]' : 'border-white/[0.04] bg-[#09090b]/40 opacity-60'
                }`}
              >
                <div className="grid gap-2.5 sm:grid-cols-12 sm:items-center">
                  <div className="sm:col-span-2">
                    <select
                      className={selectCls}
                      value={rule.kind}
                      onChange={(e) => {
                        const kind = e.target.value as PhpRoutingRule['kind'];
                        updateRule(idx, {
                          kind,
                          status: kind === 'redir' ? 301 : kind === 'block' ? 404 : 0,
                          target: kind === 'block' ? '' : rule.target || '/index.php',
                        });
                      }}
                    >
                      <option value="rewrite">Rewrite</option>
                      <option value="redir">Redirect</option>
                      <option value="block">Block</option>
                    </select>
                  </div>

                  <div className="sm:col-span-2">
                    <select
                      className={selectCls}
                      value={rule.match_type}
                      onChange={(e) => updateRule(idx, { match_type: e.target.value as PhpRoutingRule['match_type'] })}
                    >
                      <option value="exact">Exact path</option>
                      <option value="prefix">Prefix *</option>
                      <option value="regex">Regex (RE2)</option>
                    </select>
                  </div>

                  <div className="sm:col-span-3">
                    <input
                      className={inputCls}
                      value={rule.pattern}
                      onChange={(e) => updateRule(idx, { pattern: e.target.value })}
                      placeholder={rule.match_type === 'regex' ? '^blog/(.*)$' : '/old-path'}
                    />
                  </div>

                  <div className="sm:col-span-3">
                    {rule.kind === 'block' ? (
                      <select
                        className={selectCls}
                        value={rule.status || 404}
                        onChange={(e) => updateRule(idx, { status: Number(e.target.value) })}
                      >
                        <option value={404}>HTTP 404 Not Found</option>
                        <option value={403}>HTTP 403 Forbidden</option>
                        <option value={410}>HTTP 410 Gone</option>
                      </select>
                    ) : (
                      <div className="flex gap-1.5">
                        <input
                          className={inputCls}
                          value={rule.target || ''}
                          onChange={(e) => updateRule(idx, { target: e.target.value })}
                          placeholder={rule.kind === 'rewrite' ? '/post.php?slug=$1' : '/new-path'}
                        />
                        {rule.kind === 'redir' && (
                          <select
                            className={`${selectCls} w-20 shrink-0`}
                            value={rule.status || 301}
                            onChange={(e) => updateRule(idx, { status: Number(e.target.value) })}
                          >
                            <option value={301}>301</option>
                            <option value={302}>302</option>
                            <option value={307}>307</option>
                            <option value={308}>308</option>
                          </select>
                        )}
                      </div>
                    )}
                  </div>

                  <div className="flex items-center justify-end gap-1 sm:col-span-2">
                    <input
                      type="checkbox"
                      checked={rule.enabled}
                      onChange={(e) => updateRule(idx, { enabled: e.target.checked })}
                      title="Enable rule"
                      className="mr-1 h-4 w-4 accent-orange-500 cursor-pointer"
                    />
                    <button
                      type="button"
                      onClick={() => moveRule(idx, -1)}
                      disabled={idx === 0}
                      title="Move up"
                      className="rounded p-1 text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200 disabled:opacity-30 cursor-pointer"
                    >
                      <ArrowUp className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => moveRule(idx, 1)}
                      disabled={idx === rules.length - 1}
                      title="Move down"
                      className="rounded p-1 text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200 disabled:opacity-30 cursor-pointer"
                    >
                      <ArrowDown className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => removeRule(idx)}
                      title="Delete rule"
                      className="rounded p-1 text-zinc-400 hover:bg-rose-500/10 hover:text-rose-400 cursor-pointer"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
                {rule.kind !== 'block' && (
                  <div className="mt-2 flex flex-wrap items-center gap-4 border-t border-white/[0.05] pt-2 text-[11px] text-zinc-400">
                    {rule.kind === 'rewrite' && (
                      <label className="inline-flex items-center gap-1.5 cursor-pointer hover:text-zinc-200">
                        <input
                          type="checkbox"
                          checked={Boolean(rule.unless_exists)}
                          onChange={(e) => updateRule(idx, { unless_exists: e.target.checked })}
                          className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                        />
                        <span>Skip if file exists (!-f)</span>
                      </label>
                    )}
                    <label className="inline-flex items-center gap-1.5 cursor-pointer hover:text-zinc-200">
                      <input
                        type="checkbox"
                        checked={Boolean(rule.preserve_query)}
                        onChange={(e) => updateRule(idx, { preserve_query: e.target.checked })}
                        className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                      />
                      <span>Preserve query string (QSA)</span>
                    </label>
                    {rule.note && <span className="ml-auto text-[10px] text-zinc-500">{rule.note}</span>}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        <div className="flex items-start gap-2 rounded-lg border border-white/[0.05] bg-white/[0.02] px-3 py-2 text-[11px] text-zinc-400">
          <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-400" />
          <span>
            Regex rules use Go RE2 syntax with capture groups <code className="font-mono text-zinc-300">$1</code>,{' '}
            <code className="font-mono text-zinc-300">$2</code>. Rules evaluate top-to-bottom inside Caddy before fast-path caching.
          </span>
        </div>
      </div>

      {importOpen && (
        <Suspense fallback={null}>
          <HtaccessImportModal
            appId={appId}
            open={importOpen}
            onClose={() => setImportOpen(false)}
            onApply={handleImportApply}
          />
        </Suspense>
      )}
    </Card>
  );
};
