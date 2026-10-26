import React from 'react';
import {
  Archive,
  ArrowLeft,
  ArrowLeftRight,
  ChevronDown,
  ChevronRight,
  Copy,
  Download,
  FilePlus,
  Folder,
  FolderOpen,
  FolderPlus,
  RotateCcw,
  Search,
  Trash2,
  Upload,
} from 'lucide-react';
import type { ComposeFileNode } from '../../../../types/nextdeploy';
import { ActionMenu } from '../../../../components/ui/Primitives';

interface FileManagerToolbarProps {
  appSlug: string;
  appId: string;
  currentDir: string;
  editingFilePath: string | null;
  selectedFile: ComposeFileNode;
  selectedEntries: string[];
  fileSearch: string;
  setFileSearch: (s: string) => void;
  setEditingFilePath: (p: string | null) => void;
  navigateToFolder: (folder: string) => void;
  onOpenNewFile: () => void;
  onOpenNewFolder: () => void;
  onOpenUpload: () => void;
  onOpenCompress: () => void;
  onOpenExtract: () => void;
  onOpenRestoreArchive: () => void;
  onMoveSelected: () => void;
  onCopySelected: () => void;
  onDeleteSelected: () => void;
  onClearSelection?: () => void;
  mobileTreeOpen: boolean;
  setMobileTreeOpen: React.Dispatch<React.SetStateAction<boolean>>;
  currentSubfoldersCount: number;
  currentDirFilesCount: number;
  renderFileIcon: (filename: string) => React.ReactNode;
}

export const FileManagerToolbar: React.FC<FileManagerToolbarProps> = ({
  appSlug,
  appId,
  currentDir,
  editingFilePath,
  selectedFile,
  selectedEntries,
  fileSearch,
  setFileSearch,
  setEditingFilePath,
  navigateToFolder,
  onOpenNewFile,
  onOpenNewFolder,
  onOpenUpload,
  onOpenCompress,
  onOpenExtract,
  onOpenRestoreArchive,
  onMoveSelected,
  onCopySelected,
  onDeleteSelected,
  onClearSelection,
  mobileTreeOpen,
  setMobileTreeOpen,
  currentSubfoldersCount,
  currentDirFilesCount,
  renderFileIcon,
}) => {
  return (
    <>
      {/* 1. DEDICATED SELECTION ACTIONS STRIP (ONLY WHEN ITEMS SELECTED) */}
      {selectedEntries.length > 0 && (
        <div className="flex items-center justify-between gap-2 border-b border-orange-500/30 bg-orange-500/[0.09] px-3 sm:px-4 py-1.5 transition-all">
          <div className="flex items-center gap-2 min-w-0">
            <span className="inline-flex items-center gap-1 rounded-md bg-orange-500/20 border border-orange-500/30 px-2 py-0.5 font-mono text-xs font-semibold text-orange-200 shrink-0">
              {selectedEntries.length}
              <span className="font-sans font-normal text-orange-300/90 hidden xs:inline">
                {selectedEntries.length === 1 ? 'item' : 'items'}
              </span>
            </span>
            {onClearSelection && (
              <button
                type="button"
                onClick={onClearSelection}
                className="text-xs text-zinc-400 hover:text-zinc-200 underline cursor-pointer truncate"
              >
                Clear
              </button>
            )}
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            <button
              type="button"
              onClick={onMoveSelected}
              className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.1] bg-zinc-900/90 px-2 text-xs font-medium text-zinc-200 hover:bg-zinc-800 hover:text-white transition cursor-pointer"
              title="Move selected files/folders"
            >
              <ArrowLeftRight className="h-3 w-3 text-orange-400 shrink-0" />
              <span>Move</span>
            </button>
            <button
              type="button"
              onClick={onCopySelected}
              className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.1] bg-zinc-900/90 px-2 text-xs font-medium text-zinc-200 hover:bg-zinc-800 hover:text-white transition cursor-pointer"
              title="Copy selected files/folders"
            >
              <Copy className="h-3 w-3 text-sky-400 shrink-0" />
              <span>Copy</span>
            </button>
            <button
              type="button"
              onClick={onDeleteSelected}
              className="inline-flex h-7 items-center gap-1 rounded-lg border border-rose-500/40 bg-rose-500/20 px-2 text-xs font-semibold text-rose-300 hover:bg-rose-500/30 transition cursor-pointer"
              title="Delete selected files/folders"
            >
              <Trash2 className="h-3 w-3 text-rose-300 shrink-0" />
              <span>Delete</span>
            </button>
          </div>
        </div>
      )}

      {/* 2. MAIN ACTION TOOLBAR */}
      <div className="border-b border-white/[0.07] bg-[#0c0c0f] px-3 sm:px-4 py-2">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-2">
          {/* Action Buttons: Unified Creation & Utilities */}
          <div className="flex items-center justify-between sm:justify-start gap-1.5 w-full md:w-auto">
            {/* Primary Creation Actions */}
            <div className="flex items-center gap-1.5 flex-1 sm:flex-initial">
              <button
                type="button"
                onClick={onOpenNewFile}
                className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg bg-orange-500 hover:bg-orange-600 text-white font-medium px-2.5 text-xs shadow-sm shadow-orange-950/40 border border-orange-400/30 transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer shrink-0"
              >
                <FilePlus className="h-3.5 w-3.5 shrink-0" />
                <span>New File</span>
              </button>

              <button
                type="button"
                onClick={onOpenNewFolder}
                className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:text-white text-zinc-300 px-2 sm:px-2.5 text-xs font-medium transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer shrink-0"
              >
                <FolderPlus className="h-3.5 w-3.5 text-amber-400 shrink-0" />
                <span>New Folder</span>
              </button>

              <button
                type="button"
                onClick={onOpenUpload}
                className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:text-white text-zinc-300 px-2 sm:px-2.5 text-xs font-medium transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer shrink-0"
              >
                <Upload className="h-3.5 w-3.5 text-sky-400 shrink-0" />
                <span>Upload</span>
              </button>
            </div>

            {/* Mobile Archive Tools ActionMenu (< md) */}
            <div className="md:hidden shrink-0">
              <ActionMenu
                ariaLabel="Archive & Workspace Tools"
                trigger={
                  <div className="inline-flex h-8 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900/90 px-2 text-xs font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer">
                    <Archive className="h-3.5 w-3.5 text-zinc-400" />
                    <span>Tools</span>
                    <ChevronDown className="h-3 w-3 text-zinc-500" />
                  </div>
                }
                items={[
                  {
                    label: 'Compress files (.zip/.tar.gz)',
                    icon: <Archive className="h-4 w-4 text-purple-400" />,
                    onClick: onOpenCompress,
                  },
                  {
                    label: 'Extract archive',
                    icon: <FolderOpen className="h-4 w-4 text-emerald-400" />,
                    onClick: onOpenExtract,
                  },
                  {
                    label: 'Export workspace (.tar.gz)',
                    icon: <Download className="h-4 w-4 text-sky-400" />,
                    onClick: () => {
                      window.location.href = `/api/v1/apps/${encodeURIComponent(appId)}/workspace/archive`;
                    },
                  },
                  {
                    label: 'Restore archive',
                    icon: <RotateCcw className="h-4 w-4 text-amber-400" />,
                    onClick: onOpenRestoreArchive,
                  },
                ]}
              />
            </div>

            <div className="hidden md:block h-4 w-px bg-white/[0.08] mx-1" />

            {/* Desktop Full Archive & Backup Group (>= md) */}
            <div className="hidden md:flex items-center gap-1.5">
              <button
                type="button"
                onClick={onOpenCompress}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:text-white text-zinc-300 px-2.5 text-xs font-medium transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer"
                title="Compress files or folders into .zip or .tar.gz"
              >
                <Archive className="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                <span>Compress</span>
              </button>

              <button
                type="button"
                onClick={onOpenExtract}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:text-white text-zinc-300 px-2.5 text-xs font-medium transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer"
                title="Extract .zip or .tar.gz archive"
              >
                <FolderOpen className="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                <span>Extract</span>
              </button>

              <a
                href={`/api/v1/apps/${encodeURIComponent(appId)}/workspace/archive`}
                download={`${appSlug}-workspace.tar.gz`}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:text-white text-zinc-300 px-2.5 text-xs font-medium transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer"
                title="Download full workspace backup (.tar.gz archive)"
              >
                <Download className="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                <span>Export Workspace</span>
              </a>

              <button
                type="button"
                onClick={onOpenRestoreArchive}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 hover:bg-zinc-800 hover:text-white text-zinc-300 px-2.5 text-xs font-medium transition-transform active:scale-[0.97] touch-manipulation select-none cursor-pointer"
                title="Restore full workspace from a .tar.gz or .zip archive"
              >
                <RotateCcw className="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                <span>Restore Archive</span>
              </button>
            </div>
          </div>

          {/* Search Filter */}
          <div className="relative w-full md:w-52 shrink-0">
            <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={fileSearch}
              onChange={(e) => {
                setFileSearch(e.target.value);
                if (e.target.value.trim()) setEditingFilePath(null);
              }}
              placeholder="Search files..."
              className="h-8 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-2.5 text-base sm:text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none transition-colors"
            />
          </div>
        </div>
      </div>

      {/* 2. PATH & BREADCRUMB NAVIGATION BAR */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#09090b]/90 px-3.5 py-2 font-mono text-xs">
        <div className="flex flex-wrap items-center gap-1.5 min-w-0">
          <button
            type="button"
            disabled={!currentDir && !editingFilePath}
            onClick={() => {
              if (editingFilePath) {
                setEditingFilePath(null);
                return;
              }
              const parts = currentDir.split('/').filter(Boolean);
              parts.pop();
              navigateToFolder(parts.join('/'));
            }}
            className="inline-flex h-6 items-center gap-1 rounded border border-white/[0.08] bg-zinc-900 px-2 text-[11px] font-sans font-medium text-zinc-300 hover:text-white disabled:opacity-35 disabled:pointer-events-none cursor-pointer"
          >
            <ArrowLeft className="h-3 w-3" />
            <span>Up</span>
          </button>

          <div className="flex flex-wrap items-center gap-1 pl-1">
            <button
              type="button"
              onClick={() => navigateToFolder('')}
              className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 transition-colors cursor-pointer ${
                currentDir === '' && !editingFilePath
                  ? 'bg-amber-500/15 text-amber-300 font-semibold'
                  : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
              }`}
            >
              <FolderOpen className="h-3.5 w-3.5 text-amber-400 fill-amber-400/20" />
              <span>/data/apps/{appSlug}</span>
            </button>

            {currentDir
              .split('/')
              .filter(Boolean)
              .map((segment, idx, arr) => {
                const targetPath = arr.slice(0, idx + 1).join('/');
                const isLast = idx === arr.length - 1;
                return (
                  <React.Fragment key={targetPath}>
                    <ChevronRight className="h-3.5 w-3.5 text-zinc-600" />
                    <button
                      type="button"
                      onClick={() => navigateToFolder(targetPath)}
                      className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 transition-colors cursor-pointer ${
                        isLast && !editingFilePath
                          ? 'bg-amber-500/15 text-amber-300 font-semibold'
                          : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
                      }`}
                    >
                      <Folder className="h-3.5 w-3.5 text-amber-400 fill-amber-400/20" />
                      <span>{segment}</span>
                    </button>
                  </React.Fragment>
                );
              })}

            {editingFilePath && (
              <>
                <ChevronRight className="h-3.5 w-3.5 text-zinc-600" />
                <span className="inline-flex items-center gap-1.5 rounded bg-orange-500/15 px-2 py-0.5 font-semibold text-orange-300">
                  {renderFileIcon(selectedFile.name)}
                  <span>{selectedFile.name}</span>
                </span>
              </>
            )}
          </div>
        </div>

        <div className="flex items-center gap-3 text-[11px] text-zinc-500 font-sans">
          <button
            type="button"
            onClick={() => setMobileTreeOpen((prev) => !prev)}
            className="inline-flex lg:hidden items-center gap-1 rounded border border-white/[0.08] bg-zinc-900 px-2 py-0.5 text-[11px] font-medium text-zinc-200 cursor-pointer"
          >
            <Folder className="h-3 w-3 text-amber-400" />
            <span>{mobileTreeOpen ? 'Hide Tree' : 'Folder Tree'}</span>
          </button>
          <span className="hidden sm:inline">Right-click any row for actions</span>
          <span className="hidden sm:inline text-zinc-700">·</span>
          <span className="font-mono text-zinc-400">
            {currentSubfoldersCount} folders, {currentDirFilesCount} files
          </span>
        </div>
      </div>
    </>
  );
};
