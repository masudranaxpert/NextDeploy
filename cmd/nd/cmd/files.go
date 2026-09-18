package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/ui"
)

// FileItem represents a remote file or folder entry.
type FileItem struct {
	Name    string `json:"name"`
	RelPath string `json:"rel_path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	Perms   string `json:"perms,omitempty"`
}

// resolveFileApp resolves the app ID and returns remaining arguments.
func resolveFileApp(args []string) (string, []string, error) {
	var appID string
	var remaining []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		if (a == "-a" || a == "--app") && i+1 < len(args) {
			appID = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--app=") {
			appID = strings.TrimPrefix(a, "--app=")
		} else if strings.HasPrefix(a, "-a=") {
			appID = strings.TrimPrefix(a, "-a=")
		} else {
			remaining = append(remaining, a)
		}
	}

	if appID != "" {
		return appID, remaining, nil
	}

	// If linked project directory, use linked app ID
	pc, err := config.LoadProject(".")
	if err == nil && pc.AppID != "" {
		return pc.AppID, remaining, nil
	}

	// If not linked, check if first remaining argument is the app ID
	if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
		return remaining[0], remaining[1:], nil
	}

	return "", remaining, fmt.Errorf("app_id required (pass --app <id>, specify as argument, or run 'nd link <id>')")
}

// RunFiles dispatches file subcommands: list, read/cat, write/put, edit, rm/delete.
// Usage: nd files [subcommand] [args...]
func RunFiles(cl *client.Client, args []string) error {
	if len(args) > 0 {
		sub := args[0]
		switch sub {
		case "list", "ls":
			return RunFileList(cl, args[1:])
		case "read", "cat", "view":
			return RunFileRead(cl, args[1:])
		case "write", "put":
			return RunFileWrite(cl, args[1:])
		case "edit":
			return RunFileEdit(cl, args[1:])
		case "rm", "delete", "remove":
			return RunFileDelete(cl, args[1:])
		}
	}
	return RunFileList(cl, args)
}

// RunFileList lists files in the workspace (or a subfolder).
// Usage: nd files list [path] [--app <id>] [-r|--recursive] [--json]
func RunFileList(cl *client.Client, rawArgs []string) error {
	var recursive bool
	var jsonOut bool
	var filtered []string

	for _, a := range rawArgs {
		switch a {
		case "-r", "--recursive":
			recursive = true
		case "-j", "--json":
			jsonOut = true
		default:
			filtered = append(filtered, a)
		}
	}

	appID, rest, err := resolveFileApp(filtered)
	if err != nil {
		return err
	}

	var reqPath string
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		reqPath = rest[0]
	}

	pathQuery := url.QueryEscape(reqPath)
	recQuery := "false"
	if recursive {
		recQuery = "true"
	}

	apiURL := fmt.Sprintf("/api/v1/apps/%s/files?path=%s&recursive=%s", url.PathEscape(appID), pathQuery, recQuery)
	body, status, err := cl.Do("GET", apiURL, nil)

	var items []FileItem

	if err == nil && status == 200 {
		_ = json.Unmarshal(body, &items)
	} else {
		// Fallback to MCP file_list tool
		mcpResp, mcpStatus, mcpErr := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name": "file_list",
				"arguments": map[string]interface{}{
					"app_id":    appID,
					"path":      reqPath,
					"recursive": recursive,
				},
			},
		})
		if mcpErr != nil {
			return fmt.Errorf("failed to list files: %w", mcpErr)
		}
		if mcpStatus >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(mcpResp))
		}

		var parsed struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(mcpResp, &parsed); err == nil && len(parsed.Result.Content) > 0 {
			_ = json.Unmarshal([]byte(parsed.Result.Content[0].Text), &items)
		}
	}

	if jsonOut {
		if items == nil {
			items = []FileItem{}
		}
		out, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	displayPath := "/" + strings.Trim(reqPath, "/")
	if displayPath == "/" {
		displayPath = "/ (root)"
	}

	fmt.Printf("App: %s  Path: %s\n", ui.Cyan(appID), ui.Bold(displayPath))
	if len(items) == 0 {
		fmt.Println(ui.Dim("  (directory is empty)"))
		return nil
	}

	fmt.Println()
	fmt.Printf("  %-10s  %-10s  %-19s  %s\n",
		ui.Dim("TYPE"),
		ui.Dim("SIZE"),
		ui.Dim("MODIFIED"),
		ui.Dim("NAME"),
	)
	fmt.Println("  " + strings.Repeat("─", 65))

	dirCount := 0
	fileCount := 0

	for _, it := range items {
		modStr := time.Unix(it.ModTime, 0).Format("2006-01-02 15:04:05")
		if it.IsDir {
			dirCount++
			fmt.Printf("  %-10s  %-10s  %-19s  %s\n",
				ui.Cyan("DIR"),
				ui.Dim("-"),
				modStr,
				ui.Bold(ui.Cyan(it.RelPath+"/")),
			)
		} else {
			fileCount++
			fmt.Printf("  %-10s  %-10s  %-19s  %s\n",
				ui.Dim("FILE"),
				formatFileSize(it.Size),
				modStr,
				it.RelPath,
			)
		}
	}

	fmt.Println("  " + strings.Repeat("─", 65))
	fmt.Printf("  %s %s, %s %s\n\n",
		ui.Bold(fmt.Sprintf("%d", fileCount)), plural(fileCount, "file", "files"),
		ui.Bold(fmt.Sprintf("%d", dirCount)), plural(dirCount, "folder", "folders"),
	)
	return nil
}

// RunFileRead reads and prints remote file content.
// Usage: nd file read <path> [--app <id>] [--output <local_file>] [--full] [--json]
func RunFileRead(cl *client.Client, rawArgs []string) error {
	var outputFile string
	var jsonOut bool
	var fullRead bool
	var filtered []string

	for i := 0; i < len(rawArgs); i++ {
		a := rawArgs[i]
		switch {
		case (a == "-o" || a == "--output") && i+1 < len(rawArgs):
			outputFile = rawArgs[i+1]
			i++
		case strings.HasPrefix(a, "--output="):
			outputFile = strings.TrimPrefix(a, "--output=")
		case strings.HasPrefix(a, "-o="):
			outputFile = strings.TrimPrefix(a, "-o=")
		case a == "--full":
			fullRead = true
		case a == "-j" || a == "--json":
			jsonOut = true
		default:
			filtered = append(filtered, a)
		}
	}

	appID, rest, err := resolveFileApp(filtered)
	if err != nil {
		return err
	}

	if len(rest) < 1 {
		return fmt.Errorf("usage: nd file read <path> [--app <id>] [--output <file>] [--full]")
	}

	filePath := rest[0]
	apiURL := fmt.Sprintf("/api/v1/apps/%s/files/content?path=%s&full=%t",
		url.PathEscape(appID),
		url.QueryEscape(filePath),
		fullRead,
	)

	body, status, err := cl.Do("GET", apiURL, nil)
	var content []byte

	if err == nil && status == 200 {
		content = body
	} else {
		// Fallback to MCP file_read
		mcpResp, mcpStatus, mcpErr := cl.Do("POST", "/mcp", map[string]interface{}{
			"method": "tools/call",
			"params": map[string]interface{}{
				"name": "file_read",
				"arguments": map[string]interface{}{
					"app_id": appID,
					"path":   filePath,
					"full":   fullRead,
				},
			},
		})
		if mcpErr != nil {
			return fmt.Errorf("failed to read file: %w", mcpErr)
		}
		if mcpStatus >= 400 {
			return fmt.Errorf("server error: %s", client.JSONError(mcpResp))
		}

		var parsed struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(mcpResp, &parsed); err != nil || len(parsed.Result.Content) == 0 {
			return fmt.Errorf("empty or invalid file response from server")
		}
		content = []byte(parsed.Result.Content[0].Text)
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, content, 0644); err != nil {
			return fmt.Errorf("failed writing to %s: %w", outputFile, err)
		}
		ui.Success("Saved %s to %s (%s)", ui.Cyan(filePath), ui.Bold(outputFile), formatFileSize(int64(len(content))))
		return nil
	}

	if jsonOut {
		out := map[string]interface{}{
			"app_id":  appID,
			"path":    filePath,
			"size":    len(content),
			"content": string(content),
		}
		formatted, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(formatted))
		return nil
	}

	// Print raw content directly to stdout
	os.Stdout.Write(content)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

// RunFileWrite creates or updates a remote file.
// Usage: nd file write <path> [content_or_file] [--app <id>] [--from <local_file>]
func RunFileWrite(cl *client.Client, rawArgs []string) error {
	var fromFile string
	var filtered []string

	for i := 0; i < len(rawArgs); i++ {
		a := rawArgs[i]
		switch {
		case (a == "--from" || a == "-f") && i+1 < len(rawArgs):
			fromFile = rawArgs[i+1]
			i++
		case strings.HasPrefix(a, "--from="):
			fromFile = strings.TrimPrefix(a, "--from=")
		default:
			filtered = append(filtered, a)
		}
	}

	appID, rest, err := resolveFileApp(filtered)
	if err != nil {
		return err
	}

	if len(rest) < 1 {
		return fmt.Errorf("usage: nd file write <path> [content_or_local_file] [--from <file>] [--app <id>]")
	}

	remotePath := rest[0]
	var content string

	if fromFile != "" {
		b, err := os.ReadFile(fromFile)
		if err != nil {
			return fmt.Errorf("failed reading local file %s: %w", fromFile, err)
		}
		content = string(b)
	} else if len(rest) >= 2 {
		candidate := rest[1]
		// If candidate is an existing local file, read from it
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			b, err := os.ReadFile(candidate)
			if err != nil {
				return fmt.Errorf("failed reading local file %s: %w", candidate, err)
			}
			content = string(b)
		} else {
			// Otherwise treat as literal content
			content = candidate
		}
	} else {
		// Check if input is piped through stdin
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed reading stdin: %w", err)
			}
			content = string(b)
		} else {
			// Interactive terminal without arguments: launch editor
			return RunFileEdit(cl, rawArgs)
		}
	}

	apiURL := fmt.Sprintf("/api/v1/apps/%s/files/content", url.PathEscape(appID))
	payload := map[string]interface{}{
		"path":    remotePath,
		"content": content,
	}

	body, status, err := cl.Do("POST", apiURL, payload)
	if err == nil && status == 200 {
		ui.Success("Saved %s in %s (%s)", ui.Cyan(remotePath), ui.Bold(appID), formatFileSize(int64(len(content))))
		return nil
	}

	// Fallback to MCP file_write
	mcpResp, mcpStatus, mcpErr := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name": "file_write",
			"arguments": map[string]interface{}{
				"app_id":  appID,
				"path":    remotePath,
				"content": content,
			},
		},
	})
	if mcpErr != nil {
		return fmt.Errorf("failed saving file: %w", mcpErr)
	}
	if mcpStatus >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(mcpResp))
	}

	ui.Success("Saved %s in %s (%s)", ui.Cyan(remotePath), ui.Bold(appID), formatFileSize(int64(len(content))))
	_ = body
	return nil
}

// RunFileEdit opens remote file in $EDITOR and writes back changes on save.
// Usage: nd file edit <path> [--app <id>]
func RunFileEdit(cl *client.Client, rawArgs []string) error {
	appID, rest, err := resolveFileApp(rawArgs)
	if err != nil {
		return err
	}

	if len(rest) < 1 {
		return fmt.Errorf("usage: nd file edit <remote_path> [--app <id>]")
	}

	remotePath := rest[0]

	// Fetch existing content if present
	var initialContent []byte
	apiURL := fmt.Sprintf("/api/v1/apps/%s/files/content?path=%s&full=true",
		url.PathEscape(appID),
		url.QueryEscape(remotePath),
	)
	body, status, err := cl.Do("GET", apiURL, nil)
	if err == nil && status == 200 {
		initialContent = body
	}

	ext := filepath.Ext(remotePath)
	tmpFile, err := os.CreateTemp("", "nd-edit-*"+ext)
	if err != nil {
		return fmt.Errorf("failed creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if len(initialContent) > 0 {
		if _, err := tmpFile.Write(initialContent); err != nil {
			tmpFile.Close()
			return err
		}
	}
	tmpFile.Close()

	// Determine editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			for _, ed := range []string{"nano", "vim", "vi"} {
				if pth, err := exec.LookPath(ed); err == nil && pth != "" {
					editor = ed
					break
				}
			}
			if editor == "" {
				editor = "vi"
			}
		}
	}

	parts := strings.Fields(editor)
	edCmd := parts[0]
	edArgs := append(parts[1:], tmpPath)

	cmd := exec.Command(edCmd, edArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ui.Step("Opening %s in %s...", ui.Cyan(remotePath), ui.Bold(editor))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor error: %w", err)
	}

	newBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed reading saved file: %w", err)
	}

	if bytes.Equal(initialContent, newBytes) {
		ui.Warn("No changes made to %s.", remotePath)
		return nil
	}

	// Upload new content
	payload := map[string]interface{}{
		"path":    remotePath,
		"content": string(newBytes),
	}
	saveURL := fmt.Sprintf("/api/v1/apps/%s/files/content", url.PathEscape(appID))
	_, status, err = cl.Do("POST", saveURL, payload)
	if err == nil && status == 200 {
		ui.Success("Successfully updated %s in %s (%s)", ui.Cyan(remotePath), ui.Bold(appID), formatFileSize(int64(len(newBytes))))
		return nil
	}

	// Fallback to MCP
	mcpResp, mcpStatus, mcpErr := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name": "file_write",
			"arguments": map[string]interface{}{
				"app_id":  appID,
				"path":    remotePath,
				"content": string(newBytes),
			},
		},
	})
	if mcpErr != nil {
		return fmt.Errorf("failed uploading changes: %w", mcpErr)
	}
	if mcpStatus >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(mcpResp))
	}

	ui.Success("Successfully updated %s in %s (%s)", ui.Cyan(remotePath), ui.Bold(appID), formatFileSize(int64(len(newBytes))))
	return nil
}

// RunFileDelete removes a remote file or directory.
// Usage: nd file rm <path> [--app <id>] [-f|--force] [-r|--recursive]
func RunFileDelete(cl *client.Client, rawArgs []string) error {
	var force bool
	var recursive bool
	var filtered []string

	for _, a := range rawArgs {
		switch a {
		case "-f", "-y", "--force", "--yes":
			force = true
		case "-r", "--recursive":
			recursive = true
		default:
			filtered = append(filtered, a)
		}
	}

	appID, rest, err := resolveFileApp(filtered)
	if err != nil {
		return err
	}

	if len(rest) < 1 {
		return fmt.Errorf("usage: nd file rm <path> [--app <id>] [-f] [-r]")
	}

	remotePath := rest[0]

	if !force {
		fmt.Printf("Are you sure you want to delete '%s' from app %s? [y/N]: ", ui.Yellow(remotePath), ui.Cyan(appID))
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		confirm = strings.ToLower(strings.TrimSpace(confirm))
		if confirm != "y" && confirm != "yes" {
			ui.Warn("Delete cancelled.")
			return nil
		}
	}

	apiURL := fmt.Sprintf("/api/v1/apps/%s/files?path=%s",
		url.PathEscape(appID),
		url.QueryEscape(remotePath),
	)

	body, status, err := cl.Do("DELETE", apiURL, nil)
	if err == nil && status == 200 {
		ui.Success("Deleted %s from %s", ui.Cyan(remotePath), ui.Bold(appID))
		return nil
	}

	// Fallback to MCP file_delete
	mcpResp, mcpStatus, mcpErr := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name": "file_delete",
			"arguments": map[string]interface{}{
				"app_id": appID,
				"path":   remotePath,
			},
		},
	})
	if mcpErr != nil {
		return fmt.Errorf("failed deleting file: %w", mcpErr)
	}
	if mcpStatus >= 400 {
		return fmt.Errorf("server error: %s", client.JSONError(mcpResp))
	}

	ui.Success("Deleted %s from %s", ui.Cyan(remotePath), ui.Bold(appID))
	_ = body
	_ = recursive
	return nil
}

// RunFolder handles folder operations (specifically deletion / rm).
// Usage: nd folder rm <path> [--app <id>] [-f]
func RunFolder(cl *client.Client, rawArgs []string) error {
	if len(rawArgs) > 0 {
		sub := rawArgs[0]
		switch sub {
		case "rm", "delete", "remove":
			return RunFolderDelete(cl, rawArgs[1:])
		}
	}
	return RunFolderDelete(cl, rawArgs)
}

// RunFolderDelete deletes a remote folder and all its contents.
// Usage: nd folder rm <folder_path> [--app <id>] [-f]
func RunFolderDelete(cl *client.Client, rawArgs []string) error {
	argsWithRecursive := append([]string{"-r"}, rawArgs...)
	return RunFileDelete(cl, argsWithRecursive)
}

func formatFileSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
