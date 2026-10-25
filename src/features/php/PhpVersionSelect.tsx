import React from 'react';
import { AlertTriangle, ShieldAlert, ShieldCheck } from 'lucide-react';
import { selectCls } from './ui';

export interface PhpVersionSecurityInfo {
  version: string;
  status: 'eol' | 'security-only' | 'active';
  statusLabel: string;
  hasCve: boolean;
  cvss?: string;
  cves?: string[];
  cveWarning: string;
  patchedIn?: string;
  isDefault?: boolean;
}

export const PHP_SECURITY_MAP: Record<string, PhpVersionSecurityInfo> = {
  '7.4': {
    version: '7.4',
    status: 'eol',
    statusLabel: 'EOL / Critical CVEs',
    hasCve: true,
    cvss: '9.2',
    cves: ['CVE-2026-109618', 'CVE-2026-109619', 'CVE-2026-115560', 'CVE-2024-4577'],
    cveWarning: 'CRITICAL: Unpatched PHP2Shell RCE, Session RCE and CGI Injection vulnerabilities. EOL since Nov 2022. Upgrading to PHP 8.3+ is strongly advised.',
  },
  '8.0': {
    version: '8.0',
    status: 'eol',
    statusLabel: 'EOL / Critical CVEs',
    hasCve: true,
    cvss: '9.2',
    cves: ['CVE-2026-109618', 'CVE-2026-109619', 'CVE-2026-115560'],
    cveWarning: 'CRITICAL: Vulnerable to PHP2Shell RCE & Session RCE without upstream security patches. Upgrade to PHP 8.3+ recommended.',
  },
  '8.1': {
    version: '8.1',
    status: 'eol',
    statusLabel: 'EOL / Critical CVEs',
    hasCve: true,
    cvss: '9.2',
    cves: ['CVE-2026-109618', 'CVE-2026-109619', 'CVE-2026-115560'],
    cveWarning: 'CRITICAL: Vulnerable to PHP2Shell RCE. Official security support ended Dec 2025. Upgrade to PHP 8.3+ recommended.',
  },
  '8.2': {
    version: '8.2',
    status: 'security-only',
    statusLabel: 'Security Only',
    hasCve: false,
    patchedIn: '8.2.35',
    cveWarning: 'Patched against PHP2Shell in 8.2.35. Upstream security-only fixes end Dec 31, 2026.',
  },
  '8.3': {
    version: '8.3',
    status: 'active',
    statusLabel: 'Active Support (Recommended)',
    hasCve: false,
    isDefault: true,
    patchedIn: '8.3.36',
    cveWarning: 'Patched against PHP2Shell in 8.3.36. Official active support until Dec 31, 2027.',
  },
  '8.4': {
    version: '8.4',
    status: 'active',
    statusLabel: 'Latest Stable',
    hasCve: false,
    patchedIn: '8.4.27',
    cveWarning: 'Patched against PHP2Shell in 8.4.27. Official active support until Dec 31, 2028.',
  },
  '8.5': {
    version: '8.5',
    status: 'active',
    statusLabel: 'Active Support (Latest)',
    hasCve: false,
    patchedIn: '8.5.12',
    cveWarning: 'Patched against PHP2Shell in 8.5.12. Official active support until Dec 31, 2029.',
  },
};

interface PhpVersionSelectProps {
  value: string;
  onChange: (version: string) => void;
  installedVersions: string[];
  disabled?: boolean;
  className?: string;
}

export const PhpVersionSelect: React.FC<PhpVersionSelectProps> = ({
  value,
  onChange,
  installedVersions,
  disabled = false,
  className = selectCls,
}) => {
  const meta = PHP_SECURITY_MAP[value];

  return (
    <div className="space-y-2">
      <select
        className={className}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
      >
        {installedVersions.map((v) => {
          const m = PHP_SECURITY_MAP[v];
          let label = `PHP ${v}`;
          if (m?.hasCve) {
            label = `PHP ${v} ⚠️ (Critical CVEs)`;
          } else if (m?.status === 'security-only') {
            label = `PHP ${v} (Security Only)`;
          } else if (m?.isDefault) {
            label = `PHP ${v} (Recommended)`;
          } else if (m?.status === 'active') {
            label = `PHP ${v} (Latest Stable)`;
          }
          return (
            <option key={v} value={v}>
              {label}
            </option>
          );
        })}
        {value && !installedVersions.includes(value) && (
          <option value={value}>PHP {value} (not installed on host)</option>
        )}
      </select>

      {/* Dynamic Security & CVE Notice */}
      {meta && (
        <>
          {meta.hasCve ? (
            <div className="rounded-xl border border-red-500/30 bg-red-500/10 p-2.5 text-xs text-red-200 animate-in fade-in duration-150">
              <div className="flex items-start gap-2">
                <AlertTriangle className="h-4 w-4 shrink-0 text-red-400 mt-0.5" />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5 font-semibold text-red-300">
                    <span>PHP {value} has Known Critical CVE Vulnerabilities</span>
                    {meta.cvss && (
                      <span className="rounded bg-red-500/20 px-1.5 py-0.2 font-mono text-[9.5px] text-red-200 border border-red-500/30">
                        CVSS {meta.cvss}
                      </span>
                    )}
                  </div>
                  <p className="mt-1 text-[11px] leading-relaxed text-red-200/90">
                    {meta.cveWarning}
                  </p>
                  {meta.cves && meta.cves.length > 0 && (
                    <div className="mt-2 flex flex-wrap gap-1">
                      {meta.cves.map((cve) => (
                        <span
                          key={cve}
                          className="rounded bg-black/40 px-1.5 py-0.5 font-mono text-[10px] text-red-300 border border-red-500/20"
                        >
                          {cve}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </div>
          ) : meta.status === 'security-only' ? (
            <div className="flex items-center gap-2 rounded-lg border border-amber-500/25 bg-amber-500/10 px-2.5 py-1.5 text-[11px] text-amber-200 animate-in fade-in duration-150">
              <ShieldAlert className="h-3.5 w-3.5 shrink-0 text-amber-400" />
              <span>
                <strong>PHP {value} (Security Only):</strong> {meta.cveWarning}
              </span>
            </div>
          ) : (
            <div className="flex items-center gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-2.5 py-1.5 text-[11px] text-emerald-400 animate-in fade-in duration-150">
              <ShieldCheck className="h-3.5 w-3.5 shrink-0 text-emerald-400" />
              <span>
                <strong>PHP {value} ({meta.statusLabel}):</strong> {meta.patchedIn ? `Hardened & Patched ≥ ${meta.patchedIn}` : 'Safe for production'}
              </span>
            </div>
          )}
        </>
      )}
    </div>
  );
};
