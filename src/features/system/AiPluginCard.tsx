import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Cpu } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnSwitch } from '../../components/ui/Primitives';
import { backendApi } from '../../lib/backendApi';
import type { AiPluginStatus } from '../../lib/api/aiApi';

const fmtMb = (mb?: number) => (!mb ? '0 MB' : mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb} MB`);

interface AiPluginCardProps {
  /** Called whenever AI becomes usable or stops being usable, so the menu can follow. */
  onStateChange?: (enabled: boolean, installing: boolean) => void;
}

/** Settings card that installs or removes the optional AI assistant. */
export const AiPluginCard: React.FC<AiPluginCardProps> = ({ onStateChange }) => {
  const [st, setSt] = useState<AiPluginStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const pollRef = useRef<number | null>(null);
  const notifyRef = useRef(onStateChange);
  notifyRef.current = onStateChange;

  const apply = useCallback((next: AiPluginStatus) => {
    setSt(next);
    notifyRef.current?.(next.enabled, next.installing);
  }, []);

  const load = useCallback(async () => {
    const res = await backendApi.aiPluginStatus();
    if (res.ok && res.data?.plugin) apply(res.data.plugin);
    return res.data?.plugin;
  }, [apply]);

  useEffect(() => {
    void load();
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    };
  }, [load]);

  // Follow a running download until it ends.
  useEffect(() => {
    if (!st?.installing) {
      if (pollRef.current) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }
    if (pollRef.current) return;
    pollRef.current = window.setInterval(async () => {
      const next = await load();
      if (next && !next.installing) {
        if (next.enabled) toast.success(`AI assistant is ready (omp ${next.version || next.target})`);
        else if (next.error) toast.error(next.error);
      }
    }, 1500);
  }, [st?.installing, load]);

  const enable = async () => {
    setBusy(true);
    const res = await backendApi.aiPluginEnable();
    setBusy(false);
    if (res.ok && res.data?.plugin) apply(res.data.plugin);
    else toast.error(res.error || 'Could not enable the AI assistant');
  };

  const disable = async (purge: boolean) => {
    const size = fmtMb(st?.disk_mb);
    const question = purge
      ? 'Delete the AI assistant and all of its data? This removes the engine, the tools it downloaded, every chat, login and stored API key. This cannot be undone.'
      : `Turn off the AI assistant? The engine and the tools it downloaded (${size}) are removed. Chats, logins and API keys are kept, so turning it on again needs no new setup.`;
    if (!window.confirm(question)) return;
    setBusy(true);
    const res = await backendApi.aiPluginDisable(purge);
    setBusy(false);
    if (res.ok && res.data?.plugin) {
      apply(res.data.plugin);
      toast.success(purge ? 'AI assistant and its data deleted' : 'AI assistant turned off');
    } else {
      toast.error(res.error || 'Could not turn off the AI assistant');
    }
  };

  const on = Boolean(st?.wanted);
  const installing = Boolean(st?.installing);
  const version = st?.version || st?.target || '';

  return (
    <div className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 sm:p-5 space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-orange-500/10 text-orange-400 border border-orange-500/20">
            <Cpu className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <h2 className="text-sm font-semibold text-zinc-100 truncate">AI assistant</h2>
            <p className="text-[11px] text-zinc-500 truncate">Optional. Installed only when you turn it on.</p>
          </div>
        </div>
        <ShadcnSwitch
          checked={on}
          disabled={!st || busy || installing}
          onChange={(next) => void (next ? enable() : disable(false))}
        />
      </div>

      <div className="space-y-2.5 text-xs">
        {installing ? (
          <div className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3 space-y-2">
            <div className="flex items-center justify-between font-mono text-[11px]">
              <span className="text-zinc-400">Downloading omp {st?.target}</span>
              <span className="text-zinc-200">{st?.progress ?? 0}%</span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-800">
              <div
                className="h-full rounded-full bg-orange-400 transition-[width] duration-500"
                style={{ width: `${Math.max(2, st?.progress ?? 0)}%` }}
              />
            </div>
          </div>
        ) : st?.enabled ? (
          <>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono text-[11px]">
              <span className="text-zinc-400">Engine</span>
              <span className="text-zinc-200 font-semibold">
                omp {version}
                {st.source === 'system' ? ' (bundled)' : ''}
              </span>
            </div>
            <div className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-[#09090b] p-3 font-mono text-[11px]">
              <span className="text-zinc-400">Disk used</span>
              <span className="text-zinc-200 font-semibold">{fmtMb(st.disk_mb)}</span>
            </div>
          </>
        ) : (
          <p className="rounded-lg border border-white/[0.06] bg-[#09090b] p-3 text-[11px] leading-relaxed text-zinc-400">
            Turning it on downloads the omp engine {st?.target ? `(${st.target}, about 250 MB)` : ''} for this
            server and verifies its checksum. Turning it off removes the engine and the tools it downloaded.
          </p>
        )}

        {st?.error && !installing && (
          <p className="rounded-lg border border-rose-500/20 bg-rose-500/[0.06] p-3 text-[11px] text-rose-200 break-words">
            {st.error}
          </p>
        )}

        {!installing && st && (
          <div className="flex justify-end">
            <button
              type="button"
              disabled={busy}
              onClick={() => void disable(true)}
              className="text-[11px] text-zinc-500 hover:text-rose-300 disabled:opacity-50 cursor-pointer"
            >
              Delete all AI data
            </button>
          </div>
        )}
      </div>
    </div>
  );
};
