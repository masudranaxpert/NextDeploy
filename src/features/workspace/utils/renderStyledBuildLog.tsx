import React from 'react';

/**
 * Renders build output lines with terminal-style syntax highlighting and step annotations.
 */
export const renderStyledBuildLog = (text: string): React.ReactNode => {
  return text.split('\n').map((line, idx) => {
    if (!line) return <div key={idx} className="h-3" />;

    const timeMatch = line.match(/^(\d{4}-[A-Za-z]{3}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s*(.*)$/);
    const timestamp = timeMatch ? timeMatch[1] : null;
    const content = timeMatch ? timeMatch[2] : line;

    let contentEl: React.ReactNode = content;

    if (content.startsWith('#')) {
      const stepMatch = content.match(/^(#\d+)\s+(\[[^\]]+\])?\s*(.*)$/);
      if (stepMatch) {
        const stepNum = stepMatch[1];
        const target = stepMatch[2];
        const rest = stepMatch[3];
        contentEl = (
          <span>
            <span className="text-sky-400 font-semibold">{stepNum}</span>{' '}
            {target && <span className="text-amber-400/90">{target} </span>}
            <span className={rest.includes('DONE') ? 'text-emerald-400 font-medium' : rest.includes('CACHED') ? 'text-zinc-500' : 'text-zinc-300'}>
              {rest}
            </span>
          </span>
        );
      }
    } else if (content.includes('Starting Deploy') || content.includes('Compose command finished')) {
      contentEl = <span className="text-sky-300 font-semibold">{content}</span>;
    } else if (content.startsWith('---')) {
      contentEl = <span className="text-zinc-600 select-none">{content}</span>;
    } else if (content.includes('[zero-downtime]')) {
      contentEl = (
        <span>
          <span className="text-violet-400 font-semibold">[zero-downtime]</span>{' '}
          <span className="text-zinc-200">{content.replace('[zero-downtime]', '')}</span>
        </span>
      );
    } else if (content.includes('[pinned-mode]')) {
      contentEl = (
        <span>
          <span className="text-amber-400 font-semibold">[pinned-mode]</span>{' '}
          <span className="text-zinc-200">{content.replace('[pinned-mode]', '')}</span>
        </span>
      );
    } else if (content.includes('[readiness]')) {
      contentEl = (
        <span>
          <span className="text-emerald-400 font-semibold">[readiness]</span>{' '}
          <span className="text-zinc-200">{content.replace('[readiness]', '')}</span>
        </span>
      );
    } else if (content.includes('[connection-draining]')) {
      contentEl = (
        <span>
          <span className="text-amber-400 font-semibold">[connection-draining]</span>{' '}
          <span className="text-zinc-300">{content.replace('[connection-draining]', '')}</span>
        </span>
      );
    } else if (content.includes('Container ') && (content.includes('Creating') || content.includes('Starting') || content.includes('Started') || content.includes('Created'))) {
      contentEl = (
        <span>
          <span className="text-orange-400 font-medium">{content}</span>
        </span>
      );
    } else if (content.includes('✓') || content.includes('✔') || content.includes('Built')) {
      contentEl = <span className="text-emerald-400 font-medium">{content}</span>;
    } else if (content.toLowerCase().includes('error') || content.toLowerCase().includes('failed')) {
      contentEl = <span className="text-rose-400 font-medium">{content}</span>;
    } else if (content.toLowerCase().includes('warn')) {
      contentEl = <span className="text-amber-300">{content}</span>;
    }

    return (
      <div key={idx} className="flex items-start gap-2.5 font-mono text-[11px] leading-5 hover:bg-white/[0.02] px-1 py-0.5 rounded">
        {timestamp && (
          <span className="text-zinc-500/80 shrink-0 select-none text-[10px] font-mono">
            {timestamp}
          </span>
        )}
        <span className="break-all select-text">{contentEl}</span>
      </div>
    );
  });
};
