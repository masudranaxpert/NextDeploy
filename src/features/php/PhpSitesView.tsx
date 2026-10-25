import React, { useEffect, useMemo, useState } from 'react';
import { FileCode2, Plus, Search, Server, Settings2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import type { PhpStatusData } from '../../lib/api/phpApi';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { SiteCard } from './components/SiteCard';
import { DeleteSiteDialog } from './components/DeleteSiteDialog';
import { HtmlIcon, PhpIcon, WordPressIcon } from '../../components/ui/BrandIcons';
import { Card, Field, GhostButton, Pill, PrimaryButton, SITE_TYPE_LABEL, inputCls, selectCls, prefetchPhpSite } from './ui';
import { PhpVersionSelect } from './PhpVersionSelect';

type SiteType = 'php' | 'wordpress' | 'static';

const TYPE_CARDS: Array<{ id: SiteType; title: string; text: string; icon: React.ReactNode }> = [
  { id: 'php', title: 'PHP', text: 'Laravel, Symfony, custom', icon: <PhpIcon className="h-4 w-4" /> },
  { id: 'wordpress', title: 'WordPress', text: 'One-click install', icon: <WordPressIcon className="h-4 w-4" /> },
  { id: 'static', title: 'Static HTML', text: 'No PHP', icon: <HtmlIcon className="h-4 w-4" /> },
];

interface Props {
  apps: AppStack[];
  isAdmin: boolean;
  onOpenSite: (id: string) => void;
  onRefresh: () => Promise<void>;
  onGoInstance: () => void;
}

export const PhpSitesView: React.FC<Props> = ({ apps, isAdmin, onOpenSite, onRefresh, onGoInstance }) => {
  const sites = useMemo(() => apps.filter((a) => a.php), [apps]);
  const [status, setStatus] = useState<PhpStatusData | null>(null);
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const [toDelete, setToDelete] = useState<{ id: string; name: string } | null>(null);
  const [creating, setCreating] = useState(false);

  const [name, setName] = useState('');
  const [type, setType] = useState<SiteType>('php');
  const [version, setVersion] = useState('');
  const [title, setTitle] = useState('');
  const [wpUser, setWpUser] = useState('admin');
  const [wpEmail, setWpEmail] = useState('');

  // Load the first sites' details now, so opening one shows its settings without waiting.
  useEffect(() => {
    sites.slice(0, 6).forEach((s) => prefetchPhpSite(s.id));
  }, [sites.map((s) => s.id).join(',')]); // eslint-disable-line react-hooks/exhaustive-deps

  const loadStatus = () => backendApi.phpStatus().then((r) => r.ok && r.data && setStatus(r.data));
  useEffect(() => {
    void loadStatus();
  }, []);

  const installed = status?.installed_versions ?? [];
  useEffect(() => {
    if (!version && installed.length) {
      setVersion(installed.includes(status?.default_version ?? '') ? status!.default_version : installed[0]);
    }
  }, [installed.join(','), status?.default_version]); // eslint-disable-line react-hooks/exhaustive-deps

  const needsPhp = type !== 'static';
  const blocker = !status
    ? 'Loading…'
    : !status.enabled
    ? 'PHP hosting is not enabled yet.'
    : needsPhp && installed.length === 0
    ? 'No PHP version is installed yet.'
    : type === 'wordpress' && !status.mysql.ready
    ? 'The database server is not running.'
    : '';

  const create = async () => {
    setCreating(true);
    const res = await backendApi.phpCreateSite({
      name: name.trim().toLowerCase(),
      type,
      php_version: needsPhp ? version : undefined,
      title: title.trim() || undefined,
      wp_admin_user: type === 'wordpress' ? wpUser.trim() || undefined : undefined,
      wp_admin_email: type === 'wordpress' ? wpEmail.trim() || undefined : undefined,
    });
    setCreating(false);
    if (!res.ok || !res.data) {
      toast.error(res.error || 'Could not create the site');
      return;
    }
    toast.success(type === 'wordpress' ? 'Site created, installing WordPress…' : 'Site created');
    setOpen(false);
    setName('');
    await onRefresh();
    onOpenSite(res.data.id);
  };

  const filtered = sites.filter((s) => !search || s.name.toLowerCase().includes(search.toLowerCase()) || (s.primaryDomain || '').toLowerCase().includes(search.toLowerCase()));

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-zinc-100">PHP Sites</h1>
        </div>
        <div className="flex items-center gap-2">
          {isAdmin && (
            <GhostButton onClick={onGoInstance}>
              <Settings2 className="h-3.5 w-3.5 text-zinc-400" /> Instance
            </GhostButton>
          )}
          <PrimaryButton onClick={() => setOpen(true)} disabled={status !== null && !status.enabled}>
            <Plus className="h-3.5 w-3.5" /> New site
          </PrimaryButton>
        </div>
      </div>

      {status && !status.enabled && (
        <Card>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <Server className="h-5 w-5 text-orange-400" />
              <div>
                <div className="text-sm font-semibold text-zinc-100">PHP hosting is off</div>
                <p className="text-xs text-zinc-400">{isAdmin ? 'Set it up on the Instance page.' : 'Ask an administrator to enable it.'}</p>
              </div>
            </div>
            {isAdmin && <PrimaryButton onClick={onGoInstance}>Set up PHP hosting</PrimaryButton>}
          </div>
        </Card>
      )}

      {status?.enabled && installed.length === 0 && (
        <Card>
          <div className="text-xs text-amber-200">
            No PHP version is installed yet. {isAdmin ? <button type="button" onClick={onGoInstance} className="cursor-pointer underline">Install one on the Instance page.</button> : 'Ask an administrator to install one.'}
          </div>
        </Card>
      )}

      {sites.length > 0 && (
        <div className="relative max-w-xs">
          <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-500" />
          <input className={`${inputCls} pl-8`} placeholder="Search sites" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
      )}

      {sites.length === 0 ? (
        status?.enabled && (
          <div className="flex flex-col items-center rounded-2xl border border-dashed border-white/[0.08] bg-[#111115]/50 px-6 py-16 text-center">
            <FileCode2 className="h-7 w-7 text-zinc-500" />
            <h3 className="mt-4 text-sm font-semibold text-zinc-200">No sites yet</h3>
            <p className="mt-1.5 text-xs text-zinc-400">Create your first site.</p>
          </div>
        )
      ) : (
        <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((s) => (
            <SiteCard key={s.id} site={s} onOpenSite={onOpenSite} onDelete={(x) => setToDelete({ id: x.id, name: x.name })} />
          ))}
        </div>
      )}

      <DeleteSiteDialog site={toDelete} onClose={() => setToDelete(null)} onDeleted={() => onRefresh()} />

      <ShadcnDialog
        open={open}
        onClose={() => !creating && setOpen(false)}
        title="New site"
        icon={<Plus className="h-4 w-4" />}
        maxWidth="lg"
        footer={
          <>
            <GhostButton onClick={() => setOpen(false)} disabled={creating}>Cancel</GhostButton>
            <PrimaryButton busy={creating} onClick={create} disabled={!name.trim() || blocker !== ''}>Create site</PrimaryButton>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-3 gap-2">
            {TYPE_CARDS.map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => setType(t.id)}
                className={`cursor-pointer rounded-xl border p-3 text-left transition-colors ${
                  type === t.id ? 'border-orange-500/50 bg-orange-500/10' : 'border-white/[0.08] bg-zinc-900/60 hover:bg-zinc-900'
                }`}
              >
                <div className="flex items-center gap-1.5 text-zinc-100">{t.icon}<span className="text-xs font-semibold">{t.title}</span></div>
                <div className="mt-1 text-[11px] leading-snug text-zinc-400">{t.text}</div>
              </button>
            ))}
          </div>

          <Field label="Site name">
            <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="my-site" autoFocus />
          </Field>

          {needsPhp && (
            <Field label="PHP version">
              <PhpVersionSelect
                value={version}
                onChange={setVersion}
                installedVersions={installed}
              />
            </Field>
          )}

          {type === 'wordpress' && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Site title"><input className={inputCls} value={title} onChange={(e) => setTitle(e.target.value)} placeholder={name || 'My blog'} /></Field>
              <Field label="Admin user"><input className={inputCls} value={wpUser} onChange={(e) => setWpUser(e.target.value)} /></Field>
              <div className="sm:col-span-2">
                <Field label="Admin e-mail"><input type="email" className={inputCls} value={wpEmail} onChange={(e) => setWpEmail(e.target.value)} placeholder="you@example.com" /></Field>
              </div>
            </div>
          )}

          {blocker && blocker !== 'Loading…' && (
            <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
              {blocker} {isAdmin ? 'Fix this on the Instance page.' : 'Ask an administrator.'}
            </div>
          )}
        </div>
      </ShadcnDialog>
    </div>
  );
};
