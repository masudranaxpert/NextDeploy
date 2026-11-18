package handlers

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"panel/internal/dockerapi"
	"panel/internal/proxy/pki"
)

// keepSwitchBackups is how many root compose backups (*.proxy-switch-*.bak) are retained.
const keepSwitchBackups = 3

// proxyContainerImage returns the image reference of a container ("" when it does not exist).
var proxyContainerImage = func(ctx context.Context, name string) string {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Config.Image}}", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// cleanupAfterProxySwitch removes what the previous engine left behind once a switch has
// succeeded: its label-carrier holders, rendered config, unused image, old compose backups and
// (when leaving Angie/Ferron) the internal leaf certificates. Certificate volumes (caddy_data,
// angie_acme, ferron_acme) are kept on purpose: switching back must not re-issue every
// certificate, which would run into Let's Encrypt rate limits.
func (p *Panel) cleanupAfterProxySwitch(ctx context.Context, from, to, oldImage string) []string {
	var done []string
	from, to = normalizeProxyEngine(from), normalizeProxyEngine(to)
	if from == to {
		return nil
	}

	// 1. Caddy label carriers (nd-route-*, nd-phpr-*) mean nothing to Angie/Ferron. The
	//    reconcilers prune every holder that is not wanted on the active engine.
	if to != "caddy" {
		p.ReconcileProxyRoutes(ctx)
		if p.PHP != nil {
			p.PHP.Reconcile(ctx)
		}
		done = append(done, "label holders")
	}

	// 2. Rendered config of the previous file-based engine (regenerated on the next switch).
	if dir := proxyEngineDataDir(from); dir != "" && dir != proxyEngineDataDir(to) {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("proxy cleanup: remove %s: %v", dir, err)
		} else {
			done = append(done, dir)
		}
	}

	// 3. Internal leaf certificates are only read by Angie/Ferron; Caddy issues its own. The CA
	//    (two small files) stays, so later leaves chain to the same root.
	if to == "caddy" {
		if mgr, err := pki.NewManager(pki.DefaultCADir, pki.DefaultSitesDir); err == nil {
			if err := mgr.PruneOrphans(map[string]bool{}); err == nil {
				done = append(done, "internal leaf certs")
			}
		}
	}

	// 4. The old engine's image, unless another container still uses it (Docker refuses then).
	if oldImage != "" && imageIsPullable(ctx, oldImage) {
		if err := dockerapi.RemoveImageByID(ctx, oldImage); err != nil {
			log.Printf("proxy cleanup: image %s kept: %v", oldImage, err)
		} else {
			done = append(done, "image "+oldImage)
		}
	}

	// 5. Only the newest root compose backups.
	if n := pruneSwitchBackups(p.nextDeployComposePath(), keepSwitchBackups); n > 0 {
		done = append(done, "old compose backups")
	}

	if len(done) > 0 {
		log.Printf("proxy cleanup after %s -> %s: %s", from, to, strings.Join(done, ", "))
	}
	return done
}

// imageIsPullable reports whether ref came from a registry (has a repo digest), so a later
// switch back can pull it again. Locally built images are never removed.
var imageIsPullable = func(ctx context.Context, ref string) bool {
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{len .RepoDigests}}", ref).Output()
	return err == nil && strings.TrimSpace(string(out)) != "0"
}

// pruneSwitchBackups deletes all but the newest keep "<compose>.proxy-switch-*.bak" files.
func pruneSwitchBackups(composePath string, keep int) int {
	matches, err := filepath.Glob(composePath + ".proxy-switch-*.bak")
	if err != nil || len(matches) <= keep {
		return 0
	}
	sort.Strings(matches) // UTC timestamp in the name sorts chronologically
	removed := 0
	for _, f := range matches[:len(matches)-keep] {
		if os.Remove(f) == nil {
			removed++
		}
	}
	return removed
}
