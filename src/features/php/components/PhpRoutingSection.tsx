import React, { Suspense, lazy, useMemo, useState } from 'react';
import { ArrowDown, ArrowUp, BookOpen, Pencil, Plus, Route, Search, Trash2, Wand2, X } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../../lib/backendApi';
import type { PhpRoutingRule, PhpSiteRouting } from '../../../lib/api/phpApi';
import { ShadcnSwitch } from '../../../components/ui/Primitives';
import { Card, Field, GhostButton, Pill, PrimaryButton, inputCls, selectCls } from '../ui';
import type { RulePresetItem } from './RuleEditorModal';

const RULE_PRESETS: RulePresetItem[] = [
  {
    key: 'hide-php',
    label: 'Hide .php extension',
    rule: {
      kind: 'rewrite',
      match_type: 'regex',
      pattern: '^/([^./]+)$',
      target: '/$1.php',
      status: 0,
      enabled: true,
      unless_exists: true,
      preserve_query: true,
      note: 'Hide .php extension',
    },
  },
  {
    key: 'block-dot',
    label: 'Block dot-dirs',
    rule: {
      kind: 'block',
      match_type: 'prefix',
      pattern: '/.git/',
      target: '',
      status: 404,
      enabled: true,
      unless_exists: false,
      preserve_query: false,
      note: 'Hide VCS',
    },
  },
  {
    key: 'redir-301',
    label: '301 Redirect',
    rule: {
      kind: 'redir',
      match_type: 'exact',
      pattern: '/old-path',
      target: '/new-path',
      status: 301,
      enabled: true,
      unless_exists: false,
      preserve_query: true,
      note: '301 redirect',
    },
  },
];

const makeRuleId = () => `r-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;

const HtaccessImportModal = lazy(() =>
  import('./HtaccessImportModal').then((m) => ({ default: m.HtaccessImportModal }))
);

const RoutingGuideModal = lazy(() =>
  import('./RoutingGuideModal').then((m) => ({ default: m.RoutingGuideModal }))
);

const RuleEditorModal = lazy(() =>
  import('./RuleEditorModal').then((m) => ({ default: m.RuleEditorModal }))
);

interface Props {
  appId: string;
  isStatic: boolean;
  value: PhpSiteRouting;
  savedValue?: PhpSiteRouting;
  onChange: (next: PhpSiteRouting) => void;
  onSaved?: () => void | Promise<void>;
}

const serializeRouting = (r?: PhpSiteRouting) =>
  JSON.stringify({
    fallback_mode: !r?.fallback_mode || r.fallback_mode === 'default' ? '' : r.fallback_mode,
    fallback_entry: (r?.fallback_entry || '').trim(),
    rules: (r?.rules ?? []).map((rule) => ({
      id: rule.id || '',
      kind: rule.kind,
      match_type: rule.match_type,
      pattern: rule.pattern,
      target: rule.target || '',
      status: rule.status || 0,
      enabled: Boolean(rule.enabled),
      unless_exists: Boolean(rule.unless_exists),
      require_target_file: Boolean(rule.require_target_file),
      preserve_query: Boolean(rule.preserve_query),
      note: rule.note || '',
    })),
  });

const KIND_BADGE: Record<PhpRoutingRule['kind'], string> = {
  rewrite: 'border-white/[0.12] bg-white/[0.04] text-zinc-200',
  redir: 'border-orange-500/30 bg-orange-500/10 text-orange-300',
  block: 'border-red-500/30 bg-red-500/10 text-red-300',
};

const iconBtnCls =
  'rounded p-1.5 text-zinc-400 transition hover:bg-white/[0.08] hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-500 disabled:opacity-30 disabled:pointer-events-none cursor-pointer';

export const PhpRoutingSection: React.FC<Props> = ({ appId, isStatic, value, savedValue, onChange, onSaved }) => {
  const [importOpen, setImportOpen] = useState(false);
  const [guideOpen, setGuideOpen] = useState(false);
  const [editorState, setEditorState] = useState<{ open: boolean; index: number | null }>({
    open: false,
    index: null,
  });
  const [searchQuery, setSearchQuery] = useState('');
  const [savingRouting, setSavingRouting] = useState(false);

  const rules = value.rules ?? [];
  const fallbackMode = value.fallback_mode || 'default';
  const activeCount = rules.filter((r) => r.enabled).length;
  const trimmedQuery = searchQuery.trim().toLowerCase();
  const isFiltering = trimmedQuery.length > 0;
  const hasChanges = useMemo(
    () => serializeRouting(value) !== serializeRouting(savedValue),
    [value, savedValue]
  );

  const visibleEntries = useMemo(() => {
    const mapped = rules.map((rule, idx) => ({ rule, idx }));
    if (!trimmedQuery) return mapped;
    return mapped.filter(({ rule }) =>
      [rule.pattern, rule.target || '', rule.note || '', rule.kind, rule.match_type]
        .join(' ')
        .toLowerCase()
        .includes(trimmedQuery)
    );
  }, [rules, trimmedQuery]);

  const setRouting = (updater: (prev: PhpSiteRouting) => PhpSiteRouting) => {
    onChange(updater(value));
  };

  const toggleEnabled = (idx: number, enabled: boolean) => {
    setRouting((prev) => ({
      ...prev,
      rules: (prev.rules ?? []).map((r, i) => (i === idx ? { ...r, enabled } : r)),
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

  const addPreset = (preset: Omit<PhpRoutingRule, 'id'>) => {
    setRouting((prev) => ({
      ...prev,
      rules: [...(prev.rules ?? []), { ...preset, id: makeRuleId() }],
    }));
  };

  const handleRuleSave = (saved: PhpRoutingRule) => {
    const withId: PhpRoutingRule = { ...saved, id: saved.id || makeRuleId() };
    const targetIdx = editorState.index;
    setRouting((prev) => {
      const existing = prev.rules ?? [];
      if (targetIdx === null) {
        return { ...prev, rules: [...existing, withId] };
      }
      return {
        ...prev,
        rules: existing.map((r, i) => (i === targetIdx ? withId : r)),
      };
    });
    setEditorState({ open: false, index: null });
    toast.success(targetIdx === null ? 'Rule added. Click "Save rules" to sync.' : 'Rule updated. Click "Save rules" to sync.');
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

  const editingRule = editorState.index !== null ? rules[editorState.index] ?? null : null;

  return (
    <Card
      title="URL Routing"
      description="Native Caddy rewrites, redirects, and path blocking. WordPress and Laravel work by default."
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <GhostButton onClick={() => setGuideOpen(true)}>
            <BookOpen className="h-3.5 w-3.5 text-zinc-300" /> Guide
          </GhostButton>
          <GhostButton onClick={() => setImportOpen(true)}>
            <Wand2 className="h-3.5 w-3.5 text-orange-400" /> Import .htaccess
          </GhostButton>
          {hasChanges ? (
            <PrimaryButton busy={savingRouting} onClick={saveRoutingOnly}>
              Save rules
            </PrimaryButton>
          ) : (
            <GhostButton disabled className="opacity-50" title="No unsaved routing changes">
              Save rules
            </GhostButton>
          )}
        </div>
      }
    >
      <div className="space-y-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Missing file fallback">
            <select
              aria-label="Missing file fallback"
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
                {isStatic ? 'Standard 404 (Static default)' : 'Front-controller index.php (WP / Laravel)'}
              </option>
              <option value="strict_404">Strict 404 for missing files (=404)</option>
              <option value="custom">Custom entry file (e.g. app.php or index.html)</option>
            </select>
          </Field>

          {fallbackMode === 'custom' && (
            <Field label="Custom entry file">
              <input
                aria-label="Custom fallback entry file"
                className={inputCls}
                value={value.fallback_entry || ''}
                onChange={(e) => setRouting((prev) => ({ ...prev, fallback_entry: e.target.value }))}
                placeholder={isStatic ? 'index.html' : 'app.php'}
              />
            </Field>
          )}
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.08] pt-4">
          <div className="flex items-center gap-2">
            <Route className="h-4 w-4 text-zinc-400" />
            <span className="text-xs font-medium text-zinc-100">Ordered Rules</span>
            <Pill tone="zinc">
              {activeCount === rules.length ? `${activeCount} active` : `${activeCount} / ${rules.length} active`}
            </Pill>
          </div>
          <GhostButton onClick={() => setEditorState({ open: true, index: null })}>
            <Plus className="h-3.5 w-3.5" /> Add rule
          </GhostButton>
        </div>

        {rules.length > 5 && (
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2 text-zinc-400" />
            <input
              type="search"
              aria-label="Filter routing rules"
              className={`${inputCls} pl-8 pr-8`}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={`Filter ${rules.length} rules by pattern, target, or note...`}
            />
            {isFiltering && (
              <button
                type="button"
                aria-label="Clear filter"
                onClick={() => setSearchQuery('')}
                className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-1 text-zinc-400 hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-500 cursor-pointer"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        )}

        {rules.length === 0 ? (
          <div className="rounded-lg border border-dashed border-white/[0.12] bg-[#09090b] px-4 py-6 text-center">
            <p className="text-xs text-zinc-300">
              No custom rules configured. Import from <code className="font-mono text-zinc-100">htdocs/</code> or start with a preset:
            </p>
            <div className="mt-3 flex flex-wrap items-center justify-center gap-2">
              {RULE_PRESETS.map((preset) => (
                <button
                  key={preset.key}
                  type="button"
                  onClick={() => addPreset(preset.rule)}
                  className="rounded-md border border-white/[0.12] bg-white/[0.04] px-2.5 py-1.5 text-xs text-zinc-200 transition hover:bg-white/[0.09] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-500 cursor-pointer"
                >
                  + {preset.label}
                </button>
              ))}
            </div>
          </div>
        ) : visibleEntries.length === 0 ? (
          <div className="rounded-lg border border-white/[0.1] bg-[#09090b] px-4 py-6 text-center text-xs text-zinc-300">
            No rules match &ldquo;{searchQuery}&rdquo;.
          </div>
        ) : (
          <div className="max-h-[380px] divide-y divide-white/[0.07] overflow-y-auto rounded-lg border border-white/[0.1] bg-[#09090b]">
            {visibleEntries.map(({ rule, idx }) => {
              const kindLabel =
                rule.kind === 'redir'
                  ? `Redirect ${rule.status || 301}`
                  : rule.kind === 'block'
                    ? `Block ${rule.status || 404}`
                    : 'Rewrite';

              return (
                <div
                  key={rule.id || idx}
                  className={`flex items-center justify-between gap-3 px-3 py-2.5 transition hover:bg-white/[0.025] ${
                    rule.enabled ? '' : 'opacity-55'
                  }`}
                >
                  <div className="flex min-w-0 flex-1 items-center gap-2.5">
                    <span className="w-7 shrink-0 font-mono text-[11px] text-zinc-400">#{idx + 1}</span>
                    <span
                      className={`shrink-0 rounded border px-1.5 py-0.5 text-[10px] font-medium ${KIND_BADGE[rule.kind]}`}
                    >
                      {kindLabel}
                    </span>
                    <span className="hidden shrink-0 rounded bg-white/[0.06] px-1.5 py-0.5 font-mono text-[10px] text-zinc-300 sm:inline">
                      {rule.match_type}
                    </span>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5 font-mono text-xs">
                        <span className="truncate text-zinc-100" title={rule.pattern}>
                          {rule.pattern}
                        </span>
                        {rule.kind !== 'block' && (
                          <>
                            <span aria-hidden="true" className="shrink-0 text-zinc-400">
                              &rarr;
                            </span>
                            <span className="truncate text-orange-300" title={rule.target || ''}>
                              {rule.target}
                            </span>
                          </>
                        )}
                      </div>
                      {(rule.note || rule.unless_exists || rule.require_target_file || rule.preserve_query) && (
                        <div className="mt-0.5 flex flex-wrap items-center gap-2 text-[11px] text-zinc-400">
                          {rule.kind === 'rewrite' && rule.unless_exists && (
                            <span className="font-mono text-[10px] text-zinc-300">!-f</span>
                          )}
                          {rule.kind === 'rewrite' && rule.require_target_file && (
                            <span className="font-mono text-[10px] text-zinc-300">.php -f</span>
                          )}
                          {rule.kind !== 'block' && rule.preserve_query && (
                            <span className="font-mono text-[10px] text-zinc-300">QSA</span>
                          )}
                          {rule.note && <span className="truncate">{rule.note}</span>}
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="flex shrink-0 items-center gap-1.5">
                    <div className="mr-1 flex items-center" title={rule.enabled ? 'Disable rule' : 'Enable rule'}>
                      <ShadcnSwitch
                        checked={rule.enabled}
                        onChange={(next) => toggleEnabled(idx, next)}
                        ariaLabel={`Enable rule ${idx + 1}`}
                      />
                    </div>
                    <button
                      type="button"
                      aria-label={`Edit rule ${idx + 1}`}
                      onClick={() => setEditorState({ open: true, index: idx })}
                      title="Edit rule"
                      className={iconBtnCls}
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      aria-label={`Move rule ${idx + 1} up`}
                      onClick={() => moveRule(idx, -1)}
                      disabled={isFiltering || idx === 0}
                      title={isFiltering ? 'Clear filter to reorder' : 'Move up'}
                      className={iconBtnCls}
                    >
                      <ArrowUp className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      aria-label={`Move rule ${idx + 1} down`}
                      onClick={() => moveRule(idx, 1)}
                      disabled={isFiltering || idx === rules.length - 1}
                      title={isFiltering ? 'Clear filter to reorder' : 'Move down'}
                      className={iconBtnCls}
                    >
                      <ArrowDown className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      aria-label={`Delete rule ${idx + 1}`}
                      onClick={() => removeRule(idx)}
                      title="Delete rule"
                      className="rounded p-1.5 text-zinc-400 transition hover:bg-rose-500/10 hover:text-rose-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-rose-500 cursor-pointer"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
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

      {guideOpen && (
        <Suspense fallback={null}>
          <RoutingGuideModal open={guideOpen} onClose={() => setGuideOpen(false)} />
        </Suspense>
      )}

      {editorState.open && (
        <Suspense fallback={null}>
          <RuleEditorModal
            key={editorState.index !== null ? `edit-${editingRule?.id || editorState.index}` : 'new'}
            open={editorState.open}
            initialRule={editingRule}
            presets={RULE_PRESETS}
            onClose={() => setEditorState({ open: false, index: null })}
            onSave={handleRuleSave}
          />
        </Suspense>
      )}
    </Card>
  );
};
