package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
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

// RunGit manages Git integration for an app (status, sync on/off, connect, pull/sync, push, disconnect, providers).
// Usage: nd git [status|off|on|providers|connect|sync|pull|push|disconnect] [args...]
func RunGit(cl *client.Client, args []string) error {
	action := "status"
	force := false
	jsonOut := false
	publicMode := false
	noAutoDeploy := false
	skipSync := false
	var branch, msg, repoURL string
	var providerID int64
	limit := 20

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
		case a == "--public":
			publicMode = true
		case a == "--no-auto-deploy":
			noAutoDeploy = true
		case a == "--skip-sync":
			skipSync = true
		case (a == "--limit" || a == "-n") && i+1 < len(args):
			i++
			if v, err := strconv.Atoi(args[i]); err == nil && v > 0 {
				limit = v
			}
		case (a == "--branch" || a == "-b") && i+1 < len(args):
			i++
			branch = args[i]
		case (a == "--message" || a == "-m") && i+1 < len(args):
			i++
			msg = args[i]
		case (a == "--repo" || a == "-r") && i+1 < len(args):
			i++
			repoURL = args[i]
		case (a == "--provider" || a == "-p") && i+1 < len(args):
			i++
			_, _ = fmt.Sscanf(args[i], "%d", &providerID)
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
		case "providers", "provider":
			return runGitProviders(cl, jsonOut)
		case "repos", "repo":
			return runGitRepos(cl, clean[1:], providerID, jsonOut)
		case "branches", "branch":
			return runGitBranches(cl, clean[1:], providerID, repoURL, jsonOut)
		case "webhooks", "webhook", "deliveries":
			return runGitWebhooks(cl, limit, jsonOut)
		case "connect":
			return runGitConnect(cl, clean[1:], branch, providerID, publicMode, !noAutoDeploy, skipSync, jsonOut)
		case "sync", "pull":
			return runGitSyncPull(cl, clean[1:], jsonOut)
		case "push":
			return runGitPush(cl, clean[1:], repoURL, branch, msg, providerID, publicMode, force, jsonOut)
		case "disconnect", "rm", "remove":
			return runGitDisconnect(cl, clean[1:], jsonOut)
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
	fmt.Println("Usage: nd git [subcommand] [app_id] [flags]")
	fmt.Println()
	fmt.Println("Manage Git repositories and sync behavior for an app or PHP/WordPress site.")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  status [app_id]                  Show the current Git/workspace deploy source (default)")
	fmt.Println("  off [app_id]                     Deploy from workspace files (nd push / edits); pauses webhooks")
	fmt.Println("  on [app_id] [--force]            Deploy from Git again (--force discards local edits)")
	fmt.Println("  providers                        List connected GitHub/GitLab accounts")
	fmt.Println("  repos <provider_id> [app_id]     List repositories accessible to a connected provider")
	fmt.Println("  branches [app_id] [--provider <id>] [--repo <url|owner/repo>]")
	fmt.Println("                                   List branches from a provider or public remote URL")
	fmt.Println("  webhooks [--limit N]             Show recent webhook deliveries")
	fmt.Println("  connect [app_id] <repo_url>      Connect a repository (--branch, --provider, --public, --no-auto-deploy)")
	fmt.Println("  sync|pull [app_id]               Pull latest commits from the connected repository")
	fmt.Println("  push [app_id] [-m <msg>]         Commit & push app workspace (or WordPress htdocs/) to Git")
	fmt.Println("                                   Supports --repo <url>, --branch <br>, --provider <id>, --public, --force")
	fmt.Println("  disconnect [app_id]              Remove Git configuration from the app")
	fmt.Println()
	fmt.Println("Flags: --json (-j), --force (-f)")
}

func looksLikeRepoURL(s string) bool {
	if strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "ssh://") {
		return true
	}
	if !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, ".") && strings.Count(s, "/") == 1 {
		parts := strings.SplitN(s, "/", 2)
		return parts[0] != "" && parts[1] != ""
	}
	return false
}

func doGitAPI(cl *client.Client, method, path string, payload any, jsonOut bool) (map[string]any, error) {
	body, status, err := cl.Do(method, path, payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%s", client.JSONError(body))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(body)))
	}
	var res map[string]any
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("unexpected response: %s", strings.TrimSpace(string(body)))
	}
	return res, nil
}

func runGitProviders(cl *client.Client, jsonOut bool) error {
	res, err := doGitAPI(cl, "GET", "/api/v1/git/providers", nil, jsonOut)
	if err != nil {
		return err
	}
	if jsonOut {
		return nil
	}
	items, _ := res["providers"].([]any)
	if len(items) == 0 {
		fmt.Println("No Git providers connected. Connect GitHub/GitLab in the web panel.")
		return nil
	}
	fmt.Printf("%-4s %-10s %-20s %-18s %-10s %s\n", "ID", "PROVIDER", "ACCOUNT", "METHOD", "STATUS", "REPOS")
	for _, raw := range items {
		m, _ := raw.(map[string]any)
		account := fmt.Sprint(m["account_login"])
		if account == "" || account == "<nil>" {
			account = fmt.Sprint(m["name"])
		}
		method := fmt.Sprint(m["auth_method"])
		if method == "" || method == "<nil>" {
			method = fmt.Sprint(m["type"])
		}
		status := fmt.Sprint(m["status"])
		if status == "" || status == "<nil>" {
			status = "connected"
		}
		repos := m["repos_accessible"]
		if repos == nil {
			repos = "-"
		}
		fmt.Printf("%-4v %-10v %-20v %-18v %-10v %v\n", m["id"], m["provider"], account, method, status, repos)
	}
	return nil
}

func runGitConnect(cl *client.Client, clean []string, branch string, providerID int64, publicMode, autoDeploy, skipSync, jsonOut bool) error {
	var appArgs []string
	var repoURL string
	switch len(clean) {
	case 0:
		return fmt.Errorf("usage: nd git connect [app_id] <repo_url> [--branch <br>] [--provider <id>] [--public]")
	case 1:
		repoURL = clean[0]
	default:
		if looksLikeRepoURL(clean[0]) {
			repoURL = clean[0]
			appArgs = clean[1:]
		} else {
			appArgs = []string{clean[0]}
			repoURL = clean[1]
		}
	}
	appID, _, err := resolveAppID(appArgs)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"repo_url":    repoURL,
		"branch":      branch,
		"auto_deploy": autoDeploy,
		"skip_sync":   skipSync,
	}
	if providerID > 0 {
		payload["git_provider_id"] = providerID
	}
	if publicMode {
		payload["auth_mode"] = "public"
	}
	res, err := doGitAPI(cl, "POST", "/api/v1/apps/"+appID+"/git/connect", payload, jsonOut)
	if err != nil {
		return err
	}
	if !jsonOut {
		fmt.Printf("Connected %s to %s\n", appID, repoURL)
		if st, ok := res["status"].(map[string]any); ok {
			if raw, err := json.Marshal(st); err == nil {
				var state gitSyncState
				if json.Unmarshal(raw, &state) == nil {
					printGitSyncState(appID, state)
				}
			}
		}
	}
	return nil
}

func runGitSyncPull(cl *client.Client, clean []string, jsonOut bool) error {
	appID, _, err := resolveAppID(clean)
	if err != nil {
		return err
	}
	res, err := doGitAPI(cl, "POST", "/api/v1/apps/"+appID+"/git/sync", map[string]any{}, jsonOut)
	if err != nil {
		return err
	}
	if !jsonOut {
		fmt.Printf("Synced %s\n", appID)
		if out, _ := res["output"].(string); out != "" {
			fmt.Println(out)
		}
	}
	return nil
}

func runGitPush(cl *client.Client, clean []string, repoURL, branch, msg string, providerID int64, publicMode, force, jsonOut bool) error {
	var appArgs []string
	if repoURL == "" {
		switch len(clean) {
		case 0:
		case 1:
			if looksLikeRepoURL(clean[0]) {
				repoURL = clean[0]
			} else {
				appArgs = clean
			}
		default:
			if looksLikeRepoURL(clean[0]) {
				repoURL = clean[0]
				appArgs = clean[1:]
			} else if looksLikeRepoURL(clean[1]) {
				appArgs = []string{clean[0]}
				repoURL = clean[1]
			} else {
				appArgs = clean
			}
		}
	} else {
		appArgs = clean
	}
	appID, _, err := resolveAppID(appArgs)
	if err != nil {
		return err
	}
	payload := map[string]any{}
	if repoURL != "" {
		payload["repo_url"] = repoURL
	}
	if branch != "" {
		payload["branch"] = branch
	}
	if msg != "" {
		payload["message"] = msg
	}
	if providerID > 0 {
		payload["git_provider_id"] = providerID
	}
	if publicMode {
		payload["auth_mode"] = "public"
	}
	if force {
		payload["force"] = true
	}
	res, err := doGitAPI(cl, "POST", "/api/v1/apps/"+appID+"/git/push", payload, jsonOut)
	if err != nil {
		return err
	}
	if !jsonOut {
		fmt.Printf("Pushed %s -> %v (%v)\n", appID, res["repo_url"], res["branch"])
		if c, _ := res["commit"].(string); c != "" {
			fmt.Printf("Commit: %s\n", c)
		}
		if isPHP, _ := res["is_php"].(bool); isPHP {
			fmt.Println("Note: PHP/WordPress htdocs/ was pushed (.gitignore protects wp-config.php and .env).")
		}
	}
	return nil
}

func runGitDisconnect(cl *client.Client, clean []string, jsonOut bool) error {
	appID, _, err := resolveAppID(clean)
	if err != nil {
		return err
	}
	_, err = doGitAPI(cl, "DELETE", "/api/v1/apps/"+appID+"/git", nil, jsonOut)
	if err != nil {
		return err
	}
	if !jsonOut {
		fmt.Printf("Disconnected Git repository from %s\n", appID)
	}
	return nil
}

func runGitRepos(cl *client.Client, clean []string, providerID int64, jsonOut bool) error {
	rest := clean
	if providerID <= 0 && len(rest) > 0 {
		id, err := strconv.ParseInt(strings.TrimSpace(rest[0]), 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("usage: nd git repos <provider_id> [app_id] [--json]")
		}
		providerID = id
		rest = rest[1:]
	}
	if providerID <= 0 {
		return fmt.Errorf("provider ID required: nd git repos <provider_id> [app_id]")
	}
	appID, _, err := resolveAppID(rest)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/v1/apps/%s/git/providers/%d/repos", url.PathEscape(appID), providerID)
	res, err := doGitAPI(cl, "GET", path, nil, jsonOut)
	if err != nil {
		return err
	}
	if jsonOut {
		return nil
	}
	items, _ := res["repos"].([]any)
	if len(items) == 0 {
		fmt.Println("No repositories found for this provider.")
		return nil
	}
	fmt.Printf("%-36s %-14s %-10s %s\n", "REPOSITORY", "DEFAULT BRANCH", "VISIBILITY", "CLONE URL")
	for _, raw := range items {
		m, _ := raw.(map[string]any)
		full := fmt.Sprint(m["full_name"])
		if full == "" || full == "<nil>" {
			full = fmt.Sprint(m["name"])
		}
		defBr := fmt.Sprint(m["default_branch"])
		if defBr == "" || defBr == "<nil>" {
			defBr = "-"
		}
		vis := "public"
		if priv, _ := m["private"].(bool); priv {
			vis = "private"
		}
		cloneURL := fmt.Sprint(m["clone_url"])
		if cloneURL == "<nil>" {
			cloneURL = ""
		}
		fmt.Printf("%-36s %-14s %-10s %s\n", full, defBr, vis, cloneURL)
	}
	return nil
}

func runGitBranches(cl *client.Client, clean []string, providerID int64, repoFlag string, jsonOut bool) error {
	var appArgs []string
	repo := strings.TrimSpace(repoFlag)
	for _, a := range clean {
		if repo == "" && looksLikeRepoURL(a) {
			repo = a
		} else {
			appArgs = append(appArgs, a)
		}
	}
	if repo == "" {
		return fmt.Errorf("repository required: nd git branches [app_id] --repo <url|owner/repo> [--provider <id>]")
	}
	appID, _, err := resolveAppID(appArgs)
	if err != nil {
		return err
	}
	var path string
	if providerID > 0 {
		path = fmt.Sprintf("/api/v1/apps/%s/git/providers/%d/branches?repo=%s",
			url.PathEscape(appID), providerID, url.QueryEscape(repo))
	} else {
		path = fmt.Sprintf("/api/v1/apps/%s/git/branches?url=%s",
			url.PathEscape(appID), url.QueryEscape(repo))
	}
	res, err := doGitAPI(cl, "GET", path, nil, jsonOut)
	if err != nil {
		return err
	}
	if jsonOut {
		return nil
	}
	items, _ := res["branches"].([]any)
	if len(items) == 0 {
		fmt.Println("No branches found.")
		return nil
	}
	for _, b := range items {
		fmt.Println(fmt.Sprint(b))
	}
	return nil
}

func runGitWebhooks(cl *client.Client, limit int, jsonOut bool) error {
	if limit <= 0 {
		limit = 20
	}
	body, status, err := cl.Do("GET", "/api/v1/git/webhooks?limit="+strconv.Itoa(limit), nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("%s", client.JSONError(body))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(body)))
		return nil
	}
	var deliveries []struct {
		AppID     string `json:"app_id"`
		Provider  string `json:"provider"`
		Repo      string `json:"repo"`
		Event     string `json:"event"`
		SHA       string `json:"sha"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(body, &deliveries); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	if len(deliveries) == 0 {
		fmt.Println("No webhook deliveries recorded yet.")
		return nil
	}
	fmt.Printf("%-18s %-8s %-28s %-10s %-10s %s\n", "APP", "EVENT", "REPO", "SHA", "STATUS", "AT")
	for _, d := range deliveries {
		sha := d.SHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		fmt.Printf("%-18s %-8s %-28s %-10s %-10s %s\n", d.AppID, d.Event, d.Repo, sha, d.Status, d.CreatedAt)
	}
	return nil
}
