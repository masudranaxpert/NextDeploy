import React, { useEffect, useMemo, useState } from 'react';
import { FileCode2, LayoutGrid, List, Plus, Search, Server, Settings2 } from 'lucide-react';
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

const TYPE_CARDS: Array<{ id: SiteType; title: string; short: string; text: string; icon: React.ReactNode }> = [
  { id: 'php', title: 'PHP', short: 'Laravel, custom', text: 'Laravel, Symfony, custom', icon: <PhpIcon className="h-4 w-4" /> },
  { id: 'wordpress', title: 'WordPress', short: 'One-click install', text: 'One-click install', icon: <WordPressIcon className="h-4 w-4" /> },
  { id: 'static', title: 'Static HTML', short: 'No PHP runtime', text: 'No PHP · Pure HTML/CSS', icon: <HtmlIcon className="h-4 w-4" /> },
];

interface Props {
  apps: AppStack[];
  isAdmin: boolean;
  onOpenSite: (id: string) => void;
  onDeleteSite?: (id: string) => void;
  onRefresh: () => Promise<void>;
  onGoInstance: () => void;
}

export const PhpSitesView: React.FC<Props> = ({ apps, isAdmin, onOpenSite, onDeleteSite, onRefresh, onGoInstance }) => {
  const sites = useMemo(() => apps.filter((a) => a.php), [apps]);
  const [status, setStatus] = useState<PhpStatusData | null>(null);
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const [toDelete, setToDelete] = useState<{ id: string; name: string } | null>(null);
  const [creating, setCreating] = useState(false);
  const [selectorLayout, setSelectorLayout] = useState<'grid' | 'list'>('grid');

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
        <div className="relative">
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

      <DeleteSiteDialog
        site={toDelete}
        onClose={() => setToDelete(null)}
        onDeleted={async (deletedId) => {
          onDeleteSite?.(deletedId);
          await onRefresh();
        }}
      />

      <ShadcnDialog
        open={open}
        onClose={() => !creating && setOpen(false)}
        title="New site"
        icon={<Plus className="h-4 w-4" />}
        maxWidth="lg"
        headerActions={
          <button
            type="button"
            onClick={() => setSelectorLayout((p) => (p === 'grid' ? 'list' : 'grid'))}
            title={selectorLayout === 'grid' ? 'Switch to stacked list' : 'Switch to 3-column grid'}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:text-zinc-100 hover:bg-zinc-800 cursor-pointer min-h-[32px]"
          >
            {selectorLayout === 'grid' ? <List className="h-3.5 w-3.5" /> : <LayoutGrid className="h-3.5 w-3.5" />}
            <span>{selectorLayout === 'grid' ? 'List' : 'Grid'}</span>
          </button>
        }
        footer={
          <>
            <GhostButton onClick={() => setOpen(false)} disabled={creating}>Cancel</GhostButton>
            <PrimaryButton busy={creating} onClick={create} disabled={!name.trim() || blocker !== ''}>Create site</PrimaryButton>
          </>
        }
      >
        <div className="space-y-4">
          {selectorLayout === 'grid' ? (
            <div role="radiogroup" aria-label="Site type" className="grid grid-cols-1 sm:grid-cols-3 gap-2">
              {TYPE_CARDS.map((t) => {
                const isSelected = type === t.id;
                return (
                  <button
                    key={t.id}
                    type="button"
                    role="radio"
                    aria-checked={isSelected}
                    onClick={() => setType(t.id)}
                    className={`relative flex flex-col items-start rounded-lg border p-3 text-left transition-colors cursor-pointer ${
                      isSelected
                        ? 'border-orange-500/60 bg-orange-500/[0.08]'
                        : 'border-white/[0.08] bg-zinc-900/60 hover:border-white/[0.16] hover:bg-zinc-900'
                    }`}
                  >
                    <div className="flex w-full items-center justify-between">
                      <span className={`flex h-7 w-9 items-center justify-center rounded-md border ${isSelected ? 'border-orange-500/30 bg-orange-500/[0.06]' : 'border-white/[0.06] bg-white/[0.03]'}`}>
                        {t.icon}
                      </span>
                      <span aria-hidden="true" className={`h-2 w-2 rounded-full ${isSelected ? 'bg-orange-500' : 'bg-white/[0.15]'}`} />
                    </div>
                    <div className="mt-2.5 whitespace-nowrap text-[13px] font-semibold leading-tight text-zinc-100">{t.title}</div>
                    <div className={`mt-1 w-full truncate whitespace-nowrap text-[11px] leading-tight ${isSelected ? 'text-orange-200/80' : 'text-zinc-500'}`}>{t.short}</div>
                  </button>
                );
              })}
            </div>
          ) : (
            <div role="radiogroup" aria-label="Site type" className="space-y-2">
              {TYPE_CARDS.map((t) => {
                const isSelected = type === t.id;
                return (
                  <button
                    key={t.id}
                    type="button"
                    role="radio"
                    aria-checked={isSelected}
                    onClick={() => setType(t.id)}
                    className={`flex w-full items-center justify-between gap-3 rounded-lg border px-3.5 py-2.5 text-left transition-colors cursor-pointer ${
                      isSelected
                        ? 'border-orange-500/60 bg-orange-500/[0.08]'
                        : 'border-white/[0.08] bg-zinc-900/60 hover:border-white/[0.16] hover:bg-zinc-900'
                    }`}
                  >
                    <span className="flex min-w-0 items-center gap-3">
                      <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-md border ${isSelected ? 'border-orange-500/30 bg-orange-500/[0.06]' : 'border-white/[0.06] bg-white/[0.03]'}`}>
                        {t.icon}
                      </span>
                      <span className="min-w-0">
                        <span className="block text-[13.5px] font-semibold leading-tight text-zinc-100">{t.title}</span>
                        <span className={`mt-0.5 block truncate text-[12px] leading-tight ${isSelected ? 'text-orange-200/80' : 'text-zinc-500'}`}>{t.text}</span>
                      </span>
                    </span>
                    <span aria-hidden="true" className={`flex h-4 w-4 shrink-0 items-center justify-center rounded-full border ${isSelected ? 'border-orange-500 bg-orange-500/20' : 'border-white/20'}`}>
                      {isSelected && <span className="h-2 w-2 rounded-full bg-orange-500" />}
                    </span>
                  </button>
                );
              })}
            </div>
          )}

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label htmlFor="new-site-name" className="text-[11px] font-medium text-zinc-300">
                Site name
              </label>
              {name.trim() && (
                <span className="font-mono text-[10px] text-zinc-500">created as {name.trim().toLowerCase()}</span>
              )}
            </div>
            <input id="new-site-name" className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="my-site" autoFocus />
          </div>

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
