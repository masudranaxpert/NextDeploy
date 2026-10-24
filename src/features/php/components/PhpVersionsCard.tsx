import React from 'react';
import { Download, FileText, Layers, Trash2 } from 'lucide-react';
import { Card, GhostButton, Pill, PrimaryButton } from '../ui';
import type { PhpInstanceData } from '../../../lib/api/phpApi';

interface PhpVersionsCardProps {
  data: PhpInstanceData;
  busy: string;
  onOpenLog: (version: string) => void;
  onAct: (key: string, fn: () => Promise<{ ok: boolean; error?: string }>, msg: string) => Promise<void>;
  backendApi: any;
}

export const PhpVersionsCard: React.FC<PhpVersionsCardProps> = ({
  data,
  busy,
  onOpenLog,
  onAct,
  backendApi,
}) => {
  const versions = data.versions ?? [];

  return (
    <Card
      title={
        <div className="flex items-center gap-2">
          <Layers className="h-4 w-4 text-violet-400" />
          <span>PHP Runtimes</span>
        </div>
      }
      description="Pre-compiled Docker images with OPcache, common extensions, and isolated FPM pools."
    >
      <div className="overflow-x-auto -mx-4 sm:mx-0">
        <table className="w-full text-left text-xs min-w-[500px]">
          <thead>
            <tr className="border-b border-white/[0.06] text-[11px] font-medium text-zinc-400">
              <th className="py-2.5 px-4 font-medium">PHP Version</th>
              <th className="py-2.5 px-4 font-medium">Runtime Status</th>
              <th className="py-2.5 px-4 font-medium">Assigned Sites</th>
              <th className="py-2.5 px-4 text-right font-medium">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.04]">
            {versions.map((v) => {
              const isBuilding = v.build?.running;
              const hasSites = (v.sites || 0) > 0;
              const isBusy = busy === 'in' + v.version || busy === 'rm' + v.version;

              return (
                <tr key={v.version} className="hover:bg-white/[0.02] transition-colors">
                  <td className="py-3 px-4 font-mono font-semibold text-zinc-100">
                    PHP {v.version}
                  </td>
                  <td className="py-3 px-4">
                    {isBuilding ? (
                      <Pill tone="amber">Building...</Pill>
                    ) : v.installed ? (
                      <Pill tone="green">Installed</Pill>
                    ) : v.build?.error ? (
                      <Pill tone="red">Build Failed</Pill>
                    ) : (
                      <Pill tone="zinc">Available</Pill>
                    )}
                  </td>
                  <td className="py-3 px-4 font-mono text-zinc-400">
                    {v.sites || 0} {v.sites === 1 ? 'site' : 'sites'}
                  </td>
                  <td className="py-3 px-4 text-right">
                    <div className="flex items-center justify-end gap-2">
                      {v.build && (
                        <GhostButton onClick={() => onOpenLog(v.version)} className="h-7.5 px-2.5 text-[11px]">
                          <FileText className="h-3 w-3 text-zinc-400" />
                          <span>Build Log</span>
                        </GhostButton>
                      )}

                      {v.installed ? (
                        <GhostButton
                          tone="danger"
                          disabled={hasSites || isBusy}
                          title={hasSites ? 'Cannot remove: runtime is used by active sites' : 'Remove PHP image'}
                          busy={busy === 'rm' + v.version}
                          onClick={() => {
                            if (window.confirm(`Remove Docker runtime for PHP ${v.version}?`)) {
                              void onAct(
                                'rm' + v.version,
                                () => backendApi.phpVersion(v.version, 'remove'),
                                `PHP ${v.version} removed`
                              );
                            }
                          }}
                          className="h-7.5 px-2.5 text-[11px]"
                        >
                          <Trash2 className="h-3 w-3" />
                          <span>Remove</span>
                        </GhostButton>
                      ) : (
                        <PrimaryButton
                          busy={busy === 'in' + v.version || !!isBuilding}
                          onClick={() =>
                            onAct(
                              'in' + v.version,
                              () => backendApi.phpVersion(v.version, 'install'),
                              `Building PHP ${v.version} container image in background...`
                            )
                          }
                          className="h-7.5 px-2.5 text-[11px]"
                        >
                          <Download className="h-3 w-3" />
                          <span>Install</span>
                        </PrimaryButton>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
};
