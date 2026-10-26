import React from 'react';
import { Terminal as TerminalIcon, RotateCcw } from 'lucide-react';
import { ShadcnDialog } from '../../../components/ui/Primitives';
import { formatCompactPort } from '../../../lib/dockerFormatters';
import type { AppStack, DeploymentRecord } from '../../../types/nextdeploy';

interface BuildLogsModalProps {
  open: boolean;
  onClose: () => void;
  app: AppStack;
  deployment: DeploymentRecord | null;
  renderStyledBuildLog: (logText: string) => React.ReactNode;
  onRollback: (deployment: DeploymentRecord) => void;
}

export const BuildLogsModal: React.FC<BuildLogsModalProps> = ({
  open,
  onClose,
  app,
  deployment,
  renderStyledBuildLog,
  onRollback,
}) => {
  if (!deployment) return null;

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title={`Release ${deployment.id}`}
      icon={<TerminalIcon className="h-4 w-4" />}
      maxWidth="lg"
    >
      <div className="space-y-3 font-mono text-xs">
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 text-[11px]">
          <span className="text-zinc-300">
            Branch:{' '}
            <strong className="text-orange-400">
              {deployment.branch}@{deployment.commitSha.slice(0, 7)}
            </strong>
          </span>
          <span className="text-emerald-400">
            Duration: {deployment.durationSec}s
          </span>
        </div>

        <div className="rounded-xl border border-white/[0.07] bg-[#070709] p-3 space-y-0.5 leading-relaxed max-h-[60vh] overflow-y-auto font-mono text-[11px]">
          {deployment.rawOutput ? (
            <div className="select-text">
              {renderStyledBuildLog(deployment.rawOutput)}
            </div>
          ) : (
            <>
              <div className="text-zinc-500">
                [1/4] $ git checkout {deployment.branch} ({deployment.commitSha})
              </div>
              <div className="text-emerald-400">
                [2/4] ✓ Validated {app.composeFilePath} · injected {app.envVars.length} variables
              </div>
              <div className="text-zinc-300">
                [3/4] $ docker compose -p {app.slug} up -d --build --remove-orphans
              </div>
              {app.services.map((s) => (
                <div key={s.id} className="text-emerald-400 pl-4">
                  ✔ Container {s.name} Healthy ({formatCompactPort(s.ports)})
                </div>
              ))}
              <div className="text-orange-400">
                [4/4] ✓ Switched Caddy upstream route in {deployment.durationSec}s
              </div>
            </>
          )}
        </div>

        <div className="flex justify-end gap-2 pt-1">
          <button
            type="button"
            onClick={() => {
              onRollback(deployment);
              onClose();
            }}
            className="inline-flex items-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/15 px-3 py-1.5 font-sans text-xs font-medium text-orange-300 hover:bg-orange-500/25 cursor-pointer"
          >
            <RotateCcw className="h-3.5 w-3.5" />
            Promote / Rollback to this Release
          </button>
        </div>
      </div>
    </ShadcnDialog>
  );
};
