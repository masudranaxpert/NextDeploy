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
  const arrowIdx = rawPort.indexOf('->');
  if (arrowIdx !== -1) {
    return `:${rawPort.slice(arrowIdx + 2)}`;
  }
  return rawPort.startsWith(':') ? rawPort : `:${rawPort}`;
};

export const extractContainerPort = (ports?: string): string => {
  if (!ports) return '—';
  const match = ports.match(/:(\d+)->/);
  if (match) return `:${match[1]}`;
  const simple = ports.match(/(\d+)\/tcp/);
  if (simple) return `:${simple[1]}`;
  return ports;
};
