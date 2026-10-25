package phphosting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
)

// runtimeSpecVersion is mixed into the container hash: bump it whenever the `docker run`
// arguments below change so existing containers are recreated with the new settings.
const runtimeSpecVersion = "3"

func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// SettingsFor returns the effective, parsed settings of a site.
func SettingsFor(site db.PHPSite) SiteSettings { return ParseSettings(site.SettingsJSON) }

// EnsureRuntime makes the PHP-FPM container for (owner, version) match the sites assigned
// to it: pool files are rewritten and FPM reloaded when only pool content changed, the
// container is recreated when the set of mounted sites changed, and it is removed when no
// site is left.
func (m *Manager) EnsureRuntime(ctx context.Context, ownerID int64, version string) error {
	if version == "" {
		return nil
	}
	unlock := m.lock(runtimeKey(ownerID, version))
	defer unlock()

	name := RuntimeContainer(ownerID, version)
	dir := m.runtimeDir(ownerID, version)
	sites, err := m.DB.ListPHPSitesForRuntime(ctx, ownerID, version)
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		removeContainer(ctx, name)
		_ = os.RemoveAll(dir)
		return nil
	}
	image := ImageName(version)
	imgID := imageID(ctx, image)
	if imgID == "" {
		return fmt.Errorf("PHP %s is not installed", version)
	}
	owner, err := m.DB.GetUserByID(ctx, ownerID)
	if err != nil {
		return err
	}
	if owner.Status == db.UserStatusSuspended {
		// A suspended account's sites go offline; they come back through SyncOwner.
		_ = docker(ctx, "stop", name)
		return nil
	}
	cgroup := ""
	if m.CgroupParent != nil {
		cgroup = m.CgroupParent(ctx, owner)
	}
	if err := m.ensureNetwork(ctx, m.DBNetwork); err != nil {
		return err
	}

	poolsDir := filepath.Join(dir, "pools")
	runDir := filepath.Join(dir, "run")
	for _, d := range []string{poolsDir, runDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	pools := map[string]string{}
	ids := make([]string, 0, len(sites))
	for _, s := range sites {
		conf, err := RenderPool(PoolSpec{AppID: s.AppID, UID: s.UID, RootDir: s.RootDir, Settings: SettingsFor(s)})
		if err != nil {
			return fmt.Errorf("site %s: %w", s.AppID, err)
		}
		pools[s.AppID] = conf
		ids = append(ids, s.AppID)
		_ = m.PrepareSiteDirs(s)
	}
	sort.Strings(ids)
	want := hashStrings(runtimeSpecVersion, imgID, cgroup, strings.Join(ids, ","))

	ci := inspectContainer(ctx, name)
	if ci.Exists && ci.Running && ci.Labels["nd.php.hash"] == want {
		if err := WritePools(poolsDir, pools); err != nil {
			return err
		}
		if res := docker(ctx, "kill", "-s", "USR2", name); !res.OK {
			return fmt.Errorf("reload php-fpm: %s", res.Output)
		}
		return m.waitSockets(runDir, ids, 15*time.Second)
	}

	removeContainer(ctx, name)
	if err := WritePools(poolsDir, pools); err != nil {
		return err
	}
	if socks, _ := filepath.Glob(filepath.Join(runDir, "*.sock")); len(socks) > 0 {
		for _, s := range socks {
			_ = os.Remove(s)
		}
	}

	args := []string{"run", "-d", "--name", name, "--restart", "unless-stopped",
		"--network", m.DBNetwork,
		"--label", "nd.php=php",
		"--label", "nd.php.version=" + version,
		"--label", fmt.Sprintf("nd.php.owner=%d", ownerID),
		"--label", "nd.php.hash=" + want,
		// SETUID/SETGID: each pool drops to its site UID. DAC_READ_SEARCH: the pool worker
		// chdirs into the (site-owned, 0750) document root before it drops privileges.
		// KILL: the root master must be able to signal workers running as other UIDs
		// (graceful reload and stop). Workers lose every capability when they setuid().
		"--cap-drop", "ALL", "--cap-add", "SETUID", "--cap-add", "SETGID", "--cap-add", "DAC_READ_SEARCH", "--cap-add", "KILL",
		"--security-opt", "no-new-privileges",
		"--read-only", "--tmpfs", "/tmp:rw,nosuid,size=128m",
		"--pids-limit", "2048",
	}
	if cgroup != "" {
		args = append(args, "--cgroup-parent", cgroup)
	}
	args = append(args,
		"-v", m.HostPath(ctx, poolsDir)+":/nd/pools:ro",
		"-v", m.HostPath(ctx, runDir)+":/run/php")
	for _, s := range sites {
		args = append(args, "-v", m.HostPath(ctx, m.SitePath(s.AppID))+":"+SiteDir(s.AppID))
	}
	args = append(args, image)
	if res := docker(ctx, args...); !res.OK {
		return fmt.Errorf("start PHP container: %s", res.Output)
	}
	if err := m.waitSockets(runDir, ids, 20*time.Second); err != nil {
		logs := docker(ctx, "logs", "--tail", "20", name).Output
		return fmt.Errorf("%v\n%s", err, logs)
	}
	return nil
}

func (m *Manager) waitSockets(runDir string, ids []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		missing := ""
		for _, id := range ids {
			if _, err := os.Stat(filepath.Join(runDir, id+".sock")); err != nil {
				missing = id
				break
			}
		}
		if missing == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("php-fpm socket for %s did not appear", missing)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// ---- route holders ----

const holderImage = "busybox:stable"

func (m *Manager) ensureHolderImage(ctx context.Context) error {
	if imageID(ctx, holderImage) != "" {
		return nil
	}
	if res := docker(ctx, "pull", holderImage); !res.OK {
		return fmt.Errorf("pull %s: %s", holderImage, res.Output)
	}
	return nil
}

func labelsHash(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		parts = append(parts, k, labels[k])
	}
	return hashStrings(parts...)
}

// SiteLabels computes the Caddy labels for a site from its current domains.
func (m *Manager) SiteLabels(ctx context.Context, site db.PHPSite) map[string]string {
	all, _ := m.DB.ListAppDomains(ctx, site.AppID)
	domains := make([]db.AppDomain, 0, len(all))
	for _, d := range all {
		// Label values end up in a generated Caddyfile: never let anything but a plain host name through.
		if ValidHost(d.Domain) {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return nil
	}
	socket := ""
	if site.SiteType != SiteTypeStatic {
		socket = SocketPath(site.OwnerID, site.PHPVersion, site.AppID)
	}
	return caddy.PHPSiteLabels(domains, DocRoot(site.AppID, site.RootDir), socket, m.siteSecurity(ctx, site.AppID))
}

// SyncRoutes (re)creates the label-carrying holder container of a site. Only this site's
// holder is touched, so other sites keep serving while one site's domains change.
func (m *Manager) SyncRoutes(ctx context.Context, appID string) error {
	site, err := m.DB.GetPHPSite(ctx, appID)
	if err != nil {
		return err
	}
	name := RouteContainer(appID)
	labels := m.SiteLabels(ctx, site)
	if owner, err := m.DB.GetUserByID(ctx, site.OwnerID); err == nil && owner.Status == db.UserStatusSuspended {
		labels = nil // suspended account: take the site offline
	}
	if len(labels) == 0 {
		removeContainer(ctx, name)
		return nil
	}
	return m.PublishHolder(ctx, name, labels, map[string]string{"nd.php": "route", "nd.php.app": appID})
}

// PublishHolder (re)creates a tiny container whose only job is to carry Caddy labels. Nothing
// changes when the labels are identical to the running holder, so other routes keep serving
// while one route is edited. It is used for PHP sites and for the admin's proxy routes.
func (m *Manager) PublishHolder(ctx context.Context, name string, labels map[string]string, extra map[string]string) error {
	hash := labelsHash(labels)
	ci := inspectContainer(ctx, name)
	if ci.Exists && ci.Running && ci.Labels["nd.php.hash"] == hash {
		return nil
	}
	if err := m.ensureHolderImage(ctx); err != nil {
		return err
	}
	if err := m.ensureNetwork(ctx, m.IngressNetwork); err != nil {
		return err
	}
	removeContainer(ctx, name)
	args := []string{"run", "-d", "--name", name, "--restart", "unless-stopped",
		"--network", m.IngressNetwork,
		"--memory", "16m", "--cpus", "0.05", "--read-only", "--cap-drop", "ALL",
		"--label", "nd.php.hash=" + hash}
	extraKeys := make([]string, 0, len(extra))
	for k := range extra {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		args = append(args, "--label", k+"="+extra[k])
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--label", k+"="+labels[k])
	}
	args = append(args, "--entrypoint", "tail", holderImage, "-f", "/dev/null")
	if res := docker(ctx, args...); !res.OK {
		return fmt.Errorf("publish routes: %s", res.Output)
	}
	return nil
}

// RemoveHolder deletes a label holder (no error when it does not exist).
func (m *Manager) RemoveHolder(ctx context.Context, name string) {
	removeContainer(ctx, name)
}

// ---- site level operations ----

// SyncSite brings runtime and routes of one site in line with the database.
func (m *Manager) SyncSite(ctx context.Context, appID string) error {
	site, err := m.DB.GetPHPSite(ctx, appID)
	if err != nil {
		return err
	}
	if err := m.PrepareSiteDirs(site); err != nil {
		return err
	}
	if site.SiteType != SiteTypeStatic {
		if err := m.EnsureRuntime(ctx, site.OwnerID, site.PHPVersion); err != nil {
			return err
		}
	}
	return m.SyncRoutes(ctx, appID)
}

// ChangeVersion moves a site to another PHP version without downtime for the site's
// files or database: the pool is added to the new runtime first, routes are switched, and
// only then removed from the old runtime. On failure the previous version is restored.
func (m *Manager) ChangeVersion(ctx context.Context, appID, version string) error {
	if !IsSupportedVersion(version) {
		return fmt.Errorf("unsupported PHP version %q", version)
	}
	site, err := m.DB.GetPHPSite(ctx, appID)
	if err != nil {
		return err
	}
	if site.SiteType == SiteTypeStatic {
		return fmt.Errorf("static sites do not use PHP")
	}
	if site.PHPVersion == version {
		return nil
	}
	if !m.ImageInstalled(ctx, version) {
		return fmt.Errorf("PHP %s is not installed; ask an administrator to install it", version)
	}
	old := site.PHPVersion
	site.PHPVersion = version
	if err := m.DB.UpdatePHPSite(ctx, site); err != nil {
		return err
	}
	if err := m.EnsureRuntime(ctx, site.OwnerID, version); err != nil {
		site.PHPVersion = old
		_ = m.DB.UpdatePHPSite(ctx, site)
		_ = m.EnsureRuntime(ctx, site.OwnerID, version)
		_ = m.EnsureRuntime(ctx, site.OwnerID, old)
		return err
	}
	if err := m.SyncRoutes(ctx, appID); err != nil {
		return err
	}
	return m.EnsureRuntime(ctx, site.OwnerID, old)
}

// TeardownSite removes everything a site owns outside the panel database: route holder,
// databases and users, and its pool/mount. It deletes the php_sites row so the runtime
// no longer mounts the site; the caller removes the app itself afterwards.
func (m *Manager) TeardownSite(ctx context.Context, appID string) error {
	site, err := m.DB.GetPHPSite(ctx, appID)
	if err != nil {
		return nil // not a PHP site
	}
	removeContainer(ctx, RouteContainer(appID))
	dbs, _ := m.DB.ListPHPDatabases(ctx, appID)
	users, _ := m.DB.ListPHPDBUsers(ctx, appID)
	if (len(dbs) > 0 || len(users) > 0) && m.MySQLStatus(ctx).Running {
		for _, d := range dbs {
			if _, err := m.sql(ctx, "DROP DATABASE IF EXISTS "+SQLIdent(d.DBName)+";"); err != nil {
				return fmt.Errorf("drop database %s: %w", d.DBName, err)
			}
			if d.DBUser != "" && ValidIdent(d.DBUser) { // legacy one-user-per-database rows
				_ = m.DropDBUser(ctx, d.DBUser)
			}
		}
		for _, u := range users {
			if err := m.DropDBUser(ctx, u.UserName); err != nil {
				return fmt.Errorf("drop user %s: %w", u.UserName, err)
			}
		}
	}
	if _, err := m.DB.ExecRaw(ctx, `DELETE FROM php_sites WHERE app_id = ?`, appID); err != nil {
		return err
	}
	if site.SiteType != SiteTypeStatic {
		if err := m.EnsureRuntime(ctx, site.OwnerID, site.PHPVersion); err != nil {
			return err
		}
	}
	return nil
}

// Reconcile restores the desired state after a panel restart. Containers have restart
// policies, so this only repairs what is missing or stale.
func (m *Manager) Reconcile(ctx context.Context) {
	m.RepairGrants(ctx)
	sites, err := m.DB.ListPHPSites(ctx)
	if err != nil || len(sites) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, s := range sites {
		if s.SiteType == SiteTypeStatic || s.PHPVersion == "" {
			continue
		}
		k := runtimeKey(s.OwnerID, s.PHPVersion)
		if seen[k] {
			continue
		}
		seen[k] = true
		if err := m.EnsureRuntime(ctx, s.OwnerID, s.PHPVersion); err != nil {
			fmt.Fprintf(os.Stderr, "php reconcile: runtime %s: %v\n", k, err)
		}
	}
	for _, s := range sites {
		if err := m.SyncRoutes(ctx, s.AppID); err != nil {
			fmt.Fprintf(os.Stderr, "php reconcile: routes %s: %v\n", s.AppID, err)
		}
	}
}

// SyncOwner re-applies runtimes and routes for every site of one user. It is used after a
// suspend/resume so that suspended accounts go offline and resumed ones come back.
func (m *Manager) SyncOwner(ctx context.Context, ownerID int64) error {
	sites, err := m.DB.ListPHPSitesByOwner(ctx, ownerID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var first error
	for _, s := range sites {
		if s.SiteType == SiteTypeStatic || s.PHPVersion == "" || seen[s.PHPVersion] {
			continue
		}
		seen[s.PHPVersion] = true
		if err := m.EnsureRuntime(ctx, ownerID, s.PHPVersion); err != nil && first == nil {
			first = err
		}
	}
	for _, s := range sites {
		if err := m.SyncRoutes(ctx, s.AppID); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// TransferSite moves a site to another owner. The site keeps its UID, so file ownership is
// untouched; only the runtime container (and with it the resource quota) changes.
func (m *Manager) TransferSite(ctx context.Context, appID string, newOwnerID int64) error {
	site, err := m.DB.GetPHPSite(ctx, appID)
	if err != nil {
		return nil // not a PHP site
	}
	oldOwner := site.OwnerID
	if oldOwner == newOwnerID {
		return nil
	}
	if _, err := m.DB.GetUserByID(ctx, newOwnerID); err != nil {
		return err
	}
	site.OwnerID = newOwnerID
	if err := m.DB.UpdatePHPSite(ctx, site); err != nil {
		return err
	}
	if site.SiteType != SiteTypeStatic {
		if err := m.EnsureRuntime(ctx, newOwnerID, site.PHPVersion); err != nil {
			site.OwnerID = oldOwner
			_ = m.DB.UpdatePHPSite(ctx, site)
			_ = m.EnsureRuntime(ctx, newOwnerID, site.PHPVersion)
			return err
		}
	}
	if err := m.SyncRoutes(ctx, appID); err != nil {
		return err
	}
	if site.SiteType != SiteTypeStatic {
		return m.EnsureRuntime(ctx, oldOwner, site.PHPVersion)
	}
	return nil
}

// ReassignOwner hands every site of a user (about to be deleted) to another user.
func (m *Manager) ReassignOwner(ctx context.Context, fromOwner, toOwner int64) error {
	sites, err := m.DB.ListPHPSitesByOwner(ctx, fromOwner)
	if err != nil {
		return err
	}
	for _, s := range sites {
		if err := m.TransferSite(ctx, s.AppID, toOwner); err != nil {
			return fmt.Errorf("site %s: %w", s.AppID, err)
		}
	}
	return nil
}

var hostRe = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidHost reports whether s is a plain (optionally wildcard) DNS host name.
func ValidHost(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return len(s) <= 253 && hostRe.MatchString(s)
}
