import React from 'react';
import { AppStack } from '../../types/nextdeploy';
import type { ProxyBootstrapInfo } from '../../lib/api/http';
import { useProxyStack } from './hooks/useProxyStack';
import { ProxyHeaderBar } from './components/ProxyHeaderBar';
import { PanelDomainCard } from './components/PanelDomainCard';
import { SharedVolumesCard } from './components/SharedVolumesCard';
import { ProxyControlCard } from './components/ProxyControlCard';
import { ProxyConfigViewerCard } from './components/ProxyConfigViewerCard';
import { ProxyLogsCard } from './components/ProxyLogsCard';
import { ProxyEngineSettingsCard } from './components/ProxyEngineSettingsCard';

interface ProxyViewProps {
  apps: AppStack[];
  dockerVolumes?: string[];
  caddyInfo?: ProxyBootstrapInfo | null;
  onRefreshCaddy?: () => Promise<void>;
}

export const ProxyView: React.FC<ProxyViewProps> = ({
  apps,
  dockerVolumes = [],
  caddyInfo,
  onRefreshCaddy,
}) => {
  const s = useProxyStack(apps, dockerVolumes, caddyInfo, onRefreshCaddy);

  return (
    <div className="space-y-4">
      <ProxyHeaderBar
        engine={s.normalizedEngine}
        version={s.version}
        running={s.running}
        experimental={s.experimental}
        lastApply={s.lastApply}
        applyBanner={s.applyBanner}
      />

      <ProxyEngineSettingsCard
        onSwitched={() => {
          void s.loadOverview();
          void s.loadLogs();
          void onRefreshCaddy?.();
        }}
      />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <PanelDomainCard
          panelDomain={s.panelDomain}
          setPanelDomain={s.setPanelDomain}
          panelEnableHttps={s.panelEnableHttps}
          setPanelEnableHttps={s.setPanelEnableHttps}
          panelEnableWww={s.panelEnableWww}
          setPanelEnableWww={s.setPanelEnableWww}
          serverIp={s.serverIp}
          saving={s.savingPanel}
          onSave={s.handleSavePanel}
        />

        <SharedVolumesCard
          sharedMountPrefix={s.sharedMountPrefix}
          setSharedMountPrefix={s.setSharedMountPrefix}
          allVolumes={s.allDockerVolumes}
          selectedVolumes={s.selectedSharedVolumes}
          onToggleVolume={(vol) =>
            s.setSelectedSharedVolumes((prev) =>
              prev.includes(vol) ? prev.filter((item) => item !== vol) : [...prev, vol]
            )
          }
          saving={s.savingVolumes}
          onSave={s.handleSaveVolumes}
        />
      </div>

      <ProxyControlCard
        engine={s.normalizedEngine}
        supportsAdminApi={s.capabilities.admin_api}
        running={s.running}
        acmeEmail={s.acmeEmail}
        setAcmeEmail={s.setAcmeEmail}
        proxyImage={s.proxyImage}
        setProxyImage={s.setProxyImage}
        adminApi={s.adminApi}
        setAdminApi={s.setAdminApi}
        savingSettings={s.savingSettings}
        actionPending={s.actionPending}
        onSaveSettings={s.handleSaveSettings}
        onLifecycleAction={s.handleLifecycle}
      />

      <ProxyConfigViewerCard
        engine={s.normalizedEngine}
        rootCompose={s.rootCompose}
        liveConfig={s.liveConfig}
        loading={s.overviewLoading}
        error={s.overviewError}
        onRefresh={() => void s.loadOverview()}
      />

      <ProxyLogsCard
        lines={s.logLines}
        loading={s.logsLoading}
        error={s.logsError}
        onRefresh={() => void s.loadLogs()}
      />
    </div>
  );
};
