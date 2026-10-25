package phphosting

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/runutil"
)

const (
	settingMySQLEnabled  = "php_mysql_enabled"
	settingMySQLImage    = "php_mysql_image"
	settingMySQLMemoryMB = "php_mysql_memory_mb"
	settingMySQLRootPass = "php_mysql_root_password"
)

var identRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func ValidIdent(s string) bool { return identRe.MatchString(s) }

// SQLString quotes a value as a MySQL string literal.
func SQLString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\x00", `\0`, "\n", `\n`, "\r", `\r`, "\x1a", `\Z`)
	return "'" + r.Replace(s) + "'"
}

// SQLIdent quotes an identifier with backticks.
func SQLIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

// GrantIdent quotes a database name for GRANT/REVOKE. In those statements `_` and `%` are
// wildcards even inside backticks, so a grant on u2__x would also reach u21_x (another user's
// database). Escaping makes the grant match this one database only.
func GrantIdent(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "_", "\\_")
	s = strings.ReplaceAll(s, "%", "\\%")
	return SQLIdent(s)
}

const passAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// RandomPassword returns a random password made of unambiguous alphanumerics.
func RandomPassword(n int) string {
	if n < 12 {
		n = 12
	}
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(passAlphabet))))
		if err != nil {
			panic(err)
		}
		out[i] = passAlphabet[idx.Int64()]
	}
	return string(out)
}

type MySQLStatus struct {
	Enabled  bool   `json:"enabled"`
	Exists   bool   `json:"exists"`
	Running  bool   `json:"running"`
	Ready    bool   `json:"ready"`
	Image    string `json:"image"`
	MemoryMB int    `json:"memory_mb"`
	// What the memory is split into, and the limits that protect other sites (shown to the admin).
	HostMemoryMB   int `json:"host_memory_mb"`
	BufferPoolMB   int `json:"buffer_pool_mb"`
	MaxConnections int `json:"max_connections"`
	QuerySeconds   int `json:"query_seconds"`
	IdleSeconds    int `json:"idle_seconds"`
	// QueryLimitSupported: the per-user query limit is a MariaDB feature (the panel's database).
	QueryLimitSupported bool `json:"query_limit_supported"`
}

func (m *Manager) mysqlImage(ctx context.Context) string {
	if v := strings.TrimSpace(m.DB.GetSetting(ctx, settingMySQLImage)); v != "" {
		return v
	}
	return DefaultMySQL
}

func (m *Manager) mysqlMemoryMB(ctx context.Context) int {
	if n, err := strconv.Atoi(strings.TrimSpace(m.DB.GetSetting(ctx, settingMySQLMemoryMB))); err == nil && n >= 256 {
		return n
	}
	// not chosen by the admin: keep what a running database already has (changing it would
	// recreate the container); a new one gets a share of the host's RAM
	if ci := inspectContainer(ctx, MySQLContainer); ci.Exists {
		if parts := strings.Split(ci.Labels["nd.php.config"], "|"); len(parts) == 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n >= 256 {
				return n
			}
		}
	}
	return DBMemoryDefaultMB(hostRAMMB())
}

func isMariaDB(image string) bool {
	return !strings.HasPrefix(strings.TrimPrefix(image, "docker.io/library/"), "mysql")
}

func clientBin(image string) string {
	if isMariaDB(image) {
		return "mariadb"
	}
	return "mysql"
}

func dumpBin(image string) string {
	if isMariaDB(image) {
		return "mariadb-dump"
	}
	return "mysqldump"
}

func (m *Manager) MySQLEnabled(ctx context.Context) bool {
	return m.DB.GetSetting(ctx, settingMySQLEnabled) == "1"
}

func (m *Manager) MySQLStatus(ctx context.Context) MySQLStatus {
	status.mu.Lock()
	if !status.mysqlAt.IsZero() || status.mysql.Image != "" {
		st := status.mysql
		fresh := time.Since(status.mysqlAt) < mysqlTTL
		status.mu.Unlock()
		if !fresh {
			status.refreshOnce(2, func() {
				bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				fresh := m.mysqlStatusUncached(bg)
				status.mu.Lock()
				status.mysql, status.mysqlAt = fresh, time.Now()
				status.mu.Unlock()
			})
		}
		return st
	}
	status.mu.Unlock()
	st := m.mysqlStatusUncached(ctx)
	status.mu.Lock()
	status.mysql, status.mysqlAt = st, time.Now()
	status.mu.Unlock()
	return st
}

func (m *Manager) mysqlStatusUncached(ctx context.Context) MySQLStatus {
	st := MySQLStatus{Enabled: m.MySQLEnabled(ctx), Image: m.mysqlImage(ctx), MemoryMB: m.mysqlMemoryMB(ctx)}
	t := TuneDB(st.MemoryMB)
	st.HostMemoryMB, st.BufferPoolMB, st.MaxConnections = hostRAMMB(), t.BufferPoolMB, t.MaxConnections
	st.QuerySeconds, st.IdleSeconds, st.QueryLimitSupported = m.querySeconds(ctx), idleTimeoutSeconds, isMariaDB(st.Image)
	ci := inspectContainer(ctx, MySQLContainer)
	st.Exists, st.Running = ci.Exists, ci.Running
	if st.Running {
		_, err := m.sql(ctx, "SELECT 1")
		st.Ready = err == nil
	}
	return st
}

func (m *Manager) rootPassword(ctx context.Context) (string, error) {
	pw := m.DB.GetSetting(ctx, settingMySQLRootPass)
	if pw != "" {
		return pw, nil
	}
	pw = RandomPassword(32)
	if err := m.DB.SetSetting(ctx, settingMySQLRootPass, pw); err != nil {
		return "", err
	}
	return pw, nil
}

// EnableMySQL creates (or starts) the shared database container and waits until it answers.
func (m *Manager) EnableMySQL(ctx context.Context, memoryMB int) error {
	if memoryMB >= 256 {
		_ = m.DB.SetSetting(ctx, settingMySQLMemoryMB, strconv.Itoa(memoryMB))
	}
	if err := m.DB.SetSetting(ctx, settingMySQLEnabled, "1"); err != nil {
		return err
	}
	return m.ensureMySQL(ctx, true)
}

// ensureMySQL makes sure the container matches the desired image/memory. recreate forces a
// recreate when the configuration drifted; the data volume is never touched.
func (m *Manager) ensureMySQL(ctx context.Context, recreate bool) error {
	image := m.mysqlImage(ctx)
	mem := m.mysqlMemoryMB(ctx)
	pw, err := m.rootPassword(ctx)
	if err != nil {
		return err
	}
	if err := m.ensureNetwork(ctx, m.DBNetwork); err != nil {
		return err
	}
	want := fmt.Sprintf("%s|%d", image, mem)
	ci := inspectContainer(ctx, MySQLContainer)
	if ci.Exists && ci.Labels["nd.php.config"] == want {
		if !ci.Running {
			if res := docker(ctx, "start", MySQLContainer); !res.OK {
				return fmt.Errorf("start %s: %s", MySQLContainer, res.Output)
			}
		}
		return m.waitMySQL(ctx, 120*time.Second)
	}
	if ci.Exists && !recreate {
		return m.waitMySQL(ctx, 120*time.Second)
	}
	removeContainer(ctx, MySQLContainer)
	rootEnv := "MARIADB_ROOT_PASSWORD=" + pw
	if !isMariaDB(image) {
		rootEnv = "MYSQL_ROOT_PASSWORD=" + pw
	}
	args := []string{"run", "-d", "--name", MySQLContainer, "--restart", "unless-stopped",
		"--network", m.DBNetwork,
		"--label", "nd.php=mysql", "--label", "nd.php.config=" + want,
		"-v", MySQLVolume + ":/var/lib/mysql",
		"-e", rootEnv,
		"--memory", fmt.Sprintf("%dm", mem), "--memory-swap", fmt.Sprintf("%dm", mem),
		image,
		"--character-set-server=utf8mb4", "--collation-server=utf8mb4_unicode_ci",
		"--skip-name-resolve", "--max-allowed-packet=64M"}
	args = append(args, TuneDB(mem).serverArgs()...)
	if isMariaDB(image) {
		args = append([]string{args[0]}, append([]string{"-e", "MARIADB_AUTO_UPGRADE=1"}, args[1:]...)...)
	}
	if res := docker(ctx, args...); !res.OK {
		return fmt.Errorf("start database: %s", res.Output)
	}
	return m.waitMySQL(ctx, 180*time.Second)
}

func (m *Manager) waitMySQL(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if _, err := m.sql(ctx, "SELECT 1"); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("database did not become ready: %v", last)
}

// StopMySQL stops the container and marks the module's database as disabled.
func (m *Manager) StopMySQL(ctx context.Context) error {
	_ = m.DB.SetSetting(ctx, settingMySQLEnabled, "0")
	if res := docker(ctx, "stop", MySQLContainer); !res.OK && !strings.Contains(strings.ToLower(res.Output), "no such container") {
		return fmt.Errorf("%s", res.Output)
	}
	return nil
}

// sql runs statements as root; the password travels via the exec environment and
// the statements via stdin, so neither shows up in process listings.
func (m *Manager) sql(ctx context.Context, stmt string) (string, error) {
	pw := m.DB.GetSetting(ctx, settingMySQLRootPass)
	if pw == "" {
		return "", fmt.Errorf("database is not configured")
	}
	image := m.mysqlImage(ctx)
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res := runutil.RunWithStdin(cctx, "", nil, strings.NewReader(stmt),
		"docker", "exec", "-i", "-e", "MYSQL_PWD="+pw, MySQLContainer, clientBin(image), "-uroot", "-N", "-B")
	if !res.OK {
		return "", fmt.Errorf("%s", res.Output)
	}
	return res.Output, nil
}

// ErrDatabaseExists is returned when the database or its user already exists.
var ErrDatabaseExists = fmt.Errorf("a database or user with this name already exists")

// CreateDatabase creates a database plus a user that can only use that database. It never
// touches anything that already exists: on a name clash it fails before changing anything,
// and when a later step fails it removes only what this call created.
func (m *Manager) CreateDatabase(ctx context.Context, dbName, dbUser, password string) error {
	if !ValidIdent(dbName) || !ValidIdent(dbUser) {
		return fmt.Errorf("invalid database or user name")
	}
	out, err := m.sql(ctx, fmt.Sprintf(
		"SELECT (SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = %s) + (SELECT COUNT(*) FROM mysql.user WHERE user = %s);",
		SQLString(dbName), SQLString(dbUser)))
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "0" {
		return ErrDatabaseExists
	}
	if _, err := m.sql(ctx, fmt.Sprintf("CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", SQLIdent(dbName))); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "exists") {
			return ErrDatabaseExists
		}
		return err
	}
	if err := m.createLimitedUser(ctx, dbUser, password); err != nil {
		_, _ = m.sql(context.Background(), "DROP DATABASE IF EXISTS "+SQLIdent(dbName)+";")
		if strings.Contains(strings.ToLower(err.Error()), "exists") {
			return ErrDatabaseExists
		}
		return err
	}
	if _, err := m.sql(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'; FLUSH PRIVILEGES;", GrantIdent(dbName), SQLString(dbUser))); err != nil {
		_, _ = m.sql(context.Background(), fmt.Sprintf("DROP USER IF EXISTS %s@'%%'; DROP DATABASE IF EXISTS %s;", SQLString(dbUser), SQLIdent(dbName)))
		return err
	}
	return nil
}

func (m *Manager) DropDatabase(ctx context.Context, dbName, dbUser string) error {
	if !ValidIdent(dbName) || !ValidIdent(dbUser) {
		return fmt.Errorf("invalid database or user name")
	}
	_, err := m.sql(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s;\nDROP USER IF EXISTS %s@'%%';\nFLUSH PRIVILEGES;\n", SQLIdent(dbName), SQLString(dbUser)))
	return err
}

func (m *Manager) SetDatabasePassword(ctx context.Context, dbUser, password string) error {
	if !ValidIdent(dbUser) {
		return fmt.Errorf("invalid user name")
	}
	_, err := m.sql(ctx, fmt.Sprintf("ALTER USER %s@'%%' IDENTIFIED BY %s;\nFLUSH PRIVILEGES;\n", SQLString(dbUser), SQLString(password)))
	return err
}

// DatabaseSizes returns the on-disk size in bytes per schema.
func (m *Manager) DatabaseSizes(ctx context.Context, names []string) map[string]int64 {
	key := strings.Join(names, ",")
	status.mu.Lock()
	if status.sizesK == key && time.Since(status.sizesAt) < sizesTTL {
		cp := make(map[string]int64, len(status.sizes))
		for k, v := range status.sizes {
			cp[k] = v
		}
		status.mu.Unlock()
		return cp
	}
	status.mu.Unlock()
	out := m.databaseSizesUncached(ctx, names)
	status.mu.Lock()
	status.sizesK, status.sizesAt, status.sizes = key, time.Now(), out
	status.mu.Unlock()
	cp := make(map[string]int64, len(out))
	for k, v := range out {
		cp[k] = v
	}
	return cp
}

func (m *Manager) databaseSizesUncached(ctx context.Context, names []string) map[string]int64 {
	out := map[string]int64{}
	if len(names) == 0 {
		return out
	}
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		if ValidIdent(n) {
			quoted = append(quoted, SQLString(n))
		}
	}
	if len(quoted) == 0 {
		return out
	}
	res, err := m.sql(ctx, "SELECT table_schema, COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema IN ("+strings.Join(quoted, ",")+") GROUP BY table_schema;")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(res), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 2 {
			n, _ := strconv.ParseInt(strings.TrimSpace(f[1]), 10, 64)
			out[f[0]] = n
		}
	}
	return out
}

// ExportDatabase streams a gzip-compressed dump into w.
func (m *Manager) ExportDatabase(ctx context.Context, dbName string, w io.Writer) error {
	if !ValidIdent(dbName) {
		return fmt.Errorf("invalid database name")
	}
	pw := m.DB.GetSetting(ctx, settingMySQLRootPass)
	image := m.mysqlImage(ctx)
	cmd := exec.CommandContext(ctx, "docker", "exec", "-e", "MYSQL_PWD="+pw, MySQLContainer, dumpBin(image),
		"-uroot", "--single-transaction", "--routines", "--triggers", "--default-character-set=utf8mb4", dbName)
	cmd.Env = os.Environ()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	gz := gzip.NewWriter(w)
	_, copyErr := io.Copy(gz, stdout)
	_ = gz.Close()
	waitErr := cmd.Wait()
	if copyErr != nil {
		return copyErr
	}
	if waitErr != nil {
		return fmt.Errorf("dump failed: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ImportDatabase loads a .sql or .sql.gz stream into dbName. The import runs as a
// short-lived user that can only touch that one database, so a hostile dump cannot
// read other databases or create privileged users.
func (m *Manager) ImportDatabase(ctx context.Context, dbName string, r io.Reader, gzipped bool) error {
	defer status.reset()
	if !ValidIdent(dbName) {
		return fmt.Errorf("invalid database name")
	}
	if gzipped {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("not a valid gzip file: %v", err)
		}
		defer zr.Close()
		r = zr
	}
	suffix := make([]byte, 5)
	_, _ = rand.Read(suffix)
	tmpUser := "imp_" + hex.EncodeToString(suffix)
	tmpPass := RandomPassword(24)
	setup := fmt.Sprintf("CREATE USER %s@'%%' IDENTIFIED BY %s;\nGRANT ALL PRIVILEGES ON %s.* TO %s@'%%';\n",
		SQLString(tmpUser), SQLString(tmpPass), GrantIdent(dbName), SQLString(tmpUser))
	if _, err := m.sql(ctx, setup); err != nil {
		return err
	}
	defer func() {
		_, _ = m.sql(context.Background(), fmt.Sprintf("DROP USER IF EXISTS %s@'%%';\n", SQLString(tmpUser)))
	}()
	cctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()
	res := runutil.RunWithStdin(cctx, "", nil, r,
		"docker", "exec", "-i", "-e", "MYSQL_PWD="+tmpPass, MySQLContainer, clientBin(m.mysqlImage(ctx)),
		"-u"+tmpUser, "--max-allowed-packet=256M", dbName)
	if !res.OK {
		return fmt.Errorf("import failed: %s", res.Output)
	}
	return nil
}

// StopContainerOnly stops the database container without changing the enabled flag.
func (m *Manager) StopContainerOnly(ctx context.Context) {
	_ = docker(ctx, "stop", MySQLContainer)
}

// CreateDatabaseOnly creates an empty database (no user). Users are created separately and
// granted access, as in CloudPanel.
func (m *Manager) CreateDatabaseOnly(ctx context.Context, dbName string) error {
	if !ValidIdent(dbName) {
		return fmt.Errorf("invalid database name")
	}
	out, err := m.sql(ctx, fmt.Sprintf("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = %s;", SQLString(dbName)))
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "0" {
		return ErrDatabaseExists
	}
	if _, err := m.sql(ctx, fmt.Sprintf("CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", SQLIdent(dbName))); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "exists") {
			return ErrDatabaseExists
		}
		return err
	}
	return nil
}

// DropDatabaseKeepUsers drops a database after removing the privileges of the given users on it
// (privileges on a dropped schema would otherwise come back if the name is reused).
func (m *Manager) DropDatabaseKeepUsers(ctx context.Context, dbName string, users []string) error {
	if !ValidIdent(dbName) {
		return fmt.Errorf("invalid database name")
	}
	for _, u := range users {
		if ValidIdent(u) {
			_ = m.RevokeAccess(ctx, dbName, u)
		}
	}
	_, err := m.sql(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s;", SQLIdent(dbName)))
	return err
}

// CreateDBUser creates a user that can log in but has no privileges until it is granted a database.
func (m *Manager) CreateDBUser(ctx context.Context, user, password string) error {
	if !ValidIdent(user) {
		return fmt.Errorf("invalid user name")
	}
	out, err := m.sql(ctx, fmt.Sprintf("SELECT COUNT(*) FROM mysql.user WHERE user = %s;", SQLString(user)))
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "0" {
		return ErrDatabaseExists
	}
	return m.createLimitedUser(ctx, user, password)
}

func (m *Manager) DropDBUser(ctx context.Context, user string) error {
	if !ValidIdent(user) {
		return fmt.Errorf("invalid user name")
	}
	_, err := m.sql(ctx, fmt.Sprintf("DROP USER IF EXISTS %s@'%%'; FLUSH PRIVILEGES;", SQLString(user)))
	return err
}

// GrantAccess sets the user's access to one database: "all" (read/write) or "read" (select only).
func (m *Manager) GrantAccess(ctx context.Context, dbName, user, access string) error {
	if !ValidIdent(dbName) || !ValidIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}
	var privs string
	switch access {
	case "all":
		privs = "ALL PRIVILEGES"
	case "read":
		privs = "SELECT, SHOW VIEW"
	default:
		return fmt.Errorf("access must be \"all\" or \"read\"")
	}
	_ = m.RevokeAccess(ctx, dbName, user) // switching read<->all must not leave the old grant behind
	_, err := m.sql(ctx, fmt.Sprintf("GRANT %s ON %s.* TO %s@'%%'; FLUSH PRIVILEGES;", privs, GrantIdent(dbName), SQLString(user)))
	return err
}

// RevokeAccess removes every privilege of the user on the database (no error if there was none).
func (m *Manager) RevokeAccess(ctx context.Context, dbName, user string) error {
	if !ValidIdent(dbName) || !ValidIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}
	// Older versions granted on the unescaped pattern; drop that wildcard grant too.
	_, _ = m.sql(ctx, fmt.Sprintf("REVOKE ALL PRIVILEGES ON %s.* FROM %s@'%%'; FLUSH PRIVILEGES;", SQLIdent(dbName), SQLString(user)))
	_, err := m.sql(ctx, fmt.Sprintf("REVOKE ALL PRIVILEGES ON %s.* FROM %s@'%%'; FLUSH PRIVILEGES;", GrantIdent(dbName), SQLString(user)))
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "no such grant") || strings.Contains(strings.ToLower(err.Error()), "there is no such grant")) {
		return nil
	}
	return err
}

// RepairGrants re-issues every database grant once with escaped names, removing the wildcard
// grants older versions created. It is safe to run again; a setting records that it finished.
func (m *Manager) RepairGrants(ctx context.Context) {
	const key = "php_grants_escaped"
	if m.DB.GetSetting(ctx, key) == "1" || !m.MySQLEnabled(ctx) || !m.MySQLStatus(ctx).Ready {
		return
	}
	sites, err := m.DB.ListPHPSites(ctx)
	if err != nil {
		return
	}
	failed := false
	for _, site := range sites {
		dbs, _ := m.DB.ListPHPDatabases(ctx, site.AppID)
		users, _ := m.DB.ListPHPDBUsers(ctx, site.AppID)
		grants, _ := m.DB.ListPHPGrants(ctx, site.AppID)
		name := map[int64]string{}
		for _, u := range users {
			name[u.ID] = u.UserName
		}
		byID := map[int64]string{}
		for _, d := range dbs {
			byID[d.ID] = d.DBName
			if d.DBUser != "" && ValidIdent(d.DBUser) {
				if err := m.GrantAccess(ctx, d.DBName, d.DBUser, "all"); err != nil {
					failed = true
				}
			}
		}
		for _, g := range grants {
			if u, ok := name[g.UserID]; ok && byID[g.DBID] != "" {
				if err := m.GrantAccess(ctx, byID[g.DBID], u, g.Access); err != nil {
					failed = true
				}
			}
		}
	}
	if !failed {
		_ = m.DB.SetSetting(ctx, key, "1")
	}
}

type dbSizeEntry struct {
	bytes int64
	at    time.Time
}

const dbSizeTTL = 90 * time.Second

// DatabaseBytes is the combined on-disk size (data + indexes) of the named databases. The
// database server is asked at most once every 90 seconds per set of names; when it cannot be
// reached the last known size (or 0) is returned.
func (m *Manager) DatabaseBytes(ctx context.Context, names []string) int64 {
	valid := make([]string, 0, len(names))
	for _, n := range names {
		if ValidIdent(n) {
			valid = append(valid, "'"+n+"'")
		}
	}
	if len(valid) == 0 {
		return 0
	}
	sort.Strings(valid)
	key := strings.Join(valid, ",")
	var last int64
	if v, ok := m.dbSizes.Load(key); ok {
		e := v.(dbSizeEntry)
		if time.Since(e.at) < dbSizeTTL {
			return e.bytes
		}
		last = e.bytes
	}
	out, err := m.sql(ctx, "SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables WHERE table_schema IN ("+key+");")
	if err != nil {
		return last
	}
	n, perr := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if perr != nil {
		return last
	}
	m.dbSizes.Store(key, dbSizeEntry{bytes: n, at: time.Now()})
	return n
}
