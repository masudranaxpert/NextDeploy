package caddy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateRootStackComposeForEngine(t *testing.T) {
	base := `services:
  caddy:
    image: lucaslorentz/caddy-docker-proxy:ci-alpine
    volumes:
      - nd_shared_deadbeef:/mnt/shared/x
  nd-proxy:
    image: old
    environment:
      ND_FERRON_ADMIN_TOKEN: secret-token
  panel:
    image: nextdeploy-panel
    depends_on:
      - caddy
    labels:
      caddy: panel.example.com
      com.example.keep: "yes"
volumes:
  caddy_data: {}
  nd_shared_deadbeef:
    external: true
`
	svc := map[string]any{
		"image":       "ferronserver/ferron:3",
		"environment": map[string]any{"ND_FERRON_ADMIN_TOKEN": ""},
	}
	out, err := GenerateRootStackComposeForEngine([]byte(base), "nd-proxy", svc, map[string]any{"ferron_acme": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
		Volumes  map[string]any            `yaml:"volumes"`
		Networks map[string]any            `yaml:"networks"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Services["caddy"]; ok {
		t.Fatal("old caddy service must be removed")
	}
	np := doc.Services["nd-proxy"]
	if np["image"] != "ferronserver/ferron:3" {
		t.Fatalf("image = %v", np["image"])
	}
	if !strings.Contains(string(out), "secret-token") {
		t.Fatalf("previous non-empty env value must be preserved:\n%s", out)
	}
	panel := doc.Services["panel"]
	if _, ok := panel["depends_on"]; ok {
		t.Fatalf("panel depends_on caddy must be dropped:\n%s", out)
	}
	if strings.Contains(string(out), "panel.example.com") {
		t.Fatalf("panel caddy labels must be stripped:\n%s", out)
	}
	if !strings.Contains(string(out), "com.example.keep") {
		t.Fatalf("non-caddy panel labels must be kept:\n%s", out)
	}
	if _, ok := doc.Volumes["ferron_acme"]; !ok {
		t.Fatalf("engine volumes must be merged:\n%s", out)
	}
	if _, ok := doc.Volumes["nd_shared_deadbeef"]; ok {
		t.Fatalf("stale nd_shared volumes must be stripped (driver re-adds current ones):\n%s", out)
	}
	if _, ok := doc.Networks[PanelNetworkKey]; !ok {
		t.Fatalf("NextDeploy network must exist:\n%s", out)
	}
}

func TestGenerateRootStackComposeForEngineRequiresPanel(t *testing.T) {
	if _, err := GenerateRootStackComposeForEngine([]byte("services:\n  caddy: {}\n"), "nd-proxy", map[string]any{"image": "x"}, nil); err == nil {
		t.Fatal("expected error without panel service")
	}
}
