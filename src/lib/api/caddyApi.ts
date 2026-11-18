import {
  proxyApi,
  type CaddyOptions,
  type ProxyOptions,
  type ProxyRoute,
} from './proxyApi';

export type { CaddyOptions, ProxyOptions, ProxyRoute };

export const caddyApi = {
  caddyOptions: proxyApi.proxyOptions,
  caddyOptionsSave: proxyApi.proxyOptionsSave,
  caddyRoutes: proxyApi.proxyRoutes,
  caddyRouteSave: proxyApi.proxyRouteSave,
  caddyRouteToggle: proxyApi.proxyRouteToggle,
  caddyRouteDelete: proxyApi.proxyRouteDelete,
};
