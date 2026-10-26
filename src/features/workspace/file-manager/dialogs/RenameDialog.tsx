import React from 'react';
import { FileText } from 'lucide-react';
import { ShadcnDialog } from '../../../../components/ui/Primitives';

interface RenameDialogProps {
  open: boolean;
  onClose: () => void;
  renameTarget: string | null;
  renameInput: string;
  setRenameInput: (val: string) => void;
  onSubmit: (e: React.FormEvent) => void;
}

export const RenameDialog: React.FC<RenameDialogProps> = ({
  open,
  onClose,
  renameTarget,
  renameInput,
  setRenameInput,
  onSubmit,
}) => {
  if (!renameTarget) return null;

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Rename"
      icon={<FileText className="h-4 w-4" />}
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            New Name
          </label>
          <input
            type="text"
            required
            value={renameInput}
            onChange={(e) => setRenameInput(e.target.value)}
            className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
          />
        </div>
        <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="submit"
            className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            Rename
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
