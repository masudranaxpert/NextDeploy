import { NavigationSection } from '../types/nextdeploy';

export const VALID_NAVIGATION_SECTIONS: NavigationSection[] = [
  'overview',
  'nextdeploy-ai',
  'monitor',
  'apps',
  'containers',
  'images',
  'volumes',
  'terminal',
  'git',
  'registries',
  'backup',
  'mcp',
  'cli-sessions',
  'migrate',
  'proxy',
  'proxy-options',
  'proxy-routes',
  'shield-proxies',
  'settings',
  'audit-logs',
  'users',
  'php-sites',
  'php-instance',
];

export const hasLoggedInCookie = (): boolean => {
  if (typeof document === 'undefined') return false;
  return /(?:^|;\s*)nd_logged_in=1(?:\s*;|$)/.test(document.cookie);
};

export const clearLoggedInCookie = (): void => {
  if (typeof document !== 'undefined') {
    document.cookie = 'nd_logged_in=; Path=/; Expires=Thu, 01 Jan 1970 00:00:01 GMT; SameSite=Lax';
  }
};

export interface ParsedRoute {
  nav: NavigationSection;
  appId: string | null;
}

export const parseInitialRoute = (): ParsedRoute => {
  if (typeof window === 'undefined') return { nav: 'overview', appId: null };
  const path = window.location.pathname.replace(/^\/+|\/+$/g, '');
  if (!path || path === 'overview') return { nav: 'overview', appId: null };

  // Matches /apps/:id
  const appMatch = path.match(/^apps\/([^/]+)$/);
  if (appMatch) {
    return { nav: 'apps', appId: appMatch[1] };
  }

  if (path === 'caddy') return { nav: 'proxy', appId: null };
  if (path === 'copilot') return { nav: 'nextdeploy-ai', appId: null }; // old URL

  if (VALID_NAVIGATION_SECTIONS.includes(path as NavigationSection)) {
    return { nav: path as NavigationSection, appId: null };
  }
  return { nav: 'overview', appId: null };
};

export const buildRoutePath = (
  nav: NavigationSection,
  appId: string | null = null,
  tab?: string
): string => {
  if (appId) {
    return `/apps/${appId}${tab ? `?tab=${tab}` : ''}`;
  }
  if (nav === 'overview') {
    return '/overview';
  }
  return `/${nav}`;
};
