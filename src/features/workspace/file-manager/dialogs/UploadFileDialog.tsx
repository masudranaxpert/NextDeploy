import React, { useState } from 'react';
import { Upload } from 'lucide-react';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { formatFileSize } from '../utils';

interface UploadFileDialogProps {
  open: boolean;
  onClose: () => void;
  currentDir: string;
  uploadMode: 'local' | 'url';
  setUploadMode: (mode: 'local' | 'url') => void;
  selectedUploadFiles: File[];
  setSelectedUploadFiles: React.Dispatch<React.SetStateAction<File[]>>;
  uploadRemoteUrl: string;
  setUploadRemoteUrl: (url: string) => void;
  isUploadingFiles: boolean;
  onSubmit: (e: React.FormEvent) => void;
}

export const UploadFileDialog: React.FC<UploadFileDialogProps> = ({
  open,
  onClose,
  currentDir,
  uploadMode,
  setUploadMode,
  selectedUploadFiles,
  setSelectedUploadFiles,
  uploadRemoteUrl,
  setUploadRemoteUrl,
  isUploadingFiles,
  onSubmit,
}) => {
  const [isDragOverUploadZone, setIsDragOverUploadZone] = useState(false);

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Upload File or Fetch Remote URL"
      icon={<Upload className="h-4 w-4 text-sky-400" />}
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div className="grid grid-cols-2 gap-1.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-1 text-xs">
          <button
            type="button"
            onClick={() => setUploadMode('local')}
            className={`rounded-md py-1.5 font-medium transition-colors cursor-pointer ${
              uploadMode === 'local'
                ? 'bg-zinc-800 text-zinc-100'
                : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            Local File / Zip
          </button>
          <button
            type="button"
            onClick={() => setUploadMode('url')}
            className={`rounded-md py-1.5 font-medium transition-colors cursor-pointer ${
              uploadMode === 'url'
                ? 'bg-zinc-800 text-zinc-100'
                : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            Fetch Remote URL
          </button>
        </div>

        {uploadMode === 'local' ? (
          <div className="space-y-3">
            <div
              onDragOver={(e) => {
                e.preventDefault();
                setIsDragOverUploadZone(true);
              }}
              onDragLeave={() => setIsDragOverUploadZone(false)}
              onDrop={(e) => {
                e.preventDefault();
                setIsDragOverUploadZone(false);
                if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
                  const dropped = Array.from(e.dataTransfer.files);
                  setSelectedUploadFiles((prev) => [...prev, ...dropped]);
                }
              }}
              className={`relative flex flex-col items-center justify-center rounded-xl border-2 border-dashed p-6 text-center transition-all cursor-pointer ${
                isDragOverUploadZone
                  ? 'border-orange-500 bg-orange-500/10 text-orange-300'
                  : 'border-white/[0.12] bg-[#09090b]/80 hover:border-white/[0.24] hover:bg-zinc-900/60 text-zinc-400'
              }`}
              onClick={() => document.getElementById('workspace-file-input')?.click()}
            >
              <input
                id="workspace-file-input"
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  if (e.target.files && e.target.files.length > 0) {
                    const files = Array.from(e.target.files);
                    setSelectedUploadFiles((prev) => [...prev, ...files]);
                  }
                }}
              />
              <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900 mb-2">
                <Upload className="h-5 w-5 text-orange-400" />
              </div>
              <p className="text-xs font-semibold text-zinc-200">
                Click to browse or drag & drop files here
              </p>
              <p className="text-[11px] text-zinc-500 mt-1">
                Upload into: <span className="font-mono text-zinc-300">/{currentDir || 'root'}</span>
              </p>
            </div>

            {selectedUploadFiles.length > 0 && (
              <div className="space-y-1.5 rounded-lg border border-white/[0.08] bg-[#09090b] p-2.5 max-h-40 overflow-y-auto">
                <div className="flex items-center justify-between text-[11px] font-medium text-zinc-400 px-1 pb-1 border-b border-white/[0.06]">
                  <span>Selected Files ({selectedUploadFiles.length})</span>
                  <button
                    type="button"
                    onClick={() => setSelectedUploadFiles([])}
                    className="text-rose-400 hover:underline cursor-pointer"
                  >
                    Clear All
                  </button>
                </div>
                {selectedUploadFiles.map((f, fIdx) => (
                  <div
                    key={fIdx}
                    className="flex items-center justify-between gap-2 px-1 py-0.5 text-xs font-mono text-zinc-300"
                  >
                    <span className="truncate">{f.name}</span>
                    <div className="flex items-center gap-2 shrink-0 text-[10px] text-zinc-500">
                      <span>{formatFileSize(f.size)}</span>
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          setSelectedUploadFiles((prev) => prev.filter((_, i) => i !== fIdx));
                        }}
                        className="text-zinc-500 hover:text-rose-400 cursor-pointer"
                      >
                        ✕
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Remote HTTPS URL {currentDir ? `(save into /${currentDir})` : '(save into root)'}
            </label>
            <input
              type="url"
              required
              value={uploadRemoteUrl}
              onChange={(e) => setUploadRemoteUrl(e.target.value)}
              placeholder="https://github.com/org/repo/releases/download/v1.0/bundle.zip"
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100 focus:border-orange-500 focus:outline-none"
            />
          </div>
        )}

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
            disabled={isUploadingFiles}
            className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer disabled:opacity-50"
          >
            {isUploadingFiles
              ? 'Uploading...'
              : uploadMode === 'url'
              ? 'Fetch URL'
              : `Upload ${selectedUploadFiles.length > 0 ? `(${selectedUploadFiles.length})` : ''}`}
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
