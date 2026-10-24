package runutil

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

var (
	helperOnce sync.Once
	helperImg  string
)

// HelperImage is the image used for the short-lived privileged helper containers (host shell,
// kill/restart scripts, traffic shaping). It must never be a third-party tag: those helpers run
// as root in the host's namespaces. In order: PANEL_HELPER_IMAGE / PANEL_IMAGE, then the exact
// image ID this panel container was started from (pinned, no pull), then the project's own image.
func HelperImage() string {
	helperOnce.Do(func() {
		for _, k := range []string{"PANEL_HELPER_IMAGE", "PANEL_IMAGE"} {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				helperImg = v
				return
			}
		}
		if _, err := os.Stat("/.dockerenv"); err == nil {
			if host, _ := os.Hostname(); host != "" {
				if out, err := exec.Command("docker", "inspect", "--format", "{{.Image}}", host).Output(); err == nil {
					if id := strings.TrimSpace(string(out)); strings.HasPrefix(id, "sha256:") {
						helperImg = id
						return
					}
				}
			}
		}
		helperImg = "masudranaxpert/nextdeploy:latest"
	})
	return helperImg
}
