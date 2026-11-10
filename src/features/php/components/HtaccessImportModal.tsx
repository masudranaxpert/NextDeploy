import React, { useState, useTransition } from 'react';
import { AlertTriangle, CheckCircle2, FileCode2, Info, ScanSearch } from 'lucide-react';
import { toast } from 'sonner';
import { backendApi } from '../../../lib/backendApi';
import type { HtaccessConvertResult, PhpRoutingRule, PhpSiteRouting } from '../../../lib/api/phpApi';
import { ShadcnDialog } from '../../../components/ui/Primitives';
import { CodeEditorBox } from '../../../components/ui/CodeEditorBox';
import { GhostButton, Pill, PrimaryButton, inputCls } from '../ui';

interface Props {
  appId: string;
  open: boolean;
  onClose: () => void;
  onApply: (
    rules: PhpRoutingRule[],
    mode: 'append' | 'replace',
    suggestedFallback?: PhpSiteRouting['fallback_mode'],
    suggestedEntry?: string
  ) => void;
}

const SAMPLE_HTACCESS = `RewriteEngine On
RewriteCond %{REQUEST_FILENAME} !-f
RewriteRule ^([^.]+)$ $1.php [NC,L,QSA]
RewriteRule ^blog/(.*)$ /post.php?slug=$1 [QSA,L]
Redirect 301 /old-page /new-page`;

export const HtaccessImportModal: React.FC<Props> = ({ appId, open, onClose, onApply }) => {
  const [tab, setTab] = useState<'scan' | 'paste'>('scan');
  const [content, setContent] = useState(SAMPLE_HTACCESS);
  const [filePath, setFilePath] = useState('.htaccess');
  const [result, setResult] = useState<HtaccessConvertResult | null>(null);
  const [isPending, startTransition] = useTransition();

  const runScan = () => {
    startTransition(async () => {
      const res = await backendApi.phpScanHtaccess(appId, { apply: false });
      if (!res.ok || !res.data?.result) {
        toast.error(res.error || 'Failed to scan htdocs for .htaccess files');
        return;
      }
      setResult(res.data.result);
      if (res.data.result.files_scanned === 0) {
        toast.info('No .htaccess files found in htdocs');
      }
    });
  };

  const runPreview = () => {
    if (!content.trim()) {
      toast.error('Paste .htaccess content first');
      return;
    }
    startTransition(async () => {
      const res = await backendApi.phpConvertHtaccess(appId, content, filePath.trim() || '.htaccess');
      if (!res.ok || !res.data?.result) {
        toast.error(res.error || 'Failed to convert .htaccess');
        return;
      }
      setResult(res.data.result);
    });
  };

  const applyWithMode = (mode: 'append' | 'replace') => {
    if (!result) return;
    onApply(result.rules || [], mode, result.fallback_mode, result.fallback_entry);
    onClose();
  };

  const diagIcon = (severity: string) => {
    switch (severity) {
      case 'converted':
        return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-emerald-400" />;
      case 'warning':
        return <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-400" />;
      default:
        return <Info className="h-3.5 w-3.5 shrink-0 text-zinc-400" />;
    }
  };

  const summary = result
    ? result.summary ?? {
        converted: result.rules?.length ?? 0,
        skipped: (result.diagnostics ?? []).filter((d) => d.severity === 'info').length,
        warnings: (result.diagnostics ?? []).filter((d) => d.severity === 'warning').length,
      }
    : null;

  return (
    <ShadcnDialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      icon={<FileCode2 className="h-4 w-4 text-orange-400" />}
      title="Import .htaccess"
      footer={
        <div className="flex flex-wrap items-center justify-between gap-2">
          <GhostButton onClick={onClose}>Close</GhostButton>
          {result && (result.rules.length > 0 || result.fallback_mode) && (
            <div className="flex items-center gap-2">
              <GhostButton onClick={() => applyWithMode('replace')}>Replace Existing</GhostButton>
              <PrimaryButton onClick={() => applyWithMode('append')}>
                Add {result.rules.length} Rule{result.rules.length === 1 ? '' : 's'}
              </PrimaryButton>
            </div>
          )}
        </div>
      }
    >
      <div className="space-y-4">
        <div className="flex gap-1.5 rounded-lg border border-white/[0.1] bg-[#09090b] p-1">
          <button
            type="button"
            onClick={() => setTab('scan')}
            className={`flex-1 rounded-md px-3 py-1.5 text-xs font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-500 cursor-pointer ${
              tab === 'scan' ? 'bg-white/[0.1] text-white' : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            Scan htdocs
          </button>
          <button
            type="button"
            onClick={() => setTab('paste')}
            className={`flex-1 rounded-md px-3 py-1.5 text-xs font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-orange-500 cursor-pointer ${
              tab === 'paste' ? 'bg-white/[0.1] text-white' : 'text-zinc-400 hover:text-zinc-200'
            }`}
          >
            Paste
          </button>
        </div>

        {tab === 'scan' ? (
          <div className="flex flex-col gap-3 rounded-lg border border-white/[0.1] bg-[#09090b] p-3.5 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <div className="text-xs font-medium text-zinc-100">Scan htdocs/</div>
              <p className="mt-0.5 text-[11px] text-zinc-400">
                Finds .htaccess files in root and subfolders.
              </p>
            </div>
            <PrimaryButton busy={isPending} onClick={runScan} className="shrink-0">
              <ScanSearch className="h-3.5 w-3.5" /> Scan
            </PrimaryButton>
          </div>
        ) : (
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <input
                className={inputCls}
                value={filePath}
                onChange={(e) => setFilePath(e.target.value)}
                placeholder=".htaccess or subfolder/.htaccess"
              />
              <PrimaryButton busy={isPending} onClick={runPreview} className="shrink-0">
                Convert
              </PrimaryButton>
            </div>
            <CodeEditorBox
              filename={filePath || '.htaccess'}
              language="apache"
              value={content}
              onChange={setContent}
              height={185}
            />
          </div>
        )}

        {result && summary && (
          <div className="space-y-3 rounded-lg border border-white/[0.07] bg-[#09090b] p-3.5">
            <div className="flex flex-wrap items-center gap-2">
              {typeof result.files_scanned === 'number' && result.files_scanned > 0 && (
                <Pill tone="zinc">{result.files_scanned} file(s)</Pill>
              )}
              <Pill tone="green">{summary.converted} converted</Pill>
              {summary.skipped > 0 && <Pill tone="zinc">{summary.skipped} skipped</Pill>}
              {summary.warnings > 0 && <Pill tone="orange">{summary.warnings} warnings</Pill>}
              {result.fallback_mode && (
                <Pill tone="violet">
                  Suggested fallback: {result.fallback_mode}
                  {result.fallback_entry ? ` (${result.fallback_entry})` : ''}
                </Pill>
              )}
            </div>

            {result.rules.length > 0 && (
              <div className="space-y-1.5">
                <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-400">
                  Generated Caddy Rules ({result.rules.length})
                </div>
                <div className="max-h-48 space-y-1.5 overflow-y-auto pr-1">
                  {result.rules.map((r) => (
                    <div
                      key={r.id}
                      className="flex flex-col gap-1 rounded-md border border-white/[0.06] bg-white/[0.02] px-3 py-2 font-mono text-[11px]"
                    >
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span
                          className={`rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase ${
                            r.kind === 'block'
                              ? 'bg-rose-500/15 text-rose-300'
                              : r.kind === 'redir'
                              ? 'bg-emerald-500/15 text-emerald-300'
                              : 'bg-sky-500/15 text-sky-300'
                          }`}
                        >
                          {r.kind}
                          {r.status ? ` ${r.status}` : ''}
                        </span>
                        <span className="rounded bg-white/[0.06] px-1.5 py-0.5 text-[10px] text-zinc-400">
                          {r.match_type}
                        </span>
                        {r.unless_exists && (
                          <span className="rounded bg-violet-500/15 px-1.5 py-0.5 text-[10px] text-violet-300">
                            !-f
                          </span>
                        )}
                        {r.preserve_query && (
                          <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-300">
                            QSA
                          </span>
                        )}
                        {r.note && <span className="ml-auto text-[10px] text-zinc-400">{r.note}</span>}
                      </div>
                      <div className="break-all text-zinc-200">
                        <span className="text-orange-300">{r.pattern}</span>
                        {r.target ? (
                          <>
                            <span className="text-zinc-400"> → </span>
                            <span className="text-emerald-300">{r.target}</span>
                          </>
                        ) : null}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {(result.diagnostics ?? []).length > 0 && (
              <div className="space-y-1.5">
                <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-400">
                  Diagnostics
                </div>
                <div className="max-h-40 space-y-1 overflow-y-auto pr-1">
                  {result.diagnostics.map((d, idx) => (
                    <div
                      key={`${d.file}-${d.line}-${idx}`}
                      className="flex items-start gap-2 rounded-md bg-white/[0.02] px-2.5 py-1.5 text-[11px]"
                    >
                      {diagIcon(d.severity)}
                      <div className="min-w-0 flex-1">
                        <div className="text-zinc-300">
                          <span className="font-mono text-zinc-400">
                            {d.file}:{d.line || '-'}
                          </span>{' '}
                          {d.directive && <span className="font-mono text-purple-300">{d.directive}</span>}{' '}
                          {d.message}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </ShadcnDialog>
  );
};
