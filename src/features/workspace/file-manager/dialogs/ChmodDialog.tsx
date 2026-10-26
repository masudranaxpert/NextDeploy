import React, { useState, useEffect, useMemo } from 'react';
import { KeyRound } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../../components/ui/Primitives';
import { backendApi } from '../../../../lib/backendApi';
import { formatPermOctal } from '../utils';

interface ChmodDialogProps {
  open: boolean;
  onClose: () => void;
  appId: string;
  targetPath: string | null;
  currentPerm?: string;
  isDir?: boolean;
  onSaved: (path: string, newOctal: string) => void;
}

export const ChmodDialog: React.FC<ChmodDialogProps> = ({
  open,
  onClose,
  appId,
  targetPath,
  currentPerm,
  isDir = false,
  onSaved,
}) => {
  const [matrix, setMatrix] = useState({
    ownerR: true,
    ownerW: true,
    ownerX: false,
    groupR: true,
    groupW: false,
    groupX: false,
    worldR: true,
    worldW: false,
    worldX: false,
  });

  useEffect(() => {
    if (!targetPath) return;
    const oct = formatPermOctal(currentPerm, isDir);
    const u = parseInt(oct[0] || (isDir ? '7' : '6'), 10);
    const g = parseInt(oct[1] || (isDir ? '5' : '4'), 10);
    const w = parseInt(oct[2] || (isDir ? '5' : '4'), 10);
    setMatrix({
      ownerR: Boolean(u & 4),
      ownerW: Boolean(u & 2),
      ownerX: Boolean(u & 1),
      groupR: Boolean(g & 4),
      groupW: Boolean(g & 2),
      groupX: Boolean(g & 1),
      worldR: Boolean(w & 4),
      worldW: Boolean(w & 2),
      worldX: Boolean(w & 1),
    });
  }, [targetPath, currentPerm, isDir]);

  const computedOctal = useMemo(() => {
    const u = (matrix.ownerR ? 4 : 0) + (matrix.ownerW ? 2 : 0) + (matrix.ownerX ? 1 : 0);
    const g = (matrix.groupR ? 4 : 0) + (matrix.groupW ? 2 : 0) + (matrix.groupX ? 1 : 0);
    const w = (matrix.worldR ? 4 : 0) + (matrix.worldW ? 2 : 0) + (matrix.worldX ? 1 : 0);
    return `${u}${g}${w}`;
  }, [matrix]);

  if (!targetPath) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const res = await backendApi.fileChmod(appId, targetPath, computedOctal);
    if (res.ok === false) {
      toast.error(res.error || res.message || 'Failed to update file permissions');
      return;
    }
    onSaved(targetPath, computedOctal);
    toast.success(`Updated permissions for ${targetPath} to ${computedOctal}`);
    onClose();
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title="Change Permissions (chmod)"
      icon={<KeyRound className="h-4 w-4 text-emerald-400" />}
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="flex items-center justify-between rounded-lg border border-white/[0.07] bg-[#09090b] px-3 py-2 font-mono text-xs">
          <span className="text-zinc-300 truncate">{targetPath}</span>
          <span className="rounded bg-emerald-500/15 px-2 py-0.5 text-emerald-300 font-semibold">
            {computedOctal}
          </span>
        </div>

        {/* 3x3 Permission Checkboxes (Owner / Group / World) */}
        <div className="overflow-hidden rounded-lg border border-white/[0.07] bg-[#09090b]">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-white/[0.07] bg-white/[0.02] text-zinc-400">
                <th className="px-3 py-2 text-left font-medium">Scope</th>
                <th className="px-3 py-2 text-center font-medium">Read (4)</th>
                <th className="px-3 py-2 text-center font-medium">Write (2)</th>
                <th className="px-3 py-2 text-center font-medium">Execute (1)</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              <tr>
                <td className="px-3 py-2.5 font-medium text-zinc-200">Owner</td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.ownerR}
                    onChange={(e) => setMatrix((p) => ({ ...p, ownerR: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.ownerW}
                    onChange={(e) => setMatrix((p) => ({ ...p, ownerW: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.ownerX}
                    onChange={(e) => setMatrix((p) => ({ ...p, ownerX: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
              </tr>
              <tr>
                <td className="px-3 py-2.5 font-medium text-zinc-200">Group</td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.groupR}
                    onChange={(e) => setMatrix((p) => ({ ...p, groupR: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.groupW}
                    onChange={(e) => setMatrix((p) => ({ ...p, groupW: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.groupX}
                    onChange={(e) => setMatrix((p) => ({ ...p, groupX: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
              </tr>
              <tr>
                <td className="px-3 py-2.5 font-medium text-zinc-200">World</td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.worldR}
                    onChange={(e) => setMatrix((p) => ({ ...p, worldR: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.worldW}
                    onChange={(e) => setMatrix((p) => ({ ...p, worldW: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
                <td className="px-3 py-2.5 text-center">
                  <input
                    type="checkbox"
                    checked={matrix.worldX}
                    onChange={(e) => setMatrix((p) => ({ ...p, worldX: e.target.checked }))}
                    className="h-3.5 w-3.5 accent-orange-500 cursor-pointer"
                  />
                </td>
              </tr>
            </tbody>
          </table>
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
            Save Permissions ({computedOctal})
          </button>
        </div>
      </form>
    </ShadcnDialog>
  );
};
