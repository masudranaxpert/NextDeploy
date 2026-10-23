// Pure helpers to format Docker image names and port mappings compactly

export const formatCompactImage = (fullImage: string) => {
  const [repoPart, tagPart = 'latest'] = fullImage.split(':');
  const segments = repoPart.split('/');
  const shortName = segments[segments.length - 1];
  const registryPrefix =
    segments.length > 2 ? segments[0] : segments.length === 2 ? segments[0] : 'docker.io';
  return { shortName, tagPart, registryPrefix, fullImage };
};

export const formatCompactPort = (rawPort: string) => {
  if (!rawPort) return 'internal';
  const internalMatch = rawPort.match(/^(\d+)\/(tcp|udp)$/);
  if (internalMatch) {
    return `:${internalMatch[1]}`;
  }
  const arrowIdx = rawPort.indexOf('->');
  if (arrowIdx !== -1) {
    const bound = rawPort.slice(arrowIdx + 2).replace(/\/(tcp|udp)$/, '');
    return `:${bound}`;
  }
  return rawPort.startsWith(':') ? rawPort : `:${rawPort}`;
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
