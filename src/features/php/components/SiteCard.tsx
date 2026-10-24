import React from 'react';
import { ArrowUpRight, ExternalLink, Globe } from 'lucide-react';
import { AppStack } from '../../../types/nextdeploy';
import { Pill, StatusDot } from '../ui';
import { SiteTypeIcon, WordPressIcon, LaravelIcon, PhpIcon, StaticSiteIcon } from '../../../components/ui/BrandIcons';

interface SiteCardProps {
  site: AppStack;
  onOpenSite: (id: string) => void;
}

export const SiteCard: React.FC<SiteCardProps> = ({ site, onOpenSite }) => {
  const php = site.php;
  const siteType = php?.siteType || 'php';
  const phpVersion = php?.phpVersion;
  const isLaravel = site.name.toLowerCase().includes('laravel') || site.slug?.toLowerCase().includes('laravel');
  const isWp = siteType === 'wordpress' || site.name.toLowerCase().includes('wp') || site.name.toLowerCase().includes('wordpress');
  const primaryDomain = site.primaryDomain || site.domains[0]?.domain;
  const domainUrl = primaryDomain ? `https://${primaryDomain}` : null;
  const isRunning = site.status === 'running' || (site.status as string) === 'ready';

  return (
    <div
      onClick={() => onOpenSite(site.id)}
      className="group relative flex flex-col justify-between rounded-xl border border-white/[0.08] bg-[#111115] p-4 transition-all duration-150 hover:border-white/[0.18] hover:bg-[#141419] cursor-pointer shadow-xs hover:shadow-md"
    >
      <div>
        {/* Top Header: Brand Icon, Name, Status, Arrow */}
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-white/[0.08] bg-zinc-900/90 shadow-inner">
              <SiteTypeIcon type={siteType} name={site.name} className="h-5 w-5" />
            </div>

            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="truncate text-sm font-semibold text-zinc-100 group-hover:text-white">
                  {site.name}
                </span>
                <StatusDot status={isRunning ? 'running' : 'stopped'} />
              </div>

              {primaryDomain ? (
                <div className="mt-0.5 flex items-center gap-1.5 min-w-0">
                  <a
                    href={domainUrl!}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(e) => e.stopPropagation()}
                    className="inline-flex items-center gap-1 font-mono text-xs text-zinc-400 hover:text-emerald-400 transition-colors truncate"
                    title={primaryDomain}
                  >
                    <span className="truncate">{primaryDomain}</span>
                    <ExternalLink className="h-3 w-3 shrink-0 opacity-70" />
                  </a>
                </div>
              ) : (
                <p className="mt-0.5 font-mono text-xs text-zinc-500">No domain linked</p>
              )}
            </div>
          </div>

          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-white/[0.06] bg-zinc-900/80 text-zinc-400 transition-colors group-hover:border-orange-500/30 group-hover:bg-orange-500/10 group-hover:text-orange-400">
            <ArrowUpRight className="h-3.5 w-3.5" />
          </div>
        </div>

        {/* Badges / Metadata row */}
        <div className="mt-3.5 flex flex-wrap items-center gap-1.5 pt-3 border-t border-white/[0.06]">
          {isWp ? (
            <span className="inline-flex items-center gap-1 rounded-md border border-[#3858E9]/30 bg-[#3858E9]/10 px-2 py-0.5 font-mono text-[10px] font-medium text-[#7C9BFF]">
              <WordPressIcon className="h-2.5 w-2.5 text-[#3858E9]" />
              WordPress
            </span>
          ) : isLaravel ? (
            <span className="inline-flex items-center gap-1 rounded-md border border-[#FF2D20]/30 bg-[#FF2D20]/10 px-2 py-0.5 font-mono text-[10px] font-medium text-[#FFA099]">
              <LaravelIcon className="h-2.5 w-2.5 text-[#FF2D20]" />
              Laravel · PHP {phpVersion || '8.4'}
            </span>
          ) : siteType === 'php' ? (
            <span className="inline-flex items-center gap-1 rounded-md border border-[#777BB4]/30 bg-[#777BB4]/10 px-2 py-0.5 font-mono text-[10px] font-medium text-[#B0B4E8]">
              <PhpIcon className="h-2.5 w-2.5 text-[#777BB4]" />
              PHP {phpVersion || '8.3'}
            </span>
          ) : (
            <span className="inline-flex items-center gap-1 rounded-md border border-sky-500/30 bg-sky-500/10 px-2 py-0.5 font-mono text-[10px] font-medium text-sky-400">
              <StaticSiteIcon className="h-2.5 w-2.5" />
              Static
            </span>
          )}

          {site.domains.length > 0 && (
            <span className="inline-flex items-center gap-1 rounded-md border border-white/[0.06] bg-white/[0.02] px-2 py-0.5 font-mono text-[10px] text-zinc-400">
              <Globe className="h-2.5 w-2.5" />
              {site.domains.length} {site.domains.length === 1 ? 'domain' : 'domains'}
            </span>
          )}

          {site.updatedAt && (
            <span className="ml-auto text-[10px] text-zinc-500 font-mono">
              {site.updatedAt}
            </span>
          )}
        </div>
      </div>
    </div>
  );
};
