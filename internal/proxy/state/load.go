package state

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

// LoadOptions supplies optional callbacks to LoadBuildInput so state loading
// remains decoupled from handlers, Docker, and host sysinfo packages.
type LoadOptions struct {
	// ResolveAppTarget optionally resolves compose project names, container
	// names, workspace paths, or port bindings for an app.
	ResolveAppTarget func(ctx context.Context, app db.App) (AppTarget, error)

	// ResolvePublicIP optionally returns the origin host's public IP address
	// used as fallback for non-shielded preview hostnames.
	ResolvePublicIP func(ctx context.Context) string
}

// LoadAndBuild loads pre-assembled proxy inputs from store and builds the
// engine-agnostic *proxy.State IR.
func LoadAndBuild(ctx context.Context, store *db.Store, opts LoadOptions) (*proxy.State, error) {
	in, err := LoadBuildInput(ctx, store, opts)
	if err != nil {
		return nil, err
	}
	return Build(in)
}

// LoadBuildInput reads all proxy-relevant state from store's SQLite tables
// (settings, caddy_config, shield_nodes, users, apps, app_domains, php_sites,
// php_security, proxy_routes, allocated_ports) into a pure BuildInput.
func LoadBuildInput(ctx context.Context, store *db.Store, opts LoadOptions) (BuildInput, error) {
	if store == nil {
		return BuildInput{}, errors.New("db store is required")
	}

	settings, err := store.GetAllSettings(ctx)
	if err != nil {
		return BuildInput{}, err
	}
	caddyCfg, err := store.GetAllCaddyConfig(ctx)
	if err != nil {
		return BuildInput{}, err
	}
	shieldNodes, err := store.ListShieldNodes(ctx)
	if err != nil {
		return BuildInput{}, err
	}
	users, err := store.ListUsers(ctx)
	if err != nil {
		return BuildInput{}, err
	}
	apps, err := store.ListApps(ctx)
	if err != nil {
		return BuildInput{}, err
	}
	domains, err := store.ListAllAppDomains(ctx)
	if err != nil {
		return BuildInput{}, err
	}
	phpRows, err := store.ListPHPSites(ctx)
	if err != nil {
		return BuildInput{}, err
	}
	routes, err := store.ListProxyRoutes(ctx, nil)
	if err != nil {
		return BuildInput{}, err
	}
	ports, err := store.ListAllocatedPorts(ctx)
	if err != nil {
		return BuildInput{}, err
	}

	var panelShieldID *int64
	if rawSID := strings.TrimSpace(settings["panel_shield_node_id"]); rawSID != "" {
		if id, perr := strconv.ParseInt(rawSID, 10, 64); perr == nil && id > 0 {
			panelShieldID = &id
		}
	}

	proxyEngine := strings.ToLower(strings.TrimSpace(settings[db.SettingProxyEngine]))
	if proxyEngine == "" {
		proxyEngine = string(proxy.EngineCaddy)
	}

	var defaultPublicIP string
	if opts.ResolvePublicIP != nil {
		defaultPublicIP = strings.TrimSpace(opts.ResolvePublicIP(ctx))
	}

	shieldByID := make(map[int64]db.ShieldNode, len(shieldNodes))
	for _, n := range shieldNodes {
		shieldByID[n.ID] = n
	}
	usersByID := make(map[int64]db.User, len(users))
	for _, u := range users {
		usersByID[u.ID] = u
	}

	phpInputs := make([]PHPSiteInput, 0, len(phpRows))
	for _, ps := range phpRows {
		secRaw := store.GetPHPSecurityJSON(ctx, ps.AppID)
		var sec proxy.SecuritySpec
		if strings.TrimSpace(secRaw) != "" {
			_ = json.Unmarshal([]byte(secRaw), &sec)
		}
		ownerSuspended := false
		if owner, ok := usersByID[ps.OwnerID]; ok && owner.Status == db.UserStatusSuspended {
			ownerSuspended = true
		}
		var socketPath string
		if !strings.EqualFold(strings.TrimSpace(ps.SiteType), "static") && strings.TrimSpace(ps.PHPVersion) != "" {
			socketPath = defaultPHPSocketPath(ps.OwnerID, ps.PHPVersion, ps.AppID)
		}
		phpInputs = append(phpInputs, PHPSiteInput{
			Site:           ps,
			Security:       sec,
			SecurityJSON:   secRaw,
			OwnerSuspended: ownerSuspended,
			DocRoot:        defaultPHPDocRoot(ps.AppID, ps.RootDir),
			SocketPath:     socketPath,
		})
	}

	appTargets := make(map[string]AppTarget, len(apps))
	for _, app := range apps {
		target := loadDefaultAppTarget(app, settings, usersByID, shieldByID, defaultPublicIP)
		if opts.ResolveAppTarget != nil {
			custom, rerr := opts.ResolveAppTarget(ctx, app)
			if rerr != nil {
				return BuildInput{}, rerr
			}
			target = mergeAppTarget(target, custom)
		}
		appTargets[app.ID] = target
	}

	return BuildInput{
		ProxyEngine:          proxyEngine,
		AdminAPI:             strings.TrimSpace(caddyCfg["admin_api"]),
		PanelDomain:          strings.TrimSpace(settings["panel_domain"]),
		PanelEnableHTTPS:     parseSettingBool(settings["panel_enable_https"], true),
		PanelEnableWWW:       parseSettingBool(settings["panel_enable_www"], false),
		PanelID:              strings.TrimSpace(settings[db.SettingPanelID]),
		PanelShieldNodeID:    panelShieldID,
		ACMEEmail:            strings.TrimSpace(caddyCfg["email"]),
		SharedMountPrefix:    strings.TrimSpace(settings["caddy_shared_mount_prefix"]),
		SharedVolumeNamesRaw: settings["caddy_shared_volume_names"],
		Global:               parseStoredGlobalOptions(caddyCfg["global_options"]),
		ShieldNodes:          shieldNodes,
		DefaultPublicIP:      defaultPublicIP,
		Apps:                 apps,
		AppDomains:           domains,
		AppTargets:           appTargets,
		PHPSites:             phpInputs,
		ProxyRoutes:          routes,
		AllocatedPorts:       ports,
	}, nil
}

func loadDefaultAppTarget(
	app db.App,
	settings map[string]string,
	usersByID map[int64]db.User,
	shieldByID map[int64]db.ShieldNode,
	defaultPublicIP string,
) AppTarget {
	target := AppTarget{
		WorkspaceDir: defaultWorkspacesRoot + "/" + app.ID,
		PublicIP:     defaultPublicIP,
	}
	if rawPreview := strings.TrimSpace(settings["preview:"+app.ID]); rawPreview != "" {
		var pv AppPreviewConfig
		if json.Unmarshal([]byte(rawPreview), &pv) == nil && strings.TrimSpace(pv.Provider) != "" {
			target.Preview = &pv
		}
	}
	if owner, ok := usersByID[app.OwnerID]; ok && owner.DefaultShieldID != nil && *owner.DefaultShieldID > 0 {
		if node, ok := shieldByID[*owner.DefaultShieldID]; ok && strings.TrimSpace(node.IP) != "" {
			sid := node.ID
			target.ShieldNodeID = &sid
			target.PublicIP = strings.TrimSpace(node.IP)
		}
	}
	return target
}

func mergeAppTarget(base, custom AppTarget) AppTarget {
	if strings.TrimSpace(custom.WorkspaceDir) != "" {
		base.WorkspaceDir = custom.WorkspaceDir
	}
	if strings.TrimSpace(custom.ActiveProject) != "" {
		base.ActiveProject = custom.ActiveProject
	}
	if strings.TrimSpace(custom.StandbyProject) != "" {
		base.StandbyProject = custom.StandbyProject
	}
	if len(custom.ServiceContainers) > 0 {
		base.ServiceContainers = custom.ServiceContainers
	}
	if len(custom.StandbyContainers) > 0 {
		base.StandbyContainers = custom.StandbyContainers
	}
	if custom.Preview != nil {
		base.Preview = custom.Preview
	}
	if strings.TrimSpace(custom.PublicIP) != "" {
		base.PublicIP = custom.PublicIP
	}
	if custom.ShieldNodeID != nil {
		base.ShieldNodeID = custom.ShieldNodeID
	}
	if len(custom.PathRoutes) > 0 {
		base.PathRoutes = custom.PathRoutes
	}
	if len(custom.DomainPathRoutes) > 0 {
		base.DomainPathRoutes = custom.DomainPathRoutes
	}
	if len(custom.GatewayBindings) > 0 {
		base.GatewayBindings = custom.GatewayBindings
	}
	return base
}

func parseStoredGlobalOptions(raw string) proxy.GlobalOptions {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return proxy.OptionProfiles["default"]
	}
	var o proxy.GlobalOptions
	if json.Unmarshal([]byte(raw), &o) != nil {
		return proxy.OptionProfiles["default"]
	}
	norm, err := o.Normalize()
	if err != nil {
		return proxy.OptionProfiles["default"]
	}
	return norm
}

func parseSettingBool(v string, def bool) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
