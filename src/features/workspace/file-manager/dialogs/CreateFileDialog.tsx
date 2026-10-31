import React from 'react';
import { FileCode2 } from 'lucide-react';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { DevFileIcon } from '../../../../components/ui/CodeEditorBox';
import { DirTreePicker } from '../../DirTreePicker';

interface CreateFileDialogProps {
  open: boolean;
  onClose: () => void;
  appSlug: string;
  currentDir: string;
  allFolderPaths: string[];
  newFileName: string;
  setNewFileName: (name: string) => void;
  newFileTargetDir: string;
  setNewFileTargetDir: (dir: string) => void;
  onSubmit: (e: React.FormEvent) => void;
}

export const CreateFileDialog: React.FC<CreateFileDialogProps> = ({
  open,
  onClose,
  appSlug,
  currentDir,
  allFolderPaths,
  newFileName,
  setNewFileName,
  newFileTargetDir,
  setNewFileTargetDir,
  onSubmit,
}) => {
  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="New File"
      icon={<FileCode2 className="h-4 w-4 text-orange-400" />}
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            Quick Language Presets
          </label>
          <div className="flex flex-wrap gap-1.5">
            {[
              { label: 'TypeScript', file: 'handler.ts' },
              { label: 'Python', file: 'worker.py' },
              { label: 'Go', file: 'job.go' },
              { label: 'Rust', file: 'engine.rs' },
              { label: 'PHP', file: 'webhook.php' },
              { label: 'SQL', file: 'migration.sql' },
              { label: 'Shell', file: 'deploy.sh' },
              { label: 'JSON', file: 'settings.json' },
              { label: '.env.example', file: '.env.example' },
            ].map((tpl) => {
              const prefix = currentDir ? `${currentDir}/` : '';
              const suggested = `${prefix}${tpl.file}`;
              const isSelected = newFileName === suggested || newFileName.endsWith(tpl.file);
              return (
                <button
                  key={tpl.label}
                  type="button"
                  onClick={() => setNewFileName(suggested)}
                  className={`inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 font-mono text-[11px] transition-colors cursor-pointer ${
                    isSelected
                      ? 'border-orange-500/50 bg-orange-500/15 text-orange-200 font-semibold'
                      : 'border-white/[0.08] bg-[#09090b] text-zinc-300 hover:border-white/[0.16] hover:text-white'
                  }`}
                >
                  <DevFileIcon filename={tpl.file} />
                  <span>{tpl.label}</span>
                </button>
              );
            })}
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] font-medium text-zinc-400">
              Choose Target Directory
            </label>
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => setNewFileTargetDir('')}
                className={`px-1.5 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                  !newFileTargetDir
                    ? 'bg-amber-500/20 text-amber-300 font-semibold'
                    : 'text-zinc-500 hover:text-zinc-300'
                }`}
              >
                Root
              </button>
              {currentDir && (
                <button
                  type="button"
                  onClick={() => setNewFileTargetDir(currentDir)}
                  className={`px-1.5 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                    newFileTargetDir === currentDir
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
              value={newFileTargetDir}
              onChange={setNewFileTargetDir}
            />
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] font-medium text-zinc-400">
              File Name
            </label>
            <span className="font-mono text-[10px] text-zinc-500">
              Target: /{appSlug}{newFileTargetDir ? `/${newFileTargetDir}` : ''}/
            </span>
          </div>
          <input
            type="text"
            required
            value={newFileName}
            onChange={(e) => setNewFileName(e.target.value.replace(/^\/+/, ''))}
            placeholder="e.g. server.ts or config.json"
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
            Create File
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
