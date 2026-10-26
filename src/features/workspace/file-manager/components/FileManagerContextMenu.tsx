import React from 'react';
import {
  Archive,
  Copy,
  Download,
  FileCode2,
  FilePlus,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  KeyRound,
  RefreshCw,
  Trash2,
  Upload,
} from 'lucide-react';
import { toast } from 'sonner';
import { copyToClipboard } from '../../../../lib/clipboard';

export interface FileManagerContextMenuState {
  x: number;
  y: number;
  targetPath: string;
  targetName: string;
  isFolder: boolean;
}

interface FileManagerContextMenuProps {
  contextMenu: FileManagerContextMenuState | null;
  appSlug: string;
  appId: string;
  onClose: () => void;
  onNavigateToFolder: (path: string) => void;
  onOpenNewFileInFolder: (path: string) => void;
  onOpenNewFolderInFolder: (path: string) => void;
  onOpenUploadInFolder: (path: string) => void;
  onOpenFileInEditor: (path: string) => void;
  onExtractArchive: (path: string) => void;
  onCompress: (path: string) => void;
  onRename: (path: string, name: string, isFolder: boolean) => void;
  onMoveOrCopy: (path: string) => void;
  onChmod: (path: string, isFolder: boolean) => void;
  onDelete: (path: string) => void;
  isArchiveFile: (filename: string) => boolean;
  renderFileIcon: (filename: string) => React.ReactNode;
}

export const FileManagerContextMenu: React.FC<FileManagerContextMenuProps> = ({
  contextMenu,
  appSlug,
  appId,
  onClose,
  onNavigateToFolder,
  onOpenNewFileInFolder,
  onOpenNewFolderInFolder,
  onOpenUploadInFolder,
  onOpenFileInEditor,
  onExtractArchive,
  onCompress,
  onRename,
  onMoveOrCopy,
  onChmod,
  onDelete,
  isArchiveFile,
  renderFileIcon,
}) => {
  if (!contextMenu) return null;

  return (
    <div
      className="fixed z-50 w-56 rounded-xl border border-white/[0.12] bg-[#141419]/98 p-1.5 shadow-2xl backdrop-blur-xl font-sans text-xs"
      style={{
        top:
          contextMenu.y + 440 > window.innerHeight
            ? Math.max(10, window.innerHeight - 450)
            : contextMenu.y,
        left: Math.min(contextMenu.x, window.innerWidth - 240),
      }}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="border-b border-white/[0.06] px-2.5 py-1.5 font-mono text-[11px] text-zinc-400 truncate flex items-center gap-2">
        {contextMenu.isFolder ? (
          <Folder className="h-3.5 w-3.5 text-amber-400 fill-amber-400/25 shrink-0" />
        ) : (
          renderFileIcon(contextMenu.targetName)
        )}
        <span className="truncate font-semibold text-zinc-200">
          {contextMenu.targetName}
        </span>
      </div>

      <div className="py-1 space-y-0.5">
        {contextMenu.isFolder ? (
          <>
            <button
              type="button"
              onClick={() => {
                onNavigateToFolder(contextMenu.targetPath);
                onClose();
              }}
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
            >
              <FolderOpen className="h-3.5 w-3.5 text-amber-400" />
              <span>Open Folder</span>
            </button>

            <button
              type="button"
              onClick={() => {
                onOpenNewFileInFolder(contextMenu.targetPath);
                onClose();
              }}
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
            >
              <FilePlus className="h-3.5 w-3.5 text-orange-400" />
              <span>New File in this Folder</span>
            </button>

            <button
              type="button"
              onClick={() => {
                onOpenNewFolderInFolder(contextMenu.targetPath);
                onClose();
              }}
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
            >
              <FolderPlus className="h-3.5 w-3.5 text-amber-400" />
              <span>New Subfolder</span>
            </button>

            <button
              type="button"
              onClick={() => {
                onOpenUploadInFolder(contextMenu.targetPath);
                onClose();
              }}
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
            >
              <Upload className="h-3.5 w-3.5 text-sky-400" />
              <span>Upload Files Here</span>
            </button>

            <button
              type="button"
              onClick={() => {
                onClose();
                toast.success(
                  `Reloaded /data/apps/${appSlug}${
                    contextMenu.targetPath ? `/${contextMenu.targetPath}` : ''
                  }`
                );
              }}
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
            >
              <RefreshCw className="h-3.5 w-3.5 text-teal-400" />
              <span>Reload / Refresh</span>
            </button>

            <div className="my-1 border-t border-white/[0.06]" />
          </>
        ) : isArchiveFile(contextMenu.targetName) ? (
          <button
            type="button"
            onClick={() => {
              onExtractArchive(contextMenu.targetPath);
              onClose();
            }}
            className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left font-medium text-emerald-300 hover:bg-emerald-500/15 cursor-pointer"
          >
            <FolderOpen className="h-3.5 w-3.5 text-emerald-400" />
            <span>Extract Archive (Unzip)</span>
          </button>
        ) : (
          <button
            type="button"
            onClick={() => {
              onOpenFileInEditor(contextMenu.targetPath);
              onClose();
            }}
            className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
          >
            <FileCode2 className="h-3.5 w-3.5 text-orange-400" />
            <span>Edit in Code Editor</span>
          </button>
        )}

        <button
          type="button"
          onClick={() => {
            onCompress(contextMenu.targetPath);
            onClose();
          }}
          className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
        >
          <Archive className="h-3.5 w-3.5 text-purple-400" />
          <span>Compress (.zip / .tar.gz)</span>
        </button>

        <button
          type="button"
          onClick={() => {
            onRename(contextMenu.targetPath, contextMenu.targetName, contextMenu.isFolder);
            onClose();
          }}
          className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
        >
          <FileText className="h-3.5 w-3.5 text-zinc-400" />
          <span>Rename</span>
        </button>

        <button
          type="button"
          onClick={() => {
            onMoveOrCopy(contextMenu.targetPath);
            onClose();
          }}
          className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
        >
          <Copy className="h-3.5 w-3.5 text-sky-400" />
          <span>Copy / Move to...</span>
        </button>

        <button
          type="button"
          onClick={() => {
            onChmod(contextMenu.targetPath, contextMenu.isFolder);
            onClose();
          }}
          className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
        >
          <KeyRound className="h-3.5 w-3.5 text-emerald-400" />
          <span>Permissions (chmod)</span>
        </button>

        <button
          type="button"
          onClick={() => {
            void copyToClipboard(`/data/apps/${appSlug}/${contextMenu.targetPath}`);
            toast.success(`Copied path /data/apps/${appSlug}/${contextMenu.targetPath}`);
            onClose();
          }}
          className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
        >
          <Copy className="h-3.5 w-3.5 text-zinc-400" />
          <span>Copy Path</span>
        </button>

        {!contextMenu.isFolder && (
          <button
            type="button"
            onClick={() => {
              const a = document.createElement('a');
              a.href = `/apps/${encodeURIComponent(appId)}/file?path=${encodeURIComponent(
                contextMenu.targetPath
              )}&download=1`;
              a.download = contextMenu.targetName;
              document.body.appendChild(a);
              a.click();
              document.body.removeChild(a);
              toast.success(`Downloading ${contextMenu.targetName}`);
              onClose();
            }}
            className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-zinc-200 hover:bg-white/[0.06] cursor-pointer"
          >
            <Download className="h-3.5 w-3.5 text-zinc-400" />
            <span>Download</span>
          </button>
        )}

        <div className="my-1 border-t border-white/[0.06]" />

        <button
          type="button"
          onClick={() => {
            onDelete(contextMenu.targetPath);
            onClose();
          }}
          className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-rose-400 hover:bg-rose-500/15 cursor-pointer"
        >
          <Trash2 className="h-3.5 w-3.5" />
          <span>Delete</span>
        </button>
      </div>
    </div>
  );
};
