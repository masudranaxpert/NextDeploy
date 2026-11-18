package edge

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAngieMainConfig  = "/etc/angie/angie.conf"
	DefaultAngieManagedDir  = "/etc/angie/nd-edge"
	DefaultAngieServiceUnit = "angie.service"
	DefaultAngieServiceUser = "angie"
	DefaultAngieBinary      = "angie"
	DefaultAngieCertsDir    = "/var/lib/angie/acme"
	DefaultAngiePIDFile     = "/run/angie/angie.pid"
	DefaultACMEDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

	PlaceholderAngieFileName = "00-placeholder.conf"
	HealthAngieFileName      = "01-health.conf"
)

// DefaultAngieEnginePaths returns the canonical Shield VPS filesystem layout
// for Angie.
func DefaultAngieEnginePaths() EnginePaths {
	return EnginePaths{
		MainConfig:     DefaultAngieMainConfig,
		ManagedDir:     DefaultAngieManagedDir,
		GlobalFile:     "/etc/angie/nd-edge/global.conf",
		CommonFile:     "/etc/angie/nd-edge/common.conf",
		PanelsLink:     "/etc/angie/nd-edge/panels",
		OriginsLink:    "/etc/angie/nd-edge/origins",
		UserDir:        "/etc/angie/nd-edge/user",
		CertsDir:       DefaultAngieCertsDir,
		ManualCertsDir: "/etc/angie/nd-edge/certs",
		ServiceUnit:    DefaultAngieServiceUnit,
		ServiceUser:    DefaultAngieServiceUser,
		Binary:         DefaultAngieBinary,
		PanelExt:       ".conf",
	}
}

// NewAngieEnginePaths roots all Angie nd-edge paths under rootDir for isolated
// tests and Docker staging validation.
func NewAngieEnginePaths(rootDir string) EnginePaths {
	managed := filepath.Join(rootDir, "etc", "angie", "nd-edge")
	return EnginePaths{
		MainConfig:     filepath.Join(rootDir, "etc", "angie", "angie.conf"),
		ManagedDir:     managed,
		GlobalFile:     filepath.Join(managed, "global.conf"),
		CommonFile:     filepath.Join(managed, "common.conf"),
		PanelsLink:     filepath.Join(managed, "panels"),
		OriginsLink:    filepath.Join(managed, "origins"),
		UserDir:        filepath.Join(managed, "user"),
		CertsDir:       filepath.Join(rootDir, "var", "lib", "angie", "acme"),
		ManualCertsDir: filepath.Join(managed, "certs"),
		ServiceUnit:    DefaultAngieServiceUnit,
		ServiceUser:    DefaultAngieServiceUser,
		Binary:         DefaultAngieBinary,
		PanelExt:       ".conf",
	}
}

func fillAngiePathDefaults(p EnginePaths) EnginePaths {
	def := DefaultAngieEnginePaths()
	if strings.TrimSpace(p.MainConfig) == "" {
		p.MainConfig = def.MainConfig
	}
	if strings.TrimSpace(p.ManagedDir) == "" {
		p.ManagedDir = def.ManagedDir
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

// AngieEngine implements Engine for Angie 1.12.4+ on host-based nd-edge nodes.
type AngieEngine struct {
	paths             EnginePaths
	acmeEmail         string
	runner            Runner
	nowFn             func() time.Time
	manualRenewWindow time.Duration
}

// NewAngieEngine constructs an AngieEngine with default ACME email empty.
func NewAngieEngine(paths EnginePaths, runner Runner) *AngieEngine {
	return NewAngieEngineWithConfig(paths, nil, runner)
}

// NewAngieEngineWithConfig constructs an AngieEngine using cfg for ACME email
// and optional settings.
func NewAngieEngineWithConfig(paths EnginePaths, cfg *Config, runner Runner) *AngieEngine {
	ep := fillAngiePathDefaults(paths)
	acmeEmail := ""
	if cfg != nil {
		acmeEmail = sanitizeSingleLine(cfg.ACMEEmail)
	}
	if runner == nil {
		runner = &AngieRunner{
			Binary:      ep.Binary,
			MainConfig:  ep.MainConfig,
			ServiceUnit: ep.ServiceUnit,
			PIDFile:     DefaultAngiePIDFile,
		}
	}
	return &AngieEngine{
		paths:             ep,
		acmeEmail:         acmeEmail,
		runner:            runner,
		nowFn:             func() time.Time { return time.Now().UTC() },
		manualRenewWindow: DefaultManualCertRenewWindow,
	}
}

// SetManualCertClock overrides the clock and renewal threshold used when
// checking imported manual TLS certificates (intended for unit tests).
func (e *AngieEngine) SetManualCertClock(nowFn func() time.Time, renewWindow time.Duration) {
	if nowFn != nil {
		e.nowFn = nowFn
	}
	e.manualRenewWindow = renewWindow
}

// Name returns EngineAngie ("angie").
func (e *AngieEngine) Name() string {
	return EngineAngie
}

// Paths returns the Angie filesystem layout and service metadata.
func (e *AngieEngine) Paths() EnginePaths {
	return e.paths
}

// Runner returns the AngieRunner implementing Runner.
func (e *AngieEngine) Runner() Runner {
	return e.runner
}

// PlaceholderFileName returns the Angie glob-placeholder filename.
func (e *AngieEngine) PlaceholderFileName() string { return PlaceholderAngieFileName }

// PlaceholderContent returns the Angie glob-placeholder contents.
func (e *AngieEngine) PlaceholderContent() string { return RenderPlaceholderAngie() }

// HealthFileName returns the Angie empty-routes health probe filename.
func (e *AngieEngine) HealthFileName() string { return HealthAngieFileName }

// HealthContent returns the Angie empty-routes :80 health responder config.
func (e *AngieEngine) HealthContent() string { return RenderHealthProbeAngie() }

// RenderGlobal renders both the main angie.conf (worker/event limits and http
// includes) and /etc/angie/nd-edge/global.conf (timeouts and real_ip ranges).
func (e *AngieEngine) RenderGlobal(cfg *Config, cfRanges []string, anyTrustedProxy bool) (map[string]string, error) {
	mainConf := e.RenderMainConfig(e.paths.GlobalFile, e.paths.CommonFile, e.paths.UserDir, e.paths.PanelsLink)
	globalConf := renderAngieGlobalConf(cfg, cfRanges, anyTrustedProxy)
	return map[string]string{
		e.paths.MainConfig: mainConf,
		e.paths.GlobalFile: globalConf,
	}, nil
}

// RenderMainConfig renders the top-level angie.conf pointing at the specified
// global, common, user, and panels include paths (useful for both live and
// staged validation configs).
func (e *AngieEngine) RenderMainConfig(globalFile, commonFile, userDir, panelsDir string) string {
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
	svcUser := sanitizeSingleLine(e.paths.ServiceUser)
	if svcUser == "" {
		svcUser = DefaultAngieServiceUser
	}

	var b strings.Builder
	b.WriteString("# Managed by nd edge (angie). DO NOT EDIT.\n")
	b.WriteString("user " + svcUser + ";\n")
	b.WriteString("worker_processes auto;\n")
	b.WriteString("worker_rlimit_nofile 1048576;\n")
	b.WriteString("worker_shutdown_timeout 10m;\n\n")
	b.WriteString("error_log /var/log/angie/error.log warn;\n")
	b.WriteString("pid /run/angie/angie.pid;\n\n")
	b.WriteString("events {\n")
	b.WriteString("\tworker_connections 65535;\n")
	b.WriteString("}\n\n")
	b.WriteString("http {\n")
	b.WriteString("\tinclude /etc/angie/mime.types;\n")
	b.WriteString("\tdefault_type application/octet-stream;\n")
	b.WriteString("\taccess_log off;\n\n")
	b.WriteString("\tinclude " + filepath.ToSlash(globalFile) + ";\n")
	b.WriteString("\tinclude " + filepath.ToSlash(commonFile) + ";\n")
	b.WriteString("\tinclude " + filepath.ToSlash(filepath.Join(userDir, "*.conf")) + ";\n")
	b.WriteString("\tinclude " + filepath.ToSlash(filepath.Join(panelsDir, "*.conf")) + ";\n")
	b.WriteString("}\n")
	return b.String()
}

func renderAngieGlobalConf(cfg *Config, cfRanges []string, anyTrustedProxy bool) string {
	var b strings.Builder
	b.WriteString("# Managed by nd edge (angie). DO NOT EDIT.\n")
	b.WriteString("client_header_timeout 10s;\n")
	b.WriteString("client_body_timeout 15s;\n")
	b.WriteString("keepalive_timeout 20s;\n")
	b.WriteString("send_timeout 60s;\n")
	b.WriteString("client_max_body_size 0;\n\n")
	b.WriteString("variables_hash_max_size 8192;\n")
	b.WriteString("variables_hash_bucket_size 128;\n")
	b.WriteString("server_names_hash_max_size 8192;\n")
	b.WriteString("server_names_hash_bucket_size 128;\n")

	if !anyTrustedProxy {
		b.WriteString("\n# nd-edge: trusted_proxies disabled (no domains require proxy trust)\n")
		return b.String()
	}

	trusted := mergeAngieRealIPRanges(cfRanges, cfg)
	b.WriteByte('\n')
	for _, entry := range trusted {
		b.WriteString("set_real_ip_from " + entry + ";\n")
	}
	b.WriteString("real_ip_header CF-Connecting-IP;\n")
	b.WriteString("real_ip_recursive on;\n")
	return b.String()
}

func mergeAngieRealIPRanges(cfRanges []string, cfg *Config) []string {
	ranges := normalizeCIDRList(cfRanges)
	if len(ranges) == 0 {
		ranges = normalizeCIDRList(DefaultCloudflareRanges)
	}
	seen := make(map[string]struct{}, len(ranges))
	out := make([]string, 0, len(ranges))
	for _, r := range ranges {
		seen[r] = struct{}{}
		out = append(out, r)
	}
	if cfg != nil {
		for _, raw := range cfg.ShieldIPs {
			s := strings.TrimSpace(raw)
			if s == "" {
				continue
			}
			var norm string
			if p, err := netip.ParsePrefix(s); err == nil {
				norm = p.Masked().String()
			} else if a, err := netip.ParseAddr(s); err == nil && a.Zone() == "" {
				norm = a.Unmap().String()
			} else {
				continue
			}
			if _, ok := seen[norm]; !ok {
				seen[norm] = struct{}{}
				out = append(out, norm)
			}
		}
	}
	sort.Strings(out)
	return out
}

// RenderCommon renders /etc/angie/nd-edge/common.conf.
func (e *AngieEngine) RenderCommon() (map[string]string, error) {
	return map[string]string{
		e.paths.CommonFile: RenderCommonAngie(),
	}, nil
}

// RenderCommonAngie renders the shared Angie http-level directives (WebSocket
// connection upgrade map and gzip settings).
func RenderCommonAngie() string {
	return `# Managed by nd edge (angie). DO NOT EDIT.
server_tokens off;

map $http_upgrade $nd_connection_upgrade {
	default upgrade;
	'' '';
}

gzip on;
gzip_vary on;
gzip_proxied any;
gzip_comp_level 5;
gzip_min_length 256;
gzip_types text/plain text/css text/xml application/json application/javascript application/xml application/rss+xml image/svg+xml;
` + renderAngieResolver(angieResolvers()) + angieRejectUnknownTLS
}

// angieRejectUnknownTLS makes Angie refuse TLS for names no site serves (and clients without
// SNI). Without it the first 443 server in include order becomes the default, so an unknown
// name is answered with another site's certificate and proxied to that site; a manual site
// whose certificate path uses $ssl_server_name would even try to load "<unknown>.crt".
const angieRejectUnknownTLS = `
server {
	listen 443 ssl default_server;
	listen [::]:443 ssl default_server;
	ssl_reject_handshake on;
}
`

// resolvConfPath is the system resolver configuration read for Angie's ACME clients.
var resolvConfPath = "/etc/resolv.conf"

// angieResolvers returns the nameservers Angie uses at run time. ACME clients resolve the
// directory host while running, and Angie before 1.11 rejects such a config without a resolver.
func angieResolvers() []string {
	var out []string
	if raw, err := os.ReadFile(resolvConfPath); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || f[0] != "nameserver" {
				continue
			}
			a, err := netip.ParseAddr(f[1])
			if err != nil || a.Zone() != "" {
				continue
			}
			if a.Is6() && !a.Is4In6() {
				out = append(out, "["+a.String()+"]")
			} else {
				out = append(out, a.Unmap().String())
			}
		}
	}
	if len(out) == 0 {
		out = []string{"1.1.1.1", "8.8.8.8"}
	}
	return out
}

func renderAngieResolver(servers []string) string {
	return "\nresolver " + strings.Join(servers, " ") + " valid=300s ipv6=off;\nresolver_timeout 10s;\n"
}

// RenderPlaceholderAngie renders 00-placeholder.conf so include globs always
// match at least one file.
func RenderPlaceholderAngie() string {
	return "# Managed by nd edge. Placeholder so Angie include glob always matches at least one file.\n"
}

// RenderHealthProbeAngie renders 01-health.conf so an empty Shield node binds
// port 80 and responds 200 to panel connectivity probes while 0 routes exist.
func RenderHealthProbeAngie() string {
	return `# Managed by nd edge. Temporary :80 health responder while 0 routes are active.
server {
	listen 80 default_server;
	listen [::]:80 default_server;
	status_zone nd_health;
	location / {
		return 200 "ok\n";
	}
}
`
}

type angieTLSGroup struct {
	hosts      []string
	manualCert string
	manualKey  string
	leaf       *x509.Certificate
}

// RenderPanel deterministically renders /etc/angie/nd-edge/panels/<panelID>.conf.
func (e *AngieEngine) RenderPanel(in RenderPanelInput) (string, error) {
	if err := ValidatePanelID(in.PanelID); err != nil {
		return "", err
	}
	originsLink := in.OriginsLink
	if strings.TrimSpace(originsLink) == "" {
		originsLink = e.paths.OriginsLink
	}
	certsDir := e.paths.CertsDir
	if strings.TrimSpace(certsDir) == "" {
		certsDir = DefaultAngieCertsDir
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
	var needH1, needH2 bool
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
		if rCopy.Options.UpstreamH2 && !rCopy.Options.Websocket {
			needH2 = true
		} else {
			needH1 = true
		}
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
		endpoints = append(endpoints, net.JoinHostPort(a.String(), strconv.Itoa(port)))
	}
	sort.Strings(endpoints)

	upBase := angieUpstreamIdent(in.PanelID)
	if needH1 {
		writeAngieUpstreamBlock(&b, upBase, endpoints)
		b.WriteByte('\n')
	}
	if needH2 {
		writeAngieUpstreamBlock(&b, upBase+"_h2", endpoints)
		b.WriteByte('\n')
	}

	now := time.Now().UTC()
	if e.nowFn != nil {
		now = e.nowFn()
	}
	accountKeyPath := filepath.ToSlash(filepath.Join(certsDir, "nd_account.key"))
	pemPath := filepath.ToSlash(filepath.Join(originsLink, in.PanelID+".pem"))
	caMissing := in.CAStatus != CAStatusOK

	for i, r := range routes {
		if i > 0 {
			b.WriteByte('\n')
		}
		baseID := angieRouteIdent(in.PanelID, r.ID)
		groups := partitionAngieRouteTLSGroups(e.paths.ManualCertsDir, r.Hosts, now, e.manualRenewWindow)

		for gIdx, grp := range groups {
			if gIdx > 0 {
				b.WriteByte('\n')
			}
			zoneID := baseID
			if gIdx > 0 {
				zoneID = fmt.Sprintf("%s_m%d", baseID, gIdx)
			}
			writeAngieSiteBlock(&b, angieSiteRenderParams{
				route:          r,
				hosts:          grp.hosts,
				zoneID:         zoneID,
				acmeEmail:      e.acmeEmail,
				accountKeyPath: accountKeyPath,
				manualCertPath: grp.manualCert,
				manualKeyPath:  grp.manualKey,
				upBase:         upBase,
				pemPath:        pemPath,
				caMissing:      caMissing,
			})
		}
	}

	return b.String(), nil
}

func partitionAngieRouteTLSGroups(manualCertsDir string, hosts []string, now time.Time, renewWindow time.Duration) []angieTLSGroup {
	var acmeHosts []string
	var manualGroups []angieTLSGroup

	for _, host := range hosts {
		// Check if an already-loaded manual SAN cert covers this host first.
		matchedExisting := false
		for i := range manualGroups {
			if manualGroups[i].leaf != nil && manualGroups[i].leaf.VerifyHostname(host) == nil {
				manualGroups[i].hosts = append(manualGroups[i].hosts, host)
				matchedExisting = true
				break
			}
		}
		if matchedExisting {
			continue
		}

		if certPath, keyPath, leaf, ok := LoadUsableManualCert(manualCertsDir, host, now, renewWindow); ok {
			manualGroups = append(manualGroups, angieTLSGroup{
				hosts:      []string{host},
				manualCert: filepath.ToSlash(certPath),
				manualKey:  filepath.ToSlash(keyPath),
				leaf:       leaf,
			})
			continue
		}
		acmeHosts = append(acmeHosts, host)
	}

	out := make([]angieTLSGroup, 0, len(manualGroups)+1)
	if len(acmeHosts) > 0 {
		out = append(out, angieTLSGroup{hosts: acmeHosts})
	}
	out = append(out, manualGroups...)
	return out
}

type angieSiteRenderParams struct {
	route          EdgeRoute
	hosts          []string
	zoneID         string
	acmeEmail      string
	accountKeyPath string
	manualCertPath string
	manualKeyPath  string
	upBase         string
	pemPath        string
	caMissing      bool
}

func writeAngieSiteBlock(b *strings.Builder, p angieSiteRenderParams) {
	serverNames := strings.Join(p.hosts, " ")
	isManual := p.manualCertPath != "" && p.manualKeyPath != ""

	if !isManual {
		fmt.Fprintf(b, "acme_client %s %s", p.zoneID, DefaultACMEDirectoryURL)
		if p.acmeEmail != "" {
			fmt.Fprintf(b, " email=%s", p.acmeEmail)
		}
		fmt.Fprintf(b, " account_key=%s;\n\n", p.accountKeyPath)
	}

	b.WriteString("server {\n")
	b.WriteString("\tlisten 80;\n")
	b.WriteString("\tlisten [::]:80;\n")
	b.WriteString("\tserver_name " + serverNames + ";\n")
	b.WriteString("\tstatus_zone " + p.zoneID + "_http;\n")
	b.WriteString("\treturn 308 https://$host$request_uri;\n")
	b.WriteString("}\n\n")

	b.WriteString("server {\n")
	b.WriteString("\tlisten 443 ssl;\n")
	b.WriteString("\tlisten [::]:443 ssl;\n")
	b.WriteString("\tlisten 443 quic;\n")
	b.WriteString("\tlisten [::]:443 quic;\n")
	b.WriteString("\thttp2 on;\n")
	b.WriteString("\tserver_name " + serverNames + ";\n")
	b.WriteString("\tstatus_zone " + p.zoneID + ";\n\n")

	if isManual {
		b.WriteString("\tssl_certificate " + p.manualCertPath + ";\n")
		b.WriteString("\tssl_certificate_key " + p.manualKeyPath + ";\n\n")
	} else {
		b.WriteString("\tacme " + p.zoneID + ";\n")
		b.WriteString("\tssl_certificate $acme_cert_" + p.zoneID + ";\n")
		b.WriteString("\tssl_certificate_key $acme_cert_key_" + p.zoneID + ";\n\n")
	}

	b.WriteString("\tadd_header Alt-Svc 'h3=\":443\"; ma=86400' always;\n")
	if p.route.Options.MaxBodyMB > 0 {
		fmt.Fprintf(b, "\tclient_max_body_size %dm;\n\n", p.route.Options.MaxBodyMB)
	} else {
		b.WriteString("\tclient_max_body_size 0;\n\n")
	}

	b.WriteString("\terror_page 502 503 504 =503 @nd_origin_unavailable;\n")
	b.WriteString("\tlocation @nd_origin_unavailable {\n")
	b.WriteString("\t\tadd_header Cache-Control \"no-store\" always;\n")
	b.WriteString("\t\treturn 503 \"Origin is temporarily unavailable. Please retry shortly.\\n\";\n")
	b.WriteString("\t}\n\n")

	useH2 := p.route.Options.UpstreamH2 && !p.route.Options.Websocket
	upstreamName := p.upBase
	if useH2 {
		upstreamName = p.upBase + "_h2"
	}

	b.WriteString("\tlocation / {\n")
	b.WriteString("\t\tproxy_pass https://" + upstreamName + ";\n")
	if useH2 {
		b.WriteString("\t\tproxy_http_version 2;\n")
		b.WriteString("\t\tproxy_set_header Connection \"\";\n")
	} else {
		b.WriteString("\t\tproxy_http_version 1.1;\n")
		b.WriteString("\t\tproxy_set_header Connection $nd_connection_upgrade;\n")
		b.WriteString("\t\tproxy_set_header Upgrade $http_upgrade;\n")
	}
	b.WriteString("\t\tproxy_set_header Host $host;\n")
	if p.route.TrustedProxy {
		b.WriteString("\t\tproxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("\t\tproxy_set_header X-Forwarded-For $remote_addr;\n")
	} else {
		b.WriteString("\t\tproxy_set_header X-Real-IP $realip_remote_addr;\n")
		b.WriteString("\t\tproxy_set_header X-Forwarded-For $realip_remote_addr;\n")
		b.WriteString("\t\tproxy_set_header CF-Connecting-IP \"\";\n")
	}
	b.WriteString("\t\tproxy_set_header X-Forwarded-Proto https;\n")
	b.WriteString("\t\tproxy_connect_timeout 10s;\n")
	b.WriteString("\t\tproxy_send_timeout 600s;\n")
	b.WriteString("\t\tproxy_read_timeout 1h;\n")
	b.WriteString("\t\tproxy_buffering off;\n")
	b.WriteString("\t\tproxy_ssl_server_name on;\n")
	b.WriteString("\t\tproxy_ssl_name $host;\n")
	if p.caMissing {
		b.WriteString("\t\tproxy_ssl_verify off;\n")
	} else {
		b.WriteString("\t\tproxy_ssl_verify on;\n")
		b.WriteString("\t\tproxy_ssl_verify_depth 2;\n")
		b.WriteString("\t\tproxy_ssl_trusted_certificate " + p.pemPath + ";\n")
	}
	b.WriteString("\t}\n")
	b.WriteString("}\n")
}

func writeAngieUpstreamBlock(b *strings.Builder, name string, endpoints []string) {
	fmt.Fprintf(b, "upstream %s {\n", name)
	fmt.Fprintf(b, "\tzone %s 64k;\n", name)
	for _, ep := range endpoints {
		fmt.Fprintf(b, "\tserver %s;\n", ep)
	}
	b.WriteString("\tkeepalive 64;\n")
	b.WriteString("}\n")
}

func shortSHA256Hex(s string, chars int) string {
	sum := sha256.Sum256([]byte(s))
	full := hex.EncodeToString(sum[:])
	if chars <= 0 || chars >= len(full) {
		return full
	}
	return full[:chars]
}

func sanitizeAngieIdent(s string, maxLen int) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
		if maxLen > 0 && b.Len() >= maxLen {
			break
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "r"
	}
	return out
}

func angieUpstreamIdent(panelID string) string {
	return "nd_up_" + ShortPanelID(panelID) + "_" + shortSHA256Hex(panelID, 4)
}

func angieRouteIdent(panelID, routeID string) string {
	return "nd_" + shortSHA256Hex(panelID, 6) + "_" + sanitizeAngieIdent(routeID, 10) + "_" + shortSHA256Hex(routeID, 6)
}
