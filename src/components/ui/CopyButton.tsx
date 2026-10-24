import React, { useState } from 'react';
import { Copy, Check } from 'lucide-react';
import { toast } from 'sonner';
import { copyToClipboard } from '../../lib/clipboard';

export interface CopyButtonProps {
  value: string;
  label?: string;
  toastMessage?: string;
  title?: string;
  className?: string;
  iconClassName?: string;
  variant?: 'ghost' | 'outline' | 'badge';
  children?: React.ReactNode;
}

export const CopyButton: React.FC<CopyButtonProps> = ({
  value,
  label,
  toastMessage,
  title = 'Copy to clipboard',
  className = '',
  iconClassName = 'h-3.5 w-3.5',
  variant = 'outline',
  children,
}) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!value) return;
    const ok = await copyToClipboard(value);
    if (ok) {
      setCopied(true);
      toast.success(toastMessage || 'Copied to clipboard');
      setTimeout(() => setCopied(false), 1800);
    } else {
      toast.error('Failed to copy to clipboard');
    }
  };

  const defaultClasses =
    variant === 'outline'
      ? 'inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs text-zinc-300 hover:text-white hover:border-white/[0.15] cursor-pointer transition-colors'
      : variant === 'badge'
      ? 'inline-flex items-center gap-1.5 rounded-md border border-white/[0.08] bg-[#09090b] px-2 py-0.5 font-mono text-[11px] text-zinc-300 hover:border-orange-500/40 hover:text-orange-300 cursor-pointer transition-colors'
      : 'inline-flex items-center gap-1 rounded p-1 text-zinc-400 hover:text-zinc-200 cursor-pointer transition-colors';

  return (
    <button
      type="button"
      onClick={handleCopy}
      title={title}
      className={className ? className : defaultClasses}
    >
      {copied ? (
        <Check className={`${iconClassName} text-emerald-400 animate-in zoom-in-50 duration-150`} />
      ) : (
        <Copy className={`${iconClassName} text-zinc-400`} />
      )}
      {children ? children : label ? <span>{copied ? 'Copied!' : label}</span> : null}
    </button>
  );
};
