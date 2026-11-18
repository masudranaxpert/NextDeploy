package state

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

type phpSettingsPayload struct {
	Stopped bool              `json:"stopped,omitempty"`
	Routing proxy.SiteRouting `json:"routing,omitempty"`
}

func buildPHPSites(in PHPSiteInput, allDomains []db.AppDomain) ([]proxy.Site, error) {
	site := in.Site
	var valid []db.AppDomain
	for _, d := range allDomains {
		cleanDom := proxy.CleanQuotedValue(d.Domain)
		if !validPHPHost(cleanDom) {
			continue
		}
		d.Domain = cleanDom
		valid = append(valid, d)
	}
	if len(valid) == 0 {
		return nil, nil
	}
	sorted := sortedDomains(valid)

	parsed := phpSettingsPayload{}
	if raw := strings.TrimSpace(site.SettingsJSON); raw != "" {
		_ = json.Unmarshal([]byte(raw), &parsed)
	}
	routing := parsed.Routing
	if in.Routing != nil {
		routing = *in.Routing
	}
	if strings.EqualFold(strings.TrimSpace(routing.FallbackMode), proxy.FallbackDefault) {
		routing.FallbackMode = ""
	}

	sec, err := sanitizeSecuritySpec(in.Security, in.SecurityJSON)
	if err != nil {
		return nil, fmt.Errorf("php site %s security: %w", site.AppID, err)
	}

	siteType := strings.ToLower(strings.TrimSpace(site.SiteType))
	if siteType == "" {
		siteType = "php"
	}
	isStatic := siteType == "static"
	kind := proxy.SiteKindPHP
	if isStatic {
		kind = proxy.SiteKindStatic
	}

	docRoot := strings.TrimSpace(in.DocRoot)
	if docRoot == "" {
		docRoot = defaultPHPDocRoot(site.AppID, site.RootDir)
	}

	var socket string
	var phpVersion string
	if !isStatic {
		phpVersion = strings.TrimSpace(site.PHPVersion)
		socket = strings.TrimSpace(in.SocketPath)
		if socket == "" && phpVersion != "" {
			socket = defaultPHPSocketPath(site.OwnerID, phpVersion, site.AppID)
		}
	}

	suspended := parsed.Stopped || in.Stopped || in.OwnerSuspended

	var rules []proxy.RoutingRule
	if len(routing.Rules) > 0 {
		rules = make([]proxy.RoutingRule, len(routing.Rules))
		for i, r := range routing.Rules {
			r.Kind = strings.TrimSpace(strings.ToLower(r.Kind))
			r.MatchType = strings.TrimSpace(strings.ToLower(r.MatchType))
			if r.MatchType == "" {
				r.MatchType = "exact"
			}
			rules[i] = r
		}
	}

	out := make([]proxy.Site, 0, len(sorted))
	for i, d := range sorted {
		siteID := "php:" + site.AppID
		if i > 0 {
			if d.ID > 0 {
				siteID = fmt.Sprintf("php:%s:dom:%d", site.AppID, d.ID)
			} else {
				siteID = fmt.Sprintf("php:%s:idx:%d", site.AppID, i)
			}
		}

		behindShield := d.ShieldNodeID != nil && *d.ShieldNodeID > 0
		httpsOn := behindShield || d.EnableHTTPS

		cacheSpec := &proxy.PHPCacheSpec{
			WordPressPageCache: !isStatic && socket != "",
			MediaCacheControl:  "public, max-age=2592000, immutable",
			CodeCacheControl:   "public, max-age=604800, stale-while-revalidate=86400",
		}

		phpSpec := &proxy.PHPSpec{
			DocRoot:       docRoot,
			Socket:        socket,
			PHPVersion:    phpVersion,
			SiteType:      siteType,
			Suspended:     suspended,
			FallbackMode:  routing.FallbackMode,
			FallbackEntry: strings.TrimPrefix(strings.TrimSpace(routing.FallbackEntry), "/"),
			Routing:       rules,
			Compression:   []string{"zstd", "gzip"},
			Precompressed: []string{"gzip", "zstd"},
			HidePatterns:  defaultHidePatterns(),
			Cache:         cacheSpec,
			HTTPS:         httpsOn,
		}
		if !isStatic && socket != "" {
			phpSpec.FastCGIDialTimeout = "5s"
			phpSpec.FastCGIReadTimeout = "120s"
			phpSpec.FastCGIWriteTimeout = "120s"
		}

		out = append(out, proxy.Site{
			ID:           siteID,
			AppID:        site.AppID,
			DomainID:     d.ID,
			Hosts:        buildHosts(d.Domain, d.EnableWWW),
			TLS:          resolveTLSMode(d.Domain, d.EnableHTTPS, behindShield),
			BehindShield: behindShield,
			TrustedProxy: d.TrustedProxy,
			Headers:      phpBaseHeaders(),
			Kind:         kind,
			PHP:          phpSpec,
			Security:     sec,
		})
	}
	return out, nil
}

func sanitizeSecuritySpec(in proxy.SecuritySpec, rawJSON string) (*proxy.SecuritySpec, error) {
	s := in
	if isZeroSecuritySpec(s) && strings.TrimSpace(rawJSON) != "" {
		_ = json.Unmarshal([]byte(rawJSON), &s)
	}
	if len(s.BlockedIPs) > 0 {
		norm, err := proxy.NormalizeIPList(s.BlockedIPs)
		if err != nil {
			return nil, fmt.Errorf("blocked_ips: %w", err)
		}
		sort.Strings(norm)
		s.BlockedIPs = norm
	} else {
		s.BlockedIPs = nil
	}
	if len(s.AllowOnly) > 0 {
		norm, err := proxy.NormalizeIPList(s.AllowOnly)
		if err != nil {
			return nil, fmt.Errorf("allow_only: %w", err)
		}
		sort.Strings(norm)
		s.AllowOnly = norm
	} else {
		s.AllowOnly = nil
	}
	s.BasicAuthUser = strings.TrimSpace(s.BasicAuthUser)
	s.BasicAuthHash = strings.TrimSpace(s.BasicAuthHash)
	if !validAuthUser(s.BasicAuthUser) || !strings.HasPrefix(s.BasicAuthHash, "$2") {
		s.BasicAuthUser = ""
		s.BasicAuthHash = ""
	}
	if isZeroSecuritySpec(s) {
		return nil, nil
	}
	cp := s
	return &cp, nil
}

func isZeroSecuritySpec(s proxy.SecuritySpec) bool {
	return len(s.BlockedIPs) == 0 &&
		len(s.AllowOnly) == 0 &&
		s.BasicAuthUser == "" &&
		s.BasicAuthHash == "" &&
		!s.BlockXMLRPC &&
		!s.BlockUploadPHP &&
		!s.BlockScanners &&
		!s.HSTS &&
		!s.FrameProtection
}

func sortedDomains(domains []db.AppDomain) []db.AppDomain {
	out := append([]db.AppDomain(nil), domains...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			li := out[j-1]
			ri := out[j]
			var swap bool
			if li.ID > 0 && ri.ID > 0 {
				swap = ri.ID < li.ID
			} else {
				swap = strings.ToLower(ri.Domain) < strings.ToLower(li.Domain)
			}
			if !swap {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
