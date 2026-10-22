import React, { useRef, useState } from 'react';
import {
  Archive,
  Box,
  Check,
  Copy,
  Database,
  Download,
  FileCode2,
  FileText,
  Globe,
  Layers,
  Lock,
  Save,
  Terminal,
  WrapText,
} from 'lucide-react';
import { toast } from 'sonner';

export type SupportedEditorLanguage =
  | 'yaml'
  | 'dockerfile'
  | 'env'
  | 'json'
  | 'sh'
  | 'nginx'
  | 'typescript'
  | 'javascript'
  | 'python'
  | 'go'
  | 'rust'
  | 'php'
  | 'sql'
  | 'markdown';

interface CodeEditorBoxProps {
  filename: string;
  language?: SupportedEditorLanguage;
  value: string;
  onChange: (next: string) => void;
  onSave?: () => void;
  height?: number | string;
  readOnly?: boolean;
  actions?: React.ReactNode;
  permissions?: string;
}

/**
 * Detects the effective programming/config language from filename and fallback prop
 */
export function detectLanguageFromFilename(
  filename: string,
  fallback: SupportedEditorLanguage = 'yaml'
): SupportedEditorLanguage {
  const base = (filename.split('/').pop() || filename).toLowerCase();
  if (base === 'dockerfile' || base.endsWith('.dockerfile')) return 'dockerfile';
  if (base === 'caddyfile' || base.endsWith('.conf') || base === 'nginx.conf') return 'nginx';
  if (base.startsWith('.env') || base.endsWith('.env')) return 'env';
  if (base.endsWith('.py') || base === 'requirements.txt' || base === 'pipfile') return 'python';
  if (base.endsWith('.go') || base === 'go.mod' || base === 'go.sum') return 'go';
  if (base.endsWith('.rs') || base === 'cargo.toml' || base === 'cargo.lock') return 'rust';
  if (base.endsWith('.php') || base === 'composer.json') return 'php';
  if (base.endsWith('.ts') || base.endsWith('.tsx')) return 'typescript';
  if (base.endsWith('.js') || base.endsWith('.jsx') || base.endsWith('.mjs') || base.endsWith('.cjs'))
    return 'javascript';
  if (base.endsWith('.sql') || base.endsWith('.prisma')) return 'sql';
  if (base.endsWith('.json')) return 'json';
  if (base.endsWith('.sh') || base.endsWith('.bash') || base.endsWith('.zsh')) return 'sh';
  if (base.endsWith('.yml') || base.endsWith('.yaml')) return 'yaml';
  if (base.endsWith('.md') || base.endsWith('.txt')) return 'markdown';
  return fallback;
}

/**
 * Rich, authentic Developer File & Language Icon component
 * Supports Python, Go, Rust, PHP, TypeScript, JavaScript, Docker, Compose, Shell, SQL, JSON, Nginx/Caddy, Env, and Archives
 */
export const DevFileIcon: React.FC<{ filename: string; className?: string }> = ({
  filename,
  className = 'h-4 w-4',
}) => {
  const base = (filename.split('/').pop() || filename).toLowerCase();

  // Archive files (.zip, .tar.gz, .tgz, .tar)
  if (
    base.endsWith('.zip') ||
    base.endsWith('.tar.gz') ||
    base.endsWith('.tgz') ||
    base.endsWith('.tar')
  ) {
    return <Archive className={`${className} text-purple-400 shrink-0`} />;
  }

  // Python (.py)
  if (base.endsWith('.py') || base === 'requirements.txt' || base === 'pyproject.toml') {
    return (
      <span
        className="inline-flex h-4 w-4 items-center justify-center rounded-[4px] bg-[#3776AB]/20 font-mono text-[9px] font-extrabold text-[#FFD43B] ring-1 ring-[#3776AB]/50 shrink-0"
        title="Python"
      >
        Py
      </span>
    );
  }

  // Go (.go, go.mod)
  if (base.endsWith('.go') || base === 'go.mod' || base === 'go.sum') {
    return (
      <span
        className="inline-flex h-4 w-4 items-center justify-center rounded-[4px] bg-[#00ADD8]/20 font-mono text-[9px] font-extrabold text-[#29BEB0] ring-1 ring-[#00ADD8]/50 shrink-0"
        title="Go (Golang)"
      >
        GO
      </span>
    );
  }

  // Rust (.rs, Cargo.toml)
  if (base.endsWith('.rs') || base === 'cargo.toml' || base === 'cargo.lock') {
    return (
      <span
        className="inline-flex h-4 w-4 items-center justify-center rounded-[4px] bg-[#DEA584]/20 font-mono text-[9px] font-extrabold text-[#F74C00] ring-1 ring-[#F74C00]/45 shrink-0"
        title="Rust"
      >
        RS
      </span>
    );
  }

  // PHP (.php, composer.json)
  if (base.endsWith('.php')) {
    return (
      <span
        className="inline-flex h-4 w-4 items-center justify-center rounded-[4px] bg-[#777BB4]/25 font-mono text-[8px] font-extrabold text-[#AEB2D5] ring-1 ring-[#777BB4]/50 shrink-0"
        title="PHP"
      >
        php
      </span>
    );
  }

  // TypeScript (.ts, .tsx)
  if (base.endsWith('.ts') || base.endsWith('.tsx')) {
    return (
      <span
        className="inline-flex h-4 w-4 items-center justify-center rounded-[4px] bg-[#3178C6]/25 font-mono text-[9px] font-extrabold text-[#61AFEF] ring-1 ring-[#3178C6]/55 shrink-0"
        title="TypeScript"
      >
        TS
      </span>
    );
  }

  // JavaScript (.js, .jsx, .mjs)
  if (
    base.endsWith('.js') ||
    base.endsWith('.jsx') ||
    base.endsWith('.mjs') ||
    base.endsWith('.cjs')
  ) {
    return (
      <span
        className="inline-flex h-4 w-4 items-center justify-center rounded-[4px] bg-[#F7DF1E]/20 font-mono text-[9px] font-extrabold text-[#F7DF1E] ring-1 ring-[#F7DF1E]/45 shrink-0"
        title="JavaScript"
      >
        JS
      </span>
    );
  }

  // Dockerfile
  if (base === 'dockerfile' || base.endsWith('.dockerfile')) {
    return <Box className={`${className} text-sky-400 shrink-0`} />;
  }

  // Docker Compose / YAML
  if (base.endsWith('.yml') || base.endsWith('.yaml')) {
    return <Layers className={`${className} text-sky-400 shrink-0`} />;
  }

  // Environment / Secrets
  if (base.startsWith('.env') || base.endsWith('.env') || base.endsWith('.pem')) {
    return <Lock className={`${className} text-emerald-400 shrink-0`} />;
  }

  // SQL / Database
  if (base.endsWith('.sql') || base.endsWith('.prisma')) {
    return <Database className={`${className} text-cyan-400 shrink-0`} />;
  }

  // JSON
  if (base.endsWith('.json')) {
    return (
      <span
        className="inline-flex h-4 w-4 items-center justify-center rounded-[4px] bg-amber-500/15 font-mono text-[9px] font-bold text-amber-300 ring-1 ring-amber-500/35 shrink-0"
        title="JSON"
      >
        {'{}'}
      </span>
    );
  }

  // Shell / Bash
  if (base.endsWith('.sh') || base.endsWith('.bash')) {
    return <Terminal className={`${className} text-violet-400 shrink-0`} />;
  }

  // Caddyfile / Nginx
  if (base === 'caddyfile' || base.endsWith('.conf')) {
    return <Globe className={`${className} text-teal-400 shrink-0`} />;
  }

  // Default fallback
  if (base.endsWith('.md')) {
    return <FileCode2 className={`${className} text-sky-300 shrink-0`} />;
  }

  return <FileText className={`${className} text-zinc-400 shrink-0`} />;
};

const CODE_KEYWORDS = new Set([
  // TS / JS
  'import',
  'from',
  'export',
  'default',
  'const',
  'let',
  'var',
  'function',
  'async',
  'await',
  'return',
  'if',
  'else',
  'for',
  'while',
  'switch',
  'case',
  'break',
  'continue',
  'new',
  'try',
  'catch',
  'finally',
  'throw',
  'type',
  'interface',
  'extends',
  'implements',
  // Python
  'def',
  'class',
  'elif',
  'except',
  'pass',
  'raise',
  'with',
  'as',
  'yield',
  'lambda',
  'in',
  'is',
  'not',
  'and',
  'or',
  // Go
  'package',
  'func',
  'struct',
  'go',
  'defer',
  'chan',
  'select',
  'range',
  'map',
  'fallthrough',
  // Rust
  'fn',
  'pub',
  'mut',
  'impl',
  'trait',
  'enum',
  'match',
  'use',
  'mod',
  'crate',
  'where',
  'unsafe',
  // PHP
  'namespace',
  'echo',
  'require',
  'include',
  'public',
  'private',
  'protected',
  'static',
  'array',
  // SQL / Shell
  'SELECT',
  'FROM',
  'WHERE',
  'INSERT',
  'INTO',
  'VALUES',
  'UPDATE',
  'SET',
  'DELETE',
  'CREATE',
  'TABLE',
  'INDEX',
  'JOIN',
  'LEFT',
  'INNER',
  'ON',
  'ORDER',
  'BY',
  'LIMIT',
  'then',
  'fi',
  'do',
  'done',
  'exec',
]);

const CODE_TYPES_AND_BUILTINS = new Set([
  'string',
  'number',
  'boolean',
  'any',
  'void',
  'never',
  'unknown',
  'Record',
  'Promise',
  'FastifyInstance',
  'Pool',
  'process',
  'console',
  'Number',
  'String',
  'Object',
  'Array',
  'int',
  'float',
  'str',
  'bool',
  'dict',
  'list',
  'tuple',
  'set',
  'Optional',
  'List',
  'Dict',
  'error',
  'Context',
  'http',
  'fmt',
  'os',
  'time',
  'json',
  'log',
  'Result',
  'Option',
  'Vec',
  'Box',
  'Arc',
  'u8',
  'u16',
  'u32',
  'u64',
  'usize',
  'i32',
  'i64',
  'f64',
  'Self',
  'self',
]);

const CODE_LITERALS = new Set([
  'true',
  'false',
  'null',
  'undefined',
  'True',
  'False',
  'None',
  'nil',
  'Ok',
  'Err',
  'Some',
]);

/**
 * Tokenizes a line of source code (Python, Go, Rust, PHP, TS/JS, SQL, Shell, Nginx) into colored spans
 */
function renderSourceCodeTokens(line: string): React.ReactNode {
  const tokenRegex =
    /("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`|\$[a-zA-Z_][a-zA-Z0-9_]*|@[a-zA-Z_][a-zA-Z0-9_.]*|#\[[^\]]+\]|\b\d+(?:\.\d+)?(?:ms|s|m|h|M|G|KB|MB|GB)?\b|\b[A-Za-z_][A-Za-z0-9_]*\b|[{}()[\].,:;+-/*=<>!&|?~]+)/g;

  const parts: React.ReactNode[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = tokenRegex.exec(line)) !== null) {
    if (match.index > lastIndex) {
      parts.push(line.slice(lastIndex, match.index));
    }
    const token = match[0];
    const nextChar = line[tokenRegex.lastIndex] || '';
    const key = `${match.index}-${token}`;

    if (
      (token.startsWith('"') && token.endsWith('"')) ||
      (token.startsWith("'") && token.endsWith("'")) ||
      (token.startsWith('`') && token.endsWith('`'))
    ) {
      parts.push(
        <span key={key} className="text-emerald-300">
          {token}
        </span>
      );
    } else if (token.startsWith('@') || token.startsWith('#[')) {
      parts.push(
        <span key={key} className="text-pink-400 font-medium">
          {token}
        </span>
      );
    } else if (token.startsWith('$')) {
      parts.push(
        <span key={key} className="text-orange-300">
          {token}
        </span>
      );
    } else if (/^\d/.test(token)) {
      parts.push(
        <span key={key} className="text-amber-300">
          {token}
        </span>
      );
    } else if (CODE_KEYWORDS.has(token)) {
      parts.push(
        <span key={key} className="text-purple-400 font-semibold">
          {token}
        </span>
      );
    } else if (CODE_LITERALS.has(token)) {
      parts.push(
        <span key={key} className="text-orange-400 font-semibold">
          {token}
        </span>
      );
    } else if (CODE_TYPES_AND_BUILTINS.has(token) || /^[A-Z][A-Za-z0-9_]*$/.test(token)) {
      parts.push(
        <span key={key} className="text-cyan-300">
          {token}
        </span>
      );
    } else if (/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(token) && nextChar === '(') {
      parts.push(
        <span key={key} className="text-sky-300">
          {token}
        </span>
      );
    } else if (/^[{}()[\].,:;+-/*=<>!&|?~]+$/.test(token)) {
      parts.push(
        <span key={key} className="text-zinc-400">
          {token}
        </span>
      );
    } else {
      parts.push(
        <span key={key} className="text-zinc-100">
          {token}
        </span>
      );
    }

    lastIndex = tokenRegex.lastIndex;
  }

  if (lastIndex < line.length) {
    parts.push(line.slice(lastIndex));
  }

  return parts;
}

/**
 * Lightweight, zero-dependency syntax highlighter for Compose YAML, Dockerfile, ENV, JSON, Shell, Nginx/Caddy,
 * plus full polyglot support for TypeScript, JavaScript, Python, Go, Rust, PHP, and SQL.
 */
function renderHighlightedLine(line: string, lang: SupportedEditorLanguage): React.ReactNode {
  if (!line) return '\u00A0';

  const trimmed = line.trimStart();

  // Shebang
  if (trimmed.startsWith('#!')) {
    return <span className="text-pink-400 font-semibold">{line}</span>;
  }

  // Full-line comments (# or // or --)
  if (
    (trimmed.startsWith('#') && !trimmed.startsWith('#[')) ||
    trimmed.startsWith('//') ||
    (lang === 'sql' && trimmed.startsWith('--'))
  ) {
    return <span className="text-zinc-500 italic">{line}</span>;
  }

  // PHP opening tag
  if (trimmed.startsWith('<?php') || trimmed.startsWith('?>')) {
    return <span className="text-pink-400 font-bold">{line}</span>;
  }

  if (lang === 'env') {
    const eqIdx = line.indexOf('=');
    if (eqIdx > 0) {
      const key = line.slice(0, eqIdx);
      const val = line.slice(eqIdx + 1);
      return (
        <>
          <span className="text-orange-300 font-semibold">{key}</span>
          <span className="text-zinc-500">=</span>
          <span className="text-emerald-300">{val}</span>
        </>
      );
    }
    return <span className="text-zinc-300">{line}</span>;
  }

  if (lang === 'dockerfile') {
    const match = line.match(
      /^(\s*)(FROM|RUN|COPY|ADD|WORKDIR|ENV|ARG|EXPOSE|CMD|ENTRYPOINT|USER|HEALTHCHECK|VOLUME|LABEL)\b(.*)$/i
    );
    if (match) {
      const [, indent, instr, rest] = match;
      return (
        <>
          {indent}
          <span className="text-purple-400 font-bold">{instr}</span>
          <span className="text-zinc-200">{ renderSourceCodeTokens(rest) }</span>
        </>
      );
    }
    return <span className="text-zinc-300">{renderSourceCodeTokens(line)}</span>;
  }

  if (lang === 'yaml') {
    const listMatch = line.match(/^(\s*-\s+)(.*)$/);
    if (listMatch) {
      const [, bullet, rest] = listMatch;
      const kv = rest.match(/^([a-zA-Z0-9_.-]+)(:\s*)(.*)$/);
      if (kv) {
        return (
          <>
            <span className="text-orange-400">{bullet}</span>
            <span className="text-sky-300">{kv[1]}</span>
            <span className="text-zinc-500">{kv[2]}</span>
            <span className="text-emerald-300">{kv[3]}</span>
          </>
        );
      }
      return (
        <>
          <span className="text-orange-400">{bullet}</span>
          <span className="text-amber-200">{rest}</span>
        </>
      );
    }

    const kvMatch = line.match(/^(\s*)([a-zA-Z0-9_.-]+)(:\s*)(.*)$/);
    if (kvMatch) {
      const [, indent, key, colon, val] = kvMatch;
      const isBoolOrNum = /^(true|false|null|\d+(\.\d+)?)$/i.test(val.trim());
      return (
        <>
          {indent}
          <span className="text-sky-300 font-medium">{key}</span>
          <span className="text-zinc-500">{colon}</span>
          <span className={isBoolOrNum ? 'text-purple-300 font-semibold' : 'text-emerald-300'}>
            {val}
          </span>
        </>
      );
    }
    return <span className="text-zinc-300">{line}</span>;
  }

  if (lang === 'json') {
    const jsonKv = line.match(/^(\s*)(".*?")(:\s*)(.*)$/);
    if (jsonKv) {
      const [, indent, key, colon, val] = jsonKv;
      const isNumOrBool = /^(true|false|null|-?\d+(\.\d+)?),?$/i.test(val.trim());
      return (
        <>
          {indent}
          <span className="text-sky-300">{key}</span>
          <span className="text-zinc-500">{colon}</span>
          <span className={isNumOrBool ? 'text-amber-300' : 'text-emerald-300'}>{val}</span>
        </>
      );
    }
    return <span className="text-zinc-400">{line}</span>;
  }

  // Polyglot Source Code (TypeScript, JavaScript, Python, Go, Rust, PHP, SQL, Shell, Nginx/Caddy)
  const commentIdx =
    lang === 'python' || lang === 'sh' || lang === 'nginx'
      ? line.indexOf(' #')
      : line.indexOf('//');

  if (commentIdx !== -1) {
    const codePart = line.slice(0, commentIdx);
    const commentPart = line.slice(commentIdx);
    return (
      <>
        {renderSourceCodeTokens(codePart)}
        <span className="text-zinc-500 italic">{commentPart}</span>
      </>
    );
  }

  return renderSourceCodeTokens(line);
}

export const CodeEditorBox: React.FC<CodeEditorBoxProps> = ({
  filename,
  language = 'yaml',
  value,
  onChange,
  onSave,
  height = 420,
  readOnly = false,
  actions,
  permissions = '-rw-r--r-- (0644)',
}) => {
  const [wordWrap, setWordWrap] = useState(true);
  const [copied, setCopied] = useState(false);
  const [cursorLine, setCursorLine] = useState(1);
  const [fontSize, setFontSize] = useState<'xs' | 'sm'>('xs');

  const preRef = useRef<HTMLPreElement>(null);
  const gutterRef = useRef<HTMLDivElement>(null);

  const effectiveLanguage = detectLanguageFromFilename(filename, language);
  const lines = value.split('\n');
  const lineCount = Math.max(lines.length, 1);

  const handleScroll = (e: React.UIEvent<HTMLTextAreaElement>) => {
    const { scrollTop, scrollLeft } = e.currentTarget;
    if (preRef.current) {
      preRef.current.scrollTop = scrollTop;
      preRef.current.scrollLeft = scrollLeft;
    }
    if (gutterRef.current) {
      gutterRef.current.scrollTop = scrollTop;
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Tab') {
      e.preventDefault();
      const target = e.currentTarget;
      const start = target.selectionStart;
      const end = target.selectionEnd;
      const nextValue = value.substring(0, start) + '  ' + value.substring(end);
      onChange(nextValue);
      requestAnimationFrame(() => {
        target.selectionStart = target.selectionEnd = start + 2;
      });
    } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
      e.preventDefault();
      if (onSave) {
        onSave();
      } else {
        toast.success(`Saved ${filename}`);
      }
    }
  };

  const updateCursorLine = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
    const target = e.currentTarget;
    const textBefore = target.value.slice(0, target.selectionStart);
    setCursorLine(textBefore.split('\n').length);
  };

  const handleCopy = () => {
    navigator.clipboard?.writeText(value);
    setCopied(true);
    toast.success(`Copied ${filename}`);
    setTimeout(() => setCopied(false), 1500);
  };

  const handleDownload = () => {
    const blob = new Blob([value], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename.split('/').pop() || filename;
    a.click();
    URL.revokeObjectURL(url);
    toast.success(`Downloaded ${filename}`);
  };

  const fontSizeClass = fontSize === 'xs' ? 'text-xs leading-6' : 'text-[13px] leading-6';

  return (
    <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-[#09090b] shadow-xl">
      {/* Editor Header Bar */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.07] bg-[#0e0e12] px-3 sm:px-3.5 py-2">
        <div className="flex items-center gap-2 min-w-0">
          <div className="hidden sm:flex items-center gap-1.5 mr-1">
            <span className="h-2.5 w-2.5 rounded-full bg-rose-500/70" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-500/70" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/70" />
          </div>
          <DevFileIcon filename={filename} />
          <span className="truncate font-mono text-xs font-semibold text-zinc-100">
            {filename}
          </span>
          <span className="rounded bg-white/[0.06] px-1.5 py-0.5 font-mono text-[10px] uppercase text-zinc-300 shrink-0">
            {effectiveLanguage}
          </span>
          <span className="hidden md:inline font-mono text-[10px] text-zinc-500 shrink-0">
            {lineCount} lines · {new Blob([value]).size} B
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-1.5">
          <button
            type="button"
            onClick={() => setFontSize((f) => (f === 'xs' ? 'sm' : 'xs'))}
            className="inline-flex h-7 items-center rounded-lg border border-white/[0.08] bg-zinc-900 px-2 font-mono text-[10px] text-zinc-400 hover:text-zinc-200 cursor-pointer"
            title="Toggle font size"
          >
            {fontSize === 'xs' ? '12px' : '13px'}
          </button>

          <button
            type="button"
            onClick={() => setWordWrap((w) => !w)}
            className={`inline-flex h-7 items-center gap-1 rounded-lg border px-2 text-[11px] font-medium transition-colors cursor-pointer ${
              wordWrap
                ? 'border-orange-500/30 bg-orange-500/10 text-orange-300'
                : 'border-white/[0.08] bg-zinc-900 text-zinc-400 hover:text-zinc-200'
            }`}
            title="Toggle word wrap"
          >
            <WrapText className="h-3 w-3" />
            <span className="hidden sm:inline">Wrap</span>
          </button>

          <button
            type="button"
            onClick={handleCopy}
            className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
          >
            {copied ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
            <span className="hidden sm:inline">{copied ? 'Copied' : 'Copy'}</span>
          </button>

          <button
            type="button"
            onClick={handleDownload}
            className="inline-flex h-7 items-center gap-1 rounded-lg border border-white/[0.08] bg-zinc-900 px-2 text-[11px] font-medium text-zinc-300 hover:bg-zinc-800 hover:text-white cursor-pointer"
            title="Download file"
          >
            <Download className="h-3 w-3" />
          </button>

          {onSave && !readOnly && (
            <button
              type="button"
              onClick={onSave}
              className="inline-flex h-7 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white transition-colors cursor-pointer"
            >
              <Save className="h-3 w-3" />
              <span>Save</span>
            </button>
          )}

          {actions}
        </div>
      </div>

      {/* Line-Numbered Syntax-Highlighted Surface */}
      <div className="relative flex overflow-hidden" style={{ height }}>
        {/* Gutter */}
        <div
          ref={gutterRef}
          aria-hidden="true"
          className={`select-none overflow-hidden border-r border-white/[0.06] bg-[#0b0b0f] py-3 px-2.5 text-right font-mono ${fontSizeClass} text-zinc-500 shrink-0`}
          style={{ minWidth: '3.25rem' }}
        >
          {Array.from({ length: lineCount }, (_, i) => (
            <div
              key={i + 1}
              className={
                cursorLine === i + 1
                  ? 'font-bold text-orange-400 bg-orange-500/10 -mx-2.5 px-2.5'
                  : undefined
              }
            >
              {i + 1}
            </div>
          ))}
        </div>

        {/* Synchronized Highlight Layer + Editable Textarea */}
        <div className="relative flex-1 overflow-hidden bg-[#08080b]">
          <pre
            ref={preRef}
            aria-hidden="true"
            className={`pointer-events-none absolute inset-0 m-0 overflow-hidden py-3 px-4 font-mono ${fontSizeClass} ${
              wordWrap ? 'whitespace-pre-wrap break-words' : 'whitespace-pre'
            }`}
          >
            {lines.map((ln, idx) => (
              <div
                key={idx}
                className={cursorLine === idx + 1 ? 'bg-white/[0.03] -mx-4 px-4' : undefined}
              >
                {renderHighlightedLine(ln, effectiveLanguage)}
              </div>
            ))}
          </pre>

          <textarea
            value={value}
            readOnly={readOnly}
            onChange={(e) => onChange(e.target.value)}
            onScroll={handleScroll}
            onKeyDown={handleKeyDown}
            onClick={updateCursorLine}
            onKeyUp={updateCursorLine}
            spellCheck={false}
            autoCapitalize="off"
            autoComplete="off"
            wrap={wordWrap ? 'soft' : 'off'}
            className={`relative z-10 h-full w-full resize-none bg-transparent py-3 px-4 font-mono ${fontSizeClass} text-transparent caret-orange-400 focus:outline-none selection:bg-orange-500/30 overflow-auto`}
          />
        </div>
      </div>

      {/* Status Footer */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] bg-[#0c0c10] px-3.5 py-1.5 font-mono text-[10px] text-zinc-400">
        <div className="flex items-center gap-3">
          <span className="text-zinc-300">Ln {cursorLine}, Col 1</span>
          <span>UTF-8</span>
          <span className="text-emerald-400">{permissions}</span>
        </div>
        <div className="flex items-center gap-2">
          {!readOnly && <span>⌘S to save</span>}
        </div>
      </div>
    </div>
  );
};
