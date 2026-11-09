import React from 'react';
import { Menu, ChevronRight, Server, Plus } from 'lucide-react';
import { AppStack, NavigationSection } from '../../types/nextdeploy';

export interface AppHeaderProps {
  setMobileMenuOpen: (open: boolean) => void;
  navigateTo: (nav: NavigationSection, appId?: string | null, replace?: boolean, tab?: string) => void;
  selectedApp: AppStack | null;
  currentNavLabel: string;
  isAdminUser: boolean;
  dockerVersion: string;
  caddyInfo: { up: boolean; status: string; config?: Record<string, string> } | null;
  setNewAppModalOpen: (open: boolean) => void;
}

export const AppHeader: React.FC<AppHeaderProps> = ({
  setMobileMenuOpen,
  navigateTo,
  selectedApp,
  currentNavLabel,
  isAdminUser,
  dockerVersion,
  caddyInfo,
  setNewAppModalOpen,
}) => {
  return (
    <header className="sticky top-0 z-30 flex h-13 xl:h-14 items-center justify-between gap-4 border-b border-white/[0.06] bg-[#0c0c0f]/85 px-4 sm:px-6 lg:px-8 xl:px-10 backdrop-blur-xl">
      <div className="flex items-center gap-2 text-xs font-medium text-zinc-400 min-w-0">
        <button
          type="button"
          onClick={() => setMobileMenuOpen(true)}
          aria-label="Open navigation menu"
          className="lg:hidden -ml-1 mr-1 flex h-9 w-9 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900/60 text-zinc-300 hover:text-white hover:bg-zinc-800 transition-colors cursor-pointer shrink-0"
        >
          <Menu className="h-4 w-4" />
        </button>
        <button
          type="button"
          onClick={() => {
            navigateTo('overview');
          }}
          className="hover:text-zinc-100 transition-colors cursor-pointer whitespace-nowrap py-1.5 px-1 rounded inline-flex items-center min-h-[36px] sm:min-h-0"
        >
          NextDeploy
        </button>
        <ChevronRight className="h-3.5 w-3.5 text-zinc-600 shrink-0" />
        {selectedApp ? (
          <>
            <button
              type="button"
              onClick={() => {
                navigateTo(selectedApp.php ? 'php-sites' : 'apps');
              }}
              className="hover:text-zinc-100 transition-colors cursor-pointer whitespace-nowrap py-1.5 px-1 rounded inline-flex items-center min-h-[36px] sm:min-h-0"
            >
              {selectedApp.php ? 'PHP Sites' : 'Projects'}
            </button>
            <ChevronRight className="h-3.5 w-3.5 text-zinc-600 shrink-0" />
            <span className="font-mono font-semibold text-zinc-100 truncate">
              {selectedApp.name}
            </span>
          </>
        ) : (
          <span className="font-semibold text-zinc-100 truncate">{currentNavLabel}</span>
        )}
      </div>

      <div className="flex items-center gap-2 shrink-0">
        {isAdminUser && dockerVersion && (
          <div className="hidden md:flex items-center gap-1.5 rounded-lg border border-white/[0.07] bg-[#111115] px-2.5 py-1 font-mono text-[11px] text-zinc-400">
            <Server className={`h-3 w-3 ${caddyInfo?.up === false ? 'text-amber-400' : 'text-emerald-400'}`} />
            <span>Docker {dockerVersion}</span>
            <span className="text-zinc-600">·</span>
            <span className={caddyInfo?.up === false ? 'text-amber-400' : 'text-emerald-400'}>
              {caddyInfo?.up === false ? 'Caddy down' : 'Caddy'}
            </span>
          </div>
        )}

        <button
          type="button"
          onClick={() => setNewAppModalOpen(true)}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-100 px-3 text-xs font-semibold text-zinc-950 hover:bg-white active:scale-[0.98] shadow-xs transition-all duration-100 ease-out cursor-pointer whitespace-nowrap"
        >
          <Plus className="h-3.5 w-3.5" />
          <span className="hidden sm:inline">Create Project</span>
          <span className="sm:hidden">New</span>
        </button>
      </div>
    </header>
  );
};
