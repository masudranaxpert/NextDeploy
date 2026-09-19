package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/gitx"
	"panel/internal/handlers"
	"panel/internal/handlers/utils"
	"panel/internal/runutil"
	"panel/internal/sandbox"
	"panel/internal/workspace"

	"gopkg.in/yaml.v3"
)

// Handler handles execution of MCP tools.
type Handler struct {
	p *handlers.Panel
}

// NewHandler creates a new tool execution handler.
func NewHandler(p *handlers.Panel) *Handler {
	return &Handler{p: p}
}

// hasAppAccess validates that the user has the required permission level (viewer or developer) to access the specified app.
func (h *Handler) hasAppAccess(ctx context.Context, u db.User, appID string, requiredRole string) (db.App, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return db.App{}, errors.New("app_id is required")
	}
	app, err := h.p.DB.GetApp(ctx, appID)
	if err != nil {
		return db.App{}, fmt.Errorf("app %q not found", appID)
	}

	allowed, err := h.p.CanAccessApp(ctx, u.ID, u.Role, appID, requiredRole)
	if err != nil {
		return db.App{}, err
	}
	if !allowed {
		return db.App{}, fmt.Errorf("forbidden: requires %s access to app %q", requiredRole, appID)
	}

	if requiredRole != db.CollabRoleViewer && app.Status == db.AppStatusSuspended && u.Role != db.RoleAdmin {
		return db.App{}, fmt.Errorf("forbidden: app %q is suspended and cannot be modified", appID)
	}

	return app, nil
}

// CallTool executes the requested tool on behalf of the user.
func (h *Handler) CallTool(ctx context.Context, u db.User, params CallToolParams) (CallToolResult, error) {
	switch params.Name {
	case "app_list":
		return h.handleAppList(ctx, u)
	case "app_get":
		return h.handleAppGet(ctx, u, params.Arguments)
	case "app_create":
		return h.handleAppCreate(ctx, u, params.Arguments)
	case "app_delete":
		return h.handleAppDelete(ctx, u, params.Arguments)
	case "workspace_manifest":
		return h.handleWorkspaceManifest(ctx, u, params.Arguments)
	case "workspace_apply":
		return h.handleWorkspaceApply(ctx, u, params.Arguments)
	case "file_read":
		return h.handleFileRead(ctx, u, params.Arguments)
	case "file_write":
		return h.handleFileWrite(ctx, u, params.Arguments)
	case "file_delete":
		return h.handleFileDelete(ctx, u, params.Arguments)
	case "file_patch":
		return h.handleFilePatch(ctx, u, params.Arguments)
	case "file_search":
		return h.handleFileSearch(ctx, u, params.Arguments)
	case "env_list":
		return h.handleEnvList(ctx, u, params.Arguments)
	case "env_reveal":
		return h.handleEnvReveal(ctx, u, params.Arguments)
	case "env_set":
		return h.handleEnvSet(ctx, u, params.Arguments)
	case "compose_get":
		return h.handleComposeGet(ctx, u, params.Arguments)
	case "deploy":
		return h.handleDeploy(ctx, u, params.Arguments, "Deploy", dockerx.ComposeUp)
	case "restart":
		return h.handleRestart(ctx, u, params.Arguments)
	case "stop":
		return h.handleStop(ctx, u, params.Arguments)
	case "deploy_status":
		return h.handleDeployStatus(ctx, u, params.Arguments)
	case "container_logs":
		return h.handleContainerLogs(ctx, u, params.Arguments)
	case "deploy_log_tail":
		return h.handleDeployLogTail(ctx, u, params.Arguments)
	case "container_exec":
		return h.handleContainerExec(ctx, u, params.Arguments)
	case "server_exec":
		return h.handleServerExec(ctx, u, params.Arguments)
	case "git_pull":
		return h.handleGitPull(ctx, u, params.Arguments)

	// Backward compatibility aliases for merged tools
	case "redeploy":
		if params.Arguments == nil {
			params.Arguments = make(map[string]interface{})
		}
		params.Arguments["rebuild"] = true
		return h.handleDeploy(ctx, u, params.Arguments, "Redeploy (pull + up)", dockerx.ComposePullUp)
	case "deploy_and_wait":
		if params.Arguments == nil {
			params.Arguments = make(map[string]interface{})
		}
		if _, ok := params.Arguments["wait_seconds"]; !ok {
			if to, ok := params.Arguments["timeout_seconds"]; ok {
				params.Arguments["wait_seconds"] = to
			} else {
				params.Arguments["wait_seconds"] = 180
			}
		}
		return h.handleDeploy(ctx, u, params.Arguments, "Deploy", dockerx.ComposeUp)
	case "file_write_batch":
		return h.handleFileWriteBatch(ctx, u, params.Arguments)
	case "env_set_batch":
		return h.handleEnvSet(ctx, u, params.Arguments)
	case "file_list":
		return h.handleFileList(ctx, u, params.Arguments)
	case "app_health_check":
		return h.handleAppHealthCheck(ctx, u, params.Arguments)
	default:
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)}},
			IsError: true,
		}, nil
	}
}

func (h *Handler) handleAppList(ctx context.Context, u db.User) (CallToolResult, error) {
	var apps []db.App
	var err error
	if u.Role == db.RoleAdmin {
		apps, err = h.p.DB.ListApps(ctx)
	} else {
		apps, err = h.p.DB.ListAppsForUser(ctx, u.ID)
	}
	if err != nil {
		return errorResult(err)
	}
	type appSummary struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Status    string   `json:"status"`
		CreatedAt string   `json:"created_at"`
		Domains   []string `json:"domains"`
		Services  []string `json:"services"`
	}
	var out []appSummary
	for _, a := range apps {
		domains, _ := h.p.DB.ListAppDomains(ctx, a.ID)
		var dNames []string
		for _, d := range domains {
			dNames = append(dNames, d.Domain)
		}
		svcs := h.p.LoadComposeServices(ctx, a.ID)
		out = append(out, appSummary{
			ID:        a.ID,
			Name:      a.Name,
			Status:    a.Status,
			CreatedAt: a.CreatedAt.Format(time.RFC3339),
			Domains:   dNames,
			Services:  svcs,
		})
	}
	return jsonResult(out)
}

func (h *Handler) handleAppGet(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer)
	if err != nil {
		return errorResult(err)
	}
	domains, _ := h.p.DB.ListAppDomains(ctx, appID)
	svcs := h.p.LoadComposeServices(ctx, appID)
	_, psRows, _ := h.p.ComposeProjectAndPS(ctx, app, appID)
	out := map[string]interface{}{
		"app":      app,
		"domains":  domains,
		"services": svcs,
		"ps":       psRows,
	}
	if getBoolArg(args, "include_health") {
		health, hErr := h.collectAppHealth(ctx, app, appID)
		if hErr == nil {
			out["health"] = health
		}
	}
	return jsonResult(out)
}

func (h *Handler) handleAppCreate(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	rawName := getStringArg(args, "name")
	slug, err := h.p.ValidateAppSlug(rawName)
	if err != nil {
		return errorResult(err)
	}

	exists, err := h.p.DB.AppNameExistsForUser(ctx, slug, u.ID)
	if err != nil {
		return errorResult(err)
	}
	if exists {
		return errorResult(fmt.Errorf("an app with the name %q already exists", slug))
	}

	if u.Role != db.RoleAdmin {
		count, err := h.p.DB.CountAppsOwnedByUser(ctx, u.ID)
		if err == nil && u.MaxApps > 0 && count >= u.MaxApps {
			return errorResult(fmt.Errorf("maximum app limit reached (%d)", u.MaxApps))
		}
	}

	var id string
	for {
		suffix := h.p.RandomAppSuffix()
		id = fmt.Sprintf("%s-%s", slug, suffix)
		if _, err := h.p.DB.GetApp(ctx, id); err != nil {
			break
		}
	}

	wsPath := h.p.Store.Path(id)
	if err := os.MkdirAll(wsPath, 0750); err != nil {
		return errorResult(fmt.Errorf("failed creating workspace: %w", err))
	}
	if err := h.p.Store.WriteMeta(id, slug); err != nil {
		_ = os.RemoveAll(wsPath)
		return errorResult(fmt.Errorf("failed writing meta: %w", err))
	}
	if err := h.p.DB.CreateApp(ctx, id, slug, u.ID); err != nil {
		_ = os.RemoveAll(wsPath)
		return errorResult(fmt.Errorf("failed creating app record: %w", err))
	}

	sourceType := strings.ToLower(strings.TrimSpace(getStringArg(args, "source_type")))
	repoURL := strings.TrimSpace(getStringArg(args, "repo_url"))
	branch := strings.TrimSpace(getStringArg(args, "branch"))
	if branch == "" {
		branch = "main"
	}

	if sourceType == "git" || sourceType == "github" || repoURL != "" {
		_ = h.p.DB.SetAppSourceType(ctx, id, "git")
		if repoURL != "" {
			cfg := db.AppGitConfig{
				AppID:         id,
				Provider:      "git",
				RepoURL:       utils.NormalizeRepoURL(repoURL),
				RepoFullName:  utils.RepoFullNameFromURL(repoURL),
				Branch:        utils.NormalizeBranch(branch),
				AuthMode:      "public",
				WebhookSecret: utils.RandomSecret(),
				AutoDeploy:    true,
			}
			_ = h.p.DB.UpsertAppGitConfig(ctx, cfg)
			_ = os.MkdirAll(filepath.Join(h.p.Store.ReservedPath(id), "repo"), 0750)
		}
	}

	composeContent := getStringArg(args, "compose_content")
	if strings.TrimSpace(composeContent) != "" {
		if err := sandbox.CheckComposeSecurity([]byte(composeContent)); err != nil {
			return errorResult(fmt.Errorf("initial compose security check failed: %w", err))
		}
		composePath := filepath.Join(wsPath, "docker-compose.yml")
		_ = os.WriteFile(composePath, []byte(composeContent), 0640)
		_ = h.p.SyncAppCaddyOverrideCtx(ctx, id)
	}

	go func() {
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     "mcp_create_app",
			TargetType: "app",
			TargetID:   id,
			Details:    fmt.Sprintf("Created app %s (%s) via MCP", slug, id),
			CreatedAt:  time.Now(),
		})
	}()

	return jsonResult(map[string]interface{}{
		"app_id":      id,
		"name":        slug,
		"source_type": sourceType,
		"message":     fmt.Sprintf("Application %q successfully created with ID %q", slug, id),
	})
}

func (h *Handler) handleAppDelete(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	tok, _ := ctx.Value(apiTokenContextKey{}).(db.APIToken)
	if !tok.AllowAppDelete {
		return errorResult(errors.New("permission denied: app_delete is restricted. Enable 'Allow App Delete' for this API token in NextDeploy Panel under CLI Sessions (/cli-sessions) or MCP Settings (/mcp-docs)"))
	}

	appID := getStringArg(args, "app_id")
	app, err := h.p.DB.GetApp(ctx, appID)
	if err != nil {
		return errorResult(fmt.Errorf("app not found: %w", err))
	}
	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return errorResult(errors.New("permission denied: only app owner or admin can delete this app"))
	}
	confirmName := strings.TrimSpace(getStringArg(args, "confirm_name"))
	if confirmName != app.Name && confirmName != app.ID {
		return errorResult(errors.New("confirm_name must exactly match the app name"))
	}
	delCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := h.p.DeleteAppResources(delCtx, appID); err != nil {
		return errorResult(err)
	}
	go func() {
		auditCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     "mcp_delete_app",
			TargetType: "app",
			TargetID:   appID,
			Details:    fmt.Sprintf("Deleted app %s (%s) via MCP", app.Name, appID),
			CreatedAt:  time.Now(),
		})
	}()
	return jsonResult(map[string]interface{}{
		"ok":      true,
		"message": fmt.Sprintf("Application %q (%s) permanently deleted", app.Name, appID),
	})
}

func (h *Handler) handleWorkspaceManifest(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	depth := getIntArg(args, "depth", 0)
	computeHash := true
	if v, ok := args["hash"].(bool); ok {
		computeHash = v
	}
	includeLocks := getBoolArg(args, "include_locks")
	fullHash := getBoolArg(args, "full_hash")
	maxEntries := getIntArg(args, "max_entries", 5000)

	var localFiles map[string]string
	if rawFiles, ok := args["local_files"].(map[string]interface{}); ok {
		localFiles = make(map[string]string, len(rawFiles))
		for k, v := range rawFiles {
			if s, ok := v.(string); ok {
				localFiles[k] = s
			}
		}
	}

	var customExcludes []string
	if rawEx, ok := args["exclude"].([]interface{}); ok {
		for _, e := range rawEx {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				customExcludes = append(customExcludes, strings.TrimSpace(s))
			}
		}
	}

	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
	}

	wsRoot := h.p.Store.Path(appID)
	res, err := BuildWorkspaceManifest(wsRoot, appID, path, depth, computeHash, includeLocks, fullHash, customExcludes, maxEntries)
	if err != nil {
		return errorResult(fmt.Errorf("manifest generation failed: %w", err))
	}
	if localFiles != nil {
		return jsonResult(ComputeManifestDiff(res, localFiles))
	}
	return jsonResult(res)
}

func (h *Handler) handleFileList(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	recursive := getBoolArg(args, "recursive")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
	}

	type item struct {
		Name    string `json:"name"`
		RelPath string `json:"rel_path"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size"`
		ModTime int64  `json:"mod_time"`
	}
	var out []item

	isGit := h.p.IsGitApp(ctx, appID)
	if !recursive {
		var children []workspace.FileEntry
		var err error
		if isGit {
			children, err = h.p.Store.ListGitRepoChildren(appID, path)
		} else {
			children, err = h.p.Store.ListChildren(appID, path)
		}
		if err != nil {
			return errorResult(fmt.Errorf("file listing failed: %w", err))
		}
		for _, ch := range children {
			out = append(out, item{
				Name:    ch.Name,
				RelPath: ch.RelPath,
				IsDir:   ch.IsDir,
				Size:    ch.Size,
				ModTime: ch.ModTime.Unix(),
			})
		}
		return jsonResult(out)
	}

	// Recursive directory listing within application workspace
	base := h.p.Store.Path(appID)
	if isGit {
		base = filepath.Clean(filepath.Join(h.p.Store.ReservedPath(appID), "repo"))
	}
	targetDir := base
	cleanRel := filepath.ToSlash(strings.Trim(path, "/"))
	if cleanRel != "" {
		var safe string
		var err error
		if isGit {
			safe, err = h.p.Store.SafeGitRepoFilePath(appID, cleanRel)
		} else {
			safe, err = h.p.Store.SafeFilePath(appID, cleanRel)
		}
		if err != nil {
			return errorResult(fmt.Errorf("invalid path: %w", err))
		}
		targetDir = safe
	}

	err := filepath.WalkDir(targetDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, err := filepath.Rel(base, p)
		if err != nil || rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		name := d.Name()

		if name == ".panel-meta" || name == ".nextdeploy" || name == ".nextdeploy.generated.compose.yml" || name == ".git" || name == ".env" || strings.HasPrefix(name, ".env.") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		out = append(out, item{
			Name:    name,
			RelPath: relSlash,
			IsDir:   d.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return errorResult(fmt.Errorf("recursive file listing failed: %w", err))
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].RelPath < out[j].RelPath
	})
	return jsonResult(out)
}

func (h *Handler) handleFileRead(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	offset := getIntArg(args, "offset", 1)
	limit := getIntArg(args, "limit", 0)
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
	}
	var full string
	var err error
	if h.p.IsGitApp(ctx, appID) {
		full, err = h.p.Store.SafeGitRepoFilePath(appID, path)
	} else {
		full, err = h.p.Store.SafeFilePath(appID, path)
	}
	if err != nil {
		return errorResult(fmt.Errorf("invalid path: %w", err))
	}
	st, err := os.Stat(full)
	if err != nil {
		return errorResult(err)
	}
	if st.IsDir() {
		return errorResult(errors.New("specified path is a directory"))
	}
	const maxRead = 10 * 1024 * 1024
	if st.Size() > maxRead {
		return errorResult(fmt.Errorf("file size (%d bytes) exceeds 10MB limit", st.Size()))
	}

	fullRead := getBoolArg(args, "full")
	if !fullRead && offset <= 1 && limit <= 0 && st.Size() > 128*1024 {
		f, err := os.Open(full)
		if err != nil {
			return errorResult(err)
		}
		defer f.Close()
		buf := make([]byte, 128*1024)
		n, _ := io.ReadFull(f, buf)
		notice := fmt.Sprintf("[File truncated: showing first 128KB of %d bytes. Pass 'offset' and 'limit' to paginate, or 'full: true' to read entire file.]\n\n", st.Size())
		return textResult(notice + string(buf[:n]))
	}

	if offset <= 1 && limit <= 0 {
		b, err := os.ReadFile(full)
		if err != nil {
			return errorResult(err)
		}
		return textResult(string(b))
	}

	f, err := os.Open(full)
	if err != nil {
		return errorResult(err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < offset {
			continue
		}
		lines = append(lines, scanner.Text())
		if limit > 0 && len(lines) >= limit {
			break
		}
		if !fullRead && limit <= 0 && len(lines) >= 1000 {
			lines = append(lines, fmt.Sprintf("\n[File truncated: showing first 1000 lines. Pass 'offset' and 'limit' to paginate, or 'full: true' for all lines.]"))
			break
		}
	}
	if err := scanner.Err(); err != nil && len(lines) == 0 {
		return errorResult(err)
	}
	return textResult(strings.Join(lines, "\n"))
}

func (h *Handler) writeWorkspaceFile(ctx context.Context, app db.App, path, content string) error {
	var full string
	var err error
	if h.p.IsGitApp(ctx, app.ID) {
		full, err = h.p.Store.SafeGitRepoFilePath(app.ID, path)
	} else {
		full, err = h.p.Store.SafeFilePath(app.ID, path)
	}
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if filepath.Base(path) == ".nextdeploy.generated.compose.yml" {
		return errors.New(".nextdeploy.generated.compose.yml is managed by NextDeploy; edit docker-compose.yml instead")
	}
	cleanRel := filepath.ToSlash(strings.Trim(path, "/"))
	if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == "compose.yml" || cleanRel == "compose.yaml" || cleanRel == app.ComposeFile {
		if err := sandbox.CheckComposeSecurity([]byte(content)); err != nil {
			return fmt.Errorf("compose security validation failed: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return err
	}
	if err := os.WriteFile(full, []byte(content), 0640); err != nil {
		return err
	}
	h.p.InvalidateAfterAppWorkspaceChange(app.ID)
	if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == app.ComposeFile {
		_ = h.p.SyncAppCaddyOverrideCtx(ctx, app.ID)
	}
	return nil
}

func (h *Handler) handleFileWrite(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	content := getStringArg(args, "content")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}
	if err := h.writeWorkspaceFile(ctx, app, path, content); err != nil {
		return errorResult(err)
	}
	res := map[string]interface{}{
		"path":    path,
		"bytes":   len(content),
		"message": fmt.Sprintf("Saved %d bytes to %s", len(content), path),
	}
	if h.p.IsGitApp(ctx, appID) {
		// Inform agent: local edits are safe; next deploy will see the dirty workspace and skip git pull.
		res["git_app_notice"] = "App uses Git. Local edits are preserved on deploy (dirty workspace auto-skips git pull). " +
			"Use git_pull:true on deploy only when you want to discard local changes and sync from remote."
	}
	return jsonResult(res)
}

func (h *Handler) handleFileWriteBatch(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}
	rawFiles, ok := args["files"].([]interface{})
	if !ok || len(rawFiles) == 0 {
		return errorResult(errors.New("'files' must be a non-empty array of objects with 'path' and 'content'"))
	}
	var written []string
	for i, item := range rawFiles {
		m, ok := item.(map[string]interface{})
		if !ok {
			return errorResult(fmt.Errorf("file item at index %d is invalid", i))
		}
		p, _ := m["path"].(string)
		c, _ := m["content"].(string)
		p = strings.TrimSpace(p)
		if p == "" {
			return errorResult(fmt.Errorf("file item at index %d has empty path", i))
		}
		if err := h.writeWorkspaceFile(ctx, app, p, c); err != nil {
			return errorResult(fmt.Errorf("failed writing %s: %w", p, err))
		}
		written = append(written, p)
	}
	InvalidateManifestCache(appID, "")
	res := map[string]interface{}{
		"app_id":  appID,
		"written": written,
		"count":   len(written),
		"message": fmt.Sprintf("Successfully wrote %d file(s)", len(written)),
	}
	if h.p.IsGitApp(ctx, appID) {
		res["git_app_notice"] = "App uses Git. Local edits are preserved on deploy (dirty workspace auto-skips git pull). " +
			"Use git_pull:true on deploy only when you want to discard local changes and sync from remote."
	}
	return jsonResult(res)
}

func (h *Handler) handleWorkspaceApply(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	dryRun := getBoolArg(args, "dry_run")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	type fileWriteItem struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	var writes []fileWriteItem
	if rawWrites, ok := args["writes"].([]interface{}); ok {
		for i, item := range rawWrites {
			m, ok := item.(map[string]interface{})
			if !ok {
				return errorResult(fmt.Errorf("writes item at index %d is invalid", i))
			}
			p, _ := m["path"].(string)
			c, _ := m["content"].(string)
			p = strings.TrimSpace(p)
			if p == "" {
				return errorResult(fmt.Errorf("writes item at index %d has empty path", i))
			}
			writes = append(writes, fileWriteItem{Path: p, Content: c})
		}
	}

	var deletes []string
	if rawDeletes, ok := args["deletes"].([]interface{}); ok {
		for i, item := range rawDeletes {
			s, ok := item.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return errorResult(fmt.Errorf("deletes item at index %d is empty", i))
			}
			deletes = append(deletes, strings.TrimSpace(s))
		}
	}

	if len(writes) == 0 && len(deletes) == 0 {
		return errorResult(errors.New("workspace_apply requires at least one write or delete operation"))
	}

	// Validation phase: check paths, security, and permissions before touching any files.
	type writePreview struct {
		Path  string `json:"path"`
		Bytes int    `json:"bytes"`
	}
	var writePreviews []writePreview

	for _, d := range deletes {
		base := filepath.Base(d)
		if base == ".env" || strings.HasPrefix(base, ".env.") {
			return errorResult(fmt.Errorf("cannot delete environment file %q; use the Env tab or env_set", d))
		}
		if base == ".nextdeploy.generated.compose.yml" || base == ".panel-meta" {
			return errorResult(fmt.Errorf("cannot delete protected file %q", d))
		}
		if _, err := h.p.Store.SafeFilePath(appID, d); err != nil {
			return errorResult(fmt.Errorf("invalid delete path %q: %w", d, err))
		}
	}

	for _, w := range writes {
		if filepath.Base(w.Path) == ".nextdeploy.generated.compose.yml" || filepath.Base(w.Path) == ".panel-meta" {
			return errorResult(fmt.Errorf("cannot write to protected file %q", w.Path))
		}
		if _, err := h.p.Store.SafeFilePath(appID, w.Path); err != nil {
			return errorResult(fmt.Errorf("invalid write path %q: %w", w.Path, err))
		}
		cleanRel := filepath.ToSlash(strings.Trim(w.Path, "/"))
		if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == "compose.yml" || cleanRel == "compose.yaml" || cleanRel == app.ComposeFile {
			if err := sandbox.CheckComposeSecurity([]byte(w.Content)); err != nil {
				return errorResult(fmt.Errorf("compose security validation failed for %s: %w", w.Path, err))
			}
		}
		writePreviews = append(writePreviews, writePreview{Path: w.Path, Bytes: len(w.Content)})
	}

	// Dry-run mode: return simulated plan without making disk changes.
	if dryRun {
		return jsonResult(map[string]interface{}{
			"app_id":   appID,
			"dry_run":  true,
			"valid":    true,
			"writes":   writePreviews,
			"deletes":  deletes,
			"message":  fmt.Sprintf("Dry run validated: %d file(s) would be written, %d item(s) would be deleted.", len(writes), len(deletes)),
		})
	}

	// Apply deletes first
	var deletedPaths []string
	for _, d := range deletes {
		if err := h.p.Store.RemoveRel(appID, d); err != nil && !os.IsNotExist(err) {
			return errorResult(fmt.Errorf("failed deleting %s: %w", d, err))
		}
		deletedPaths = append(deletedPaths, d)
	}

	// Apply writes
	var writtenPaths []string
	for _, w := range writes {
		if err := h.writeWorkspaceFile(ctx, app, w.Path, w.Content); err != nil {
			return errorResult(fmt.Errorf("failed writing %s: %w", w.Path, err))
		}
		writtenPaths = append(writtenPaths, w.Path)
	}

	InvalidateManifestCache(appID, "")
	h.p.InvalidateAfterAppWorkspaceChange(appID)

	res := map[string]interface{}{
		"app_id":        appID,
		"dry_run":       false,
		"written":       writtenPaths,
		"deleted":       deletedPaths,
		"written_count": len(writtenPaths),
		"deleted_count": len(deletedPaths),
		"message":       fmt.Sprintf("Successfully applied: %d written, %d deleted", len(writtenPaths), len(deletedPaths)),
	}
	if h.p.IsGitApp(ctx, appID) {
		res["git_app_notice"] = "App uses Git. Local edits are preserved on deploy (dirty workspace auto-skips git pull). " +
			"Use git_pull:true on deploy only when you want to discard local changes and sync from remote."
	}
	return jsonResult(res)
}

func (h *Handler) handleFileDelete(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper); err != nil {
		return errorResult(err)
	}
	base := filepath.Base(path)
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return errorResult(errors.New("cannot delete environment file; use the Env tab or env_set"))
	}
	if base == ".nextdeploy.generated.compose.yml" || base == ".panel-meta" {
		return errorResult(errors.New("cannot delete internal generated files"))
	}
	var delErr error
	if h.p.IsGitApp(ctx, appID) {
		delErr = h.p.Store.RemoveGitRepoRel(appID, path)
	} else {
		delErr = h.p.Store.RemoveRel(appID, path)
	}
	if delErr != nil {
		return errorResult(fmt.Errorf("delete failed: %w", delErr))
	}
	h.p.InvalidateAfterAppWorkspaceChange(appID)
	return textResult(fmt.Sprintf("Deleted %s", path))
}

func (h *Handler) handleEnvList(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
	}
	panelEnv, _ := h.p.DB.GetPanelEnv(ctx, appID)
	parsed := parseDotEnv(panelEnv)
	keys := make([]string, 0, len(parsed))
	for k := range parsed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return jsonResult(map[string]interface{}{
		"keys":  keys,
		"count": len(keys),
	})
}

func (h *Handler) handleEnvReveal(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	// Permission check: token must explicitly have AllowEnvReveal enabled
	tok, ok := ctx.Value(apiTokenContextKey{}).(db.APIToken)
	if !ok || !tok.AllowEnvReveal {
		return errorResult(errors.New("permission denied: env_reveal is restricted. Enable 'Allow env_reveal' for this API token in NextDeploy Panel under MCP Settings (/mcp-docs)"))
	}

	panelEnv, _ := h.p.DB.GetPanelEnv(ctx, appID)
	parsed := parseDotEnv(panelEnv)

	outEnv := parsed
	if rawKeys, ok := args["keys"]; ok && rawKeys != nil {
		var requestedKeys []string
		switch v := rawKeys.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					requestedKeys = append(requestedKeys, strings.TrimSpace(s))
				}
			}
		case []string:
			requestedKeys = v
		case string:
			if strings.TrimSpace(v) != "" {
				requestedKeys = []string{strings.TrimSpace(v)}
			}
		}

		if len(requestedKeys) > 0 {
			outEnv = make(map[string]string, len(requestedKeys))
			for _, k := range requestedKeys {
				if val, exists := parsed[k]; exists {
					outEnv[k] = val
				}
			}
		}
	}

	go func() {
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     "mcp_env_reveal",
			TargetType: "app",
			TargetID:   appID,
			Details:    fmt.Sprintf("Revealed %d environment secret(s) via MCP for app %s", len(outEnv), app.Name),
			CreatedAt:  time.Now(),
		})
	}()

	return jsonResult(map[string]interface{}{
		"env":     outEnv,
		"count":   len(outEnv),
		"warning": "Sensitive environment secrets revealed via env_reveal.",
	})
}

func (h *Handler) handleEnvSet(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper); err != nil {
		return errorResult(err)
	}

	updates := make(map[string]string)
	singleKey := strings.TrimSpace(getStringArg(args, "key"))
	if singleKey != "" {
		updates[singleKey] = getStringArg(args, "value")
	}

	if rawMap, ok := args["variables"].(map[string]interface{}); ok {
		for k, v := range rawMap {
			k = strings.TrimSpace(k)
			if k != "" {
				updates[k] = fmt.Sprint(v)
			}
		}
	} else if rawList, ok := args["variables"].([]interface{}); ok {
		for _, item := range rawList {
			if m, ok := item.(map[string]interface{}); ok {
				k := strings.TrimSpace(fmt.Sprint(m["key"]))
				if k != "" && k != "<nil>" {
					updates[k] = fmt.Sprint(m["value"])
				}
			}
		}
	}

	if len(updates) == 0 {
		return errorResult(errors.New("either 'key' and 'value' or 'variables' must be provided"))
	}

	cur, _ := h.p.DB.GetPanelEnv(ctx, appID)
	for k, v := range updates {
		cur = setDotEnvVar(cur, k, v)
	}

	if err := h.p.DB.UpdatePanelEnv(ctx, appID, cur); err != nil {
		return errorResult(fmt.Errorf("failed updating panel env: %w", err))
	}

	root := h.p.ComposeWorkspaceRoot(ctx, appID)
	_ = h.p.SyncWorkspaceEnvFromPanel(appID, root, cur)
	_ = h.p.SyncAppCaddyOverrideCtx(ctx, appID)

	if len(updates) == 1 && singleKey != "" {
		return textResult(fmt.Sprintf("Environment variable %q set successfully", singleKey))
	}

	var updatedKeys []string
	for k := range updates {
		updatedKeys = append(updatedKeys, k)
	}
	sort.Strings(updatedKeys)

	return jsonResult(map[string]interface{}{
		"app_id":       appID,
		"updated_keys": updatedKeys,
		"count":        len(updatedKeys),
		"message":      fmt.Sprintf("Successfully set %d environment variable(s)", len(updatedKeys)),
	})
}

func (h *Handler) handleEnvSetBatch(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	return h.handleEnvSet(ctx, u, args)
}

func (h *Handler) handleComposeGet(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer)
	if err != nil {
		return errorResult(err)
	}
	_ = h.p.SyncAppCaddyOverrideCtx(ctx, appID)
	overridePath := h.p.ComposeOverridePath(ctx, appID)
	b, err := os.ReadFile(overridePath)
	if err != nil {
		cp := h.p.ComposeFilePath(ctx, app, appID)
		b, err = os.ReadFile(cp)
		if err != nil {
			return errorResult(errors.New("compose file not found"))
		}
	}

	summary := getBoolArg(args, "summary")
	targetService := strings.TrimSpace(getStringArg(args, "service"))

	if summary || targetService != "" {
		var doc map[string]interface{}
		if yerr := yaml.Unmarshal(b, &doc); yerr == nil {
			services, _ := doc["services"].(map[string]interface{})
			if targetService != "" {
				if svc, ok := services[targetService]; ok {
					outYaml, merr := yaml.Marshal(map[string]interface{}{
						targetService: svc,
					})
					if merr == nil {
						return textResult(string(outYaml))
					}
					return jsonResult(map[string]interface{}{targetService: svc})
				}
				return errorResult(fmt.Errorf("service %q not found in docker-compose.yml", targetService))
			}
			if summary {
				summaryMap := make(map[string]interface{}, len(services))
				for name, raw := range services {
					if svcMap, ok := raw.(map[string]interface{}); ok {
						info := map[string]interface{}{}
						if img, ok := svcMap["image"]; ok {
							info["image"] = img
						}
						if ports, ok := svcMap["ports"]; ok {
							info["ports"] = ports
						}
						if len(info) == 0 {
							info["configured"] = true
						}
						summaryMap[name] = info
					} else {
						summaryMap[name] = "configured"
					}
				}
				return jsonResult(map[string]interface{}{
					"app_id":   appID,
					"services": summaryMap,
				})
			}
		}
	}

	return textResult(string(b))
}

func (h *Handler) handleDeploy(ctx context.Context, u db.User, args map[string]interface{}, action string, fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	// rebuild:true triggers full image pull and rebuild (redeploy behavior)
	if getBoolArg(args, "rebuild") {
		action = "Redeploy (pull + up)"
		fn = dockerx.ComposePullUp
	}

	v, _ := h.p.ComposeMu.LoadOrStore(appID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			mu.Unlock()
		}
	}()

	// git_pull:true = force sync from remote (discards local workspace edits).
	// Default (omitted/false): perform a dirty-check first; skip sync if workspace has local
	// changes so that file_write → deploy workflows are never silently destructive.
	var gitSyncWarning string
	forceGitPull := getBoolArg(args, "git_pull")
	if h.p.IsGitApp(ctx, appID) && (action == "Deploy" || action == "Redeploy (pull + up)") {
		repoDir := h.p.AppCheckoutPath(appID)
		if forceGitPull {
			// Caller explicitly requested a git pull — proceed even if workspace is dirty.
			syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Minute)
			_, syncErr := h.p.SyncGitAppSource(syncCtx, appID)
			syncCancel()
			if syncErr != nil {
				return errorResult(fmt.Errorf("git sync failed before deploy: %w", syncErr))
			}
			h.p.InvalidateAfterAppWorkspaceChange(appID)
		} else if gitx.IsDirty(ctx, repoDir) {
			// Workspace has local edits — protect them by skipping the destructive pull.
			gitSyncWarning = "Git pull skipped: workspace has local changes (dirty). Deploying from current files. " +
				"Pass git_pull:true to force-sync from remote (WARNING: discards all local workspace edits)."
		} else {
			// Clean workspace — safe to pull.
			syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Minute)
			_, syncErr := h.p.SyncGitAppSource(syncCtx, appID)
			syncCancel()
			if syncErr != nil {
				return errorResult(fmt.Errorf("git sync failed before deploy: %w", syncErr))
			}
			h.p.InvalidateAfterAppWorkspaceChange(appID)
		}
	}

	if err := h.p.SyncAppCaddyOverrideCtx(ctx, appID); err != nil {
		return errorResult(fmt.Errorf("caddy sync failed: %w", err))
	}
	projCtx, projCancel := context.WithTimeout(ctx, 30*time.Second)
	project := h.p.ActiveComposeProjectName(projCtx, app, appID)
	projCancel()

	if action == "Deploy" || action == "Redeploy (pull + up)" {
		stopCtx, stopCancel := context.WithTimeout(ctx, 2*time.Minute)
		h.p.StopOtherComposeStacks(stopCtx, app, appID, project)
		stopCancel()
	}

	jobID, err := h.p.StartComposeJob(appID, project, h.p.EffectiveComposePaths(ctx, app, appID), action, fn, "")
	if err != nil {
		return errorResult(fmt.Errorf("failed to start %s job: %w", action, err))
	}

	// Synchronous waiting if wait_seconds or timeout_seconds is requested
	waitSec := getIntArg(args, "wait_seconds", 0)
	if waitSec <= 0 {
		waitSec = getIntArg(args, "timeout_seconds", 0)
	}
	summaryOnly := true
	if v, ok := args["summary_only"].(bool); ok {
		summaryOnly = v
	}
	if waitSec > 0 {
		if waitSec > 300 {
			waitSec = 300
		}
		mu.Unlock()
		unlocked = true
		return h.waitForDeployJob(ctx, jobID, appID, action, waitSec, summaryOnly)
	}

	out := map[string]interface{}{
		"job_id":  jobID,
		"app_id":  appID,
		"action":  action,
		"status":  "started",
		"message": "Deployment job started in background. Poll deploy_status with job_id for progress.",
	}
	if gitSyncWarning != "" {
		out["git_sync_warning"] = gitSyncWarning
	}
	return jsonResult(out)
}

func (h *Handler) handleRestart(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	service := strings.TrimSpace(getStringArg(args, "service"))
	recreate := getBoolArg(args, "recreate")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}
	if service != "" {
		if recreate {
			composeFiles := h.p.EffectiveComposePaths(ctx, app, appID)
			envFiles := h.p.ComposeEnvFiles(ctx, appID)
			project := h.p.ActiveComposeProjectName(ctx, app, appID)
			dir := h.p.AppCheckoutPath(appID)
			res := dockerx.ComposeRecreate(ctx, dir, composeFiles, project, nil, envFiles, service)
			if !res.OK {
				return errorResult(fmt.Errorf("recreate failed for service %q: %s", service, res.Output))
			}
			return textResult(fmt.Sprintf("Recreated container for service %q: %s", service, res.Output))
		}
		project := h.p.ActiveComposeProjectName(ctx, app, appID)
		cid, err := dockerapi.ContainerIDForComposeService(ctx, project, service)
		if err != nil {
			return errorResult(fmt.Errorf("container for service %q not found: %w", service, err))
		}
		if err := dockerapi.RestartContainerByName(ctx, cid); err != nil {
			return errorResult(fmt.Errorf("restart failed: %w", err))
		}
		return textResult(fmt.Sprintf("Restarted container %s for service %q", cid, service))
	}
	if recreate {
		return h.handleDeploy(ctx, u, args, "Stack recreate", func(c context.Context, d string, cf []string, p string, w io.Writer, ef []string) dockerx.Result {
			return dockerx.ComposeRecreate(c, d, cf, p, w, ef)
		})
	}
	return h.handleDeploy(ctx, u, args, "Stack restart", dockerx.ComposeRestart)
}

func (h *Handler) handleStop(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	service := strings.TrimSpace(getStringArg(args, "service"))
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}
	if service != "" {
		project := h.p.ActiveComposeProjectName(ctx, app, appID)
		cid, err := dockerapi.ContainerIDForComposeService(ctx, project, service)
		if err != nil {
			return errorResult(fmt.Errorf("container for service %q not found: %w", service, err))
		}
		if err := dockerapi.StopContainerByName(ctx, cid); err != nil {
			return errorResult(fmt.Errorf("stop failed: %w", err))
		}
		return textResult(fmt.Sprintf("Stopped container %s for service %q", cid, service))
	}
	return h.handleDeploy(ctx, u, args, "Stop", dockerx.ComposeDown)
}

func (h *Handler) handleDeployStatus(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	jobID := strings.TrimSpace(getStringArg(args, "job_id"))
	appID := strings.TrimSpace(getStringArg(args, "app_id"))
	summaryOnly := true
	if v, ok := args["summary_only"].(bool); ok {
		summaryOnly = v
	}

	buildJobResult := func(job handlers.DeployJobRecord) (CallToolResult, error) {
		if !summaryOnly {
			return jsonResult(job)
		}
		res := map[string]interface{}{
			"job_id":     job.JobID,
			"app_id":     job.AppID,
			"action":     job.Action,
			"running":    job.Running,
			"ok":         job.OK,
			"created_at": job.CreatedAt,
		}
		if !job.FinishedAt.IsZero() {
			res["finished_at"] = job.FinishedAt
			res["duration_s"] = job.FinishedAt.Sub(job.CreatedAt).Round(time.Second).Seconds()
		}
		if job.Running {
			res["output_tail"] = truncateLogLines(job.Output, 5)
		} else if !job.OK {
			res["output_tail"] = truncateLogLines(job.Output, 30)
		} else {
			res["message"] = "Deployment completed successfully"
		}
		return jsonResult(res)
	}

	if jobID != "" {
		if job, ok := h.p.GetDeployJob(jobID); ok {
			if _, err := h.hasAppAccess(ctx, u, job.AppID, db.CollabRoleViewer); err != nil {
				return errorResult(err)
			}
			return buildJobResult(job)
		}
	}
	if appID != "" {
		if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
			return errorResult(err)
		}
		if job, ok := h.p.LatestDeployJobForApp(appID); ok {
			return buildJobResult(job)
		}
		out, action, running := h.p.DeploySnapshot(appID)
		if !summaryOnly {
			return jsonResult(map[string]interface{}{
				"app_id":  appID,
				"action":  action,
				"running": running,
				"output":  out,
			})
		}
		res := map[string]interface{}{
			"app_id":  appID,
			"action":  action,
			"running": running,
		}
		if running {
			res["output_tail"] = truncateLogLines(out, 5)
		} else {
			res["output_tail"] = truncateLogLines(out, 20)
		}
		return jsonResult(res)
	}
	return errorResult(errors.New("either job_id or app_id must be provided"))
}

func (h *Handler) handleContainerLogs(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	service := strings.TrimSpace(getStringArg(args, "service"))
	tail := getIntArg(args, "tail", 0)
	if tail <= 0 {
		tail = getIntArg(args, "lines", 30)
	}
	if tail <= 0 {
		tail = 30
	}
	filterHealth := true
	if v, ok := args["filter_health"].(bool); ok {
		filterHealth = v
	}
	level := strings.ToLower(strings.TrimSpace(getStringArg(args, "level")))

	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer)
	if err != nil {
		return errorResult(err)
	}
	project, composeRows, composeRes := h.p.ComposeProjectAndPS(ctx, app, appID)
	logRef := service
	if service != "" {
		if composeRes.OK && h.p.ComposeServiceInRows(composeRows, service) {
			cid, rerr := dockerapi.ContainerIDForComposeService(ctx, project, service)
			if rerr == nil && cid != "" {
				logRef = cid
			}
		}
	} else if len(composeRows) > 0 {
		logRef = composeRows[0].Name
	} else {
		logRef = project
	}
	raw, ferr := dockerapi.FetchContainerLogsText(ctx, logRef, tail)
	if ferr != nil && strings.TrimSpace(raw) == "" {
		return errorResult(fmt.Errorf("failed fetching container logs: %w", ferr))
	}

	if filterHealth || level != "" {
		lines := strings.Split(raw, "\n")
		var filtered []string
		for _, l := range lines {
			trimmed := strings.TrimSpace(l)
			if trimmed == "" {
				continue
			}
			if filterHealth {
				lower := strings.ToLower(trimmed)
				if strings.Contains(lower, "/healthz") || strings.Contains(lower, "/health") ||
					strings.Contains(lower, "get /ping") || strings.Contains(lower, "head / http") ||
					strings.Contains(lower, "kube-probe") || strings.Contains(lower, "docker-healthcheck") {
					continue
				}
			}
			if level == "error" {
				lower := strings.ToLower(trimmed)
				if !strings.Contains(lower, "error") && !strings.Contains(lower, "fatal") &&
					!strings.Contains(lower, "exception") && !strings.Contains(lower, "panic") &&
					!strings.Contains(lower, "failed") {
					continue
				}
			} else if level == "warn" {
				lower := strings.ToLower(trimmed)
				if !strings.Contains(lower, "warn") && !strings.Contains(lower, "error") &&
					!strings.Contains(lower, "fatal") && !strings.Contains(lower, "exception") &&
					!strings.Contains(lower, "panic") && !strings.Contains(lower, "failed") {
					continue
				}
			}
			filtered = append(filtered, l)
		}
		return textResult(strings.Join(filtered, "\n"))
	}

	return textResult(raw)
}

func (h *Handler) handleDeployLogTail(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	jobID := strings.TrimSpace(getStringArg(args, "job_id"))
	sinceOffset := getIntArg(args, "since_offset", 0)
	waitSeconds := getIntArg(args, "wait_seconds", 0)
	limit := getIntArg(args, "limit", 5)

	if jobID == "" && appID == "" {
		return errorResult(errors.New("either job_id or app_id must be provided"))
	}

	// When job_id is provided, stream or tail specific job output via long-polling
	if jobID != "" {
		job, found := h.p.GetDeployJob(jobID)
		if !found {
			return errorResult(fmt.Errorf("job %q not found", jobID))
		}
		if _, err := h.hasAppAccess(ctx, u, job.AppID, db.CollabRoleViewer); err != nil {
			return errorResult(err)
		}

		if waitSeconds > 30 {
			waitSeconds = 30
		}
		deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)

		for {
			curJob, ok := h.p.GetDeployJob(jobID)
			if !ok {
				break
			}
			out := curJob.Output
			currentLen := len(out)

			if currentLen > sinceOffset {
				return jsonResult(map[string]interface{}{
					"job_id":       curJob.JobID,
					"app_id":       curJob.AppID,
					"action":       curJob.Action,
					"running":      curJob.Running,
					"ok":           curJob.OK,
					"since_offset": sinceOffset,
					"next_offset":  currentLen,
					"new_output":   out[sinceOffset:],
				})
			}

			if !curJob.Running || waitSeconds <= 0 || time.Now().After(deadline) {
				return jsonResult(map[string]interface{}{
					"job_id":       curJob.JobID,
					"app_id":       curJob.AppID,
					"action":       curJob.Action,
					"running":      curJob.Running,
					"ok":           curJob.OK,
					"since_offset": sinceOffset,
					"next_offset":  currentLen,
					"new_output":   "",
				})
			}

			time.Sleep(300 * time.Millisecond)
		}
	}

	// Fallback to app_id: if an ongoing job exists, stream it; otherwise return snapshot + history.
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
	}

	if latestJob, ok := h.p.LatestDeployJobForApp(appID); ok && (latestJob.Running || sinceOffset > 0) {
		args["job_id"] = latestJob.JobID
		return h.handleDeployLogTail(ctx, u, args)
	}

	liveOut, liveAction, liveRunning := h.p.DeploySnapshot(appID)
	history, _ := h.p.DB.ListDeployLogs(ctx, appID, limit)
	return jsonResult(map[string]interface{}{
		"app_id":       appID,
		"live_running": liveRunning,
		"live_action":  liveAction,
		"live_output":  liveOut,
		"history":      history,
	})
}

func (h *Handler) handleContainerExec(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	// container_exec is restricted; require explicit token flag.
	tok, ok := ctx.Value(apiTokenContextKey{}).(db.APIToken)
	if !ok || !tok.AllowContainerExec {
		return errorResult(errors.New("permission denied: container_exec is restricted. Enable 'Allow container_exec' for this API token in NextDeploy Panel under CLI Sessions (/cli-sessions) or MCP Settings (/mcp-docs)"))
	}

	var cmdArgs []string
	if rawArr, ok := args["args"].([]interface{}); ok {
		for _, item := range rawArr {
			if s, ok := item.(string); ok {
				cmdArgs = append(cmdArgs, s)
			}
		}
	} else if rawStrArr, ok := args["args"].([]string); ok {
		cmdArgs = rawStrArr
	}

	command := getStringArg(args, "command")
	if command == "" && len(cmdArgs) == 0 {
		return errorResult(errors.New("command or args is required"))
	}
	if command == "" && len(cmdArgs) > 0 {
		command = strings.Join(cmdArgs, " ")
	}
	service := strings.TrimSpace(getStringArg(args, "service"))
	workDir := strings.TrimSpace(getStringArg(args, "work_dir"))
	stdinStr := getStringArg(args, "stdin")
	var stdinReader io.Reader
	if stdinStr != "" {
		stdinReader = strings.NewReader(stdinStr)
	}

	timeoutSec := getIntArg(args, "timeout_seconds", 60)
	if timeoutSec < 1 {
		timeoutSec = 1
	} else if timeoutSec > 3600 {
		timeoutSec = 3600
	}

	project, composeRows, composeRes := h.p.ComposeProjectAndPS(ctx, app, appID)
	targetContainer := ""
	matchedService := service

	if service != "" {
		if composeRes.OK && h.p.ComposeServiceInRows(composeRows, service) {
			cid, rerr := dockerapi.ContainerIDForComposeService(ctx, project, service)
			if rerr == nil && cid != "" {
				targetContainer = cid
			}
		}
		if targetContainer == "" {
			for _, row := range composeRows {
				if strings.EqualFold(row.Name, service) || strings.EqualFold(row.Service, service) {
					targetContainer = row.Name
					matchedService = row.Service
					break
				}
			}
		}
		if targetContainer == "" && h.p.ContainerBelongsToApp(ctx, appID, service) {
			targetContainer = service
		}
		if targetContainer == "" {
			return errorResult(fmt.Errorf("service or container %q not found for app %q", service, appID))
		}
	} else {
		for _, row := range composeRows {
			if strings.EqualFold(row.State, "running") {
				targetContainer = row.Name
				matchedService = row.Service
				break
			}
		}
		if targetContainer == "" && len(composeRows) > 0 {
			targetContainer = composeRows[0].Name
			matchedService = composeRows[0].Service
		}
		if targetContainer == "" {
			return errorResult(fmt.Errorf("no running container found for app %q. Deploy the application stack first", appID))
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var res dockerx.Result
	if len(cmdArgs) > 0 {
		res = dockerx.DockerExecArgs(execCtx, targetContainer, cmdArgs, workDir, stdinReader)
	} else {
		res = dockerx.DockerExecWorkDirWithStdin(execCtx, targetContainer, command, workDir, stdinReader)
	}

	go func() {
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     "mcp_container_exec",
			TargetType: "app",
			TargetID:   appID,
			Details:    fmt.Sprintf("container_exec on %s (service=%s ok=%v): %s", app.Name, matchedService, res.OK, command),
			CreatedAt:  time.Now(),
		})
	}()

	return jsonResult(map[string]interface{}{
		"app_id":    appID,
		"container": targetContainer,
		"service":   matchedService,
		"command":   command,
		"ok":        res.OK,
		"exit_code": res.ExitCode,
		"output":    res.Output,
	})
}

func (h *Handler) handleServerExec(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	if u.Role != db.RoleAdmin {
		return errorResult(errors.New("forbidden: server_exec requires admin role"))
	}

	// server_exec is highest-privilege; require explicit token flag.
	tok, ok := ctx.Value(apiTokenContextKey{}).(db.APIToken)
	if !ok || !tok.AllowServerExec {
		return errorResult(errors.New("permission denied: server_exec is restricted. Enable 'Allow server_exec' for this API token in NextDeploy Panel under CLI Sessions (/cli-sessions) or MCP Settings (/mcp-docs)"))
	}

	command := getStringArg(args, "command")
	if command == "" {
		return errorResult(errors.New("command is required"))
	}
	timeoutSec := getIntArg(args, "timeout_seconds", 60)
	if timeoutSec < 1 {
		timeoutSec = 1
	} else if timeoutSec > 300 {
		timeoutSec = 300
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var res runutil.Result
	if runtime.GOOS == "windows" {
		res = runutil.Run(execCtx, ".", nil, "cmd", "/c", command)
	} else {
		res = runutil.Run(execCtx, ".", nil, "sh", "-c", command)
	}

	go func() {
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     "mcp_server_exec",
			TargetType: "system",
			TargetID:   "server",
			Details:    fmt.Sprintf("server_exec (ok=%v): %s", res.OK, command),
			CreatedAt:  time.Now(),
		})
	}()

	return jsonResult(map[string]interface{}{
		"command":   command,
		"ok":        res.OK,
		"exit_code": res.ExitCode,
		"output":    res.Output,
	})
}

func (h *Handler) handleGitPull(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	branch := strings.TrimSpace(getStringArg(args, "branch"))
	force := getBoolArg(args, "force")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	cfg, err := h.p.DB.GetAppGitConfig(ctx, appID)
	if err != nil || strings.TrimSpace(cfg.RepoURL) == "" {
		return errorResult(errors.New("no git repository configured for this app. Connect a git repo in the panel first"))
	}

	repoDir := h.p.AppCheckoutPath(appID)

	// Protect uncommitted local changes unless force:true is explicitly set
	if !force && gitx.IsDirty(ctx, repoDir) {
		return errorResult(errors.New("workspace has local uncommitted changes. Pass force:true to discard local edits and sync from remote"))
	}

	if branch != "" && branch != cfg.Branch {
		cfg.Branch = branch
		_ = h.p.DB.UpsertAppGitConfig(ctx, cfg)
	}

	out, err := h.p.SyncGitAppSource(ctx, appID)
	if err != nil {
		return errorResult(fmt.Errorf("git pull failed: %w", err))
	}

	h.p.InvalidateAfterAppWorkspaceChange(appID)
	_ = h.p.SyncAppCaddyOverrideCtx(ctx, appID)

	commit := gitx.CurrentCommit(ctx, repoDir)
	subject := gitx.CurrentCommitSubject(ctx, repoDir)

	go func() {
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     "mcp_git_pull",
			TargetType: "app",
			TargetID:   appID,
			Details:    fmt.Sprintf("Pulled git commit %s (%s) for app %s", commit, subject, app.Name),
			CreatedAt:  time.Now(),
		})
	}()

	return jsonResult(map[string]interface{}{
		"app_id":  appID,
		"branch":  cfg.Branch,
		"commit":  commit,
		"subject": subject,
		"output":  out,
	})
}

func (h *Handler) handleDeployAndWait(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	if args == nil {
		args = make(map[string]interface{})
	}
	if _, ok := args["wait_seconds"]; !ok {
		if to, ok := args["timeout_seconds"]; ok {
			args["wait_seconds"] = to
		} else {
			args["wait_seconds"] = 180
		}
	}
	return h.handleDeploy(ctx, u, args, "Deploy", dockerx.ComposeUp)
}

func (h *Handler) waitForDeployJob(ctx context.Context, jobID, appID, action string, timeoutSec int, summaryOnly bool) (CallToolResult, error) {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	pollInterval := 1500 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return errorResult(ctx.Err())
		default:
		}

		job, found := h.p.GetDeployJob(jobID)
		if !found {
			return errorResult(fmt.Errorf("deploy job %s disappeared", jobID))
		}

		if !job.Running {
			if summaryOnly && job.OK {
				return jsonResult(map[string]interface{}{
					"job_id":     job.JobID,
					"app_id":     job.AppID,
					"action":     job.Action,
					"ok":         true,
					"running":    false,
					"duration_s": time.Since(job.CreatedAt).Round(time.Second).Seconds(),
					"message":    "Deployment succeeded",
				})
			}
			tailLines := 60
			if summaryOnly && !job.OK {
				tailLines = 40
			}
			output := truncateLogLines(job.Output, tailLines)
			return jsonResult(map[string]interface{}{
				"job_id":      job.JobID,
				"app_id":      job.AppID,
				"action":      job.Action,
				"ok":          job.OK,
				"running":     false,
				"duration_s":  time.Since(job.CreatedAt).Round(time.Second).Seconds(),
				"output_tail": output,
			})
		}

		if time.Now().After(deadline) {
			output := truncateLogLines(job.Output, 40)
			return jsonResult(map[string]interface{}{
				"job_id":      job.JobID,
				"app_id":      job.AppID,
				"action":      job.Action,
				"status":      "timeout",
				"running":     true,
				"message":     fmt.Sprintf("Deployment still running after %d seconds. Continue checking with deploy_status.", timeoutSec),
				"output_tail": output,
			})
		}

		time.Sleep(pollInterval)
	}
}

func (h *Handler) handleFilePatch(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	patch := getStringArg(args, "patch")
	if strings.TrimSpace(patch) == "" {
		return errorResult(errors.New("patch content is required"))
	}
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	workDir := h.p.ComposeWorkspaceRoot(ctx, appID)

	if strings.Contains(patch, ".nextdeploy.generated.compose.yml") {
		return errorResult(errors.New("cannot patch .nextdeploy.generated.compose.yml"))
	}

	outStr, applyErr := applyUnifiedPatch(ctx, workDir, patch)
	if applyErr != nil {
		return errorResult(fmt.Errorf("failed to apply patch: %w", applyErr))
	}

	// Validate compose security if a compose file might have been modified
	composeFiles := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml", app.ComposeFile}
	for _, cf := range composeFiles {
		if cf == "" {
			continue
		}
		if strings.Contains(patch, cf) {
			cp := filepath.Join(workDir, cf)
			if b, err := os.ReadFile(cp); err == nil {
				if secErr := sandbox.CheckComposeSecurity(b); secErr != nil {
					_ = revertUnifiedPatch(ctx, workDir, patch)
					return errorResult(fmt.Errorf("patch produced insecure compose file (%s): %w (reverted)", cf, secErr))
				}
			}
		}
	}

	h.p.InvalidateAfterAppWorkspaceChange(appID)
	_ = h.p.SyncAppCaddyOverrideCtx(ctx, appID)

	go func() {
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.p.DB.CreateAuditLog(auditCtx, db.AuditLog{
			UserID:     u.ID,
			Username:   u.Username,
			Action:     "mcp_file_patch",
			TargetType: "app",
			TargetID:   appID,
			Details:    fmt.Sprintf("Applied patch to app %s", app.Name),
			CreatedAt:  time.Now(),
		})
	}()

	msg := "Patch applied successfully"
	if outStr != "" {
		msg += ":\n" + outStr
	}
	return textResult(msg)
}

func applyUnifiedPatch(ctx context.Context, dir string, patch string) (string, error) {
	if !strings.HasSuffix(patch, "\n") {
		patch += "\n"
	}
	tmpFile, err := os.CreateTemp("", "nextdeploy_patch_*.diff")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(patch); err != nil {
		_ = tmpFile.Close()
		return "", err
	}
	_ = tmpFile.Close()

	cmd := exec.CommandContext(ctx, "git", "apply", "--ignore-whitespace", "--inaccurate-eof", tmpFile.Name())
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err == nil {
		return strings.TrimSpace(out.String()), nil
	}

	cmd0 := exec.CommandContext(ctx, "git", "apply", "-p0", "--ignore-whitespace", "--inaccurate-eof", tmpFile.Name())
	cmd0.Dir = dir
	var out0 bytes.Buffer
	cmd0.Stdout = &out0
	cmd0.Stderr = &out0
	err0 := cmd0.Run()
	if err0 == nil {
		return strings.TrimSpace(out0.String()), nil
	}

	errMsg := strings.TrimSpace(out.String())
	if errMsg == "" {
		errMsg = strings.TrimSpace(out0.String())
	}
	if errMsg == "" {
		errMsg = err.Error()
	}
	return "", errors.New(errMsg)
}

func revertUnifiedPatch(ctx context.Context, dir string, patch string) error {
	if !strings.HasSuffix(patch, "\n") {
		patch += "\n"
	}
	tmpFile, err := os.CreateTemp("", "nextdeploy_revert_*.diff")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(patch); err != nil {
		_ = tmpFile.Close()
		return err
	}
	_ = tmpFile.Close()

	cmd := exec.CommandContext(ctx, "git", "apply", "-R", "--ignore-whitespace", "--inaccurate-eof", tmpFile.Name())
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return nil
	}
	cmd0 := exec.CommandContext(ctx, "git", "apply", "-p0", "-R", "--ignore-whitespace", "--inaccurate-eof", tmpFile.Name())
	cmd0.Dir = dir
	return cmd0.Run()
}

// collectAppHealth inspects container runtime states and performs live HTTP domain checks.
func (h *Handler) collectAppHealth(ctx context.Context, app db.App, appID string) (map[string]interface{}, error) {
	project, psRows, psRes := h.p.ComposeProjectAndPS(ctx, app, appID)
	containers := make([]map[string]interface{}, 0, len(psRows))
	allRunning := len(psRows) > 0
	for _, row := range psRows {
		isUp := strings.EqualFold(row.State, "running")
		if !isUp {
			allRunning = false
		}
		containers = append(containers, map[string]interface{}{
			"service": row.Service,
			"name":    row.Name,
			"state":   row.State,
			"status":  row.Status,
		})
	}
	if len(psRows) == 0 {
		allRunning = false
	}

	domains, _ := h.p.DB.ListAppDomains(ctx, appID)
	type HttpCheckResult struct {
		Domain     string `json:"domain"`
		URL        string `json:"url"`
		StatusCode int    `json:"status_code,omitempty"`
		LatencyMS  int64  `json:"latency_ms"`
		OK         bool   `json:"ok"`
		Error      string `json:"error,omitempty"`
	}
	httpChecks := make([]HttpCheckResult, 0, len(domains))
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("stopped after 3 redirects")
			}
			return nil
		},
	}

	allHttpOK := true
	for _, d := range domains {
		targetURL := fmt.Sprintf("https://%s", d.Domain)
		start := time.Now()
		req, reqErr := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		var resp *http.Response
		if reqErr == nil {
			resp, reqErr = client.Do(req)
		}
		if reqErr != nil {
			targetURL = fmt.Sprintf("http://%s", d.Domain)
			reqHttp, _ := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
			start = time.Now()
			resp, reqErr = client.Do(reqHttp)
		}
		latency := time.Since(start).Milliseconds()

		if reqErr != nil {
			allHttpOK = false
			httpChecks = append(httpChecks, HttpCheckResult{
				Domain:    d.Domain,
				URL:       targetURL,
				LatencyMS: latency,
				OK:        false,
				Error:     reqErr.Error(),
			})
		} else {
			_ = resp.Body.Close()
			okStatus := resp.StatusCode >= 200 && resp.StatusCode < 400
			if !okStatus {
				allHttpOK = false
			}
			httpChecks = append(httpChecks, HttpCheckResult{
				Domain:     d.Domain,
				URL:        targetURL,
				StatusCode: resp.StatusCode,
				LatencyMS:  latency,
				OK:         okStatus,
			})
		}
	}

	healthy := allRunning && (len(domains) == 0 || allHttpOK)

	return map[string]interface{}{
		"app_id":      appID,
		"app_name":    app.Name,
		"project":     project,
		"healthy":     healthy,
		"containers":  containers,
		"http_checks": httpChecks,
		"compose_ok":  psRes.OK,
	}, nil
}

func (h *Handler) handleAppHealthCheck(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer)
	if err != nil {
		return errorResult(err)
	}
	health, err := h.collectAppHealth(ctx, app, appID)
	if err != nil {
		return errorResult(err)
	}
	return jsonResult(health)
}

func truncateLogLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func (h *Handler) handleFileSearch(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	query := getStringArg(args, "query")
	pattern := strings.TrimSpace(getStringArg(args, "pattern"))
	subPath := strings.TrimSpace(getStringArg(args, "path"))
	maxResults := getIntArg(args, "max_results", 50)
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}
	if pattern == "" {
		pattern = "*"
	}

	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer)
	_ = app
	if err != nil {
		return errorResult(err)
	}

	rootPath := h.p.Store.Path(appID)
	targetDir := rootPath
	if subPath != "" {
		var err error
		targetDir, err = h.p.Store.SafeFilePath(appID, subPath)
		if err != nil {
			return errorResult(fmt.Errorf("invalid path: %w", err))
		}
	}

	type ContentMatch struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}

	var contentMatches []ContentMatch
	var fileMatches []string
	filesSearched := 0
	lowerQuery := strings.ToLower(query)

	ignoredDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"vendor":       true,
		".venv":        true,
		"venv":         true,
		"__pycache__":   true,
		"dist":         true,
		"build":        true,
		".next":        true,
		".nuxt":        true,
		".turbo":       true,
	}

	err = filepath.WalkDir(targetDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if ignoredDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		matched, _ := filepath.Match(pattern, name)
		if !matched && pattern != "*" {
			return nil
		}

		rel, err := filepath.Rel(rootPath, p)
		if err != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)

		if query == "" {
			fileMatches = append(fileMatches, rel)
			if len(fileMatches) >= maxResults {
				return filepath.SkipAll
			}
			return nil
		}

		filesSearched++
		file, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer file.Close()

		header := make([]byte, 512)
		n, _ := file.Read(header)
		if bytes.IndexByte(header[:n], 0) != -1 {
			return nil
		}
		_, _ = file.Seek(0, io.SeekStart)

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 256*1024)
		namesOnly := getBoolArg(args, "names_only")
		lineNum := 1
		for scanner.Scan() {
			lineText := scanner.Text()
			if strings.Contains(strings.ToLower(lineText), lowerQuery) {
				if namesOnly {
					fileMatches = append(fileMatches, rel)
					if len(fileMatches) >= maxResults {
						return filepath.SkipAll
					}
					break
				}
				trimmed := strings.TrimSpace(lineText)
				if len(trimmed) > 300 {
					trimmed = trimmed[:297] + "..."
				}
				contentMatches = append(contentMatches, ContentMatch{
					File:    rel,
					Line:    lineNum,
					Content: trimmed,
				})
				if len(contentMatches) >= maxResults {
					return filepath.SkipAll
				}
			}
			lineNum++
		}
		return nil
	})

	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return errorResult(fmt.Errorf("search error: %w", err))
	}

	resMap := map[string]interface{}{
		"app_id":  appID,
		"pattern": pattern,
	}
	namesOnly := getBoolArg(args, "names_only")
	if query != "" && !namesOnly {
		resMap["query"] = query
		resMap["matches"] = contentMatches
		resMap["total_matches"] = len(contentMatches)
		resMap["files_searched"] = filesSearched
	} else {
		resMap["files"] = fileMatches
		resMap["total_files"] = len(fileMatches)
		if query != "" {
			resMap["query"] = query
			resMap["files_searched"] = filesSearched
		}
	}

	return jsonResult(resMap)
}

func textResult(s string) (CallToolResult, error) {
	return CallToolResult{
		Content: []ContentItem{{Type: "text", Text: s}},
	}, nil
}

func jsonResult(v interface{}) (CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return errorResult(err)
	}
	return CallToolResult{
		Content: []ContentItem{{Type: "text", Text: string(b)}},
	}, nil
}

func errorResult(err error) (CallToolResult, error) {
	return CallToolResult{
		Content: []ContentItem{{Type: "text", Text: err.Error()}},
		IsError: true,
	}, nil
}

func getStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func getBoolArg(args map[string]interface{}, key string) bool {
	if args == nil {
		return false
	}
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return strings.EqualFold(val, "true") || val == "1"
		}
	}
	return false
}

func getIntArg(args map[string]interface{}, key string, def int) int {
	if args == nil {
		return def
	}
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return def
}

func parseDotEnv(s string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			k := strings.TrimSpace(line[:idx])
			v := strings.TrimSpace(line[idx+1:])
			v = strings.Trim(v, `"'`)
			out[k] = v
		}
	}
	return out
}

func setDotEnvVar(envText, key, val string) string {
	lines := strings.Split(envText, "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") {
			lines[i] = fmt.Sprintf("%s=%s", key, val)
			found = true
			break
		}
	}
	if !found {
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines[len(lines)-1] = fmt.Sprintf("%s=%s", key, val)
		} else {
			lines = append(lines, fmt.Sprintf("%s=%s", key, val))
		}
	}
	return strings.Join(lines, "\n")
}
