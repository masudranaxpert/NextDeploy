package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nd/internal/client"
	"nd/internal/ui"
)

// gitSyncState mirrors the server's git_sync status/result payload.
type gitSyncState struct {
	AppID          string `json:"app_id"`
	Configured     bool   `json:"configured"`
	Active         bool   `json:"active"`
	Paused         bool   `json:"paused"`
	Source         string `json:"source"`
	RepoURL        string `json:"repo_url"`
	Branch         string `json:"branch"`
	CheckoutExists bool   `json:"checkout_exists"`
	CheckoutDirty  bool   `json:"checkout_dirty"`
}

type gitSyncResult struct {
	Status    gitSyncState `json:"status"`
	Changed   bool         `json:"changed"`
	Copied    int          `json:"files_copied"`
	KeptNewer int          `json:"files_kept_newer_in_workspace"`
	Unchanged int          `json:"files_unchanged"`
	Notes     []string     `json:"notes"`
	NextStep  string       `json:"next_step"`
}

// callMCPTool invokes one MCP tool and returns its text payload. Tool errors (isError) become Go errors.
func callMCPTool(cl *client.Client, name string, args map[string]interface{}) (string, error) {
	body, status, err := cl.Do("POST", "/mcp", map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{"name": name, "arguments": args},
	})
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("server error: %s", client.JSONError(body))
	}
	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("unexpected server response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", errors.New(resp.Error.Message)
	}
	text := ""
	if len(resp.Result.Content) > 0 {
		text = resp.Result.Content[0].Text
	}
	if resp.Result.IsError {
		return "", errors.New(strings.TrimSpace(text))
	}
	return text, nil
}

// fetchGitSyncState returns the app's source mode. ok is false when the server cannot tell (older server).
func fetchGitSyncState(cl *client.Client, appID string) (st gitSyncState, ok bool) {
	text, err := callMCPTool(cl, "git_sync", map[string]interface{}{"app_id": appID, "action": "status"})
	if err != nil {
		return st, false
	}
	if json.Unmarshal([]byte(text), &st) != nil {
		return st, false
	}
	return st, true
}

// guardGitSourceForPush stops `nd push` on an app whose deploy source is Git: the pushed files would land in
// the workspace but deploys would build the repository version, so a "successful" push would be misleading.
// Older servers without the git_sync tool, and unknown states, fall through silently.
func guardGitSourceForPush(cl *client.Client, appID string) error {
	st, ok := fetchGitSyncState(cl, appID)
	if !ok || !st.Active || !st.Configured {
		return nil
	}
	return fmt.Errorf("app %s deploys from Git (%s, branch %s), so pushed files would not be deployed.\n"+
		"  Turn Git sync off first:  nd git off %s\n"+
		"  (the repository settings are kept; turn it back on with: nd git on %s)",
		appID, st.RepoURL, st.Branch, appID, appID)
}

// RunGit shows or changes whether an app deploys from its Git repository.
// Usage: nd git [status|off|on] [app_id] [--force] [--json]
func RunGit(cl *client.Client, args []string) error {
	action := "status"
	force := false
	jsonOut := false
	var clean []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-h" || a == "--help":
			printGitHelp()
			return nil
		case a == "--force" || a == "-f":
			force = true
		case a == "--json" || a == "-j":
			jsonOut = true
		default:
			clean = append(clean, a)
		}
	}
	if len(clean) > 0 {
		switch strings.ToLower(clean[0]) {
		case "status", "show":
			action, clean = "status", clean[1:]
		case "off", "disable", "pause", "detach":
			action, clean = "off", clean[1:]
		case "on", "enable", "resume", "attach":
			action, clean = "on", clean[1:]
		}
	}
	appID, _, err := resolveAppID(clean)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{"app_id": appID, "action": action}
	if force {
		payload["force"] = true
	}
	text, err := callMCPTool(cl, "git_sync", payload)
	if err != nil {
		if msg := strings.ToLower(err.Error()); strings.Contains(msg, "unknown tool") {
			return errors.New("this server does not support Git sync control yet; update NextDeploy")
		}
		return err
	}
	if jsonOut {
		fmt.Println(text)
		return nil
	}

	if action == "status" {
		var st gitSyncState
		if err := json.Unmarshal([]byte(text), &st); err != nil {
			return fmt.Errorf("unexpected server response: %w", err)
		}
		printGitSyncState(appID, st)
		return nil
	}

	var res gitSyncResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return fmt.Errorf("unexpected server response: %w", err)
	}
	if !res.Changed {
		for _, n := range res.Notes {
			fmt.Println(n)
		}
		printGitSyncState(appID, res.Status)
		return nil
	}
	if action == "off" {
		ui.Success("Git sync is OFF for %s", ui.Cyan(appID))
	} else {
		ui.Success("Git sync is ON for %s", ui.Cyan(appID))
	}
	for _, n := range res.Notes {
		fmt.Println("  " + n)
	}
	if res.NextStep != "" {
		fmt.Println("  " + res.NextStep)
	}
	return nil
}

func printGitSyncState(appID string, st gitSyncState) {
	switch {
	case !st.Configured && st.Active:
		fmt.Printf("%s: source is Git (no repository details stored). Connect a repository in the panel (Git tab).\n", appID)
	case !st.Configured:
		fmt.Printf("%s: no Git repository connected; deploys use the workspace files.\n", appID)
	case st.Active:
		fmt.Printf("%s: Git sync ON  (%s, branch %s)\n", appID, st.RepoURL, st.Branch)
		fmt.Println("  Deploys and webhook pushes pull from the repository. `nd push` is blocked; use `nd git off` first.")
		if st.CheckoutDirty {
			ui.Warn("  The Git checkout has uncommitted edits (from `nd file write`); the next deploy keeps them, a webhook or panel deploy discards them.")
		}
	default:
		fmt.Printf("%s: Git sync OFF (%s, branch %s kept)\n", appID, st.RepoURL, st.Branch)
		fmt.Println("  Deploys use the workspace files; webhook pushes are ignored. Turn it back on with `nd git on`.")
	}
}

func printGitHelp() {
	fmt.Println("Usage: nd git [status|off|on] [app_id] [--force] [--json]")
	fmt.Println()
	fmt.Println("Choose whether an app deploys from its connected Git repository or from the workspace files.")
	fmt.Println("The repository settings are never deleted; turning Git sync off only pauses them.")
	fmt.Println()
	fmt.Println("  nd git                 Show the current source (alias: status)")
	fmt.Println("  nd git off             Deploy from the workspace (nd push / file edits). Webhooks are ignored.")
	fmt.Println("                         Files only present in the Git checkout are copied into the workspace once.")
	fmt.Println("  nd git on [--force]    Deploy from Git again. Refused while the Git checkout has uncommitted")
	fmt.Println("                         edits unless --force (the next pull discards them).")
	fmt.Println()
	fmt.Println("Typical flow for a Git-connected app:  nd git off  ->  nd push --deploy  (later: nd git on)")
}
