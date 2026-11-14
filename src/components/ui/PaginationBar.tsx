import React from 'react';

interface PaginationBarProps {
  page: number;
  pageSize: number;
  totalItems: number;
  onPageChange: (nextPage: number) => void;
  label?: string;
}

/**
 * Compact numbered pagination footer rendered only once items exceed `pageSize`.
 */
export const PaginationBar: React.FC<PaginationBarProps> = ({
  page,
  pageSize,
  totalItems,
  onPageChange,
  label = 'Pagination',
}) => {
  if (totalItems <= pageSize) return null;

  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
  const safePage = Math.min(Math.max(1, page), totalPages);
  const start = (safePage - 1) * pageSize + 1;
  const end = Math.min(totalItems, safePage * pageSize);

  const pages: Array<number | 'ellipsis'> = [];
  if (totalPages <= 7) {
    for (let i = 1; i <= totalPages; i++) pages.push(i);
  } else {
    const windowStart = Math.max(2, safePage - 1);
    const windowEnd = Math.min(totalPages - 1, safePage + 1);
    pages.push(1);
    if (windowStart > 2) pages.push('ellipsis');
    for (let i = windowStart; i <= windowEnd; i++) pages.push(i);
    if (windowEnd < totalPages - 1) pages.push('ellipsis');
    pages.push(totalPages);
  }

  return (
    <nav
      aria-label={label}
      className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.07] bg-[#09090b]/50 px-4 py-2.5 text-xs text-zinc-400"
    >
      <span className="font-mono text-[11px] tabular-nums">
        Showing {start}–{end} of {totalItems}
      </span>
      <div className="flex flex-wrap items-center gap-1.5">
        <button
          type="button"
          disabled={safePage <= 1}
          onClick={() => onPageChange(safePage - 1)}
          className="rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-400 disabled:opacity-40 disabled:pointer-events-none cursor-pointer"
        >
          Prev
        </button>
        {pages.map((entry, idx) =>
          entry === 'ellipsis' ? (
            <span key={`e-${idx}`} aria-hidden="true" className="px-1 font-mono text-zinc-400">
              …
            </span>
          ) : (
            <button
              key={entry}
              type="button"
              aria-current={entry === safePage ? 'page' : undefined}
              onClick={() => onPageChange(entry)}
              className={`h-7 min-w-[28px] rounded-md px-2 font-mono text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-400 cursor-pointer ${
                entry === safePage
                  ? 'bg-orange-500 text-zinc-950 font-semibold'
                  : 'border border-white/[0.08] bg-zinc-900 text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100'
              }`}
            >
              {entry}
            </button>
          )
        )}
        <button
          type="button"
          disabled={safePage >= totalPages}
          onClick={() => onPageChange(safePage + 1)}
          className="rounded-md border border-white/[0.08] bg-zinc-900 px-2.5 py-1 text-xs text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-400 disabled:opacity-40 disabled:pointer-events-none cursor-pointer"
        >
          Next
        </button>
      </div>
    </nav>
  );
};
