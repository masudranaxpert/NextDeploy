package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"

	"nd/internal/client"
	"nd/internal/sync"
)

// RunPush syncs local workspace files to the server and triggers deploy.
// Usage: nd push <app_id> [local_dir]
func RunPush(cl *client.Client, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: nd push <app_id> [local_dir]")
	}
	appID := args[0]
	localDir := "."
	if len(args) >= 2 {
		localDir = args[1]
	}

	abs, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("invalid dir: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("directory not found: %s", abs)
	}

	fmt.Printf("→ Scanning local files in %s\n", abs)
	localHashes, err := sync.LocalHashes(abs)
	if err != nil {
		return fmt.Errorf("local scan failed: %w", err)
	}
	fmt.Printf("  Found %d files locally\n", len(localHashes))

	// Fetch server manifest
	fmt.Println("→ Fetching server manifest...")
	manifestBody, status, err := cl.Do("GET", "/api/v1/apps/"+appID+"/manifest?hash=true", nil)
	if err != nil {
		return fmt.Errorf("manifest fetch failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("manifest error: %s", client.JSONError(manifestBody))
	}

	// Compute diff
	toUpload, toDelete, err := sync.Diff(manifestBody, localHashes)
	if err != nil {
		return fmt.Errorf("diff computation failed: %w", err)
	}

	fmt.Printf("  %d to upload, %d to delete, %d in sync\n",
		len(toUpload), len(toDelete), len(localHashes)-len(toUpload))

	if len(toUpload) == 0 && len(toDelete) == 0 {
		fmt.Println("✓ Already in sync — nothing to push.")
		return nil
	}

	// Upload changed files as multipart tar.gz
	if len(toUpload) > 0 {
		fmt.Printf("→ Uploading %d changed file(s)...\n", len(toUpload))

		tarReader, _, err := sync.PackTarGz(abs, toUpload)
		if err != nil {
			return fmt.Errorf("pack failed: %w", err)
		}

		// Buffer tar.gz into memory then wrap in multipart
		// ponytail: buffering in RAM; for very large repos a temp file would be better
		tarBytes, err := io.ReadAll(tarReader)
		if err != nil {
			return fmt.Errorf("read tar failed: %w", err)
		}

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, err := mw.CreateFormFile("archive", "workspace.tar.gz")
		if err != nil {
			return err
		}
		if _, err := part.Write(tarBytes); err != nil {
			return err
		}
		_ = mw.Close()

		respBody, status, err := cl.DoRaw("POST",
			"/api/v1/apps/"+appID+"/workspace/archive",
			mw.FormDataContentType(),
			&buf)
		if err != nil {
			return fmt.Errorf("upload failed: %w", err)
		}
		if status >= 400 {
			return fmt.Errorf("upload error: %s", client.JSONError(respBody))
		}

		var resp struct {
			FilesExtracted int `json:"files_extracted"`
		}
		_ = json.Unmarshal(respBody, &resp)
		fmt.Printf("  ✓ %d file(s) extracted on server\n", resp.FilesExtracted)
	}

	// Delete removed files
	for _, path := range toDelete {
		_, _, _ = cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name":      "file_delete",
				"arguments": map[string]interface{}{"app_id": appID, "path": path},
			},
		})
		fmt.Printf("  - deleted %s\n", path)
	}

	// Trigger deploy
	fmt.Println("→ Deploying...")
	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      "deploy",
			"arguments": map[string]interface{}{"app_id": appID, "wait_seconds": 120},
		},
	})
	if err != nil {
		return fmt.Errorf("deploy failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("deploy error: %s", client.JSONError(body))
	}

	fmt.Println("✓ Push complete.")
	return nil
}
