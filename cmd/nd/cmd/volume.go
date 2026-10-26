package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/ui"
)

// RunVolume handles all volume operations under 'nd volume' or 'nd volumes'.
func RunVolume(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printVolumeHelp()
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		return runVolumeList(cl, rest)
	case "browse":
		return runVolumeBrowse(cl, rest)
	case "download", "backup":
		return runVolumeDownload(cl, rest)
	case "restore":
		return runVolumeRestore(cl, rest)
	case "rm", "delete":
		return runVolumeRemove(cl, rest)
	case "prune":
		return runVolumePrune(cl, rest)
	default:
		return fmt.Errorf("unknown volume command %q. Run 'nd volume --help' for usage", sub)
	}
}

func printVolumeHelp() {
	fmt.Println("Usage: nd volume <command> [args...]")
	fmt.Println("\nManage persistent Docker volumes:")
	fmt.Println("  list, ls                           List all visible volumes")
	fmt.Println("  browse <name> [path]               Browse files inside a volume")
	fmt.Println("  download <name> [file.tar.gz]      Download volume backup archive")
	fmt.Println("  restore <name> <file.tar.gz>       Restore tar.gz backup into volume")
	fmt.Println("  rm <name>                          Remove a volume")
	fmt.Println("  prune                              Prune unused volumes (admin only)")
}

func runVolumeList(cl *client.Client, args []string) error {
	data, status, err := cl.Do("GET", "/api/v1/volumes", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var vols []struct {
		Name  string `json:"name"`
		InUse bool   `json:"in_use"`
	}
	_ = json.Unmarshal(data, &vols)
	if len(vols) == 0 {
		fmt.Println("No volumes found.")
		return nil
	}

	fmt.Printf("%-36s %-10s\n", "VOLUME NAME", "STATUS")
	fmt.Println(strings.Repeat("─", 50))
	for _, v := range vols {
		status := "active"
		if !v.InUse {
			status = "unused"
		}
		fmt.Printf("%-36s %-10s\n", v.Name, status)
	}
	return nil
}

func runVolumeBrowse(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("volume name required: nd volume browse <name> [path]")
	}
	name := args[0]
	path := ""
	if len(args) > 1 {
		path = args[1]
	}

	data, status, err := cl.Do("GET", "/api/v1/volumes/browse?name="+name+"&path="+path+"&format=json", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var resp struct {
		Volume  string `json:"volume"`
		Path    string `json:"path"`
		Entries []struct {
			Name    string `json:"name"`
			IsDir   bool   `json:"is_dir"`
			RelPath string `json:"rel_path"`
		} `json:"entries"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("browse error: %s", resp.Error)
	}

	fmt.Printf("Browsing %s (path: /%s):\n", ui.Bold(resp.Volume), resp.Path)
	if len(resp.Entries) == 0 {
		fmt.Println("  (empty directory)")
		return nil
	}
	for _, e := range resp.Entries {
		if e.IsDir {
			fmt.Printf("  📁 %s/\n", ui.Cyan(e.Name))
		} else {
			fmt.Printf("  📄 %s\n", e.Name)
		}
	}
	return nil
}

func runVolumeDownload(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("volume name required: nd volume download <name> [file.tar.gz]")
	}
	name := args[0]
	outFile := name + "-backup.tar.gz"
	if len(args) > 1 {
		outFile = args[1]
	}

	fmt.Printf("→ Downloading backup for volume %s...\n", name)
	data, status, err := cl.Do("GET", "/api/v1/volumes/download?name="+name, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("download error: %s", client.JSONError(data))
	}

	if err := os.WriteFile(outFile, data, 0640); err != nil {
		return fmt.Errorf("could not write file: %w", err)
	}
	ui.Success("Saved volume backup to %s (%d bytes)", outFile, len(data))
	return nil
}

func runVolumeRestore(cl *client.Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: nd volume restore <name> <file.tar.gz>")
	}
	name := args[0]
	tarPath := args[1]

	data, err := os.ReadFile(tarPath)
	if err != nil {
		return fmt.Errorf("could not read backup file: %w", err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", name)
	_ = mw.WriteField("volume", name)
	part, err := mw.CreateFormFile("backup", filepath.Base(tarPath))
	if err != nil {
		return fmt.Errorf("failed to create multipart form: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	_ = mw.Close()

	fmt.Printf("→ Restoring %s to volume %s (%d bytes)...\n", tarPath, name, len(data))
	respBody, status, err := cl.DoRaw("POST", "/api/v1/volumes/restore", mw.FormDataContentType(), &buf)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("restore error: %s", client.JSONError(respBody))
	}

	var resp struct {
		OK      bool   `json:"ok"`
		JobID   string `json:"job_id"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(respBody, &resp)

	if resp.JobID != "" {
		fmt.Printf("→ Background restore job started (ID: %s), waiting for completion...\n", resp.JobID)
		for {
			time.Sleep(1 * time.Second)
			stBody, stStatus, stErr := cl.Do("GET", "/api/v1/volumes/restore/status?id="+resp.JobID, nil)
			if stErr != nil || stStatus >= 400 {
				continue
			}
			var jobStatus struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal(stBody, &jobStatus); err == nil {
				if jobStatus.Status == "completed" {
					break
				}
				if jobStatus.Status == "failed" {
					return fmt.Errorf("restore failed: %s", jobStatus.Error)
				}
			}
		}
	}
	ui.Success("Volume %s restored successfully", name)
	return nil
}

func runVolumeRemove(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("volume name required: nd volume rm <name>")
	}
	name := args[0]
	data, status, err := cl.Do("DELETE", "/api/v1/volumes/"+name, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	ui.Success("Removed volume %s", name)
	return nil
}

func runVolumePrune(cl *client.Client, args []string) error {
	data, status, err := cl.Do("POST", "/api/v1/volumes/prune", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	var resp struct {
		Output string `json:"output"`
	}
	_ = json.Unmarshal(data, &resp)
	ui.Success("Pruned unused volumes:")
	if resp.Output != "" {
		fmt.Println(resp.Output)
	}
	return nil
}
