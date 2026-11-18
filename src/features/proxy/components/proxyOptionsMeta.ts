import type { ProxyCapabilities, ProxyEngineName, ProxyOptions } from '../../../lib/api/proxyApi';

export const OPTION_PROFILES: Array<{ id: string; title: string; text: string }> = [
  { id: 'default', title: 'Default', text: 'Use engine defaults.' },
  { id: 'strict', title: 'Strict', text: 'Short 15s-30s limits.' },
  { id: 'slow-upload', title: 'Slow upload', text: '10m upload pause limit for large forms.' },
  { id: 'streaming', title: 'Streaming / SSE', text: '30m idle and grace period.' },
];

export interface ProxyOptionFieldSpec {
  key: keyof Pick<ProxyOptions, 'read_header' | 'read_body_idle' | 'idle' | 'grace_period' | 'max_header_size'>;
  label: string;
  placeholder: string;
  capabilityKey?: keyof ProxyCapabilities;
  unsupportedReason?: string;
}

export const OPTION_FIELDS: ProxyOptionFieldSpec[] = [
  {
    key: 'read_header',
    label: 'Header timeout',
    placeholder: 'default 1m',
    capabilityKey: 'read_header_timeout',
    unsupportedReason: 'Not configurable on Ferron.',
  },
  {
    key: 'read_body_idle',
    label: 'Upload pause limit',
    placeholder: 'default 1m',
    capabilityKey: 'read_body_timeout',
    unsupportedReason: 'Not configurable on Ferron.',
  },
  {
    key: 'idle',
    label: 'Keep-alive timeout',
    placeholder: 'default 5m',
    capabilityKey: 'h2_idle_timeout',
    unsupportedReason: 'H2 idle timeout is not configurable on Ferron.',
  },
  {
    key: 'grace_period',
    label: 'Reload grace period',
    placeholder: 'default unlimited',
    capabilityKey: 'grace_period',
    unsupportedReason: 'Grace period tuning is not exposed by Ferron.',
  },
  {
    key: 'max_header_size',
    label: 'Header size limit',
    placeholder: 'default 16KB',
  },
];

export function fieldHint(spec: ProxyOptionFieldSpec, engine: ProxyEngineName, caps: ProxyCapabilities): {
  disabled: boolean;
  note?: string;
} {
  if (spec.capabilityKey && !caps[spec.capabilityKey]) {
    return {
      disabled: true,
      note: spec.unsupportedReason || 'Unsupported on active engine.',
    };
  }
  if (spec.key === 'max_header_size' && engine === 'ferron' && caps.max_header_size_http1) {
    return {
      disabled: false,
      note: 'Applies to HTTP/1.1 headers only on Ferron.',
    };
  }
  return { disabled: false };
}
