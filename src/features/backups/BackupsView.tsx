import React, { useState } from 'react';
import {
  Plus,
  Download,
  RotateCw,
  Cloud,
  Archive,
} from 'lucide-react';
import { toast } from 'sonner';
import { BackupArchiveRecord, BackupDestination } from '../../types/nextdeploy';
import { ActionMenu, ShadcnDialog, StatusBadge } from '../../components/ui/Primitives';

interface BackupsViewProps {
  destinations: BackupDestination[];
  archives: BackupArchiveRecord[];
  onTriggerBackup: () => void;
}

export const BackupsView: React.FC<BackupsViewProps> = ({
  destinations: initialDestinations,
  archives,
  onTriggerBackup,
}) => {
  const [destinations, setDestinations] = useState<BackupDestination[]>(initialDestinations);
  const [r2ModalOpen, setR2ModalOpen] = useState(false);
  const [r2Bucket, setR2Bucket] = useState('nextdeploy-prod-backups');
  const [r2Endpoint, setR2Endpoint] = useState('https://91a8c4f.r2.cloudflarestorage.com');

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900">
            <Archive className="h-4 w-4 text-orange-400" />
          </div>
          <h1 className="text-base font-semibold tracking-tight text-zinc-100">Backups & S3</h1>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setR2ModalOpen(true)}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-3 text-xs font-medium text-zinc-200 hover:bg-zinc-800 cursor-pointer"
          >
            <Cloud className="h-3.5 w-3.5 text-orange-400" />
            Add S3 / R2 Destination
          </button>
          <button
            type="button"
            onClick={onTriggerBackup}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            Create Snapshot
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
        {destinations.map((d) => (
          <div
            key={d.id}
            className="rounded-xl border border-white/[0.07] bg-[#111115]/90 p-4 flex items-center justify-between"
          >
            <div className="flex items-center gap-3 min-w-0">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900">
                <Cloud className="h-4 w-4 text-orange-400" />
              </div>
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-zinc-100 truncate">{d.name}</span>
                  <StatusBadge status={d.status} />
                </div>
                <div className="mt-0.5 font-mono text-[11px] text-zinc-400 truncate">
                  {d.bucket} · cron {d.scheduleCron}
                </div>
              </div>
            </div>
            <span className="font-mono text-xs text-zinc-300 shrink-0">
              {d.usedGB} / {d.quotaGB} GB
            </span>
          </div>
        ))}
      </div>

      <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#111115]/90">
        <table className="w-full table-fixed text-left text-xs">
          <thead>
            <tr className="border-b border-white/[0.07] bg-[#09090b]/60 text-zinc-400">
              <th className="w-[44%] px-4 py-2.5 font-medium">Archive</th>
              <th className="w-[22%] px-4 py-2.5 font-medium">Project</th>
              <th className="w-[14%] px-4 py-2.5 font-medium">Created</th>
              <th className="w-[12%] px-4 py-2.5 font-medium text-right">Size</th>
              <th className="w-12 px-3 py-2.5 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.05]">
            {archives.map((bk) => (
              <tr key={bk.id} className="hover:bg-white/[0.03]">
                <td className="px-4 py-3 font-mono font-semibold text-zinc-100 truncate">
                  {bk.archiveName}
                </td>
                <td className="px-4 py-3 font-mono text-orange-400 truncate">{bk.appSlug}</td>
                <td className="px-4 py-3 font-mono text-zinc-400">{bk.createdAt}</td>
                <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">
                  {(bk.sizeMB / 1024).toFixed(2)} GB
                </td>
                <td className="px-3 py-3 text-right">
                  <ActionMenu
                    items={[
                      {
                        label: 'Restore Snapshot',
                        icon: <RotateCw className="h-3.5 w-3.5" />,
                        onClick: () => toast.success(`Restoring ${bk.archiveName}...`),
                      },
                      {
                        label: 'Download .tar.gz',
                        icon: <Download className="h-3.5 w-3.5" />,
                        onClick: () => toast.success(`Downloading ${bk.archiveName}`),
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
        open={r2ModalOpen}
        onClose={() => setR2ModalOpen(false)}
        title="Connect S3 / Cloudflare R2"
        icon={<Cloud className="h-4 w-4" />}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setDestinations((prev) => [
              ...prev,
              {
                id: `dest-${Date.now()}`,
                provider: 'Cloudflare R2',
                name: `R2 (${r2Bucket})`,
                bucket: `s3://${r2Bucket}`,
                endpoint: r2Endpoint,
                region: 'auto',
                status: 'connected',
                scheduleCron: '0 3 * * *',
                retentionDays: 30,
                usedGB: 0.1,
                quotaGB: 100,
                lastBackupAt: 'Just now',
              },
            ]);
            setR2ModalOpen(false);
            toast.success('Connected S3/R2 bucket');
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              Bucket Name
            </label>
            <input
              type="text"
              required
              value={r2Bucket}
              onChange={(e) => setR2Bucket(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div>
            <label className="block text-[11px] font-medium text-zinc-400 mb-1.5">
              S3 Endpoint URL
            </label>
            <input
              type="text"
              required
              value={r2Endpoint}
              onChange={(e) => setR2Endpoint(e.target.value)}
              className="w-full rounded-lg border border-white/[0.09] bg-[#09090b] px-3 py-2 font-mono text-xs text-zinc-100"
            />
          </div>
          <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
            <button
              type="button"
              onClick={() => setR2ModalOpen(false)}
              className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
            >
              Connect Bucket
            </button>
          </div>
        </form>
      </ShadcnDialog>
    </div>
  );
};
