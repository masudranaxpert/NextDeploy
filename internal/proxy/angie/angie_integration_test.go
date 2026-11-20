//go:build integration

package angie

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/proxy"
	"panel/internal/proxy/pki"
)

func writeDummyCertPair(t *testing.T, certPath, keyPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		DNSNames:     []string{"test.local"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationAngieValidateAllGoldenFixtures(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not available")
	}

	tmpBase := t.TempDir()
	pkiSites := filepath.Join(tmpBase, "pki", "sites")
	pkiImported := filepath.Join(tmpBase, "pki", "imported")
	if err := os.MkdirAll(pkiSites, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkiImported, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDummyCertPair(t, filepath.Join(pkiImported, "ext.example.com.crt"), filepath.Join(pkiImported, "ext.example.com.key"))

	d := New(
		WithPKIDirs(pkiSites, pkiImported),
		WithImportedCerts(map[string]ManualCert{
			"ext.example.com": {
				CertPath: "/etc/nd-pki/imported/ext.example.com.crt",
				KeyPath:  "/etc/nd-pki/imported/ext.example.com.key",
			},
		}),
	)

	fixtures := []string{
		"01_app_simple_www_auto_tls.golden.json",
		"02_app_path_routes.golden.json",
		"03_internal_tls_variants.golden.json",
		"04_trusted_proxy_cloudflare.golden.json",
		"05_blue_green_slot_preview.golden.json",
		"06_php_wordpress_security_cache.golden.json",
		"07_php_laravel_routing_suspended.golden.json",
		"08_php_dummy_domain_exclusion.golden.json",
		"09_static_site.golden.json",
		"10_proxy_routes_external_redirect.golden.json",
		"11_gateway_http_vs_direct_l4.golden.json",
		"12_panel_shared_mounts_deterministic.golden.json",
	}

	for _, f := range fixtures {
		t.Run(strings.TrimSuffix(f, ".golden.json"), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "state", "testdata", f))
			if err != nil {
				t.Fatal(err)
			}
			var st proxy.State
			if err := json.Unmarshal(raw, &st); err != nil {
				t.Fatal(err)
			}

			for _, site := range st.Sites {
				if effectiveTLSMode(site) == proxy.TLSInternal {
					stem, err := pki.SafeSiteStem(site.ID)
					if err != nil {
						t.Fatal(err)
					}
					writeDummyCertPair(t, filepath.Join(pkiSites, stem+".crt"), filepath.Join(pkiSites, stem+".key"))
				}
			}

			files, err := d.Render(&st)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			stageDir := filepath.Join(tmpBase, "stage-"+strings.TrimSuffix(f, ".golden.json"))
			for rel, content := range files {
				dst := filepath.Join(stageDir, filepath.FromSlash(rel))
				if err := proxy.WriteFileAtomic(dst, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			if err := d.Validate(t.Context(), stageDir); err != nil {
				t.Fatalf("angie -t failed for %s: %v", f, err)
			}
		})
	}

	t.Run("strict404_and_https_external_verify", func(t *testing.T) {
		st := &proxy.State{
			Global: proxy.GlobalOptions{RealIP: "cloudflare"},
			Sites: []proxy.Site{
				{
					ID:    "php:strict",
					Hosts: []string{"strict.example.com"},
					TLS:   proxy.TLSAuto,
					Kind:  proxy.SiteKindPHP,
					PHP: &proxy.PHPSpec{
						DocRoot:      "/data/workspaces/strict/htdocs",
						Socket:       "/data/workspaces/.nd-php/u1-v83/run/strict.sock",
						FallbackMode: proxy.FallbackStrict404,
						Compression:  []string{"zstd", "gzip"},
					},
				},
				{
					ID:           "route:strict_ext",
					Hosts:        []string{"origin.example.com"},
					TLS:          proxy.TLSAuto,
					TrustedProxy: true,
					Kind:         proxy.SiteKindExternalProxy,
					Upstream: &proxy.UpstreamRef{
						Name:       "nd_route_strict",
						Scheme:     "https",
						SkipVerify: false,
						External:   "https://upstream.example.com:8443",
					},
				},
			},
		}
		files, err := d.Render(st)
		if err != nil {
			t.Fatal(err)
		}
		stageDir := filepath.Join(tmpBase, "stage-extra")
		for rel, content := range files {
			dst := filepath.Join(stageDir, filepath.FromSlash(rel))
			if err := proxy.WriteFileAtomic(dst, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := d.Validate(t.Context(), stageDir); err != nil {
			t.Fatalf("angie -t failed for extra cases: %v", err)
		}
	})
}

func TestIntegrationAngieLiveStatusPrometheusAndSymlinkReload(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not available")
	}

	tmpBase := t.TempDir()
	stage, err := proxy.NewStageStore(proxy.StageConfig{BaseDir: tmpBase, KeepGenerations: 3})
	if err != nil {
		t.Fatal(err)
	}
	d := New(WithDataDir(tmpBase), WithStageStore(stage))

	filesV1, err := d.Render(&proxy.State{
		Sites: []proxy.Site{
			{
				ID:       "app:live:dom:1",
				Hosts:    []string{"v1.local"},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 302, Target: "https://v1.example.com"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, target1, err := stage.Stage("v1", filesV1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Activate(target1); err != nil {
		t.Fatal(err)
	}

	filesV2, err := d.Render(&proxy.State{
		Sites: []proxy.Site{
			{
				ID:       "app:live:dom:1",
				Hosts:    []string{"v2.local"},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 302, Target: "https://v2.example.com"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Check whether the host filesystem supports real POSIX symlinks (Windows without
	// Developer Mode falls back to writing pointer text files).
	_, readlinkErr := os.Readlink(stage.LinkPath())
	hostSymlinksSupported := readlinkErr == nil

	absBase, err := filepath.Abs(tmpBase)
	if err != nil {
		t.Fatal(err)
	}
	name := "nd-angie-it-" + strings.ReplaceAll(time.Now().Format("150405.000"), ".", "")
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})

	if hostSymlinksSupported {
		startCmd := exec.Command("docker", "run", "-d", "--name", name,
			"-v", absBase+":"+containerConfigRoot+":ro",
			AngieImage,
			"angie", "-c", containerLiveConfig, "-g", "daemon off;",
		)
		if out, err := startCmd.CombinedOutput(); err != nil {
			t.Fatalf("start angie container: %v\n%s", err, string(out))
		}
	} else {
		_, target2, err := stage.Stage("v2", filesV2)
		if err != nil {
			t.Fatal(err)
		}
		startCmd := exec.Command("docker", "run", "-d", "--name", name,
			"-v", absBase+":/mnt/host-angie:ro",
			AngieImage,
			"sh", "-c",
			"mkdir -p "+containerConfigRoot+" && cp -r /mnt/host-angie/"+target1+" /mnt/host-angie/"+target2+" "+containerConfigRoot+"/ && ln -sfn "+target1+" "+containerConfigRoot+"/sites && exec angie -c "+containerLiveConfig+" -g 'daemon off;'",
		)
		if out, err := startCmd.CombinedOutput(); err != nil {
			t.Fatalf("start angie container: %v\n%s", err, string(out))
		}
	}

	liveDrv := New(WithContainerName(name), WithDataDir(tmpBase), WithStageStore(stage))
	var st1 proxy.Status
	for i := 0; i < 25; i++ {
		st1, err = liveDrv.Status(t.Context())
		if err == nil && st1.Running && st1.Version != "" {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err != nil || !st1.Running || st1.Generation != "1" {
		t.Fatalf("unexpected initial live Status: %+v err=%v", st1, err)
	}

	promOut, err := exec.Command("docker", "exec", name, "wget", "-qO-", "http://127.0.0.1:2019/p8s").CombinedOutput()
	if err != nil || !strings.Contains(string(promOut), "angie_connections_active") {
		t.Fatalf("prometheus /p8s check failed: err=%v out=%s", err, string(promOut))
	}

	if hostSymlinksSupported {
		res, err := liveDrv.Apply(t.Context(), filesV2)
		if err != nil || !res.Reloaded {
			t.Fatalf("live Apply v2 failed: res=%+v err=%v", res, err)
		}
	} else {
		if out, err := exec.Command("docker", "exec", name, "sh", "-c",
			"t2=$(ls -d "+containerConfigRoot+"/sites.gen-* | sort | tail -n1) && ln -sfn $(basename \"$t2\") "+containerConfigRoot+"/sites && angie -t -c "+containerLiveConfig+" && angie -s reload -c "+containerLiveConfig,
		).CombinedOutput(); err != nil {
			t.Fatalf("container symlink swap + reload failed: %v\n%s", err, string(out))
		}
	}

	// Angie applies a reload asynchronously (the master starts new workers after the signal),
	// so the generation counter moves a moment after the reload command returns.
	var st2 proxy.Status
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
		st2, err = liveDrv.Status(t.Context())
		if err == nil && st2.Generation == "2" {
			break
		}
	}
	if err != nil || st2.Generation != "2" {
		t.Fatalf("expected generation 2 after reload, got %+v err=%v", st2, err)
	}
}
