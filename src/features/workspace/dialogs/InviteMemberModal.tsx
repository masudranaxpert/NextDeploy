import React, { useState } from 'react';
import { Users } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../components/ui/Primitives';
import { backendApi } from '../../../lib/backendApi';

interface InviteMemberModalProps {
  open: boolean;
  onClose: () => void;
  appId: string;
  onRefresh?: () => void | Promise<void>;
}

export const InviteMemberModal: React.FC<InviteMemberModalProps> = ({
  open,
  onClose,
  appId,
  onRefresh,
}) => {
  const [newMemberEmail, setNewMemberEmail] = useState('');
  const [newMemberRole, setNewMemberRole] = useState<'deployer' | 'viewer'>('deployer');
  const [inviting, setInviting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const username = newMemberEmail.trim();
    if (!username) return;
    setInviting(true);
    const res = await backendApi.addCollaborator(
      appId,
      username,
      newMemberRole === 'deployer' ? 'developer' : 'viewer'
    );
    setInviting(false);
    if (!res.ok) {
      toast.error(res.error || 'Could not add this user');
      return;
    }
    setNewMemberEmail('');
    onClose();
    toast.success(`${username} now has access`);
    await onRefresh?.();
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={() => !inviting && onClose()}
      title="Invite member"
      icon={<Users className="h-4 w-4" />}
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <div>
          <label htmlFor="invite-username" className="block text-[11px] font-medium text-zinc-300 mb-1.5">
            Username
          </label>
          <input
            id="invite-username"
            type="text"
            required
            autoFocus
            autoComplete="off"
            value={newMemberEmail}
            onChange={(e) => setNewMemberEmail(e.target.value)}
            placeholder="their NextDeploy username"
            className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 text-xs text-zinc-100 outline-none focus:border-orange-500/50"
          />
        </div>
        <div className="grid grid-cols-2 gap-2.5">
          {(
            [
              { id: 'deployer', title: 'Developer', text: 'Deploy, edit files, use the terminal, change environment variables and backups.' },
              { id: 'viewer', title: 'Viewer', text: 'See the project and its status. Cannot open files or secrets, and cannot change anything.' },
            ] as const
          ).map((r) => (
            <button
              key={r.id}
              type="button"
              onClick={() => setNewMemberRole(r.id)}
              className={`cursor-pointer rounded-lg border p-3 text-left transition-colors ${
                newMemberRole === r.id
                  ? 'border-orange-500/50 bg-orange-500/10'
                  : 'border-white/[0.08] bg-[#09090b] hover:bg-zinc-900'
              }`}
            >
              <div className="text-xs font-semibold text-zinc-100">{r.title}</div>
              <div className="mt-1 text-[11px] leading-snug text-zinc-400">{r.text}</div>
            </button>
          ))}
        </div>
        <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
          <button
            type="button"
            onClick={onClose}
            disabled={inviting}
            className="h-8 rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 text-xs text-zinc-300 hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={inviting || !newMemberEmail.trim()}
            className="h-8 rounded-lg bg-orange-500 px-4 text-xs font-semibold text-white hover:bg-orange-600 cursor-pointer disabled:opacity-50"
          >
            {inviting ? 'Adding…' : 'Add member'}
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
