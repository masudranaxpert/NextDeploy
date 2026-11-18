import React, { useMemo, useState } from 'react';
import { HardDrive, Loader2, Search } from 'lucide-react';

interface SharedVolumesCardProps {
  sharedMountPrefix: string;
  setSharedMountPrefix: (v: string) => void;
  allVolumes: string[];
  selectedVolumes: string[];
  onToggleVolume: (name: string) => void;
  saving: boolean;
  onSave: (e: React.FormEvent) => void;
}

export const SharedVolumesCard: React.FC<SharedVolumesCardProps> = ({
  sharedMountPrefix,
  setSharedMountPrefix,
  allVolumes,
  selectedVolumes,
  onToggleVolume,
  saving,
  onSave,
}) => {
  const [filter, setFilter] = useState('');

  const filteredVolumes = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const union = Array.from(new Set([...selectedVolumes, ...allVolumes])).sort();
    if (!q) return union;
    return union.filter((v) => v.toLowerCase().includes(q));
  }, [allVolumes, selectedVolumes, filter]);

  return (
    <form
      onSubmit={onSave}
      className="rounded-xl border border-white/[0.08] bg-[#111115] p-4 sm:p-5 space-y-4"
    >
      <div className="space-y-0.5">
        <div className="flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-cyan-300" />
          <h2 className="text-sm font-semibold text-zinc-100">Shared Asset Volumes</h2>
        </div>
        <p className="text-xs text-zinc-400">
          Mount Docker volumes read-only into the proxy runtime for direct static file delivery.
        </p>
      </div>

      <div className="space-y-1.5">
        <label htmlFor="shared-mount-prefix" className="block text-xs font-medium text-zinc-300">
          Mount Prefix Inside Proxy
        </label>
        <input
          id="shared-mount-prefix"
          type="text"
          value={sharedMountPrefix}
          onChange={(e) => setSharedMountPrefix(e.target.value)}
          placeholder="/mnt/shared"
          className="h-9 w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 font-mono text-xs text-zinc-100 placeholder:text-zinc-400 focus:border-cyan-500/50 focus:outline-none"
        />
      </div>

      <div className="space-y-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-400" />
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter Docker volumes..."
            aria-label="Filter Docker volumes"
            className="h-8 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-8 pr-3 text-xs text-zinc-200 placeholder:text-zinc-400 focus:border-cyan-500/40 focus:outline-none"
          />
        </div>

        <div className="max-h-40 overflow-y-auto rounded-lg border border-white/[0.07] bg-[#09090b] divide-y divide-white/[0.05]">
          {filteredVolumes.length === 0 ? (
            <div className="p-3 text-center text-[11px] text-zinc-400">
              No matching Docker volumes detected.
            </div>
          ) : (
            filteredVolumes.map((vol) => {
              const checked = selectedVolumes.includes(vol);
              return (
                <label
                  key={vol}
                  className="flex items-center justify-between gap-3 px-3 py-2 text-xs hover:bg-zinc-900/60 cursor-pointer"
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => onToggleVolume(vol)}
                      className="rounded border-white/20 bg-zinc-950"
                    />
                    <span className="font-mono text-zinc-200 truncate">{vol}</span>
                  </div>
                  {checked && (
                    <span className="font-mono text-[10px] text-cyan-300 shrink-0">
                      {sharedMountPrefix.replace(/\/+$/, '') || '/mnt/shared'}/{vol}
                    </span>
                  )}
                </label>
              );
            })
          )}
        </div>
      </div>

      <div className="flex items-center justify-between gap-3">
        <span className="text-[11px] font-mono text-zinc-400">
          {selectedVolumes.length} volume{selectedVolumes.length === 1 ? '' : 's'} mounted
        </span>
        <button
          type="submit"
          disabled={saving}
          className="inline-flex items-center justify-center gap-1.5 min-h-[44px] sm:min-h-0 sm:h-9 px-4 rounded-lg border border-white/[0.1] bg-zinc-900 text-xs font-medium text-zinc-100 hover:bg-zinc-800 disabled:opacity-50 transition-colors cursor-pointer"
        >
          {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          {saving ? 'Updating...' : 'Save Shared Volumes'}
        </button>
      </div>
    </form>
  );
};
