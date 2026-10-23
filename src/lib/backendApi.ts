// Unified NextDeploy Go Backend API Barrel
// Re-exports modular domain APIs under ./api/*

import { appsAndDockerApi } from './api/appsApi';
import { gitAndRegistryApi } from './api/gitAndRegistryApi';
import { backupsAndMigrateApi } from './api/backupsAndMigrateApi';
import { systemAndTokensApi } from './api/systemAndTokensApi';

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
  ...gitAndRegistryApi,
  ...backupsAndMigrateApi,
  ...systemAndTokensApi,
};
