export const SHIELD_PREP_COMMAND =
  'apt update && apt install -y curl ufw && ufw allow 80/tcp && ufw allow 443/tcp && ufw allow 443/udp';

export function getPanelOrigin(): string {
  if (typeof window === 'undefined') return '';
  return `${window.location.protocol}//${window.location.host}`;
}

export function shellQuote(val: string): string {
  return `'${val.replace(/'/g, `'\\''`)}'`;
}

export function buildInstallCommand(origin: string, token: string): string {
  const safeToken = shellQuote(token || 'YOUR_TOKEN');
  return `curl -sSL "${origin}/install-nd.sh" | bash && printf '%s\\n' ${safeToken} | nd edge install --panel-url "${origin}" --token-stdin`;
}

export function buildSetTokenCommand(panelId: string, token: string): string {
  const safeToken = shellQuote(token || 'YOUR_TOKEN');
  const pid = panelId || '<panel_id>';
  return `printf '%s\\n' ${safeToken} | nd edge set-token ${pid} --token-stdin`;
}

export function buildUpgradeCommand(): string {
  return 'nd edge upgrade';
}

export function buildUninstallCommand(panelId: string): string {
  return panelId ? `nd edge remove-panel ${panelId}` : 'nd edge uninstall --all';
}
