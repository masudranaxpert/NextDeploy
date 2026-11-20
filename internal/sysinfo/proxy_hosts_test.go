package sysinfo

import "testing"

func TestParseCaddyHostCounters(t *testing.T) {
	m := `# HELP x
caddy_http_request_duration_seconds_count{code="200",handler="subroute",host="a.com",method="GET",server="srv0"} 90
caddy_http_request_duration_seconds_count{code="503",handler="subroute",host="a.com",method="GET",server="srv0"} 10
caddy_http_request_duration_seconds_count{code="200",handler="reverse_proxy",host="a.com",method="GET",server="srv0"} 85
caddy_http_request_duration_seconds_count{code="404",handler="file_server",host="B.com",method="GET",server="srv0"} 3
`
	got, ok := parseCaddyHostCounters(m)
	if !ok {
		t.Fatal("host labels not found")
	}
	if a := got["a.com"]; a.Requests != 100 || a.S2xx != 90 || a.S5xx != 10 {
		t.Fatalf("a.com = %+v", a)
	}
	if b := got["b.com"]; b.Requests != 3 || b.S4xx != 3 {
		t.Fatalf("b.com = %+v", b)
	}
	if _, ok := parseCaddyHostCounters(`caddy_http_request_duration_seconds_count{code="200",handler="subroute",server="srv0"} 5`); ok {
		t.Fatal("metrics without host labels must report not per-host")
	}
}
