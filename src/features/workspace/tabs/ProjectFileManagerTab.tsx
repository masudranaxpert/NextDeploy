import React, { useState, useMemo, useEffect, useRef, useCallback } from 'react';
import { toast } from 'sonner';
import { DevFileIcon } from '../../../components/ui/CodeEditorBox';
import { backendApi } from '../../../lib/backendApi';
import type { AppStack, ComposeFileNode } from '../../../types/nextdeploy';
import { Upload } from 'lucide-react';
import {
  FileManagerToolbar,
  FileManagerTable,
  FileManagerEditor,
  FileManagerContextMenu,
  FileManagerUploadToast,
  FileManagerContextMenuState,
} from '../file-manager/components';
import {
  CreateFileDialog,
  CreateFolderDialog,
  ChmodDialog,
  CompressDialog,
  ExtractDialog,
  RestoreArchiveDialog,
  RenameDialog,
  MoveCopyDialog,
  CollisionDialog,
  UploadFileDialog,
} from '../file-manager/dialogs';
import { formatPermOctal } from '../file-manager/utils';

export { formatPermOctal };

interface ProjectFileManagerTabProps {
  app: AppStack;
  onUpdateApp: (updated: AppStack) => void;
  onRefresh?: () => void | Promise<void>;
}

export const ProjectFileManagerTab: React.FC<ProjectFileManagerTabProps> = ({
  app,
  onUpdateApp,
}) => {
  const [deletedFilePaths, setDeletedFilePaths] = useState<string[]>([]);
  const [customFolders, setCustomFolders] = useState<string[]>([]);
  const [renamedPaths, setRenamedPaths] = useState<Record<string, string>>({});
  const [isDragOverFileManager, setIsDragOverFileManager] = useState(false);
  const [liveFileList, setLiveFileList] = useState<ComposeFileNode[] | null>(null);

  // Sync real files from server disk into frontend state
  const syncWorkspaceFilesFromDisk = async (invalidatedPaths?: string[]) => {
    try {
      const listRes = await backendApi.filesList(app.id, '', true);
      const res = listRes.data;
      if (Array.isArray(res)) {
        const invalidatedSet = new Set(invalidatedPaths || []);
        const contentByPath = new Map<string, string>();
        (app.files || []).forEach((f) => {
          if (f.path && f.content && !invalidatedSet.has(f.path)) contentByPath.set(f.path, f.content);
        });
        (liveFileList || []).forEach((f) => {
          if (f.path && f.content && !invalidatedSet.has(f.path)) contentByPath.set(f.path, f.content);
        });

        const liveNodes: ComposeFileNode[] = res.map((item: any) => {
          const p = item.rel_path || item.path || item.name;
          const isDir = Boolean(item.is_dir || item.type === 'directory' || item.type === 'dir');
          const ext = p.split('.').pop()?.toLowerCase() || '';
          const lang =
            ext === 'ts' || ext === 'tsx'
              ? 'typescript'
              : ext === 'js' || ext === 'jsx'
              ? 'javascript'
              : ext === 'json'
              ? 'json'
              : ext === 'yml' || ext === 'yaml'
              ? 'yaml'
              : ext === 'md'
              ? 'markdown'
              : ext === 'sh'
              ? 'sh'
              : ext === 'env'
              ? 'env'
              : ext === 'conf'
              ? 'nginx'
              : 'dockerfile';

          return {
            path: p,
            name: item.name || p.split('/').pop() || p,
            type: (isDir ? 'dir' : 'file') as ComposeFileNode['type'],
            sizeBytes: item.size,
            updatedAt:
              item.modified ||
              (item.mod_time ? new Date(item.mod_time * 1000).toLocaleString() : 'Just now'),
            language: lang as ComposeFileNode['language'],
            content: contentByPath.get(p) || '',
          };
        });

        const perms: Record<string, string> = {};
        res.forEach((item: any) => {
          const p = item.rel_path || item.path || item.name;
          const perm = item.perms || item.permissions;
          if (p && perm) {
            perms[p] = perm;
          }
        });
        setFilePermissions((prev) => ({ ...prev, ...perms }));
        setLiveFileList(liveNodes);

        if (editingFilePath && invalidatedSet.has(editingFilePath)) {
          const freshNode = liveNodes.find((f) => f.path === editingFilePath && f.type !== 'dir');
          if (freshNode) {
            try {
              const fetchRes = await backendApi.fileBlob(app.id, editingFilePath);
              if (fetchRes.ok && typeof fetchRes.data?.text === 'string') {
                freshNode.content = fetchRes.data.text;
                setLiveFileList([...liveNodes]);
              }
            } catch {
              // ignore
            }
          }
        }
      }
    } catch {
      // ignore disk sync failure
    }
  };

  useEffect(() => {
    void syncWorkspaceFilesFromDisk();
  }, [app.id]);

  const defaultRichFiles: ComposeFileNode[] = useMemo(() => {
    const sourceFiles = liveFileList || app.files;
    return sourceFiles
      .map((f) => {
        const mappedPath = renamedPaths[f.path] || f.path;
        return {
          ...f,
          path: mappedPath,
          name: mappedPath.split('/').pop() || mappedPath,
        };
      })
      .filter((f) => !deletedFilePaths.includes(f.path));
  }, [liveFileList, app.files, app.primaryDomain, app.services, app.slug, deletedFilePaths, renamedPaths]);

  const [editingFilePath, setEditingFilePath] = useState<string | null>(null);
  const [isFileLoading, setIsFileLoading] = useState(false);
  const inFlightFetchesRef = useRef<Map<string, Promise<string | null>>>(new Map());
  const loadedPathsRef = useRef<Set<string>>(new Set());
  const [currentDir, setCurrentDir] = useState<string>('');
  const [openEditorTabs, setOpenEditorTabs] = useState<string[]>([]);
  const [editorSaveTick, setEditorSaveTick] = useState<number>(0);
  const [selectedEntries, setSelectedEntries] = useState<string[]>([]);
  const [fileSearch, setFileSearch] = useState('');
  const [mobileTreeOpen, setMobileTreeOpen] = useState(false);
  const [expandedDirs, setExpandedDirs] = useState<Record<string, boolean>>({ src: true, config: true });

  const [filePermissions, setFilePermissions] = useState<Record<string, string>>({
    'docker-compose.yml': '-rw-r--r--',
    Dockerfile: '-rw-r--r--',
    'src/server.ts': '-rw-r--r--',
    'src/index.html': '-rw-r--r--',
    '.env': '-rw-------',
  });

  const [contextMenu, setContextMenu] = useState<FileManagerContextMenuState | null>(null);

  // Modals state
  const [newFileModalOpen, setNewFileModalOpen] = useState(false);
  const [newFileName, setNewFileName] = useState('');
  const [newFileTargetDir, setNewFileTargetDir] = useState('');
  const [newFolderModalOpen, setNewFolderModalOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [newFolderTargetDir, setNewFolderTargetDir] = useState('');
  const [chmodModalFile, setChmodModalFile] = useState<string | null>(null);
  const [renameTarget, setRenameTarget] = useState<{ path: string; name: string; isFolder: boolean } | null>(null);
  const [renameInput, setRenameInput] = useState('');
  const [moveTarget, setMoveTarget] = useState<{ paths: string[]; mode: 'move' | 'copy' } | null>(null);
  const [moveDestInput, setMoveDestInput] = useState('');
  const [conflictModal, setConflictModal] = useState<{
    paths: string[];
    destFolder: string;
    mode: 'move' | 'copy';
    conflicts: string[];
  } | null>(null);
  const [compressModalOpen, setCompressModalOpen] = useState(false);
  const [compressTargetPaths, setCompressTargetPaths] = useState<string[]>([]);
  const [compressFormat, setCompressFormat] = useState<'zip' | 'tar.gz'>('zip');
  const [compressArchiveName, setCompressArchiveName] = useState('archive.zip');
  const [extractTargetFile, setExtractTargetFile] = useState<ComposeFileNode | null>(null);
  const [extractDestDir, setExtractDestDir] = useState('');
  const [restoreArchiveModalOpen, setRestoreArchiveModalOpen] = useState(false);
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [uploadMode, setUploadMode] = useState<'local' | 'url'>('local');
  const [selectedUploadFiles, setSelectedUploadFiles] = useState<File[]>([]);
  const [uploadRemoteUrl, setUploadRemoteUrl] = useState('');
  const [isUploadingFiles, setIsUploadingFiles] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadFileName, setUploadFileName] = useState('');

  const isArchiveFile = (filename: string): boolean => {
    const lower = filename.toLowerCase();
    return (
      lower.endsWith('.zip') ||
      lower.endsWith('.tar.gz') ||
      lower.endsWith('.tgz') ||
      lower.endsWith('.tar') ||
      lower.endsWith('.gz')
    );
  };

  const selectedFile = useMemo(() => {
    return (
      defaultRichFiles.find((f) => f.path === editingFilePath && f.type !== 'dir') ||
      defaultRichFiles.find((f) => f.type !== 'dir') || {
        path: 'docker-compose.yml',
        name: 'docker-compose.yml',
        type: 'file',
        sizeBytes: 1024,
        updatedAt: 'Just now',
        language: 'yaml',
        content: '',
      }
    );
  }, [defaultRichFiles, editingFilePath]);

  const allFolderPaths: string[] = useMemo(() => {
    const folderSet = new Set<string>();
    customFolders
      .filter((d) => !deletedFilePaths.includes(`dir:${d}`))
      .forEach((d) => folderSet.add(d));

    defaultRichFiles.forEach((f) => {
      if (f.type === 'dir') {
        if (!deletedFilePaths.includes(`dir:${f.path}`)) {
          folderSet.add(f.path);
        }
      }
      const parts = f.path.split('/');
      parts.pop();
      let acc = '';
      for (const p of parts) {
        acc = acc ? `${acc}/${p}` : p;
        if (!deletedFilePaths.includes(`dir:${acc}`)) {
          folderSet.add(acc);
        }
      }
    });
    return Array.from(folderSet).sort();
  }, [customFolders, defaultRichFiles, deletedFilePaths]);

  const rootFolders = useMemo(() => {
    return allFolderPaths.filter((d) => !d.includes('/'));
  }, [allFolderPaths]);

  const treeIndex = useMemo(() => {
    const folders = new Map<string, string[]>();
    const files = new Map<string, ComposeFileNode[]>();
    for (const d of allFolderPaths) {
      folders.set(d, []);
      files.set(d, []);
    }
    for (const d of allFolderPaths) {
      const slash = d.lastIndexOf('/');
      if (slash > 0) {
        const parent = d.slice(0, slash);
        folders.get(parent)?.push(d);
      }
    }
    for (const f of defaultRichFiles) {
      const slash = f.path.lastIndexOf('/');
      const parent = slash > 0 ? f.path.slice(0, slash) : '';
      if (parent && files.has(parent)) {
        files.get(parent)?.push(f);
      }
    }
    return { folders, files };
  }, [allFolderPaths, defaultRichFiles]);

  const toggleFolderTreeExpand = (folder: string, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    setExpandedDirs((prev) => ({ ...prev, [folder]: !prev[folder] }));
  };

  const currentSubfolders: string[] = useMemo(() => {
    if (fileSearch.trim()) {
      const q = fileSearch.toLowerCase().trim();
      return allFolderPaths.filter((d) => d.toLowerCase().includes(q));
    }
    if (!currentDir) {
      return rootFolders;
    }
    return allFolderPaths.filter((d) => {
      if (!d.startsWith(`${currentDir}/`)) return false;
      const sub = d.slice(currentDir.length + 1);
      return !sub.includes('/');
    });
  }, [allFolderPaths, currentDir, fileSearch, rootFolders]);

  const currentDirFiles: ComposeFileNode[] = useMemo(() => {
    if (fileSearch.trim()) {
      const q = fileSearch.toLowerCase().trim();
      return defaultRichFiles.filter(
        (f) =>
          f.type !== 'dir' &&
          (f.path.toLowerCase().includes(q) ||
            f.name.toLowerCase().includes(q) ||
            f.content.toLowerCase().includes(q))
      );
    }
    return defaultRichFiles.filter((f) => {
      if (f.type === 'dir') return false;
      const slash = f.path.lastIndexOf('/');
      if (currentDir === '') {
        return slash === -1;
      }
      return slash === currentDir.length && f.path.startsWith(`${currentDir}/`);
    });
  }, [defaultRichFiles, currentDir, fileSearch]);

  const navigateToFolder = (folder: string, options?: { preserveTreeToggle?: boolean }) => {
    setCurrentDir(folder);
    setSelectedEntries([]);
    if (!options?.preserveTreeToggle && folder) {
      setExpandedDirs((prev) => ({ ...prev, [folder]: true }));
    }
  };

  // High-performance deduplicated and cached file blob loader
  const fetchFileContent = useCallback(
    async (filePath: string): Promise<string | null> => {
      if (!filePath) return null;

      // 1. In-memory check (0ms response if already loaded)
      if (loadedPathsRef.current.has(filePath)) {
        const cached = (liveFileList || app.files).find((f) => f.path === filePath);
        if (cached && typeof cached.content === 'string') {
          return cached.content;
        }
      }

      // 2. In-flight check: reuse existing promise if another trigger (e.g. hover or click) is already fetching
      if (inFlightFetchesRef.current.has(filePath)) {
        return inFlightFetchesRef.current.get(filePath)!;
      }

      // 3. Initiate fetch from backend
      const fetchPromise = (async () => {
        try {
          const res = await backendApi.fileBlob(app.id, filePath);
          const text = res.data?.text ?? '';
          loadedPathsRef.current.add(filePath);

          setLiveFileList((prev) => {
            const list = prev ? [...prev] : [...app.files];
            const idx = list.findIndex((f) => f.path === filePath);
            if (idx >= 0) {
              list[idx] = { ...list[idx], content: text };
            } else {
              list.push({
                path: filePath,
                name: filePath.split('/').pop() || filePath,
                content: text,
                type: 'file',
                language: 'yaml',
                sizeBytes: new Blob([text]).size,
                updatedAt: 'Just now',
              });
            }
            return list;
          });

          onUpdateApp({
            ...app,
            files: app.files.some((f) => f.path === filePath)
              ? app.files.map((f) => (f.path === filePath ? { ...f, content: text } : f))
              : [
                  ...app.files,
                  {
                    path: filePath,
                    name: filePath.split('/').pop() || filePath,
                    content: text,
                    type: 'file',
                    language: 'yaml',
                    sizeBytes: new Blob([text]).size,
                    updatedAt: 'Just now',
                  },
                ],
          });

          return text;
        } catch {
          return null;
        } finally {
          inFlightFetchesRef.current.delete(filePath);
        }
      })();

      inFlightFetchesRef.current.set(filePath, fetchPromise);
      return fetchPromise;
    },
    [app, liveFileList, onUpdateApp]
  );

  const openFileInEditor = async (file: ComposeFileNode) => {
    if (file.type === 'dir') {
      navigateToFolder(file.path);
      return;
    }
    const filePath = file.path;
    setEditingFilePath(filePath);
    if (!openEditorTabs.includes(filePath)) {
      setOpenEditorTabs((prev) => [...prev, filePath]);
    }

    const isAlreadyLoaded = loadedPathsRef.current.has(filePath) || Boolean(file.content);
    if (!isAlreadyLoaded) {
      setIsFileLoading(true);
      await fetchFileContent(filePath);
      setIsFileLoading(false);
    }
  };

  const closeEditorTab = (tabPath: string, e: React.MouseEvent) => {
    e.stopPropagation();
    const idx = openEditorTabs.indexOf(tabPath);
    const next = openEditorTabs.filter((p) => p !== tabPath);
    setOpenEditorTabs(next);
    if (editingFilePath === tabPath) {
      if (next.length > 0) {
        const fallback = next[Math.max(0, idx - 1)];
        setEditingFilePath(fallback);
      } else {
        setEditingFilePath(null);
      }
    }
  };

  const toggleSelectEntry = (entryKey: string) => {
    setSelectedEntries((prev) =>
      prev.includes(entryKey) ? prev.filter((k) => k !== entryKey) : [...prev, entryKey]
    );
  };

  const openChmodFor = (targetPath: string) => {
    setChmodModalFile(targetPath);
  };

  const openCompressModalFor = (paths: string[]) => {
    const valid = paths.filter(Boolean);
    if (valid.length === 0) {
      toast.info('Please select at least one file or folder to compress');
      return;
    }
    setCompressTargetPaths(valid);
    const primary = valid[0].replace(/^dir:/, '').split('/').pop() || 'archive';
    setCompressArchiveName(`${primary}.${compressFormat}`);
    setCompressModalOpen(true);
  };

  const openMoveOrCopyModal = (paths: string[], mode: 'move' | 'copy' = 'move') => {
    const valid = paths.filter(Boolean).map((p) => p.replace(/^dir:/, ''));
    if (valid.length === 0) {
      toast.info('Please select at least one file or folder');
      return;
    }
    setMoveTarget({ paths: valid, mode });
    setMoveDestInput(currentDir || '');
  };

  // Create File Submit
  const handleCreateFileSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanName = newFileName.trim().replace(/^\/+/, '');
    if (!cleanName) return;

    const dir = newFileTargetDir ? newFileTargetDir.trim().replace(/^\/+|\/+$/g, '') : '';
    const fullPath = dir ? `${dir}/${cleanName}` : cleanName;

    const ext = fullPath.split('.').pop()?.toLowerCase() || '';
    const lang =
      ext === 'ts' || ext === 'tsx'
        ? 'typescript'
        : ext === 'js' || ext === 'jsx'
        ? 'javascript'
        : ext === 'json'
        ? 'json'
        : ext === 'yml' || ext === 'yaml'
        ? 'yaml'
        : ext === 'md'
        ? 'markdown'
        : ext === 'py'
        ? 'python'
        : ext === 'go'
        ? 'go'
        : ext === 'rs'
        ? 'rust'
        : ext === 'php'
        ? 'php'
        : ext === 'sql'
        ? 'sql'
        : ext === 'sh'
        ? 'sh'
        : 'dockerfile';

    let starterContent = `# ${fullPath}\n`;
    if (ext === 'py') starterContent = `#!/usr/bin/env python3\nimport os\n\ndef main():\n    pass\n`;
    else if (ext === 'php') starterContent = `<?php\ndeclare(strict_types=1);\n`;
    else if (ext === 'json') starterContent = `{\n  "name": "${app.slug}"\n}\n`;

    await backendApi.fileSave(app.id, fullPath, starterContent);
    await syncWorkspaceFilesFromDisk();

    const created: ComposeFileNode = {
      path: fullPath,
      name: fullPath.split('/').pop() || fullPath,
      type: 'file',
      sizeBytes: new Blob([starterContent]).size,
      updatedAt: 'Just now',
      language: lang as ComposeFileNode['language'],
      content: starterContent,
    };
    setLiveFileList((prev) =>
      prev ? [...prev.filter((f) => f.path !== fullPath), created] : [created]
    );
    setDeletedFilePaths((prev) => prev.filter((p) => p !== fullPath));
    setEditingFilePath(fullPath);
    setOpenEditorTabs((prev) => (prev.includes(fullPath) ? prev : [...prev, fullPath]));
    setNewFileName('');
    setNewFileModalOpen(false);
    toast.success(`Created ${fullPath}`);
  };

  // Create Folder Submit
  const handleCreateFolderSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const raw = newFolderName
      .trim()
      .replace(/^\/+|\/+$/g, '')
      .replace(/[^a-zA-Z0-9/_.-]/g, '-');
    if (!raw) return;

    const parent = newFolderTargetDir ? newFolderTargetDir.trim().replace(/^\/+|\/+$/g, '') : '';
    const fullPath = parent ? `${parent}/${raw}` : raw;

    setCustomFolders((prev) => (prev.includes(fullPath) ? prev : [...prev, fullPath]));
    setDeletedFilePaths((prev) => prev.filter((p) => p !== `dir:${fullPath}`));
    setNewFolderName('');
    setNewFolderModalOpen(false);
    setCurrentDir(fullPath);
    toast.success(`Created folder /${fullPath}`);
    await syncWorkspaceFilesFromDisk();
  };

  // Compress Submit
  const handleCompressSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const archiveName = compressArchiveName.trim();
    if (!archiveName) return;

    const sources = compressTargetPaths.map((p) => p.replace(/^dir:/, ''));
    toast.info(`Compressing ${sources.length} items into ${archiveName}...`);

    const destFolder = currentDir || '';
    const res = await backendApi.fileZip(app.id, sources, archiveName, destFolder);
    if (!res.ok) {
      toast.error(res.error || res.message || 'Compression failed');
      return;
    }
    toast.success(`Archive ${archiveName} created successfully`);
    setCompressModalOpen(false);
    await syncWorkspaceFilesFromDisk();
  };

  // Extract Submit
  const handleExtractSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!extractTargetFile) return;

    const archivePath = extractTargetFile.path;
    const destDir = extractDestDir.trim().replace(/^\/+|\/+$/g, '');
    toast.info(`Extracting ${archivePath} into /${destDir || 'root'}...`);

    const res = await backendApi.fileUnzip(app.id, archivePath, destDir);
    if (!res.ok) {
      toast.error(res.error || res.message || 'Extraction failed');
      return;
    }
    toast.success(`Extracted ${archivePath}`);
    setExtractTargetFile(null);
    await syncWorkspaceFilesFromDisk();
  };

  // Rename Submit
  const handleRenameSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!renameTarget) return;

    const cleanNewName = renameInput.trim().replace(/^\/+/, '');
    if (!cleanNewName || cleanNewName === renameTarget.name) {
      setRenameTarget(null);
      return;
    }

    const oldPath = renameTarget.path;
    const pathParts = oldPath.split('/');
    pathParts.pop();
    const newPath = pathParts.length > 0 ? `${pathParts.join('/')}/${cleanNewName}` : cleanNewName;

    const res = await backendApi.fileRename(app.id, oldPath, newPath);
    if (res.ok === false) {
      toast.error(res.error || res.message || 'Rename failed');
      return;
    }

    setRenamedPaths((prev) => ({ ...prev, [oldPath]: newPath }));
    if (editingFilePath === oldPath) setEditingFilePath(newPath);
    setOpenEditorTabs((prev) => prev.map((p) => (p === oldPath ? newPath : p)));
    setRenameTarget(null);
    toast.success(`Renamed to ${newPath}`);
    await syncWorkspaceFilesFromDisk();
  };

  // Move or Copy Execution
  const executeMoveOrCopy = async (
    paths: string[],
    destFolder: string,
    mode: 'move' | 'copy',
    replace: boolean
  ) => {
    const isMove = mode === 'move';
    toast.info(`${isMove ? 'Moving' : 'Copying'} ${paths.length} item(s)...`);

    for (const src of paths) {
      const fname = src.split('/').pop() || src;
      const targetDest = destFolder ? `${destFolder}/${fname}` : fname;
      const res = isMove
        ? await backendApi.fileMove(app.id, [src], destFolder, replace)
        : await backendApi.fileCopy(app.id, [src], destFolder, replace);
      if (!res.ok) {
        toast.error(res.error || res.message || `Failed to ${mode} ${src}`);
        return;
      }
    }

    toast.success(`Successfully ${isMove ? 'moved' : 'copied'} items`);
    setMoveTarget(null);
    await syncWorkspaceFilesFromDisk();
  };

  const handleMoveOrCopySubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!moveTarget) return;

    const destFolder = moveDestInput.trim().replace(/^\/+|\/+$/g, '');
    const conflicts: string[] = [];
    for (const src of moveTarget.paths) {
      const fname = src.split('/').pop() || src;
      const targetDest = destFolder ? `${destFolder}/${fname}` : fname;
      const exists = defaultRichFiles.some((f) => f.path === targetDest);
      if (exists) conflicts.push(src);
    }

    if (conflicts.length > 0) {
      setConflictModal({
        paths: moveTarget.paths,
        destFolder,
        mode: moveTarget.mode,
        conflicts,
      });
      return;
    }

    await executeMoveOrCopy(moveTarget.paths, destFolder, moveTarget.mode, false);
  };

  // Delete Entries
  const deleteEntries = async (entries: string[]) => {
    for (const entry of entries) {
      const isDir = entry.startsWith('dir:');
      const cleanPath = isDir ? entry.replace(/^dir:/, '') : entry;
      await backendApi.fileDelete(app.id, cleanPath);
      setDeletedFilePaths((prev) => (prev.includes(entry) ? prev : [...prev, entry]));
      if (editingFilePath === cleanPath) setEditingFilePath(null);
      setOpenEditorTabs((prev) => prev.filter((p) => p !== cleanPath));
    }
    toast.success(`Deleted ${entries.length} item(s)`);
    await syncWorkspaceFilesFromDisk();
  };

  // Run Local File Upload with Progress
  const runLocalUpload = async (files: File[]): Promise<boolean> => {
    setIsUploadingFiles(true);
    setUploadProgress(0);
    try {
      for (let i = 0; i < files.length; i++) {
        const file = files[i];
        setUploadFileName(file.name);
        const pct = Math.round(((i + 1) / files.length) * 100);
        setUploadProgress(pct);
        const res = await backendApi.fileUpload(app.id, file, currentDir || '');
        if (!res.ok) {
          toast.error(res.message || `Failed to upload ${file.name}`);
          return false;
        }
      }
      toast.success(`Uploaded ${files.length} file(s) into /${currentDir || 'root'}`);
      await syncWorkspaceFilesFromDisk();
      return true;
    } catch (err: any) {
      toast.error(err?.message || 'Upload failed');
      return false;
    } finally {
      setIsUploadingFiles(false);
      setUploadProgress(0);
      setUploadFileName('');
    }
  };

  const handleUploadSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (uploadMode === 'url') {
      const rawUrl = uploadRemoteUrl.trim();
      if (!rawUrl) return;
      setIsUploadingFiles(true);
      try {
        const res = await backendApi.fileUrlUpload(app.id, rawUrl, currentDir || '');
        setUploadRemoteUrl('');
        setUploadModalOpen(false);
        toast.success(`Fetched from ${rawUrl}`);
        await syncWorkspaceFilesFromDisk();
      } catch (err: any) {
        toast.error(err?.message || 'Failed to fetch remote URL');
      } finally {
        setIsUploadingFiles(false);
      }
      return;
    }

    if (selectedUploadFiles.length === 0) {
      toast.error('Please select at least one file to upload');
      return;
    }

    setUploadModalOpen(false);
    const ok = await runLocalUpload(selectedUploadFiles);
    if (ok) setSelectedUploadFiles([]);
    else setUploadModalOpen(true);
  };

  const handleSaveFileContent = (nextContent: string) => {
    selectedFile.content = nextContent;
    setLiveFileList((prev) => {
      if (!prev) return null;
      return prev.map((f) =>
        f.path === selectedFile.path
          ? {
              ...f,
              content: nextContent,
              sizeBytes: new Blob([nextContent]).size,
              updatedAt: 'Just now',
            }
          : f
      );
    });
    const existsInApp = app.files.some((f) => f.path === selectedFile.path);
    if (existsInApp) {
      const updatedFiles = app.files.map((f) =>
        f.path === selectedFile.path
          ? {
              ...f,
              content: nextContent,
              sizeBytes: new Blob([nextContent]).size,
              updatedAt: 'Just now',
            }
          : f
      );
      onUpdateApp({ ...app, files: updatedFiles });
    } else {
      onUpdateApp({
        ...app,
        files: [
          ...app.files,
          {
            ...selectedFile,
            content: nextContent,
            sizeBytes: new Blob([nextContent]).size,
            updatedAt: 'Just now',
          },
        ],
      });
    }
  };

  const renderFileIcon = (filename: string) => {
    return <DevFileIcon filename={filename} />;
  };

  return (
    <>
      <div
        className={`relative overflow-hidden rounded-xl border bg-[#111115] shadow-xl select-none transition-colors ${
          isDragOverFileManager
            ? 'border-orange-500 ring-2 ring-orange-500/30'
            : 'border-white/[0.08]'
        }`}
        onClick={() => {
          if (contextMenu) setContextMenu(null);
        }}
        onDragOver={(e) => {
          e.preventDefault();
          e.stopPropagation();
          if (!isDragOverFileManager) setIsDragOverFileManager(true);
        }}
        onDragLeave={(e) => {
          e.preventDefault();
          e.stopPropagation();
          if (e.currentTarget.contains(e.relatedTarget as Node)) return;
          setIsDragOverFileManager(false);
        }}
        onDrop={async (e) => {
          e.preventDefault();
          e.stopPropagation();
          setIsDragOverFileManager(false);
          if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
            const droppedFiles = Array.from(e.dataTransfer.files);
            void runLocalUpload(droppedFiles);
          }
        }}
      >
        {/* Visual Drag & Drop Overlay */}
        {isDragOverFileManager && (
          <div className="pointer-events-none absolute inset-0 z-40 flex flex-col items-center justify-center bg-[#09090b]/85 backdrop-blur-sm border-2 border-dashed border-orange-500">
            <Upload className="h-10 w-10 text-orange-400 mb-2 animate-bounce" />
            <p className="text-sm font-semibold text-zinc-100">
              Drop files here to upload into /{app.slug}{currentDir ? `/${currentDir}` : ''}
            </p>
            <p className="text-xs text-zinc-400 mt-0.5">Zip archives, code files, and assets are supported</p>
          </div>
        )}

        {/* Top cPanel Toolbar & Breadcrumb Strip */}
        <FileManagerToolbar
          appSlug={app.slug}
          appId={app.id}
          currentDir={currentDir}
          editingFilePath={editingFilePath}
          selectedFile={selectedFile}
          selectedEntries={selectedEntries}
          fileSearch={fileSearch}
          setFileSearch={setFileSearch}
          setEditingFilePath={setEditingFilePath}
          navigateToFolder={navigateToFolder}
          onOpenNewFile={() => {
            setNewFileName('');
            setNewFileTargetDir(currentDir || '');
            setNewFileModalOpen(true);
          }}
          onOpenNewFolder={() => {
            setNewFolderName('');
            setNewFolderTargetDir(currentDir || '');
            setNewFolderModalOpen(true);
          }}
          onOpenUpload={() => setUploadModalOpen(true)}
          onOpenCompress={() => openCompressModalFor(selectedEntries)}
          onOpenExtract={() => {
            const selectedArchive = defaultRichFiles.find(
              (f) => selectedEntries.includes(f.path) && isArchiveFile(f.name)
            );
            const fallbackArchive = currentDirFiles.find((f) => isArchiveFile(f.name));
            const target = selectedArchive || fallbackArchive;
            if (target) {
              setExtractTargetFile(target);
              setExtractDestDir(currentDir || '');
            } else {
              toast.info('Select a .zip or .tar.gz archive to extract');
            }
          }}
          onOpenRestoreArchive={() => {
            setRestoreArchiveModalOpen(true);
          }}
          onMoveSelected={() => openMoveOrCopyModal(selectedEntries, 'move')}
          onCopySelected={() => openMoveOrCopyModal(selectedEntries, 'copy')}
          onDeleteSelected={() => {
            void deleteEntries(selectedEntries);
            setSelectedEntries([]);
          }}
          onClearSelection={() => setSelectedEntries([])}
          mobileTreeOpen={mobileTreeOpen}
          setMobileTreeOpen={setMobileTreeOpen}
          currentSubfoldersCount={currentSubfolders.length}
          currentDirFilesCount={currentDirFiles.length}
          renderFileIcon={renderFileIcon}
        />

        {/* Main Body: Code Editor (when file opened) OR Directory Listing Table */}
        {editingFilePath ? (
          <FileManagerEditor
            app={app}
            selectedFile={selectedFile}
            openEditorTabs={openEditorTabs}
            defaultRichFiles={defaultRichFiles}
            filePermissions={filePermissions}
            editorSaveTick={editorSaveTick}
            setEditorSaveTick={setEditorSaveTick}
            isLoading={isFileLoading}
            onCloseEditor={() => setEditingFilePath(null)}
            onSelectTab={async (tab) => {
              setEditingFilePath(tab);
              if (!loadedPathsRef.current.has(tab)) {
                setIsFileLoading(true);
                await fetchFileContent(tab);
                setIsFileLoading(false);
              }
            }}
            onCloseTab={closeEditorTab}
            onCloseAllTabs={() => {
              setOpenEditorTabs([]);
              setEditingFilePath(null);
            }}
            onOpenChmod={(path) => openChmodFor(path)}
            onSaveContent={handleSaveFileContent}
            onSyncFiles={syncWorkspaceFilesFromDisk}
            onUpdateApp={onUpdateApp}
            renderFileIcon={renderFileIcon}
          />
        ) : (
          <FileManagerTable
            appSlug={app.slug}
            appId={app.id}
            currentDir={currentDir}
            editingFilePath={editingFilePath}
            mobileTreeOpen={mobileTreeOpen}
            setMobileTreeOpen={setMobileTreeOpen}
            expandedDirs={expandedDirs}
            setExpandedDirs={setExpandedDirs}
            allFolderPaths={allFolderPaths}
            rootFolders={rootFolders}
            treeIndex={treeIndex}
            toggleFolderTreeExpand={toggleFolderTreeExpand}
            navigateToFolder={navigateToFolder}
            setContextMenu={setContextMenu}
            defaultRichFiles={defaultRichFiles}
            currentSubfolders={currentSubfolders}
            currentDirFiles={currentDirFiles}
            selectedEntries={selectedEntries}
            setSelectedEntries={setSelectedEntries}
            toggleSelectEntry={toggleSelectEntry}
            filePermissions={filePermissions}
            openChmodFor={openChmodFor}
            openCompressModalFor={openCompressModalFor}
            openMoveOrCopyModal={openMoveOrCopyModal}
            deleteEntries={deleteEntries}
            openFileInEditor={openFileInEditor}
            onPrefetchFile={(p) => void fetchFileContent(p)}
            setExtractTargetFile={setExtractTargetFile}
            setExtractDestDir={setExtractDestDir}
            setRenameTarget={setRenameTarget}
            setRenameInput={setRenameInput}
            fileSearch={fileSearch}
            isArchiveFile={isArchiveFile}
            renderFileIcon={renderFileIcon}
          />
        )}
      </div>

      {/* Floating Right-Click Context Menu */}
      <FileManagerContextMenu
        contextMenu={contextMenu}
        appSlug={app.slug}
        appId={app.id}
        onClose={() => setContextMenu(null)}
        onNavigateToFolder={navigateToFolder}
        onOpenNewFileInFolder={(dir) => {
          setCurrentDir(dir);
          setNewFileName('');
          setNewFileTargetDir(dir || '');
          setNewFileModalOpen(true);
        }}
        onOpenNewFolderInFolder={(dir) => {
          setCurrentDir(dir);
          setNewFolderName('');
          setNewFolderTargetDir(dir || '');
          setNewFolderModalOpen(true);
        }}
        onOpenUploadInFolder={(dir) => {
          setCurrentDir(dir);
          setUploadModalOpen(true);
        }}
        onOpenFileInEditor={(p) => {
          const f = defaultRichFiles.find((node) => node.path === p);
          if (f) openFileInEditor(f);
        }}
        onExtractArchive={(p) => {
          const f = defaultRichFiles.find((node) => node.path === p);
          if (f) {
            setExtractTargetFile(f);
            setExtractDestDir(currentDir || '');
          }
        }}
        onCompress={(p) => openCompressModalFor([p])}
        onRename={(p, name, isFolder) => {
          setRenameTarget({ path: p, name, isFolder });
          setRenameInput(name);
        }}
        onMoveOrCopy={(p) => openMoveOrCopyModal([p])}
        onChmod={(p) => openChmodFor(p)}
        onDelete={(p) => void deleteEntries([p])}
        isArchiveFile={isArchiveFile}
        renderFileIcon={renderFileIcon}
      />

      {/* Extracted File Manager Dialogs */}
      <CreateFileDialog
        open={newFileModalOpen}
        onClose={() => setNewFileModalOpen(false)}
        appSlug={app.slug}
        currentDir={currentDir}
        allFolderPaths={allFolderPaths}
        newFileName={newFileName}
        setNewFileName={setNewFileName}
        newFileTargetDir={newFileTargetDir}
        setNewFileTargetDir={setNewFileTargetDir}
        onSubmit={handleCreateFileSubmit}
      />

      <CreateFolderDialog
        open={newFolderModalOpen}
        onClose={() => setNewFolderModalOpen(false)}
        appSlug={app.slug}
        currentDir={currentDir}
        allFolderPaths={allFolderPaths}
        newFolderName={newFolderName}
        setNewFolderName={setNewFolderName}
        newFolderTargetDir={newFolderTargetDir}
        setNewFolderTargetDir={setNewFolderTargetDir}
        onSubmit={handleCreateFolderSubmit}
      />

      <ChmodDialog
        open={Boolean(chmodModalFile)}
        onClose={() => setChmodModalFile(null)}
        appId={app.id}
        targetPath={chmodModalFile}
        currentPerm={chmodModalFile ? filePermissions[chmodModalFile] : undefined}
        isDir={Boolean(chmodModalFile && (customFolders.includes(chmodModalFile) || allFolderPaths.includes(chmodModalFile)))}
        onSaved={(path, octal) => {
          setFilePermissions((prev) => ({ ...prev, [path]: octal }));
          void syncWorkspaceFilesFromDisk();
        }}
      />

      <CompressDialog
        open={compressModalOpen}
        onClose={() => setCompressModalOpen(false)}
        currentDir={currentDir}
        compressFormat={compressFormat}
        setCompressFormat={setCompressFormat}
        compressArchiveName={compressArchiveName}
        setCompressArchiveName={setCompressArchiveName}
        onSubmit={handleCompressSubmit}
      />

      <ExtractDialog
        open={Boolean(extractTargetFile)}
        onClose={() => setExtractTargetFile(null)}
        appSlug={app.slug}
        extractTargetFile={extractTargetFile}
        extractDestDir={extractDestDir}
        setExtractDestDir={setExtractDestDir}
        allFolderPaths={allFolderPaths}
        onSubmit={handleExtractSubmit}
      />

      <RestoreArchiveDialog
        open={restoreArchiveModalOpen}
        onClose={() => setRestoreArchiveModalOpen(false)}
        appId={app.id}
        appSlug={app.slug}
        onRestored={syncWorkspaceFilesFromDisk}
      />

      <RenameDialog
        open={Boolean(renameTarget)}
        onClose={() => setRenameTarget(null)}
        renameTarget={renameTarget?.path || null}
        renameInput={renameInput}
        setRenameInput={setRenameInput}
        onSubmit={handleRenameSubmit}
      />

      <MoveCopyDialog
        open={Boolean(moveTarget)}
        onClose={() => setMoveTarget(null)}
        appSlug={app.slug}
        moveTarget={moveTarget}
        setMoveTarget={setMoveTarget}
        moveDestInput={moveDestInput}
        setMoveDestInput={setMoveDestInput}
        allFolderPaths={allFolderPaths}
        onAddCustomFolder={(folder) => {
          setCustomFolders((prev) => (prev.includes(folder) ? prev : [...prev, folder]));
        }}
        onSubmit={handleMoveOrCopySubmit}
      />

      <CollisionDialog
        open={Boolean(conflictModal)}
        onClose={() => setConflictModal(null)}
        conflictModal={conflictModal}
        onExecute={executeMoveOrCopy}
      />

      <UploadFileDialog
        open={uploadModalOpen}
        onClose={() => setUploadModalOpen(false)}
        currentDir={currentDir}
        uploadMode={uploadMode}
        setUploadMode={setUploadMode}
        selectedUploadFiles={selectedUploadFiles}
        setSelectedUploadFiles={setSelectedUploadFiles}
        uploadRemoteUrl={uploadRemoteUrl}
        setUploadRemoteUrl={setUploadRemoteUrl}
        isUploadingFiles={isUploadingFiles}
        onSubmit={handleUploadSubmit}
      />

      {/* Floating Persistent Upload Toast */}
      <FileManagerUploadToast
        isUploading={isUploadingFiles}
        progress={uploadProgress}
        fileName={uploadFileName}
      />
    </>
  );
};
