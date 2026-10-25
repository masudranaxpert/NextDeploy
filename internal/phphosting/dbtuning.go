package phphosting

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/mem"
)

// Guard rails for the shared database. One careless site (an unindexed search over a big table,
// a plugin that never closes its connections) must not be able to starve every other site.
//
//   - a runaway-query limit per site user (MariaDB aborts the statement; the panel's own root
//     connection, backups and imports through phpMyAdmin are not held to the same limit)
//   - idle connections are closed after five minutes, which frees their memory (the default is
//     eight hours); sixty seconds would break busy sites and long-running workers
//   - memory is split so the buffer pool cannot crowd out the per-connection buffers
const (
	settingMySQLQuerySeconds = "php_mysql_query_seconds"
	settingQueryLimitsDone   = "php_query_limits_v1"

	defaultQuerySeconds = 60  // a site user's statement is aborted after this long (0 = no limit)
	pmaQuerySeconds     = 300 // phpMyAdmin sessions run admin work (imports, ALTER TABLE) and get longer
	idleTimeoutSeconds  = 300

	dbMemoryMinMB = 512
	dbMemoryMaxMB = 4096
)

// SnapToBinaryRAM rounds memory up or down to the nearest standard binary RAM chunk (e.g. 1000 -> 1024, 2000 -> 2048, 4000 -> 4096).
func SnapToBinaryRAM(mb, hostMaxMB int) int {
	if mb < dbMemoryMinMB {
		mb = dbMemoryMinMB
	}
	switch mb {
	case 500:
		mb = 512
	case 1000:
		mb = 1024
	case 1500:
		mb = 1536
	case 2000:
		mb = 2048
	case 3000:
		mb = 3072
	case 4000:
		mb = 4096
	case 6000:
		mb = 6144
	case 8000:
		mb = 8192
	case 12000:
		mb = 12288
	case 16000:
		mb = 16384
	case 24000:
		mb = 24576
	case 32000:
		mb = 32768
	default:
		rem := mb % 128
		if rem != 0 {
			if rem >= 64 {
				mb += 128 - rem
			} else {
				mb -= rem
			}
		}
	}
	if hostMaxMB > 0 && mb > hostMaxMB {
		mb = (hostMaxMB / 128) * 128
		if mb < dbMemoryMinMB && hostMaxMB >= dbMemoryMinMB {
			mb = dbMemoryMinMB
		}
	}
	if mb < 256 {
		mb = 256
	}
	return mb
}

// DBMemoryDefaultMB is the database container's memory when nobody chose one: a quarter of the
// host's RAM (rounded to 128 MB), but not below 512 MB (small servers) or above 4 GB.
func DBMemoryDefaultMB(hostRAMMB int) int {
	n := (hostRAMMB / 4) / 128 * 128
	if n < dbMemoryMinMB {
		return dbMemoryMinMB
	}
	if n > dbMemoryMaxMB {
		return dbMemoryMaxMB
	}
	return n
}

func hostRAMMB() int {
	if v, err := mem.VirtualMemory(); err == nil && v != nil {
		return int(v.Total / (1024 * 1024))
	}
	return 0
}

// DBTuning is how the container's memory is divided.
type DBTuning struct {
	BufferPoolMB   int // InnoDB cache: half of the container, the rest is for connections and the server itself
	MaxConnections int // each connection can take a few MB for sorting and joining, so this follows memory
	TmpTableMB     int // in-memory temporary tables are capped so one query cannot eat the container
}

func TuneDB(memMB int) DBTuning {
	conns := memMB / 8
	if conns < 100 {
		conns = 100
	}
	if conns > 500 {
		conns = 500
	}
	tmp := memMB / 16
	if tmp < 16 {
		tmp = 16
	}
	if tmp > 64 {
		tmp = 64
	}
	return DBTuning{BufferPoolMB: memMB / 2, MaxConnections: conns, TmpTableMB: tmp}
}

// serverArgs are the database server flags for this tuning.
func (t DBTuning) serverArgs() []string {
	return []string{
		fmt.Sprintf("--innodb-buffer-pool-size=%dM", t.BufferPoolMB),
		fmt.Sprintf("--max-connections=%d", t.MaxConnections),
		fmt.Sprintf("--tmp-table-size=%dM", t.TmpTableMB),
		fmt.Sprintf("--max-heap-table-size=%dM", t.TmpTableMB),
		fmt.Sprintf("--wait-timeout=%d", idleTimeoutSeconds),
		fmt.Sprintf("--interactive-timeout=%d", idleTimeoutSeconds),
	}
}

// userLimitClause is the WITH clause for a site database user. The statement-time limit exists
// only on MariaDB (MySQL can limit nothing but SELECT, and only for everybody, root included).
func userLimitClause(image string, maxConnections, querySeconds int) string {
	s := fmt.Sprintf("WITH MAX_USER_CONNECTIONS %d", maxConnections)
	if isMariaDB(image) {
		s += fmt.Sprintf(" MAX_STATEMENT_TIME %d", querySeconds)
	}
	return s
}

// querySeconds is the runaway-query limit for site users; the admin can change it by setting
// php_mysql_query_seconds (0 turns the limit off).
func (m *Manager) querySeconds(ctx context.Context) int {
	if n, err := strconv.Atoi(strings.TrimSpace(m.DB.GetSetting(ctx, settingMySQLQuerySeconds))); err == nil && n >= 0 {
		return n
	}
	return defaultQuerySeconds
}

// sqlPreferring runs a statement written with the newer limit options and, if the server does not
// know them (an old image the admin chose), runs the plain version instead of failing.
func (m *Manager) sqlPreferring(ctx context.Context, preferred, plain string) (string, error) {
	out, err := m.sql(ctx, preferred)
	if err == nil || preferred == plain {
		return out, err
	}
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "syntax") || strings.Contains(low, "max_statement_time") {
		log.Printf("php database: server does not support the query limit, creating the user without it")
		return m.sql(ctx, plain)
	}
	return out, err
}

// createLimitedUser creates a site database user with its connection and query limits.
func (m *Manager) createLimitedUser(ctx context.Context, user, password string) error {
	image := m.mysqlImage(ctx)
	base := fmt.Sprintf("CREATE USER %s@'%%' IDENTIFIED BY %s ", SQLString(user), SQLString(password))
	_, err := m.sqlPreferring(ctx,
		base+userLimitClause(image, 25, m.querySeconds(ctx))+";",
		base+"WITH MAX_USER_CONNECTIONS 25;")
	return err
}

// ApplyServerTuning sets the dynamic guard rails on the running server, so servers created before
// they existed get them too without a restart (restarts re-read the container's own flags).
func (m *Manager) ApplyServerTuning(ctx context.Context) {
	if !m.MySQLEnabled(ctx) || !m.MySQLStatus(ctx).Ready {
		return
	}
	t := TuneDB(m.mysqlMemoryMB(ctx))
	stmt := fmt.Sprintf("SET GLOBAL wait_timeout=%d, interactive_timeout=%d, max_connections=%d, tmp_table_size=%d, max_heap_table_size=%d;",
		idleTimeoutSeconds, idleTimeoutSeconds, t.MaxConnections, t.TmpTableMB<<20, t.TmpTableMB<<20)
	if _, err := m.sql(ctx, stmt); err != nil {
		log.Printf("php database: applying server limits: %v", err)
	}
}

// ApplyQueryLimits gives the site users that already exist the runaway-query limit (once; a new
// user gets it when it is created). Failures are logged and retried at the next start.
func (m *Manager) ApplyQueryLimits(ctx context.Context) {
	image := m.mysqlImage(ctx)
	if !isMariaDB(image) || m.DB.GetSetting(ctx, settingQueryLimitsDone) == "1" || !m.MySQLEnabled(ctx) || !m.MySQLStatus(ctx).Ready {
		return
	}
	sites, err := m.DB.ListPHPSites(ctx)
	if err != nil {
		return
	}
	users := map[string]bool{}
	for _, s := range sites {
		dbs, _ := m.DB.ListPHPDatabases(ctx, s.AppID)
		for _, d := range dbs {
			if d.DBUser != "" && ValidIdent(d.DBUser) {
				users[d.DBUser] = true
			}
		}
		list, _ := m.DB.ListPHPDBUsers(ctx, s.AppID)
		for _, u := range list {
			if ValidIdent(u.UserName) {
				users[u.UserName] = true
			}
		}
	}
	secs := m.querySeconds(ctx)
	failed := false
	for u := range users {
		if _, err := m.sql(ctx, fmt.Sprintf("ALTER USER %s@'%%' %s;", SQLString(u), userLimitClause(image, 25, secs))); err != nil {
			log.Printf("php database: query limit for %s: %v", u, err)
			failed = true
		}
	}
	if !failed {
		_ = m.DB.SetSetting(ctx, settingQueryLimitsDone, "1")
	}
}

// SetQuerySeconds changes the runaway-query limit and applies it to every existing site user.
func (m *Manager) SetQuerySeconds(ctx context.Context, seconds int) error {
	if seconds < 0 || seconds > 3600 {
		return fmt.Errorf("the limit must be between 0 (off) and 3600 seconds")
	}
	if err := m.DB.SetSetting(ctx, settingMySQLQuerySeconds, strconv.Itoa(seconds)); err != nil {
		return err
	}
	if !isMariaDB(m.mysqlImage(ctx)) {
		return fmt.Errorf("only MariaDB can limit how long one user's query runs")
	}
	if err := m.DB.SetSetting(ctx, settingQueryLimitsDone, "0"); err != nil {
		return err
	}
	m.ApplyQueryLimits(ctx)
	if m.DB.GetSetting(ctx, settingQueryLimitsDone) != "1" {
		return fmt.Errorf("saved, but the database server could not apply it to every user; it is retried at the next start")
	}
	status.reset()
	return nil
}
