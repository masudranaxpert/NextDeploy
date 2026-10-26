import React from 'react';
import { Plus, KeyRound, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack } from '../../../types/nextdeploy';
import { backendApi } from '../../../lib/backendApi';
import { ActionMenu } from '../../../components/ui/Primitives';

interface ProjectCollaboratorsTabProps {
  app: AppStack;
  onRefresh?: () => Promise<void> | void;
  onOpenInviteModal: () => void;
}

export const ProjectCollaboratorsTab: React.FC<ProjectCollaboratorsTabProps> = ({
  app,
  onRefresh,
  onOpenInviteModal,
}) => {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-zinc-100">Team Access</h2>
          <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-400">
            {app.collaborators.length}
          </span>
        </div>
        <button
          type="button"
          onClick={onOpenInviteModal}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
        >
          <Plus className="h-3.5 w-3.5" />
          Invite Member
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[45%] px-4 py-2.5 font-medium">User</th>
              <th className="w-[25%] px-4 py-2.5 font-medium">Role</th>
              <th className="w-[20%] px-4 py-2.5 font-medium">Added</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {app.collaborators.map((c) => (
              <tr key={c.userId} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3">
                  <div className="font-semibold text-zinc-100">{c.username}</div>
                </td>
                <td className="px-4 py-3">
                  <span className="rounded-md bg-white/[0.06] px-2 py-0.5 font-mono text-[11px] text-zinc-200">
                    {c.role === 'viewer'
                      ? 'Viewer'
                      : c.role === 'owner'
                      ? 'Owner'
                      : 'Developer'}
                  </span>
                </td>
                <td className="px-4 py-3 font-mono text-zinc-400">{c.addedAt}</td>
                <td className="px-3 py-3 text-right">
                  {c.role !== 'owner' && (
                    <ActionMenu
                      items={[
                        {
                          label: 'Transfer Ownership',
                          icon: <KeyRound className="h-3.5 w-3.5" />,
                          onClick: async () => {
                            if (
                              !window.confirm(
                                `Make ${c.username} the owner of ${app.name}? You will become a collaborator.`
                              )
                            )
                              return;
                            const res = await backendApi.transferOwnership(app.id, c.username);
                            if (!res.ok) {
                              toast.error(res.error || 'Could not transfer ownership');
                              return;
                            }
                            toast.success(`${c.username} is now the owner`);
                            await onRefresh?.();
                          },
                        },
                        {
                          label: 'Revoke Access',
                          icon: <Trash2 className="h-3.5 w-3.5" />,
                          variant: 'danger',
                          onClick: async () => {
                            const res = await backendApi.removeCollaborator(app.id, c.userId);
                            if (!res.ok) {
                              toast.error(res.error || 'Could not remove this user');
                              return;
                            }
                            toast.success(`Removed ${c.username}`);
                            await onRefresh?.();
                          },
                        },
                      ]}
                    />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};
