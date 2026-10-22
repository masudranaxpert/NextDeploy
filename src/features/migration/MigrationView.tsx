import React, { useState } from 'react';
import { Copy, Plus, Download, ArrowLeftRight } from 'lucide-react';
import { toast } from 'sonner';
import { AppStack, MigrationExportBundle } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';

interface MigrationViewProps {
  apps: AppStack[];
  bundles: MigrationExportBundle[];
  onExportBundle: (selectedSlugs: string[]) => void;
}

export const MigrationView: React.FC<MigrationViewProps> = ({
  apps,
  bundles,
  onExportBundle,
}) => {
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [selectedSlugs, setSelectedSlugs] = useState<string[]>(apps.map((a) => a.slug));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <ArrowLeftRight className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">
            Server Migration
          </h1>
        </div>

        <button
          type="button"
          onClick={() => setExportModalOpen(true)}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
        >
          <Plus className="h-3.5 w-3.5" />
          Create Export Bundle
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[22%] px-4 py-2.5 font-medium">Bundle ID</th>
              <th className="w-[38%] px-4 py-2.5 font-medium">Projects Included</th>
              <th className="w-[16%] px-4 py-2.5 font-medium">Status</th>
              <th className="w-[14%] px-4 py-2.5 font-medium text-right">Size</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {bundles.map((b) => (
              <tr key={b.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3 font-mono font-semibold text-zinc-100">{b.id}</td>
                <td className="px-4 py-3 font-mono text-orange-400 truncate">
                  {b.selectedApps.join(', ')}
                </td>
                <td className="px-4 py-3">
                  <StatusBadge status={b.status} />
                </td>
                <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                  {(b.actualSizeMB / 1024).toFixed(2)} GB
                </td>
                <td className="px-3 py-3 text-right">
                  <ActionMenu
                    items={[
                      {
                        label: 'Copy Migration Token',
                        icon: <Copy className="h-3.5 w-3.5" />,
                        onClick: () => {
                          navigator.clipboard?.writeText(b.token);
                          toast.success('Copied migration token');
                        },
                      },
                      {
                        label: 'Download Bundle',
                        icon: <Download className="h-3.5 w-3.5" />,
                        onClick: () => toast.success(`Downloading ${b.id}.tar.zst`),
                      },
                    ]}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <ShadcnDialog
        open={exportModalOpen}
        onClose={() => setExportModalOpen(false)}
        title="Create Migration Bundle"
        icon={<ArrowLeftRight className="h-4 w-4" />}
      >
        <div className="space-y-3">
          {apps.map((a) => {
            const checked = selectedSlugs.includes(a.slug);
            return (
              <label
                key={a.id}
                className="flex items-center justify-between rounded-lg border border-white/[0.08] bg-[#09090b] px-3 py-2.5 cursor-pointer"
              >
                <span className="font-mono text-xs font-medium text-zinc-100">{a.name}</span>
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={() =>
                    setSelectedSlugs((prev) =>
                      checked ? prev.filter((s) => s !== a.slug) : [...prev, a.slug]
                    )
                  }
                  className="accent-orange-500"
                />
              </label>
            );
          })}
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setExportModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => {
                onExportBundle(selectedSlugs);
                setExportModalOpen(false);
              }}
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Generate Bundle
            </button>
          </div>
        </div>
      </ShadcnDialog>
    </div>
  );
};
