package handlers

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/phphosting"
	"panel/internal/quota"

	"github.com/gofiber/fiber/v2"
)

// ---- databases ----

func validDBInput(s string) bool {
	if len(s) < 1 || len(s) > 24 {
		return false
	}
	if s[0] < 'a' || s[0] > 'z' { // a leading digit or underscore makes the name a wider pattern
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// dbPrefix namespaces every database and user of one owner: u<ownerid>_<name>.
func dbPrefix(owner db.User) string { return fmt.Sprintf("u%d_", owner.ID) }

// POST /apps/:id/php/databases {"name":"shop"} — creates an empty database; users are added separately.
func (p *Panel) PHPDatabaseCreate(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	body.Name = strings.ToLower(strings.TrimSpace(body.Name))
	if !validDBInput(body.Name) {
		return phpJSONErr(c, 400, "names start with a letter and may contain lowercase letters, numbers and underscores (max 24 characters)")
	}
	if !p.PHP.MySQLStatus(ctx).Ready {
		return phpJSONErr(c, 400, "the database server is not running. Ask an administrator to enable it in PHP Hosting → Instance.")
	}
	owner, err := p.phpOwner(ctx, site)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := quota.CheckDatabaseSlot(ctx, p.DB, owner); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	if err := p.CheckStorageRoom(ctx, owner, 0); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	dbName := dbPrefix(owner) + body.Name
	if err := p.PHP.CreateDatabaseOnly(ctx, dbName); err != nil {
		if errors.Is(err, phphosting.ErrDatabaseExists) {
			return phpJSONErr(c, 409, err.Error())
		}
		return phpJSONErr(c, 500, err.Error())
	}
	id, err := p.DB.CreatePHPDatabase(ctx, db.PHPDatabase{AppID: site.AppID, DBName: dbName})
	if err != nil {
		_ = p.PHP.DropDatabaseKeepUsers(ctx, dbName, nil) // nothing refers to it
		return phpJSONErr(c, 409, phphosting.ErrDatabaseExists.Error())
	}
	p.RecordAuditLog(c, "php_db_create", "app", site.AppID, "Created database "+dbName)
	return c.JSON(fiber.Map{"ok": true, "id": id, "db_name": dbName, "host": phphosting.MySQLContainer, "port": 3306})
}

func (p *Panel) phpDatabaseFor(c *fiber.Ctx, site db.PHPSite) (db.PHPDatabase, bool) {
	id, _ := strconv.ParseInt(c.Params("dbid"), 10, 64)
	d, err := p.DB.GetPHPDatabase(c.UserContext(), site.AppID, id)
	if err != nil {
		_ = phpJSONErr(c, 404, "database not found")
		return db.PHPDatabase{}, false
	}
	return d, true
}

// POST /apps/:id/php/databases/:dbid/delete — drops the database; its users stay (without access)
func (p *Panel) PHPDatabaseDelete(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	d, ok := p.phpDatabaseFor(c, site)
	if !ok {
		return nil
	}
	ctx := c.UserContext()
	users, _ := p.DB.ListPHPDBUsers(ctx, site.AppID)
	grants, _ := p.DB.ListPHPGrants(ctx, site.AppID)
	names := map[int64]string{}
	for _, u := range users {
		names[u.ID] = u.UserName
	}
	var granted []string
	for _, g := range grants {
		if g.DBID == d.ID {
			granted = append(granted, names[g.UserID])
		}
	}
	if d.DBUser != "" { // legacy row created with its own user
		granted = append(granted, d.DBUser)
	}
	if err := p.PHP.DropDatabaseKeepUsers(ctx, d.DBName, granted); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := p.DB.DeletePHPDatabase(ctx, d.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.RecordAuditLog(c, "php_db_delete", "app", site.AppID, "Deleted database "+d.DBName)
	return c.JSON(fiber.Map{"ok": true})
}

type phpGrantIn struct {
	DBID   int64  `json:"db_id"`
	Access string `json:"access"` // all | read
}

// resolveGrants validates that every grant targets a database of this site.
func (p *Panel) resolveGrants(ctx context.Context, site db.PHPSite, in []phpGrantIn) ([]db.PHPDBGrant, map[int64]string, error) {
	dbs, _ := p.DB.ListPHPDatabases(ctx, site.AppID)
	byID := map[int64]string{}
	for _, d := range dbs {
		byID[d.ID] = d.DBName
	}
	out := make([]db.PHPDBGrant, 0, len(in))
	seen := map[int64]bool{}
	for _, g := range in {
		if _, ok := byID[g.DBID]; !ok {
			return nil, nil, fmt.Errorf("database %d does not belong to this site", g.DBID)
		}
		if g.Access != "all" && g.Access != "read" {
			return nil, nil, fmt.Errorf("access must be \"all\" or \"read\"")
		}
		if seen[g.DBID] {
			continue
		}
		seen[g.DBID] = true
		out = append(out, db.PHPDBGrant{DBID: g.DBID, Access: g.Access})
	}
	return out, byID, nil
}

// applyGrants makes MariaDB match the wanted grants of one user.
func (p *Panel) applyGrants(ctx context.Context, user string, want []db.PHPDBGrant, dbNames map[int64]string, before []db.PHPDBGrant) error {
	wantSet := map[int64]string{}
	for _, g := range want {
		wantSet[g.DBID] = g.Access
	}
	for _, g := range before {
		if _, keep := wantSet[g.DBID]; !keep {
			if err := p.PHP.RevokeAccess(ctx, dbNames[g.DBID], user); err != nil {
				return err
			}
		}
	}
	for _, g := range want {
		if err := p.PHP.GrantAccess(ctx, dbNames[g.DBID], user, g.Access); err != nil {
			return err
		}
	}
	return nil
}

// POST /apps/:id/php/db-users {"name":"shop","password":"","grants":[{"db_id":1,"access":"all"}]}
func (p *Panel) PHPDBUserCreate(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	var body struct {
		Name     string       `json:"name"`
		Password string       `json:"password"`
		Grants   []phpGrantIn `json:"grants"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	body.Name = strings.ToLower(strings.TrimSpace(body.Name))
	if !validDBInput(body.Name) {
		return phpJSONErr(c, 400, "names start with a letter and may contain lowercase letters, numbers and underscores (max 24 characters)")
	}
	if !p.PHP.MySQLStatus(ctx).Ready {
		return phpJSONErr(c, 400, "the database server is not running. Ask an administrator to enable it in PHP Hosting → Instance.")
	}
	owner, err := p.phpOwner(ctx, site)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if owner.Role != db.RoleAdmin {
		if n, _ := p.DB.CountPHPDBUsersForOwner(ctx, owner.ID); n >= maxDBUsersPerOwner {
			return phpJSONErr(c, 400, fmt.Sprintf("database user limit reached (%d)", maxDBUsersPerOwner))
		}
	}
	grants, dbNames, err := p.resolveGrants(ctx, site, body.Grants)
	if err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	password := body.Password
	if password == "" {
		password = phphosting.RandomPassword(24)
	} else if len(password) < 10 {
		return phpJSONErr(c, 400, "password must be at least 10 characters")
	}
	user := dbPrefix(owner) + body.Name
	if err := p.PHP.CreateDBUser(ctx, user, password); err != nil {
		if errors.Is(err, phphosting.ErrDatabaseExists) {
			return phpJSONErr(c, 409, "a database user with this name already exists")
		}
		return phpJSONErr(c, 500, err.Error())
	}
	uid, err := p.DB.CreatePHPDBUser(ctx, site.AppID, user)
	if err != nil {
		_ = p.PHP.DropDBUser(ctx, user)
		return phpJSONErr(c, 409, "a database user with this name already exists")
	}
	if err := p.applyGrants(ctx, user, grants, dbNames, nil); err != nil {
		_ = p.PHP.DropDBUser(ctx, user)
		_ = p.DB.DeletePHPDBUser(ctx, uid)
		return phpJSONErr(c, 500, err.Error())
	}
	_ = p.DB.ReplacePHPUserGrants(ctx, uid, grants)
	p.RecordAuditLog(c, "php_dbuser_create", "app", site.AppID, "Created database user "+user)
	return c.JSON(fiber.Map{"ok": true, "id": uid, "user": user, "password": password, "host": phphosting.MySQLContainer, "port": 3306})
}

const maxDBUsersPerOwner = 25

func (p *Panel) phpDBUserFor(c *fiber.Ctx, site db.PHPSite) (db.PHPDBUser, bool) {
	id, _ := strconv.ParseInt(c.Params("uid"), 10, 64)
	u, err := p.DB.GetPHPDBUser(c.UserContext(), site.AppID, id)
	if err != nil {
		_ = phpJSONErr(c, 404, "database user not found")
		return db.PHPDBUser{}, false
	}
	return u, true
}

// POST /apps/:id/php/db-users/:uid/grants {"grants":[{"db_id":1,"access":"read"}]} — replaces the user's access
func (p *Panel) PHPDBUserGrants(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	u, ok := p.phpDBUserFor(c, site)
	if !ok {
		return nil
	}
	var body struct {
		Grants []phpGrantIn `json:"grants"`
	}
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	grants, dbNames, err := p.resolveGrants(ctx, site, body.Grants)
	if err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	all, _ := p.DB.ListPHPGrants(ctx, site.AppID)
	var before []db.PHPDBGrant
	for _, g := range all {
		if g.UserID == u.ID {
			before = append(before, g)
		}
	}
	if err := p.applyGrants(ctx, u.UserName, grants, dbNames, before); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := p.DB.ReplacePHPUserGrants(ctx, u.ID, grants); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /apps/:id/php/db-users/:uid/password {"password":""}
func (p *Panel) PHPDBUserPassword(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	u, ok := p.phpDBUserFor(c, site)
	if !ok {
		return nil
	}
	var body struct {
		Password string `json:"password"`
	}
	_ = c.BodyParser(&body)
	pw := body.Password
	if pw == "" {
		pw = phphosting.RandomPassword(24)
	} else if len(pw) < 10 {
		return phpJSONErr(c, 400, "password must be at least 10 characters")
	}
	if err := p.PHP.SetDatabasePassword(c.UserContext(), u.UserName, pw); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "password": pw})
}

// POST /apps/:id/php/db-users/:uid/delete
func (p *Panel) PHPDBUserDelete(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	u, ok := p.phpDBUserFor(c, site)
	if !ok {
		return nil
	}
	if err := p.PHP.DropDBUser(c.UserContext(), u.UserName); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := p.DB.DeletePHPDBUser(c.UserContext(), u.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	p.RecordAuditLog(c, "php_dbuser_delete", "app", site.AppID, "Deleted database user "+u.UserName)
	return c.JSON(fiber.Map{"ok": true})
}

// GET /apps/:id/php/databases/:dbid/export — gzip-compressed dump (developer access required)
func (p *Panel) PHPDatabaseExport(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	if u, ok := currentUser(c); ok {
		if allowed, _ := p.CanAccessApp(c.UserContext(), u.ID, u.Role, site.AppID, db.CollabRoleDeveloper); !allowed {
			return phpJSONErr(c, 403, "forbidden")
		}
	}
	d, ok := p.phpDatabaseFor(c, site)
	if !ok {
		return nil
	}
	c.Set("Content-Type", "application/gzip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.sql.gz"`, d.DBName, time.Now().Format("20060102-150405")))
	pr, pw := io.Pipe()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		pw.CloseWithError(p.PHP.ExportDatabase(ctx, d.DBName, pw))
	}()
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		_, _ = io.Copy(w, pr)
		_ = pr.Close()
	})
	return nil
}

// POST /apps/:id/php/databases/:dbid/import (multipart field "file": .sql or .sql.gz, or raw stream)
func (p *Panel) PHPDatabaseImport(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	d, ok := p.phpDatabaseFor(c, site)
	if !ok {
		return nil
	}

	var r io.Reader
	var gz bool
	fh, err := c.FormFile("file")
	if err == nil && fh != nil {
		f, ferr := fh.Open()
		if ferr != nil {
			return phpJSONErr(c, 400, ferr.Error())
		}
		defer f.Close()
		r = f
		gz = strings.HasSuffix(strings.ToLower(fh.Filename), ".gz")
	} else if fh2, err2 := c.FormFile("sql"); err2 == nil && fh2 != nil {
		f, ferr := fh2.Open()
		if ferr != nil {
			return phpJSONErr(c, 400, ferr.Error())
		}
		defer f.Close()
		r = f
		gz = strings.HasSuffix(strings.ToLower(fh2.Filename), ".gz")
	} else {
		body := c.Body()
		if len(body) == 0 {
			return phpJSONErr(c, 400, "missing file or empty request body")
		}
		if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
			gz = true
		}
		r = bytes.NewReader(body)
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Minute)
	defer cancel()
	if err := p.PHP.ImportDatabase(ctx, d.DBName, r, gz); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	p.RecordAuditLog(c, "php_db_import", "app", site.AppID, "Imported into "+d.DBName)
	return c.JSON(fiber.Map{"ok": true, "message": "Imported database successfully"})
}

// ---- cron ----

type phpCronBody struct {
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  *bool  `json:"enabled"`
}

func (p *Panel) phpCronValidate(ctx context.Context, owner db.User, b phpCronBody) error {
	min := p.PHP.MinCronIntervalMinutes(ctx)
	if owner.Role == db.RoleAdmin {
		min = 1
	}
	if err := phphosting.ValidateSchedule(b.Schedule, min); err != nil {
		return err
	}
	return phphosting.ValidateCommand(b.Command)
}

// POST /apps/:id/php/cron
func (p *Panel) PHPCronCreate(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	if site.SiteType == phphosting.SiteTypeStatic {
		return phpJSONErr(c, 400, "static sites cannot run cron jobs")
	}
	var body phpCronBody
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	owner, err := p.phpOwner(ctx, site)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := quota.CheckCronSlot(ctx, p.DB, owner); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	if err := p.phpCronValidate(ctx, owner, body); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	enabled := body.Enabled == nil || *body.Enabled
	id, err := p.DB.CreatePHPCronJob(ctx, db.PHPCronJob{AppID: site.AppID, Schedule: strings.TrimSpace(body.Schedule), Command: strings.TrimSpace(body.Command), Enabled: enabled})
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "id": id})
}

func (p *Panel) phpCronFor(c *fiber.Ctx, site db.PHPSite) (db.PHPCronJob, bool) {
	id, _ := strconv.ParseInt(c.Params("jid"), 10, 64)
	j, err := p.DB.GetPHPCronJob(c.UserContext(), site.AppID, id)
	if err != nil {
		_ = phpJSONErr(c, 404, "cron job not found")
		return db.PHPCronJob{}, false
	}
	return j, true
}

// POST /apps/:id/php/cron/:jid
func (p *Panel) PHPCronUpdate(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	j, ok := p.phpCronFor(c, site)
	if !ok {
		return nil
	}
	var body phpCronBody
	if err := c.BodyParser(&body); err != nil {
		return phpJSONErr(c, 400, "invalid body")
	}
	if body.Schedule == "" {
		body.Schedule = j.Schedule
	}
	if body.Command == "" {
		body.Command = j.Command
	}
	owner, err := p.phpOwner(ctx, site)
	if err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	if err := p.phpCronValidate(ctx, owner, body); err != nil {
		return phpJSONErr(c, 400, err.Error())
	}
	j.Schedule, j.Command = strings.TrimSpace(body.Schedule), strings.TrimSpace(body.Command)
	if body.Enabled != nil {
		j.Enabled = *body.Enabled
	}
	if err := p.DB.UpdatePHPCronJob(ctx, j); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /apps/:id/php/cron/:jid/delete
func (p *Panel) PHPCronDelete(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	j, ok := p.phpCronFor(c, site)
	if !ok {
		return nil
	}
	if err := p.DB.DeletePHPCronJob(c.UserContext(), site.AppID, j.ID); err != nil {
		return phpJSONErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// POST /apps/:id/php/cron/:jid/run — run now and return the output
func (p *Panel) PHPCronRun(c *fiber.Ctx) error {
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	j, ok := p.phpCronFor(c, site)
	if !ok {
		return nil
	}
	status, out, err := p.PHP.TryRunCronJobNow(c.UserContext(), j)
	if err != nil {
		return phpJSONErr(c, 409, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "status": status, "output": out})
}

// ---- logs ----

// GET /apps/:id/php/logs?source=php|runtime&lines=200
func (p *Panel) PHPLogs(c *fiber.Ctx) error {
	ctx := c.UserContext()
	_, site, err := p.phpSiteFor(c)
	if site.AppID == "" {
		return err
	}
	lines, _ := strconv.Atoi(c.Query("lines", "200"))
	if lines < 10 {
		lines = 10
	}
	if lines > 5000 {
		lines = 5000
	}
	source := c.Query("source", "php")
	switch source {
	case "php":
		path := filepath.Join(p.PHP.SitePath(site.AppID), "logs", "php-error.log")
		return c.JSON(fiber.Map{"ok": true, "source": source, "text": tailFile(path, lines)})
	case "runtime":
		// This container serves every site of the owner, so only the owner (or an admin) may read it.
		if u, ok := currentUser(c); !ok || (u.Role != db.RoleAdmin && u.ID != site.OwnerID) {
			return phpJSONErr(c, 403, "only the site owner can read the runtime output")
		}
		if site.SiteType == phphosting.SiteTypeStatic {
			return c.JSON(fiber.Map{"ok": true, "source": source, "text": ""})
		}
		res := p.PHP.ContainerLogs(ctx, phphosting.RuntimeContainer(site.OwnerID, site.PHPVersion), lines)
		return c.JSON(fiber.Map{"ok": true, "source": source, "text": res})
	}
	return phpJSONErr(c, 400, "unknown log source")
}

// tailFile returns the last n lines of a file (reading at most the final 1 MiB).
func tailFile(path string, n int) string {
	// The log lives in a folder the site's own user can write to, so it may have been replaced by a link.
	if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	const maxRead = 1 << 20
	off := int64(0)
	if st.Size() > maxRead {
		off = st.Size() - maxRead
	}
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return ""
	}
	text := strings.TrimRight(string(buf), "\n")
	parts := strings.Split(text, "\n")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "\n")
}
