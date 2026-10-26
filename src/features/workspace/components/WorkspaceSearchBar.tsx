import React from 'react';
import { Search, X } from 'lucide-react';

interface WorkspaceSearchBarProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  onClear?: () => void;
}

export const WorkspaceSearchBar: React.FC<WorkspaceSearchBarProps> = ({
  value,
  onChange,
  placeholder = 'Filter...',
  className = '',
  onClear,
}) => {
  return (
    <div className={`relative w-full ${className}`}>
      <Search className="pointer-events-none absolute left-3 top-2.5 h-3.5 w-3.5 text-zinc-500" />
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-9 w-full rounded-lg border border-white/[0.08] bg-[#09090b] pl-9 pr-8 text-xs text-zinc-100 placeholder-zinc-500 focus:border-orange-500 focus:outline-none transition-colors"
      />
      {value && (
        <button
          type="button"
          onClick={() => {
            onChange('');
            onClear?.();
          }}
          className="absolute right-2.5 top-2.5 text-zinc-500 hover:text-zinc-300 transition-colors cursor-pointer"
        >
          <X className="h-4 w-4" />
        </button>
      )}
    </div>
  );
};
