import React from 'react';
import { Database, RotateCw, Server, Box } from 'lucide-react';
import { Card, GhostButton, Pill, StatusDot } from '../ui';
import type { PhpInstanceData } from '../../../lib/api/phpApi';

interface PhpContainersCardProps {
  data: PhpInstanceData;
  busy: string;
  onAct: (key: string, fn: () => Promise<{ ok: boolean; error?: string }>, msg: string) => Promise<void>;
  backendApi: any;
}

export const PhpContainersCard: React.FC<PhpContainersCardProps> = ({
  data,
  busy,
  onAct,
  backendApi,
}) => {
  const allContainers = data.containers ?? [];
  const managedContainers = allContainers.filter((c) => c.kind !== 'route');
  const routeCount = allContainers.filter((c) => c.kind === 'route').length;

  return (
    <Card
      title={
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-emerald-400" />
          <span>Managed Services & Containers</span>
        </div>
      }
      description="Active Docker containers provisioned by the PHP hosting engine."
    >
      {managedContainers.length === 0 ? (
        <div className="py-6 text-center text-xs text-zinc-500 font-mono">
          No services currently running.
        </div>
      ) : (
        <div className="divide-y divide-white/[0.04]">
          {managedContainers.map((c) => {
            const isRunning = c.state === 'running';

            return (
              <div
                key={c.name}
                className="flex flex-col gap-2 py-3 sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 text-zinc-300">
                    {c.kind === 'mysql' ? (
                      <Database className="h-4 w-4 text-sky-400" />
                    ) : c.kind === 'pma' ? (
                      <Box className="h-4 w-4 text-orange-400" />
                    ) : (
                      <Server className="h-4 w-4 text-violet-400" />
                    )}
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs font-semibold text-zinc-100 truncate">
                        {c.name}
                      </span>
                      <StatusDot status={isRunning ? 'running' : 'stopped'} />
                    </div>
                    <div className="font-mono text-[11px] text-zinc-500 truncate">
                      {c.image} <span className="text-zinc-600">·</span> {c.status}
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2 self-end sm:self-auto shrink-0">
                  <Pill tone={isRunning ? 'green' : 'red'}>{c.state}</Pill>
                  <GhostButton
                    busy={busy === 'rs' + c.name}
                    onClick={() =>
                      onAct(
                        'rs' + c.name,
                        () => backendApi.phpRestartContainer(c.name),
                        `Restarted ${c.name}`
                      )
                    }
                    className="h-7.5 px-2.5"
                    title={`Restart ${c.name}`}
                  >
                    <RotateCw className="h-3 w-3 text-zinc-400" />
                  </GhostButton>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {routeCount > 0 && (
        <div className="mt-3 text-[11px] text-zinc-500 pt-3 border-t border-white/[0.06]">
          {routeCount} background route router {routeCount === 1 ? 'container' : 'containers'} managing internal socket bindings.
        </div>
      )}
    </Card>
  );
};
