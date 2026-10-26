package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"nd/internal/client"
	"nd/internal/ui"
)

// RunTop handles 'nd top' and 'nd stats' for host performance metrics.
func RunTop(cl *client.Client, args []string) error {
	data, status, err := cl.Do("GET", "/api/v1/system/top", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var snap struct {
		CPUPct        float64 `json:"cpu_pct"`
		MemUsedGB     float64 `json:"mem_used_gb"`
		MemTotalGB    float64 `json:"mem_total_gb"`
		MemPct        float64 `json:"mem_pct"`
		DiskUsedGB    float64 `json:"disk_used_gb"`
		DiskTotalGB   float64 `json:"disk_total_gb"`
		DiskPct       float64 `json:"disk_pct"`
		ContainersNum int     `json:"containers_num"`
		Containers    []struct {
			Name    string `json:"name"`
			Service string `json:"service"`
			State   string `json:"state"`
			Status  string `json:"status"`
			Image   string `json:"image"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	fmt.Println(ui.Bold("Host System Telemetry:"))
	fmt.Printf("  CPU Usage:       %.1f%%\n", snap.CPUPct)
	fmt.Printf("  RAM Usage:       %.2f GB / %.2f GB (%.1f%%)\n", snap.MemUsedGB, snap.MemTotalGB, snap.MemPct)
	fmt.Printf("  Disk Usage:      %.1f GB / %.1f GB (%.1f%%)\n", snap.DiskUsedGB, snap.DiskTotalGB, snap.DiskPct)
	fmt.Printf("  Live Containers: %d container(s)\n\n", snap.ContainersNum)

	if len(snap.Containers) > 0 {
		fmt.Printf("%-24s %-16s %-10s %-20s\n", "CONTAINER", "SERVICE", "STATE", "STATUS")
		fmt.Println(strings.Repeat("─", 74))
		for _, c := range snap.Containers {
			fmt.Printf("%-24s %-16s %-10s %-20s\n", c.Name, c.Service, c.State, c.Status)
		}
	}
	return nil
}

// RunClean handles 'nd clean' for pruning system resources.
func RunClean(cl *client.Client, args []string) error {
	fmt.Println("→ Pruning stopped containers, unused images, build cache, and temp files...")
	data, status, err := cl.Do("POST", "/api/v1/system/cleanup", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("cleanup failed: %s", client.JSONError(data))
	}

	ui.Success("System cleanup finished successfully.")
	return nil
}

// RunAudit handles 'nd audit' to display recent system audit logs.
func RunAudit(cl *client.Client, args []string) error {
	lines := 25
	for i := 0; i < len(args); i++ {
		if (args[i] == "-n" || args[i] == "--lines") && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
				lines = n
			}
			i++
		}
	}

	data, status, err := cl.Do("GET", "/api/v1/system/audit-logs?per_page="+strconv.Itoa(lines)+"&format=json", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var resp struct {
		Logs []struct {
			ID        int64  `json:"id"`
			Username  string `json:"username"`
			Action    string `json:"action"`
			Target    string `json:"target_id"`
			Details   string `json:"details"`
			CreatedAt string `json:"created_at"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	if len(resp.Logs) == 0 {
		fmt.Println("No recent audit logs.")
		return nil
	}

	fmt.Printf("%-19s %-12s %-20s %-16s %s\n", "TIMESTAMP", "USER", "ACTION", "TARGET", "DETAILS")
	fmt.Println(strings.Repeat("─", 95))
	for _, l := range resp.Logs {
		ts := l.CreatedAt
		if len(ts) > 19 {
			ts = ts[:19]
		}
		fmt.Printf("%-19s %-12s %-20s %-16s %s\n", ts, l.Username, l.Action, l.Target, l.Details)
	}
	return nil
}
