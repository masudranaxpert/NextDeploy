import React from 'react';
import { ArrowLeft, RotateCw, X } from 'lucide-react';
import { toast } from 'sonner';
import { CodeEditorBox } from '../../../../components/ui/CodeEditorBox';
import { backendApi } from '../../../../lib/backendApi';
import { formatPermOctal } from '../utils';
import type { AppStack, ComposeFileNode } from '../../../../types/nextdeploy';

interface FileManagerEditorProps {
  app: AppStack;
  selectedFile: ComposeFileNode;
  openEditorTabs: string[];
  defaultRichFiles: ComposeFileNode[];
  filePermissions: Record<string, string>;
  editorSaveTick: number;
  setEditorSaveTick: React.Dispatch<React.SetStateAction<number>>;
  isLoading?: boolean;
  onCloseEditor: () => void;
  onSelectTab: (path: string) => void;
  onCloseTab: (path: string, e: React.MouseEvent) => void;
  onCloseAllTabs: () => void;
  onOpenChmod: (path: string) => void;
  onSaveContent: (content: string) => void;
  onSyncFiles: () => Promise<void>;
  onUpdateApp: (updated: AppStack) => void;
  renderFileIcon: (filename: string) => React.ReactNode;
}

export const FileManagerEditor: React.FC<FileManagerEditorProps> = ({
  app,
  selectedFile,
  openEditorTabs,
  defaultRichFiles,
  filePermissions,
  editorSaveTick,
  setEditorSaveTick,
  isLoading = false,
  onCloseEditor,
  onSelectTab,
  onCloseTab,
  onCloseAllTabs,
  onOpenChmod,
  onSaveContent,
  onSyncFiles,
  onUpdateApp,
  renderFileIcon,
}) => {
  return (
    <div className="space-y-2 p-3">
      {/* Tab Strip */}
      <div className="flex items-center justify-between gap-2 rounded-lg border border-white/[0.07] bg-[#09090b] px-2 py-1.5 min-w-0">
        <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar min-w-0 flex-1">
          <button
            type="button"
            onClick={onCloseEditor}
            className="inline-flex items-center gap-1.5 rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs font-medium text-zinc-200 hover:bg-zinc-800 hover:text-white shrink-0 cursor-pointer"
          >
            <ArrowLeft className="h-3.5 w-3.5 text-amber-400" />
            <span>Back to Files</span>
          </button>

          {openEditorTabs.map((tabPath) => {
            const tabFile = defaultRichFiles.find((f) => f.path === tabPath);
            if (!tabFile) return null;
            const active = selectedFile.path === tabPath;
            return (
              <div
                key={tabPath}
                onClick={() => onSelectTab(tabPath)}
                className={`group inline-flex items-center gap-1.5 rounded-md border pl-2.5 pr-1.5 py-1 font-mono text-xs cursor-pointer transition-colors shrink-0 ${
                  active
                    ? 'border-orange-500/40 bg-orange-500/12 text-zinc-100 font-semibold'
                    : 'border-white/[0.05] bg-white/[0.03] text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-200'
                }`}
              >
                {renderFileIcon(tabFile.name)}
                <span className="max-w-[160px] truncate">{tabFile.name}</span>
                <button
                  type="button"
                  onClick={(e) => onCloseTab(tabPath, e)}
                  title={`Close ${tabFile.name}`}
                  className={`ml-0.5 inline-flex h-4 w-4 items-center justify-center rounded transition-colors cursor-pointer ${
                    active
                      ? 'text-zinc-300 hover:bg-orange-500/25 hover:text-white'
                      : 'text-zinc-500 hover:bg-white/[0.1] hover:text-zinc-200'
                  }`}
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            );
          })}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {openEditorTabs.length > 1 && (
            <button
              type="button"
              onClick={onCloseAllTabs}
              className="hidden sm:inline-flex items-center gap-1 rounded border border-white/[0.07] bg-zinc-900/80 px-2 py-1 text-[10px] font-medium text-zinc-400 hover:text-zinc-200 cursor-pointer"
              title="Close all open tabs"
            >
              <span>Close All</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => onOpenChmod(selectedFile.path)}
            className="hidden md:inline-block font-mono text-[11px] text-emerald-400 hover:underline cursor-pointer px-1.5"
          >
            {formatPermOctal(filePermissions[selectedFile.path], false)}
          </button>
        </div>
      </div>

      <CodeEditorBox
        filename={selectedFile.path}
        language={selectedFile.language}
        value={selectedFile.content}
        isLoading={isLoading}
        onChange={onSaveContent}
        onSave={async () => {
          const res = await backendApi.fileSave(
            app.id,
            selectedFile.path,
            selectedFile.content
          );
          if (!res.ok) {
            toast.error(res.error || res.message || `Failed to save ${selectedFile.path}`);
            return;
          }
          if (app.gitRepo && (app.gitSync !== false || app.sourceType === 'git')) {
            void backendApi.setGitSync(app.id, false, false);
            onUpdateApp({
              ...app,
              sourceType: 'compose',
              gitSync: false,
            });
            toast.success(
              `Saved ${selectedFile.path} · Git Sync automatically paused due to manual file edit`
            );
          } else {
            toast.success(`Saved ${selectedFile.path}`);
          }
          await onSyncFiles();
          setEditorSaveTick((t) => t + 1);
        }}
        permissions={formatPermOctal(filePermissions[selectedFile.path], false)}
        savedTick={editorSaveTick}
        height={420}
        actions={
          !app.php ? (
            <button
              type="button"
              onClick={async () => {
                const res = await backendApi.fileSave(
                  app.id,
                  selectedFile.path,
                  selectedFile.content
                );
                if (!res.ok) {
                  toast.error(
                    res.error || res.message || `Failed to save ${selectedFile.path}`
                  );
                  return;
                }
                if (app.gitRepo && (app.gitSync !== false || app.sourceType === 'git')) {
                  void backendApi.setGitSync(app.id, false, false);
                  onUpdateApp({
                    ...app,
                    sourceType: 'compose',
                    gitSync: false,
                  });
                }
                await onSyncFiles();
                toast.success(`Saved ${selectedFile.path}`);
              }}
              className="inline-flex h-7 items-center gap-1 rounded-lg border border-orange-500/30 bg-orange-500/15 px-2.5 text-[11px] font-semibold text-orange-300 hover:bg-orange-500/25 cursor-pointer"
            >
              <RotateCw className="h-3 w-3" />
              <span className="hidden sm:inline">Save File</span>
              <span className="sm:hidden">Save</span>
            </button>
          ) : null
        }
      />
    </div>
  );
};
