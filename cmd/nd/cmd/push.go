package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/sync"
	"nd/internal/ui"
)

// PushResult represents the structured JSON output for nd push --json.
type PushResult struct {
	AppID       string   `json:"app_id"`
	Uploaded    int      `json:"uploaded"`
	Deleted     int      `json:"deleted"`
	InSync      int      `json:"in_sync"`
	FilesPruned []string `json:"files_pruned,omitempty"`
	DeployJobID string   `json:"deploy_job_id,omitempty"`
	Status      string   `json:"status"`
}

// RunPush syncs local workspace files to the server and optionally triggers deploy.
// Usage: nd push [app_id] [local_dir] [--deploy] [--prune] [-y] [--json]
func RunPush(cl *client.Client, rawArgs []string) error {
	prune := false
	autoConfirm := false
	jsonOutput := false
	doDeploy := false
	var args []string
	var flagAppID string

	for i := 0; i < len(rawArgs); i++ {
		a := rawArgs[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Println("Usage: nd push [app_id] [local_path] [--deploy] [--prune] [-y] [--json]")
			fmt.Println("\nPush local workspace files or a single file to the server.")
			fmt.Println("\nFlags:")
			fmt.Println("  -a, --app <app_id>   Target application ID (optional if linked)")
			fmt.Println("  -d, --deploy         Trigger auto-deployment after push")
			fmt.Println("  --prune              Delete server-only files not found locally")
			fmt.Println("  -y, --yes            Auto-confirm prune without prompt")
			fmt.Println("  -j, --json           Output result as JSON")
			return nil
		case a == "--deploy" || a == "-d":
			doDeploy = true
		case a == "--prune":
			prune = true
		case a == "-y" || a == "--yes":
			autoConfirm = true
		case a == "--json" || a == "-j":
			jsonOutput = true
		case (a == "-a" || a == "--app") && i+1 < len(rawArgs):
			flagAppID = rawArgs[i+1]
			i++
		case strings.HasPrefix(a, "--app="):
			flagAppID = strings.TrimPrefix(a, "--app=")
		case strings.HasPrefix(a, "-a="):
			flagAppID = strings.TrimPrefix(a, "-a=")
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
			return fmt.Errorf("app_id required: nd push [app_id] [local_path] (or run 'nd link <app_id>' first)")
		}
		appID = pc.AppID
		target = "."
	} else if len(args) == 1 {
		pc, lerr := config.LoadProject(".")
		if lerr == nil && pc.AppID != "" {
			// If target exists locally on disk (file or directory), use it as target for linked app
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
	targetFi, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("file or directory not found: %s", target)
	}

	// Single file push mode
	if !targetFi.IsDir() {
		cwd, _ := os.Getwd()
		rel, rerr := filepath.Rel(cwd, abs)
		if rerr != nil || strings.HasPrefix(rel, "..") {
			cwd = filepath.Dir(abs)
			rel = filepath.Base(abs)
		}
		relSlash := filepath.ToSlash(rel)

		if !jsonOutput {
			fmt.Printf("→ Pushing single file: %s (%d bytes) to %s\n", relSlash, targetFi.Size(), appID)
		}

		tarReader, _, err := sync.PackTarGz(cwd, []string{relSlash})
		if err != nil {
			return fmt.Errorf("pack failed: %w (deployment aborted)", err)
		}

		tarBytes, err := io.ReadAll(tarReader)
		if err != nil {
			return fmt.Errorf("read tar failed: %w (deployment aborted)", err)
		}

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, err := mw.CreateFormFile("archive", "workspace.tar.gz")
		if err != nil {
			return fmt.Errorf("archive part creation failed: %w (deployment aborted)", err)
		}
		if _, err := part.Write(tarBytes); err != nil {
			return fmt.Errorf("archive write failed: %w (deployment aborted)", err)
		}
		_ = mw.Close()

		respBody, status, err := cl.DoRaw("POST",
			"/api/v1/apps/"+appID+"/workspace/archive",
			mw.FormDataContentType(),
			&buf)
		if err != nil {
			return fmt.Errorf("upload failed: %w (deployment aborted)", err)
		}
		if status >= 400 {
			return fmt.Errorf("upload error: %s (deployment aborted)", client.JSONError(respBody))
		}

		if !jsonOutput {
			ui.Success("Successfully pushed %s to %s", relSlash, appID)
		}

		if doDeploy {
			if !jsonOutput {
				fmt.Println("→ Deploying...")
			}
			body, status, err := cl.DoWithTimeout("POST", "/mcp", map[string]interface{}{
				"method": "tools/call",
				"params": map[string]interface{}{
					"name":      "deploy",
					"arguments": map[string]interface{}{"app_id": appID, "wait_seconds": 120},
				},
			}, 3*time.Minute)
			if err != nil {
				return fmt.Errorf("deploy failed: %w", err)
			}
			if status >= 400 {
				return fmt.Errorf("deploy error: %s", client.JSONError(body))
			}
			if !jsonOutput {
				fmt.Println("✓ Push and deployment completed successfully.")
			}
		}
		return nil
	}

	if !jsonOutput {
		fmt.Printf("→ Scanning local files in %s\n", abs)
	}
	localHashes, err := sync.LocalHashes(abs)
	if err != nil {
		return fmt.Errorf("local scan failed: %w", err)
	}
	if !jsonOutput {
		fmt.Printf("  Found %d files locally\n", len(localHashes))
		fmt.Println("→ Fetching server manifest...")
	}

	// Fetch server manifest with locks included so lockfiles are checked via hash and not needlessly re-uploaded
	manifestBody, status, err := cl.Do("GET", "/api/v1/apps/"+appID+"/manifest?hash=true&include_locks=true", nil)
	if err != nil {
		return fmt.Errorf("manifest fetch failed: %w (deployment aborted)", err)
	}
	if status >= 400 {
		return fmt.Errorf("manifest error: %s (deployment aborted)", client.JSONError(manifestBody))
	}

	// Compute diff
	toUpload, toDelete, err := sync.Diff(manifestBody, localHashes)
	if err != nil {
		return fmt.Errorf("diff computation failed: %w (deployment aborted)", err)
	}

	// Exclude protected environment and configuration files from pruning defensively
	var safeToDelete []string
	for _, p := range toDelete {
		clean := filepath.ToSlash(strings.TrimSpace(p))
		base := filepath.Base(clean)
		if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasPrefix(clean, ".nd/") || clean == ".nd" {
			continue
		}
		safeToDelete = append(safeToDelete, p)
	}
	toDelete = safeToDelete

	inSyncCount := len(localHashes) - len(toUpload)
	if !jsonOutput {
		fmt.Printf("  %d to upload, %d server-only, %d in sync\n",
			len(toUpload), len(toDelete), inSyncCount)
	}

	if len(toUpload) == 0 && (!prune || len(toDelete) == 0) {
		if jsonOutput {
			res := PushResult{
				AppID:    appID,
				Uploaded: 0,
				Deleted:  0,
				InSync:   inSyncCount,
				Status:   "already_in_sync",
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(res)
		}
		fmt.Println("✓ Already in sync — nothing to push.")
		return nil
	}

	// Step 1: Upload changed files as multipart tar.gz
	if len(toUpload) > 0 {
		if !jsonOutput {
			fmt.Printf("→ Uploading %d changed file(s)...\n", len(toUpload))
		}

		tarReader, _, err := sync.PackTarGz(abs, toUpload)
		if err != nil {
			return fmt.Errorf("pack failed: %w (deployment aborted)", err)
		}

		tarBytes, err := io.ReadAll(tarReader)
		if err != nil {
			return fmt.Errorf("read tar failed: %w (deployment aborted)", err)
		}

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, err := mw.CreateFormFile("archive", "workspace.tar.gz")
		if err != nil {
			return fmt.Errorf("archive part creation failed: %w (deployment aborted)", err)
		}
		if _, err := part.Write(tarBytes); err != nil {
			return fmt.Errorf("archive write failed: %w (deployment aborted)", err)
		}
		_ = mw.Close()

		respBody, status, err := cl.DoRaw("POST",
			"/api/v1/apps/"+appID+"/workspace/archive",
			mw.FormDataContentType(),
			&buf)
		if err != nil {
			return fmt.Errorf("upload failed: %w (deployment aborted)", err)
		}
		if status >= 400 {
			return fmt.Errorf("upload error: %s (deployment aborted)", client.JSONError(respBody))
		}

		var resp struct {
			FilesExtracted int `json:"files_extracted"`
		}
		_ = json.Unmarshal(respBody, &resp)
		if resp.FilesExtracted == 0 && len(toUpload) > 0 {
			return fmt.Errorf("upload error: no files were extracted on server (deployment aborted)")
		}
		if !jsonOutput {
			fmt.Printf("  ✓ %d file(s) extracted on server\n", resp.FilesExtracted)
		}
	}

	// Step 2: Handle deletions (opt-in with --prune, batched via workspace_apply)
	deletedCount := 0
	var deletedFiles []string
	if len(toDelete) > 0 {
		if !prune {
			if !jsonOutput {
				fmt.Printf("  • Notice: %d server-only file(s) not pruned (run with --prune to sync deletions).\n", len(toDelete))
			}
		} else {
			if !autoConfirm && !jsonOutput {
				fmt.Printf("\nWARNING: --prune will permanently delete the following %d file(s) on the server:\n", len(toDelete))
				for _, p := range toDelete {
					fmt.Printf("  - %s\n", p)
				}
				fmt.Print("Proceed with pruning? (y/N): ")
				var confirm string
				_, _ = fmt.Scanln(&confirm)
				confirm = strings.ToLower(strings.TrimSpace(confirm))
				if confirm != "y" && confirm != "yes" {
					if !jsonOutput {
						fmt.Println("Pruning cancelled.")
					}
					toDelete = nil
				}
			}

			if len(toDelete) > 0 {
				if !jsonOutput {
					fmt.Printf("→ Pruning %d remote file(s) via workspace_apply...\n", len(toDelete))
				}
				delBody, delStatus, delErr := cl.Do("POST", "/mcp", map[string]interface{}{
					"method": "tools/call",
					"params": map[string]interface{}{
						"name": "workspace_apply",
						"arguments": map[string]interface{}{
							"app_id":  appID,
							"deletes": toDelete,
						},
					},
				})
				if delErr != nil {
					return fmt.Errorf("prune failed: %w (deployment aborted)", delErr)
				}
				if delStatus >= 400 {
					return fmt.Errorf("prune error: %s (deployment aborted)", client.JSONError(delBody))
				}
				var applyResp struct {
					Result struct {
						IsError bool `json:"isError"`
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"result"`
				}
				if err := json.Unmarshal(delBody, &applyResp); err == nil && applyResp.Result.IsError {
					errMsg := "workspace_apply failed"
					if len(applyResp.Result.Content) > 0 {
						errMsg = applyResp.Result.Content[0].Text
					}
					return fmt.Errorf("prune error: %s (deployment aborted)", errMsg)
				}
				deletedCount = len(toDelete)
				deletedFiles = toDelete
				if !jsonOutput {
					fmt.Printf("  ✓ %d remote file(s) pruned\n", deletedCount)
				}
			}
		}
	}

	jobID := ""
	if doDeploy {
		// Step 3: Trigger deploy
		if !jsonOutput {
			fmt.Println("→ Deploying...")
		}
		body, status, err := cl.DoWithTimeout("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "deploy",
				"arguments": map[string]interface{}{"app_id": appID, "wait_seconds": 120},
			},
		}, 3*time.Minute)
		if err != nil {
			return fmt.Errorf("deploy failed: %w", err)
		}
		if status >= 400 {
			return fmt.Errorf("deploy error: %s", client.JSONError(body))
		}

		var depResp struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &depResp); err == nil {
			if depResp.Result.IsError && len(depResp.Result.Content) > 0 {
				return fmt.Errorf("deploy error: %s", depResp.Result.Content[0].Text)
			}
			if len(depResp.Result.Content) > 0 {
				var depData struct {
					JobID  string `json:"job_id"`
					Status string `json:"status"`
				}
				_ = json.Unmarshal([]byte(depResp.Result.Content[0].Text), &depData)
				jobID = depData.JobID
			}
		}
	}

	if jsonOutput {
		res := PushResult{
			AppID:       appID,
			Uploaded:    len(toUpload),
			Deleted:     deletedCount,
			InSync:      inSyncCount,
			FilesPruned: deletedFiles,
			DeployJobID: jobID,
			Status:      "success",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	if doDeploy {
		fmt.Println("✓ Push and deployment completed successfully.")
	} else {
		fmt.Println("✓ Workspace synchronized successfully. (Run with --deploy to auto-deploy)")
	}
	return nil
}
