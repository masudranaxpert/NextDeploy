import React, { useState } from 'react';
import { Copy, Check } from 'lucide-react';

interface MarkdownContentProps {
  content: string;
  isStreaming?: boolean;
}

export const MarkdownContent: React.FC<MarkdownContentProps> = ({ content, isStreaming }) => {
  if (!content) {
    if (isStreaming) {
      return (
        <span className="inline-flex items-center gap-1 text-xs text-zinc-500 py-1">
          <span className="inline-block size-1.5 rounded-full bg-orange-400 animate-pulse" />
          <span>Generating response...</span>
        </span>
      );
    }
    return null;
  }

  // Split content by code blocks: ```lang ... ```
  const parts = content.split(/(```[\s\S]*?```)/g);

  return (
    <div data-slot="markdown" className="space-y-2 text-xs leading-relaxed text-zinc-200">
      {parts.map((part, index) => {
        if (part.startsWith('```') && part.endsWith('```')) {
          // Extract language and code
          const firstLineBreak = part.indexOf('\n');
          const lang = part.slice(3, firstLineBreak).trim() || 'text';
          const code = part.slice(firstLineBreak + 1, -3);

          return <CodeBlock key={index} code={code} lang={lang} />;
        }

        // Render normal text with inline bold, code, bullets
        return <TextParagraph key={index} text={part} />;
      })}
      {isStreaming && (
        <span className="inline-block h-3.5 w-1.5 ml-0.5 bg-orange-400 animate-pulse align-middle" />
      )}
    </div>
  );
};

interface CodeBlockProps {
  code: string;
  lang: string;
}

const CodeBlock: React.FC<CodeBlockProps> = ({ code, lang }) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      data-slot="code-block"
      className="my-2.5 overflow-hidden rounded-xl border border-white/[0.08] bg-[#090a0c] font-mono text-xs shadow-xs"
    >
      <div className="flex items-center justify-between border-b border-white/[0.06] bg-white/[0.02] px-3 py-1.5 text-[11px] text-zinc-400">
        <span className="font-medium text-zinc-300">{lang}</span>
        <button
          type="button"
          onClick={handleCopy}
          className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-300 transition-colors"
          title="Copy code"
        >
          {copied ? (
            <>
              <Check className="size-3 text-emerald-400" />
              <span className="text-[10px] text-emerald-400">Copied</span>
            </>
          ) : (
            <>
              <Copy className="size-3" />
              <span className="text-[10px]">Copy</span>
            </>
          )}
        </button>
      </div>
      <pre className="overflow-x-auto p-3 text-zinc-200 selection:bg-orange-500/30 leading-snug">
        <code>{code}</code>
      </pre>
    </div>
  );
};

const TextParagraph: React.FC<{ text: string }> = ({ text }) => {
  const lines = text.split('\n');

  return (
    <div className="space-y-1">
      {lines.map((line, i) => {
        const trimmed = line.trim();
        if (!trimmed) return <div key={i} className="h-1" />;

        // Header ###
        if (line.startsWith('### ')) {
          return (
            <h4 key={i} className="font-semibold text-white pt-2 pb-1 text-[13px]">
              {line.slice(4)}
            </h4>
          );
        }
        if (line.startsWith('## ')) {
          return (
            <h3 key={i} className="font-semibold text-white pt-2.5 pb-1 text-sm">
              {line.slice(3)}
            </h3>
          );
        }

        // Bullet point -
        if (line.trim().startsWith('- ') || line.trim().startsWith('* ')) {
          const itemText = line.trim().slice(2);
          return (
            <div key={i} className="flex items-start gap-2 pl-2">
              <span className="text-orange-400 select-none">•</span>
              <div>{renderInline(itemText)}</div>
            </div>
          );
        }

        return <div key={i}>{renderInline(line)}</div>;
      })}
    </div>
  );
};

function renderInline(str: string): React.ReactNode {
  // Regex match bold **text** or inline `code`
  const parts = str.split(/(\*\*.*?\*\*|`.*?`)/g);

  return parts.map((part, index) => {
    if (part.startsWith('**') && part.endsWith('**')) {
      return (
        <strong key={index} className="font-semibold text-white">
          {part.slice(2, -2)}
        </strong>
      );
    }
    if (part.startsWith('`') && part.endsWith('`')) {
      return (
        <code
          key={index}
          className="rounded border border-white/[0.08] bg-white/[0.04] px-1 py-0.5 font-mono text-[11px] text-orange-300"
        >
          {part.slice(1, -1)}
        </code>
      );
    }
    return part;
  });
}
