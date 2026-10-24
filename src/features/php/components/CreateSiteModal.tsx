import React, { useState, useEffect } from 'react';
import { FileCode2, Globe, Plus, Sparkles } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../components/ui/Primitives';
import { Field, GhostButton, PrimaryButton, inputCls, selectCls } from '../ui';
import { backendApi } from '../../../lib/backendApi';
import type { PhpStatusData } from '../../../lib/api/phpApi';

import { PhpIcon, WordPressIcon, StaticSiteIcon } from '../../../components/ui/BrandIcons';

type SiteType = 'php' | 'wordpress' | 'static';

const TYPE_CARDS: Array<{ id: SiteType; title: string; subtitle: string; icon: React.ReactNode }> = [
  {
    id: 'php',
    title: 'PHP / Laravel',
    subtitle: 'Laravel, Symfony, or custom PHP scripts',
    icon: <PhpIcon className="h-4 w-4 text-[#777BB4]" />,
  },
  {
    id: 'wordpress',
    title: 'WordPress',
    subtitle: 'Automated CMS install & MySQL database',
    icon: <WordPressIcon className="h-4 w-4 text-[#3858E9]" />,
  },
  {
    id: 'static',
    title: 'Static Site',
    subtitle: 'HTML, CSS, JS served by Caddy',
    icon: <StaticSiteIcon className="h-4 w-4 text-sky-400" />,
  },
];

interface CreateSiteModalProps {
  open: boolean;
  status: PhpStatusData | null;
  isAdmin: boolean;
  onClose: () => void;
  onCreated: (siteId: string) => void;
  onGoInstance: () => void;
}

export const CreateSiteModal: React.FC<CreateSiteModalProps> = ({
  open,
  status,
  isAdmin,
  onClose,
  onCreated,
  onGoInstance,
}) => {
  const [name, setName] = useState('');
  const [type, setType] = useState<SiteType>('php');
  const [version, setVersion] = useState('');
  const [title, setTitle] = useState('');
  const [wpUser, setWpUser] = useState('admin');
  const [wpEmail, setWpEmail] = useState('');
  const [creating, setCreating] = useState(false);

  const installed = status?.installed_versions ?? [];

  useEffect(() => {
    if (!version && installed.length) {
      setVersion(
        installed.includes(status?.default_version ?? '')
          ? status!.default_version
          : installed[0]
      );
    }
  }, [installed, status?.default_version, version]);

  const needsPhp = type !== 'static';
  const blocker = !status
    ? 'Checking system status...'
    : !status.enabled
    ? 'PHP hosting is disabled on this server.'
    : needsPhp && installed.length === 0
    ? 'No PHP version is currently installed.'
    : type === 'wordpress' && !status.mysql.ready
    ? 'Database server (MariaDB) is not running.'
    : '';

  const handleCreate = async () => {
    const cleanName = name.trim().toLowerCase();
    if (!cleanName) return;

    setCreating(true);
    const res = await backendApi.phpCreateSite({
      name: cleanName,
      type,
      php_version: needsPhp ? version : undefined,
      title: title.trim() || undefined,
      wp_admin_user: type === 'wordpress' ? wpUser.trim() || undefined : undefined,
      wp_admin_email: type === 'wordpress' ? wpEmail.trim() || undefined : undefined,
    });
    setCreating(false);

    if (!res.ok || !res.data) {
      toast.error(res.error || 'Failed to create site');
      return;
    }

    toast.success(
      type === 'wordpress' ? 'Site created. Installing WordPress...' : 'Site created successfully'
    );
    setName('');
    setTitle('');
    setWpEmail('');
    onClose();
    onCreated(res.data.id);
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={() => !creating && onClose()}
      title="Create New Site"
      description="Choose your site stack and enter a unique site identifier."
      icon={<Plus className="h-4 w-4 text-orange-400" />}
      maxWidth="lg"
      footer={
        <div className="flex w-full flex-wrap items-center justify-between gap-3">
          <div className="text-[11px] text-zinc-500">
            {type === 'static' ? 'Static files served via Caddy' : `Using PHP ${version || '8.3'}`}
          </div>
          <div className="flex items-center gap-2">
            <GhostButton onClick={onClose} disabled={creating}>
              Cancel
            </GhostButton>
            <PrimaryButton
              busy={creating}
              onClick={handleCreate}
              disabled={!name.trim() || blocker !== ''}
            >
              Create Site
            </PrimaryButton>
          </div>
        </div>
      }
    >
      <div className="space-y-4">
        {/* Site Type Cards */}
        <div>
          <label className="block text-[11px] font-medium text-zinc-300 mb-2">
            Stack Type
          </label>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
            {TYPE_CARDS.map((t) => {
              const isSelected = type === t.id;
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => setType(t.id)}
                  className={`flex flex-col justify-between rounded-xl border p-3 text-left transition-all cursor-pointer ${
                    isSelected
                      ? 'border-orange-500/50 bg-orange-500/10 text-white shadow-xs'
                      : 'border-white/[0.08] bg-zinc-900/60 text-zinc-400 hover:border-white/[0.15] hover:bg-zinc-900'
                  }`}
                >
                  <div className="flex items-center gap-2 text-zinc-200">
                    <span className={isSelected ? 'text-orange-400' : 'text-zinc-400'}>
                      {t.icon}
                    </span>
                    <span className="text-xs font-semibold">{t.title}</span>
                  </div>
                  <div className="mt-1.5 text-[11px] leading-snug text-zinc-400">
                    {t.subtitle}
                  </div>
                </button>
              );
            })}
          </div>
        </div>

        {/* Site Name Field */}
        <Field label="Site Name" hint="Lowercase alphanumeric characters and hyphens (e.g. blog-app)">
          <input
            className={inputCls}
            value={name}
            onChange={(e) =>
              setName(
                e.target.value
                  .toLowerCase()
                  .replace(/[^a-z0-9-]/g, '')
                  .replace(/--+/g, '-')
              )
            }
            placeholder="my-site"
            autoFocus
          />
        </Field>

        {/* PHP Version Select (if PHP or WordPress) */}
        {needsPhp && (
          <Field label="PHP Version" hint="Shared Docker runtime image">
            <select
              className={selectCls}
              value={version}
              onChange={(e) => setVersion(e.target.value)}
            >
              {installed.map((v) => (
                <option key={v} value={v}>
                  PHP {v}
                </option>
              ))}
            </select>
          </Field>
        )}

        {/* WordPress Specific Configuration */}
        {type === 'wordpress' && (
          <div className="rounded-xl border border-white/[0.07] bg-black/40 p-3.5 space-y-3">
            <div className="text-xs font-medium text-zinc-200">WordPress Initial Setup</div>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Site Title">
                <input
                  className={inputCls}
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder={name ? `${name} Site` : 'My WordPress Site'}
                />
              </Field>
              <Field label="Admin Username">
                <input
                  className={inputCls}
                  value={wpUser}
                  onChange={(e) => setWpUser(e.target.value)}
                  placeholder="admin"
                />
              </Field>
              <div className="sm:col-span-2">
                <Field label="Admin Email" hint="One-time secure admin password is generated upon install">
                  <input
                    type="email"
                    className={inputCls}
                    value={wpEmail}
                    onChange={(e) => setWpEmail(e.target.value)}
                    placeholder="admin@example.com"
                  />
                </Field>
              </div>
            </div>
          </div>
        )}

        {/* Blocker alert banner */}
        {blocker && blocker !== 'Checking system status...' && (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-200">
            <span>{blocker}</span>
            {isAdmin && (
              <button
                type="button"
                onClick={() => {
                  onClose();
                  onGoInstance();
                }}
                className="font-medium underline hover:text-white cursor-pointer"
              >
                Go to Instance setup
              </button>
            )}
          </div>
        )}
      </div>
    </ShadcnDialog>
  );
};
