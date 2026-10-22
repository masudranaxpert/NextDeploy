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
 * Authentic pheralb/react-symbols (Miguel Solorio VSCode Symbols theme) SVG file icons
 * Supports TypeScript, TSX/JSX, JavaScript, Python, Go, Rust, PHP, Docker, Compose/YAML, Shell, SQL, JSON, Nginx/Caddy, Env, Markdown, and Archives
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
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="3" y="4" width="18" height="16" rx="2.5" fill="#A855F7" fillOpacity="0.18" stroke="#C084FC" strokeWidth="1.6" />
        <path d="M12 4v16M10 7h4M10 10h4M10 13h4" stroke="#E9D5FF" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    );
  }

  // Python (.py, requirements.txt, pyproject.toml) — react-symbols Python dual-snake emblem
  if (base.endsWith('.py') || base === 'requirements.txt' || base === 'pyproject.toml') {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <path
          d="M11.9 2C7.8 2 8.1 3.8 8.1 3.8V6h3.9v.8H6.5S4 6.5 4 10.2c0 3.7 2.2 3.6 2.2 3.6h1.3v-1.8s-.1-2.2 2.1-2.2h3.8s2 .1 2-1.9V4.2S15.7 2 11.9 2Z"
          fill="#387EB8"
        />
        <path
          d="M12.1 22c4.1 0 3.8-1.8 3.8-1.8V18h-3.9v-.8h5.5S20 17.5 20 13.8c0-3.7-2.2-3.6-2.2-3.6h-1.3v1.8s.1 2.2-2.1 2.2h-3.8s-2-.1-2 1.9v3.7S8.3 22 12.1 22Z"
          fill="#FFE052"
        />
        <circle cx="9.8" cy="4.1" r="0.75" fill="#FFF" />
        <circle cx="14.2" cy="19.9" r="0.75" fill="#1E293B" />
      </svg>
    );
  }

  // Go (.go, go.mod, go.sum) — react-symbols Go cyan badge
  if (base.endsWith('.go') || base === 'go.mod' || base === 'go.sum') {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="2" y="4" width="20" height="16" rx="3.5" fill="#00ADD8" />
        <path
          d="M10.5 10.2H7.8c-.9 0-1.6.7-1.6 1.8s.7 1.8 1.6 1.8h1.8v-1.2H8.2M15.8 10.2c-1.1 0-1.9.8-1.9 1.8s.8 1.8 1.9 1.8 1.9-.8 1.9-1.8-.8-1.8-1.9-1.8Z"
          stroke="#06202B"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  // Rust (.rs, Cargo.toml, Cargo.lock) — react-symbols Rust gear
  if (base.endsWith('.rs') || base === 'cargo.toml' || base === 'cargo.lock') {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <circle cx="12" cy="12" r="7.5" stroke="#F74C00" strokeWidth="2" strokeDasharray="2.2 1.4" />
        <circle cx="12" cy="12" r="5" fill="#F74C00" fillOpacity="0.2" stroke="#FF8A4C" strokeWidth="1.5" />
        <path d="M10 14.5v-5h2.3c1 0 1.6.5 1.6 1.4 0 .8-.5 1.3-1.3 1.4l1.5 2.2M10 12.3h2.1" stroke="#FFD6B8" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }

  // PHP (.php, composer.json) — react-symbols PHP oval emblem
  if (base.endsWith('.php') || base === 'composer.json') {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <ellipse cx="12" cy="12" rx="9.5" ry="6.5" fill="#777BB4" />
        <path
          d="M6.8 14.2l.8-4.4h2c.9 0 1.4.5 1.3 1.3-.1.9-.7 1.4-1.6 1.4H7.3M11.6 9.8l-.8 4.4M11.3 11.8h1.8c.6 0 .9.3.8.9l-.3 1.5M15.4 14.2l.8-4.4h1.9c.9 0 1.4.5 1.3 1.3-.1.9-.7 1.4-1.6 1.4h-1.9"
          stroke="#F8FAFC"
          strokeWidth="1.35"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  // React TSX / JSX (.tsx, .jsx) — react-symbols React atom
  if (base.endsWith('.tsx') || base.endsWith('.jsx')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <circle cx="12" cy="12" r="1.9" fill="#61DAFB" />
        <ellipse cx="12" cy="12" rx="9" ry="3.6" stroke="#61DAFB" strokeWidth="1.4" />
        <ellipse cx="12" cy="12" rx="9" ry="3.6" transform="rotate(60 12 12)" stroke="#61DAFB" strokeWidth="1.4" />
        <ellipse cx="12" cy="12" rx="9" ry="3.6" transform="rotate(120 12 12)" stroke="#61DAFB" strokeWidth="1.4" />
      </svg>
    );
  }

  // TypeScript (.ts) — react-symbols TS rounded square
  if (base.endsWith('.ts')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="2.5" y="2.5" width="19" height="19" rx="3.2" fill="#3178C6" />
        <path
          d="M7.5 10.5h5M10 10.5v7M17.8 11.6c-.4-.6-1.1-.9-2-.9-1.2 0-2 .6-2 1.5 0 2.1 4.2 1.2 4.2 3.6 0 1.1-.9 1.8-2.3 1.8-1 0-1.8-.4-2.3-1.1"
          stroke="#FFF"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  // JavaScript (.js, .mjs, .cjs) — react-symbols JS yellow square
  if (base.endsWith('.js') || base.endsWith('.mjs') || base.endsWith('.cjs')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="2.5" y="2.5" width="19" height="19" rx="3.2" fill="#F7DF1E" />
        <path
          d="M11.2 10.5v5.2c0 1.2-.7 1.9-1.8 1.9-.8 0-1.4-.4-1.7-1M17.8 11.5c-.4-.6-1-.9-1.9-.9-1.1 0-1.9.6-1.9 1.4 0 2 4 1.2 4 3.5 0 1.1-.9 1.9-2.2 1.9-1 0-1.8-.4-2.2-1.1"
          stroke="#111827"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  // Dockerfile / Compose — react-symbols Docker whale/containers
  if (
    base === 'dockerfile' ||
    base.endsWith('.dockerfile') ||
    base.includes('docker-compose') ||
    base.includes('compose.')
  ) {
    const isCompose = base.includes('compose');
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="4" y="9" width="3" height="2.6" rx="0.4" fill={isCompose ? '#F472B6' : '#38BDF8'} />
        <rect x="7.6" y="9" width="3" height="2.6" rx="0.4" fill={isCompose ? '#F472B6' : '#38BDF8'} />
        <rect x="11.2" y="9" width="3" height="2.6" rx="0.4" fill={isCompose ? '#F472B6' : '#38BDF8'} />
        <rect x="7.6" y="5.8" width="3" height="2.6" rx="0.4" fill={isCompose ? '#FB7185' : '#0284C7'} />
        <rect x="11.2" y="5.8" width="3" height="2.6" rx="0.4" fill={isCompose ? '#FB7185' : '#0284C7'} />
        <path
          d="M2.5 12.5h15.2c1.1 0 2-.5 2.6-1.4.8.4 1.6.3 2.2-.3-.3 1.6-1.3 2.7-2.6 3.1-.7 3.3-3.6 5.1-8.3 5.1-5.1 0-8.4-2.4-9.1-6.5Z"
          fill={isCompose ? '#EC4899' : '#0EA5E9'}
        />
      </svg>
    );
  }

  // YAML (.yml, .yaml)
  if (base.endsWith('.yml') || base.endsWith('.yaml')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="3" y="3" width="18" height="18" rx="3" fill="#EF4444" fillOpacity="0.16" stroke="#F87171" strokeWidth="1.5" />
        <path d="M7.5 8l3 4v4M13.5 8l-3 4M15.5 16h2" stroke="#FCA5A5" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }

  // Environment / Secrets (.env, .pem)
  if (base.startsWith('.env') || base.endsWith('.env') || base.endsWith('.pem')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="4" y="10" width="16" height="10" rx="2.2" fill="#10B981" fillOpacity="0.2" stroke="#34D399" strokeWidth="1.6" />
        <path d="M8 10V7a4 4 0 0 1 8 0v3" stroke="#34D399" strokeWidth="1.6" strokeLinecap="round" />
        <circle cx="12" cy="15" r="1.4" fill="#6EE7B7" />
      </svg>
    );
  }

  // SQL / Prisma (.sql, .prisma)
  if (base.endsWith('.sql') || base.endsWith('.prisma')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <ellipse cx="12" cy="6" rx="7.5" ry="3" fill="#22D3EE" fillOpacity="0.25" stroke="#22D3EE" strokeWidth="1.6" />
        <path d="M4.5 6v6c0 1.66 3.36 3 7.5 3s7.5-1.34 7.5-3V6" stroke="#22D3EE" strokeWidth="1.6" />
        <path d="M4.5 12v6c0 1.66 3.36 3 7.5 3s7.5-1.34 7.5-3v-6" stroke="#22D3EE" strokeWidth="1.6" />
      </svg>
    );
  }

  // JSON (.json) — react-symbols yellow braces
  if (base.endsWith('.json')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <path
          d="M9.5 5C7.8 5 7 5.8 7 7.4v2.1c0 1.1-.7 1.8-2 2.5 1.3.7 2 1.4 2 2.5v2.1C7 18.2 7.8 19 9.5 19M14.5 5c1.7 0 2.5.8 2.5 2.4v2.1c0 1.1.7 1.8 2 2.5-1.3.7-2 1.4-2 2.5v2.1c0 1.6-.8 2.4-2.5 2.4"
          stroke="#FBBF24"
          strokeWidth="1.85"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  // Shell / Bash (.sh, .bash, .zsh)
  if (base.endsWith('.sh') || base.endsWith('.bash') || base.endsWith('.zsh')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="2.5" y="4" width="19" height="16" rx="2.8" fill="#18181B" stroke="#A78BFA" strokeWidth="1.6" />
        <path d="M7 9.5L10.2 12 7 14.5M12 14.5h4.5" stroke="#C4B5FD" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }

  // Caddyfile / Nginx (.conf)
  if (base === 'caddyfile' || base.endsWith('.conf')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <polygon points="12,2.5 20.2,7.2 20.2,16.8 12,21.5 3.8,16.8 3.8,7.2" fill="#14B8A6" fillOpacity="0.2" stroke="#2DD4BF" strokeWidth="1.6" />
        <path d="M9 15V9l6 6V9" stroke="#5EEAD4" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }

  // Markdown (.md)
  if (base.endsWith('.md')) {
    return (
      <svg viewBox="0 0 24 24" fill="none" className={`${className} shrink-0`} aria-hidden="true">
        <rect x="2.5" y="5" width="19" height="14" rx="2.4" stroke="#38BDF8" strokeWidth="1.6" fill="#0284C7" fillOpacity="0.15" />
        <path d="M6 15V9l2.5 3L11 9v6M16.5 9v6m0 0l-2-2m2 2l2-2" stroke="#7DD3FC" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
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
