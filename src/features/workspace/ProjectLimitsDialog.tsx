import React, { useCallback, useContext, useEffect, useState } from 'react';
import { Sliders } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../components/ui/Primitives';
import { OwnerContext } from '../../components/ui/OwnerBadge';
import { backendApi } from '../../lib/backendApi';
import type { AppLimitsInfo } from '../../lib/api/appsApi';
import { AppStack } from '../../types/nextdeploy';
import { Field, GhostButton, PrimaryButton, inputCls } from '../php/ui';

const MEM_PRESETS = [256, 512, 1024, 2048, 4096];
const CPU_PRESETS = [0.5, 1, 2, 4];

const fmtMem = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(mb % 1024 === 0 ? 0 : 1)} GB` : `${mb} MB`);

/** The project's own limits, loaded from the server (null while loading or for PHP sites). */
export function useAppLimits(app: AppStack, active: boolean, reloadKey = 0) {
  const [info, setInfo] = useState<AppLimitsInfo | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!active || app.php) return;
    let cancelled = false;
    void backendApi.appLimitsGet(app.id).then((r) => {
      if (cancelled) return;
      if (r.ok && r.data) {
        setInfo(r.data);
        setError('');
      } else {
        setError(r.error || 'Could not load the limits');
      }
    });
    return () => {
      cancelled = true;
    };
  }, [app.id, app.php, active, reloadKey]);
  return { info, error };
}

/** Shows and changes one project's memory and CPU ceiling, inside what the owner's account allows. */
export const ProjectLimitsDialog: React.FC<{
  app: AppStack;
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
}> = ({ app, open, onClose, onSaved }) => {
  const { viewerId, isAdmin } = useContext(OwnerContext);
  const canEdit = isAdmin || app.ownerId === viewerId;
  const { info, error } = useAppLimits(app, open);
  const [mem, setMem] = useState('');
  const [cpu, setCpu] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (info) {
      setMem(info.limits.memory_mb > 0 ? String(info.limits.memory_mb) : '');
      setCpu(info.limits.cpus > 0 ? String(info.limits.cpus) : '');
      setSaved(false);
    }
  }, [info]);

  const capMem = info?.cap.memory_mb ?? 0;
  const capCpu = info?.cap.cpus ?? 0;
  const memNum = Math.round(Number(mem) || 0);
  const cpuNum = Number(cpu) || 0;
  const problem =
    memNum < 0 || cpuNum < 0
      ? 'Limits cannot be negative'
      : memNum > 0 && memNum < 64
        ? 'Memory must be at least 64 MB'
        : cpuNum > 0 && cpuNum < 0.1
          ? 'CPU must be at least 0.1 cores'
          : capMem > 0 && memNum > capMem
            ? `Memory is more than ${info?.cap.source === 'account' ? 'your account allows' : 'this server has'} (${fmtMem(capMem)})`
            : capCpu > 0 && cpuNum > capCpu
              ? `CPU is more than ${info?.cap.source === 'account' ? 'your account allows' : 'this server has'} (${capCpu} cores)`
              : '';

  const save = useCallback(async () => {
    setSaving(true);
    const r = await backendApi.appLimitsSet(app.id, memNum, cpuNum);
    setSaving(false);
    if (!r.ok) {
      toast.error(r.error || 'Could not save the limits');
      return;
    }
    toast.success('Limits saved');
    setSaved(true);
    onSaved();
  }, [app.id, memNum, cpuNum, onSaved]);

  const redeploy = async () => {
    const r = await backendApi.composeAction(app.id, 'up');
    if (!r.ok) {
      toast.error(r.error || 'Could not start the redeploy');
      return;
    }
    toast.success('Applying the limits: containers are being recreated');
    onClose();
  };

  const whose = info?.cap.source === 'account' ? 'your account allows' : 'this server has';
  const memPresets = MEM_PRESETS.filter((m) => !capMem || m <= capMem);
  const cpuPresets = CPU_PRESETS.filter((c) => !capCpu || c <= capCpu);

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Resource limits"
      icon={<Sliders className="h-4 w-4" />}
      footer={
        <>
          <GhostButton onClick={onClose}>Close</GhostButton>
          {canEdit && !app.php && (
            <PrimaryButton busy={saving} disabled={!info || problem !== ''} onClick={() => void save()}>
              Save limits
            </PrimaryButton>
          )}
        </>
      }
    >
      <div className="space-y-4 text-xs">
        {app.php ? (
          <p className="text-zinc-400">PHP sites are limited through their PHP settings (memory limit and workers), inside your account limits.</p>
        ) : error ? (
          <p className="text-rose-300">{error}</p>
        ) : !info ? (
          <p className="text-zinc-500">Loading…</p>
        ) : (
          <>
            <p className="leading-5 text-zinc-400">
              Cap what <strong className="text-zinc-200">{app.name}</strong> can use. Leave a field empty for no limit of its own: the
              project then shares everything {whose}. A limit can never be higher than that.
            </p>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Memory (MB)" hint={capMem ? `up to ${fmtMem(capMem)}` : undefined}>
                <input
                  type="number"
                  min={0}
                  max={capMem || undefined}
                  step={64}
                  disabled={!canEdit}
                  className={inputCls}
                  value={mem}
                  placeholder="No limit"
                  onChange={(e) => setMem(e.target.value)}
                />
              </Field>
              <Field label="CPU (cores)" hint={capCpu ? `up to ${capCpu} cores` : undefined}>
                <input
                  type="number"
                  min={0}
                  max={capCpu || undefined}
                  step={0.25}
                  disabled={!canEdit}
                  className={inputCls}
                  value={cpu}
                  placeholder="No limit"
                  onChange={(e) => setCpu(e.target.value)}
                />
              </Field>
            </div>
            {canEdit && (
              <div className="flex flex-wrap gap-1.5">
                {memPresets.map((m) => (
                  <button key={`m${m}`} type="button" onClick={() => setMem(String(m))} className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-1 font-mono text-[11px] text-zinc-300 hover:bg-zinc-800 cursor-pointer">
                    {fmtMem(m)}
                  </button>
                ))}
                {cpuPresets.map((c) => (
                  <button key={`c${c}`} type="button" onClick={() => setCpu(String(c))} className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-1 font-mono text-[11px] text-zinc-300 hover:bg-zinc-800 cursor-pointer">
                    {c} {c === 1 ? 'core' : 'cores'}
                  </button>
                ))}
                <button type="button" onClick={() => { setMem(''); setCpu(''); }} className="rounded-md border border-white/[0.08] bg-zinc-900 px-2 py-1 text-[11px] text-zinc-400 hover:bg-zinc-800 cursor-pointer">
                  No limit
                </button>
              </div>
            )}
            {problem && <p className="text-[11px] text-rose-300">{problem}</p>}
            <p className="font-mono text-[11px] text-zinc-500">
              Using now: {info.usage.memory_mb} MB · {info.usage.cpu_cores.toFixed(2)} cores
            </p>
            {!info.enforced && (
              <p className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-300">
                This server cannot enforce group limits, so the numbers are saved but not applied.
              </p>
            )}
            {!canEdit && <p className="text-[11px] text-zinc-500">Only the project owner or an administrator can change these limits.</p>}
            {saved && (
              <div className="flex items-center justify-between gap-3 rounded-lg border border-emerald-500/25 bg-emerald-500/10 px-3 py-2.5">
                <span className="text-[11px] text-emerald-300">Saved. Running containers pick the new limits up when they are recreated.</span>
                <button type="button" onClick={() => void redeploy()} className="shrink-0 rounded-md bg-emerald-600 px-2.5 py-1 text-[11px] font-semibold text-white hover:bg-emerald-500 cursor-pointer">
                  Apply now
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </ShadcnDialog>
  );
};
