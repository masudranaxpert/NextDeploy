import React from 'react';
import {
  Archive,
  ChevronRight,
  Copy,
  Download,
  FileCode2,
  FileText,
  Folder,
  FolderOpen,
  KeyRound,
  Trash2,
} from 'lucide-react';
import { toast } from 'sonner';
import { ActionMenu } from '../../../../components/ui/Primitives';
import { formatFileSize, formatPermOctal } from '../utils';
import type { ComposeFileNode } from '../../../../types/nextdeploy';

interface FileManagerTableProps {
  appSlug: string;
  appId: string;
  currentDir: string;
  editingFilePath: string | null;
  mobileTreeOpen: boolean;
  setMobileTreeOpen: (open: boolean) => void;
  expandedDirs: Record<string, boolean>;
  setExpandedDirs: React.Dispatch<React.SetStateAction<Record<string, boolean>>>;
  allFolderPaths: string[];
  rootFolders: string[];
  treeIndex: { folders: Map<string, string[]>; files: Map<string, ComposeFileNode[]> };
  toggleFolderTreeExpand: (folder: string, e?: React.MouseEvent) => void;
  navigateToFolder: (folder: string, options?: { preserveTreeToggle?: boolean }) => void;
  setContextMenu: (ctx: any) => void;
  defaultRichFiles: ComposeFileNode[];
  currentSubfolders: string[];
  currentDirFiles: ComposeFileNode[];
  selectedEntries: string[];
  setSelectedEntries: React.Dispatch<React.SetStateAction<string[]>>;
  toggleSelectEntry: (key: string) => void;
  filePermissions: Record<string, string>;
  openChmodFor: (path: string, isDir?: boolean) => void;
  openCompressModalFor: (paths: string[]) => void;
  openMoveOrCopyModal: (paths: string[], mode?: 'move' | 'copy') => void;
  deleteEntries: (paths: string[]) => Promise<void>;
  openFileInEditor: (file: ComposeFileNode) => void;
  setExtractTargetFile: (file: ComposeFileNode) => void;
  setExtractDestDir: (dir: string) => void;
  setRenameTarget: (target: any) => void;
  setRenameInput: (val: string) => void;
  fileSearch: string;
  isArchiveFile: (filename: string) => boolean;
  renderFileIcon: (filename: string) => React.ReactNode;
}

export const FileManagerTable: React.FC<FileManagerTableProps> = ({
  appSlug,
  appId,
  currentDir,
  editingFilePath,
  mobileTreeOpen,
  setMobileTreeOpen,
  expandedDirs,
  setExpandedDirs,
  allFolderPaths,
  rootFolders,
  treeIndex,
  toggleFolderTreeExpand,
  navigateToFolder,
  setContextMenu,
  defaultRichFiles,
  currentSubfolders,
  currentDirFiles,
  selectedEntries,
  setSelectedEntries,
  toggleSelectEntry,
  filePermissions,
  openChmodFor,
  openCompressModalFor,
  openMoveOrCopyModal,
  deleteEntries,
  openFileInEditor,
  setExtractTargetFile,
  setExtractDestDir,
  setRenameTarget,
  setRenameInput,
  fileSearch,
  isArchiveFile,
  renderFileIcon,
}) => {
  const renderTreeFolder = (folder: string, depth: number): React.ReactNode => {
    const name = folder.split('/').pop() || folder;
    const isExpanded = Boolean(expandedDirs[folder]);
    const isCurrent = currentDir === folder && !editingFilePath;
    const childFolders = treeIndex.folders.get(folder) ?? [];
    const count = childFolders.length;
    const small = depth > 0;
    return (
      <div key={folder} className="space-y-0.5">
        <div
          onContextMenu={(e) => {
            e.preventDefault();
            e.stopPropagation();
            setContextMenu({
              x: e.clientX,
              y: e.clientY,
              targetPath: folder,
              targetName: name,
              isFolder: true,
            });
          }}
          className={`group flex items-center justify-between rounded-lg pr-2 py-1 font-mono text-xs transition-colors ${
            isCurrent
              ? 'bg-amber-500/15 text-amber-200 font-semibold ring-1 ring-amber-500/25'
              : 'text-zinc-300 hover:bg-white/[0.04] hover:text-zinc-100'
          }`}
        >
          <button
            type="button"
            onClick={(e) => toggleFolderTreeExpand(folder, e)}
            title={isExpanded ? 'Collapse folder' : 'Expand folder'}
            className="flex h-6 w-6 items-center justify-center rounded hover:bg-white/[0.08] text-zinc-400 hover:text-amber-300 shrink-0 cursor-pointer"
          >
            <ChevronRight
              className={`h-3.5 w-3.5 transition-transform duration-150 ${
                isExpanded ? 'rotate-90 text-amber-400' : 'text-zinc-500'
              }`}
            />
          </button>
          <button
            type="button"
            onClick={() => {
              if (isCurrent) {
                toggleFolderTreeExpand(folder);
              } else {
                setExpandedDirs((prev) => ({ ...prev, [folder]: true }));
                navigateToFolder(folder, { preserveTreeToggle: true });
                setMobileTreeOpen(false);
              }
            }}
            className="flex flex-1 items-center justify-between gap-1.5 text-left min-w-0 py-0.5 cursor-pointer"
          >
            <span className="flex items-center gap-1.5 min-w-0">
              {isCurrent || isExpanded ? (
                <FolderOpen
                  className={`${small ? 'h-3.5 w-3.5' : 'h-4 w-4'} text-amber-400 fill-amber-400/25 shrink-0`}
                />
              ) : (
                <Folder
                  className={`${small ? 'h-3.5 w-3.5' : 'h-4 w-4'} text-amber-400 fill-amber-400/20 shrink-0`}
                />
              )}
              <span className="truncate">{name}</span>
            </span>
            {count > 0 && (
              <span className="text-[10px] text-zinc-500 group-hover:text-zinc-400 shrink-0">
                {count}
              </span>
            )}
          </button>
        </div>
        {isExpanded && count > 0 && (
          <div className="ml-3 border-l border-white/[0.07] pl-2 space-y-0.5">
            {childFolders.map((sub) => renderTreeFolder(sub, depth + 1))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="grid grid-cols-1 lg:grid-cols-12 min-h-[480px]">
      {/* LEFT PANE: Interactive Folder Tree */}
      <div
        className={`${
          mobileTreeOpen ? 'flex' : 'hidden lg:flex'
        } lg:col-span-3 border-b lg:border-b-0 lg:border-r border-white/[0.07] bg-[#0d0d10] p-2.5 flex-col justify-between`}
      >
        <div className="space-y-1">
          <div className="flex items-center justify-between px-2 pb-1">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
              Directories
            </span>
            <button
              type="button"
              onClick={() => {
                const anyOpen = Object.values(expandedDirs).some(Boolean);
                if (anyOpen) {
                  setExpandedDirs({});
                } else {
                  const allOpen: Record<string, boolean> = {};
                  const limitDepth = allFolderPaths.length > 400;
                  allFolderPaths.forEach((d) => {
                    if (!limitDepth || d.split('/').length <= 2) allOpen[d] = true;
                  });
                  setExpandedDirs(allOpen);
                }
              }}
              className="text-[10px] font-mono text-zinc-500 hover:text-zinc-300 cursor-pointer"
            >
              {Object.values(expandedDirs).some(Boolean) ? 'Collapse All' : 'Expand All'}
            </button>
          </div>

          <button
            type="button"
            onClick={() => {
              navigateToFolder('');
              setMobileTreeOpen(false);
            }}
            onContextMenu={(e) => {
              e.preventDefault();
              e.stopPropagation();
              setContextMenu({
                x: e.clientX,
                y: e.clientY,
                targetPath: '',
                targetName: appSlug,
                isFolder: true,
              });
            }}
            className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left font-mono text-xs transition-colors cursor-pointer ${
              currentDir === '' && !editingFilePath
                ? 'bg-amber-500/15 text-amber-300 font-semibold ring-1 ring-amber-500/25'
                : 'text-zinc-200 hover:bg-white/[0.04]'
            }`}
          >
            <FolderOpen className="h-4 w-4 text-amber-400 fill-amber-400/25 shrink-0" />
            <span className="truncate">/{appSlug}</span>
          </button>

          <div className="pl-1.5 pt-0.5 space-y-0.5">
            {rootFolders.map((folder) => renderTreeFolder(folder, 0))}
          </div>
        </div>

        <div className="mt-4 border-t border-white/[0.06] pt-2.5 px-2 font-mono text-[11px] text-zinc-500 flex items-center justify-between">
          <span>Total Files</span>
          <span className="text-zinc-300">{defaultRichFiles.length} items</span>
        </div>
      </div>

      {/* RIGHT PANE: Directory Listing Table */}
      <div
        className="lg:col-span-9 flex flex-col justify-between bg-[#111115] min-w-0"
        onContextMenu={(e) => {
          if ((e.target as HTMLElement).closest('tr')) return;
          e.preventDefault();
          setContextMenu({
            x: e.clientX,
            y: e.clientY,
            targetPath: currentDir,
            targetName: currentDir ? (currentDir.split('/').pop() || currentDir) : appSlug,
            isFolder: true,
          });
        }}
      >
        <div className="overflow-x-auto">
          <table className="w-full min-w-[600px] table-fixed text-left text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-[#0b0b0e] text-zinc-400">
                <th className="w-10 px-3.5 py-2.5">
                  <input
                    type="checkbox"
                    aria-label="Select all files"
                    checked={
                      currentSubfolders.length + currentDirFiles.length > 0 &&
                      selectedEntries.length ===
                        currentSubfolders.length + currentDirFiles.length
                    }
                    onChange={(e) => {
                      if (e.target.checked) {
                        setSelectedEntries([
                          ...currentSubfolders.map((d) => `dir:${d}`),
                          ...currentDirFiles.map((f) => f.path),
                        ]);
                      } else {
                        setSelectedEntries([]);
                      }
                    }}
                    className="h-3.5 w-3.5 rounded border-white/20 bg-zinc-900 accent-orange-500 cursor-pointer"
                  />
                </th>
                <th className="w-[42%] px-2 py-2.5 font-medium">Name</th>
                <th className="w-[14%] px-3 py-2.5 font-medium">Size</th>
                <th className="w-[16%] px-3 py-2.5 font-medium">Modified</th>
                <th className="w-[18%] px-3 py-2.5 font-medium">Permissions</th>
                <th className="w-12 px-3 py-2.5 text-right font-medium"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05] font-mono">
              {/* Parent Directory (..) Row */}
              {currentDir !== '' && (
                <tr
                  onClick={() => {
                    const parts = currentDir.split('/').filter(Boolean);
                    parts.pop();
                    navigateToFolder(parts.join('/'));
                  }}
                  className="hover:bg-white/[0.03] transition-colors cursor-pointer"
                >
                  <td className="px-3.5 py-2.5"></td>
                  <td className="px-2 py-2.5">
                    <div className="flex items-center gap-2.5 text-zinc-300 hover:text-amber-300">
                      <FolderOpen className="h-4 w-4 text-amber-400/80 fill-amber-400/15 shrink-0" />
                      <span className="font-semibold">..</span>
                      <span className="font-sans text-[11px] text-zinc-500">
                        Up One Level
                      </span>
                    </div>
                  </td>
                  <td className="px-3 py-2.5 text-zinc-600">—</td>
                  <td className="px-3 py-2.5 text-zinc-600">—</td>
                  <td className="px-3 py-2.5 text-[11px] text-zinc-500">755</td>
                  <td className="px-3 py-2.5"></td>
                </tr>
              )}

              {/* Folder Rows */}
              {currentSubfolders.map((dirPath) => {
                const folderName = fileSearch.trim()
                  ? dirPath
                  : dirPath.split('/').pop() || dirPath;
                const entryKey = `dir:${dirPath}`;
                const isChecked = selectedEntries.includes(entryKey);

                return (
                  <tr
                    key={entryKey}
                    onClick={() => navigateToFolder(dirPath)}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setContextMenu({
                        x: e.clientX,
                        y: e.clientY,
                        targetPath: dirPath,
                        targetName: folderName,
                        isFolder: true,
                      });
                    }}
                    className={`group transition-colors cursor-pointer ${
                      isChecked ? 'bg-amber-500/[0.07]' : 'hover:bg-white/[0.03]'
                    }`}
                  >
                    <td
                      className="px-3.5 py-2.5"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <input
                        type="checkbox"
                        aria-label={`Select ${entryKey}`}
                        checked={isChecked}
                        onChange={() => toggleSelectEntry(entryKey)}
                        className="h-3.5 w-3.5 rounded border-white/20 bg-zinc-900 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-2 py-2.5">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <Folder className="h-4 w-4 text-amber-400 fill-amber-400/25 shrink-0" />
                        <span className="truncate font-semibold text-zinc-100 group-hover:text-amber-300">
                          {folderName}
                        </span>
                      </div>
                    </td>
                    <td className="px-3 py-2.5 text-[11px] text-zinc-500">Directory</td>
                    <td className="px-3 py-2.5 font-sans text-[11px] text-zinc-400">
                      2d ago
                    </td>
                    <td
                      className="px-3 py-2.5 text-[11px] text-zinc-400"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <button
                        type="button"
                        onClick={() => openChmodFor(dirPath, true)}
                        className="font-mono hover:text-emerald-400 hover:underline cursor-pointer"
                      >
                        {formatPermOctal(filePermissions[dirPath], true)}
                      </button>
                    </td>
                    <td
                      className="px-3 py-2.5 text-right"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <ActionMenu
                        items={[
                          {
                            label: 'Open Folder',
                            icon: <FolderOpen className="h-3.5 w-3.5" />,
                            onClick: () => navigateToFolder(dirPath),
                          },
                          {
                            label: 'Compress to .zip / .tar.gz',
                            icon: <Archive className="h-3.5 w-3.5" />,
                            onClick: () => openCompressModalFor([dirPath]),
                          },
                          {
                            label: 'Rename Folder',
                            icon: <FileCode2 className="h-3.5 w-3.5" />,
                            onClick: () => {
                              setRenameTarget({
                                path: dirPath,
                                name: folderName,
                                isFolder: true,
                              });
                              setRenameInput(folderName);
                            },
                          },
                          {
                            label: 'Copy / Move to...',
                            icon: <Copy className="h-3.5 w-3.5" />,
                            onClick: () => openMoveOrCopyModal([dirPath]),
                          },
                          {
                            label: 'Permissions (chmod)',
                            icon: <KeyRound className="h-3.5 w-3.5" />,
                            onClick: () => openChmodFor(dirPath),
                          },
                          {
                            label: 'Delete Folder',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger',
                            onClick: () => {
                              void deleteEntries([dirPath]);
                            },
                          },
                        ]}
                      />
                    </td>
                  </tr>
                );
              })}

              {/* File Rows */}
              {currentDirFiles.map((file) => {
                const displayName = fileSearch.trim() ? file.path : file.name;
                const isChecked = selectedEntries.includes(file.path);
                const isArchive = isArchiveFile(file.name);

                return (
                  <tr
                    key={file.path}
                    onClick={() => openFileInEditor(file)}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setContextMenu({
                        x: e.clientX,
                        y: e.clientY,
                        targetPath: file.path,
                        targetName: file.name,
                        isFolder: false,
                      });
                    }}
                    className={`group transition-colors cursor-pointer ${
                      isChecked ? 'bg-orange-500/[0.07]' : 'hover:bg-white/[0.03]'
                    }`}
                  >
                    <td
                      className="px-3.5 py-2.5"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <input
                        type="checkbox"
                        checked={isChecked}
                        onChange={() => toggleSelectEntry(file.path)}
                        className="h-3.5 w-3.5 rounded border-white/20 bg-zinc-900 accent-orange-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-2 py-2.5">
                      <div className="flex items-center gap-2.5 min-w-0">
                        {renderFileIcon(file.name)}
                        <span className="truncate font-semibold text-zinc-100 group-hover:text-orange-400">
                          {displayName}
                        </span>
                        {isArchive && (
                          <span className="rounded bg-purple-500/15 px-1.5 py-0.2 font-mono text-[9px] font-semibold uppercase text-purple-300 ring-1 ring-purple-500/30 shrink-0">
                            ARCHIVE
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-3 py-2.5 text-[11px] text-zinc-300 tabular-nums">
                      {formatFileSize(file.sizeBytes)}
                    </td>
                    <td className="px-3 py-2.5 font-sans text-[11px] text-zinc-400">
                      {file.updatedAt}
                    </td>
                    <td
                      className="px-3 py-2.5 text-[11px] text-emerald-400"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <button
                        type="button"
                        onClick={() => openChmodFor(file.path, false)}
                        className="font-mono hover:underline cursor-pointer"
                      >
                        {formatPermOctal(filePermissions[file.path], false)}
                      </button>
                    </td>
                    <td
                      className="px-3 py-2.5 text-right"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <ActionMenu
                        items={[
                          ...(isArchive
                            ? [
                                {
                                  label: 'Extract Archive (Unzip)',
                                  icon: <FolderOpen className="h-3.5 w-3.5" />,
                                  onClick: () => {
                                    setExtractTargetFile(file);
                                    setExtractDestDir(currentDir || '');
                                  },
                                },
                              ]
                            : [
                                {
                                  label: 'Edit File',
                                  icon: <FileCode2 className="h-3.5 w-3.5" />,
                                  onClick: () => openFileInEditor(file),
                                },
                              ]),
                          {
                            label: 'Rename',
                            icon: <FileText className="h-3.5 w-3.5" />,
                            onClick: () => {
                              setRenameTarget({
                                path: file.path,
                                name: file.name,
                                isFolder: false,
                              });
                              setRenameInput(file.name);
                            },
                          },
                          {
                            label: 'Copy / Move to...',
                            icon: <Copy className="h-3.5 w-3.5" />,
                            onClick: () => openMoveOrCopyModal([file.path]),
                          },
                          {
                            label: 'Compress (.zip / .tar.gz)',
                            icon: <Archive className="h-3.5 w-3.5" />,
                            onClick: () => openCompressModalFor([file.path]),
                          },
                          {
                            label: 'Permissions (chmod)',
                            icon: <KeyRound className="h-3.5 w-3.5" />,
                            onClick: () => openChmodFor(file.path),
                          },
                          {
                            label: 'Download',
                            icon: <Download className="h-3.5 w-3.5" />,
                            onClick: () => {
                              const a = document.createElement('a');
                              a.href = `/apps/${encodeURIComponent(
                                appId
                              )}/file?path=${encodeURIComponent(file.path)}&download=1`;
                              a.download = file.name;
                              document.body.appendChild(a);
                              a.click();
                              document.body.removeChild(a);
                              toast.success(`Downloading ${file.name}`);
                            },
                          },
                          {
                            label: 'Delete',
                            icon: <Trash2 className="h-3.5 w-3.5" />,
                            variant: 'danger',
                            onClick: () => {
                              void deleteEntries([file.path]);
                            },
                          },
                        ]}
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
