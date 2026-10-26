import React, { useState, useMemo } from 'react';
import {
  Pin,
  PinOff,
  Lock,
  RotateCcw,
  Rocket,
  RefreshCw,
  Play,
  CheckCircle2,
  Loader2,
  ChevronDown,
  ChevronRight,
  Trash2,
  ExternalLink,
  Copy,
  Globe,
  ArrowUpRight,
  Terminal as TerminalIcon,
} from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, DeploymentRecord } from '../../../types/nextdeploy';
import { backendApi } from '../../../lib/backendApi';
import { copyToClipboard } from '../../../lib/clipboard';
import { ActionMenu, StatusBadge } from '../../../components/ui/Primitives';

interface ProjectDeploymentsTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
  hasGitConnection: boolean;
  liveDeployState: {
    running: boolean;
    output: string;
    action?: string;
  };
  isBuildLogExpanded: boolean | null;
  setIsBuildLogExpanded: (expanded: boolean) => void;
  liveDeployLogRef: React.RefObject<HTMLDivElement | null>;
  renderStyledBuildLog: (logText: string) => React.ReactNode;
  triggerDeploy: (options?: { noCache?: boolean; rollbackFrom?: DeploymentRecord }) => Promise<void>;
  setBuildLogsDeployment: (dep: DeploymentRecord | null) => void;
  isPinnedProduction: boolean;
  isPinningSlot: boolean;
  handleTogglePinSlot: () => Promise<void>;
  isPromotingSlot: boolean;
  handlePromotePreviewSlot: () => Promise<void>;
}

export const ProjectDeploymentsTab: React.FC<ProjectDeploymentsTabProps> = ({
  app,
  onUpdateApp,
  hasGitConnection,
  liveDeployState,
  isBuildLogExpanded,
  setIsBuildLogExpanded,
  liveDeployLogRef,
  renderStyledBuildLog,
  triggerDeploy,
  setBuildLogsDeployment,
  isPinnedProduction,
  isPinningSlot,
  handleTogglePinSlot,
  isPromotingSlot,
  handlePromotePreviewSlot,
}) => {
  const [deployEnvFilter, setDeployEnvFilter] = useState<'all' | 'production' | 'rollback'>('all');

  const activeSlotLower = (app.activeSlot || 'blue').toLowerCase() as 'blue' | 'green';
  const standbySlotName = (activeSlotLower === 'green' ? 'blue' : 'green') as 'blue' | 'green';
  const standbySlotHasImage = Boolean(app.slotImages?.[standbySlotName]);
  const isStandbyContainerRunning = Boolean(
    app.services?.some(
      (s) =>
        s.state === 'running' &&
        (s.name?.toLowerCase().includes(`-${standbySlotName}`) ||
          s.service?.toLowerCase().includes(`-${standbySlotName}`))
    )
  );

  const activeProductionDeployment = useMemo(() => {
    return (
      app.deployments?.find(
        (d) =>
          d.slot?.toLowerCase() === activeSlotLower &&
          (d.status === 'ready' || d.status === 'running')
      ) ||
      app.deployments?.[0] ||
      null
    );
  }, [app.deployments, activeSlotLower]);

  const latestStandbyDeployment = useMemo(() => {
    return (
      app.deployments?.find(
        (d) => d.slot?.toLowerCase() === standbySlotName && d.status === 'ready'
      ) || null
    );
  }, [app.deployments, standbySlotName]);

  const productionUrl = useMemo(() => {
    const custom = app.domains?.find((d) => d.domain && !d.domain.includes('preview-'));
    if (custom) {
      return `${custom.forceHttps !== false ? 'https' : 'http'}://${custom.domain}`;
    }
    return activeProductionDeployment?.previewUrl || '';
  }, [app.domains, activeProductionDeployment]);

  const previewUrl = useMemo(() => {
    const previewDomain = app.domains?.find((d) => d.domain && d.domain.startsWith('preview-'));
    if (previewDomain) {
      return `${previewDomain.forceHttps !== false ? 'https' : 'http'}://${previewDomain.domain}`;
    }
    return latestStandbyDeployment?.previewUrl || '';
  }, [app.domains, latestStandbyDeployment]);

  const previewHost = previewUrl ? previewUrl.replace(/^https?:\/\//, '') : '';

  const enrichedDeployments = useMemo(() => {
    const currentProdId = activeProductionDeployment?.id;
    const latestStandbyId = latestStandbyDeployment?.id;

    return (app.deployments || [])
      .filter((dep) => {
        if (deployEnvFilter === 'all') return true;
        if (deployEnvFilter === 'production') {
          return dep.slot?.toLowerCase() === activeSlotLower;
        }
        if (deployEnvFilter === 'rollback') {
          return dep.envType === 'rollback';
        }
        return true;
      })
      .map((dep) => {
        const isCurrentProd = dep.id === currentProdId;
        const isLatestStandby = dep.id === latestStandbyId;
        const canInstantRollback =
          Boolean(standbySlotHasImage) &&
          !isCurrentProd &&
          (dep.slot?.toLowerCase() === standbySlotName || isLatestStandby);

        return {
          ...dep,
          isCurrentProd,
          isLatestStandby,
          canInstantRollback,
        };
      });
  }, [
    app.deployments,
    deployEnvFilter,
    activeSlotLower,
    standbySlotName,
    activeProductionDeployment,
    latestStandbyDeployment,
    standbySlotHasImage,
  ]);

  return (
    <div className="space-y-3">
      {/* Unified Slot & Production Control Bar */}
      <div className="flex flex-col gap-3 rounded-xl border border-white/[0.08] bg-[#111115]/90 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-start gap-3 min-w-0">
          <div
            className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border ${
              isPinnedProduction
                ? 'border-amber-500/30 bg-amber-500/10 text-amber-400'
                : 'border-emerald-500/25 bg-emerald-500/10 text-emerald-400'
            }`}
          >
            {isPinnedProduction ? (
              <Pin className="h-3.5 w-3.5" />
            ) : (
              <CheckCircle2 className="h-3.5 w-3.5" />
            )}
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase text-emerald-400 border border-emerald-500/25">
                PROD · {activeSlotLower.toUpperCase()}
              </span>
              {activeProductionDeployment && (
                <span className="font-mono text-xs font-semibold text-zinc-100">
                  {activeProductionDeployment.id}
                </span>
              )}
              {isPinnedProduction && (
                <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10px] font-medium text-amber-300 border border-amber-500/25">
                  <Lock className="h-2.5 w-2.5" />
                  Pinned
                </span>
              )}
              {productionUrl && (
                <a
                  href={productionUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 font-mono text-[11px] text-zinc-300 hover:text-emerald-400 transition-colors truncate"
                >
                  <Globe className="h-3 w-3 text-emerald-400 shrink-0" />
                  <span className="truncate">{productionUrl.replace('https://', '')}</span>
                  <ArrowUpRight className="h-3 w-3 shrink-0 opacity-60" />
                </a>
              )}
            </div>

            <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-zinc-400">
              {isPinnedProduction && isStandbyContainerRunning ? (
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="rounded bg-violet-500/15 px-1.5 py-0.5 font-mono text-[9px] font-semibold uppercase text-violet-300 border border-violet-500/25">
                    PREVIEW · {standbySlotName.toUpperCase()}
                  </span>
                  {latestStandbyDeployment && (
                    <span className="font-mono text-[11px] text-zinc-300">
                      {latestStandbyDeployment.id}
                    </span>
                  )}
                  {previewUrl && (
                    <a
                      href={previewUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 font-mono text-[11px] text-violet-300 hover:text-violet-200 underline"
                    >
                      {previewHost}
                      <ArrowUpRight className="h-3 w-3" />
                    </a>
                  )}
                </div>
              ) : isPinnedProduction ? (
                <span>
                  Standby · <span className="font-mono text-violet-300">Slot {standbySlotName.toUpperCase()}</span>
                  {previewHost && <span className="text-zinc-400"> · {previewHost}</span>}
                </span>
              ) : (
                <span>
                  Standby · <span className="font-mono text-zinc-300">Slot {standbySlotName.toUpperCase()}</span>
                  {standbySlotHasImage && latestStandbyDeployment ? (
                    <span className="text-emerald-400/90 font-mono">
                      {' '}· {latestStandbyDeployment.id}
                    </span>
                  ) : (
                    <span className="text-zinc-500"> · Idle</span>
                  )}
                </span>
              )}
            </div>
          </div>
        </div>

        <div className="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:flex-wrap sm:items-center sm:gap-1.5 sm:shrink-0">
          {activeProductionDeployment && (
            <button
              type="button"
              onClick={() => setBuildLogsDeployment(activeProductionDeployment)}
              className="inline-flex h-7 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer transition-colors"
            >
              <TerminalIcon className="h-3.5 w-3.5 text-zinc-400" />
              <span>Logs</span>
            </button>
          )}

          <button
            type="button"
            disabled={isPinningSlot}
            onClick={handleTogglePinSlot}
            className={`inline-flex h-7 items-center justify-center gap-1.5 rounded-lg border px-2.5 text-xs font-medium transition-colors cursor-pointer ${
              isPinnedProduction
                ? 'border-amber-500/35 bg-amber-500/15 text-amber-200 hover:bg-amber-500/25'
                : 'border-white/[0.08] bg-zinc-900 text-zinc-300 hover:bg-zinc-800 hover:text-white'
            }`}
            title={
              isPinnedProduction
                ? 'Unpin production to resume automatic zero-downtime cutover'
                : 'Pin production so new builds deploy to the Preview Slot first'
            }
          >
            {isPinnedProduction ? (
              <PinOff className="h-3.5 w-3.5 text-amber-400" />
            ) : (
              <Pin className="h-3.5 w-3.5 text-zinc-400" />
            )}
            <span>{isPinnedProduction ? 'Unpin' : 'Pin Prod'}</span>
          </button>

          {(isStandbyContainerRunning || standbySlotHasImage) && (
            <button
              type="button"
              disabled={isPromotingSlot || liveDeployState.running}
              onClick={handlePromotePreviewSlot}
              className="inline-flex h-7 items-center justify-center gap-1.5 rounded-lg bg-emerald-500 px-2.5 text-xs font-semibold text-zinc-950 hover:bg-emerald-400 transition-colors cursor-pointer disabled:opacity-50"
              title={`Promote Standby Slot ${standbySlotName.toUpperCase()} directly to live production traffic`}
            >
              <Rocket className="h-3.5 w-3.5" />
              <span>
                {isPromotingSlot
                  ? 'Promoting...'
                  : `Promote Slot ${standbySlotName.toUpperCase()} to Prod`}
              </span>
            </button>
          )}

          {!isPinnedProduction && standbySlotHasImage && latestStandbyDeployment && (
            <button
              type="button"
              disabled={liveDeployState.running}
              onClick={() => triggerDeploy({ rollbackFrom: latestStandbyDeployment })}
              className="inline-flex h-7 items-center justify-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 text-xs font-medium text-amber-300 hover:bg-amber-500/20 transition-colors cursor-pointer disabled:opacity-50"
              title={`Switch production back to Slot ${standbySlotName.toUpperCase()} (${latestStandbyDeployment.id})`}
            >
              <RotateCcw className="h-3.5 w-3.5" />
              <span>Rollback to {standbySlotName.toUpperCase()}</span>
            </button>
          )}
        </div>
      </div>

      {/* Release History Header & Toolbar */}
      <div className="rounded-2xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold text-zinc-100">Release History</h2>
            <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {enrichedDeployments.length}
            </span>
          </div>
          <button
            type="button"
            onClick={async () => {
              const res = await backendApi.clearDeployLogs(app.id);
              if (!res.ok) {
                toast.error(res.error || 'Failed to clear deployment history');
                return;
              }
              onUpdateApp({
                ...app,
                deployments: [],
              });
              toast.success('Cleared deployment history');
            }}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-xs font-medium text-zinc-300 hover:bg-zinc-800 cursor-pointer"
          >
            <Trash2 className="h-3.5 w-3.5 text-zinc-400" />
            Clear History
          </button>
        </div>

        {/* Filter Pills & Action Buttons */}
        <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
          <div className="grid grid-cols-3 gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-1 sm:flex sm:items-center sm:gap-0.5 sm:p-0.5">
            {(
              [
                { id: 'all', label: 'All' },
                { id: 'production', label: 'Production' },
                { id: 'rollback', label: 'Rollbacks' },
              ] as const
            ).map((f) => (
              <button
                key={f.id}
                type="button"
                onClick={() => setDeployEnvFilter(f.id)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors text-center cursor-pointer ${
                  deployEnvFilter === f.id
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>

          <div className="grid grid-cols-2 gap-2 sm:flex sm:items-center sm:gap-1.5">
            <button
              type="button"
              disabled={liveDeployState.running}
              onClick={() => triggerDeploy()}
              className="inline-flex h-9 sm:h-7 items-center justify-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
            >
              {liveDeployState.running ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Play className="h-3.5 w-3.5 fill-current" />
              )}
              {liveDeployState.running ? 'Deploying...' : 'Deploy Now'}
            </button>
            <button
              type="button"
              disabled={liveDeployState.running}
              onClick={() => triggerDeploy({ noCache: true })}
              className="inline-flex h-9 sm:h-7 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
            >
              <RefreshCw className={`h-3.5 w-3.5 text-orange-400 ${liveDeployState.running ? 'animate-spin' : ''}`} />
              Rebuild (No Cache)
            </button>
          </div>
        </div>

        {/* Live Deployment Streaming Console Drawer */}
        {(liveDeployState.running || liveDeployState.output) && (() => {
          const isVisible = isBuildLogExpanded ?? liveDeployState.running;
          return (
            <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-[#09090b]">
              <div
                className="flex items-center justify-between bg-zinc-900/70 px-3.5 py-2 cursor-pointer select-none"
                onClick={() => setIsBuildLogExpanded(!isVisible)}
              >
                <div className="flex items-center gap-2">
                  {liveDeployState.running ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin text-orange-400" />
                  ) : (
                    <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
                  )}
                  <span className="font-mono text-xs font-medium text-zinc-200">
                    {liveDeployState.running
                      ? `Building · ${liveDeployState.action || 'Deploy'}`
                      : `Build output · ${liveDeployState.action || 'Completed'}`}
                  </span>
                </div>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    setIsBuildLogExpanded(!isVisible);
                  }}
                  className="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] font-medium text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200 cursor-pointer"
                >
                  <span>{isVisible ? 'Hide' : 'Show'}</span>
                  {isVisible ? (
                    <ChevronDown className="h-3.5 w-3.5" />
                  ) : (
                    <ChevronRight className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              {isVisible && (
                <div
                  ref={liveDeployLogRef}
                  className="max-h-64 overflow-y-auto border-t border-white/[0.06] p-3 font-mono text-[11px] scroll-smooth"
                >
                  {renderStyledBuildLog(liveDeployState.output || 'Waiting for output...')}
                </div>
              )}
            </div>
          );
        })()}

        {/* Mobile Deployments Card List */}
        <div className="block md:hidden divide-y divide-white/[0.06] rounded-xl border border-white/[0.07] bg-[#09090b]/40 overflow-hidden">
          {enrichedDeployments.length === 0 ? (
            <div className="p-6 text-center">
              <p className="text-xs font-medium text-zinc-300">No deployments recorded yet</p>
              <p className="mt-1 text-[11px] text-zinc-500">
                Click Deploy to build and launch your project into an active slot.
              </p>
            </div>
          ) : (
            enrichedDeployments.map((dep) => {
              const isCurrentProd = dep.isCurrentProd;
              const canRollbackThisRow = dep.canInstantRollback;
              const isPreviewLive =
                dep.isLatestStandby && isStandbyContainerRunning && dep.status === 'ready';
              const isRowLive =
                ((isCurrentProd && dep.status === 'ready') || isPreviewLive) &&
                Boolean(dep.previewUrl);
              const hasValidGitSha =
                hasGitConnection &&
                dep.commitSha &&
                dep.commitSha !== 'latest' &&
                dep.commitSha !== 'main';

              const formattedTime = (() => {
                if (!dep.startedAt) return '—';
                const parsed = new Date(dep.startedAt);
                if (Number.isNaN(parsed.getTime())) {
                  return dep.startedAt.replace('T', ' ').replace('Z', '');
                }
                return (
                  parsed.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) +
                  ', ' +
                  parsed.toLocaleTimeString('en-US', {
                    hour: '2-digit',
                    minute: '2-digit',
                    hour12: false,
                  })
                );
              })();

              const statusDotColor =
                dep.status === 'ready'
                  ? 'bg-emerald-400'
                  : dep.status === 'building' || dep.status === 'running'
                  ? 'bg-amber-400'
                  : dep.status === 'failed' || dep.status === 'error'
                  ? 'bg-rose-400'
                  : 'bg-zinc-500';

              return (
                <div key={dep.id} className="p-3.5 space-y-2.5">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className={`h-2 w-2 rounded-full shrink-0 ${statusDotColor}`} />
                      <span className="font-mono text-xs font-semibold text-zinc-100 shrink-0">
                        {dep.id}
                      </span>
                      {isCurrentProd ? (
                        <span className="inline-flex items-center gap-1 rounded border border-emerald-500/30 bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-emerald-400 shrink-0">
                          {dep.envType === 'rollback' && <RotateCcw className="h-2.5 w-2.5" />}
                          PROD · {dep.slot?.toUpperCase()}
                        </span>
                      ) : isPreviewLive || (dep.isLatestStandby && isPinnedProduction && standbySlotHasImage) ? (
                        <span className="inline-flex items-center gap-1 rounded border border-violet-500/30 bg-violet-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-violet-300 shrink-0">
                          Slot {dep.slot?.toUpperCase()}
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded border border-white/[0.08] bg-zinc-800/60 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 shrink-0">
                          Slot {dep.slot ? dep.slot.toUpperCase() : 'BLUE'}
                        </span>
                      )}
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0">
                      <span className="font-mono text-[11px] text-zinc-400">
                        {formattedTime}
                      </span>
                      <ActionMenu
                        items={[
                          {
                            label: 'View Build Logs',
                            icon: <TerminalIcon className="h-3.5 w-3.5" />,
                            onClick: () => setBuildLogsDeployment(dep),
                          },
                          ...((dep.isLatestStandby || dep.slot?.toLowerCase() === standbySlotName) && !isCurrentProd
                            ? [
                                {
                                  label: `Promote Slot ${dep.slot?.toUpperCase()} to Prod`,
                                  icon: <Rocket className="h-3.5 w-3.5" />,
                                  onClick: () => handlePromotePreviewSlot(),
                                },
                              ]
                            : []),
                          ...(canRollbackThisRow && !isPinnedProduction
                            ? [
                                {
                                  label: `Rollback to Slot ${dep.slot?.toUpperCase()}`,
                                  icon: <RotateCcw className="h-3.5 w-3.5" />,
                                  onClick: () => triggerDeploy({ rollbackFrom: dep }),
                                },
                              ]
                            : []),
                          ...(hasValidGitSha && !isCurrentProd
                            ? [
                                {
                                  label: `Redeploy Commit (${dep.commitSha.slice(0, 7)})`,
                                  icon: <RefreshCw className="h-3.5 w-3.5" />,
                                  onClick: () => triggerDeploy({ rollbackFrom: dep }),
                                },
                              ]
                            : []),
                          ...(isRowLive
                            ? [
                                {
                                  label: 'Open Live URL',
                                  icon: <ExternalLink className="h-3.5 w-3.5" />,
                                  onClick: () => window.open(dep.previewUrl, '_blank'),
                                },
                              ]
                            : []),
                          ...(hasValidGitSha
                            ? [
                                {
                                  label: 'Copy Commit SHA',
                                  icon: <Copy className="h-3.5 w-3.5" />,
                                  onClick: () => {
                                    void copyToClipboard(dep.commitSha);
                                    toast.success(`Copied SHA ${dep.commitSha.slice(0, 7)}`);
                                  },
                                },
                              ]
                            : []),
                          {
                            label: 'Delete Build Log',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger',
                            onClick: async () => {
                              const rawLogId = String(dep.id).replace(/^dep-/, '');
                              const res = await backendApi.deployLogDelete(app.id, rawLogId);
                              if (!res.ok) {
                                toast.error(res.error || 'Failed to delete build log');
                                return;
                              }
                              onUpdateApp({
                                ...app,
                                deployments: app.deployments.filter((d) => d.id !== dep.id),
                              });
                              toast.success(`Deleted build log ${dep.id}`);
                            },
                          },
                        ]}
                      />
                    </div>
                  </div>

                  <div className="flex items-center justify-between gap-2 text-xs">
                    <span className="font-mono text-[11px] text-zinc-400 truncate">
                      {dep.status === 'ready' ? 'Ready' : dep.status} · {hasGitConnection ? `${dep.branch}@${dep.commitSha.slice(0, 7)}` : 'Workspace'} · {dep.durationSec}s
                    </span>
                    {isRowLive ? (
                      <a
                        href={dep.previewUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 font-mono text-[11px] text-violet-400 hover:text-violet-300 transition-colors shrink-0"
                      >
                        <span>Live URL</span>
                        <ArrowUpRight className="h-3 w-3" />
                      </a>
                    ) : (
                      <span className="font-mono text-[11px] text-zinc-500 shrink-0">
                        History
                      </span>
                    )}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Desktop Deployments Table */}
        <div className="hidden md:block overflow-x-auto rounded-xl border border-white/[0.07] bg-[#09090b]/40">
          <table className="w-full min-w-[760px] table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#09090b]/80 text-[11px] font-medium uppercase tracking-wider text-zinc-400">
                <th className="w-[34%] px-3.5 py-2.5">Release</th>
                <th className="w-[22%] px-3.5 py-2.5">Slot & State</th>
                <th className="w-[24%] px-3.5 py-2.5">Endpoint</th>
                <th className="w-[12%] px-3.5 py-2.5 text-right">Time</th>
                <th className="w-[8%] px-3 py-2.5 text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {enrichedDeployments.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-10 text-center">
                    <p className="text-xs font-medium text-zinc-300">No deployments recorded yet</p>
                    <p className="mt-1 text-[11px] text-zinc-500">
                      Click Deploy to build and launch your project into an active slot.
                    </p>
                  </td>
                </tr>
              ) : (
                enrichedDeployments.map((dep) => {
                  const isCurrentProd = dep.isCurrentProd;
                  const canRollbackThisRow = dep.canInstantRollback;
                  const isPreviewLive =
                    dep.isLatestStandby && isStandbyContainerRunning && dep.status === 'ready';
                  const isRowLive =
                    ((isCurrentProd && dep.status === 'ready') || isPreviewLive) &&
                    Boolean(dep.previewUrl);
                  const hasValidGitSha =
                    hasGitConnection &&
                    dep.commitSha &&
                    dep.commitSha !== 'latest' &&
                    dep.commitSha !== 'main';

                  const formattedTime = (() => {
                    if (!dep.startedAt) return '—';
                    const parsed = new Date(dep.startedAt);
                    if (Number.isNaN(parsed.getTime())) {
                      return dep.startedAt.replace('T', ' ').replace('Z', '');
                    }
                    return (
                      parsed.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) +
                      ', ' +
                      parsed.toLocaleTimeString('en-US', {
                        hour: '2-digit',
                        minute: '2-digit',
                        hour12: false,
                      })
                    );
                  })();

                  return (
                    <tr
                      key={dep.id}
                      className="hover:bg-white/[0.02] transition-colors"
                    >
                      {/* Column 1: Release + Source */}
                      <td className="px-3.5 py-2.5 min-w-0">
                        <div className="flex items-center gap-1.5 min-w-0">
                          <StatusBadge status={dep.status} />
                          <span className="font-mono text-xs font-semibold text-zinc-100">
                            {dep.id}
                          </span>
                          {hasGitConnection ? (
                            <span className="rounded bg-white/[0.05] px-1.5 py-0.5 font-mono text-[10px] text-orange-400 shrink-0">
                              {dep.branch}@{dep.commitSha.slice(0, 7)}
                            </span>
                          ) : (
                            <span className="rounded bg-white/[0.05] px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 shrink-0">
                              Workspace
                            </span>
                          )}
                        </div>
                        <div className="mt-0.5 truncate text-[11px] text-zinc-400">
                          {dep.commitMessage}
                        </div>
                      </td>

                      {/* Column 2: Slot & State */}
                      <td className="px-3.5 py-2.5 min-w-0">
                        {isCurrentProd ? (
                          <div className="inline-flex items-center gap-1 rounded border border-emerald-500/30 bg-emerald-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-emerald-400">
                            {dep.envType === 'rollback' && (
                              <RotateCcw className="h-2.5 w-2.5 text-emerald-300" />
                            )}
                            <span>PROD · {dep.slot?.toUpperCase()}</span>
                            {isPinnedProduction && (
                              <Pin className="h-2.5 w-2.5 text-amber-300 ml-0.5" />
                            )}
                          </div>
                        ) : isPreviewLive || (dep.isLatestStandby && isPinnedProduction && standbySlotHasImage) ? (
                          <span className="inline-flex items-center gap-1 rounded border border-violet-500/30 bg-violet-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-violet-300">
                            PREVIEW · {dep.slot?.toUpperCase()}
                          </span>
                        ) : canRollbackThisRow ? (
                          <span className="inline-flex items-center gap-1 rounded border border-sky-500/25 bg-sky-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-sky-300">
                            STANDBY · {dep.slot?.toUpperCase()}
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 font-mono text-[10px] text-zinc-500">
                            {dep.envType === 'rollback' && (
                              <RotateCcw className="h-2.5 w-2.5 text-zinc-500" />
                            )}
                            <span>History {dep.slot && `· ${dep.slot.toUpperCase()}`}</span>
                          </span>
                        )}
                      </td>

                      {/* Column 3: Endpoint */}
                      <td className="px-3.5 py-2.5 min-w-0">
                        {isRowLive ? (
                          <a
                            href={dep.previewUrl}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center gap-1 truncate font-mono text-[11px] text-zinc-300 hover:text-orange-400 transition-colors max-w-full"
                            title={dep.previewUrl}
                          >
                            <Globe
                              className={`h-3 w-3 shrink-0 ${
                                isCurrentProd ? 'text-emerald-400' : 'text-violet-400'
                              }`}
                            />
                            <span className="truncate">
                              {dep.previewUrl.replace('https://', '')}
                            </span>
                            <ArrowUpRight className="h-3 w-3 shrink-0 opacity-60" />
                          </a>
                        ) : (
                          <span className="font-mono text-[11px] text-zinc-600">—</span>
                        )}
                      </td>

                      {/* Column 4: Time & Duration */}
                      <td className="px-3.5 py-2.5 text-right font-mono tabular-nums text-[11px] text-zinc-300 whitespace-nowrap">
                        <div>{dep.durationSec}s</div>
                        <div className="text-[10px] text-zinc-500">{formattedTime}</div>
                      </td>

                      {/* Column 5: Actions */}
                      <td className="px-3 py-2.5 text-right whitespace-nowrap">
                        <ActionMenu
                          items={[
                            {
                              label: 'View Build Logs',
                              icon: <TerminalIcon className="h-3.5 w-3.5" />,
                              onClick: () => setBuildLogsDeployment(dep),
                            },
                            ...((dep.isLatestStandby || dep.slot?.toLowerCase() === standbySlotName) && !isCurrentProd
                              ? [
                                  {
                                    label: `Promote Slot ${dep.slot?.toUpperCase()} to Prod`,
                                    icon: <Rocket className="h-3.5 w-3.5" />,
                                    onClick: () => handlePromotePreviewSlot(),
                                  },
                                ]
                              : []),
                            ...(canRollbackThisRow && !isPinnedProduction
                              ? [
                                  {
                                    label: `Rollback to Slot ${dep.slot?.toUpperCase()}`,
                                    icon: <RotateCcw className="h-3.5 w-3.5" />,
                                    onClick: () => triggerDeploy({ rollbackFrom: dep }),
                                  },
                                ]
                              : []),
                            ...(hasValidGitSha && !isCurrentProd
                              ? [
                                  {
                                    label: `Redeploy Commit (${dep.commitSha.slice(0, 7)})`,
                                    icon: <RefreshCw className="h-3.5 w-3.5" />,
                                    onClick: () => triggerDeploy({ rollbackFrom: dep }),
                                  },
                                ]
                              : []),
                            ...(isRowLive
                              ? [
                                  {
                                    label: 'Open Live URL',
                                    icon: <ExternalLink className="h-3.5 w-3.5" />,
                                    onClick: () => window.open(dep.previewUrl, '_blank'),
                                  },
                                ]
                              : []),
                            ...(hasValidGitSha
                              ? [
                                  {
                                    label: 'Copy Commit SHA',
                                    icon: <Copy className="h-3.5 w-3.5" />,
                                    onClick: () => {
                                      void copyToClipboard(dep.commitSha);
                                      toast.success(`Copied SHA ${dep.commitSha.slice(0, 7)}`);
                                    },
                                  },
                                ]
                              : []),
                            {
                              label: 'Delete Build Log',
                              icon: <Trash2 className="h-3.5 w-3.5" />,
                              variant: 'danger',
                              onClick: async () => {
                                const rawLogId = String(dep.id).replace(/^dep-/, '');
                                const res = await backendApi.deployLogDelete(app.id, rawLogId);
                                if (!res.ok) {
                                  toast.error(res.error || 'Failed to delete build log');
                                  return;
                                }
                                onUpdateApp({
                                  ...app,
                                  deployments: app.deployments.filter((d) => d.id !== dep.id),
                                });
                                toast.success(`Deleted build log ${dep.id}`);
                              },
                            },
                          ]}
                        />
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
