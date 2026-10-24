import React, { useEffect, useMemo, useState } from 'react';
import { FileCode2, Globe, Plus, Search, Server, Settings2, Sparkles, Database } from 'lucide-react';
import { AppStack } from '../../types/nextdeploy';
import { backendApi } from '../../lib/backendApi';
import type { PhpStatusData } from '../../lib/api/phpApi';
import { Card, EmptyBlock, GhostButton, MetricStat, PrimaryButton, inputCls } from './ui';
import { SiteCard } from './components/SiteCard';
import { CreateSiteModal } from './components/CreateSiteModal';

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
  const [createModalOpen, setCreateModalOpen] = useState(false);

  const loadStatus = () =>
    backendApi.phpStatus().then((r) => r.ok && r.data && setStatus(r.data));

  useEffect(() => {
    void loadStatus();
  }, []);

  const wpCount = sites.filter((s) => s.php?.siteType === 'wordpress').length;
  const standardPhpCount = sites.filter((s) => s.php?.siteType === 'php').length;
  const staticCount = sites.filter((s) => s.php?.siteType === 'static').length;

  const filtered = sites.filter((s) => {
    if (!search) return true;
    const query = search.toLowerCase();
    const nameMatch = s.name.toLowerCase().includes(query);
    const domainMatch = (s.primaryDomain || '').toLowerCase().includes(query);
    const versionMatch = (s.php?.phpVersion || '').toLowerCase().includes(query);
    return nameMatch || domainMatch || versionMatch;
  });

  return (
    <div className="space-y-5">
      {/* Page Header */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100">PHP Sites</h1>
          <p className="mt-0.5 text-xs text-zinc-400">
            Hosted web applications and domain management
          </p>
        </div>

        <div className="flex items-center gap-2">
          {isAdmin && (
            <GhostButton onClick={onGoInstance} className="h-8.5">
              <Settings2 className="h-3.5 w-3.5 text-zinc-400" />
              <span>Instance Setup</span>
            </GhostButton>
          )}
          <PrimaryButton
            onClick={() => setCreateModalOpen(true)}
            disabled={status !== null && !status.enabled}
            className="h-8.5"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>New Site</span>
          </PrimaryButton>
        </div>
      </div>

      {/* Metrics Summary Strip */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MetricStat
          label="Total Sites"
          value={sites.length}
          subtext={`${standardPhpCount} PHP · ${wpCount} WP · ${staticCount} Static`}
          icon={<FileCode2 className="h-4 w-4" />}
          tone="orange"
        />
        <MetricStat
          label="PHP Engine"
          value={status?.enabled ? 'Active' : 'Disabled'}
          subtext={
            status?.enabled
              ? `${status.installed_versions?.length || 0} runtimes installed`
              : 'Requires activation'
          }
          icon={<Server className="h-4 w-4" />}
          tone={status?.enabled ? 'emerald' : 'zinc'}
        />
        <MetricStat
          label="Database Server"
          value={status?.mysql.ready ? 'Running' : 'Stopped'}
          subtext={status?.mysql.ready ? 'MariaDB 11.4' : 'Offline'}
          icon={<Database className="h-4 w-4" />}
          tone={status?.mysql.ready ? 'emerald' : 'amber'}
        />
        <MetricStat
          label="CMS Instances"
          value={wpCount}
          subtext="Automated WordPress"
          icon={<Sparkles className="h-4 w-4" />}
          tone="violet"
        />
      </div>

      {/* Disabled Engine Alert Banner */}
      {status && !status.enabled && (
        <Card className="border-amber-500/30 bg-amber-500/[0.04]">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-amber-500/30 bg-amber-500/10 text-amber-400">
                <Server className="h-4 w-4" />
              </div>
              <div>
                <h3 className="text-xs font-semibold text-zinc-200">PHP Hosting Engine is Offline</h3>
                <p className="mt-0.5 text-xs text-zinc-400">
                  {isAdmin
                    ? 'Enable PHP hosting in Instance Setup to provision PHP-FPM pools and MariaDB.'
                    : 'Contact your administrator to enable PHP hosting services.'}
                </p>
              </div>
            </div>
            {isAdmin && (
              <PrimaryButton onClick={onGoInstance}>
                Open Instance Setup
              </PrimaryButton>
            )}
          </div>
        </Card>
      )}

      {/* Search & Filter Bar */}
      {sites.length > 0 && (
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative w-full max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-3.5 w-3.5 text-zinc-500" />
            <input
              className={`${inputCls} pl-9`}
              placeholder="Search by site name, domain, or PHP version..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>

          <div className="text-xs text-zinc-500">
            Showing <span className="font-mono text-zinc-300 font-semibold">{filtered.length}</span> of{' '}
            <span className="font-mono text-zinc-300">{sites.length}</span> sites
          </div>
        </div>
      )}

      {/* Sites Grid or Empty State */}
      {sites.length === 0 ? (
        <EmptyBlock
          icon={<FileCode2 className="h-5 w-5 text-orange-400" />}
          title="No PHP Sites Created Yet"
          description="Deploy WordPress, Laravel, or custom PHP code with isolated pools and automated SSL."
          action={
            <PrimaryButton
              onClick={() => setCreateModalOpen(true)}
              disabled={status !== null && !status.enabled}
            >
              <Plus className="h-3.5 w-3.5" /> Create First Site
            </PrimaryButton>
          }
        />
      ) : filtered.length === 0 ? (
        <div className="rounded-xl border border-white/[0.08] bg-[#111115]/50 p-8 text-center text-xs text-zinc-400">
          No sites match &ldquo;<span className="text-zinc-200 font-mono">{search}</span>&rdquo;.
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((s) => (
            <SiteCard key={s.id} site={s} onOpenSite={onOpenSite} />
          ))}
        </div>
      )}

      {/* Create Site Modal */}
      <CreateSiteModal
        open={createModalOpen}
        status={status}
        isAdmin={isAdmin}
        onClose={() => setCreateModalOpen(false)}
        onCreated={async (newId) => {
          await onRefresh();
          onOpenSite(newId);
        }}
        onGoInstance={onGoInstance}
      />
    </div>
  );
};
