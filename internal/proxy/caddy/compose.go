package caddy

import (
	"context"

	icaddy "panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/proxy"
)

const (
	// DefaultAdminAPI is Caddy's default Docker-internal Admin API endpoint.
	DefaultAdminAPI = icaddy.DefaultAdminAPI
	// CaddyImage is the default pinned caddy-docker-proxy image.
	CaddyImage = icaddy.CaddyImage
	// CaddyContainerName is the default Caddy container name.
	CaddyContainerName = icaddy.CaddyContainerName
)

// GenerateMergedComposeWithLabeler merges a Compose file using labeler (or default Caddy
// labels when labeler is nil) for services with attached domains.
func GenerateMergedComposeWithLabeler(base []byte, projectName, appID string, domains []db.AppDomain, panelEnv, cgroupParent string, labeler proxy.AppLabeler) ([]byte, error) {
	return icaddy.GenerateMergedComposeWithLabeler(base, projectName, appID, domains, panelEnv, cgroupParent, labeler)
}

// GenerateSlotMergedComposeWithEnvAndLabeler merges a slot compose override using labeler
// (or legacy Caddy labels when labeler is nil) while returning partitioned host port bindings.
func GenerateSlotMergedComposeWithEnvAndLabeler(base []byte, projectName, slot, appID string, domains []db.AppDomain, panelEnv, dotenvEnv, cgroupParent string, preview *db.AppDomain, labeler proxy.AppLabeler, previewOnly ...bool) ([]byte, []proxy.HostPortBinding, error) {
	return icaddy.GenerateSlotMergedComposeWithEnvAndLabeler(base, projectName, slot, appID, domains, panelEnv, dotenvEnv, cgroupParent, preview, labeler, previewOnly...)
}

// GenerateDomainLabels builds legacy caddy-docker-proxy container labels for a single domain preview.
func GenerateDomainLabels(d db.AppDomain) map[string]string {
	return icaddy.GenerateLabels(d)
}

// RouteLabels builds legacy caddy-docker-proxy container labels for a custom proxy route.
func RouteLabels(r db.ProxyRoute) map[string]string {
	return icaddy.RouteLabels(r)
}

// GlobalOptionLabels builds legacy caddy.* labels for GlobalOptions.
func GlobalOptionLabels(g proxy.GlobalOptions) map[string]string {
	return toCaddyGlobalOptions(g).Labels()
}

// AdminConfigGet fetches Caddy's live /config/ JSON via the Admin API.
func AdminConfigGet(ctx context.Context, adminAPI string) (string, error) {
	return icaddy.AdminConfigGet(ctx, adminAPI)
}

// AdminStatus checks Caddy's live /config/ endpoint reachability.
func AdminStatus(ctx context.Context, adminAPI string) (bool, string) {
	return icaddy.AdminStatus(ctx, adminAPI)
}
