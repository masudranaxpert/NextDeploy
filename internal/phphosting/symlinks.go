package phphosting

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A site's own user can create symlinks in htdocs (shell, cron). The web server and the panel can
// read the whole workspace tree, so a link such as htdocs/l -> /data/workspaces/<other app> would
// let one site publish another site's files. Links that leave the site's own folder are removed.

func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// removeEscapingLink deletes the symlink at p when it points outside siteDir. It returns true if removed.
func removeEscapingLink(siteDir, realSiteDir, p string) bool {
	target, err := os.Readlink(p)
	if err != nil {
		return false
	}
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(p), target)
	}
	abs = filepath.Clean(abs)
	if !within(siteDir, abs) && !within(realSiteDir, abs) {
		return os.Remove(p) == nil
	}
	if real, err := filepath.EvalSymlinks(p); err == nil && !within(realSiteDir, real) {
		return os.Remove(p) == nil
	}
	return false
}

// SweepSymlinks removes escaping symlinks from one site and returns how many it removed.
func (m *Manager) SweepSymlinks(appID string) int {
	siteDir := filepath.Clean(m.SitePath(appID))
	realSite, err := filepath.EvalSymlinks(siteDir)
	if err != nil {
		return 0
	}
	removed, seen := 0, 0
	for _, d := range []string{"htdocs", "logs", "tmp"} {
		_ = filepath.WalkDir(filepath.Join(siteDir, d), func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if seen++; seen > 200000 {
				return filepath.SkipAll
			}
			if e.Type()&os.ModeSymlink != 0 && removeEscapingLink(siteDir, realSite, p) {
				removed++
				fmt.Fprintf(os.Stderr, "php: removed symlink %s that points outside its site\n", p)
			}
			return nil
		})
	}
	return removed
}

// StartSymlinkSweeper continuously checks PHP sites to stop TOCTOU symlink traversal.
func (m *Manager) StartSymlinkSweeper(ctx context.Context) {
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			sites, err := m.DB.ListPHPSites(ctx)
			if err != nil {
				continue
			}
			for _, s := range sites {
				m.SweepSymlinks(s.AppID)
			}
		}
	}()
}
