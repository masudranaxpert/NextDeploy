package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nd/internal/ui"
)

// GitHubRelease represents a GitHub release payload.
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// compareSemver returns: 1 if a > b, -1 if a < b, 0 if a == b.
func compareSemver(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			_, _ = fmt.Sscanf(aParts[i], "%d", &aNum)
		}
		if i < len(bParts) {
			_, _ = fmt.Sscanf(bParts[i], "%d", &bNum)
		}
		if aNum > bNum {
			return 1
		}
		if aNum < bNum {
			return -1
		}
	}
	return 0
}

// RunUpdate checks for the latest GitHub release and updates the nd binary in-place.
func RunUpdate(args []string) error {
	checkOnly := false
	force := false
	for _, arg := range args {
		if arg == "--check" || arg == "-c" {
			checkOnly = true
		}
		if arg == "--force" || arg == "-f" {
			force = true
		}
	}

	ui.Step("Checking for updates from GitHub...")

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/masudranaxpert/NextDeploy/releases/latest", nil)
	if err != nil {
		return fmt.Errorf("failed to create update request: %w", err)
	}
	req.Header.Set("User-Agent", "nd-cli/"+Version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update check returned status %d", resp.StatusCode)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("failed to parse release information: %w", err)
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")
	currentVersion := strings.TrimPrefix(Version, "v")

	if !force && currentVersion != "dev" && (compareSemver(latestVersion, currentVersion) <= 0 || latestVersion == "") {
		ui.Success("nd is already up to date (%s)", ui.Cyan("v"+currentVersion))
		return nil
	}

	if checkOnly {
		ui.Success("Update available: %s -> %s (Run 'nd update' to install)", ui.Dim("v"+currentVersion), ui.Green("v"+latestVersion))
		return nil
	}

	ui.Step("Found newer version %s (current: %s)", ui.Green("v"+latestVersion), ui.Dim("v"+currentVersion))

	// Find the matching binary asset for current OS/arch
	targetName := fmt.Sprintf("nd-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		targetName += ".exe"
	}

	var downloadURL string
	for _, asset := range rel.Assets {
		if asset.Name == targetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary release asset found for %s (%s). Visit https://github.com/masudranaxpert/NextDeploy/releases to download manually", targetName, rel.TagName)
	}

	ui.Step("Downloading %s...", targetName)
	dlReq, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return err
	}
	dlReq.Header.Set("User-Agent", "nd-cli/"+Version)

	dlResp, err := client.Do(dlReq)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", dlResp.StatusCode)
	}

	// Current executable location
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("could not resolve executable path: %w", err)
	}

	dir := filepath.Dir(execPath)
	tmpFile, err := os.CreateTemp(dir, "nd-update-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary update file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmpFile, dlResp.Body); err != nil {
		return fmt.Errorf("failed to write update file: %w", err)
	}
	_ = tmpFile.Close()

	// Make executable on unix
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmpName, 0755)
	}

	// In-place replacement
	if runtime.GOOS == "windows" {
		oldPath := execPath + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(execPath, oldPath); err != nil {
			return fmt.Errorf("failed to move existing binary: %w", err)
		}
		if err := os.Rename(tmpName, execPath); err != nil {
			// Rollback
			_ = os.Rename(oldPath, execPath)
			return fmt.Errorf("failed to replace binary: %w", err)
		}
		_ = os.Remove(oldPath)
	} else {
		if err := os.Rename(tmpName, execPath); err != nil {
			return fmt.Errorf("failed to replace binary: %w", err)
		}
	}

	ui.Success("Successfully updated nd to %s!", ui.HiGreen("v"+latestVersion))
	return nil
}
