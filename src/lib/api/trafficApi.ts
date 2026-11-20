import { requestJson } from './http';

export interface TrafficCounters {
  requests: number;
  s2xx: number;
  s3xx: number;
  s4xx: number;
  s5xx: number;
}

export type TrafficSiteKind = 'project' | 'php' | 'wordpress' | 'static' | 'panel' | 'route' | 'other';

export interface TrafficSite {
  key: string;
  name: string;
  kind: TrafficSiteKind;
  app_id?: string;
  domains?: string[];
  owner?: string;
  total: TrafficCounters;
  last_minute: number;
  per_minute: number[];
  active: boolean;
}

export interface TrafficSummary {
  ok: boolean;
  engine: string;
  per_site: boolean;
  note?: string;
  window: number;
  since: string;
  updated: string;
  minutes: number[];
  per_minute: number[];
  total: TrafficCounters;
  coverage_minutes: number;
  sites: TrafficSite[];
}

export const trafficApi = {
  trafficSummary: (window: 15 | 30 | 60) => requestJson<TrafficSummary>(`/api/v1/traffic?window=${window}`),
};
