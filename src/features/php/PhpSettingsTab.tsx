import React, { useEffect, useRef, useState } from 'react';
import { KeyRound, RotateCw, ShieldCheck, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
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

const fmtTime = (s: number) => (s >= 60 ? `${s / 60} min` : `${s} s`);

export const PhpSettingsTab: React.FC<{ app: AppStack; onChanged: () => void | Promise<void>; onDelete?: () => void }> = ({ app, onChanged, onDelete }) => {
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

      <Card title="Site">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <div className="text-[11px] text-zinc-500">Type</div>
            <div className="mt-1"><Pill tone="orange">{SITE_TYPE_LABEL[detail.site.site_type]}</Pill></div>
          </div>
          <div>
            <div className="text-[11px] text-zinc-500">Runtime</div>
            <div className="mt-1 flex items-center gap-1.5">
              {isStatic ? <Pill>no PHP</Pill> : <><Pill tone="violet">PHP {detail.site.php_version}</Pill><Pill tone={runtimeTone}>{detail.runtime.state}</Pill></>}
            </div>
          </div>
          <div>
            <div className="text-[11px] text-zinc-500">System user (UID)</div>
            <div className="mt-1 font-mono text-xs text-zinc-200">{detail.site.uid}</div>
          </div>
          <div>
            <div className="text-[11px] text-zinc-500">Document root</div>
            <div className="mt-1 truncate font-mono text-[11px] text-zinc-300" title={detail.doc_root}>{detail.doc_root}</div>
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
              <select className={selectCls} value={version} onChange={(e) => setVersion(e.target.value)}>
                {installed.map((v) => (
                  <option key={v} value={v}>PHP {v}</option>
                ))}
                {!installed.includes(version) && <option value={version}>PHP {version} (not installed)</option>}
              </select>
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

      {!isStatic && (
        <Card title="Maintenance">
          <div className="flex flex-wrap items-center gap-2">
            <GhostButton busy={busy === 'restart'} onClick={() => run('restart', () => backendApi.phpSiteRestart(app.id), 'PHP runtime restarted')}>
              <RotateCw className="h-3.5 w-3.5 text-zinc-400" /> Restart PHP runtime
            </GhostButton>
            <GhostButton busy={busy === 'perm'} onClick={() => run('perm', () => backendApi.phpFixPermissions(app.id), 'File ownership repaired')}>
              <ShieldCheck className="h-3.5 w-3.5 text-zinc-400" /> Repair file permissions
            </GhostButton>
          </div>
        </Card>
      )}

      {onDelete && (
        <Card title="Delete site" description="Removes the site, its files, databases and domains for good.">
          <div className="flex justify-end px-4 py-3">
            <button
              type="button"
              onClick={onDelete}
              className="inline-flex items-center gap-1.5 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-1.5 text-xs font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
            >
              <Trash2 className="h-3.5 w-3.5" /> Delete this site
            </button>
          </div>
        </Card>
      )}

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
    </div>
  );
};
