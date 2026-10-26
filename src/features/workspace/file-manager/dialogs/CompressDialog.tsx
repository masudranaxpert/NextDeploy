import React from 'react';
import { Archive } from 'lucide-react';
import { ShadcnDialog } from '../../../../components/ui/Primitives';

interface CompressDialogProps {
  open: boolean;
  onClose: () => void;
  currentDir: string;
  compressFormat: 'zip' | 'tar.gz';
  setCompressFormat: (fmt: 'zip' | 'tar.gz') => void;
  compressArchiveName: string;
  setCompressArchiveName: (name: string) => void;
  onSubmit: (e: React.FormEvent) => void;
}

export const CompressDialog: React.FC<CompressDialogProps> = ({
  open,
  onClose,
  currentDir,
  compressFormat,
  setCompressFormat,
  compressArchiveName,
  setCompressArchiveName,
  onSubmit,
}) => {
  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Compress Archive (Zip / Tar.gz)"
      icon={<Archive className="h-4 w-4 text-purple-400" />}
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            Archive Format
          </label>
          <div className="grid grid-cols-2 gap-2">
            {(['zip', 'tar.gz'] as const).map((fmt) => (
              <button
                key={fmt}
                type="button"
                onClick={() => {
                  setCompressFormat(fmt);
                  const stem = compressArchiveName
                    .replace(/\.tar\.gz$/i, '')
                    .replace(/\.zip$/i, '');
                  setCompressArchiveName(`${stem}.${fmt}`);
                }}
                className={`rounded-lg border px-3 py-2 font-mono text-xs font-semibold cursor-pointer ${
                  compressFormat === fmt
                    ? 'border-purple-500/50 bg-purple-500/15 text-purple-200'
                    : 'border-white/[0.08] bg-[#09090b] text-zinc-400'
                }`}
              >
                .{fmt} Archive
              </button>
            ))}
          </div>
        </div>

        <div>
          <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
            Archive File Name {currentDir ? `(in /${currentDir})` : '(in root)'}
          </label>
          <input
            type="text"
            required
            value={compressArchiveName}
            onChange={(e) => setCompressArchiveName(e.target.value)}
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
            Compress Now
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
