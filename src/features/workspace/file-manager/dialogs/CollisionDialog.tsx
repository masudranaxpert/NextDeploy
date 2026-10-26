import React from 'react';
import { AlertTriangle } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { DevFileIcon } from '../../../../components/ui/CodeEditorBox';

interface ConflictModalState {
  paths: string[];
  destFolder: string;
  mode: 'move' | 'copy';
  conflicts: string[];
}

interface CollisionDialogProps {
  open: boolean;
  onClose: () => void;
  conflictModal: ConflictModalState | null;
  onExecute: (paths: string[], destFolder: string, mode: 'move' | 'copy', replace: boolean) => Promise<void>;
}

export const CollisionDialog: React.FC<CollisionDialogProps> = ({
  open,
  onClose,
  conflictModal,
  onExecute,
}) => {
  if (!conflictModal) return null;

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="File Collision Detected"
      icon={<AlertTriangle className="h-4 w-4 text-amber-400" />}
    >
      <div className="space-y-4">
        <div className="rounded-lg border border-amber-500/25 bg-amber-500/[0.08] p-3 text-xs text-amber-200">
          <p className="font-semibold text-amber-100">
            The destination already contains {conflictModal.conflicts.length} item(s) with the same name:
          </p>
          <div className="mt-2 max-h-32 overflow-y-auto space-y-1 font-mono text-[11px] text-zinc-300">
            {conflictModal.conflicts.map((p) => {
              const fname = p.split('/').pop() || p;
              return (
                <div key={p} className="flex items-center gap-2 rounded bg-black/40 px-2 py-1">
                  <DevFileIcon filename={fname} />
                  <span className="font-semibold text-zinc-100">{fname}</span>
                  <span className="text-zinc-500 text-[10px]">
                    ({conflictModal.destFolder ? `/${conflictModal.destFolder}/${fname}` : `/${fname}`})
                  </span>
                </div>
              );
            })}
          </div>
          <p className="mt-2.5 text-zinc-400">
            Would you like to <strong>Replace</strong> the existing items or <strong>Skip</strong> them during the {conflictModal.mode}?
          </p>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.07] pt-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-400 hover:text-zinc-200 cursor-pointer"
          >
            Cancel
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={async () => {
                const ctx = conflictModal;
                onClose();
                const nonConflicting = ctx.paths.filter((p) => !ctx.conflicts.includes(p));
                if (nonConflicting.length > 0) {
                  await onExecute(nonConflicting, ctx.destFolder, ctx.mode, false);
                } else {
                  toast.info('Skipped all items because they already exist in the destination.');
                }
              }}
              className="rounded-lg border border-white/[0.12] bg-zinc-800 px-3.5 py-1.5 text-xs font-medium text-zinc-200 hover:bg-zinc-700 cursor-pointer"
            >
              Skip Conflicting
            </button>
            <button
              type="button"
              onClick={async () => {
                const ctx = conflictModal;
                onClose();
                await onExecute(ctx.paths, ctx.destFolder, ctx.mode, true);
              }}
              className="rounded-lg bg-orange-500 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-orange-400 cursor-pointer shadow-sm"
            >
              Replace Existing
            </button>
          </div>
        </div>
      </div>
    </ShadcnDialog>
  );
};
