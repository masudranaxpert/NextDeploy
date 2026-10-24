import React from 'react';
import { Database, Power, RotateCw, HardDrive } from 'lucide-react';
import { Card, GhostButton, Pill, PrimaryButton, StatusDot, inputCls } from '../ui';
import type { PhpInstanceData } from '../../../lib/api/phpApi';

interface DatabaseServerCardProps {
  data: PhpInstanceData;
  memory: number;
  busy: string;
  onMemoryChange: (mb: number) => void;
  onAct: (key: string, fn: () => Promise<{ ok: boolean; error?: string }>, msg: string) => Promise<void>;
  backendApi: any;
}

export const DatabaseServerCard: React.FC<DatabaseServerCardProps> = ({
  data,
  memory,
  busy,
  onMemoryChange,
  onAct,
  backendApi,
}) => {
  const m = data.mysql;
  const isStarting = data.mysql_task?.running || (!m.ready && m.running);
  const mysqlTone = m.ready ? 'green' : isStarting ? 'amber' : 'zinc';
  const mysqlLabel = m.ready ? 'Running' : isStarting ? 'Starting...' : m.exists ? 'Stopped' : 'Not Provisioned';

  const PRESETS = [256, 512, 1024, 2048];

  return (
    <Card
      title={
        <div className="flex items-center gap-2">
          <Database className="h-4 w-4 text-sky-400" />
          <span>Database Server (MariaDB)</span>
        </div>
      }
      description="MariaDB container with isolated credentials per site"
      actions={
        <div className="flex items-center gap-2">
          <StatusDot status={m.ready ? 'running' : isStarting ? 'starting' : 'stopped'} />
          <Pill tone={mysqlTone}>{mysqlLabel}</Pill>
        </div>
      }
    >
      {data.mysql_task?.error && (
        <div className="mb-4 rounded-lg border border-red-500/25 bg-red-500/10 px-3.5 py-2.5 text-xs text-red-200">
          {data.mysql_task.error}
        </div>
      )}

      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="flex flex-wrap items-end gap-3 sm:gap-4">
          <div>
            <span className="mb-1.5 block text-[11px] font-medium text-zinc-400">Container Image</span>
            <code className="inline-flex h-8.5 items-center rounded-lg border border-white/[0.08] bg-[#09090b] px-3 font-mono text-xs text-zinc-300">
              {m.image || 'mariadb:11.4'}
            </code>
          </div>

          <div>
            <label className="mb-1.5 block text-[11px] font-medium text-zinc-400">
              Memory Limit
            </label>
            <div className="flex items-center gap-1.5">
              <input
                type="number"
                min={128}
                step={128}
                className={`${inputCls} !w-28 text-center font-mono`}
                value={memory}
                onChange={(e) => onMemoryChange(Number(e.target.value))}
              />
              <span className="text-xs text-zinc-500 font-mono">MB</span>
            </div>
          </div>

          <div className="hidden sm:flex items-center gap-1 pb-1">
            {PRESETS.map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => onMemoryChange(p)}
                className={`rounded border px-2 py-1 font-mono text-[10px] transition-colors cursor-pointer ${
                  memory === p
                    ? 'border-orange-500/50 bg-orange-500/10 text-orange-300 font-semibold'
                    : 'border-white/[0.06] bg-zinc-900 text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {p >= 1024 ? `${p / 1024}GB` : `${p}MB`}
              </button>
            ))}
          </div>
        </div>

        {/* Action Controls */}
        <div className="flex flex-wrap items-center gap-2 pt-2 lg:pt-0">
          {!m.running ? (
            <PrimaryButton
              busy={busy === 'my-on' || data.mysql_task.running}
              onClick={() =>
                onAct('my-on', () => backendApi.phpMysql('enable', memory), 'Starting database server...')
              }
              className="h-8.5"
            >
              <Power className="h-3.5 w-3.5" />
              <span>{m.exists ? 'Start Database' : 'Provision Database'}</span>
            </PrimaryButton>
          ) : (
            <>
              <GhostButton
                busy={busy === 'my-re' || data.mysql_task.running}
                onClick={() =>
                  onAct('my-re', () => backendApi.phpMysql('restart', memory), 'Restarting database server...')
                }
                className="h-8.5"
              >
                <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
                <span>Apply / Restart</span>
              </GhostButton>
              <GhostButton
                tone="danger"
                busy={busy === 'my-off'}
                onClick={() => {
                  if (
                    window.confirm(
                      'Stop MariaDB server? Sites with database queries will experience connection errors until restarted.'
                    )
                  ) {
                    void onAct('my-off', () => backendApi.phpMysql('disable'), 'Database server stopped');
                  }
                }}
                className="h-8.5"
              >
                <span>Stop</span>
              </GhostButton>
            </>
          )}
        </div>
      </div>

      <div className="mt-4 flex items-center gap-1.5 text-[11px] text-zinc-500 pt-3 border-t border-white/[0.06]">
        <HardDrive className="h-3 w-3 text-zinc-400" />
        <span>Data persisted in Docker volume <code className="font-mono text-zinc-300">nd-mysql-data</code></span>
      </div>
    </Card>
  );
};
