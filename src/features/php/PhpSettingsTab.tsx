import React, { useEffect, useRef, useState } from 'react';
import { Check, Copy, FolderOpen, KeyRound, RotateCw, ShieldCheck, Trash2, Zap } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';
import { SiteTypeIcon } from '../../components/ui/BrandIcons';
import type { PhpProvision, PhpSettings } from '../../lib/api/phpApi';
import { ShadcnDialog } from '../../components/ui/Primitives';
import {
  Card,
  CopyField,
  ErrorBlock,
  Field,
  GhostButton,
  LoadingBlock,
  Pill,
  PrimaryButton,
  SITE_TYPE_LABEL,
  inputCls,
  selectCls,
  textareaCls,
  usePhpSite,
  usePhpStatus,
} from './ui';
import { PhpVersionSelect } from './PhpVersionSelect';
import { WordPressOptimizationModal } from './WordPressOptimizationModal';

const fmtTime = (s: number) => (s >= 60 ? `${s / 60} min` : `${s} s`);

export const PhpSettingsTab: React.FC<{ app: AppStack; onChanged: () => void | Promise<void>; onDelete?: () => void; onOpenFiles?: () => void }> = ({ app, onChanged, onDelete, onOpenFiles }) => {
  const [pollMs, setPollMs] = useState(0);
  const { detail, error, loading, reload } = usePhpSite(app.id, pollMs);
  const status = usePhpStatus();
  const [form, setForm] = useState<PhpSettings | null>(null);
  const [version, setVersion] = useState('');
  const [rootDir, setRootDir] = useState('');
  const [saving, setSaving] = useState(false);
  const [busy, setBusy] = useState('');
  const [secrets, setSecrets] = useState<Record<string, string> | null>(null);
  const loadedFor = useRef('');
  const [copied, setCopied] = useState('');
  const [optModalOpen, setOptModalOpen] = useState(false);

  const copyValue = async (value: string, key: string) => {
    if (await copyToClipboard(value)) {
      setCopied(key);
      setTimeout(() => setCopied((prev) => (prev === key ? '' : prev)), 1600);
    }
  };

  // seed the form once per site (don't clobber edits on polling reloads)
  useEffect(() => {
    if (detail && loadedFor.current !== app.id) {
      loadedFor.current = app.id;
      setForm(detail.settings);
      setVersion(detail.site.php_version);
      setRootDir(detail.site.root_dir);
    }
  }, [detail, app.id]);

  // follow WordPress provisioning and pick up the one-time credentials
  const prov: PhpProvision | undefined = detail?.provision?.status ? detail.provision : undefined;
  useEffect(() => {
    setPollMs(prov?.status === 'running' ? 2500 : 0);
    if (prov && prov.status === 'done' && prov.secrets === undefined) {
      void backendApi.phpProvision(app.id, true).then((r) => {
        const s = r.data?.provision?.secrets;
        if (s && Object.keys(s).length) setSecrets(s);
        void onChanged();
      });
    }
  }, [prov?.status]); // eslint-disable-line react-hooks/exhaustive-deps

  if (loading) return <LoadingBlock />;
  if (error || !detail || !form) return <ErrorBlock message={error || 'Site not found'} onRetry={reload} />;

  const choices = status?.choices;
  const isStatic = detail.site.site_type === 'static';
  const set = <K extends keyof PhpSettings>(k: K, v: PhpSettings[K]) => setForm({ ...form, [k]: v });
  // the server already filtered these by the owner's quota and the machine's real capacity
  const memOptions = choices?.memory_limit ?? ['256M'];
  const sizeOptions = choices?.sizes ?? ['64M'];
  const maxWorkers = detail.limits.max_children || choices?.max_children || 100;
  const withCurrent = (opts: string[], cur: string) => (opts.includes(cur) ? opts : [...opts, cur]);

  const save = async () => {
    setSaving(true);
    const body: Parameters<typeof backendApi.phpSiteSettings>[1] = { ...form, root_dir: rootDir };
    if (!isStatic && version !== detail.site.php_version) body.php_version = version;
    const res = await backendApi.phpSiteSettings(app.id, body);
    setSaving(false);
    if (!res.ok) {
      toast.error(res.error || 'Could not save settings');
      return;
    }
    toast.success(version !== detail.site.php_version ? `Switched to PHP ${version}` : 'Settings saved');
    loadedFor.current = '';
    await reload();
    await onChanged();
  };

  const run = async (key: string, fn: () => Promise<{ ok: boolean; error?: string }>, okMsg: string) => {
    setBusy(key);
    const res = await fn();
    setBusy('');
    if (res.ok) toast.success(okMsg);
    else toast.error(res.error || 'Action failed');
    await reload();
  };

  const runtimeTone = detail.runtime.state === 'running' ? 'green' : detail.runtime.state === 'none' ? 'zinc' : 'red';
  const installed = detail.installed_versions;

  return (
    <div className="space-y-5">
      {prov && prov.status !== 'done' && (
        <Card
          title={prov.status === 'running' ? 'Installing WordPress…' : 'WordPress install failed'}
          description={prov.status === 'running' ? undefined : prov.error}
        >
          <pre className="max-h-48 overflow-auto rounded-lg bg-[#09090b] p-3 font-mono text-[11px] leading-relaxed text-zinc-300">
            {prov.log || 'Starting…'}
          </pre>
        </Card>
      )}

      <Card
        title="Site"
        actions={
          detail.site.site_type === 'wordpress' ? (
            <button
              type="button"
              onClick={() => setOptModalOpen(true)}
              className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/25 bg-amber-500/10 px-2.5 py-1 text-xs font-medium text-amber-400 transition hover:border-amber-500/40 hover:bg-amber-500/20 active:scale-95 cursor-pointer"
              title="WordPress Caching & Performance Guide"
            >
              <Zap className="h-3.5 w-3.5" />
              <span>Speed & Cache</span>
            </button>
          ) : undefined
        }
      >
        {/* Mobile: stacked key-value rows */}
        <div className="divide-y divide-white/[0.05] sm:hidden">
          <div className="flex items-center justify-between gap-4 py-2.5 first:pt-0 last:pb-0">
            <div className="shrink-0 text-xs text-zinc-500">Type</div>
            <Pill tone="orange">
              <SiteTypeIcon type={detail.site.site_type} className="h-3 w-3" />
              {SITE_TYPE_LABEL[detail.site.site_type]}
            </Pill>
          </div>
          <div className="flex items-center justify-between gap-4 py-2.5 first:pt-0 last:pb-0">
            <div className="shrink-0 text-xs text-zinc-500">Runtime</div>
            <div className="flex items-center gap-1.5">
              {isStatic ? <Pill>no PHP</Pill> : <><Pill tone="violet">PHP {detail.site.php_version}</Pill><Pill tone={runtimeTone}><span className={`inline-block h-1.5 w-1.5 rounded-full ${runtimeTone === 'green' ? 'bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.7)]' : runtimeTone === 'red' ? 'bg-rose-400' : 'bg-zinc-400'}`} aria-hidden="true" />{detail.runtime.state}</Pill></>}
            </div>
          </div>
          <div className="flex items-center justify-between gap-4 py-2.5 first:pt-0 last:pb-0">
            <div className="shrink-0 text-xs text-zinc-500">System user (UID)</div>
            <button
              type="button"
              onClick={() => void copyValue(String(detail.site.uid), 'uid')}
              title="Click to copy UID"
              className="group inline-flex items-center gap-2 font-mono text-xs font-medium tabular-nums text-zinc-100 transition-colors hover:text-white cursor-pointer"
            >
              {detail.site.uid}
              <span className="flex h-6 w-6 items-center justify-center rounded-md bg-white/[0.04] text-zinc-500 transition-colors group-hover:bg-white/[0.09] group-hover:text-zinc-100">
                {copied === 'uid' ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
              </span>
            </button>
          </div>
          <div className="flex flex-col gap-2.5 py-2.5 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
            <div className="shrink-0 text-xs text-zinc-500">Document root</div>
            <div className="flex min-w-0 items-center gap-1.5">
              <button
                type="button"
                onClick={() => void copyValue(detail.doc_root, 'docroot')}
                title="Click to copy document root"
                className="group flex min-w-0 flex-1 items-center justify-between gap-2 rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-1.5 font-mono text-[11.5px] text-zinc-300 transition-colors hover:border-white/[0.14] hover:text-zinc-100 cursor-pointer sm:flex-none sm:max-w-[22rem]"
              >
                <span className="truncate">{detail.doc_root}</span>
                <span className="shrink-0 text-zinc-500 transition-colors group-hover:text-zinc-100">
                  {copied === 'docroot' ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                </span>
              </button>
              {onOpenFiles && (
                <button
                  type="button"
                  onClick={onOpenFiles}
                  title="Open in File Manager"
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.07] bg-[#09090b] text-zinc-400 transition-colors hover:border-white/[0.14] hover:text-orange-400 cursor-pointer"
                >
                  <FolderOpen className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          </div>
        </div>

        {/* Desktop: 4-column horizontal card grid */}
        <div className="hidden sm:grid sm:grid-cols-2 lg:grid-cols-4 sm:gap-4">
          <div>
            <div className="text-[11px] text-zinc-500">Site Type</div>
            <div className="mt-1">
              <Pill tone="orange">
                <SiteTypeIcon type={detail.site.site_type} className="h-3 w-3" />
                {SITE_TYPE_LABEL[detail.site.site_type]}
              </Pill>
            </div>
          </div>
          <div>
            <div className="text-[11px] text-zinc-500">Runtime</div>
            <div className="mt-1 flex items-center gap-1.5">
              {isStatic ? <Pill>no PHP</Pill> : (
                <>
                  <Pill tone="violet">PHP {detail.site.php_version}</Pill>
                  <Pill tone={runtimeTone}>
                    <span className={`inline-block h-1.5 w-1.5 rounded-full ${runtimeTone === 'green' ? 'bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.7)]' : runtimeTone === 'red' ? 'bg-rose-400' : 'bg-zinc-400'}`} aria-hidden="true" />
                    {detail.runtime.state}
                  </Pill>
                </>
              )}
            </div>
          </div>
          <div>
            <div className="text-[11px] text-zinc-500">System user (UID)</div>
            <div className="mt-1">
              <button
                type="button"
                onClick={() => void copyValue(String(detail.site.uid), 'uid')}
                title="Click to copy UID"
                className="group inline-flex items-center gap-1.5 font-mono text-xs font-medium tabular-nums text-zinc-100 transition-colors hover:text-white cursor-pointer"
              >
                {detail.site.uid}
                <span className="flex h-5 w-5 items-center justify-center rounded bg-white/[0.04] text-zinc-500 transition-colors group-hover:bg-white/[0.09] group-hover:text-zinc-100">
                  {copied === 'uid' ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
                </span>
              </button>
            </div>
          </div>
          <div>
            <div className="text-[11px] text-zinc-500">Document root</div>
            <div className="mt-1 flex min-w-0 items-center gap-1.5">
              <button
                type="button"
                onClick={() => void copyValue(detail.doc_root, 'docroot')}
                title="Click to copy document root"
                className="group flex min-w-0 flex-1 items-center justify-between gap-1.5 rounded-lg border border-white/[0.07] bg-[#09090b] px-2.5 py-1 font-mono text-[11px] text-zinc-300 transition-colors hover:border-white/[0.14] hover:text-zinc-100 cursor-pointer"
              >
                <span className="truncate" title={detail.doc_root}>{detail.doc_root}</span>
                <span className="shrink-0 text-zinc-500 transition-colors group-hover:text-zinc-100">
                  {copied === 'docroot' ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
                </span>
              </button>
              {onOpenFiles && (
                <button
                  type="button"
                  onClick={onOpenFiles}
                  title="Open in File Manager"
                  className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/[0.07] bg-[#09090b] text-zinc-400 transition-colors hover:border-white/[0.14] hover:text-orange-400 cursor-pointer"
                >
                  <FolderOpen className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          </div>
        </div>
      </Card>

      <Card
        title="PHP settings"
        actions={<PrimaryButton busy={saving} onClick={save}>Save changes</PrimaryButton>}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          {!isStatic && (
            <Field label="PHP version">
              <PhpVersionSelect
                value={version}
                onChange={setVersion}
                installedVersions={installed}
              />
            </Field>
          )}
          <Field label="Root directory">
            <input className={inputCls} value={rootDir} placeholder="htdocs  (e.g. public for Laravel)" onChange={(e) => setRootDir(e.target.value)} />
          </Field>
        </div>

        {!isStatic && (
          <>
            <div className="my-4 border-t border-white/[0.06]" />
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <Field label="memory_limit">
                <select className={selectCls} value={form.memory_limit} onChange={(e) => set('memory_limit', e.target.value)}>
                  {withCurrent(memOptions, form.memory_limit).map((m) => (
                    <option key={m} value={m} disabled={!memOptions.includes(m)}>{memOptions.includes(m) ? m : `${m} (above your limit)`}</option>
                  ))}
                </select>
              </Field>
              <Field label="max_execution_time">
                <select className={selectCls} value={form.max_execution_time} onChange={(e) => set('max_execution_time', Number(e.target.value))}>
                  {(choices?.times ?? [30]).map((t) => <option key={t} value={t}>{fmtTime(t)}</option>)}
                </select>
              </Field>
              <Field label="max_input_time">
                <select className={selectCls} value={form.max_input_time} onChange={(e) => set('max_input_time', Number(e.target.value))}>
                  {(choices?.times ?? [60]).map((t) => <option key={t} value={t}>{fmtTime(t)}</option>)}
                </select>
              </Field>
              <Field label="max_input_vars">
                <select className={selectCls} value={form.max_input_vars} onChange={(e) => set('max_input_vars', Number(e.target.value))}>
                  {(choices?.max_input_vars ?? [1000]).map((t) => <option key={t}>{t}</option>)}
                </select>
              </Field>
              <Field label="post_max_size">
                <select className={selectCls} value={form.post_max_size} onChange={(e) => set('post_max_size', e.target.value)}>
                  {withCurrent(sizeOptions, form.post_max_size).map((t) => <option key={t}>{t}</option>)}
                </select>
              </Field>
              <Field label="upload_max_filesize">
                <select className={selectCls} value={form.upload_max_filesize} onChange={(e) => set('upload_max_filesize', e.target.value)}>
                  {withCurrent(sizeOptions, form.upload_max_filesize).map((t) => <option key={t}>{t}</option>)}
                </select>
              </Field>
              <Field label={`Max PHP workers (up to ${maxWorkers})`}>
                <input type="number" min={1} max={maxWorkers} className={inputCls} value={form.max_children} onChange={(e) => set('max_children', Number(e.target.value))} />
              </Field>
            </div>
            <div className="mt-4">
              <Field label="Additional php.ini directives">
                <textarea className={textareaCls} rows={4} value={form.additional} onChange={(e) => set('additional', e.target.value)} placeholder="one per line, e.g. opcache.enable = 0" />
              </Field>
            </div>
          </>
        )}
      </Card>

      <Card title="Maintenance & Operations">
        <div className="divide-y divide-white/[0.05]">
          {!isStatic && (
            <div className="flex items-center justify-between gap-4 py-3 first:pt-0">
              <div className="min-w-0">
                <div className="text-sm font-medium text-zinc-100">Restart PHP-FPM</div>
                <p className="mt-0.5 text-xs text-zinc-500">Reload PHP {detail.site.php_version} workers and OPcache</p>
              </div>
              <GhostButton busy={busy === 'restart'} onClick={() => run('restart', () => backendApi.phpSiteRestart(app.id), 'PHP-FPM runtime restarted')} className="shrink-0">
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Restart
              </GhostButton>
            </div>
          )}
          <div className="flex items-center justify-between gap-4 py-3 last:pb-0">
            <div className="min-w-0">
              <div className="text-sm font-medium text-zinc-100">Repair file permissions</div>
              <p className="mt-0.5 text-xs text-zinc-500">Reset directories to 755 and files to 644</p>
            </div>
            <GhostButton busy={busy === 'perm'} onClick={() => run('perm', () => backendApi.phpFixPermissions(app.id), 'File ownership repaired')} className="shrink-0">
              <ShieldCheck className="h-3.5 w-3.5 text-zinc-400" /> Repair
            </GhostButton>
          </div>
          {onDelete && (
            <div className="-mx-1 flex items-center justify-between gap-4 rounded-xl bg-red-500/[0.05] px-3 py-3">
              <div className="min-w-0">
                <div className="text-sm font-medium text-zinc-100">Delete site</div>
                <p className="mt-0.5 text-xs text-zinc-500">Removes the site, its files, databases and domains for good.</p>
              </div>
              <button
                type="button"
                onClick={onDelete}
                className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-1.5 text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
              >
                <Trash2 className="h-3.5 w-3.5" /> Delete this site
              </button>
            </div>
          )}
        </div>
      </Card>

      <ShadcnDialog
        open={secrets !== null}
        onClose={() => setSecrets(null)}
        title="WordPress is ready"
        description="The password is shown only once."
        icon={<KeyRound className="h-4 w-4" />}
        footer={<PrimaryButton onClick={() => setSecrets(null)}>I saved them</PrimaryButton>}
      >
        <div className="space-y-3">
          {secrets?.url && <CopyField label="Admin URL" value={secrets.url} />}
          {secrets?.admin_user && <CopyField label="Username" value={secrets.admin_user} />}
          {secrets?.admin_password && <CopyField label="Password" value={secrets.admin_password} secret />}
          {secrets?.admin_email && <CopyField label="E-mail" value={secrets.admin_email} />}
        </div>
      </ShadcnDialog>

      <WordPressOptimizationModal
        open={optModalOpen}
        onClose={() => setOptModalOpen(false)}
        siteName={app.name || detail.site.primary_domain || 'WordPress'}
        plugins={detail.wp_cache_plugins}
        onRefresh={() => void reload()}
        onPurge={async () => {
          await phpApi.phpPurgeCache(app.id);
          await reload();
        }}
      />
    </div>
  );
};
