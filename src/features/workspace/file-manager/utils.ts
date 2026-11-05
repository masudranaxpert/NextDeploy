/**
 * Normalizes any file permission (symbolic, octal with/without leading 0, parenthesized)
 * into a clean 3-digit octal string like "644" or "755".
 */
export function formatPermOctal(perm?: string | null, isDir = false): string {
  if (!perm) return isDir ? '755' : '644';
  const trimmed = perm.trim();
  const parenMatch = trimmed.match(/\(0?([0-7]{3})\)/);
  if (parenMatch) return parenMatch[1];
  const octalMatch = trimmed.match(/^0?([0-7]{3})$/);
  if (octalMatch) return octalMatch[1];
  const sym = trimmed.replace(/^[d-]/, '');
  if (sym.length >= 9) {
    const parseTriplet = (s: string) =>
      (s[0] === 'r' ? 4 : 0) + (s[1] === 'w' ? 2 : 0) + (s[2] === 'x' || s[2] === 's' ? 1 : 0);
    const u = parseTriplet(sym.slice(0, 3));
    const g = parseTriplet(sym.slice(3, 6));
    const o = parseTriplet(sym.slice(6, 9));
    return `${u}${g}${o}`;
  }
  return isDir ? '755' : '644';
}

export function formatFileSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  if (bytes >= 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  }
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  if (bytes >= 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${bytes} B`;
}

/**
 * Returns true if the file or folder is a secret/protected environment file.
 * Preserves safe template files like .env.example, .env.sample, .env.template, .env.dist.
 */
export function isProtectedEnvFile(nameOrPath: string): boolean {
  if (!nameOrPath) return false;
  const base = nameOrPath.split('/').pop()?.toLowerCase().trim() || '';
  if (
    base === '.env.example' ||
    base === '.env.sample' ||
    base === '.env.template' ||
    base === '.env.dist' ||
    base === '.env.defaults'
  ) {
    return false;
  }
  return (
    base === '.env' ||
    base.startsWith('.env.') ||
    base.endsWith('.env')
  );
}

/**
 * Returns true if a file or folder starts with a dot (e.g. .git, .gitignore, .dockerignore)
 */
export function isHiddenDotFile(nameOrPath: string): boolean {
  if (!nameOrPath) return false;
  const base = nameOrPath.split('/').pop()?.trim() || '';
  return base.startsWith('.');
}
