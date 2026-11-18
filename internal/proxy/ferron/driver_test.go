package ferron

import (
	"slices"
	"strings"
	"testing"

	"panel/internal/proxy"
)

func TestDriverIdentityAndRegistration(t *testing.T) {
	d := New()
	if d.Name() != "ferron" {
		t.Fatalf("Name() = %q, want ferron", d.Name())
	}
	if d.ContainerName() != "nd-proxy" {
		t.Fatalf("ContainerName() = %q, want nd-proxy", d.ContainerName())
	}
	if FerronImage != "ferronserver/ferron:3.0.0-rc.11-alpine" {
		t.Fatalf("FerronImage = %q, want pinned 3.0.0-rc.11-alpine", FerronImage)
	}
	if got := d.AppLabels([]proxy.Site{{ID: "app:1:dom:1", Hosts: []string{"a.example.com"}}}, proxy.UpstreamRef{Name: "u", Port: 3000}); got != nil {
		t.Fatalf("expected AppLabels to return nil, got %v", got)
	}

	globalDrv, err := proxy.Get("ferron")
	if err != nil || globalDrv.Name() != "ferron" {
		t.Fatalf("expected global registry to contain ferron driver via init(): drv=%v err=%v", globalDrv, err)
	}

	reg := proxy.NewRegistry()
	if err := RegisterInto(reg); err != nil {
		t.Fatalf("RegisterInto(reg): %v", err)
	}
	if got, err := reg.Get("ferron"); err != nil || got.Name() != "ferron" {
		t.Fatalf("reg.Get(ferron) = (%v, %v)", got, err)
	}

	custom := New(WithContainerName("custom-ferron"))
	if custom.ContainerName() != "custom-ferron" {
		t.Fatalf("WithContainerName failed, got %q", custom.ContainerName())
	}
}

func TestDriverCapabilitiesMatchPreflight(t *testing.T) {
	caps := New().Capabilities()
	if !caps.Experimental {
		t.Fatalf("expected Experimental=true")
	}
	if !caps.AutoTLS || !caps.InternalTLS || !caps.OnDemandTLS || !caps.PHPFastCGI ||
		!caps.BasicAuthBcrypt || !caps.RegexRouting || !caps.AdminAPI || !caps.LongStreamsOnReload {
		t.Fatalf("unexpected false core capability in %+v", caps)
	}
	if caps.RequireTargetFile || caps.TrustedStrict || caps.DockerUpstreamDiscovery ||
		caps.H2IdleTimeout || caps.WriteTimeout || caps.ReadHeaderTimeout ||
		caps.ReadBodyTimeout || caps.GracePeriod || caps.MaxHeaderSizeHTTP1 {
		t.Fatalf("unexpected true unsupported capability in %+v", caps)
	}

	st := &proxy.State{
		Global: proxy.GlobalOptions{Profile: "default", RealIP: "cloudflare", DirectCloudflare: true},
		Sites: []proxy.Site{{
			ID:           "app:1:dom:1",
			Hosts:        []string{"app.example.com"},
			TLS:          proxy.TLSAuto,
			TrustedProxy: true,
			Kind:         proxy.SiteKindAppProxy,
			Upstream:     &proxy.UpstreamRef{Name: "nd_app_web_3000", Container: "app-web", Port: 3000},
		}},
	}
	diff := proxy.EvaluateCapabilities(st, caps)
	if !diff.CanSwitch || !diff.RequiresExperimentalAck {
		t.Fatalf("expected CanSwitch=true RequiresExperimentalAck=true, got %+v", diff)
	}
}

func TestStackService_ConfigMountsEnvCommandAndPorts(t *testing.T) {
	d := New()
	svc, vols, err := d.StackService(proxy.StackOptions{
		Gateways: []proxy.GatewayPort{
			{Port: 8080, ContainerPort: 8080, Protocol: "tcp"},
			{Port: 9000, ContainerPort: 9000, BindIP: "127.0.0.1", Protocol: "tcp"},
			{Port: 5353, ContainerPort: 5353, Protocol: "udp"},
			{Port: 5432, ContainerPort: 5432, Protocol: "tcp", Direct: true},
		},
		SharedMounts: []proxy.SharedMount{
			{VolumeName: "nd_shared_media", Target: "/srv/shared/media", ReadOnly: true},
			{VolumeName: "nd_shared_uploads", Target: "/srv/shared/uploads", ReadOnly: false},
		},
	})
	if err != nil {
		t.Fatalf("StackService: %v", err)
	}

	if svc["image"] != FerronImage {
		t.Fatalf("image = %v, want %s", svc["image"], FerronImage)
	}
	if svc["container_name"] != "nd-proxy" {
		t.Fatalf("container_name = %v, want nd-proxy", svc["container_name"])
	}
	if svc["user"] != "0:0" {
		t.Fatalf("user = %v, want 0:0", svc["user"])
	}

	cmd, _ := svc["command"].([]string)
	wantCmd := []string{"/usr/local/bin/ferron", "run", "-c", "/etc/ferron/nd/sites/ferron.conf", "--config-adapter", "ferronconf"}
	if !slices.Equal(cmd, wantCmd) {
		t.Fatalf("command = %v, want %v", cmd, wantCmd)
	}
	if strings.Contains(strings.Join(cmd, " "), "watch") {
		t.Fatalf("command must not use broken watch=1 flag: %v", cmd)
	}

	env, _ := svc["environment"].(map[string]any)
	tokVal, ok := env[DefaultAdminTokenEnv].(string)
	if !ok || strings.TrimSpace(tokVal) == "" {
		t.Fatalf("expected non-empty %s env value, got %#v", DefaultAdminTokenEnv, env)
	}

	ports, _ := svc["ports"].([]string)
	wantPorts := []string{"80:80", "443:443/tcp", "443:443/udp", "127.0.0.1:9000:9000", "8080:8080"}
	if !slices.Equal(ports, wantPorts) {
		t.Fatalf("ports = %v, want %v (UDP and Direct gateways must be skipped)", ports, wantPorts)
	}

	mounts, _ := svc["volumes"].([]string)
	for _, want := range []string{
		"/data/proxy/ferron:/etc/ferron/nd:ro",
		"/data/proxy/pki/sites:/etc/nd-pki/sites:ro",
		"/data/proxy/pki/imported:/etc/nd-pki/imported:ro",
		"ferron_acme:/var/cache/ferron-acme",
		"/data/workspaces:/data/workspaces:ro",
		"/data/workspaces/.nd-php:/data/workspaces/.nd-php",
		"nd_shared_media:/srv/shared/media:ro",
		"nd_shared_uploads:/srv/shared/uploads",
	} {
		if !slices.Contains(mounts, want) {
			t.Errorf("missing expected volume mount %q in %v", want, mounts)
		}
	}

	if _, ok := vols["ferron_acme"]; !ok {
		t.Fatalf("expected top-level ferron_acme volume, got %v", vols)
	}
	if m, _ := vols["nd_shared_media"].(map[string]any); m["external"] != true {
		t.Fatalf("expected external shared volume nd_shared_media, got %v", vols["nd_shared_media"])
	}
}
