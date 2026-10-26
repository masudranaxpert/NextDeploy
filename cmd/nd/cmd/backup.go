package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/ui"
)

// RunBackup handles all backup operations under 'nd backup' or 'nd backups'.
func RunBackup(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printBackupHelp()
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls", "history":
		return runBackupList(cl, rest)
	case "create", "manual", "run":
		return runBackupCreate(cl, rest)
	case "log":
		return runBackupLog(cl, rest)
	case "restore":
		return runBackupRestore(cl, rest)
	case "schedule", "schedules":
		return runBackupSchedule(cl, rest)
	case "dest", "destination", "destinations":
		return runBackupDestList(cl, rest)
	default:
		return fmt.Errorf("unknown backup command %q. Run 'nd backup --help' for usage", sub)
	}
}

func printBackupHelp() {
	fmt.Println("Usage: nd backup <command> [args...]")
	fmt.Println("\nManage application backups, schedules, and remote cloud destinations:")
	fmt.Println("  list [app_id]                                 List backup history")
	fmt.Println("  create [app_id] [dest_id]                     Trigger manual backup")
	fmt.Println("  log [app_id] <history_id>                     View execution log of a backup")
	fmt.Println("  restore [app_id] <history_id>                 Restore app from backup history")
	fmt.Println("  schedule <list|add|rm>                        Manage automated backup schedules")
	fmt.Println("  dest list                                     List connected cloud backup destinations")
}

func runBackupList(cl *client.Client, args []string) error {
	urlPath := "/api/v1/backups/history"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		appID, _, _ := resolveAppID(args)
		if appID != "" {
			urlPath += "?app_id=" + appID
		}
	} else if pc, err := config.LoadProject("."); err == nil && pc.AppID != "" {
		urlPath += "?app_id=" + pc.AppID
	}

	data, status, err := cl.Do("GET", urlPath, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var history []struct {
		ID        int64  `json:"id"`
		AppID     string `json:"app_id"`
		DestID    int64  `json:"destination_id"`
		Status    string `json:"status"`
		Size      int64  `json:"size_bytes"`
		CreatedAt string `json:"created_at"`
	}
	_ = json.Unmarshal(data, &history)
	if len(history) == 0 {
		fmt.Println("No backup history found.")
		return nil
	}

	fmt.Printf("%-6s %-16s %-8s %-10s %-12s %-20s\n", "ID", "APP ID", "DEST ID", "STATUS", "SIZE", "DATE")
	fmt.Println(strings.Repeat("─", 80))
	for _, h := range history {
		sizeStr := formatFileSize(h.Size)
		dateStr := h.CreatedAt
		if len(dateStr) > 19 {
			dateStr = dateStr[:19]
		}
		fmt.Printf("%-6d %-16s %-8d %-10s %-12s %-20s\n", h.ID, h.AppID, h.DestID, h.Status, sizeStr, dateStr)
	}
	return nil
}

func runBackupCreate(cl *client.Client, args []string) error {
	appID, rem, err := resolveAppID(args)
	if err != nil {
		return err
	}

	destID := int64(0)
	backupType := "app"
	for i := 0; i < len(rem); i++ {
		if (rem[i] == "-t" || rem[i] == "--type") && i+1 < len(rem) {
			backupType = rem[i+1]
			i++
		} else if strings.HasPrefix(rem[i], "--type=") {
			backupType = strings.TrimPrefix(rem[i], "--type=")
		} else if id, perr := strconv.ParseInt(rem[i], 10, 64); perr == nil && destID == 0 {
			destID = id
		}
	}

	payload := map[string]interface{}{
		"app_id":      appID,
		"dest_id":     destID,
		"type":        backupType,
		"backup_type": backupType,
	}

	fmt.Printf("→ Triggering %s backup for %s...\n", backupType, appID)
	data, status, err := cl.Do("POST", "/api/v1/backups/manual", payload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("backup failed: %s", client.JSONError(data))
	}
	ui.Success("Backup started for app %s", appID)
	return nil
}

func runBackupLog(cl *client.Client, args []string) error {
	appID, rem, err := resolveAppID(args)
	histID := ""
	if len(rem) > 0 {
		histID = rem[0]
	} else if appID != "" {
		histID = appID
	}
	if err != nil || histID == "" {
		return fmt.Errorf("history ID required: nd backup log [app_id] <history_id>")
	}

	data, status, err := cl.Do("GET", "/api/v1/backups/history/"+histID+"/log", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	var resp struct {
		Log string `json:"log"`
	}
	if err := json.Unmarshal(data, &resp); err == nil && resp.Log != "" {
		fmt.Println(resp.Log)
	} else {
		fmt.Println(string(data))
	}
	return nil
}

func runBackupRestore(cl *client.Client, args []string) error {
	appID, rem, err := resolveAppID(args)
	histID := ""
	if len(rem) > 0 {
		histID = rem[0]
	} else if appID != "" {
		histID = appID
	}
	if err != nil || histID == "" {
		return fmt.Errorf("history ID required: nd backup restore [app_id] <history_id>")
	}

	fmt.Printf("→ Restoring app %s from backup #%s...\n", appID, histID)
	data, status, err := cl.Do("POST", "/api/v1/backups/restore/"+histID, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("restore failed: %s", client.JSONError(data))
	}
	ui.Success("Restore started for app %s from backup #%s", appID, histID)
	return nil
}

func runBackupSchedule(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: nd backup schedule <list|add|rm> [args...]")
		fmt.Println("\nCommands:")
		fmt.Println("  list [app_id]                                               List backup schedules")
		fmt.Println("  add [app_id] <dest_id> \"<cron>\" [--retention 7]             Create backup schedule")
		fmt.Println("  rm [app_id] <schedule_id>                                   Delete backup schedule")
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		appID := ""
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			appID, _, _ = resolveAppID(rest)
		} else if pc, err := config.LoadProject("."); err == nil && pc.AppID != "" {
			appID = pc.AppID
		}
		path := "/api/v1/backups/schedules"
		if appID != "" {
			path += "?app_id=" + appID
		}
		data, status, err := cl.Do("GET", path, nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("error: %s", client.JSONError(data))
		}
		var schedules []struct {
			ID        int64  `json:"id"`
			AppID     string `json:"app_id"`
			DestID    int64  `json:"destination_id"`
			CronExpr  string `json:"cron_expression"`
			KeepCount int    `json:"keep_count"`
			Enabled   bool   `json:"enabled"`
		}
		_ = json.Unmarshal(data, &schedules)
		if len(schedules) == 0 {
			fmt.Println("No backup schedules configured.")
			return nil
		}
		fmt.Printf("%-6s %-16s %-8s %-16s %-10s %-8s\n", "ID", "APP ID", "DEST ID", "SCHEDULE", "RETENTION", "ENABLED")
		fmt.Println(strings.Repeat("─", 72))
		for _, s := range schedules {
			enStr := "yes"
			if !s.Enabled {
				enStr = "no"
			}
			fmt.Printf("%-6d %-16s %-8d %-16s %-10d %-8s\n", s.ID, s.AppID, s.DestID, s.CronExpr, s.KeepCount, enStr)
		}
		return nil

	case "add":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) < 2 {
			return fmt.Errorf("usage: nd backup schedule add [app_id] <dest_id> \"<cron>\" [--retention 7]")
		}
		destID, _ := strconv.ParseInt(rem[0], 10, 64)
		cronExpr := rem[1]
		retention := 7
		for i := 2; i < len(rem); i++ {
			if rem[i] == "--retention" && i+1 < len(rem) {
				if n, rerr := strconv.Atoi(rem[i+1]); rerr == nil {
					retention = n
				}
				i++
			}
		}

		payload := map[string]interface{}{
			"app_id":    appID,
			"dest_id":   destID,
			"cron":      cronExpr,
			"retention": retention,
		}
		data, status, err := cl.Do("POST", "/api/v1/backups/schedules", payload)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to create schedule: %s", client.JSONError(data))
		}
		ui.Success("Created backup schedule (%s) for app %s", cronExpr, appID)
		return nil

	case "rm", "delete":
		appID, rem, err := resolveAppID(rest)
		schedID := ""
		if len(rem) > 0 {
			schedID = rem[0]
		} else if appID != "" {
			schedID = appID
		}
		if err != nil || schedID == "" {
			return fmt.Errorf("schedule ID required: nd backup schedule rm [app_id] <schedule_id>")
		}
		data, status, err := cl.Do("DELETE", "/api/v1/backups/schedules/"+schedID, nil)
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("failed to delete schedule: %s", client.JSONError(data))
		}
		ui.Success("Deleted backup schedule #%s", schedID)
		return nil
	}
	return fmt.Errorf("unknown schedule command %q", sub)
}

func runBackupDestList(cl *client.Client, args []string) error {
	data, status, err := cl.Do("GET", "/api/v1/backups/destinations", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var dests []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Provider string `json:"provider"`
	}
	_ = json.Unmarshal(data, &dests)
	if len(dests) == 0 {
		fmt.Println("No backup destinations configured. Configure Google Drive or S3/R2 in NextDeploy Panel (/backup).")
		return nil
	}

	fmt.Printf("%-6s %-24s %-12s\n", "ID", "DESTINATION NAME", "PROVIDER")
	fmt.Println(strings.Repeat("─", 45))
	for _, d := range dests {
		fmt.Printf("%-6d %-24s %-12s\n", d.ID, d.Name, d.Provider)
	}
	return nil
}
