import React, { useState } from 'react';
import { Route } from 'lucide-react';
import { toast } from 'sonner';
import type { PhpRoutingRule } from '../../../lib/api/phpApi';
import { ShadcnDialog, ShadcnSwitch } from '../../../components/ui/Primitives';
import { Field, GhostButton, PrimaryButton, inputCls, selectCls } from '../ui';

export interface RulePresetItem {
  key: string;
  label: string;
  rule: Omit<PhpRoutingRule, 'id'>;
}

interface Props {
  open: boolean;
  initialRule: PhpRoutingRule | null;
  presets?: RulePresetItem[];
  onClose: () => void;
  onSave: (rule: PhpRoutingRule) => void;
}

const EMPTY_DRAFT = (): PhpRoutingRule => ({
  id: '',
  kind: 'rewrite',
  match_type: 'prefix',
  pattern: '/api/',
  target: '/api.php',
  status: 0,
  enabled: true,
  unless_exists: false,
  preserve_query: true,
  note: '',
});

export const RuleEditorModal: React.FC<Props> = ({ open, initialRule, presets = [], onClose, onSave }) => {
  const isEditing = Boolean(initialRule);
  const [draft, setDraft] = useState<PhpRoutingRule>(() =>
    initialRule ? { ...initialRule } : EMPTY_DRAFT()
  );

  const patch = (next: Partial<PhpRoutingRule>) => setDraft((prev) => ({ ...prev, ...next }));

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmedPattern = draft.pattern.trim();
    if (!trimmedPattern) {
      toast.error('Pattern is required');
      return;
    }
    if (draft.kind !== 'block' && !(draft.target || '').trim()) {
      toast.error('Target path is required for rewrites and redirects');
      return;
    }
    onSave({
      ...draft,
      id: draft.id,
      pattern: trimmedPattern,
      target: draft.kind === 'block' ? '' : (draft.target || '').trim(),
      note: (draft.note || '').trim(),
    });
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title={isEditing ? 'Edit routing rule' : 'Add routing rule'}
      description="Configure a single Caddy rewrite, redirect, or path block."
      icon={<Route className="h-4 w-4 text-orange-400" />}
      maxWidth="md"
      footer={
        <>
          <GhostButton onClick={onClose}>Cancel</GhostButton>
          <PrimaryButton type="submit" form="php-rule-editor-form">
            {isEditing ? 'Update rule' : 'Add rule'}
          </PrimaryButton>
        </>
      }
    >
      <form id="php-rule-editor-form" onSubmit={handleSubmit} className="space-y-4">
        {!isEditing && presets.length > 0 && (
          <div className="space-y-1.5">
            <span className="block text-[11px] font-medium text-zinc-300">Quick presets</span>
            <div className="flex flex-wrap gap-1.5">
              {presets.map((preset) => (
                <button
                  key={preset.key}
                  type="button"
                  onClick={() => setDraft((prev) => ({ ...preset.rule, id: prev.id }))}
                  className="rounded-md border border-white/[0.12] bg-[#09090b] px-2.5 py-1 text-xs text-zinc-200 transition hover:bg-white/[0.08] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-500 cursor-pointer"
                >
                  {preset.label}
                </button>
              ))}
            </div>
          </div>
        )}

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Action">
            <select
              aria-label="Rule action"
              className={selectCls}
              value={draft.kind}
              onChange={(e) => {
                const kind = e.target.value as PhpRoutingRule['kind'];
                patch({
                  kind,
                  status: kind === 'redir' ? 301 : kind === 'block' ? 404 : 0,
                  target: kind === 'block' ? '' : draft.target || '/index.php',
                });
              }}
            >
              <option value="rewrite">Rewrite (internal)</option>
              <option value="redir">Redirect (HTTP)</option>
              <option value="block">Block path</option>
            </select>
          </Field>

          <Field label="Match mode">
            <select
              aria-label="Match mode"
              className={selectCls}
              value={draft.match_type}
              onChange={(e) => patch({ match_type: e.target.value as PhpRoutingRule['match_type'] })}
            >
              <option value="exact">Exact path</option>
              <option value="prefix">Prefix *</option>
              <option value="regex">Regex (RE2)</option>
            </select>
          </Field>
        </div>

        <Field label="Pattern">
          <input
            aria-label="Pattern"
            className={`${inputCls} font-mono`}
            value={draft.pattern}
            onChange={(e) => patch({ pattern: e.target.value })}
            placeholder={draft.match_type === 'regex' ? '^/blog/([^/]+)/?$' : '/old-path'}
            autoFocus
          />
        </Field>

        {draft.kind === 'block' ? (
          <Field label="Block response status">
            <select
              aria-label="HTTP block status"
              className={selectCls}
              value={draft.status || 404}
              onChange={(e) => patch({ status: Number(e.target.value) })}
            >
              <option value={404}>HTTP 404 Not Found</option>
              <option value={403}>HTTP 403 Forbidden</option>
              <option value={410}>HTTP 410 Gone</option>
            </select>
          </Field>
        ) : (
          <div className={`grid gap-3 ${draft.kind === 'redir' ? 'sm:grid-cols-3' : ''}`}>
            <div className={draft.kind === 'redir' ? 'sm:col-span-2' : ''}>
              <Field label="Target path">
                <input
                  aria-label="Target path"
                  className={`${inputCls} font-mono`}
                  value={draft.target || ''}
                  onChange={(e) => patch({ target: e.target.value })}
                  placeholder={draft.kind === 'rewrite' ? '/post.php?slug=$1' : '/new-path'}
                />
              </Field>
            </div>
            {draft.kind === 'redir' && (
              <Field label="Redirect code">
                <select
                  aria-label="Redirect HTTP status code"
                  className={selectCls}
                  value={draft.status || 301}
                  onChange={(e) => patch({ status: Number(e.target.value) })}
                >
                  <option value={301}>301 Permanent</option>
                  <option value={302}>302 Found</option>
                  <option value={307}>307 Temporary</option>
                  <option value={308}>308 Permanent</option>
                </select>
              </Field>
            )}
          </div>
        )}

        {draft.kind !== 'block' && (
          <div className="flex flex-wrap items-center gap-5 rounded-lg border border-white/[0.1] bg-[#09090b] px-3 py-2.5">
            {draft.kind === 'rewrite' && (
              <ShadcnSwitch
                checked={Boolean(draft.unless_exists)}
                onChange={(next) => patch({ unless_exists: next })}
                label="Skip if file exists (!-f)"
              />
            )}
            <ShadcnSwitch
              checked={Boolean(draft.preserve_query)}
              onChange={(next) => patch({ preserve_query: next })}
              label="Preserve query string (QSA)"
            />
          </div>
        )}

        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div className="flex-1">
            <Field label="Note (optional)">
              <input
                aria-label="Rule note"
                className={inputCls}
                value={draft.note || ''}
                onChange={(e) => patch({ note: e.target.value })}
                placeholder="Short label shown in the rule list"
              />
            </Field>
          </div>
          <div className="flex h-9 items-center rounded-lg border border-white/[0.1] bg-[#09090b] px-3">
            <ShadcnSwitch
              checked={draft.enabled}
              onChange={(next) => patch({ enabled: next })}
              label="Enabled"
            />
          </div>
        </div>
      </form>
    </ShadcnDialog>
  );
};
