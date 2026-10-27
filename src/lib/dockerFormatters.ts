// Pure helpers to format Docker image names and port mappings compactly

export const formatCompactImage = (fullImage: string) => {
  const [repoPart, tagPart = 'latest'] = fullImage.split(':');
  const segments = repoPart.split('/');
  const shortName = segments[segments.length - 1];
  const registryPrefix =
    segments.length > 2 ? segments[0] : segments.length === 2 ? segments[0] : 'docker.io';
  return { shortName, tagPart, registryPrefix, fullImage };
};

export const formatCompactPort = (rawPort?: string, fallback: string = ''): string => {
  if (!rawPort) return fallback;
  const trimmed = rawPort.trim();
  if (!trimmed || trimmed === '—' || trimmed === '-' || trimmed.toLowerCase() === 'none') {
    return fallback;
  }

  // Parse comma-separated port list from Docker engine
  const parts = trimmed.split(',').map((p) => p.trim()).filter(Boolean);
  const portsSet = new Set<string>();

  for (const part of parts) {
    const hostMatch = part.match(/:(\d+)->/);
    if (hostMatch) {
      portsSet.add(hostMatch[1]);
      continue;
    }
    const arrowMatch = part.match(/->(\d+)/);
    if (arrowMatch) {
      portsSet.add(arrowMatch[1]);
      continue;
    }
    const protoMatch = part.match(/^(\d+)\/(tcp|udp)$/);
    if (protoMatch) {
      portsSet.add(protoMatch[1]);
      continue;
    }
    const numMatch = part.match(/\b\d{2,5}\b/);
    if (numMatch) {
      portsSet.add(numMatch[0]);
    }
  }

  const ports = Array.from(portsSet);
  if (ports.length === 0) {
    return fallback;
  }

  ports.sort((a, b) => Number(a) - Number(b));

  if (ports.length <= 2) {
    return ports.map((p) => `:${p}`).join(', ');
  }
  return `${ports.slice(0, 2).map((p) => `:${p}`).join(', ')} (+${ports.length - 2})`;
};

export const formatCompactUptime = (uptime?: string): string => {
  if (!uptime) return 'Active';
  let s = uptime.replace(/^Up\s+/i, '').trim();
  s = s.replace(/\(health:\s*([^)]+)\)/i, '($1)');
  s = s.replace(/Less than a second/i, '<1s');
  s = s.replace(/(\d+)\s+seconds?\b/gi, '$1s');
  s = s.replace(/(\d+)\s+minutes?\b/gi, '$1m');
  s = s.replace(/(\d+)\s+hours?\b/gi, '$1h');
  s = s.replace(/(\d+)\s+days?\b/gi, '$1d');
  s = s.replace(/(\d+)\s+weeks?\b/gi, '$1w');
  s = s.replace(/(\d+)\s+months?\b/gi, '$1mo');
  s = s.replace(/\bseconds?\b/gi, 's');
  s = s.replace(/\bminutes?\b/gi, 'm');
  s = s.replace(/\bhours?\b/gi, 'h');
  s = s.replace(/\bdays?\b/gi, 'd');
  s = s.replace(/\s+/g, ' ');
  return s;
};

export const extractContainerPort = (ports?: string): string => {
  if (!ports) return '—';
  const match = ports.match(/:(\d+)->/);
  if (match) return `:${match[1]}`;
  const simple = ports.match(/(\d+)\/tcp/);
  if (simple) return `:${simple[1]}`;
  return ports;
};
