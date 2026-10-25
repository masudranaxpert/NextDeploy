import React, { useEffect, useState } from 'react';
import { Lock, ShieldCheck } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import type { PhpSecurity } from '../../lib/api/phpApi';
import { ShadcnSwitch } from '../../components/ui/Primitives';
import { Card, ErrorBlock, Field, GhostButton, LoadingBlock, Pill, PrimaryButton, inputCls, textareaCls } from './ui';

const HARDENING: Array<{ key: keyof PhpSecurity; title: string; text: string }> = [
  { key: 'block_scanners', title: 'Block known scanners', text: 'Refuse requests from tools such as sqlmap, nikto and wpscan.' },
  { key: 'block_upload_php', title: 'No PHP in upload folders', text: 'A script uploaded to uploads/ or files/ can never run.' },
  { key: 'block_xmlrpc', title: 'Block xmlrpc.php', text: 'Closes the WordPress endpoint most used for password guessing.' },
  { key: 'frame_protection', title: 'Clickjacking protection', text: 'Other websites cannot show this site inside a frame.' },
  { key: 'hsts', title: 'Force HTTPS in browsers (HSTS)', text: 'Browsers refuse to open the site over plain HTTP for a year.' },
];

const toLines = (v: string[]) => v.join('\n');
const fromLines = (v: string) => v.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean);

export const PhpSecurityTab: React.FC<{ app: AppStack }> = ({ app }) => {
  const [form, setForm] = useState<PhpSecurity | null>(null);
  const [blocked, setBlocked] = useState('');
  const [allow, setAllow] = useState('');
  const [password, setPassword] = useState('');
  const [clientIp, setClientIp] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const load = async () => {
    const res = await backendApi.phpSecurity(app.id);
    if (!res.ok || !res.data) {
      setError(res.error || 'Could not load the settings');
      return;
    }
    setError('');
    setForm(res.data.security);
    setBlocked(toLines(res.data.security.blocked_ips));
    setAllow(toLines(res.data.security.allow_only));
    setClientIp(res.data.client_ip);
  };

  useEffect(() => {
    void load();
  }, [app.id]); // eslint-disable-line react-hooks/exhaustive-deps

  if (error) return <ErrorBlock message={error} onRetry={load} />;
  if (!form) return <LoadingBlock />;

  const save = async (extra: { remove_password?: boolean } = {}) => {
    setSaving(true);
    const res = await backendApi.phpSecuritySave(app.id, {
      ...form,
      blocked_ips: fromLines(blocked),
      allow_only: fromLines(allow),
      basic_auth_password: password || undefined,
      ...extra,
    });
    setSaving(false);
    if (!res.ok || !res.data) {
      toast.error(res.error || 'Could not save');
      return;
    }
    toast.success('Protection updated');
    setPassword('');
    setForm(res.data.security);
    setBlocked(toLines(res.data.security.blocked_ips));
    setAllow(toLines(res.data.security.allow_only));
  };

  const set = <K extends keyof PhpSecurity>(k: K, v: PhpSecurity[K]) => setForm({ ...form, [k]: v });
  const addMine = (list: string, setList: (v: string) => void) => {
    if (clientIp && !fromLines(list).includes(clientIp)) setList(list.trim() ? `${list.trim()}\n${clientIp}` : clientIp);
  };

  return (
    <div className="space-y-5">
      <Card
        title="IP rules"
        actions={<PrimaryButton busy={saving} onClick={() => save()}>Save changes</PrimaryButton>}
      >
        <div className="grid gap-5 lg:grid-cols-2">
          <Field label={`Block these addresses (${fromLines(blocked).length})`}>
            <textarea
              className={textareaCls}
              rows={6}
              value={blocked}
              onChange={(e) => setBlocked(e.target.value)}
              placeholder={'203.0.113.7\n198.51.100.0/24\n2001:db8::/32'}
            />
          </Field>
          <Field label={`Allow only these addresses (${fromLines(allow).length})`}>
            <textarea
              className={textareaCls}
              rows={6}
              value={allow}
              onChange={(e) => setAllow(e.target.value)}
              placeholder="Empty = everyone may visit"
            />
          </Field>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] text-zinc-500">
          <span>One address or range (CIDR) per line. Your address:</span>
          <Pill>{clientIp || 'unknown'}</Pill>
          {clientIp && (
            <>
              <GhostButton className="!h-6 !px-2 !text-[11px]" onClick={() => addMine(blocked, setBlocked)}>Block it</GhostButton>
              <GhostButton className="!h-6 !px-2 !text-[11px]" onClick={() => addMine(allow, setAllow)}>Allow it</GhostButton>
            </>
          )}
        </div>
        {fromLines(allow).length > 0 && (
          <p className="mt-3 rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
            With an allow list, every other visitor gets “403 Forbidden”, search engines included.
          </p>
        )}
      </Card>

      <Card
        title="Password protection"
        actions={form.has_password ? <Pill tone="green">on · {form.basic_auth_user}</Pill> : <Pill>off</Pill>}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="User name">
            <input className={inputCls} value={form.basic_auth_user} onChange={(e) => set('basic_auth_user', e.target.value)} autoComplete="off" />
          </Field>
          <Field label={form.has_password ? 'New password (leave empty to keep)' : 'Password'}>
            <input className={inputCls} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
          </Field>
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <PrimaryButton busy={saving} onClick={() => save()} disabled={!form.basic_auth_user.trim() || (!form.has_password && password.length < 8)}>
            <Lock className="h-3.5 w-3.5" /> {form.has_password ? 'Update' : 'Turn on'}
          </PrimaryButton>
          {form.has_password && (
            <GhostButton tone="danger" busy={saving} onClick={() => save({ remove_password: true })}>
              Turn off
            </GhostButton>
          )}
        </div>
      </Card>

      <Card title="Hardening" actions={<ShieldCheck className="h-4 w-4 text-emerald-400" />}>
        <ul className="divide-y divide-white/[0.05]">
          {HARDENING.map((h) => (
            <li key={h.key} className="flex items-center justify-between gap-4 py-3 first:pt-0 last:pb-0">
              <div className="min-w-0">
                <div className="text-xs font-medium text-zinc-100">{h.title}</div>
                <div className="mt-0.5 text-[11px] text-zinc-500">{h.text}</div>
              </div>
              <div className="shrink-0">
                <ShadcnSwitch checked={Boolean(form[h.key])} onChange={(v) => set(h.key, v as never)} />
              </div>
            </li>
          ))}
        </ul>
        <div className="mt-4 flex justify-end">
          <PrimaryButton busy={saving} onClick={() => save()}>Save changes</PrimaryButton>
        </div>
      </Card>
    </div>
  );
};
