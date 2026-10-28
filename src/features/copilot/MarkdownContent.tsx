import React, { useState } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { CheckIcon, CopyIcon } from 'lucide-react';

import { cn } from '@/lib/utils';

interface MarkdownContentProps {
  content: string;
  isStreaming?: boolean;
}

function CodeBlock({ code, lang }: { code: string; lang: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard?.writeText(code).then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1800);
    });
  };
  return (
    <div data-slot="code-block" className="glass-panel my-2 overflow-hidden rounded-md font-mono text-[12.5px]">
      <div className="flex items-center justify-between border-b border-border/70 px-3 py-1 text-[11px] text-muted-foreground">
        <span>{lang || 'text'}</span>
        <button
          type="button"
          onClick={copy}
          aria-label="Copy code"
          className="inline-flex h-7 items-center gap-1.5 rounded-sm px-2 outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:bg-accent"
        >
          {copied ? <CheckIcon className="size-3.5 text-emerald-400" /> : <CopyIcon className="size-3.5" />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre className="scrollbar-thin max-w-full overflow-x-auto px-3 py-2.5 leading-[1.55]">
        <code>{code}</code>
      </pre>
    </div>
  );
}

const components: Components = {
  p: ({ children }) => <p className="my-2 first:mt-0 last:mb-0">{children}</p>,
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noopener noreferrer" className="text-orange-400 underline underline-offset-2 hover:text-orange-300">
      {children}
    </a>
  ),
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  em: ({ children }) => <em className="italic">{children}</em>,
  h1: ({ children }) => <h1 className="mt-4 mb-2 text-base font-semibold text-foreground">{children}</h1>,
  h2: ({ children }) => <h2 className="mt-4 mb-1.5 text-[15px] font-semibold text-foreground">{children}</h2>,
  h3: ({ children }) => <h3 className="mt-3 mb-1 text-sm font-semibold text-foreground">{children}</h3>,
  h4: ({ children }) => <h4 className="mt-3 mb-1 text-[13px] font-semibold text-foreground">{children}</h4>,
  ul: ({ children }) => <ul className="my-2 list-disc space-y-1 pl-5 marker:text-orange-400/80">{children}</ul>,
  ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 pl-5 marker:text-orange-400/80">{children}</ol>,
  li: ({ children }) => <li className="pl-0.5">{children}</li>,
  blockquote: ({ children }) => (
    <blockquote className="my-2 border-l-2 border-orange-500/40 pl-3 text-muted-foreground">{children}</blockquote>
  ),
  hr: () => <hr className="my-4 border-border/70" />,
  img: ({ src, alt }) => <img src={src} alt={alt} loading="lazy" className="my-2 max-h-96 max-w-full rounded-md" />,
  table: ({ children }) => (
    <div className="scrollbar-thin glass-panel my-2 max-w-full overflow-x-auto rounded-md">
      <table className="w-full min-w-max border-collapse text-left text-[12.5px]">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="border-b border-border/70 text-muted-foreground">{children}</thead>,
  th: ({ children }) => <th className="px-3 py-1.5 font-medium whitespace-nowrap">{children}</th>,
  td: ({ children }) => <td className="border-t border-border/40 px-3 py-1.5 align-top">{children}</td>,
  // Fenced blocks arrive as <pre><code class="language-x">; inline code has no <pre>.
  pre: ({ children }) => {
    const child = React.Children.toArray(children)[0] as React.ReactElement<{ className?: string; children?: React.ReactNode }> | undefined;
    const lang = /language-([\w-]+)/.exec(child?.props?.className || '')?.[1] || '';
    const code = String(child?.props?.children ?? '').replace(/\n$/, '');
    return <CodeBlock code={code} lang={lang} />;
  },
  code: ({ className, children }) => (
    <code className={cn('rounded border border-orange-500/25 bg-orange-500/[0.08] px-1.5 py-0.5 font-mono text-[0.9em] text-orange-300 break-words', className)}>
      {children}
    </code>
  ),
};

export const MarkdownContent: React.FC<MarkdownContentProps> = ({ content, isStreaming }) => {
  if (!content) {
    if (isStreaming) {
      return (
        <span className="inline-flex items-center gap-1.5 py-1 text-xs text-muted-foreground">
          <span className="inline-block size-1.5 animate-pulse rounded-full bg-sky-400" />
          <span className="status-shimmer">Working</span>
        </span>
      );
    }
    return null;
  }

  return (
    <div data-slot="markdown" className="min-w-0 max-w-full text-[13.5px] leading-relaxed [overflow-wrap:anywhere] text-foreground/90">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
      {isStreaming && <span className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-sky-400 align-middle" />}
    </div>
  );
};
