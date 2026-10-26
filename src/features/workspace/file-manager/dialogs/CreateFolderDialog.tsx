import React from 'react';
import { FolderPlus } from 'lucide-react';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { DirTreePicker } from '../../DirTreePicker';

interface CreateFolderDialogProps {
  open: boolean;
  onClose: () => void;
  appSlug: string;
  currentDir: string;
  allFolderPaths: string[];
  newFolderName: string;
  setNewFolderName: (name: string) => void;
  newFolderTargetDir: string;
  setNewFolderTargetDir: (dir: string) => void;
  onSubmit: (e: React.FormEvent) => void;
}

export const CreateFolderDialog: React.FC<CreateFolderDialogProps> = ({
  open,
  onClose,
  appSlug,
  currentDir,
  allFolderPaths,
  newFolderName,
  setNewFolderName,
  newFolderTargetDir,
  setNewFolderTargetDir,
  onSubmit,
}) => {
  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="New Folder"
      icon={<FolderPlus className="h-4 w-4 text-amber-400" />}
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] font-medium text-zinc-400">
              Parent Directory
            </label>
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => setNewFolderTargetDir('')}
                className={`px-1.5 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                  !newFolderTargetDir
                    ? 'bg-amber-500/20 text-amber-300 font-semibold'
                    : 'text-zinc-500 hover:text-zinc-300'
                }`}
              >
                Root
              </button>
              {currentDir && (
                <button
                  type="button"
                  onClick={() => setNewFolderTargetDir(currentDir)}
                  className={`px-1.5 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                    newFolderTargetDir === currentDir
                      ? 'bg-amber-500/20 text-amber-300 font-semibold'
                      : 'text-zinc-500 hover:text-zinc-300'
                  }`}
                >
                  Current
                </button>
              )}
            </div>
          </div>
          <div className="relative">
            <DirTreePicker
              rootLabel={`/${appSlug} (Root)`}
              paths={allFolderPaths}
              value={newFolderTargetDir}
              onChange={setNewFolderTargetDir}
            />
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] font-medium text-zinc-400">
              Folder Name
            </label>
            <span className="font-mono text-[10px] text-zinc-500">
              Target: /{appSlug}{newFolderTargetDir ? `/${newFolderTargetDir}` : ''}/
            </span>
          </div>
          <input
            type="text"
            required
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value.replace(/^\/+/, ''))}
            placeholder="e.g. controllers or migrations"
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
            Create Folder
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
