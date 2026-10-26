package cmd

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"nd/internal/client"
	"nd/internal/ui"
)

// RunPHP handles all PHP hosting operations under 'nd php'.
func RunPHP(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printPHPHelp()
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		return runPHPList(cl, rest)
	case "create", "new":
		return runPHPCreate(cl, rest)
	case "info", "status":
		return runPHPInfo(cl, rest)
	case "set", "config":
		return runPHPSet(cl, rest)
	case "restart":
		return runPHPRestart(cl, rest)
	case "fix-perms", "perms":
		return runPHPFixPerms(cl, rest)
	case "pma", "phpmyadmin":
		return runPHPPMA(cl, rest)
	case "db":
		return runPHPDB(cl, rest)
	case "cron":
		return runPHPCron(cl, rest)
	default:
		return fmt.Errorf("unknown php command %q. Run 'nd php --help' for usage", sub)
	}
}

func printPHPHelp() {
	fmt.Println("Usage: nd php <command> [args...]")
	fmt.Println("\nManage PHP hosting, WordPress sites, MariaDB databases, and crons.")
	fmt.Println("\nCommands:")
	fmt.Println("  list, ls                     List all PHP and WordPress sites")
	fmt.Println("  create <name> [flags]        Create a new PHP or WordPress site")
	fmt.Println("  info [app_id]                Show PHP site configuration and status")
	fmt.Println("  set [app_id] [flags]         Configure PHP version, memory, and limits")
	fmt.Println("  restart [app_id]             Restart PHP runtime container for site")
	fmt.Println("  fix-perms [app_id]           Fix file ownership and permissions in htdocs")
	fmt.Println("  pma [app_id]                 Open phpMyAdmin in browser with one-click SSO")
	fmt.Println("  db <subcommand>              Manage MariaDB databases and database users")
	fmt.Println("  cron <subcommand>            Manage PHP scheduled cron jobs")
}

func runPHPList(cl *client.Client, args []string) error {
	data, status, err := cl.Do("GET", "/api/v1/php/sites", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(data))
	}

	var sites []struct {
		AppID      string   `json:"app_id"`
		Name       string   `json:"name"`
		SiteType   string   `json:"site_type"`
		PHPVersion string   `json:"php_version"`
		RootDir    string   `json:"root_dir"`
		Domains    []string `json:"domains"`
		Status     string   `json:"status"`
	}
	if err := json.Unmarshal(data, &sites); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(sites) == 0 {
		fmt.Println("No PHP sites found. Create one with: nd php create <name>")
		return nil
	}

	fmt.Printf("%-18s %-20s %-10s %-8s %-28s %-10s\n", "APP ID", "NAME", "TYPE", "PHP", "DOMAINS", "STATUS")
	fmt.Println(strings.Repeat("─", 100))
	for _, s := range sites {
		domStr := strings.Join(s.Domains, ", ")
		if domStr == "" {
			domStr = "-"
		}
		phpVer := s.PHPVersion
		if phpVer == "" {
			phpVer = "-"
		}
		fmt.Printf("%-18s %-20s %-10s %-8s %-28s %-10s\n", s.AppID, s.Name, s.SiteType, phpVer, domStr, s.Status)
	}
	return nil
}

func runPHPCreate(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nd php create <name> [--version 8.3] [--type php|wordpress|static] [--title ...] [--admin-user ...] [--admin-email ...]")
	}

	name := args[0]
	version := "8.3"
	siteType := "php"
	title := ""
	adminUser := ""
	adminEmail := ""

	for i := 1; i < len(args); i++ {
		a := args[i]
		if (a == "-v" || a == "--version") && i+1 < len(args) {
			version = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--version=") {
			version = strings.TrimPrefix(a, "--version=")
		} else if (a == "-t" || a == "--type") && i+1 < len(args) {
			siteType = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--type=") {
			siteType = strings.TrimPrefix(a, "--type=")
		} else if a == "--title" && i+1 < len(args) {
			title = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--title=") {
			title = strings.TrimPrefix(a, "--title=")
		} else if a == "--admin-user" && i+1 < len(args) {
			adminUser = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--admin-user=") {
			adminUser = strings.TrimPrefix(a, "--admin-user=")
		} else if a == "--admin-email" && i+1 < len(args) {
			adminEmail = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--admin-email=") {
			adminEmail = strings.TrimPrefix(a, "--admin-email=")
		}
	}

	payload := map[string]string{
		"name":           name,
		"php_version":    version,
		"type":           siteType,
		"title":          title,
		"wp_admin_user":  adminUser,
		"wp_admin_email": adminEmail,
	}

	data, status, err := cl.Do("POST", "/api/v1/php/sites", payload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to create PHP site: %s", client.JSONError(data))
	}

	var resp struct {
		AppID string `json:"app_id"`
		Name  string `json:"name"`
	}
	_ = json.Unmarshal(data, &resp)

	ui.Success("PHP site %s (%s) created successfully", ui.Bold(name), ui.Cyan(resp.AppID))
	fmt.Printf("Files root: htdocs/\nOpen shell: nd shell %s\nUpload code: nd push %s\n", resp.AppID, resp.AppID)
	return nil
}

func runPHPInfo(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}

	data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var info struct {
		AppID      string                 `json:"app_id"`
		Name       string                 `json:"name"`
		SiteType   string                 `json:"site_type"`
		PHPVersion string                 `json:"php_version"`
		RootDir    string                 `json:"root_dir"`
		Domains    []string               `json:"domains"`
		Status     string                 `json:"status"`
		Settings   map[string]interface{} `json:"settings"`
		Databases  []interface{}          `json:"databases"`
		DBUsers    []interface{}          `json:"db_users"`
		Crons      []interface{}          `json:"crons"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Println(ui.Bold("PHP Site: ") + ui.Cyan(info.Name) + " (" + info.AppID + ")")
	fmt.Printf("  Type:         %s\n", info.SiteType)
	fmt.Printf("  PHP Version:  %s\n", info.PHPVersion)
	fmt.Printf("  Root Dir:     htdocs/%s\n", info.RootDir)
	fmt.Printf("  Status:       %s\n", info.Status)
	fmt.Printf("  Domains:      %s\n", strings.Join(info.Domains, ", "))
	fmt.Printf("  Databases:    %d database(s), %d user(s)\n", len(info.Databases), len(info.DBUsers))
	fmt.Printf("  Crons:        %d scheduled job(s)\n", len(info.Crons))

	if len(info.Settings) > 0 {
		fmt.Println("\nConfiguration:")
		if mem, ok := info.Settings["memory_limit"]; ok {
			fmt.Printf("  memory_limit:        %v\n", mem)
		}
		if up, ok := info.Settings["upload_max_filesize"]; ok {
			fmt.Printf("  upload_max_filesize: %v\n", up)
		}
		if ex, ok := info.Settings["max_execution_time"]; ok {
			fmt.Printf("  max_execution_time:  %v\n", ex)
		}
	}
	return nil
}

func runPHPSet(cl *client.Client, args []string) error {
	appID, rest, err := resolveAppID(args)
	if err != nil {
		return err
	}

	settings := make(map[string]interface{})
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "--memory-limit" && i+1 < len(rest) {
			settings["memory_limit"] = rest[i+1]
			i++
		} else if strings.HasPrefix(a, "--memory-limit=") {
			settings["memory_limit"] = strings.TrimPrefix(a, "--memory-limit=")
		} else if a == "--upload-max" && i+1 < len(rest) {
			settings["upload_max_filesize"] = rest[i+1]
			i++
		} else if strings.HasPrefix(a, "--upload-max=") {
			settings["upload_max_filesize"] = strings.TrimPrefix(a, "--upload-max=")
		} else if a == "--max-execution" && i+1 < len(rest) {
			if n, err := strconv.Atoi(rest[i+1]); err == nil {
				settings["max_execution_time"] = n
			}
			i++
		} else if strings.HasPrefix(a, "--max-execution=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(a, "--max-execution=")); err == nil {
				settings["max_execution_time"] = n
			}
		} else if a == "--root-dir" && i+1 < len(rest) {
			settings["root_dir"] = rest[i+1]
			i++
		} else if strings.HasPrefix(a, "--root-dir=") {
			settings["root_dir"] = strings.TrimPrefix(a, "--root-dir=")
		} else if a == "--version" && i+1 < len(rest) {
			settings["php_version"] = rest[i+1]
			i++
		} else if strings.HasPrefix(a, "--version=") {
			settings["php_version"] = strings.TrimPrefix(a, "--version=")
		}
	}

	if len(settings) == 0 {
		return fmt.Errorf("no settings specified. Flags: --memory-limit, --upload-max, --max-execution, --version, --root-dir")
	}

	data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/settings", settings)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("update failed: %s", client.JSONError(data))
	}
	ui.Success("Updated settings for PHP site %s", appID)
	return nil
}

func runPHPRestart(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}
	data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/restart", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("restart failed: %s", client.JSONError(data))
	}
	ui.Success("Restarted PHP runtime container for %s", appID)
	return nil
}

func runPHPFixPerms(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}
	data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/fix-permissions", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("fix permissions failed: %s", client.JSONError(data))
	}
	ui.Success("Fixed file permissions for PHP site %s", appID)
	return nil
}

func runPHPPMA(cl *client.Client, args []string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}

	data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID+"/pma-url", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to open phpMyAdmin: %s", client.JSONError(data))
	}

	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.URL == "" {
		return fmt.Errorf("invalid phpMyAdmin URL returned")
	}

	fmt.Println("phpMyAdmin SSO URL: " + resp.URL)
	_ = openBrowser(resp.URL)
	ui.Success("Opened phpMyAdmin for site %s in browser", appID)
	return nil
}


// ─── Database Operations (nd php db ...) ───────────────────────────────────

func runPHPDB(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: nd php db <subcommand> [args...]")
		fmt.Println("\nManage MariaDB databases and users:")
		fmt.Println("  list [app_id]                           List databases for site")
		fmt.Println("  create [app_id] <dbname>                Create a new database")
		fmt.Println("  rm [app_id] <dbid>                      Delete a database")
		fmt.Println("  export [app_id] <dbid> [file.sql]       Export SQL dump of database")
		fmt.Println("  import [app_id] <dbid> <file.sql>       Import SQL dump into database")
		fmt.Println("  user list [app_id]                      List database users")
		fmt.Println("  user create [app_id] <user> [password]  Create database user")
		fmt.Println("  user rm [app_id] <uid>                  Delete database user")
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		appID, _, err := resolveAppID(rest)
		if err != nil {
			return err
		}
		data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID+"/databases", nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("error: %s", client.JSONError(data))
		}
		var dbs []struct {
			ID     int64  `json:"id"`
			DBName string `json:"db_name"`
			DBUser string `json:"db_user"`
		}
		_ = json.Unmarshal(data, &dbs)
		if len(dbs) == 0 {
			fmt.Println("No databases found. Create one with: nd php db create [app_id] <name>")
			return nil
		}
		fmt.Printf("%-6s %-24s %-20s\n", "ID", "DATABASE NAME", "OWNER")
		fmt.Println(strings.Repeat("─", 54))
		for _, d := range dbs {
			fmt.Printf("%-6d %-24s %-20s\n", d.ID, d.DBName, d.DBUser)
		}
		return nil

	case "create":
		if len(rest) == 0 {
			return fmt.Errorf("usage: nd php db create [app_id] <dbname>")
		}
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("database name required: nd php db create [app_id] <dbname>")
		}
		dbName := rem[0]
		data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/databases", map[string]string{"name": dbName})
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to create database: %s", client.JSONError(data))
		}
		ui.Success("Created database %s for site %s", dbName, appID)
		return nil

	case "rm", "delete":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("database ID required: nd php db rm [app_id] <dbid>")
		}
		dbID := rem[0]
		data, status, err := cl.Do("DELETE", "/api/v1/php/sites/"+appID+"/databases/"+dbID, nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to remove database: %s", client.JSONError(data))
		}
		ui.Success("Deleted database #%s from site %s", dbID, appID)
		return nil

	case "export":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("database ID required: nd php db export [app_id] <dbid> [output.sql.gz]")
		}
		dbID := rem[0]
		outFile := "backup.sql.gz"
		if len(rem) > 1 {
			outFile = rem[1]
		}
		data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID+"/databases/"+dbID+"/export", nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("export failed: %s", client.JSONError(data))
		}
		if outFile == "-" {
			_, _ = os.Stdout.Write(data)
			return nil
		}
		// If user requested uncompressed .sql, decompress gzip bytes
		if strings.HasSuffix(strings.ToLower(outFile), ".sql") {
			gzr, gzErr := gzip.NewReader(bytes.NewReader(data))
			if gzErr == nil {
				decompressed, dErr := io.ReadAll(gzr)
				gzr.Close()
				if dErr == nil {
					data = decompressed
				}
			}
		}
		if err := os.WriteFile(outFile, data, 0640); err != nil {
			return fmt.Errorf("could not save export file: %w", err)
		}
		ui.Success("Exported database #%s to %s (%d bytes)", dbID, outFile, len(data))
		return nil

	case "import":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) < 2 {
			return fmt.Errorf("usage: nd php db import [app_id] <dbid> <input.sql>")
		}
		dbID := rem[0]
		sqlFile := rem[1]
		var sqlBytes []byte
		if sqlFile == "-" {
			var rerr error
			sqlBytes, rerr = io.ReadAll(os.Stdin)
			if rerr != nil {
				return fmt.Errorf("could not read from stdin: %w", rerr)
			}
		} else {
			var rerr error
			sqlBytes, rerr = os.ReadFile(sqlFile)
			if rerr != nil {
				return fmt.Errorf("could not read SQL file: %w", rerr)
			}
		}
		if len(sqlBytes) == 0 {
			return fmt.Errorf("SQL file or stdin is empty")
		}
		fmt.Printf("→ Importing %s into database #%s (%d bytes)...\n", sqlFile, dbID, len(sqlBytes))
		data, status, err := cl.DoRaw("POST", "/api/v1/php/sites/"+appID+"/databases/"+dbID+"/import", "application/octet-stream", bytes.NewReader(sqlBytes))
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("import failed: %s", client.JSONError(data))
		}
		ui.Success("Imported %s into database #%s", sqlFile, dbID)
		return nil

	case "user":
		return runPHPDBUser(cl, rest)
	}

	return fmt.Errorf("unknown db subcommand %q. Run 'nd php db --help' for usage", sub)
}

func runPHPDBUser(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: nd php db user <list|create|rm> [args...]")
		return nil
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list", "ls":
		appID, _, err := resolveAppID(rest)
		if err != nil {
			return err
		}
		data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID+"/db-users", nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("error: %s", client.JSONError(data))
		}
		var users []struct {
			ID     int64  `json:"id"`
			DBUser string `json:"db_user"`
		}
		_ = json.Unmarshal(data, &users)
		if len(users) == 0 {
			fmt.Println("No database users found.")
			return nil
		}
		fmt.Printf("%-6s %-24s\n", "ID", "USERNAME")
		fmt.Println(strings.Repeat("─", 32))
		for _, u := range users {
			fmt.Printf("%-6d %-24s\n", u.ID, u.DBUser)
		}
		return nil

	case "create":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("username required: nd php db user create [app_id] <username> [password]")
		}
		username := rem[0]
		password := ""
		if len(rem) > 1 {
			password = rem[1]
		}
		payload := map[string]string{
			"name":     username,
			"password": password,
		}
		data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/db-users", payload)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to create user: %s", client.JSONError(data))
		}
		var resp struct {
			User     string `json:"user"`
			Password string `json:"password"`
		}
		_ = json.Unmarshal(data, &resp)
		ui.Success("Created database user %s with password: %s", ui.Bold(resp.User), ui.Cyan(resp.Password))
		return nil

	case "rm", "delete":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("user ID required: nd php db user rm [app_id] <uid>")
		}
		uID := rem[0]
		data, status, err := cl.Do("DELETE", "/api/v1/php/sites/"+appID+"/db-users/"+uID, nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to delete user: %s", client.JSONError(data))
		}
		ui.Success("Deleted database user #%s", uID)
		return nil
	}
	return fmt.Errorf("unknown db user command %q", sub)
}

// ─── Cron Operations (nd php cron ...) ─────────────────────────────────────

func runPHPCron(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: nd php cron <list|add|rm|run> [args...]")
		fmt.Println("\nManage PHP scheduled cron jobs:")
		fmt.Println("  list [app_id]                            List cron jobs for site")
		fmt.Println("  add [app_id] \"<schedule>\" \"<command>\"    Add a new cron job")
		fmt.Println("  rm [app_id] <cron_id>                    Delete a cron job")
		fmt.Println("  run [app_id] <cron_id>                   Run a cron job immediately")
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		appID, _, err := resolveAppID(rest)
		if err != nil {
			return err
		}
		data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID+"/cron", nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("error: %s", client.JSONError(data))
		}
		var crons []struct {
			ID         int64  `json:"id"`
			Schedule   string `json:"schedule"`
			Command    string `json:"command"`
			Enabled    bool   `json:"enabled"`
			LastRunAt  string `json:"last_run_at"`
			LastStatus string `json:"last_status"`
		}
		_ = json.Unmarshal(data, &crons)
		if len(crons) == 0 {
			fmt.Println("No cron jobs found. Add one with: nd php cron add [app_id] \"0 * * * *\" \"php cron.php\"")
			return nil
		}
		fmt.Printf("%-6s %-15s %-32s %-8s %-20s\n", "ID", "SCHEDULE", "COMMAND", "ENABLED", "LAST RUN")
		fmt.Println(strings.Repeat("─", 85))
		for _, c := range crons {
			enStr := "yes"
			if !c.Enabled {
				enStr = "no"
			}
			last := c.LastRunAt
			if last == "" {
				last = "never"
			}
			fmt.Printf("%-6d %-15s %-32s %-8s %-20s\n", c.ID, c.Schedule, c.Command, enStr, last)
		}
		return nil

	case "add":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) < 2 {
			return fmt.Errorf("schedule and command required: nd php cron add [app_id] \"*/15 * * * *\" \"php artisan schedule:run\"")
		}
		sched := rem[0]
		cmdStr := rem[1]
		payload := map[string]string{
			"schedule": sched,
			"command":  cmdStr,
		}
		data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/cron", payload)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to add cron job: %s", client.JSONError(data))
		}
		var resp struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(data, &resp)
		ui.Success("Added cron job #%d (%s) for %s", resp.ID, sched, appID)
		return nil

	case "rm", "delete":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("cron ID required: nd php cron rm [app_id] <cron_id>")
		}
		cronID := rem[0]
		data, status, err := cl.Do("DELETE", "/api/v1/php/sites/"+appID+"/cron/"+cronID, nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to delete cron: %s", client.JSONError(data))
		}
		ui.Success("Deleted cron job #%s", cronID)
		return nil

	case "run":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("cron ID required: nd php cron run [app_id] <cron_id>")
		}
		cronID := rem[0]
		data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/cron/"+cronID+"/run", nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("cron run failed: %s", client.JSONError(data))
		}
		var resp struct {
			Output string `json:"output"`
		}
		_ = json.Unmarshal(data, &resp)
		ui.Success("Executed cron job #%s:", cronID)
		if resp.Output != "" {
			fmt.Println(resp.Output)
		}
		return nil
	}
	return fmt.Errorf("unknown cron command %q", sub)
}
