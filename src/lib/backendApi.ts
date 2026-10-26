// Unified NextDeploy Go Backend API Barrel
// Re-exports modular domain APIs under ./api/*

import { appsAndDockerApi } from './api/appsApi';
import { gitAndRegistryApi } from './api/gitAndRegistryApi';
import { backupsAndMigrateApi } from './api/backupsAndMigrateApi';
import { systemAndTokensApi } from './api/systemAndTokensApi';
import { phpApi } from './api/phpApi';
import { caddyApi } from './api/caddyApi';
import { shieldApi } from './api/shieldApi';

export {
  type ApiResult,
  type BootstrapData,
  type DomainFormPayload,
  formatSlotPreviewDomain,
  formatWildcardDomain,
  getServerPublicIp,
  setServerPublicIp,
} from './api/http';

export const backendApi = {
  ...appsAndDockerApi,
  ...phpApi,
  ...caddyApi,
  ...shieldApi,
  ...gitAndRegistryApi,
  ...backupsAndMigrateApi,
  ...systemAndTokensApi,
};
