package edge

import (
	"reflect"
	"testing"
)

func TestAngieUserConfigIgnoresMapsAndResolvesUpstreams(t *testing.T) {
	conf := `
map "$http_sec_fetch_site|$http_sec_fetch_dest" $nd_u_xsite {
	default 0;
}
map $args $nd_u_q {
	default "";
}
upstream nd_u_desi_origin {
	zone nd_u_desi 64k;
	server 89.106.64.39:443;
	server 89.106.64.40:443 backup;
}
server {
	listen 443 ssl;
	server_name desi4u.net www.desi4u.net;
	location / {
		proxy_pass https://nd_u_desi_origin;
	}
	location /api/ {
		proxy_pass http://204.136.10.31:8989/api/;
	}
}
`
	sites := parseUserConfigSites(conf, "manual-sites.conf", EngineAngie)
	if len(sites) != 1 {
		t.Fatalf("want only the server block, got %d sites: %+v", len(sites), sites)
	}
	if !reflect.DeepEqual(sites[0].Hosts, []string{"desi4u.net", "www.desi4u.net"}) {
		t.Fatalf("hosts = %v", sites[0].Hosts)
	}
	want := []string{"https://89.106.64.39:443", "https://89.106.64.40:443", "http://204.136.10.31:8989/api/"}
	if !reflect.DeepEqual(sites[0].Upstreams, want) {
		t.Fatalf("upstreams = %v, want %v", sites[0].Upstreams, want)
	}
	if got := normalizeUserConfigHostToken("$args"); got != "" {
		t.Fatalf("a variable must not be a host, got %q", got)
	}
}
