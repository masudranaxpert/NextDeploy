import React from 'react';
import { Layers } from 'lucide-react';
import { toast } from 'sonner';
import { ShadcnDialog } from '../../../components/ui/Primitives';
import { CodeEditorBox } from '../../../components/ui/CodeEditorBox';
import { copyToClipboard } from '../../../lib/clipboard';

interface MergedComposeModalProps {
  open: boolean;
  onClose: () => void;
  appSlug: string;
  yaml: string;
}

export const MergedComposeModal: React.FC<MergedComposeModalProps> = ({
  open,
  onClose,
  appSlug,
  yaml,
}) => {
  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      title={`Merged Caddy Compose · ${appSlug}`}
      icon={<Layers className="h-4 w-4 text-emerald-400" />}
      maxWidth="lg"
    >
      <div className="space-y-3">
        <CodeEditorBox
          filename="docker-compose.merged.yml"
          language="yaml"
          value={yaml}
          onChange={() => {}}
          readOnly
          height={280}
        />
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={() => {
              void copyToClipboard(yaml);
              toast.success('Copied merged Compose YAML');
            }}
            className="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white cursor-pointer"
          >
            Copy Merged YAML
          </button>
        </div>
      </div>
    </ShadcnDialog>
  );
};
