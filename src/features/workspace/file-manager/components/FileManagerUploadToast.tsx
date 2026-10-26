import React from 'react';
import { Loader2 } from 'lucide-react';

interface FileManagerUploadToastProps {
  isUploading: boolean;
  progress: number;
  fileName: string;
}

export const FileManagerUploadToast: React.FC<FileManagerUploadToastProps> = ({
  isUploading,
  progress,
  fileName,
}) => {
  if (!isUploading) return null;

  return (
    <div className="fixed bottom-5 right-5 z-50 flex w-80 items-center gap-3.5 rounded-xl border border-orange-500/30 bg-[#121217]/98 p-3.5 shadow-2xl backdrop-blur-xl">
      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-orange-500/15 border border-orange-500/25">
        <Loader2 className="h-5 w-5 animate-spin text-orange-400" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-1">
          <span className="text-xs font-semibold text-zinc-100 truncate">Uploading files...</span>
          <span className="font-mono text-[10px] text-orange-400 font-semibold tabular-nums">{progress}%</span>
        </div>
        <p className="mt-0.5 text-[11px] text-zinc-400 truncate">
          {progress >= 100 ? 'Processing on server...' : fileName}
        </p>
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-zinc-800">
          <div
            className="h-full rounded-full bg-gradient-to-r from-orange-500 to-amber-400 transition-[width] duration-150"
            style={{ width: `${progress}%` }}
          />
        </div>
      </div>
    </div>
  );
};
