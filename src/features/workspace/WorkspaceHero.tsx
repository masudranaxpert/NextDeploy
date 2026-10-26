import React from 'react';
import { motion } from 'motion/react';
import {
  ArrowLeft,
  ArrowUpRight,
  Check,
  Copy,
  GitBranch,
  Globe,
  Pin,
  Play,
  RefreshCw,
  RotateCw,
  Sliders,
  Square,
  Trash2,
  User,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, AppWorkspaceTab, DeploymentRecord } from '../../types/nextdeploy';
import { ActionMenu, StatusBadge } from '../../components/ui/Primitives';
import { SiteTypeIcon } from '../../components/ui/BrandIcons';
import { SITE_TYPE_LABEL } from '../php/ui';
import { copyToClipboard } from '../../lib/clipboard';
import { backendApi } from '../../lib/backendApi';

interface WorkspaceHeroProps {
  app: AppStack;
  tabs: { id: AppWorkspaceTab; label: string; count?: number; icon?: React.ReactNode }[];
  activeTab: AppWorkspaceTab;
  onSelectTab: (tab: AppWorkspaceTab) => void;
  onBack: () => void;
  runningContainersCount: number;
  productionUrl: string;
  activeSlotLower: string;
  isPinnedProduction: boolean;
  hasGitConnection: boolean;
  copiedOwner: boolean;
  onCopyOwner: () => void | Promise<void>;
  onToggleStopOrStart: () => void | Promise<void>;
  onTriggerDeploy: (options?: { noCache?: boolean; rollbackFrom?: DeploymentRecord }) => void | Promise<void>;
  onOpenLimits: () => void;
  onOpenDelete: () => void;
  onOpenGit: () => void;
}

/** Project hero: identity row, actions, metadata chip strip, and the section tabs. */
export const WorkspaceHero: React.FC<WorkspaceHeroProps> = ({
  app,
  tabs,
  activeTab,
  onSelectTab,
  onBack,
  runningContainersCount,
  productionUrl,
  activeSlotLower,
  isPinnedProduction,
  hasGitConnection,
  copiedOwner,
  onCopyOwner,
  onToggleStopOrStart,
  onTriggerDeploy,
  onOpenLimits,
  onOpenDelete,
  onOpenGit,
}) => {
  return (
    <div className="rounded-2xl border border-white/[0.07] bg-[#111115]/90 p-5 shadow-[inset_0_1px_0_0_rgba(255,255,255,0.04)]">
      <div className={app.php ? 'flex items-center justify-between gap-3' : 'flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between'}>
        <div className="flex items-center gap-3.5 min-w-0">
          <button
            type="button"
            onClick={onBack}
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900 text-zinc-400 hover:border-white/[0.16] hover:text-zinc-100 transition-colors cursor-pointer"
            title="Back to projects"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>

          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-white/[0.09] bg-zinc-900 font-display text-xs font-bold text-orange-400 uppercase shadow-inner">
            {app.php ? (
              <SiteTypeIcon type={app.php.siteType} className="h-5 w-5" />
            ) : (
              app.name.slice(0, 2)
            )}
          </div>

          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate font-mono text-lg font-semibold leading-none tracking-tight text-zinc-100">
                {app.name}
              </h1>
              <StatusBadge
                status={
                  app.php
                    ? app.status
                    : runningContainersCount > 0
                    ? 'running'
                    : app.status === 'failed' || app.status === 'building'
                    ? app.status
                    : 'stopped'
                }
              />
              {isPinnedProduction && (
                <span className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-amber-300">
                  <Pin className="h-2.5 w-2.5" />
                  Pinned
                </span>
              )}
            </div>
            {productionUrl && (
              <a
                href={productionUrl}
                target="_blank"
                rel="noreferrer"
                className="group mt-1.5 inline-flex max-w-full items-center gap-1.5 font-mono text-xs text-zinc-400 transition-colors hover:text-orange-300"
              >
                <Globe className="h-3 w-3 shrink-0 text-emerald-400" />
                <span className="truncate">{productionUrl.replace(/^https?:\/\//, '')}</span>
                <ArrowUpRight className="h-3 w-3 shrink-0 opacity-60 transition-transform group-hover:-translate-y-0.5 group-hover:translate-x-0.5" />
              </a>
            )}
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2">
          {!app.php && (
            <button
              type="button"
              onClick={() => onTriggerDeploy()}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white shadow-xs transition-all cursor-pointer"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              Deploy
            </button>
          )}

          {!app.php && (
            <button
              type="button"
              onClick={async () => {
                toast.info(`Restarting ${app.name} containers...`);
                const res = await backendApi.composeAction(app.id, 'restart');
                if (!res.ok) {
                  toast.error(res.error || `Failed to restart ${app.name}`);
                  return;
                }
                toast.success(`Restarting ${app.name} containers`);
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 transition-colors cursor-pointer"
            >
              <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
              Restart
            </button>
          )}

          <ActionMenu
            items={[
              {
                label: app.status === 'stopped' ? 'Start Compose Stack' : 'Stop Compose Stack',
                icon:
                  app.status === 'stopped' ? (
                    <Play className="h-3.5 w-3.5" />
                  ) : (
                    <Square className="h-3.5 w-3.5" />
                  ),
                onClick: onToggleStopOrStart,
              },
              {
                label: 'Rebuild Without Cache',
                icon: <RefreshCw className="h-3.5 w-3.5" />,
                onClick: () => onTriggerDeploy({ noCache: true }),
              },
              {
                label: 'Resource Limits',
                icon: <Sliders className="h-3.5 w-3.5" />,
                onClick: onOpenLimits,
              },
              {
                label: 'Copy Project ID',
                icon: <Copy className="h-3.5 w-3.5" />,
                onClick: async () => {
                  const ok = await copyToClipboard(app.id);
                  if (ok) toast.success(`Copied ${app.id}`);
                },
              },
              {
                label: 'Delete Project',
                icon: <Trash2 className="h-3.5 w-3.5" />,
                variant: 'danger' as const,
                onClick: onOpenDelete,
              },
            ].filter((it) => !app.php || it.label === 'Copy Project ID' || it.label === 'Delete Project')}
          />
        </div>
      </div>

      {/* Compact metadata strip — quick facts as tappable chips */}
      <div className="mt-4 flex flex-wrap items-center gap-2 rounded-xl border border-white/[0.05] bg-white/[0.02] px-3 py-2">
        {app.php ? (
          <button
            type="button"
            onClick={() => onSelectTab('php-settings')}
            title="Change PHP version in Settings"
            className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-lg border border-sky-500/20 bg-sky-500/[0.07] px-2.5 py-1 font-mono text-[12px] text-sky-200/90 transition-colors hover:border-sky-500/40 hover:text-sky-100 cursor-pointer"
          >
            <SiteTypeIcon type={app.php.siteType} className="h-3.5 w-3.5" />
            {SITE_TYPE_LABEL[app.php.siteType] ?? app.php.siteType}
            <span className="text-zinc-600" aria-hidden="true">·</span>
            <span>PHP {app.php.phpVersion}</span>
          </button>
        ) : (
          <span className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-lg border border-sky-500/25 bg-sky-500/[0.07] px-2.5 py-1 font-mono text-[12px] text-sky-300">
            <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-sky-400" />
            production · {activeSlotLower.toUpperCase()}
          </span>
        )}
        <button
          type="button"
          onClick={() => void onCopyOwner()}
          title="Click to copy owner name"
          className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-lg border border-white/[0.06] bg-white/[0.03] px-2.5 py-1 font-mono text-[12px] text-zinc-300 transition-colors hover:border-white/[0.14] hover:text-zinc-100 cursor-pointer"
        >
          <User className="h-3.5 w-3.5 shrink-0 text-zinc-500" />
          {app.owner}
          {copiedOwner && <Check className="h-3 w-3 shrink-0 text-emerald-400" />}
        </button>
        {hasGitConnection && app.gitBranch && (
          <button
            type="button"
            onClick={onOpenGit}
            title="Open Git settings"
            className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-lg border border-white/[0.06] bg-white/[0.03] px-2.5 py-1 font-mono text-[12px] text-zinc-300 transition-colors hover:border-white/[0.14] hover:text-zinc-100 cursor-pointer"
          >
            <GitBranch className="h-3.5 w-3.5 shrink-0 text-orange-400" />
            {app.gitBranch}
            {app.gitCommitSha && app.gitCommitSha !== 'blue' && app.gitCommitSha !== 'green' && (
              <span className="text-zinc-500">@{app.gitCommitSha.slice(0, 7)}</span>
            )}
          </button>
        )}
        {!app.php && (
          <span className="ml-auto hidden whitespace-nowrap font-mono text-[11px] text-zinc-500 sm:inline">
            {runningContainersCount}/{app.services.length} containers · {app.cpuPct.toFixed(2)}% CPU · {app.memMB} MB
          </span>
        )}
      </div>

      {/* Navigation Tabs — Segmented Buttons on mobile, sleek tabs on desktop */}
      <div className="mt-5 -mb-5 border-t border-white/[0.06] pt-2">
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:flex md:flex-wrap md:items-center md:gap-x-1">
          {tabs.map((t) => {
            const active = activeTab === t.id;
            return (
              <button
                key={t.id}
                type="button"
                onClick={() => onSelectTab(t.id)}
                className={`group relative flex h-11 items-center justify-between rounded-xl px-3 text-xs sm:text-[13px] transition-all cursor-pointer whitespace-nowrap select-none active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-orange-500 md:h-auto md:py-2.5 md:text-xs md:justify-start md:gap-1.5 ${
                  active
                    ? 'border border-orange-500/40 bg-orange-500/[0.08] font-semibold text-orange-200 shadow-xs md:border-transparent md:bg-white/[0.025] md:text-zinc-100 md:font-medium md:shadow-none'
                    : 'border border-white/[0.08] bg-zinc-900/60 text-zinc-300 hover:border-white/[0.14] hover:bg-zinc-800/80 hover:text-white md:border-transparent md:bg-transparent md:text-zinc-400 md:hover:bg-white/[0.03] md:hover:text-zinc-200'
                }`}
              >
                <div className="flex items-center gap-2 min-w-0">
                  {t.icon && (
                    <span
                      className={`shrink-0 transition-colors ${
                        active ? 'text-orange-400' : 'text-zinc-500 group-hover:text-zinc-300'
                      }`}
                    >
                      {t.icon}
                    </span>
                  )}
                  <span className="truncate">{t.label}</span>
                </div>
                {typeof t.count === 'number' && (
                  <span
                    className={`ml-1.5 inline-flex h-4.5 min-w-[19px] items-center justify-center rounded-full px-1.5 font-mono text-[10px] tabular-nums transition-colors shrink-0 ${
                      active
                        ? 'border border-orange-500/30 bg-orange-500/25 text-orange-300 font-semibold md:bg-orange-500/20'
                        : t.count > 0
                        ? 'border border-white/[0.06] bg-white/[0.08] text-zinc-300 group-hover:bg-white/[0.14]'
                        : 'border border-white/[0.04] bg-white/[0.04] text-zinc-500'
                    }`}
                  >
                    {t.count}
                  </span>
                )}
                {active && (
                  <motion.span
                    layoutId="workspaceTabUnderline"
                    transition={{ type: 'spring', stiffness: 420, damping: 32 }}
                    className="hidden md:block absolute inset-x-3 bottom-0 h-[2.5px] rounded-full bg-orange-500 shadow-[0_-2px_10px_rgba(255,107,0,0.45)]"
                  />
                )}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
};
