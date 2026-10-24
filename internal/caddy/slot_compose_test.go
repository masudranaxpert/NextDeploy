package caddy

import (
	"strings"
	"testing"

	"panel/internal/db"

	"gopkg.in/yaml.v3"
)

func TestExtractAndStripHostPorts(t *testing.T) {
	inputYAML := `
services:
  web:
    image: nginx:alpine
    ports:
      - "4013:3000"
      - "127.0.0.1:8080:80"
      - "${APP_PORT:-3001}:3001"
      - "${BIND_IP:-127.0.0.1}:${WEB_PORT:-8081}:8081"
      - "5000:5000/udp"
      - "9000"
  worker:
    image: redis:alpine
    ports:
      - target: 6379
        published: 6379
        protocol: tcp
`
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(inputYAML), &doc); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	services := doc["services"].(map[string]interface{})
	bindings, err := ExtractAndStripHostPorts(services)
	if err != nil {
		t.Fatalf("ExtractAndStripHostPorts failed: %v", err)
	}

	// Should extract 6 host port bindings (4013, 8080, 3001, 8081, 5000, 6379), ignoring standalone 9000
	if len(bindings) != 6 {
		t.Fatalf("expected 6 host port bindings, got %d: %+v", len(bindings), bindings)
	}

	// Verify 4013 binding
	var b4013 *HostPortBinding
	for _, b := range bindings {
		if b.HostPort == 4013 {
			b4013 = &b
			break
		}
	}
	if b4013 == nil || b4013.ContainerPort != 3000 || b4013.BindIP != "0.0.0.0" || b4013.Service != "web" {
		t.Fatalf("unexpected b4013: %+v", b4013)
	}

	// Verify 127.0.0.1:8080 binding
	var b8080 *HostPortBinding
	for _, b := range bindings {
		if b.HostPort == 8080 {
			b8080 = &b
			break
		}
	}
	if b8080 == nil || b8080.ContainerPort != 80 || b8080.BindIP != "127.0.0.1" {
		t.Fatalf("unexpected b8080: %+v", b8080)
	}

	// Verify web service ports has only "9000" left
	webSvc := services["web"].(map[string]interface{})
	ports, ok := webSvc["ports"].([]interface{})
	if !ok || len(ports) != 1 || ports[0] != "9000" {
		t.Fatalf("expected web ports to only have ['9000'], got: %+v", webSvc["ports"])
	}

	// Verify expose has 3000, 80, 5000
	expose, ok := webSvc["expose"].([]interface{})
	if !ok || len(expose) < 3 {
		t.Fatalf("expected web expose to have container ports, got: %+v", webSvc["expose"])
	}
}

func TestGenerateSlotMergedCompose(t *testing.T) {
	inputYAML := `
services:
  web:
    image: mynodeapp:v1
    container_name: web_service
    ports:
      - "4013:3000"
`
	domains := []db.AppDomain{
		{
			Domain:      "myapp.example.com",
			Service:     "web",
			Port:        3000,
			EnableHTTPS: true,
		},
	}

	merged, bindings, err := GenerateSlotMergedCompose([]byte(inputYAML), "proj-green", "green", domains, "", "", nil)
	if err != nil {
		t.Fatalf("GenerateSlotMergedCompose failed: %v", err)
	}

	if len(bindings) != 1 || bindings[0].HostPort != 4013 {
		t.Fatalf("expected 1 binding for 4013, got: %+v", bindings)
	}

	content := string(merged)

	// Check container_name normalization
	if !strings.Contains(content, "proj-green_web_service") {
		t.Errorf("expected container_name to be prefixed with proj-green, got:\n%s", content)
	}

	// Check that 4013 is stripped from ports
	if strings.Contains(content, "4013:3000") {
		t.Errorf("expected 4013:3000 to be stripped from ports, but it is present in:\n%s", content)
	}

	// Check expose: 3000
	if !strings.Contains(content, "3000") {
		t.Errorf("expected internal port 3000 to be preserved")
	}

	// Check Caddy labels for domain and port 4013
	if !strings.Contains(content, "caddy: myapp.example.com") {
		t.Errorf("missing caddy domain label in:\n%s", content)
	}
	if !strings.Contains(content, ":4013") {
		t.Errorf("missing caddy host port gateway label in:\n%s", content)
	}
	if !strings.Contains(content, "lb_try_duration: 5s") {
		t.Errorf("missing lb_try_duration: 5s in:\n%s", content)
	}
	if !strings.Contains(content, "fail_duration: 5s") {
		t.Errorf("missing fail_duration: 5s in:\n%s", content)
	}
	if strings.Contains(content, "green.myapp.example.com") {
		t.Errorf("production compose must not carry preview labels:\n%s", content)
	}

	// Verify previewOnly=true mode excludes primary domain and host port :4013 so pinned production keeps exclusive routing
	previewMerged, _, err := GenerateSlotMergedCompose([]byte(inputYAML), "proj-green", "green", domains, "", "", &db.AppDomain{Domain: "green.myapp.example.com", Service: "web", Port: 3000, EnableHTTPS: true}, true)
	if err != nil {
		t.Fatalf("GenerateSlotMergedCompose(previewOnly=true) failed: %v", err)
	}
	pvContent := string(previewMerged)
	if strings.Contains(pvContent, "caddy: myapp.example.com") {
		t.Errorf("expected preview-only compose NOT to include primary domain label, got:\n%s", pvContent)
	}
	if strings.Contains(pvContent, ":4013") {
		t.Errorf("expected preview-only compose NOT to include host port :4013 gateway label, got:\n%s", pvContent)
	}
	if !strings.Contains(pvContent, "caddy: green.myapp.example.com") {
		t.Errorf("expected preview-only compose to include preview domain label as primary caddy label, got:\n%s", pvContent)
	}
}

func TestSslipAndNipWildcardDomains(t *testing.T) {
	pubDom := FormatWildcardDomain("commerce-api", "203.0.113.42", "sslip.io")
	if pubDom != "commerce-api-203-0-113-42.sslip.io" {
		t.Fatalf("unexpected public sslip domain: %s", pubDom)
	}
	if ShouldUseInternalTLS(pubDom) {
		t.Errorf("expected public sslip.io domain %q to use public Let's Encrypt TLS, got internal TLS", pubDom)
	}

	nipDom := FormatWildcardDomain("commerce-api", "203.0.113.42", "nip.io")
	if nipDom != "commerce-api-203-0-113-42.nip.io" {
		t.Fatalf("unexpected public nip domain: %s", nipDom)
	}
	if ShouldUseInternalTLS(nipDom) {
		t.Errorf("expected public nip.io domain %q to use public Let's Encrypt TLS", nipDom)
	}

	privDom := FormatWildcardDomain("commerce-api", "127.0.0.1", "sslip.io")
	if !ShouldUseInternalTLS(privDom) {
		t.Errorf("expected loopback sslip.io domain %q to use internal TLS", privDom)
	}
	lanDom := "myapp.192.168.1.50.nip.io"
	if !ShouldUseInternalTLS(lanDom) {
		t.Errorf("expected LAN nip.io domain %q to use internal TLS", lanDom)
	}

	slotPv := SlotPreviewDomain(pubDom, "blue")
	if slotPv != "blue-commerce-api-203-0-113-42.sslip.io" {
		t.Errorf("unexpected slot preview domain: %s", slotPv)
	}

	// Numeric-ending slug must not collide with IPv4 octets in sslip.io / nip.io
	numSlugDom := FormatWildcardDomain("api-2", "45.250.230.88", "sslip.io")
	if numSlugDom != "api-2-app-45-250-230-88.sslip.io" {
		t.Fatalf("expected numeric-ending slug to append -app before IP, got: %s", numSlugDom)
	}
	if ip := ExtractWildcardDomainIP(numSlugDom); ip == nil || ip.String() != "45.250.230.88" {
		t.Errorf("expected extracted IP 45.250.230.88, got: %v", ip)
	}
}
