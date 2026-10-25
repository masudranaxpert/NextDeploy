import React, { useEffect, useMemo, useState } from 'react';
import { ChevronRight, Folder, Search, X } from 'lucide-react';

// Collapsible folder tree for target-directory pickers: renders only the expanded
// branch instead of dumping every folder of the site into one flat <select>.
// Typing in the search box switches to a flat list of matching paths, so huge
// trees (a WordPress site has hundreds of folders) stay reachable in one step.
export const DirTreePicker: React.FC<{
  rootLabel: string;
  paths: string[];
  value: string;
  onChange: (dir: string) => void;
}> = ({ rootLabel, paths, value, onChange }) => {
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [query, setQuery] = useState('');

  const childrenOf = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const p of paths) {
      const parts = p.split('/');
      const parent = parts.slice(0, -1).join('/');
      if (!map.has(parent)) map.set(parent, []);
      map.get(parent)!.push(parts[parts.length - 1]);
    }
    for (const list of map.values()) list.sort((a, b) => a.localeCompare(b));
    return map;
  }, [paths]);

  // ponytail: flat render cap at 200 hits — typing narrows it; raise only if a real site needs more
  const search = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return null;
    const hits = paths.filter((p) => p.toLowerCase().includes(q));
    return { shown: hits.slice(0, 200), total: hits.length };
  }, [paths, query]);

  // keep the selected folder visible when it changes from outside (Root/Current buttons)
  useEffect(() => {
    setExpanded((prev) => {
      const next = new Set(prev);
      for (let i = 1; i < value.split('/').length; i++) next.add(value.split('/').slice(0, i).join('/'));
      return next.size === prev.size ? prev : next;
    });
  }, [value]); // eslint-disable-line react-hooks/exhaustive-deps

  const toggle = (dir: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(dir)) next.delete(dir);
      else next.add(dir);
      return next;
    });

  const row = (label: string, dir: string, depth: number, hasKids: boolean) => {
    const selected = value === dir;
    return (
      <div
        className={`flex items-center gap-0.5 rounded-lg pr-2 ${selected ? 'bg-orange-500/15' : 'hover:bg-white/[0.05]'}`}
        style={{ paddingLeft: depth * 14 }}
      >
        <button
          type="button"
          onClick={() => toggle(dir)}
          className="flex h-6 w-5 shrink-0 cursor-pointer items-center justify-center text-zinc-500 hover:text-zinc-200"
          aria-label={expanded.has(dir) ? 'Collapse' : 'Expand'}
        >
          <ChevronRight
            className={`h-3.5 w-3.5 transition-transform ${expanded.has(dir) ? 'rotate-90' : ''} ${hasKids ? '' : 'invisible'}`}
          />
        </button>
        <button
          type="button"
          onClick={() => onChange(dir)}
          className={`flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 py-1.5 text-left font-mono text-xs ${selected ? 'font-semibold text-orange-200' : 'text-zinc-300'}`}
        >
          <Folder className={`h-3.5 w-3.5 shrink-0 ${selected ? 'text-orange-300' : 'text-amber-400/70'}`} />
          <span className="truncate">{label}</span>
        </button>
      </div>
    );
  };

  const renderLevel = (parent: string, depth: number): React.ReactNode =>
    (childrenOf.get(parent) ?? []).map((name) => {
      const full = parent ? `${parent}/${name}` : name;
      return (
        <div key={full}>
          {row(name, full, depth, childrenOf.has(full))}
          {expanded.has(full) && renderLevel(full, depth + 1)}
        </div>
      );
    });

  return (
    <div className="rounded-lg border border-white/[0.09] bg-[#09090b] p-1.5 focus-within:border-amber-500/60">
      <div className="relative mb-1">
        <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search folders…"
          className="w-full rounded-md border border-white/[0.06] bg-[#111115] py-1.5 pl-7 pr-7 font-mono text-xs text-zinc-100 placeholder-zinc-500 focus:border-amber-500/60 focus:outline-none"
        />
        {query && (
          <button
            type="button"
            onClick={() => setQuery('')}
            aria-label="Clear search"
            className="absolute right-1.5 top-1/2 -translate-y-1/2 cursor-pointer text-zinc-500 hover:text-zinc-200"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      {search ? (
        <div className="max-h-64 overflow-y-auto">
          {search.shown.length === 0 && (
            <div className="px-2 py-3 text-center font-mono text-[11px] text-zinc-500">No folders match “{query.trim()}”.</div>
          )}
          {search.shown.map((full) =>
            row(`/${full}`, full, 0, false)
          )}
          {search.total > search.shown.length && (
            <div className="px-2 py-1.5 text-center font-mono text-[10px] text-zinc-500">
              …{search.total - search.shown.length} more — keep typing to narrow down
            </div>
          )}
        </div>
      ) : (
        <div className="max-h-64 overflow-y-auto">
          {row(rootLabel, '', 0, childrenOf.has(''))}
          {expanded.has('') && renderLevel('', 1)}
        </div>
      )}
    </div>
  );
};
