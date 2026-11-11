import React from 'react';
import {
  Activity,
  Archive,
  ArrowLeftRight,
  Bot,
  Box,
  Cable,
  FileCode2,
  FileText,
  FolderGit2,
  Globe,
  HardDrive,
  KeyRound,
  Layers,
  LayoutGrid,
  Server,
  Settings,
  Shield,
  SlidersHorizontal,
  Terminal,
  Users,
} from 'lucide-react';
import { AppStack, NavigationSection } from '../../types/nextdeploy';

export interface NavItem {
  id: NavigationSection;
  label: string;
  icon: React.ReactNode;
  badge?: { text: string; tone: 'emerald' | 'violet' | 'count' };
}

export interface NavGroup {
  group: string;
  items: NavItem[];
}

export const ADMIN_ONLY_NAV: NavigationSection[] = [
  'nextdeploy-ai',
  'monitor',
  'terminal',
  'proxy',
  'proxy-options',
  'shield-proxies',
  'migrate',
  'users',
  'settings',
  'cli-sessions',
  'php-instance',
];

export function buildNavGroups(params: {
  apps: AppStack[];
  phpEnabled: boolean;
  userRole?: string;
}): NavGroup[] {
  const { apps, phpEnabled, userRole } = params;
  const isAdmin = userRole === 'admin';

  const groups: NavGroup[] = [
    {
      group: 'Platform',
      items: [
        { id: 'overview', label: 'Overview', icon: <LayoutGrid className="h-3.5 w-3.5" /> },
        {
          id: 'nextdeploy-ai',
          label: 'NextDeploy AI',
          icon: <Bot className="h-3.5 w-3.5" />,
          badge: { text: 'ND AI', tone: 'violet' },
        },
        {
          id: 'apps',
          label: 'Projects',
          icon: <Layers className="h-3.5 w-3.5" />,
          badge: { text: String(apps.filter((a) => !a.php).length), tone: 'count' },
        },
        { id: 'monitor', label: 'Monitoring', icon: <Activity className="h-3.5 w-3.5" /> },
      ],
    },
    ...(phpEnabled || isAdmin
      ? [
          {
            group: 'PHP Hosting',
            items: [
              {
                id: 'php-sites' as NavigationSection,
                label: 'PHP Sites',
                icon: <FileCode2 className="h-3.5 w-3.5" />,
                badge: { text: String(apps.filter((a) => a.php).length), tone: 'count' as const },
              },
              ...(isAdmin
                ? [{ id: 'php-instance' as NavigationSection, label: 'Instance', icon: <Server className="h-3.5 w-3.5" /> }]
                : []),
            ],
          },
        ]
      : []),
    {
      group: 'Docker Engine',
      items: [
        { id: 'containers', label: 'Containers', icon: <Box className="h-3.5 w-3.5" /> },
        { id: 'images', label: 'Images', icon: <Layers className="h-3.5 w-3.5" /> },
        { id: 'volumes', label: 'Volumes', icon: <HardDrive className="h-3.5 w-3.5" /> },
        { id: 'terminal', label: 'Host Terminal', icon: <Terminal className="h-3.5 w-3.5" /> },
      ],
    },
    {
      group: 'Integrations',
      items: [
        { id: 'git', label: 'Git Providers', icon: <FolderGit2 className="h-3.5 w-3.5" /> },
        { id: 'registries', label: 'Registries', icon: <HardDrive className="h-3.5 w-3.5" /> },
        { id: 'backup', label: 'Backup', icon: <Archive className="h-3.5 w-3.5" /> },
      ],
    },
    {
      group: 'Automation',
      items: [
        {
          id: 'mcp',
          label: 'MCP Server',
          icon: <Cable className="h-3.5 w-3.5" />,
        },
        {
          id: 'cli-sessions',
          label: 'CLI Sessions',
          icon: <KeyRound className="h-3.5 w-3.5" />,
        },
      ],
    },
    {
      group: 'Caddy',
      items: [
        { id: 'proxy', label: 'Proxy & TLS', icon: <Globe className="h-3.5 w-3.5" /> },
        { id: 'proxy-options', label: 'Server Options', icon: <SlidersHorizontal className="h-3.5 w-3.5" /> },
        { id: 'proxy-routes', label: 'Routes & Redirects', icon: <ArrowLeftRight className="h-3.5 w-3.5" /> },
        { id: 'shield-proxies', label: 'Shield Proxies', icon: <Shield className="h-3.5 w-3.5" /> },
      ],
    },
    {
      group: 'System',
      items: [
        { id: 'migrate', label: 'Migration', icon: <ArrowLeftRight className="h-3.5 w-3.5" /> },
        { id: 'users', label: 'Team & Quotas', icon: <Users className="h-3.5 w-3.5" /> },
        { id: 'audit-logs', label: 'Audit Trail', icon: <FileText className="h-3.5 w-3.5" /> },
        { id: 'settings', label: 'Settings', icon: <Settings className="h-3.5 w-3.5" /> },
      ],
    },
  ];

  return groups
    .map((g) => ({
      ...g,
      items: g.items.filter((i) => isAdmin || !ADMIN_ONLY_NAV.includes(i.id)),
    }))
    .filter((g) => g.items.length > 0);
}
