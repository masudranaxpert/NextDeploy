import React, { useState } from 'react';
import { Check, Copy, Folder, FolderPlus } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { DevFileIcon } from '../../../../components/ui/CodeEditorBox';
import { DirTreePicker } from '../../DirTreePicker';
import { copyToClipboard } from '../../../../lib/clipboard';

interface MoveTarget {
  paths: string[];
  mode: 'move' | 'copy';
}

interface MoveCopyDialogProps {
  open: boolean;
  onClose: () => void;
  appSlug: string;
  moveTarget: MoveTarget | null;
  setMoveTarget: (target: MoveTarget | null) => void;
  moveDestInput: string;
  setMoveDestInput: (val: string) => void;
  allFolderPaths: string[];
  onAddCustomFolder: (folder: string) => void;
  onSubmit: (e: React.FormEvent) => void;
}

export const MoveCopyDialog: React.FC<MoveCopyDialogProps> = ({
  open,
  onClose,
  appSlug,
  moveTarget,
  setMoveTarget,
  moveDestInput,
  setMoveDestInput,
  allFolderPaths,
  onAddCustomFolder,
  onSubmit,
}) => {
  const [showInlineFolderCreator, setShowInlineFolderCreator] = useState(false);
  const [inlineNewDestFolder, setInlineNewDestFolder] = useState('');
  const [copiedPath, setCopiedPath] = useState(false);

  if (!moveTarget) return null;

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title={moveTarget.mode === 'move' ? 'Move Files / Folders' : 'Copy Files / Folders'}
      icon={<Copy className="h-4 w-4 text-sky-400" />}
    >
      <form onSubmit={onSubmit} className="space-y-4">
        {/* Source Files Indicator */}
        <div className="rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 font-mono text-xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] text-zinc-400 font-sans">
              Selected Items ({moveTarget.paths.length}):
            </span>
            <span className="rounded bg-white/[0.06] px-1.5 py-0.5 text-[10px] uppercase text-zinc-400 shrink-0">
              Source
            </span>
          </div>
          <div className="max-h-24 overflow-y-auto space-y-1">
            {moveTarget.paths.map((p) => {
              const fname = p.split('/').pop() || p;
              return (
                <div key={p} className="flex items-center gap-2 text-zinc-200 truncate">
                  <DevFileIcon filename={fname} />
                  <span className="truncate font-semibold">{p}</span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Mode Switch: Move vs Copy */}
        <div className="grid grid-cols-2 gap-2">
          {(['move', 'copy'] as const).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setMoveTarget({ ...moveTarget, mode: m })}
              className={`rounded-lg border px-3 py-1.5 text-xs font-semibold uppercase cursor-pointer transition-colors ${
                moveTarget.mode === m
                  ? 'border-orange-500/50 bg-orange-500/15 text-orange-300'
                  : 'border-white/[0.08] bg-[#09090b] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {m === 'move' ? 'Move File' : 'Copy File'}
            </button>
          ))}
        </div>

        {/* Interactive Visual Folder Tree Picker */}
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] font-medium text-zinc-400">
              Choose Destination Folder
            </label>
            <button
              type="button"
              onClick={() => setShowInlineFolderCreator((prev) => !prev)}
              className="inline-flex items-center gap-1 font-mono text-[11px] text-amber-400 hover:text-amber-300 cursor-pointer"
            >
              <FolderPlus className="h-3 w-3" />
              <span>{showInlineFolderCreator ? 'Cancel' : '+ New Folder'}</span>
            </button>
          </div>

          {showInlineFolderCreator && (
            <div className="mb-2 flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/[0.06] p-2">
              <Folder className="h-3.5 w-3.5 text-amber-400 shrink-0" />
              <input
                type="text"
                value={inlineNewDestFolder}
                onChange={(e) => setInlineNewDestFolder(e.target.value)}
                placeholder="e.g. src/controllers or backups"
                className="flex-1 bg-transparent font-mono text-xs text-zinc-100 placeholder-zinc-500 focus:outline-none"
              />
              <button
                type="button"
                onClick={() => {
                  const clean = inlineNewDestFolder
                    .trim()
                    .replace(/^\/+|\/+$/g, '')
                    .replace(/[^a-zA-Z0-9/_-]/g, '-');
                  if (!clean) return;
                  onAddCustomFolder(clean);
                  setMoveDestInput(clean);
                  setInlineNewDestFolder('');
                  setShowInlineFolderCreator(false);
                  toast.success(`Created & selected /${clean}`);
                }}
                className="rounded-md bg-amber-500 px-2.5 py-1 text-[11px] font-semibold text-zinc-950 hover:bg-amber-400 cursor-pointer"
              >
                Add & Select
              </button>
            </div>
          )}

          <DirTreePicker
            rootLabel="/ (Root Directory)"
            paths={allFolderPaths}
            value={moveDestInput.trim().replace(/^\/+|\/+$/g, '')}
            onChange={setMoveDestInput}
          />
        </div>

        {/* Custom Path Input + Live Target Preview */}
        <div className="space-y-2">
          <label className="block text-[11px] font-medium text-zinc-300">
            Destination Folder Path
          </label>
          <input
            type="text"
            value={moveDestInput}
            onChange={(e) => setMoveDestInput(e.target.value)}
            placeholder="/ (Root) or src/routes"
            className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
          />

          {(() => {
            const cleanSub = moveDestInput.trim().replace(/^\/+|\/+$/g, '');
            const fullPath = `/data/apps/${appSlug}${cleanSub ? `/${cleanSub}` : ''}`;

            return (
              <div className="flex items-center justify-between gap-2.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-2.5 transition-colors hover:border-white/[0.12]">
                <div className="min-w-0 flex-1">
                  <span className="block text-[10px] font-medium uppercase tracking-wider text-zinc-500">
                    Full Destination Path
                  </span>
                  <div
                    className="font-mono text-xs font-semibold text-emerald-400 break-all select-all pt-0.5 leading-snug"
                    title={fullPath}
                  >
                    {fullPath}
                  </div>
                </div>

                <button
                  type="button"
                  onClick={async () => {
                    const ok = await copyToClipboard(fullPath);
                    if (ok) {
                      setCopiedPath(true);
                      setTimeout(() => setCopiedPath(false), 1500);
                    }
                  }}
                  title="Copy full path"
                  className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md border transition-all cursor-pointer ${
                    copiedPath
                      ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                      : 'border-white/[0.08] bg-zinc-900 text-zinc-400 hover:border-white/[0.15] hover:text-white'
                  }`}
                >
                  {copiedPath ? (
                    <Check className="h-3.5 w-3.5 text-emerald-400 animate-in zoom-in-75 duration-150" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
            );
          })()}
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
            {moveTarget.mode === 'move' ? 'Move Here' : 'Copy Here'}
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
