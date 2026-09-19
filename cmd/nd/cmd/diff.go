package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/sync"
	"nd/internal/ui"
)

// DiffResult represents structured JSON output for nd diff.
type DiffResult struct {
	AppID      string   `json:"app_id"`
	InSync     int      `json:"in_sync"`
	Created    []string `json:"created"`
	Modified   []string `json:"modified"`
	ServerOnly []string `json:"server_only"`
}

// RunDiff compares local workspace files against the server manifest and displays the differences.
// Usage: nd diff [app_id] [local_dir] [--json]
func RunDiff(cl *client.Client, rawArgs []string) error {
	jsonOutput := false
	var flagAppID string
	var args []string

	for i := 0; i < len(rawArgs); i++ {
		a := rawArgs[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Println("Usage: nd diff [app_id] [local_dir] [--json]")
			fmt.Println("\nCompare local workspace files against remote server files before pushing or deploying.")
			fmt.Println("\nFlags:")
			fmt.Println("  -a, --app <app_id>   Target application ID (optional if linked)")
			fmt.Println("  -j, --json           Output diff summary as JSON")
			return nil
		case a == "--json" || a == "-j":
			jsonOutput = true
		case (a == "-a" || a == "--app") && i+1 < len(rawArgs):
			flagAppID = rawArgs[i+1]
			i++
		case strings.HasPrefix(a, "--app="):
			flagAppID = strings.TrimPrefix(a, "--app=")
		default:
			args = append(args, a)
		}
	}

	var appID string
	target := "."

	if flagAppID != "" {
		appID = flagAppID
		if len(args) >= 1 {
			target = args[0]
		}
	} else if len(args) == 0 {
		pc, err := config.LoadProject(".")
		if err != nil || pc.AppID == "" {
			return fmt.Errorf("app_id required: nd diff [app_id] [local_dir] (or run 'nd link <app_id>' first)")
		}
		appID = pc.AppID
		target = "."
	} else if len(args) == 1 {
		pc, lerr := config.LoadProject(".")
		if lerr == nil && pc.AppID != "" {
			if _, serr := os.Stat(args[0]); serr == nil {
				appID = pc.AppID
				target = args[0]
			} else {
				appID = args[0]
				target = "."
			}
		} else {
			appID = args[0]
			target = "."
		}
	} else {
		appID = args[0]
		target = args[1]
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("directory not found: %s", target)
	}

	if !jsonOutput {
		fmt.Printf("→ Scanning local files in %s...\n", abs)
	}
	localHashes, err := sync.LocalHashes(abs)
	if err != nil {
		return fmt.Errorf("local scan failed: %w", err)
	}

	if !jsonOutput {
		fmt.Printf("→ Fetching server manifest for %s...\n", ui.Cyan(appID))
	}
	manifestBody, status, err := cl.Do("GET", "/api/v1/apps/"+appID+"/manifest?hash=true&include_locks=true", nil)
	if err != nil {
		return fmt.Errorf("manifest fetch failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("manifest error (%d): %s", status, client.JSONError(manifestBody))
	}

	toUpload, toDelete, err := sync.Diff(manifestBody, localHashes)
	if err != nil {
		return fmt.Errorf("diff computation failed: %w", err)
	}

	// Parse remote manifest to differentiate newly created files vs modified files
	var res struct {
		Files []struct {
			Path   string `json:"path"`
			IsDir  bool   `json:"is_dir"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	_ = json.Unmarshal(manifestBody, &res)
	remoteMap := make(map[string]bool, len(res.Files))
	for _, f := range res.Files {
		if !f.IsDir {
			remoteMap[f.Path] = true
		}
	}

	var created []string
	var modified []string
	for _, p := range toUpload {
		if remoteMap[p] {
			modified = append(modified, p)
		} else {
			created = append(created, p)
		}
	}

	var safeServerOnly []string
	for _, p := range toDelete {
		clean := filepath.ToSlash(strings.TrimSpace(p))
		base := filepath.Base(clean)
		if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasPrefix(clean, ".nd/") || clean == ".nd" {
			continue
		}
		safeServerOnly = append(safeServerOnly, p)
	}

	sort.Strings(created)
	sort.Strings(modified)
	sort.Strings(safeServerOnly)

	inSyncCount := len(localHashes) - len(toUpload)

	if jsonOutput {
		out := DiffResult{
			AppID:      appID,
			InSync:     inSyncCount,
			Created:    created,
			Modified:   modified,
			ServerOnly: safeServerOnly,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Println()
	if len(created) == 0 && len(modified) == 0 && len(safeServerOnly) == 0 {
		ui.Success("All %d file(s) are in sync with server (%s).", inSyncCount, ui.Bold(appID))
		return nil
	}

	fmt.Printf("Diff for app %s (Local: %d files | In-sync: %s):\n\n",
		ui.HiCyan(appID), len(localHashes), ui.Green(fmt.Sprintf("%d", inSyncCount)))

	if len(created) > 0 {
		fmt.Printf("  %s (%d files to upload):\n", ui.Green("+ Created locally"), len(created))
		for _, f := range created {
			fmt.Printf("      %s\n", ui.Green("+ "+f))
		}
		fmt.Println()
	}

	if len(modified) > 0 {
		fmt.Printf("  %s (%d files to upload):\n", ui.Yellow("~ Modified locally"), len(modified))
		for _, f := range modified {
			fmt.Printf("      %s\n", ui.Yellow("~ "+f))
		}
		fmt.Println()
	}

	if len(safeServerOnly) > 0 {
		fmt.Printf("  %s (%d server files not in local dir):\n", ui.Red("- Server only"), len(safeServerOnly))
		for _, f := range safeServerOnly {
			fmt.Printf("      %s\n", ui.Red("- "+f))
		}
		fmt.Println()
	}

	fmt.Printf("Run %s to synchronize changes, or %s to preview file content.\n\n",
		ui.Bold("nd push"), ui.Bold("nd cat <path>"))
	return nil
}
