import React from 'react';
import {
  Archive,
  ArrowLeft,
  ArrowLeftRight,
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
  mobileTreeOpen,
  setMobileTreeOpen,
  currentSubfoldersCount,
  currentDirFilesCount,
  renderFileIcon,
}) => {
  return (
    <>
      {/* 1. TOP CPANEL-STYLE FILE MANAGER ACTION TOOLBAR */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#0c0c0f] px-3.5 py-2">
        <div className="flex flex-wrap items-center gap-1.5">
          <button
            type="button"
            onClick={onOpenNewFile}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg bg-zinc-100 px-2.5 text-[11px] font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <FilePlus className="h-3.5 w-3.5" />
            <span>New File</span>
          </button>

          <button
            type="button"
            onClick={onOpenNewFolder}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <FolderPlus className="h-3.5 w-3.5 text-amber-400" />
            <span>New Folder</span>
          </button>

          <button
            type="button"
            onClick={onOpenUpload}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Upload className="h-3.5 w-3.5 text-sky-400" />
            <span>Upload</span>
          </button>

          <div className="mx-0.5 h-4 w-px bg-white/[0.07]" />

          {/* Compress (Zip / Tar.gz) Button */}
          <button
            type="button"
            onClick={onOpenCompress}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
            title="Compress files or folders into .zip or .tar.gz"
          >
            <Archive className="h-3.5 w-3.5 text-purple-400" />
            <span>Compress</span>
          </button>

          {/* Extract (Unzip) Button */}
          <button
            type="button"
            onClick={onOpenExtract}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 text-[11px] font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
            title="Extract .zip or .tar.gz archive"
          >
            <FolderOpen className="h-3.5 w-3.5 text-emerald-400" />
            <span>Extract</span>
          </button>

          <div className="mx-0.5 h-4 w-px bg-white/[0.07]" />

          <a
            href={`/api/v1/apps/${encodeURIComponent(appId)}/workspace/archive`}
            download={`${appSlug}-workspace.tar.gz`}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-2.5 text-[11px] font-medium text-emerald-300 hover:bg-emerald-500/20 cursor-pointer transition-colors"
            title="Download full workspace backup (.tar.gz archive)"
          >
            <Download className="h-3.5 w-3.5" />
            <span>Export Workspace</span>
          </a>

          <button
            type="button"
            onClick={onOpenRestoreArchive}
            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 text-[11px] font-medium text-amber-300 hover:bg-amber-500/20 cursor-pointer transition-colors"
            title="Restore full workspace from a .tar.gz or .zip archive"
          >
            <RotateCcw className="h-3.5 w-3.5" />
            <span>Restore Archive</span>
          </button>

          {/* Bulk Move, Copy & Delete when items are checked */}
          {selectedEntries.length > 0 && (
            <>
              <div className="mx-0.5 h-4 w-px bg-white/[0.07]" />
              <button
                type="button"
                onClick={onMoveSelected}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-orange-500/30 bg-orange-500/10 px-2.5 text-[11px] font-medium text-orange-300 hover:bg-orange-500/20 cursor-pointer"
                title="Move selected files/folders"
              >
                <ArrowLeftRight className="h-3.5 w-3.5" />
                <span>Move ({selectedEntries.length})</span>
              </button>
              <button
                type="button"
                onClick={onCopySelected}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-sky-500/30 bg-sky-500/10 px-2.5 text-[11px] font-medium text-sky-300 hover:bg-sky-500/20 cursor-pointer"
                title="Copy selected files/folders"
              >
                <Copy className="h-3.5 w-3.5" />
                <span>Copy ({selectedEntries.length})</span>
              </button>
              <button
                type="button"
                onClick={onDeleteSelected}
                className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-rose-500/30 bg-rose-500/10 px-2.5 text-[11px] font-medium text-rose-300 hover:bg-rose-500/20 cursor-pointer"
              >
                <Trash2 className="h-3.5 w-3.5" />
                <span>Delete ({selectedEntries.length})</span>
              </button>
            </>
          )}
        </div>

        {/* Right: Search Filter */}
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2 h-3.5 w-3.5 text-zinc-500" />
            <input
              type="text"
              value={fileSearch}
              onChange={(e) => {
                setFileSearch(e.target.value);
                if (e.target.value.trim()) setEditingFilePath(null);
              }}
              placeholder="Search files..."
              className="h-7 w-44 rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-2.5 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none"
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
