import React, { useState } from 'react';
import { Trash2 } from 'lucide-react';
import { KobraInputOTP, ShadcnDialog } from '../../../components/ui/Primitives';

interface DeleteAppDialogProps {
  open: boolean;
  onClose: () => void;
  appName: string;
  appId: string;
  onConfirmDelete: (appId: string) => void;
}

export const DeleteAppDialog: React.FC<DeleteAppDialogProps> = ({
  open,
  onClose,
  appName,
  appId,
  onConfirmDelete,
}) => {
  const [otpDeleteCode, setOtpDeleteCode] = useState('');

  const handleClose = () => {
    setOtpDeleteCode('');
    onClose();
  };

  return (
    <ShadcnDialog
      open={open}
      onClose={handleClose}
      title="Delete Project"
      icon={<Trash2 className="h-4 w-4 text-rose-400" />}
    >
      <div className="space-y-4">
        <p className="text-xs text-zinc-400">
          Enter <code className="font-mono font-bold text-rose-400">DELETE</code> to permanently
          remove <strong className="text-zinc-100">{appName}</strong>:
        </p>
        <KobraInputOTP length={6} value={otpDeleteCode} onChange={setOtpDeleteCode} />
        <div className="flex justify-end gap-2 border-t border-white/[0.07] pt-3">
          <button
            type="button"
            onClick={handleClose}
            className="rounded-lg border border-white/[0.08] bg-zinc-900 px-3.5 py-1.5 text-xs text-zinc-300 cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={otpDeleteCode !== 'DELETE'}
            onClick={() => onConfirmDelete(appId)}
            className="rounded-lg bg-rose-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 disabled:opacity-40 cursor-pointer"
          >
            Delete Project
          </button>
        </div>
      </div>
    </ShadcnDialog>
  );
};
