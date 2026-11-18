package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/proxy"

	"gopkg.in/yaml.v3"
)

// proxyStackOptions builds the root-stack options for a non-Caddy engine.
func (p *Panel) proxyStackOptions(engine, panelDomain string, enableHTTPS, enableWWW bool, email string, sharedMounts []caddy.NDSharedMount, gatewayPorts []db.AllocatedPort) proxy.StackOptions {
	opts := proxy.StackOptions{
		Engine:           proxy.Engine(normalizeProxyEngine(engine)),
		NetworkName:      caddy.PanelNetworkName,
		NetworkKey:       caddy.PanelNetworkKey,
		ACMEEmail:        email,
		PanelDomain:      panelDomain,
		PanelEnableHTTPS: enableHTTPS,
		PanelEnableWWW:   enableWWW,
	}
	if p.WorkspacesRoot != "" {
		opts.WorkspacesDir = p.WorkspacesRoot
	}
	for _, m := range sharedMounts {
		opts.SharedMounts = append(opts.SharedMounts, proxy.SharedMount{VolumeName: m.VolumeName, Target: m.Target, ReadOnly: true})
	}
	for _, ap := range gatewayPorts {
		if ap.Port <= 0 || ap.Direct {
			continue
		}
		opts.Gateways = append(opts.Gateways, proxy.GatewayPort{
			Port:     ap.Port,
			BindIP:   ap.BindIP,
			Protocol: ap.Protocol,
			AppID:    ap.AppID,
		})
	}
	return opts
}

// generateEngineRootCompose rewrites the root stack for a non-Caddy engine.
func (p *Panel) generateEngineRootCompose(ctx context.Context, engine string, base []byte, panelDomain string, enableHTTPS, enableWWW bool, email string, sharedMounts []caddy.NDSharedMount, gatewayPorts []db.AllocatedPort) ([]byte, error) {
	drv, err := p.proxyDriverFor(engine)
	if err != nil {
		return nil, err
	}
	svc, vols, err := drv.StackService(p.proxyStackOptions(engine, panelDomain, enableHTTPS, enableWWW, email, sharedMounts, gatewayPorts))
	if err != nil {
		return nil, fmt.Errorf("%s stack service: %w", drv.Name(), err)
	}
	if normalizeProxyEngine(engine) == "ferron" {
		p.fillFerronAdminToken(ctx, base, svc)
	}
	translateServiceVolumes(svc, p.hostPathOf)
	return caddy.GenerateRootStackComposeForEngine(base, "nd-proxy", svc, vols)
}

const (
	ferronAdminTokenEnv     = "ND_FERRON_ADMIN_TOKEN"
	settingFerronAdminToken = "proxy_ferron_admin_token"
)

// fillFerronAdminToken replaces the driver's "${ND_FERRON_ADMIN_TOKEN:?…}" placeholder with a
// concrete secret: the one already in the root compose (installer-generated) or a panel-generated
// one kept in settings. Without it `docker compose up` refuses to start the service after a
// switch to Ferron, because nothing defines that variable.
func (p *Panel) fillFerronAdminToken(ctx context.Context, base []byte, svc map[string]any) {
	env, _ := svc["environment"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	if cur, _ := env[ferronAdminTokenEnv].(string); cur != "" && !strings.HasPrefix(cur, "${") {
		return
	}
	tok := existingComposeEnv(base, "nd-proxy", ferronAdminTokenEnv)
	if tok == "" || strings.HasPrefix(tok, "${") {
		tok = strings.TrimSpace(p.DB.GetSetting(ctx, settingFerronAdminToken))
	}
	if tok == "" {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err == nil {
			tok = hex.EncodeToString(buf)
			_ = p.DB.SetSetting(ctx, settingFerronAdminToken, tok)
		}
	}
	if tok != "" {
		env[ferronAdminTokenEnv] = tok
		svc["environment"] = env
	}
}

// existingComposeEnv returns an environment value of a service in a compose document.
func existingComposeEnv(base []byte, service, key string) string {
	var doc struct {
		Services map[string]struct {
			Environment any `yaml:"environment"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal(base, &doc) != nil {
		return ""
	}
	s, ok := doc.Services[service]
	if !ok {
		return ""
	}
	switch v := s.Environment.(type) {
	case map[string]any:
		if val, ok := v[key]; ok && val != nil {
			return strings.TrimSpace(fmt.Sprint(val))
		}
	case []any:
		for _, item := range v {
			if k, val, ok := strings.Cut(fmt.Sprint(item), "="); ok && k == key {
				return strings.TrimSpace(val)
			}
		}
	}
	return ""
}

// translateServiceVolumes rewrites absolute bind-mount sources ("/src:/dst[:mode]") of a compose
// service to host paths. Driver stack services use panel paths, which differ from the host
// paths when the panel's data is mounted from elsewhere (e.g. ./data:/data).
func translateServiceVolumes(svc map[string]any, hostPath func(string) string) {
	if svc == nil || hostPath == nil {
		return
	}
	conv := func(v string) string {
		if !strings.HasPrefix(v, "/") {
			return v // named volume
		}
		src, rest, ok := strings.Cut(v, ":")
		if !ok {
			return v
		}
		return hostPath(src) + ":" + rest
	}
	switch vols := svc["volumes"].(type) {
	case []string:
		for i, v := range vols {
			vols[i] = conv(v)
		}
	case []any:
		for i, v := range vols {
			if sv, ok := v.(string); ok {
				vols[i] = conv(sv)
			}
		}
	}
}
