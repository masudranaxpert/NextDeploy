package phphosting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// phpMyAdmin runs as one shared, official-image container. Nothing is published on the host:
// the panel itself reverse-proxies /phpmyadmin/* after checking the NextDeploy session, and injects a
// short-lived database login, so users never see a phpMyAdmin login screen.
const (
	PMAContainer   = "nd-phpmyadmin"
	PMANetwork     = "nd-pma" // only the panel and phpMyAdmin are on it
	DefaultPMA     = "phpmyadmin:5-apache"
	settingPMAOn   = "php_pma_enabled"
	settingPMAImg  = "php_pma_image"
	pmaUserPrefix  = "ndpma_"
	pmaSessionTTL  = 4 * time.Hour
	pmaMaxSessions = 200
	pmaSpec        = "2"
)

// Everything below runs inside the container before the stock entrypoint: serve under /pma,
// and switch phpMyAdmin to HTTP authentication so the panel can hand it the temporary login.
const pmaBootScript = `set -e
echo 'Alias /phpmyadmin /var/www/html' > /etc/apache2/conf-enabled/nd-pma.conf
echo 'ServerName localhost' > /etc/apache2/conf-enabled/nd-name.conf
printf '%s' "$ND_PMA_CONF" > /etc/phpmyadmin/config.user.inc.php
exec /docker-entrypoint.sh apache2-foreground`

const pmaUserConfig = `<?php
$cfg['Servers'][1]['auth_type'] = 'http';
$cfg['Servers'][1]['host'] = 'nd-mysql';
$cfg['Servers'][1]['AllowNoPassword'] = false;
$cfg['AllowArbitraryServer'] = false;
$cfg['VersionCheck'] = false;
$cfg['SendErrorReports'] = 'never';
$cfg['ShowServerInfo'] = false;
$cfg['ExecTimeLimit'] = 300;
$cfg['LoginCookieValidity'] = 14400;
`

type PMAStatus struct {
	Enabled bool   `json:"enabled"`
	Exists  bool   `json:"exists"`
	Running bool   `json:"running"`
	Image   string `json:"image"`
}

func (m *Manager) pmaImage(ctx context.Context) string {
	if v := strings.TrimSpace(m.DB.GetSetting(ctx, settingPMAImg)); v != "" {
		return v
	}
	return DefaultPMA
}

func (m *Manager) PMAEnabled(ctx context.Context) bool {
	return m.DB.GetSetting(ctx, settingPMAOn) == "1"
}

func (m *Manager) PMAStatus(ctx context.Context) PMAStatus {
	st := PMAStatus{Enabled: m.PMAEnabled(ctx), Image: m.pmaImage(ctx)}
	ci := inspectContainer(ctx, PMAContainer)
	st.Exists, st.Running = ci.Exists, ci.Running
	return st
}

// EnablePMA creates (or restarts) the phpMyAdmin container. The database module must be on.
func (m *Manager) EnablePMA(ctx context.Context) error {
	if !m.MySQLEnabled(ctx) {
		return fmt.Errorf("enable the database first")
	}
	if err := m.DB.SetSetting(ctx, settingPMAOn, "1"); err != nil {
		return err
	}
	return m.ensurePMA(ctx, false)
}

func (m *Manager) ensurePMA(ctx context.Context, recreate bool) error {
	image := m.pmaImage(ctx)
	if err := m.ensureNetwork(ctx, m.DBNetwork); err != nil {
		return err
	}
	if err := m.ensureNetwork(ctx, PMANetwork); err != nil {
		return err
	}
	want := image + "|" + pmaSpec
	ci := inspectContainer(ctx, PMAContainer)
	if ci.Exists && !recreate && ci.Labels["nd.php.config"] == want {
		if !ci.Running {
			if res := docker(ctx, "start", PMAContainer); !res.OK {
				return fmt.Errorf("start %s: %s", PMAContainer, res.Output)
			}
		}
		m.connectSelfToPMA(ctx)
		return nil
	}
	removeContainer(ctx, PMAContainer)
	args := []string{"run", "-d", "--name", PMAContainer, "--restart", "unless-stopped",
		"--network", PMANetwork,
		"--label", "nd.php=pma", "--label", "nd.php.config=" + want,
		"--cap-drop", "ALL", "--cap-add", "SETUID", "--cap-add", "SETGID", "--cap-add", "NET_BIND_SERVICE",
		"--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE", "--cap-add", "FOWNER",
		"--security-opt", "no-new-privileges",
		"--memory", "256m", "--memory-swap", "256m", "--pids-limit", "200",
		"-e", "PMA_HOST=" + MySQLContainer,
		"-e", "UPLOAD_LIMIT=512M",
		"-e", "MEMORY_LIMIT=512M",
		"-e", "MAX_EXECUTION_TIME=300",
		"-e", "ND_PMA_CONF=" + pmaUserConfig,
		"--entrypoint", "sh", image, "-c", pmaBootScript}
	if res := docker(ctx, args...); !res.OK {
		return fmt.Errorf("start phpMyAdmin: %s", res.Output)
	}
	if res := docker(ctx, "network", "connect", m.DBNetwork, PMAContainer); !res.OK && !strings.Contains(res.Output, "already exists") {
		removeContainer(ctx, PMAContainer)
		return fmt.Errorf("connect phpMyAdmin to the database network: %s", res.Output)
	}
	m.connectSelfToPMA(ctx)
	return nil
}

// connectSelfToPMA joins the panel's own container to the private phpMyAdmin network (no-op
// when the panel runs directly on the host, which can reach the bridge IP anyway).
func (m *Manager) connectSelfToPMA(ctx context.Context) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return
	}
	host, _ := os.Hostname()
	if host == "" {
		return
	}
	_ = docker(ctx, "network", "connect", PMANetwork, host)
}

// PMAAddr returns host:port the panel can reach phpMyAdmin on.
func (m *Manager) PMAAddr(ctx context.Context) (string, error) {
	res := docker(ctx, "inspect", "--format", fmt.Sprintf("{{with index .NetworkSettings.Networks %q}}{{.IPAddress}}{{end}}", PMANetwork), PMAContainer)
	ip := strings.TrimSpace(res.Output)
	if !res.OK || net.ParseIP(ip) == nil {
		return "", fmt.Errorf("phpMyAdmin is not running")
	}
	return ip + ":80", nil
}

func (m *Manager) RestartPMA(ctx context.Context) error {
	return m.ensurePMA(ctx, true)
}

func (m *Manager) StopPMA(ctx context.Context) error {
	_ = m.DB.SetSetting(ctx, settingPMAOn, "0")
	m.pma.dropAll(ctx, m)
	removeContainer(ctx, PMAContainer)
	return nil
}

// ---- single sign-on sessions ----

// PMASession is a temporary database login bound to one panel user.
type PMASession struct {
	Token    string
	UserID   int64
	AppID    string // "" = administrator view of every database
	DBUser   string
	Password string
	Expires  time.Time
}

type pmaRegistry struct {
	mu   sync.Mutex
	byTk map[string]*PMASession
}

func (r *pmaRegistry) get(token string) (*PMASession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byTk[token]
	if !ok || time.Now().After(s.Expires) {
		return nil, false
	}
	cp := *s
	return &cp, true
}

func (r *pmaRegistry) dropAll(ctx context.Context, m *Manager) {
	r.mu.Lock()
	all := r.byTk
	r.byTk = nil
	r.mu.Unlock()
	for _, s := range all {
		_ = m.DropDBUser(ctx, s.DBUser)
	}
	m.dropStalePMAUsers(ctx)
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PMAOpen creates a temporary login for the user. It is limited to dbNames; all (administrator
// only) grants every database.
func (m *Manager) PMAOpen(ctx context.Context, userID int64, appID string, dbNames []string, all bool) (*PMASession, error) {
	if !m.PMAEnabled(ctx) {
		return nil, fmt.Errorf("phpMyAdmin is not enabled")
	}
	if st := m.PMAStatus(ctx); !st.Running {
		if err := m.ensurePMA(ctx, false); err != nil {
			return nil, err
		}
	}
	if !all && len(dbNames) == 0 {
		return nil, fmt.Errorf("this site has no databases yet")
	}
	// A user keeps one login per site; reopening replaces the previous one.
	m.pma.mu.Lock()
	if m.pma.byTk == nil {
		m.pma.byTk = map[string]*PMASession{}
	}
	var old []*PMASession
	for k, s := range m.pma.byTk {
		if s.UserID == userID && s.AppID == appID {
			old = append(old, s)
			delete(m.pma.byTk, k)
		}
	}
	if len(m.pma.byTk) >= pmaMaxSessions {
		m.pma.mu.Unlock()
		return nil, fmt.Errorf("too many open phpMyAdmin sessions, try again later")
	}
	m.pma.mu.Unlock()
	for _, s := range old {
		_ = m.DropDBUser(ctx, s.DBUser)
	}

	rnd := make([]byte, 6)
	_, _ = rand.Read(rnd)
	s := &PMASession{
		Token: newToken(), UserID: userID, AppID: appID,
		DBUser: pmaUserPrefix + hex.EncodeToString(rnd), Password: RandomPassword(24),
		Expires: time.Now().Add(pmaSessionTTL),
	}
	userBase := fmt.Sprintf("CREATE USER %s@'%%' IDENTIFIED BY %s ", SQLString(s.DBUser), SQLString(s.Password))
	plainUser := userBase + "WITH MAX_USER_CONNECTIONS 10;"
	var stmt string
	if all {
		stmt += fmt.Sprintf("GRANT ALL PRIVILEGES ON *.* TO %s@'%%' WITH GRANT OPTION;", SQLString(s.DBUser))
	} else {
		for _, d := range dbNames {
			if !ValidIdent(d) {
				continue
			}
			stmt += fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s@'%%';", SQLIdent(strings.ReplaceAll(d, "_", "\\_")), SQLString(s.DBUser))
		}
	}
	stmt += "FLUSH PRIVILEGES;"
	preferred := userBase + userLimitClause(m.mysqlImage(ctx), 10, pmaQuerySeconds) + ";" + stmt
	if _, err := m.sqlPreferring(ctx, preferred, plainUser+stmt); err != nil {
		_ = m.DropDBUser(ctx, s.DBUser)
		return nil, err
	}
	m.pma.mu.Lock()
	m.pma.byTk[s.Token] = s
	m.pma.mu.Unlock()
	return s, nil
}

func (m *Manager) PMASessionFor(token string) (*PMASession, bool) { return m.pma.get(token) }

// PMAClose drops a session's database login.
func (m *Manager) PMAClose(ctx context.Context, token string) {
	m.pma.mu.Lock()
	s := m.pma.byTk[token]
	delete(m.pma.byTk, token)
	m.pma.mu.Unlock()
	if s != nil {
		_ = m.DropDBUser(ctx, s.DBUser)
	}
}

// dropStalePMAUsers removes every temporary login (used at startup and when phpMyAdmin is stopped).
func (m *Manager) dropStalePMAUsers(ctx context.Context) {
	if !m.MySQLEnabled(ctx) || !inspectContainer(ctx, MySQLContainer).Running {
		return
	}
	out, err := m.sql(ctx, fmt.Sprintf("SELECT user FROM mysql.user WHERE user LIKE '%s%%';", strings.ReplaceAll(pmaUserPrefix, "_", "\\_")))
	if err != nil {
		return
	}
	for _, u := range strings.Fields(out) {
		if strings.HasPrefix(u, pmaUserPrefix) && ValidIdent(u) {
			_ = m.DropDBUser(ctx, u)
		}
	}
}

// StartPMAJanitor expires old logins and, on start, clears leftovers of a previous run.
func (m *Manager) StartPMAJanitor(ctx context.Context) {
	go func() {
		m.dropStalePMAUsers(ctx)
		if m.PMAEnabled(ctx) {
			if err := m.ensurePMA(ctx, false); err != nil {
				fmt.Fprintf(os.Stderr, "phpmyadmin: %v\n", err)
			}
		}
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			var dead []*PMASession
			m.pma.mu.Lock()
			for k, s := range m.pma.byTk {
				if time.Now().After(s.Expires) {
					dead = append(dead, s)
					delete(m.pma.byTk, k)
				}
			}
			m.pma.mu.Unlock()
			for _, s := range dead {
				_ = m.DropDBUser(ctx, s.DBUser)
			}
		}
	}()
}
