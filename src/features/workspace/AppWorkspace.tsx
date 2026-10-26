import React, { useState, useMemo, useEffect } from 'react';
import { toast } from 'sonner';
import {
  Archive as PhpTabArchive,
  Clock as PhpTabClock,
  Database as PhpTabDatabase,
  FolderOpen as PhpTabFolder,
  Globe as PhpTabGlobe,
  ScrollText as PhpTabLogs,
  Settings2 as PhpTabSettings,
  ShieldCheck as PhpTabShield,
  SquareTerminal as PhpTabTerminal,
  Users as PhpTabUsers,
} from 'lucide-react';
import type {
  AppStack,
  AppWorkspaceTab,
  DeploymentRecord,
} from '../../types/nextdeploy';
import {
  backendApi,
  formatSlotPreviewDomain,
} from '../../lib/backendApi';
import { copyToClipboard } from '../../lib/clipboard';
import { WorkspaceHero } from './WorkspaceHero';
import { ProjectOverviewTab } from './tabs/ProjectOverviewTab';
import { ProjectDeploymentsTab } from './tabs/ProjectDeploymentsTab';
import { ProjectDomainsTab } from './tabs/ProjectDomainsTab';
import { ProjectEnvTab } from './tabs/ProjectEnvTab';
import { ProjectContainersTab } from './tabs/ProjectContainersTab';
import { ProjectFileManagerTab } from './tabs/ProjectFileManagerTab';
import { ProjectGitTab } from './tabs/ProjectGitTab';
import { ProjectVolumesTab } from './tabs/ProjectVolumesTab';
import { ProjectBackupsTab } from './tabs/ProjectBackupsTab';
import { ProjectLogsTab } from './tabs/ProjectLogsTab';
import { ProjectCollaboratorsTab } from './tabs/ProjectCollaboratorsTab';
import { PhpDomainsTab } from '../php/PhpDomainsTab';
import { PhpSettingsTab } from '../php/PhpSettingsTab';
import { PhpDatabasesTab } from '../php/PhpDatabasesTab';
import { PhpCronTab } from '../php/PhpCronTab';
import { PhpLogsTab } from '../php/PhpLogsTab';
import { PhpSecurityTab } from '../php/PhpSecurityTab';
import { prefetchPhpSite } from '../php/ui';
import { XTermConsole } from '../../components/ui/XTermConsole';
import {
  BuildLogsModal,
  InviteMemberModal,
  DeleteAppDialog,
  MergedComposeModal,
  ProjectLimitsDialog,
  useAppLimits,
} from './dialogs';
import { renderStyledBuildLog } from './utils/renderStyledBuildLog';
import { mapBootstrapApp } from './utils/workspaceSync';
import { useWorkspaceLogs } from './utils/useWorkspaceLogs';

interface AppWorkspaceProps {
  app: AppStack;
  onBack: () => void;
  onNavigateToBackup?: () => void;
  onUpdateApp: (updated: AppStack) => void;
  onDeleteApp: (appId: string) => void;
  /** Reloads app data from the backend (used by PHP site tabs after changes). */
  onRefresh?: () => void | Promise<void>;
  /** The signed-in user's own limits and usage (null for admins). */
  quota?: {
    max_apps: number;
    apps_used: number;
    max_memory_mb: number;
    memory_used_mb: number;
    max_cpus: number;
    cpu_used_cores: number;
    max_storage_mb: number;
    storage_used_mb: number;
  } | null;
}

export const AppWorkspace: React.FC<AppWorkspaceProps> = ({
  app,
  onBack,
  onNavigateToBackup,
  onUpdateApp,
  onDeleteApp,
  onRefresh,
  quota = null,
}) => {
  const [activeTab, setActiveTab] = useState<AppWorkspaceTab>(() => {
    if (typeof window !== 'undefined') {
      const tabParam = new URLSearchParams(window.location.search).get('tab');
      const validTabs: AppWorkspaceTab[] = [
        'overview',
        'containers',
        'deployments',
        'terminal',
        'logs',
        'env',
        'domains',
        'files',
        'volumes',
        'backups',
        'git',
        'php-settings',
        'php-databases',
        'php-cron',
        'php-logs',
        'php-security',
        'collaborators',
      ];
      if (tabParam && validTabs.includes(tabParam as AppWorkspaceTab)) {
        return tabParam as AppWorkspaceTab;
      }
    }
    return app.php ? 'php-settings' : 'overview';
  });

  useEffect(() => {
    const handleUrlTab = () => {
      const tabParam = new URLSearchParams(window.location.search).get('tab');
      if (tabParam && tabParam !== activeTab) {
        setActiveTab(tabParam as AppWorkspaceTab);
      }
    };
    handleUrlTab();
    window.addEventListener('popstate', handleUrlTab);
    return () => window.removeEventListener('popstate', handleUrlTab);
  }, []);

  // Keep open tab in query param (?tab=)
  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get('tab') !== activeTab) {
      url.searchParams.set('tab', activeTab);
      window.history.replaceState(window.history.state, '', url);
    }
  }, [activeTab]);

  // Dialog & Modal states
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [limitsModalOpen, setLimitsModalOpen] = useState(false);
  const [memberModalOpen, setMemberModalOpen] = useState(false);
  const [mergedComposeModalOpen, setMergedComposeModalOpen] = useState(false);
  const [mergedComposeYaml, setMergedComposeYaml] = useState('');
  const [buildLogsDeployment, setBuildLogsDeployment] = useState<DeploymentRecord | null>(null);
  const [limitsReload, setLimitsReload] = useState(0);
  const { info: limitsInfo } = useAppLimits(app, activeTab === 'overview' || limitsModalOpen, limitsReload);

  const [copiedOwner, setCopiedOwner] = useState(false);
  const copyOwnerName = async () => {
    if (await copyToClipboard(app.owner)) {
      setCopiedOwner(true);
      setTimeout(() => setCopiedOwner(false), 1600);
    }
  };

  // Deployments state
  const [liveDeployState, setLiveDeployState] = useState<{
    running: boolean;
    action: string;
    output: string;
  }>({ running: false, action: '', output: '' });
  const [isBuildLogExpanded, setIsBuildLogExpanded] = useState<boolean | null>(null);

  // Refresh current app state from backend bootstrap
  const refreshAppFromBackend = async () => {
    try {
      const bootRes = await backendApi.bootstrap();
      if (!bootRes.ok || !bootRes.data) return;
      const updates = mapBootstrapApp(app, bootRes.data);
      if (updates) {
        onUpdateApp({ ...app, ...updates });
      }
    } catch {
      // Ignore refresh errors
    }
  };

  useEffect(() => {
    if (app.php) return;
    let active = true;
    let wasRunning = liveDeployState.running;
    let timer: number | undefined;

    const checkProgress = async () => {
      const res = await backendApi.deployProgress(app.id);
      if (!active || !res.ok || !res.data) return;
      const { running, action, output } = res.data;
      setLiveDeployState((prev) => {
        if (prev.running === running && prev.output === output && prev.action === action) {
          return prev;
        }
        return { running: Boolean(running), action: action || 'Deploy', output: output || '' };
      });
      if (wasRunning && !running) {
        wasRunning = false;
        await refreshAppFromBackend();
      } else if (running) {
        wasRunning = true;
      }
    };

    // Initial check on mount
    void checkProgress();

    // High frequency 1.2s polling only while deploy is actively executing
    // Relaxed 15s check when idle to catch external actions without background hammering
    const intervalMs = liveDeployState.running ? 1200 : 15000;
    timer = window.setInterval(checkProgress, intervalMs);

    return () => {
      active = false;
      if (timer) window.clearInterval(timer);
    };
  }, [app.id, app.php, liveDeployState.running]);

  const defaultWildcardHost = useMemo(() => {
    const doms = app.domains || [];
    return (doms.find((d) => d.isPrimary) || doms[0])?.domain || '';
  }, [app.domains]);

  const previewBaseHost = app.previewHostBase || '';

  const triggerDeploy = async (options?: { noCache?: boolean; rollbackFrom?: DeploymentRecord }) => {
    if (options?.rollbackFrom) {
      const dep = options.rollbackFrom;
      const rawLogId = String(dep.id).replace(/^dep-/, '');
      const res = await backendApi.rollbackDeployment(app.id, rawLogId);
      if (res.ok) {
        toast.success(res.message || `Rolled back production to ${dep.id}`);
        await refreshAppFromBackend();
      } else {
        toast.error(res.error || 'Rollback failed');
      }
      return;
    }

    const action = options?.noCache ? 'redeploy' : 'up';
    toast.info(`Starting ${options?.noCache ? 'clean rebuild' : 'deployment'} for ${app.name}...`);
    setLiveDeployState({
      running: true,
      action: options?.noCache ? 'Redeploy (pull + up)' : 'Deploy',
      output: 'Initializing compose build pipeline...\n',
    });
    const res = await backendApi.composeAction(app.id, action);
    if (!res.ok) {
      setLiveDeployState((prev) => ({ ...prev, running: false }));
      toast.error(res.error || 'Deployment failed to start');
      await refreshAppFromBackend();
      return;
    }
    setActiveTab('deployments');
    toast.success(`Deployment job started for ${app.name}`);
  };

  const toggleStopOrStart = async () => {
    const isStopped = app.status === 'stopped';
    const action = isStopped ? 'up' : 'down';
    toast.info(`${isStopped ? 'Starting' : 'Stopping'} ${app.name}...`);
    const res = await backendApi.composeAction(app.id, action);
    if (!res.ok) {
      toast.error(res.error || `Failed to ${isStopped ? 'start' : 'stop'} ${app.name}`);
      return;
    }
    toast.success(isStopped ? `Starting ${app.name}...` : `Stopping ${app.name}...`);
    setTimeout(() => {
      void refreshAppFromBackend();
    }, 1500);
  };

  // Logs Explorer State & Realtime Streaming
  const logs = useWorkspaceLogs(app, activeTab);

  const hasGitConnection = Boolean(app.gitRepo && app.gitRepo.trim() !== '');
  const isGitSyncActive = hasGitConnection && (app.gitSync ?? app.sourceType === 'git');

  useEffect(() => {
    if (!hasGitConnection && activeTab === 'git') {
      setActiveTab('overview');
    }
  }, [hasGitConnection, activeTab]);

  useEffect(() => {
    if (app.php) prefetchPhpSite(app.id);
  }, [app.id, !!app.php]);

  const activeSlotLower = (app.activeSlot || 'blue').toLowerCase() as 'blue' | 'green';
  const standbySlotName = (activeSlotLower === 'green' ? 'blue' : 'green') as 'blue' | 'green';
  const isPinnedProduction = Boolean(app.pinnedSlot && (app.pinnedSlot === 'blue' || app.pinnedSlot === 'green'));
  const runningContainersCount = app.services.filter((s) => s.state === 'running').length;

  const productionUrl = app.php
    ? app.primaryDomain
      ? `${app.domains.find((d) => d.domain === app.primaryDomain)?.forceHttps === false ? 'http' : 'https'}://${app.primaryDomain}`
      : ''
    : defaultWildcardHost
    ? `https://${defaultWildcardHost}`
    : '';
  const previewHost = previewBaseHost ? formatSlotPreviewDomain(previewBaseHost, standbySlotName) : '';
  const previewUrl = previewHost ? `https://${previewHost}` : '';

  // Tab configurations
  const phpTabs: { id: AppWorkspaceTab; label: string; count?: number; icon?: React.ReactNode }[] = [
    { id: 'php-settings', label: 'Settings', icon: <PhpTabSettings className="h-3.5 w-3.5" /> },
    { id: 'domains', label: 'Domains', count: app.domains.length, icon: <PhpTabGlobe className="h-3.5 w-3.5" /> },
    ...(app.php?.siteType !== 'static' ? [{ id: 'php-databases' as AppWorkspaceTab, label: 'Databases', icon: <PhpTabDatabase className="h-3.5 w-3.5" /> }] : []),
    { id: 'files', label: 'File Manager', icon: <PhpTabFolder className="h-3.5 w-3.5" /> },
    ...(app.php?.siteType !== 'static' ? [{ id: 'php-cron' as AppWorkspaceTab, label: 'Cron Jobs', icon: <PhpTabClock className="h-3.5 w-3.5" /> }] : []),
    ...(app.php?.siteType !== 'static' ? [{ id: 'php-logs' as AppWorkspaceTab, label: 'Logs', icon: <PhpTabLogs className="h-3.5 w-3.5" /> }] : []),
    ...(app.php?.siteType !== 'static' ? [{ id: 'terminal' as AppWorkspaceTab, label: 'Terminal', icon: <PhpTabTerminal className="h-3.5 w-3.5" /> }] : []),
    { id: 'php-security' as AppWorkspaceTab, label: 'Security', icon: <PhpTabShield className="h-3.5 w-3.5" /> },
    { id: 'backups', label: 'Backups', icon: <PhpTabArchive className="h-3.5 w-3.5" /> },
    { id: 'collaborators', label: 'Access', icon: <PhpTabUsers className="h-3.5 w-3.5" /> },
  ];

  const tabs: { id: AppWorkspaceTab; label: string; count?: number; icon?: React.ReactNode }[] = app.php ? phpTabs : [
    { id: 'overview', label: 'General' },
    { id: 'containers', label: 'Containers', count: app.services.length },
    { id: 'deployments', label: 'Deployments', count: app.deployments.length },
    { id: 'domains', label: 'Domains', count: app.domains.length },
    { id: 'env', label: 'Environment', count: app.envVars.length },
    { id: 'files', label: 'File Manager' },
    ...(hasGitConnection ? [{ id: 'git' as AppWorkspaceTab, label: 'Git' }] : []),
    { id: 'volumes', label: 'Volumes', count: app.volumes.length },
    { id: 'backups', label: 'Backups' },
    { id: 'logs', label: 'Logs' },
    { id: 'terminal', label: 'Terminal' },
    { id: 'collaborators', label: 'Access' },
  ];

  return (
    <div className="space-y-5">
      {/* Workspace Header, Breadcrumbs & Hero */}
      <WorkspaceHero
        app={app}
        tabs={tabs}
        activeTab={activeTab}
        onSelectTab={setActiveTab}
        onBack={onBack}
        runningContainersCount={runningContainersCount}
        productionUrl={productionUrl}
        activeSlotLower={activeSlotLower}
        isPinnedProduction={isPinnedProduction}
        hasGitConnection={hasGitConnection}
        isDeploying={liveDeployState.running}
        copiedOwner={copiedOwner}
        onCopyOwner={copyOwnerName}
        onToggleStopOrStart={toggleStopOrStart}
        onTriggerDeploy={triggerDeploy}
        onOpenLimits={() => setLimitsModalOpen(true)}
        onOpenDelete={() => setDeleteDialogOpen(true)}
        onOpenGit={() => setActiveTab('git')}
      />

      {/* Tab Contents */}
      {activeTab === 'overview' && (
        <ProjectOverviewTab
          app={app}
          onUpdateApp={onUpdateApp}
          hasGitConnection={hasGitConnection}
          isGitSyncActive={isGitSyncActive}
          detectedBranches={[]}
          limitsInfo={limitsInfo}
          productionUrl={productionUrl}
          previewUrl={previewUrl}
          previewHost={previewHost}
          standbySlotName={standbySlotName}
          onOpenLimitsModal={() => setLimitsModalOpen(true)}
          onOpenDeleteModal={() => setDeleteDialogOpen(true)}
          onOpenGitModal={() => setActiveTab('git')}
          onOpenFilesTab={() => setActiveTab('files')}
          onOpenGitTab={() => setActiveTab('git')}
          onViewCaddyCompose={async () => {
            const res = await backendApi.composeView(app.id);
            const fallbackYaml =
              app.files.find((f) => f.path.includes('docker-compose'))?.content ||
              'services:\n  app:\n    build: .\n';
            setMergedComposeYaml(res.yaml || fallbackYaml);
            setMergedComposeModalOpen(true);
          }}
        />
      )}

      {activeTab === 'containers' && (
        <ProjectContainersTab
          app={app}
          onRefresh={refreshAppFromBackend}
          onOpenLogs={(service) => {
            logs.setLogContainerFilter(service);
            setActiveTab('logs');
          }}
          onOpenTerminal={(service) => {
            logs.setTerminalService(service);
            setActiveTab('terminal');
          }}
        />
      )}

      {activeTab === 'deployments' && (
        <ProjectDeploymentsTab
          app={app}
          onUpdateApp={onUpdateApp}
          hasGitConnection={hasGitConnection}
          liveDeployState={liveDeployState}
          isBuildLogExpanded={isBuildLogExpanded}
          setIsBuildLogExpanded={setIsBuildLogExpanded}
          renderStyledBuildLog={renderStyledBuildLog}
          triggerDeploy={triggerDeploy}
          setBuildLogsDeployment={setBuildLogsDeployment}
          isPinnedProduction={isPinnedProduction}
          onRefresh={refreshAppFromBackend}
        />
      )}

      {activeTab === 'domains' && !app.php && (
        <ProjectDomainsTab app={app} onUpdateApp={onUpdateApp} />
      )}
      {activeTab === 'domains' && app.php && (
        <PhpDomainsTab app={app} onChanged={() => onRefresh?.()} onUpdateApp={onUpdateApp} />
      )}

      {activeTab === 'php-settings' && app.php && (
        <PhpSettingsTab
          app={app}
          onChanged={() => onRefresh?.()}
          onDelete={() => setDeleteDialogOpen(true)}
          onOpenFiles={() => setActiveTab('files')}
        />
      )}
      {activeTab === 'php-databases' && app.php && <PhpDatabasesTab app={app} />}
      {activeTab === 'php-cron' && app.php && <PhpCronTab app={app} />}
      {activeTab === 'php-logs' && app.php && <PhpLogsTab app={app} />}
      {activeTab === 'php-security' && app.php && <PhpSecurityTab app={app} />}

      {activeTab === 'env' && (
        <ProjectEnvTab app={app} onUpdateApp={onUpdateApp} />
      )}

      {activeTab === 'files' && (
        <ProjectFileManagerTab
          app={app}
          onUpdateApp={onUpdateApp}
          onRefresh={refreshAppFromBackend}
        />
      )}

      {activeTab === 'git' && (
        <ProjectGitTab app={app} onUpdateApp={onUpdateApp} />
      )}

      {activeTab === 'volumes' && (
        <ProjectVolumesTab app={app} onUpdateApp={onUpdateApp} />
      )}

      {activeTab === 'backups' && (
        <ProjectBackupsTab app={app} onOpenBackupDestinations={onNavigateToBackup} />
      )}

      {activeTab === 'logs' && (
        <ProjectLogsTab
          app={app}
          logContainerFilter={logs.logContainerFilter}
          setLogContainerFilter={logs.setLogContainerFilter}
          logLevelFilter={logs.logLevelFilter}
          setLogLevelFilter={logs.setLogLevelFilter}
          logSearch={logs.logSearch}
          setLogSearch={logs.setLogSearch}
          logLiveTail={logs.logLiveTail}
          setLogLiveTail={logs.setLogLiveTail}
          logTimeRange={logs.logTimeRange}
          setLogTimeRange={logs.setLogTimeRange}
          filteredLogs={logs.filteredLogs}
          logCounts={logs.logCounts}
          renderLogMessage={logs.renderLogMessage}
        />
      )}

      {activeTab === 'terminal' && (
        <XTermConsole
          hostLabel={app.php ? `site@${app.slug}` : `root@${app.slug}`}
          promptPrefix={app.php ? `site@${app.slug}:~$ ` : `root@${app.slug}:/app# `}
          services={app.services}
          appSlug={app.id}
          initialService={logs.terminalService || app.services[0]?.service}
          height={400}
        />
      )}

      {activeTab === 'collaborators' && (
        <ProjectCollaboratorsTab
          app={app}
          onRefresh={onRefresh}
          onOpenInviteModal={() => setMemberModalOpen(true)}
        />
      )}

      {/* Extracted Workspace Modals */}
      <BuildLogsModal
        open={Boolean(buildLogsDeployment)}
        onClose={() => setBuildLogsDeployment(null)}
        app={app}
        deployment={buildLogsDeployment}
        renderStyledBuildLog={renderStyledBuildLog}
        onRollback={(dep) => triggerDeploy({ rollbackFrom: dep })}
      />

      <InviteMemberModal
        open={memberModalOpen}
        onClose={() => setMemberModalOpen(false)}
        appId={app.id}
        onRefresh={onRefresh}
      />

      <MergedComposeModal
        open={mergedComposeModalOpen}
        onClose={() => setMergedComposeModalOpen(false)}
        appSlug={app.slug}
        yaml={mergedComposeYaml}
      />

      <ProjectLimitsDialog
        app={app}
        open={limitsModalOpen}
        onClose={() => setLimitsModalOpen(false)}
        onSaved={() => setLimitsReload((n) => n + 1)}
      />

      <DeleteAppDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        appName={app.name}
        appId={app.id}
        onConfirmDelete={onDeleteApp}
      />
    </div>
  );
};
