import React, { useEffect, useState } from 'react';
import { Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../components/ui/Primitives';
import { backendApi } from '../../../lib/backendApi';
import { GhostButton, inputCls } from '../ui';

/** Confirms and deletes a PHP site: the user must type the site name first. */
export const DeleteSiteDialog: React.FC<{
  site: { id: string; name: string } | null;
  onClose: () => void;
  onDeleted: (id: string) => void | Promise<void>;
  /** Wording: a PHP site or a Docker project. */
  kind?: 'site' | 'project';
}> = ({ site, onClose, onDeleted, kind = 'site' }) => {
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setTyped('');
  }, [site?.id]);

  const remove = async () => {
    if (!site) return;
    setBusy(true);
    const res = await backendApi.deleteApp(site.id);
    setBusy(false);
    if (!res.ok) {
      toast.error(res.error || `Could not delete the ${kind}`);
      return;
    }
    toast.success(`${site.name} was deleted`);
    onClose();
    await onDeleted(site.id);
  };

  return (
    <ShadcnDialog
      open={site !== null}
      onClose={() => (busy ? undefined : onClose())}
      title={kind === 'site' ? 'Delete site' : 'Delete project'}
      icon={<Trash2 className="h-4 w-4 text-rose-400" />}
    >
      <div className="space-y-3">
        <p className="text-xs leading-5 text-zinc-400">
          This permanently removes <strong className="text-zinc-100">{site?.name}</strong> with{' '}
          {kind === 'site'
            ? 'its files, databases, database users, cron jobs and domains'
            : 'its containers, images, volumes, networks, domains, backup schedules and files'}
          . Backups already stored in a remote destination are kept. It cannot be undone.
        </p>
        <label className="block text-[11px] text-zinc-400">
          Type <code className="font-mono text-rose-300">{site?.name}</code> to confirm
          <input
            className={`${inputCls} mt-1.5`}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
          <GhostButton onClick={onClose} disabled={busy}>
            Cancel
          </GhostButton>
          <button
            type="button"
            disabled={busy || typed.trim() !== site?.name}
            onClick={() => void remove()}
            className="rounded-lg bg-rose-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 disabled:opacity-40 cursor-pointer"
          >
            {busy ? 'Deleting…' : kind === 'site' ? 'Delete site' : 'Delete project'}
          </button>
        </div>
      </div>
    </ShadcnDialog>
  );
};
