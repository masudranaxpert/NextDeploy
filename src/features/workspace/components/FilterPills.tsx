import React from 'react';

export interface FilterPillOption<T extends string> {
  id: T;
  label: string;
  count?: number;
}

interface FilterPillsProps<T extends string> {
  options: readonly FilterPillOption<T>[];
  value: T;
  onChange: (value: T) => void;
  className?: string;
}

export function FilterPills<T extends string>({
  options,
  value,
  onChange,
  className = '',
}: FilterPillsProps<T>) {
  return (
    <div
      className={`grid grid-cols-${options.length} gap-1 rounded-lg border border-white/[0.07] bg-[#09090b] p-1 sm:flex sm:items-center sm:gap-0.5 sm:p-0.5 ${className}`}
      style={{
        gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))`,
      }}
    >
      {options.map((opt) => {
        const isActive = value === opt.id;
        return (
          <button
            key={opt.id}
            type="button"
            onClick={() => onChange(opt.id)}
            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors text-center cursor-pointer flex items-center justify-center gap-1.5 ${
              isActive
                ? 'bg-zinc-800 text-zinc-100 font-semibold'
                : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            <span>{opt.label}</span>
            {opt.count !== undefined && (
              <span
                className={`font-mono text-[10px] ${
                  isActive ? 'text-zinc-300' : 'text-zinc-600'
                }`}
              >
                ({opt.count})
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
