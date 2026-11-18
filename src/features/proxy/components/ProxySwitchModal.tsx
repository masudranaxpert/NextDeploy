import React, { useEffect, useState } from 'react';
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  Loader2,
  ShieldAlert,
  Undo2,
  WifiOff,
  XCircle,
} from 'lucide-react';
import { ShadcnDialog } from '../../../components/ui/Primitives';
import { ProxyEngineBadge } from '../../../components/proxy/ProxyEngineBadge';
import { formatProxyEngineLabel } from '../../../components/shield/shieldProxyDisplay';
import type { ProxyEngineName } from '../../../lib/api/proxyApi';
import type { useProxyEngine } from '../hooks/useProxyEngine';

type EngineState = ReturnType<typeof useProxyEngine>;

interface ProxySwitchModalProps {
  open: boolean;
  currentEngine: ProxyEngineName;
  targetEngine: ProxyEngineName;
  state: EngineState;
  onRetryPreflight: () => void;
  onConfirm: (experimentalAck: boolean) => void;
  onClose: () => void;
}

const CHECK_LABELS: Record<string, string> = {
  'host-paths': 'Server layout',
  render: 'Generate config for every site',
  validate: 'Syntax check with the engine itself',
};

function formatElapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

export const ProxySwitchModal: React.FC<ProxySwitchModalProps> = ({
  open,
  currentEngine,
  targetEngine,
  state,
  onRetryPreflight,
  onConfirm,
  onClose,
}) => {
  const {
    preflightLoading,
    preflightError,
    preflightResult,
    switchPhase,
    switchError,
    switchStartedAt,
    connectionLost,
    finalSwitch,
    switching,
  } = state;

  const [ackChecked, setAckChecked] = useState(false);
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    if (open) setAckChecked(false);
  }, [open, targetEngine]);

  useEffect(() => {
    if (!switching) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [switching]);

  const targetLabel = formatProxyEngineLabel(targetEngine);
  const currentLabel = formatProxyEngineLabel(currentEngine);
  const hardBlocks = preflightResult?.hard_blocks || [];
  const softWarnings = preflightResult?.soft_warnings || [];
  const checks = preflightResult?.checks || [];
  const failedChecks = checks.filter((c) => !c.ok);
  const needsAck = Boolean(preflightResult?.requires_experimental_ack || targetEngine === 'ferron');

  const stage: 'preflight' | 'progress' | 'done' | 'failed' =
    switchPhase === 'starting' || switchPhase === 'running'
      ? 'progress'
      : switchPhase === 'ok'
      ? 'done'
      : switchPhase === 'failed'
      ? 'failed'
      : 'preflight';

  const preflightPassed =
    !preflightLoading &&
    !preflightError &&
    Boolean(preflightResult) &&
    preflightResult?.can_switch !== false &&
    hardBlocks.length === 0 &&
    failedChecks.length === 0;

  const canExecute = preflightPassed && (!needsAck || ackChecked) && !switching;

  const btnSecondary =
    'min-h-[44px] sm:min-h-0 sm:h-9 px-3.5 rounded-lg border border-white/[0.08] bg-zinc-900 text-xs font-medium text-zinc-200 hover:bg-zinc-800 disabled:opacity-50 cursor-pointer';
  const btnPrimary =
    'inline-flex items-center justify-center gap-1.5 min-h-[44px] sm:min-h-0 sm:h-9 px-4 rounded-lg bg-zinc-100 text-xs font-semibold text-zinc-950 hover:bg-white disabled:opacity-45 disabled:cursor-not-allowed cursor-pointer';

  const footer =
    stage === 'progress' ? (
      <div className="flex items-center justify-between gap-3 text-[11px] text-zinc-400">
        <span>Keep this window open. It will update on its own.</span>
        <span className="font-mono tabular-nums text-zinc-300">
          {switchStartedAt ? formatElapsed(now - switchStartedAt) : '0:00'}
        </span>
      </div>
    ) : stage === 'done' ? (
      <div className="flex justify-end">
        <button type="button" onClick={onClose} className={btnPrimary}>
          Done
        </button>
      </div>
    ) : stage === 'failed' ? (
      <div className="flex flex-col-reverse sm:flex-row sm:justify-end gap-2">
        <button type="button" onClick={onClose} className={btnSecondary}>
          Close
        </button>
        <button type="button" onClick={onRetryPreflight} className={btnPrimary}>
          Run checks again
        </button>
      </div>
    ) : (
      <div className="flex flex-col-reverse sm:flex-row sm:items-center sm:justify-end gap-2">
        <button type="button" onClick={onClose} className={btnSecondary}>
          Cancel
        </button>
        <button type="button" disabled={!canExecute} onClick={() => onConfirm(ackChecked)} className={btnPrimary}>
          <ArrowRight className="h-3.5 w-3.5" />
          Switch to {targetLabel}
        </button>
      </div>
    );

  return (
    <ShadcnDialog
      open={open}
      onClose={() => {
        if (!switching) onClose();
      }}
      title={
        stage === 'done'
          ? `Now running on ${targetLabel}`
          : stage === 'progress'
          ? `Switching to ${targetLabel}…`
          : `Switch proxy to ${targetLabel}`
      }
      description={
        stage === 'preflight'
          ? 'Every domain is checked first; the running proxy is only replaced if all checks pass.'
          : undefined
      }
      maxWidth="xl"
      footer={footer}
    >
      <div className="space-y-4 text-xs">
        <div className="flex flex-wrap items-center gap-2.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-3">
          <ProxyEngineBadge engine={currentEngine} size="sm" />
          <ArrowRight className="h-3.5 w-3.5 text-zinc-500" />
          <ProxyEngineBadge engine={targetEngine} size="sm" />
        </div>

        {stage === 'preflight' && (
          <>
            {preflightLoading && (
              <div className="flex items-center gap-2.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-4 text-zinc-300">
                <Loader2 className="h-4 w-4 animate-spin text-cyan-300 shrink-0" />
                <span>Checking whether every site works on {targetLabel}…</span>
              </div>
            )}

            {preflightError && (
              <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 flex flex-wrap items-center justify-between gap-2 text-rose-200">
                <span className="break-words">{preflightError}</span>
                <button
                  type="button"
                  onClick={onRetryPreflight}
                  className="min-h-[36px] px-2.5 rounded border border-rose-500/30 bg-zinc-950 text-xs font-medium text-rose-200 hover:bg-zinc-900 cursor-pointer"
                >
                  Try again
                </button>
              </div>
            )}

            {!preflightLoading && preflightResult && (
              <>
                {checks.length > 0 && (
                  <div className="rounded-lg border border-white/[0.08] bg-[#09090b] divide-y divide-white/[0.05]">
                    {checks.map((chk) => (
                      <div key={chk.name} className="flex items-start gap-2.5 px-3 py-2.5">
                        {chk.ok ? (
                          <CheckCircle2 className="h-4 w-4 mt-px text-emerald-400 shrink-0" />
                        ) : (
                          <XCircle className="h-4 w-4 mt-px text-rose-400 shrink-0" />
                        )}
                        <div className="min-w-0 flex-1">
                          <div className={chk.ok ? 'text-zinc-200' : 'text-rose-200 font-medium'}>
                            {CHECK_LABELS[chk.name] || chk.name}
                          </div>
                          {chk.detail && (
                            <div
                              className={`mt-0.5 break-words font-mono text-[11px] leading-relaxed ${
                                chk.ok ? 'text-zinc-500' : 'text-rose-200/90'
                              }`}
                            >
                              {chk.detail}
                            </div>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                {hardBlocks.length > 0 && (
                  <div className="rounded-lg border border-rose-500/35 bg-rose-500/10 p-3.5 space-y-2.5 text-rose-200">
                    <div className="flex items-center gap-2 font-semibold">
                      <ShieldAlert className="h-4 w-4 text-rose-300 shrink-0" />
                      <span>
                        {hardBlocks.length === 1 ? 'One feature in use' : `${hardBlocks.length} features in use`} cannot
                        run on {targetLabel}
                      </span>
                    </div>
                    {hardBlocks.map((issue, idx) => (
                      <div key={`${issue.capability}-${idx}`} className="rounded border border-rose-500/20 bg-zinc-950/60 p-2.5 space-y-1">
                        <div className="text-rose-100">{issue.message}</div>
                        {issue.affected_sites && issue.affected_sites.length > 0 && (
                          <div className="font-mono text-[11px] text-rose-200/90">
                            {issue.affected_sites.map((s) => s.label).join(', ')}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                )}

                {softWarnings.length > 0 && (
                  <div className="rounded-lg border border-amber-500/30 bg-amber-500/[0.08] p-3.5 space-y-2 text-amber-100">
                    <div className="flex items-center gap-2 font-semibold text-amber-200">
                      <AlertTriangle className="h-4 w-4 text-amber-300 shrink-0" />
                      <span>Good to know</span>
                    </div>
                    <ul className="space-y-1.5 list-disc pl-5 text-[12px]">
                      {softWarnings.map((warn, idx) => (
                        <li key={`${warn.capability}-${idx}`}>
                          {warn.message}
                          {warn.affected_sites && warn.affected_sites.length > 0 && (
                            <span className="block font-mono text-[11px] text-amber-200/80">
                              {warn.affected_sites.map((s) => s.label).join(', ')}
                            </span>
                          )}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {preflightPassed && (
                  <div className="rounded-lg border border-white/[0.08] bg-[#09090b] p-3.5 space-y-2 text-zinc-300">
                    <div className="font-semibold text-zinc-100">What happens when you switch</div>
                    <ol className="list-decimal pl-5 space-y-1 text-[12px] leading-relaxed">
                      <li>The current docker-compose file is backed up.</li>
                      <li>
                        {currentLabel} stops and {targetLabel} starts on ports 80 and 443. Sites are unreachable for a few
                        seconds, and this page may briefly lose its connection.
                      </li>
                      <li>Running apps are updated for the new engine. Their containers are not rebuilt.</li>
                      <li>
                        If {targetLabel} does not come up, {currentLabel} is restored automatically.
                      </li>
                    </ol>
                  </div>
                )}

                {preflightPassed && needsAck && (
                  <label className="flex items-start gap-2.5 rounded-lg border border-amber-500/35 bg-amber-500/[0.08] p-3.5 cursor-pointer text-amber-100">
                    <input
                      type="checkbox"
                      checked={ackChecked}
                      onChange={(e) => setAckChecked(e.target.checked)}
                      className="mt-0.5 h-4 w-4 shrink-0 rounded border-white/20 bg-zinc-950 accent-amber-400"
                    />
                    <span className="text-[12px] leading-relaxed">
                      I understand {targetLabel} is experimental. It does not close idle HTTP/2 connections and has no
                      write timeout, so a large number of idle visitors can keep memory in use.
                    </span>
                  </label>
                )}
              </>
            )}
          </>
        )}

        {stage === 'progress' && (
          <div className="space-y-3">
            <div className="flex items-center gap-3 rounded-lg border border-cyan-500/25 bg-cyan-500/[0.07] p-4">
              <Loader2 className="h-5 w-5 animate-spin text-cyan-300 shrink-0" />
              <div className="min-w-0">
                <div className="font-semibold text-cyan-100">
                  Replacing {currentLabel} with {targetLabel}
                </div>
                <div className="text-[11px] text-cyan-200/80">
                  This usually takes 20 to 90 seconds, depending on the number of apps.
                </div>
              </div>
            </div>
            {connectionLost && (
              <div className="flex items-start gap-2.5 rounded-lg border border-white/[0.08] bg-zinc-900/70 p-3 text-zinc-300">
                <WifiOff className="h-4 w-4 mt-px text-zinc-400 shrink-0" />
                <span className="text-[12px] leading-relaxed">
                  The panel is not reachable for a moment while the proxy restarts. This is expected. Waiting for it to come
                  back…
                </span>
              </div>
            )}
          </div>
        )}

        {stage === 'done' && (
          <div className="flex items-start gap-3 rounded-lg border border-emerald-500/25 bg-emerald-500/10 p-4 text-emerald-100">
            <CheckCircle2 className="h-5 w-5 text-emerald-300 shrink-0" />
            <div className="min-w-0 space-y-1">
              <div className="font-semibold">All traffic now goes through {targetLabel}.</div>
              {finalSwitch?.message && (
                <div className="break-words text-[11px] text-emerald-200/80">{finalSwitch.message}</div>
              )}
              <div className="text-[11px] text-emerald-200/80">
                You can switch back at any time from this page.
              </div>
            </div>
          </div>
        )}

        {stage === 'failed' && (
          <div className="flex items-start gap-3 rounded-lg border border-rose-500/35 bg-rose-500/10 p-4 text-rose-100">
            {finalSwitch?.rolled_back ? (
              <Undo2 className="h-5 w-5 text-rose-300 shrink-0" />
            ) : (
              <XCircle className="h-5 w-5 text-rose-300 shrink-0" />
            )}
            <div className="min-w-0 space-y-1">
              <div className="font-semibold">
                {finalSwitch?.rolled_back
                  ? `The switch failed, so ${currentLabel} was restored.`
                  : 'The switch did not finish.'}
              </div>
              {switchError && <div className="break-words font-mono text-[11px] text-rose-200/90">{switchError}</div>}
            </div>
          </div>
        )}
      </div>
    </ShadcnDialog>
  );
};
