package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/ui"
)

// RunPull downloads and extracts the remote application workspace to a local directory.
// Usage: nd pull [app_id] [target_dir] [--force|-f]
func RunPull(cl *client.Client, rawArgs []string) error {
	force := false
	var flagAppID string
	var args []string

	for i := 0; i < len(rawArgs); i++ {
		a := rawArgs[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Println("Usage: nd pull [app_id] [target_dir] [--force|-f]")
			fmt.Println("\nDownload the complete remote workspace (or a subfolder) to your local machine.")
			fmt.Println("\nFlags:")
			fmt.Println("  -a, --app <app_id>   Target application ID (optional if linked)")
			fmt.Println("  -f, --force, -y      Overwrite existing local files without prompt")
			fmt.Println("\nExamples:")
			fmt.Println("  nd pull                      # Pull linked app to current directory")
			fmt.Println("  nd pull myapp                # Pull 'myapp' to current directory")
			fmt.Println("  nd pull myapp ./myapp-src    # Pull 'myapp' into ./myapp-src directory")
			return nil
		case a == "-f" || a == "--force" || a == "-y" || a == "--yes":
			force = true
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
	targetDir := "."

	if flagAppID != "" {
		appID = flagAppID
		if len(args) >= 1 {
			targetDir = args[0]
		}
	} else if len(args) == 0 {
		pc, err := config.LoadProject(".")
		if err != nil || pc.AppID == "" {
			return fmt.Errorf("app_id required: nd pull [app_id] [target_dir] (or run 'nd link <app_id>' first)")
		}
		appID = pc.AppID
		targetDir = "."
	} else if len(args) == 1 {
		pc, lerr := config.LoadProject(".")
		if lerr == nil && pc.AppID != "" {
			// If already linked, 1 argument is the destination directory
			appID = pc.AppID
			targetDir = args[0]
		} else {
			appID = args[0]
			targetDir = "."
		}
	} else {
		appID = args[0]
		targetDir = args[1]
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("invalid target directory: %w", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(absTarget, 0755); err != nil {
		return fmt.Errorf("failed creating directory %s: %w", absTarget, err)
	}

	// Check if directory has existing files
	entries, _ := os.ReadDir(absTarget)
	hasNonConfigEntries := false
	for _, e := range entries {
		name := e.Name()
		if name != ".nd" && name != ".git" {
			hasNonConfigEntries = true
			break
		}
	}

	if hasNonConfigEntries && !force {
		fmt.Printf("Destination '%s' contains existing files.\nPull will merge and overwrite matching files (excluding local .env and .nd/ config).\nProceed? [y/N]: ", ui.Cyan(targetDir))
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		confirm = strings.ToLower(strings.TrimSpace(confirm))
		if confirm != "y" && confirm != "yes" {
			ui.Warn("Pull cancelled.")
			return nil
		}
	}

	ui.Step("Downloading workspace archive for %s...", ui.Cyan(appID))
	apiURL := fmt.Sprintf("/api/v1/apps/%s/workspace/archive", url.PathEscape(appID))
	body, status, err := cl.Do("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("server error (%d): %s", status, client.JSONError(body))
	}

	gzReader, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("invalid gzip archive: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	extractedCount := 0
	var totalBytes int64

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("archive read error: %w", err)
		}

		if !isSafeTarPath(header.Name) {
			continue // Prevent path traversal
		}
		cleanName := filepath.Clean(filepath.ToSlash(header.Name))

		// Strictly protect local configuration and credentials
		base := filepath.Base(cleanName)
		if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasPrefix(cleanName, ".nd/") || cleanName == ".nd" {
			continue
		}

		destPath := filepath.Join(absTarget, filepath.FromSlash(cleanName))

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed creating directory %s: %w", destPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			dir := filepath.Dir(destPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed creating parent directory for %s: %w", destPath, err)
			}

			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return fmt.Errorf("failed creating file %s: %w", destPath, err)
			}

			n, err := io.Copy(outFile, tarReader)
			_ = outFile.Close()
			if err != nil {
				return fmt.Errorf("failed writing file %s: %w", destPath, err)
			}
			extractedCount++
			totalBytes += n
		}
	}

	ui.Success("Successfully pulled %d file(s) (%s) into %s",
		extractedCount, formatFileSize(totalBytes), ui.Bold(targetDir))

	// If directory was not linked to this app, auto-link it
	pc, err := config.LoadProject(absTarget)
	if err != nil || pc.AppID == "" {
		_ = config.SaveProject(absTarget, appID)
		ui.Step("Linked directory to app %s (saved in .nd/project.json)", ui.Cyan(appID))
	}

	return nil
}

// isSafeTarPath validates that an archive path does not escape the target root.
func isSafeTarPath(name string) bool {
	cleanName := filepath.Clean(filepath.ToSlash(name))
	if cleanName == "" || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") || filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, "/") {
		return false
	}
	return true
}
