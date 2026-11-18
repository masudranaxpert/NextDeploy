package phphosting

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"panel/internal/proxy"
)

var authUserRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// ValidAuthUser reports whether name is safe to use as a basic-auth user (it becomes part of a label key).
func ValidAuthUser(name string) bool { return authUserRe.MatchString(name) }

// siteSecurity loads a site's protection settings; a missing or damaged record means "none".
func (m *Manager) siteSecurity(ctx context.Context, appID string) proxy.SiteSecurity {
	raw := m.DB.GetPHPSecurityJSON(ctx, appID)
	var s proxy.SiteSecurity
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &s) != nil {
		return proxy.SiteSecurity{}
	}
	if !ValidAuthUser(s.BasicAuthUser) || !strings.HasPrefix(s.BasicAuthHash, "$2") {
		s.BasicAuthUser, s.BasicAuthHash = "", ""
	}
	return s
}

// SiteSecurity is the exported read used by the API.
func (m *Manager) SiteSecurity(ctx context.Context, appID string) proxy.SiteSecurity {
	return m.siteSecurity(ctx, appID)
}

// SaveSiteSecurity stores the settings and synchronizes routes with the active proxy.
func (m *Manager) SaveSiteSecurity(ctx context.Context, appID string, s proxy.SiteSecurity) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := m.DB.SetPHPSecurityJSON(ctx, appID, string(b)); err != nil {
		return err
	}
	return m.SyncRoutes(ctx, appID)
}
