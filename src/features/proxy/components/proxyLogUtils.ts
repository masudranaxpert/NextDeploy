export interface ParsedProxyLogLine {
  raw: string;
  level: 'error' | 'warn' | 'info';
  timestamp?: string;
  logger?: string;
  message: string;
  meta?: string;
}

export function parseProxyLogLine(line: string): ParsedProxyLogLine {
  const raw = String(line || '');
  const trimmed = raw.trim();
  const braceIdx = trimmed.indexOf('{');
  if (braceIdx >= 0 && trimmed.endsWith('}')) {
    try {
      const obj = JSON.parse(trimmed.slice(braceIdx)) as {
        level?: string;
        ts?: number | string;
        time?: string;
        logger?: string;
        msg?: string;
        message?: string;
        error?: string;
      };
      const lvlRaw = String(obj.level || 'info').toLowerCase();
      const level: ParsedProxyLogLine['level'] =
        lvlRaw === 'error' || lvlRaw === 'fatal' || lvlRaw === 'panic'
          ? 'error'
          : lvlRaw === 'warn' || lvlRaw === 'warning'
          ? 'warn'
          : 'info';

      const ts =
        typeof obj.ts === 'number'
          ? new Date(obj.ts * 1000).toISOString().replace('T', ' ').slice(0, 19)
          : typeof obj.time === 'string'
          ? obj.time.replace('T', ' ').slice(0, 19)
          : typeof obj.ts === 'string'
          ? obj.ts
          : undefined;

      return {
        raw,
        level,
        timestamp: ts,
        logger: obj.logger,
        message: obj.msg || obj.message || raw,
        meta: obj.error || undefined,
      };
    } catch {
      // fall through to plain-text parsing
    }
  }

  const lower = raw.toLowerCase();
  const level: ParsedProxyLogLine['level'] = /\b(error|fatal|panic|crit|emerg|exception)\b/.test(lower)
    ? 'error'
    : /\b(warn|warning)\b/.test(lower)
    ? 'warn'
    : 'info';

  const isoMatch = trimmed.match(/^(\d{4}[-/]\d{2}[-/]\d{2}[T\s]\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s+(.*)$/);
  if (isoMatch) {
    return {
      raw,
      level,
      timestamp: isoMatch[1].replace('T', ' ').slice(0, 19),
      message: isoMatch[2],
    };
  }

  return { raw, level, message: raw };
}
