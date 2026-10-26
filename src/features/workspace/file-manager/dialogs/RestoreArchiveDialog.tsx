import React, { useState } from 'react';
import { RotateCcw } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { backendApi } from '../../../../lib/backendApi';

interface RestoreArchiveDialogProps {
  open: boolean;
  onClose: () => void;
  appId: string;
  appSlug: string;
  onRestored: () => Promise<void>;
}

export const RestoreArchiveDialog: React.FC<RestoreArchiveDialogProps> = ({
  open,
  onClose,
  appId,
  appSlug,
  onRestored,
}) => {
  const [selectedRestoreFile, setSelectedRestoreFile] = useState<File | null>(null);
  const [isRestoringArchive, setIsRestoringArchive] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedRestoreFile) {
      toast.error('Please select a .tar.gz or .zip archive file');
      return;
    }
    setIsRestoringArchive(true);
    toast.info(`Extracting ${selectedRestoreFile.name} into workspace...`);
    try {
      const res = await backendApi.uploadWorkspaceArchive(appId, selectedRestoreFile);
      if (res.ok) {
        toast.success(
          res.message || `Successfully restored ${res.files_extracted || ''} files to workspace`
        );
        await onRestored();
        onClose();
        setSelectedRestoreFile(null);
      } else {
        toast.error(res.error || 'Failed to extract archive');
      }
    } catch (err: any) {
      toast.error(err.message || 'Restore failed');
    } finally {
      setIsRestoringArchive(false);
    }
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Restore Workspace Archive"
      icon={<RotateCcw className="h-4 w-4 text-amber-400" />}
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-3 text-xs text-amber-200">
          <strong>Warning:</strong> Restoring an archive will unpack its files directly into{' '}
          <code className="font-mono text-zinc-100">/{appSlug}</code>, overwriting matching existing files.
        </div>

        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            Select Archive File (.tar.gz, .tgz, .zip)
          </label>
          <input
            type="file"
            required
            accept=".tar.gz,.tgz,.zip,.tar"
            onChange={(e) => {
              if (e.target.files && e.target.files[0]) {
                setSelectedRestoreFile(e.target.files[0]);
              }
            }}
            className="w-full text-xs text-zinc-300 file:mr-3 file:py-1.5 file:px-3 file:rounded-lg file:border-0 file:bg-zinc-800 file:text-xs file:font-semibold file:text-zinc-200 hover:file:bg-zinc-700 cursor-pointer"
          />
        </div>

        <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
          <button
            type="button"
            disabled={isRestoringArchive}
            onClick={onClose}
            className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={isRestoringArchive || !selectedRestoreFile}
            className="rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-amber-400 cursor-pointer disabled:opacity-50"
          >
            {isRestoringArchive ? 'Extracting...' : 'Restore Workspace'}
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
