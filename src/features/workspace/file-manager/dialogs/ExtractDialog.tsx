import React from 'react';
import { Archive, FolderOpen } from 'lucide-react';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { DirTreePicker } from '../../DirTreePicker';
import { formatFileSize } from '../utils';
import type { ComposeFileNode } from '../../../../types/nextdeploy';

interface ExtractDialogProps {
  open: boolean;
  onClose: () => void;
  appSlug: string;
  extractTargetFile: ComposeFileNode | null;
  extractDestDir: string;
  setExtractDestDir: (dir: string) => void;
  allFolderPaths: string[];
  onSubmit: (e: React.FormEvent) => void;
}

export const ExtractDialog: React.FC<ExtractDialogProps> = ({
  open,
  onClose,
  appSlug,
  extractTargetFile,
  extractDestDir,
  setExtractDestDir,
  allFolderPaths,
  onSubmit,
}) => {
  if (!extractTargetFile) return null;

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Extract Archive (Unzip)"
      icon={<FolderOpen className="h-4 w-4 text-emerald-400" />}
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div className="rounded-lg border border-white/[0.07] bg-[#09090b] p-3 font-mono text-xs flex items-center justify-between">
          <span className="flex items-center gap-2 text-zinc-100 font-semibold truncate">
            <Archive className="h-4 w-4 text-purple-400 shrink-0" />
            <span className="truncate">{extractTargetFile.path}</span>
          </span>
          <span className="text-zinc-500 shrink-0">
            {formatFileSize(extractTargetFile.sizeBytes)}
          </span>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] font-medium text-zinc-400">
              Select Destination Folder
            </label>
            <span className="font-mono text-[10px] text-emerald-400">
              Click any folder below
            </span>
          </div>
          <DirTreePicker
            rootLabel={`/data/apps/${appSlug} (Root)`}
            paths={allFolderPaths}
            value={extractDestDir.trim().replace(/^\/+|\/+$/g, '')}
            onChange={setExtractDestDir}
          />
        </div>

        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            Extract Into Directory Path
          </label>
          <input
            type="text"
            value={extractDestDir}
            onChange={(e) => setExtractDestDir(e.target.value)}
            placeholder="/ (root directory)"
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
            className="rounded-lg bg-emerald-500 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-emerald-400 cursor-pointer"
          >
            Extract Files
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
