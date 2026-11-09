import React from 'react';
import { Search, X, LogOut } from 'lucide-react';
import { NextDeployLogoMark } from '../ui/BrandIcons';
import { AppStack, NavigationSection } from '../../types/nextdeploy';
import { NavGroup } from './navItems';

export interface AppSidebarProps {
  mobileMenuOpen: boolean;
  setMobileMenuOpen: (open: boolean) => void;
  activeNav: NavigationSection;
  selectedAppId: string | null;
  selectedApp: AppStack | null;
  visibleNavGroups: NavGroup[];
  navigateTo: (nav: NavigationSection, appId?: string | null, replace?: boolean, tab?: string) => void;
  prefetchRoute: (nav: NavigationSection) => void;
  setCmdPaletteOpen: (open: boolean) => void;
  currentUser: { id: number; username: string; role: string } | null;
  onLogout: () => void;
}

export const AppSidebar: React.FC<AppSidebarProps> = ({
  mobileMenuOpen,
  setMobileMenuOpen,
  activeNav,
  selectedAppId,
  selectedApp,
  visibleNavGroups,
  navigateTo,
  prefetchRoute,
  setCmdPaletteOpen,
  currentUser,
  onLogout,
}) => {
  return (
    <>
      {mobileMenuOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/70 backdrop-blur-xs lg:hidden"
          onClick={() => setMobileMenuOpen(false)}
        />
      )}

      {/* Dokploy / shadcn Inset Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-50 flex w-72 sm:w-68 lg:w-60 xl:w-64 2xl:w-68 shrink-0 flex-col bg-[#09090b] border-r border-white/[0.06] transition-transform duration-200 ease-out lg:sticky lg:top-0 lg:h-screen lg:self-start lg:translate-x-0 ${
          mobileMenuOpen ? 'translate-x-0 shadow-2xl shadow-black/80' : '-translate-x-full'
        }`}
      >
        <div className="flex h-14 sm:h-14 items-center justify-between px-3.5 pt-2 shrink-0">
          <button
            type="button"
            onClick={() => {
              navigateTo('overview');
              setMobileMenuOpen(false);
            }}
            className="flex flex-1 items-center gap-2.5 rounded-xl border border-white/[0.07] bg-[#111115] px-2.5 py-2 lg:py-1.5 text-left hover:border-white/[0.14] transition-all cursor-pointer active:scale-[0.99]"
          >
            <NextDeployLogoMark className="h-7 w-7 shrink-0 rounded-lg shadow-sm" />
            <div className="min-w-0 flex-1 leading-tight">
              <div className="flex items-center gap-1.5">
                <span className="truncate font-display text-xs sm:text-xs font-semibold text-zinc-100">
                  NextDeploy
                </span>
                <span className="rounded bg-white/[0.06] px-1 py-0.2 font-mono text-[9px] text-zinc-400">
                  v2.5
                </span>
              </div>
            </div>
          </button>
          <button
            type="button"
            onClick={() => setMobileMenuOpen(false)}
            aria-label="Close navigation drawer"
            className="ml-2 flex h-9 w-9 items-center justify-center rounded-lg border border-white/[0.08] bg-zinc-900/90 text-zinc-400 hover:text-white active:bg-zinc-800 transition-colors lg:hidden shrink-0 cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Sidebar Search Trigger */}
        <div className="px-3.5 pt-2.5 pb-1 shrink-0">
          <button
            type="button"
            onClick={() => {
              setCmdPaletteOpen(true);
              setMobileMenuOpen(false);
            }}
            className="flex h-10 lg:h-8 w-full items-center justify-between rounded-lg border border-white/[0.07] bg-[#111115]/70 px-3 lg:px-2.5 text-[13px] lg:text-xs text-zinc-400 hover:border-white/[0.14] hover:text-zinc-200 transition-colors cursor-pointer active:scale-[0.99]"
          >
            <span className="flex items-center gap-2.5">
              <Search className="h-4 w-4 lg:h-3.5 lg:w-3.5 text-zinc-500" />
              <span>Quick Jump...</span>
            </span>
            <kbd className="rounded border border-white/[0.08] bg-zinc-900 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">
              ⌘K
            </kbd>
          </button>
        </div>

        {/* Navigation Links */}
        <nav className="flex-1 min-h-0 overflow-y-auto sidebar-scrollbar px-3 py-2 space-y-3.5 lg:space-y-4">
          {visibleNavGroups.map((grp) => (
            <div key={grp.group} className="space-y-0.5 sm:space-y-0.5">
              <p className="px-3 lg:px-2.5 pt-1.5 pb-1 text-[11px] lg:text-[10px] font-semibold uppercase tracking-wider text-zinc-400 select-none">
                {grp.group}
              </p>
              {grp.items.map((item) => {
                const isActive =
                  (!selectedAppId && activeNav === item.id) ||
                  (selectedAppId && item.id === (selectedApp?.php ? 'php-sites' : 'apps'));
                return (
                  <button
                    key={item.id}
                    type="button"
                    onMouseEnter={() => prefetchRoute(item.id)}
                    onTouchStart={() => prefetchRoute(item.id)}
                    onClick={() => {
                      navigateTo(item.id);
                      setMobileMenuOpen(false);
                    }}
                    className={`relative flex w-full items-center gap-3 lg:gap-2.5 rounded-lg px-3 lg:px-2.5 py-2.5 lg:py-1.5 xl:py-2 text-[13px] lg:text-xs font-medium transition-all cursor-pointer whitespace-nowrap select-none touch-manipulation active:scale-[0.99] ${
                      isActive
                        ? 'bg-white/[0.08] text-zinc-100 font-semibold shadow-[inset_0_1px_0_0_rgba(255,255,255,0.08)]'
                        : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                    }`}
                  >
                    <span className={isActive ? 'text-orange-400 shrink-0' : 'text-zinc-500 shrink-0'}>
                      {item.icon}
                    </span>
                    <span>{item.label}</span>
                    {item.badge && (
                      <span
                        className={`ml-auto rounded-md px-1.5 py-0.5 font-mono text-[11px] lg:text-[10px] font-medium ${
                          item.badge.tone === 'emerald'
                            ? 'bg-emerald-500/12 text-emerald-400 ring-1 ring-emerald-500/25'
                            : item.badge.tone === 'violet'
                              ? 'bg-violet-500/12 text-violet-300 ring-1 ring-violet-500/25'
                              : 'bg-white/[0.06] text-zinc-400'
                        }`}
                      >
                        {item.badge.text}
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </nav>

        {/* User Footer */}
        <div className="p-3 sm:p-3 shrink-0 border-t border-white/[0.05]">
          <div className="flex items-center justify-between gap-2 rounded-xl border border-white/[0.07] bg-[#111115] px-3 py-2.5 lg:px-2.5 lg:py-2">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="flex h-7 w-7 lg:h-6 lg:w-6 shrink-0 items-center justify-center rounded-md bg-orange-500/15 font-mono text-[11px] lg:text-[10px] font-bold text-orange-400 uppercase ring-1 ring-orange-500/30">
                {(currentUser?.username || 'admin').slice(0, 2).toUpperCase()}
              </div>
              <div className="min-w-0">
                <span className="block truncate text-[13px] lg:text-xs font-medium text-zinc-200">
                  {currentUser?.username || 'admin'}
                </span>
              </div>
            </div>
            <button
              type="button"
              onClick={onLogout}
              title="Sign out of NextDeploy"
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-zinc-900/90 px-3 py-1 text-xs lg:text-[11px] font-medium text-zinc-400 hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-300 transition-all cursor-pointer shrink-0 active:scale-95"
            >
              <LogOut className="h-3.5 w-3.5 lg:h-3 lg:w-3" />
              <span>Logout</span>
            </button>
          </div>
        </div>
      </aside>
    </>
  );
};
