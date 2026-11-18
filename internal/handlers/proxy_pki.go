package handlers

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/proxy/pki"
)

const caddyLocalCADir = "/data/caddy/pki/authorities/local"

// readCaddyLocalRoot returns Caddy's local root CA (certificate and key PEM). Shield edges pin
// this root for origin TLS, so Angie/Ferron must present certificates that chain to it. It is
// read from the running caddy container, or from the kept caddy_data volume after a switch.
var readCaddyLocalRoot = func(ctx context.Context) (certPEM, keyPEM []byte, ok bool) {
	read := func(args ...string) []byte {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cctx, "docker", args...).Output()
		if err != nil {
			return nil
		}
		return out
	}
	crt := read("exec", "caddy", "cat", caddyLocalCADir+"/root.crt")
	key := read("exec", "caddy", "cat", caddyLocalCADir+"/root.key")
	if len(crt) == 0 || len(key) == 0 {
		for _, vol := range []string{"nextdeploy_caddy_data", "caddy_data"} {
			crt = read("run", "--rm", "--quiet", "-v", vol+":/d:ro", "busybox:stable", "cat", "/d/caddy/pki/authorities/local/root.crt")
			key = read("run", "--rm", "--quiet", "-v", vol+":/d:ro", "busybox:stable", "cat", "/d/caddy/pki/authorities/local/root.key")
			if len(crt) > 0 && len(key) > 0 {
				break
			}
		}
	}
	if !bytes.Contains(crt, []byte("BEGIN CERTIFICATE")) || !bytes.Contains(key, []byte("PRIVATE KEY")) {
		return nil, nil, false
	}
	return crt, key, true
}

// alignPKIWithCaddyRoot makes the internal PKI use Caddy's local root CA, so origin certificates
// served by Angie/Ferron are trusted by Shield edges exactly like Caddy's own. When the CA
// changes, leaf certificates are removed so they are re-issued under the new root.
func alignPKIWithCaddyRoot(ctx context.Context, caDir, sitesDir string) {
	crt, key, ok := readCaddyLocalRoot(ctx)
	if !ok {
		return
	}
	caCrt := filepath.Join(caDir, pki.CACertFilename)
	if cur, err := os.ReadFile(caCrt); err == nil && bytes.Equal(bytes.TrimSpace(cur), bytes.TrimSpace(crt)) {
		return
	}
	if err := os.MkdirAll(caDir, pki.CADirMode); err != nil {
		log.Printf("proxy: internal PKI: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(caDir, pki.CAKeyFilename), key, pki.PrivateKeyMode); err != nil {
		log.Printf("proxy: internal PKI: write CA key: %v", err)
		return
	}
	if err := os.WriteFile(caCrt, crt, pki.CertMode); err != nil {
		log.Printf("proxy: internal PKI: write CA cert: %v", err)
		return
	}
	entries, _ := os.ReadDir(sitesDir)
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".crt") || strings.HasSuffix(n, ".key") {
			_ = os.Remove(filepath.Join(sitesDir, n))
		}
	}
	log.Printf("proxy: internal PKI now uses the Caddy local root CA (trusted by Shield edges)")
}
