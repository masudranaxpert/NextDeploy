package state

import (
	"fmt"
	"sort"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
)

// Build constructs the deterministic, engine-agnostic *proxy.State IR from
// pre-loaded panel settings, global options, shield nodes, apps, domains,
// PHP/static sites, custom proxy routes, and allocated host ports.
func Build(in BuildInput) (*proxy.State, error) {
	global, err := buildGlobalOptions(in)
	if err != nil {
		return nil, err
	}

	sharedMounts, err := buildSharedMounts(in.SharedMountPrefix, in.SharedVolumeNamesRaw, in.SharedVolumeNames)
	if err != nil {
		return nil, err
	}

	appMap := make(map[string]db.App, len(in.Apps))
	for _, a := range in.Apps {
		appMap[a.ID] = a
	}

	shieldMap := make(map[int64]db.ShieldNode, len(in.ShieldNodes))
	for _, n := range in.ShieldNodes {
		shieldMap[n.ID] = n
	}

	phpAppIDs := make(map[string]bool, len(in.PHPSites))
	for _, p := range in.PHPSites {
		if p.Site.AppID != "" {
			phpAppIDs[p.Site.AppID] = true
		}
	}

	domainsByApp := make(map[string][]db.AppDomain)
	for _, d := range in.AppDomains {
		domainsByApp[d.AppID] = append(domainsByApp[d.AppID], d)
	}

	var sites []proxy.Site
	sites = append(sites, buildAppSites(in, phpAppIDs, appMap, shieldMap)...)

	for _, phpIn := range in.PHPSites {
		phpSites, err := buildPHPSites(phpIn, domainsByApp[phpIn.Site.AppID])
		if err != nil {
			return nil, err
		}
		sites = append(sites, phpSites...)
	}

	for _, r := range in.ProxyRoutes {
		site, include, err := buildProxyRouteSite(r)
		if err != nil {
			return nil, err
		}
		if include {
			sites = append(sites, site)
		}
	}

	gateways := buildGateways(in, appMap)

	sortSitesDeterministic(sites)
	sortGatewaysDeterministic(gateways)
	sortSharedMountsDeterministic(sharedMounts)

	st := &proxy.State{
		Global:       global,
		ACMEEmail:    strings.TrimSpace(in.ACMEEmail),
		Panel:        buildPanelSite(in, global),
		Sites:        sites,
		Gateways:     gateways,
		SharedMounts: sharedMounts,
	}
	st.Generation = st.ComputeGeneration()
	return st, nil
}

func buildGlobalOptions(in BuildInput) (proxy.GlobalOptions, error) {
	g := in.Global
	if isZeroUserGlobalOptions(g) {
		g = proxy.OptionProfiles["default"]
	}

	// Recompute ShieldProxies deterministically from ShieldNodes.
	shieldSeen := make(map[string]struct{}, len(in.ShieldNodes))
	var shieldIPs []string
	for _, n := range in.ShieldNodes {
		ip := strings.TrimSpace(n.IP)
		if ip == "" {
			continue
		}
		if _, dup := shieldSeen[ip]; dup {
			continue
		}
		shieldSeen[ip] = struct{}{}
		shieldIPs = append(shieldIPs, ip)
	}
	sort.Strings(shieldIPs)
	g.ShieldProxies = shieldIPs

	// DirectCloudflare is true when any direct (non-shielded) app domain or
	// enabled proxy route marks TrustedProxy = true.
	directCF := false
	for _, d := range in.AppDomains {
		if (d.ShieldNodeID == nil || *d.ShieldNodeID <= 0) && d.TrustedProxy {
			directCF = true
			break
		}
	}
	if !directCF {
		for _, r := range in.ProxyRoutes {
			if r.Enabled && (r.ShieldNodeID == nil || *r.ShieldNodeID <= 0) && r.TrustedProxy {
				directCF = true
				break
			}
		}
	}
	g.DirectCloudflare = directCF

	norm, err := g.Normalize()
	if err != nil {
		return proxy.GlobalOptions{}, fmt.Errorf("global options: %w", err)
	}
	sort.Strings(norm.TrustedProxies)
	sort.Strings(norm.ShieldProxies)
	return norm, nil
}

func isZeroUserGlobalOptions(g proxy.GlobalOptions) bool {
	return strings.TrimSpace(g.Profile) == "" &&
		strings.TrimSpace(g.ReadHeader) == "" &&
		strings.TrimSpace(g.ReadBodyIdle) == "" &&
		strings.TrimSpace(g.Idle) == "" &&
		strings.TrimSpace(g.GracePeriod) == "" &&
		strings.TrimSpace(g.MaxHeaderSize) == "" &&
		strings.TrimSpace(g.RealIP) == "" &&
		len(g.TrustedProxies) == 0 &&
		!g.TrustedStrict
}

func sortSitesDeterministic(sites []proxy.Site) {
	sort.Slice(sites, func(i, j int) bool {
		a, b := sites[i], sites[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		ha, hb := strings.Join(a.Hosts, ","), strings.Join(b.Hosts, ",")
		if ha != hb {
			return ha < hb
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Slot < b.Slot
	})
}

func sortGatewaysDeterministic(gateways []proxy.GatewayPort) {
	sort.Slice(gateways, func(i, j int) bool {
		a, b := gateways[i], gateways[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.BindIP != b.BindIP {
			return a.BindIP < b.BindIP
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		if a.AppID != b.AppID {
			return a.AppID < b.AppID
		}
		return a.ContainerPort < b.ContainerPort
	})
}

func sortSharedMountsDeterministic(mounts []proxy.SharedMount) {
	sort.Slice(mounts, func(i, j int) bool {
		a, b := mounts[i], mounts[j]
		if a.VolumeName != b.VolumeName {
			return a.VolumeName < b.VolumeName
		}
		return a.Target < b.Target
	})
}
