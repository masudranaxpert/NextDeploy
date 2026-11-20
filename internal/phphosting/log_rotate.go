package phphosting

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// SiteLogMaxBytes is the size at which a PHP site's log file is rotated. PHP's error_log and the
// panel's other per-site logs append forever otherwise; a plugin that warns on every request can
// fill the disk.
const SiteLogMaxBytes = 10 << 20

// RotateSiteLogs renames every *.log file above SiteLogMaxBytes in each PHP site's logs folder to
// <name>.1, replacing the previous .1. PHP opens the log file for every message, so the next
// message starts a fresh file (owned by the site user, who owns the folder).
func (m *Manager) RotateSiteLogs(ctx context.Context) (rotated int) {
	if m == nil || m.DB == nil {
		return 0
	}
	sites, err := m.DB.ListPHPSites(ctx)
	if err != nil {
		return 0
	}
	for _, s := range sites {
		if ctx.Err() != nil {
			return rotated
		}
		rotated += rotateLogsIn(filepath.Join(m.SitePath(s.AppID), "logs"), SiteLogMaxBytes)
	}
	return rotated
}

func rotateLogsIn(dir string, max int64) (rotated int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() || fi.Size() <= max {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := os.Rename(p, p+".1"); err != nil {
			log.Printf("php logs: rotate %s: %v", p, err)
			continue
		}
		rotated++
	}
	return rotated
}
