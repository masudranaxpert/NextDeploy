package caddy

import (
	"testing"

	"panel/internal/db"
	"panel/internal/proxy"

	"gopkg.in/yaml.v3"
)

func TestGenerateMergedComposeWithLabeler(t *testing.T) {
	base := []byte(`services:
  web:
    image: node:22-alpine
    ports:
      - "12002:3000"
      - "54325:5432"
`)
	doms := []db.AppDomain{
		{ID: 7, AppID: "my-app", Domain: "app.example.com", Service: "web", Port: 3000, EnableHTTPS: true},
	}

	t.Run("nil labeler keeps caddy labels", func(t *testing.T) {
		out, err := GenerateMergedComposeWithLabeler(base, "my-app-blue", "my-app", doms, "PORT=3000\n", "", nil)
		if err != nil {
			t.Fatalf("GenerateMergedComposeWithLabeler: %v", err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(out, &doc); err != nil {
			t.Fatal(err)
		}
		services := doc["services"].(map[string]any)
		web := services["web"].(map[string]any)
		labels := web["labels"].(map[string]any)
		if labels["caddy"] != "app.example.com" {
			t.Fatalf("expected caddy label app.example.com, got %v", labels)
		}
	})

	t.Run("angie labeler emits upstream port and network labels", func(t *testing.T) {
		angieLabeler := func(sites []proxy.Site, up proxy.UpstreamRef) map[string]string {
			if len(sites) == 0 || up.Port <= 0 {
				return nil
			}
			return map[string]string{
				"angie.http.upstreams." + up.Name + ".port": "3000",
				"angie.network":                             "NextDeploy",
			}
		}
		out, bindings, err := GenerateSlotMergedComposeWithEnvAndLabeler(
			base, "my-app-blue", "blue", "my-app", doms, "", "", "", nil, angieLabeler, false,
		)
		if err != nil {
			t.Fatalf("GenerateSlotMergedComposeWithEnvAndLabeler: %v", err)
		}
		if len(bindings) == 0 {
			t.Fatal("expected parsed HostPortBindings")
		}
		var doc map[string]any
		if err := yaml.Unmarshal(out, &doc); err != nil {
			t.Fatal(err)
		}
		services := doc["services"].(map[string]any)
		web := services["web"].(map[string]any)
		labels := web["labels"].(map[string]any)
		wantKey := "angie.http.upstreams." + proxy.UpstreamName("my-app", "web", 3000, "blue") + ".port"
		if labels[wantKey] != "3000" {
			t.Fatalf("missing %s in labels: %v", wantKey, labels)
		}
		if labels["angie.network"] != "NextDeploy" {
			t.Fatalf("missing angie.network=NextDeploy in labels: %v", labels)
		}
		if _, hasCaddy := labels["caddy"]; hasCaddy {
			t.Fatalf("unexpected caddy label on angie compose: %v", labels)
		}
	})

	t.Run("ferron nil labeler attaches network without labels", func(t *testing.T) {
		ferronLabeler := func(_ []proxy.Site, _ proxy.UpstreamRef) map[string]string {
			return nil
		}
		out, _, err := GenerateSlotMergedComposeWithEnvAndLabeler(
			base, "my-app-blue", "blue", "my-app", doms, "", "", "", nil, ferronLabeler, false,
		)
		if err != nil {
			t.Fatalf("GenerateSlotMergedComposeWithEnvAndLabeler: %v", err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(out, &doc); err != nil {
			t.Fatal(err)
		}
		services := doc["services"].(map[string]any)
		web := services["web"].(map[string]any)
		if lbl, has := web["labels"]; has && lbl != nil {
			t.Fatalf("expected no labels for Ferron compose, got %v", lbl)
		}
		nets, _ := web["networks"].([]any)
		foundNet := false
		for _, n := range nets {
			if n == "nextdeploy" {
				foundNet = true
			}
		}
		if !foundNet {
			t.Fatalf("expected nextdeploy network attached for Ferron container DNS discovery: %v", web["networks"])
		}
	})
}
