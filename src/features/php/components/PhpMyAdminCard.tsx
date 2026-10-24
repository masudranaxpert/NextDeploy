import React from 'react';
import { ExternalLink, Power, RotateCw, Wrench } from 'lucide-react';
import { toast } from 'sonner';
import { Card, GhostButton, Pill, PrimaryButton, StatusDot } from '../ui';
import type { PhpInstanceData } from '../../../lib/api/phpApi';

interface PhpMyAdminCardProps {
  data: PhpInstanceData;
  busy: string;
  onAct: (key: string, fn: () => Promise<{ ok: boolean; error?: string }>, msg: string) => Promise<void>;
  backendApi: any;
  setBusy: (key: string) => void;
}

export const PhpMyAdminCard: React.FC<PhpMyAdminCardProps> = ({
  data,
  busy,
  onAct,
  backendApi,
  setBusy,
}) => {
  const p = data.pma;
  const isStarting = data.pma_task?.running || (!p.running && p.enabled);
  const tone = p.running ? 'green' : isStarting ? 'amber' : 'zinc';
  const label = p.running ? 'Running' : isStarting ? 'Starting...' : 'Disabled';

  return (
    <Card
      title={
        <div className="flex items-center gap-2">
          <Wrench className="h-4 w-4 text-orange-400" />
          <span>phpMyAdmin Database GUI</span>
        </div>
      }
      description="Web administration dashboard with instant tokenized login"
      actions={
        <div className="flex items-center gap-2">
          <StatusDot status={p.running ? 'running' : isStarting ? 'starting' : 'stopped'} />
          <Pill tone={tone}>{label}</Pill>
        </div>
      }
    >
      {data.pma_task?.error && (
        <div className="mb-4 rounded-lg border border-red-500/25 bg-red-500/10 px-3.5 py-2.5 text-xs text-red-200">
          {data.pma_task.error}
        </div>
      )}

      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <span className="mb-1.5 block text-[11px] font-medium text-zinc-400">Image Tag</span>
            <code className="inline-flex h-8.5 items-center rounded-lg border border-white/[0.08] bg-[#09090b] px-3 font-mono text-xs text-zinc-300">
              {p.image || 'phpmyadmin:5.2-apache'}
            </code>
          </div>
        </div>

        {/* Action Controls */}
        <div className="flex flex-wrap items-center gap-2">
          {!p.enabled ? (
            <PrimaryButton
              disabled={!data.mysql.ready}
              busy={busy === 'pma-on' || data.pma_task.running}
              onClick={() =>
                onAct('pma-on', () => backendApi.phpPma('enable'), 'Enabling phpMyAdmin service...')
              }
              className="h-8.5"
            >
              <Power className="h-3.5 w-3.5" />
              <span>Enable phpMyAdmin</span>
            </PrimaryButton>
          ) : (
            <>
              <GhostButton
                busy={busy === 'pma-open'}
                onClick={async () => {
                  setBusy('pma-open');
                  const r = await backendApi.phpPmaOpenAll();
                  setBusy('');
                  if (r.ok && r.data?.url) {
                    window.open(r.data.url, '_blank', 'noopener');
                  } else {
                    toast.error(r.error || 'Failed to authenticate into phpMyAdmin');
                  }
                }}
                className="h-8.5 text-orange-300 hover:text-orange-200"
              >
                <ExternalLink className="h-3.5 w-3.5 text-orange-400" />
                <span>Open Dashboard (All DBs)</span>
              </GhostButton>

              <GhostButton
                busy={busy === 'pma-re' || data.pma_task.running}
                onClick={() =>
                  onAct('pma-re', () => backendApi.phpPma('restart'), 'Restarting phpMyAdmin...')
                }
                className="h-8.5"
              >
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
                <span>Restart</span>
              </GhostButton>

              <GhostButton
                tone="danger"
                busy={busy === 'pma-off'}
                onClick={() =>
                  onAct('pma-off', () => backendApi.phpPma('disable'), 'phpMyAdmin service disabled')
                }
                className="h-8.5"
              >
                <span>Disable</span>
              </GhostButton>
            </>
          )}
        </div>
      </div>

      {!data.mysql.ready && (
        <div className="mt-4 text-[11px] text-amber-400/90 pt-3 border-t border-white/[0.06]">
          MariaDB database container must be running before starting phpMyAdmin.
        </div>
      )}
    </Card>
  );
};
