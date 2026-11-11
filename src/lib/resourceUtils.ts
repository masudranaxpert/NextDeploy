import { AppStack } from '../types/nextdeploy';

export function isAppStopped(app: Pick<AppStack, 'status'>): boolean {
  return app.status === 'stopped';
}

export function sortStoppedLast<T extends Pick<AppStack, 'status'>>(items: T[]): T[] {
  return [...items].sort((a, b) => Number(isAppStopped(a)) - Number(isAppStopped(b)));
}

export function matchesAppSearch(app: AppStack, rawQuery: string): boolean {
  const q = rawQuery.trim().toLowerCase();
  if (!q) return true;

  if (q.startsWith('@')) {
    const ownerQ = q.slice(1).trim();
    if (!ownerQ) return true;
    return (app.owner || '').toLowerCase().includes(ownerQ);
  }

  return (
    app.name.toLowerCase().includes(q) ||
    app.slug.toLowerCase().includes(q) ||
    (app.owner || '').toLowerCase().includes(q) ||
    (app.primaryDomain || '').toLowerCase().includes(q) ||
    app.domains.some((d) => d.domain.toLowerCase().includes(q)) ||
    app.services.some((s) => s.service.toLowerCase().includes(q) || s.name.toLowerCase().includes(q)) ||
    (app.php?.phpVersion || '').toLowerCase().includes(q) ||
    (app.php?.siteType || '').toLowerCase().includes(q)
  );
}

export function formatDiskMB(diskMB: number): string {
  if (!diskMB || diskMB <= 0) return '0 MB';
  if (diskMB < 1) {
    const kb = Math.round(diskMB * 1024);
    return kb > 0 ? `${kb} KB` : '< 1 KB';
  }
  if (diskMB < 10) return `${diskMB.toFixed(1)} MB`;
  if (diskMB < 1024) return `${Math.round(diskMB)} MB`;
  return `${(diskMB / 1024).toFixed(2)} GB`;
}

export function normalizeCpuPct(rawSingleCorePct: number, capacityCores = 1): number {
  const raw = Number(rawSingleCorePct) || 0;
  if (raw <= 0) return 0;
  const div = capacityCores > 0 ? capacityCores : 1;
  return Math.round(Math.min(100, raw / div) * 100) / 100;
}

function decimalsForStep(step: number): number {
  const s = String(step);
  const dot = s.indexOf('.');
  return dot === -1 ? 0 : s.length - dot - 1;
}

export function snapLimitValue(
  raw: string,
  snapStep = 1,
  max?: number,
  min = 0
): { value: string; adjusted: boolean } {
  const trimmed = raw.trim();
  if (trimmed === '') return { value: '', adjusted: false };
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed)) return { value: String(min), adjusted: true };

  let target = Math.max(min, parsed);
  if (max && max > 0) {
    target = Math.min(target, max);
  }

  let snapped = target;
  if (target > 0 && snapStep > 0) {
    snapped = Math.round(target / snapStep) * snapStep;
    if (max && max > 0 && snapped > max) {
      snapped = Math.floor(max / snapStep) * snapStep;
      if (snapped < min || (snapped === 0 && max > 0 && target > 0)) {
        snapped = max;
      }
    }
    if (snapped < min) snapped = min;
  }

  const precision = decimalsForStep(snapStep);
  const formatted = precision > 0 ? String(Number(snapped.toFixed(precision))) : String(Math.round(snapped));
  const adjusted = Math.abs(Number(formatted) - parsed) > 1e-6;
  return { value: formatted, adjusted };
}

export function stepLimitValue(
  raw: string,
  direction: 1 | -1,
  snapStep = 1,
  max?: number,
  min = 0
): string {
  const base = Number(raw.trim());
  const current = Number.isFinite(base) ? base : 0;
  const next = Math.max(min, current + direction * snapStep);
  return snapLimitValue(String(next), snapStep, max, min).value;
}
