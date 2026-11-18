package edge

import (
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultFerronMainConfig is the nd-edge managed root Ferron entry config.
	DefaultFerronMainConfig = "/etc/ferron/nd-edge/ferron.conf"
	// DefaultFerronRootConfig is the system-wide Ferron config that includes DefaultFerronMainConfig.
	DefaultFerronRootConfig  = "/etc/ferron/ferron.conf"
	DefaultFerronManagedDir  = "/etc/ferron/nd-edge"
	DefaultFerronServiceUnit = "ferron.service"
	DefaultFerronServiceUser = "ferron"
	DefaultFerronBinary      = "ferron"
	DefaultFerronCertsDir    = "/var/cache/ferron-acme"
	DefaultFerronPIDFile     = "/run/ferron.pid"
	DefaultFerronAdminURL    = "http://127.0.0.1:8081"

	PlaceholderFerronFileName = "00-placeholder.conf"
	HealthFerronFileName      = "01-health.conf"
)

// DefaultFerronEnginePaths returns the canonical Shield VPS filesystem layout
// for the experimental Ferron engine.
func DefaultFerronEnginePaths() EnginePaths {
	return EnginePaths{
		MainConfig:     DefaultFerronMainConfig,
		ManagedDir:     DefaultFerronManagedDir,
		GlobalFile:     "/etc/ferron/nd-edge/global.conf",
		CommonFile:     "/etc/ferron/nd-edge/common.conf",
		PanelsLink:     "/etc/ferron/nd-edge/panels",
		OriginsLink:    "/etc/ferron/nd-edge/origins",
		UserDir:        "/etc/ferron/nd-edge/user",
		CertsDir:       DefaultFerronCertsDir,
		ManualCertsDir: "/etc/ferron/nd-edge/certs",
		ServiceUnit:    DefaultFerronServiceUnit,
		ServiceUser:    DefaultFerronServiceUser,
		Binary:         DefaultFerronBinary,
		PanelExt:       ".conf",
	}
}

// NewFerronEnginePaths roots all Ferron nd-edge paths inside rootDir for isolated
// tests and Docker validation.
func NewFerronEnginePaths(rootDir string) EnginePaths {
	managed := filepath.Join(rootDir, "etc", "ferron", "nd-edge")
	return EnginePaths{
		MainConfig:     filepath.Join(managed, "ferron.conf"),
		ManagedDir:     managed,
		GlobalFile:     filepath.Join(managed, "global.conf"),
		CommonFile:     filepath.Join(managed, "common.conf"),
		PanelsLink:     filepath.Join(managed, "panels"),
		OriginsLink:    filepath.Join(managed, "origins"),
		UserDir:        filepath.Join(managed, "user"),
		CertsDir:       filepath.Join(rootDir, "var", "cache", "ferron-acme"),
		ManualCertsDir: filepath.Join(managed, "certs"),
		ServiceUnit:    DefaultFerronServiceUnit,
		ServiceUser:    DefaultFerronServiceUser,
		Binary:         DefaultFerronBinary,
		PanelExt:       ".conf",
	}
}

func fillFerronPathDefaults(p EnginePaths) EnginePaths {
	def := DefaultFerronEnginePaths()
	if strings.TrimSpace(p.ManagedDir) == "" {
		p.ManagedDir = def.ManagedDir
	}
	if strings.TrimSpace(p.MainConfig) == "" {
		p.MainConfig = filepath.Join(p.ManagedDir, "ferron.conf")
	}
	if strings.TrimSpace(p.GlobalFile) == "" {
		p.GlobalFile = filepath.Join(p.ManagedDir, "global.conf")
	}
	if strings.TrimSpace(p.CommonFile) == "" {
		p.CommonFile = filepath.Join(p.ManagedDir, "common.conf")
	}
	if strings.TrimSpace(p.PanelsLink) == "" {
		p.PanelsLink = filepath.Join(p.ManagedDir, "panels")
	}
	if strings.TrimSpace(p.OriginsLink) == "" {
		p.OriginsLink = filepath.Join(p.ManagedDir, "origins")
	}
	if strings.TrimSpace(p.UserDir) == "" {
		p.UserDir = filepath.Join(p.ManagedDir, "user")
	}
	if strings.TrimSpace(p.CertsDir) == "" {
		p.CertsDir = def.CertsDir
	}
	if strings.TrimSpace(p.ManualCertsDir) == "" {
		p.ManualCertsDir = filepath.Join(p.ManagedDir, "certs")
	}
	if strings.TrimSpace(p.ServiceUnit) == "" {
		p.ServiceUnit = def.ServiceUnit
	}
	if strings.TrimSpace(p.ServiceUser) == "" {
		p.ServiceUser = def.ServiceUser
	}
	if strings.TrimSpace(p.Binary) == "" {
		p.Binary = def.Binary
	}
	if strings.TrimSpace(p.PanelExt) == "" {
		p.PanelExt = def.PanelExt
	}
	return p
}

// FerronEngine implements the experimental Engine driver for Ferron 3 on host-based
// nd-edge Shield nodes.
type FerronEngine struct {
	paths             EnginePaths
	acmeEmail         string
	runner            Runner
	nowFn             func() time.Time
	manualRenewWindow time.Duration
}

// NewFerronEngine constructs a FerronEngine with empty ACME email.
func NewFerronEngine(paths EnginePaths, runner Runner) *FerronEngine {
	return NewFerronEngineWithConfig(paths, nil, runner)
}

// NewFerronEngineWithConfig constructs a FerronEngine using cfg for ACME email.
func NewFerronEngineWithConfig(paths EnginePaths, cfg *Config, runner Runner) *FerronEngine {
	ep := fillFerronPathDefaults(paths)
	acmeEmail := ""
	if cfg != nil {
		acmeEmail = sanitizeSingleLine(cfg.ACMEEmail)
	}
	if runner == nil {
		runner = &FerronRunner{
			Binary:      ep.Binary,
			MainConfig:  ep.MainConfig,
			ServiceUnit: ep.ServiceUnit,
			AdminURL:    DefaultFerronAdminURL,
			PIDFile:     DefaultFerronPIDFile,
		}
	}
	return &FerronEngine{
		paths:             ep,
		acmeEmail:         acmeEmail,
		runner:            runner,
		nowFn:             func() time.Time { return time.Now().UTC() },
		manualRenewWindow: DefaultManualCertRenewWindow,
	}
}

// SetManualCertClock overrides the clock and renewal threshold used when
// checking imported manual TLS certificates (intended for unit tests).
func (e *FerronEngine) SetManualCertClock(nowFn func() time.Time, renewWindow time.Duration) {
	if nowFn != nil {
		e.nowFn = nowFn
	}
	e.manualRenewWindow = renewWindow
}

// Name returns EngineFerron ("ferron").
func (e *FerronEngine) Name() string {
	return EngineFerron
}

// Paths returns the Ferron filesystem layout and service metadata.
func (e *FerronEngine) Paths() EnginePaths {
	return e.paths
}

// Runner returns the FerronRunner implementing Runner.
func (e *FerronEngine) Runner() Runner {
	return e.runner
}

// PlaceholderFileName returns the Ferron glob-placeholder filename.
func (e *FerronEngine) PlaceholderFileName() string { return PlaceholderFerronFileName }

// PlaceholderContent returns the Ferron glob-placeholder contents.
func (e *FerronEngine) PlaceholderContent() string { return RenderPlaceholderFerron() }

// HealthFileName returns the Ferron empty-routes health probe filename.
func (e *FerronEngine) HealthFileName() string { return HealthFerronFileName }

// HealthContent returns the Ferron empty-routes :80 health responder config.
func (e *FerronEngine) HealthContent() string { return RenderHealthProbeFerron() }

// RenderGlobal renders /etc/ferron/nd-edge/ferron.conf and /etc/ferron/nd-edge/global.conf.
func (e *FerronEngine) RenderGlobal(cfg *Config, cfRanges []string, anyTrustedProxy bool) (map[string]string, error) {
	mainConf := e.RenderMainConfig(e.paths.GlobalFile, e.paths.CommonFile, e.paths.UserDir, e.paths.PanelsLink)
	globalConf := renderFerronGlobalConf(cfg, cfRanges, anyTrustedProxy)
	return map[string]string{
		e.paths.MainConfig: mainConf,
		e.paths.GlobalFile: globalConf,
	}, nil
}

// RenderMainConfig renders ferron.conf including global, common, user, and panel configs.
func (e *FerronEngine) RenderMainConfig(globalFile, commonFile, userDir, panelsDir string) string {
	if strings.TrimSpace(globalFile) == "" {
		globalFile = e.paths.GlobalFile
	}
	if strings.TrimSpace(commonFile) == "" {
		commonFile = e.paths.CommonFile
	}
	if strings.TrimSpace(userDir) == "" {
		userDir = e.paths.UserDir
	}
	if strings.TrimSpace(panelsDir) == "" {
		panelsDir = e.paths.PanelsLink
	}

	var b strings.Builder
	b.WriteString("# Managed by nd edge (ferron). DO NOT EDIT.\n")
	b.WriteString("include \"" + filepath.ToSlash(globalFile) + "\"\n")
	b.WriteString("include \"" + filepath.ToSlash(commonFile) + "\"\n")
	b.WriteString("include \"" + filepath.ToSlash(filepath.Join(userDir, "*.conf")) + "\"\n")
	b.WriteString("include \"" + filepath.ToSlash(filepath.Join(panelsDir, "*.conf")) + "\"\n")
	return b.String()
}

func renderFerronGlobalConf(cfg *Config, cfRanges []string, anyTrustedProxy bool) string {
	var b strings.Builder
	b.WriteString("# Managed by nd edge (ferron). DO NOT EDIT.\n")
	b.WriteString("{\n")
	b.WriteString("    admin {\n")
	b.WriteString("        listen \"127.0.0.1:8081\"\n")
	b.WriteString("        auth_token \"{{env.ND_FERRON_ADMIN_TOKEN}}\"\n")
	b.WriteString("    }\n")
	b.WriteString("    http {\n")
	b.WriteString("        protocols h1 h2\n")
	b.WriteString("        timeout \"3660s\"\n")
	b.WriteString("    }\n")

	if anyTrustedProxy {
		trusted := mergeAngieRealIPRanges(cfRanges, cfg)
		b.WriteString("    client_ip_from_header x-forwarded-for {\n")
		for _, entry := range trusted {
			b.WriteString("        trusted_proxy \"" + entry + "\"\n")
		}
		b.WriteString("    }\n")
	} else {
		b.WriteString("    # nd-edge: proxy trust disabled (no domains require proxy trust)\n")
	}

	b.WriteString("    strict_hosts\n")
	b.WriteString("}\n")
	return b.String()
}

// RenderCommon renders /etc/ferron/nd-edge/common.conf.
func (e *FerronEngine) RenderCommon() (map[string]string, error) {
	return map[string]string{
		e.paths.CommonFile: RenderCommonFerron(),
	}, nil
}

// RenderCommonFerron renders the shared Ferron snippet included by panel host blocks.
func RenderCommonFerron() string {
	return `# Managed by nd edge (ferron). DO NOT EDIT.
snippet nd_common {
    dynamic_compressed
}
`
}

// RenderPlaceholderFerron renders 00-placeholder.conf so include globs never match zero files.
func RenderPlaceholderFerron() string {
	return "# Managed by nd edge. Placeholder so Ferron include glob always matches at least one file.\n"
}

// RenderHealthProbeFerron renders 01-health.conf so an empty Shield node binds :80
// and returns 200 to panel connectivity probes while 0 routes exist.
func RenderHealthProbeFerron() string {
	return `# Managed by nd edge. Temporary :80 health responder while 0 routes are active.
http *:80 {
    status 200 {
        body "ok\n"
    }
}
`
}

// RenderPanel deterministically renders /etc/ferron/nd-edge/panels/<panelID>.conf.
func (e *FerronEngine) RenderPanel(in RenderPanelInput) (string, error) {
	if err := ValidatePanelID(in.PanelID); err != nil {
		return "", err
	}
	originsLink := in.OriginsLink
	if strings.TrimSpace(originsLink) == "" {
		originsLink = e.paths.OriginsLink
	}
	certsDir := e.paths.CertsDir
	if strings.TrimSpace(certsDir) == "" {
		certsDir = DefaultFerronCertsDir
	}
	port := in.OriginPort
	if port <= 0 || port > 65535 {
		port = 443
	}

	panelLabel := sanitizeCommentValue(in.PanelName)
	if panelLabel == "" {
		panelLabel = in.PanelID
	}
	etagLabel := sanitizeTokenValue(in.ETag)

	routes := make([]EdgeRoute, 0, len(in.Routes))
	for _, r := range in.Routes {
		if len(r.Hosts) == 0 {
			continue
		}
		if err := ValidateRouteID(r.ID); err != nil {
			return "", err
		}
		hosts := make([]string, 0, len(r.Hosts))
		for _, rawHost := range r.Hosts {
			norm, verr := NormalizeAndValidateHost(rawHost, "")
			if verr != nil {
				return "", fmt.Errorf("route %q host %q rejected: %w", r.ID, rawHost, verr)
			}
			hosts = append(hosts, norm)
		}
		sort.Strings(hosts)
		rCopy := r
		rCopy.Hosts = hosts
		routes = append(routes, rCopy)
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].ID < routes[j].ID
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by nd edge for panel %q (%s). DO NOT EDIT. etag=%s\n", panelLabel, in.PanelID, etagLabel)

	if len(routes) == 0 || len(in.OriginIPs) == 0 {
		return b.String(), nil
	}

	endpoints := make([]string, 0, len(in.OriginIPs))
	seenIP := make(map[netip.Addr]struct{}, len(in.OriginIPs))
	for _, addr := range in.OriginIPs {
		a := addr.Unmap()
		if _, ok := seenIP[a]; ok {
			continue
		}
		seenIP[a] = struct{}{}
		endpoints = append(endpoints, "https://"+net.JoinHostPort(a.String(), strconv.Itoa(port)))
	}
	sort.Strings(endpoints)

	now := time.Now().UTC()
	if e.nowFn != nil {
		now = e.nowFn()
	}
	pemPath := filepath.ToSlash(filepath.Join(originsLink, in.PanelID+".pem"))
	caMissing := in.CAStatus != CAStatusOK
	cacheDir := filepath.ToSlash(certsDir)

	for i, r := range routes {
		if i > 0 {
			b.WriteByte('\n')
		}
		groups := partitionAngieRouteTLSGroups(e.paths.ManualCertsDir, r.Hosts, now, e.manualRenewWindow)
		for gIdx, grp := range groups {
			if gIdx > 0 {
				b.WriteByte('\n')
			}
			writeFerronSiteBlock(&b, ferronSiteRenderParams{
				route:          r,
				hosts:          grp.hosts,
				acmeEmail:      e.acmeEmail,
				cacheDir:       cacheDir,
				manualCertPath: grp.manualCert,
				manualKeyPath:  grp.manualKey,
				endpoints:      endpoints,
				pemPath:        pemPath,
				caMissing:      caMissing,
			})
		}
	}

	return b.String(), nil
}

type ferronSiteRenderParams struct {
	route          EdgeRoute
	hosts          []string
	acmeEmail      string
	cacheDir       string
	manualCertPath string
	manualKeyPath  string
	endpoints      []string
	pemPath        string
	caMissing      bool
}

func writeFerronSiteBlock(b *strings.Builder, p ferronSiteRenderParams) {
	b.WriteString(strings.Join(p.hosts, ", ") + " {\n")
	b.WriteString("    use nd_common\n")

	if p.manualCertPath != "" && p.manualKeyPath != "" {
		b.WriteString("    tls {\n")
		b.WriteString("        provider manual\n")
		b.WriteString("        cert \"" + p.manualCertPath + "\"\n")
		b.WriteString("        key \"" + p.manualKeyPath + "\"\n")
		b.WriteString("    }\n")
	} else {
		b.WriteString("    tls {\n")
		b.WriteString("        provider acme\n")
		b.WriteString("        challenge http-01\n")
		if p.acmeEmail != "" {
			b.WriteString("        contact \"" + p.acmeEmail + "\"\n")
		}
		b.WriteString("        cache \"" + p.cacheDir + "\"\n")
		b.WriteString("    }\n")
	}

	if p.route.Options.MaxBodyMB > 0 {
		fmt.Fprintf(b, "    # note: Ferron 3 does not enforce per-route MaxBodyMB (%dMB)\n", p.route.Options.MaxBodyMB)
	}

	b.WriteString("    proxy {\n")
	for _, ep := range p.endpoints {
		b.WriteString("        upstream \"" + ep + "\" {\n")
		b.WriteString("            limit 256\n")
		b.WriteString("            idle_timeout \"90s\"\n")
		b.WriteString("            connection_timeout \"10s\"\n")
		b.WriteString("        }\n")
	}
	if len(p.endpoints) > 1 {
		b.WriteString("        algorithm round_robin\n")
	}
	if p.route.Options.UpstreamH2 && !p.route.Options.Websocket {
		b.WriteString("        http2\n")
	}
	b.WriteString("        keepalive\n")
	b.WriteString("        retry_connection\n")
	b.WriteString("        retry_interval \"0.25s\"\n")
	if p.caMissing {
		b.WriteString("        no_verification\n")
	} else {
		b.WriteString("        ca \"" + p.pemPath + "\"\n")
	}
	b.WriteString("        request_header X-Forwarded-Proto \"https\"\n")
	if !p.route.TrustedProxy {
		b.WriteString("        request_header X-Real-IP \"{{remote.ip}}\"\n")
		b.WriteString("        request_header X-Forwarded-For \"{{remote.ip}}\"\n")
		b.WriteString("        request_header -CF-Connecting-IP\n")
	}
	b.WriteString("    }\n")
	b.WriteString("}\n")
}
