package edge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanAndRemoveUserConfigCaddyAngieFerron(t *testing.T) {
	t.Run("caddy_unmanaged_caddyfile_and_user_dir", func(t *testing.T) {
		paths := NewPaths(t.TempDir())
		_ = os.MkdirAll(paths.UserCaddyDir, 0o755)
		_ = os.MkdirAll(filepath.Dir(paths.Caddyfile), 0o755)

		managed := RenderCaddyfileManagedRegion(paths, DefaultAdminAddr, "ops@example.com", paths.PanelsLink, paths.UserCaddyDir)
		caddyContent := "alpha.example.com {\n\treverse_proxy 127.0.0.1:4001\n}\n\n" +
			"beta.example.com, www.beta.example.com {\n\treverse_proxy https://10.0.0.8:8443\n\ttls internal\n}\n\n" +
			managed
		if err := os.WriteFile(paths.Caddyfile, []byte(caddyContent), 0o644); err != nil {
			t.Fatal(err)
		}
		customFile := filepath.Join(paths.UserCaddyDir, "solo.caddy")
		if err := os.WriteFile(customFile, []byte("solo.example.com {\n\treverse_proxy 127.0.0.1:4002\n}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		sites, err := ScanUserConfigSites(paths, EngineCaddy)
		if err != nil {
			t.Fatalf("ScanUserConfigSites: %v", err)
		}
		if len(sites) != 3 {
			t.Fatalf("expected 3 sites, got %d: %+v", len(sites), sites)
		}

		// Remove one host from multi-host block + remove solo.example.com (which empties solo.caddy).
		rem, err := RemoveHostsFromUserConfig(paths, EngineCaddy, []string{"beta.example.com", "solo.example.com"})
		if err != nil {
			t.Fatalf("RemoveHostsFromUserConfig: %v", err)
		}
		if len(rem.RemovedHosts) != 2 {
			t.Fatalf("expected 2 removed hosts, got %+v", rem.RemovedHosts)
		}
		if _, statErr := os.Stat(customFile); !os.IsNotExist(statErr) {
			t.Fatalf("expected empty user file %s to be deleted, err=%v", customFile, statErr)
		}

		updatedCaddy, _ := os.ReadFile(paths.Caddyfile)
		txt := string(updatedCaddy)
		if !strings.Contains(txt, ManagedStartMarker) || !strings.Contains(txt, ManagedEndMarker) {
			t.Fatalf("managed markers must stay intact:\n%s", txt)
		}
		if strings.Contains(txt, "beta.example.com,") || !strings.Contains(txt, "www.beta.example.com {") {
			t.Fatalf("expected beta.example.com stripped while keeping www.beta.example.com:\n%s", txt)
		}
		if !strings.Contains(txt, "alpha.example.com {") {
			t.Fatalf("unrelated alpha.example.com must remain intact:\n%s", txt)
		}

		// Verify rollback restores both Caddyfile and solo.caddy.
		if err := RestoreUserConfigBackups(rem.Backups); err != nil {
			t.Fatalf("RestoreUserConfigBackups: %v", err)
		}
		if _, err := os.Stat(customFile); err != nil {
			t.Fatalf("expected solo.caddy restored: %v", err)
		}
		restoredCaddy, _ := os.ReadFile(paths.Caddyfile)
		if string(restoredCaddy) != caddyContent {
			t.Fatalf("expected Caddyfile restored identically")
		}
	})

	t.Run("angie_user_conf", func(t *testing.T) {
		paths := NewPaths(t.TempDir())
		ep := paths.EnginePathsFor(EngineAngie)
		_ = os.MkdirAll(ep.UserDir, 0o755)
		confPath := filepath.Join(ep.UserDir, "custom.conf")
		angieConf := `server {
	listen 443 ssl;
	server_name one.example.com two.example.com;
	ssl_certificate /etc/ssl/one.crt;
	location / {
		proxy_pass http://127.0.0.1:5001;
	}
}

server {
	listen 80;
	server_name only.example.com;
	location / {
		proxy_pass http://127.0.0.1:5002;
	}
}
`
		if err := os.WriteFile(confPath, []byte(angieConf), 0o644); err != nil {
			t.Fatal(err)
		}

		sites, err := ScanUserConfigSites(paths, EngineAngie)
		if err != nil {
			t.Fatalf("ScanUserConfigSites(angie): %v", err)
		}
		if len(sites) != 2 {
			t.Fatalf("expected 2 angie sites, got %+v", sites)
		}
		if sites[0].TLS != ManualTLSManual || len(sites[0].Upstreams) != 1 || sites[0].Upstreams[0] != "http://127.0.0.1:5001" {
			t.Fatalf("unexpected first site parse: %+v", sites[0])
		}

		rem, err := RemoveHostsFromUserConfig(paths, EngineAngie, []string{"one.example.com", "only.example.com"})
		if err != nil {
			t.Fatalf("RemoveHostsFromUserConfig(angie): %v", err)
		}
		if len(rem.RemovedHosts) != 2 {
			t.Fatalf("expected 2 removed angie hosts, got %+v", rem.RemovedHosts)
		}
		after, _ := os.ReadFile(confPath)
		if strings.Contains(string(after), "only.example.com") || strings.Contains(string(after), "one.example.com") {
			t.Fatalf("expected removed hosts gone:\n%s", string(after))
		}
		if !strings.Contains(string(after), "server_name two.example.com;") {
			t.Fatalf("expected sibling two.example.com preserved:\n%s", string(after))
		}
	})

	t.Run("ferron_user_conf", func(t *testing.T) {
		paths := NewPaths(t.TempDir())
		ep := paths.EnginePathsFor(EngineFerron)
		_ = os.MkdirAll(ep.UserDir, 0o755)
		confPath := filepath.Join(ep.UserDir, "sites.conf")
		ferronConf := `http f1.example.com, f2.example.com {
    proxy "http://127.0.0.1:6001"
}
f3.example.com {
    proxy "http://127.0.0.1:6002"
}
`
		if err := os.WriteFile(confPath, []byte(ferronConf), 0o644); err != nil {
			t.Fatal(err)
		}
		sites, err := ScanUserConfigSites(paths, EngineFerron)
		if err != nil {
			t.Fatalf("ScanUserConfigSites(ferron): %v", err)
		}
		if len(sites) != 2 {
			t.Fatalf("expected 2 ferron sites, got %+v", sites)
		}
		if _, err := RemoveHostsFromUserConfig(paths, EngineFerron, []string{"f1.example.com", "f3.example.com"}); err != nil {
			t.Fatalf("RemoveHostsFromUserConfig(ferron): %v", err)
		}
		after, _ := os.ReadFile(confPath)
		if !strings.Contains(string(after), "http f2.example.com {") || strings.Contains(string(after), "f3.example.com") {
			t.Fatalf("unexpected ferron result:\n%s", string(after))
		}
	})
}
