package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

func (p *Panel) isPHPPanelPublicRelPath(appID, rel string) bool {
	app, err := p.DB.GetApp(context.Background(), appID)
	if err != nil || app.TemplateID != "php_panel" {
		return false
	}
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	if rel == "" || !strings.HasPrefix(rel, "sites/") {
		return false
	}
	return strings.Contains(rel, "/public_html")
}

func (p *Panel) ensurePHPPanelPublicReadable(appID, rel string, isDir bool) {
	if !p.isPHPPanelPublicRelPath(appID, rel) {
		return
	}
	root := p.Store.Path(appID)
	_ = os.Chmod(root, 0755)

	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	cur := root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		mode := os.FileMode(0755)
		if i == len(parts)-1 && !isDir {
			mode = 0644
		}
		if _, err := os.Stat(cur); err == nil {
			_ = os.Chmod(cur, mode)
		}
	}
}
