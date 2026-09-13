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
	"panel/internal/dev"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/gitx"
	"panel/internal/handlers"
	"panel/internal/runutil"
	"panel/internal/sandbox"
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
	case "file_list":
		return h.handleFileList(ctx, u, params.Arguments)
	case "file_read":
		return h.handleFileRead(ctx, u, params.Arguments)
	case "file_write":
		return h.handleFileWrite(ctx, u, params.Arguments)
	case "file_delete":
		return h.handleFileDelete(ctx, u, params.Arguments)
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
	case "redeploy":
		return h.handleDeploy(ctx, u, params.Arguments, "Redeploy (pull + up)", dockerx.ComposePullUp)
	case "restart":
		return h.handleRestart(ctx, u, params.Arguments)
	case "stop":
		return h.handleDeploy(ctx, u, params.Arguments, "Stop", dockerx.ComposeDown)
	case "deploy_status":
		return h.handleDeployStatus(ctx, u, params.Arguments)
	case "container_logs":
		return h.handleContainerLogs(ctx, u, params.Arguments)
	case "deploy_log_tail":
		return h.handleDeployLogTail(ctx, u, params.Arguments)
	case "dev_mode_set":
		return h.handleDevModeSet(ctx, u, params.Arguments)
	case "reset_dev_deps":
		return h.handleResetDevDeps(ctx, u, params.Arguments)
	case "container_exec":
		return h.handleContainerExec(ctx, u, params.Arguments)
	case "server_exec":
		return h.handleServerExec(ctx, u, params.Arguments)
	case "git_pull":
		return h.handleGitPull(ctx, u, params.Arguments)
	case "file_write_batch":
		return h.handleFileWriteBatch(ctx, u, params.Arguments)
	case "deploy_and_wait":
		return h.handleDeployAndWait(ctx, u, params.Arguments)
	case "file_patch":
		return h.handleFilePatch(ctx, u, params.Arguments)
	case "app_health_check":
		return h.handleAppHealthCheck(ctx, u, params.Arguments)
	case "file_search":
		return h.handleFileSearch(ctx, u, params.Arguments)
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
		DevMode   bool     `json:"dev_mode"`
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
			DevMode:   a.DevMode,
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
	return jsonResult(map[string]interface{}{
		"app":      app,
		"domains":  domains,
		"services": svcs,
		"ps":       psRows,
	})
}

func (h *Handler) handleFileList(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
	}
	children, err := h.p.Store.ListChildren(appID, path)
	if err != nil {
		return errorResult(fmt.Errorf("file listing failed: %w", err))
	}
	type item struct {
		Name    string `json:"name"`
		RelPath string `json:"rel_path"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size"`
		ModTime int64  `json:"mod_time"`
	}
	var out []item
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

func (h *Handler) handleFileRead(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
	}
	full, err := h.p.Store.SafeFilePath(appID, path)
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
	b, err := os.ReadFile(full)
	if err != nil {
		return errorResult(err)
	}
	return textResult(string(b))
}

func (h *Handler) writeWorkspaceFile(ctx context.Context, app db.App, path, content string) error {
	full, err := h.p.Store.SafeFilePath(app.ID, path)
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
	return textResult(fmt.Sprintf("Saved %d bytes to %s", len(content), path))
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
	return jsonResult(map[string]interface{}{
		"app_id":  appID,
		"written": written,
		"count":   len(written),
		"message": fmt.Sprintf("Successfully wrote %d file(s)", len(written)),
	})
}

func (h *Handler) handleFileDelete(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper); err != nil {
		return errorResult(err)
	}
	if filepath.Base(path) == ".nextdeploy.generated.compose.yml" {
		return errorResult(errors.New("cannot delete internal generated files"))
	}
	if err := h.p.Store.RemoveRel(appID, path); err != nil {
		return errorResult(fmt.Errorf("delete failed: %w", err))
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
	key := strings.TrimSpace(getStringArg(args, "key"))
	value := getStringArg(args, "value")
	if key == "" {
		return errorResult(errors.New("key is required"))
	}
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper); err != nil {
		return errorResult(err)
	}
	cur, _ := h.p.DB.GetPanelEnv(ctx, appID)
	updated := setDotEnvVar(cur, key, value)
	if err := h.p.DB.UpdatePanelEnv(ctx, appID, updated); err != nil {
		return errorResult(err)
	}
	root := h.p.ComposeWorkspaceRoot(ctx, appID)
	_ = h.p.SyncWorkspaceEnvFromPanel(appID, root, updated)
	_ = h.p.SyncAppCaddyOverrideCtx(ctx, appID)
	return textResult(fmt.Sprintf("Environment variable %q set successfully", key))
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
	return textResult(string(b))
}

func (h *Handler) handleDeploy(ctx context.Context, u db.User, args map[string]interface{}, action string, fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}
	v, _ := h.p.ComposeMu.LoadOrStore(appID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	if app.DevMode && action == "Deploy" {
		fn = dockerx.ComposeApply
	}

	// Synchronize latest code from Git if app is Git-connected and Dev Mode is off (matches panel web UI).
	if h.p.IsGitApp(ctx, appID) && !app.DevMode && (action == "Deploy" || action == "Redeploy (pull + up)") {
		syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Minute)
		_, syncErr := h.p.SyncGitAppSource(syncCtx, appID)
		syncCancel()
		if syncErr != nil {
			return errorResult(fmt.Errorf("git sync failed before deploy: %w", syncErr))
		}
		h.p.InvalidateAfterAppWorkspaceChange(appID)
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
	return jsonResult(map[string]interface{}{
		"job_id":  jobID,
		"app_id":  appID,
		"action":  action,
		"status":  "started",
		"message": "Deployment job started in background. Poll deploy_status with job_id for progress.",
	})
}

func (h *Handler) handleRestart(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
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
		if err := dockerapi.RestartContainerByName(ctx, cid); err != nil {
			return errorResult(fmt.Errorf("restart failed: %w", err))
		}
		return textResult(fmt.Sprintf("Restarted container %s for service %q", cid, service))
	}
	return h.handleDeploy(ctx, u, args, "Stack restart", dockerx.ComposeRestart)
}

func (h *Handler) handleDeployStatus(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	jobID := strings.TrimSpace(getStringArg(args, "job_id"))
	appID := strings.TrimSpace(getStringArg(args, "app_id"))
	if jobID != "" {
		if job, ok := h.p.GetDeployJob(jobID); ok {
			if _, err := h.hasAppAccess(ctx, u, job.AppID, db.CollabRoleViewer); err != nil {
				return errorResult(err)
			}
			return jsonResult(job)
		}
	}
	if appID != "" {
		if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
			return errorResult(err)
		}
		if job, ok := h.p.LatestDeployJobForApp(appID); ok {
			return jsonResult(job)
		}
		out, action, running := h.p.DeploySnapshot(appID)
		return jsonResult(map[string]interface{}{
			"app_id":  appID,
			"action":  action,
			"running": running,
			"output":  out,
		})
	}
	return errorResult(errors.New("either job_id or app_id must be provided"))
}

func (h *Handler) handleContainerLogs(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	service := strings.TrimSpace(getStringArg(args, "service"))
	tail := getIntArg(args, "tail", 100)
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
	return textResult(raw)
}

func (h *Handler) handleDeployLogTail(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	limit := getIntArg(args, "limit", 5)
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer); err != nil {
		return errorResult(err)
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

func (h *Handler) handleDevModeSet(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	enabled := getBoolArg(args, "enabled")
	service := strings.TrimSpace(getStringArg(args, "service"))
	target := strings.TrimSpace(getStringArg(args, "target"))
	command := getStringArg(args, "command")
	if _, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper); err != nil {
		return errorResult(err)
	}
	if enabled && (target == "" || !filepath.IsAbs(target) || !strings.HasPrefix(target, "/")) {
		return errorResult(errors.New("target path must be an absolute path inside container (e.g. /app)"))
	}
	if err := h.p.DB.UpdateAppDevMode(ctx, appID, enabled, service, target, command); err != nil {
		return errorResult(err)
	}
	_ = h.p.SyncAndApplyBackground(ctx, appID)
	return textResult(fmt.Sprintf("Dev mode saved (enabled=%v, service=%s, target=%s)", enabled, service, target))
}

func (h *Handler) handleResetDevDeps(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}
	volPrefix := fmt.Sprintf("nddev_%s_", appID)
	listCtx, listCancel := context.WithTimeout(ctx, 15*time.Second)
	cmd := exec.CommandContext(listCtx, "docker", "volume", "ls", "-q", "--filter", "name="+volPrefix)
	out, _ := cmd.Output()
	listCancel()

	var vols []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.HasPrefix(line, volPrefix) {
			vols = append(vols, line)
		}
	}
	if len(vols) == 0 {
		return textResult("No dev dependency volumes found to reset.")
	}

	var targetServices []string
	if s := strings.TrimSpace(app.DevService); s != "" {
		targetServices = []string{s}
	} else {
		svcs := h.p.LoadComposeServices(ctx, appID)
		svcSet := make(map[string]bool)
		for _, v := range vols {
			if s := dev.MatchDevVolumeService(v, volPrefix, svcs); s != "" {
				svcSet[s] = true
			}
		}
		for s := range svcSet {
			targetServices = append(targetServices, s)
		}
		if len(targetServices) == 0 {
			targetServices = svcs
		}
	}

	go func() {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer bgCancel()
		project := h.p.ActiveComposeProjectName(bgCtx, app, appID)
		dir := h.p.AppSourcePath(bgCtx, appID)
		paths := h.p.EffectiveComposePaths(bgCtx, app, appID)
		envFiles := h.p.ComposeEnvFiles(bgCtx, appID)

		_ = dockerx.ComposeRmServices(bgCtx, dir, paths, project, nil, envFiles, targetServices...)
		var volErrs []string
		for _, v := range vols {
			if out, err := exec.CommandContext(bgCtx, "docker", "volume", "rm", "-f", v).CombinedOutput(); err != nil {
				volErrs = append(volErrs, fmt.Sprintf("%s (%s)", v, strings.TrimSpace(string(out))))
			}
		}
		res := dockerx.ComposeApplyServices(bgCtx, dir, paths, project, nil, envFiles, targetServices...)
		ok := res.OK && len(volErrs) == 0
		msg := fmt.Sprintf("Reset %d dev volume(s) for service(s) [%s]. Containers recreated.", len(vols), strings.Join(targetServices, ", "))
		_ = h.p.DB.InsertDeployLog(bgCtx, appID, "Reset dev dependencies", ok, msg)
	}()

	return textResult(fmt.Sprintf("Resetting %d dev volume(s) for service(s) [%s] in the background.", len(vols), strings.Join(targetServices, ", ")))
}

func (h *Handler) handleContainerExec(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	// container_exec is high-privilege; require explicit token flag.
	tok, ok := ctx.Value(apiTokenContextKey{}).(db.APIToken)
	if !ok || !tok.AllowServerExec {
		return errorResult(errors.New("permission denied: container_exec is restricted. Enable 'Allow server_exec' for this API token in NextDeploy Panel under MCP Settings (/mcp-docs)"))
	}

	command := getStringArg(args, "command")
	if command == "" {
		return errorResult(errors.New("command is required"))
	}
	service := strings.TrimSpace(getStringArg(args, "service"))
	workDir := strings.TrimSpace(getStringArg(args, "work_dir"))
	timeoutSec := getIntArg(args, "timeout_seconds", 60)
	if timeoutSec < 1 {
		timeoutSec = 1
	} else if timeoutSec > 300 {
		timeoutSec = 300
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

	res := dockerx.DockerExecWorkDir(execCtx, targetContainer, command, workDir)

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
		return errorResult(errors.New("permission denied: server_exec is restricted. Enable 'Allow server_exec' for this API token in NextDeploy Panel under MCP Settings (/mcp-docs)"))
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
		"command": command,
		"ok":      res.OK,
		"output":  res.Output,
	})
}

func (h *Handler) handleGitPull(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	branch := strings.TrimSpace(getStringArg(args, "branch"))
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleDeveloper)
	if err != nil {
		return errorResult(err)
	}

	cfg, err := h.p.DB.GetAppGitConfig(ctx, appID)
	if err != nil || strings.TrimSpace(cfg.RepoURL) == "" {
		return errorResult(errors.New("no git repository configured for this app. Connect a git repo in the panel first"))
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

	repoDir := h.p.AppCheckoutPath(appID)
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
	timeoutSec := getIntArg(args, "timeout_seconds", 180)
	if timeoutSec <= 0 {
		timeoutSec = 180
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	res, err := h.handleDeploy(ctx, u, args, "Deploy", dockerx.ComposeUp)
	if err != nil || res.IsError {
		return res, err
	}

	var startInfo map[string]interface{}
	if len(res.Content) > 0 {
		_ = json.Unmarshal([]byte(res.Content[0].Text), &startInfo)
	}
	jobID, _ := startInfo["job_id"].(string)
	if jobID == "" {
		return errorResult(errors.New("failed to retrieve job_id for deployment"))
	}

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
			output := truncateLogLines(job.Output, 60)
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

func (h *Handler) handleAppHealthCheck(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	app, err := h.hasAppAccess(ctx, u, appID, db.CollabRoleViewer)
	if err != nil {
		return errorResult(err)
	}

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

	return jsonResult(map[string]interface{}{
		"app_id":      appID,
		"app_name":    app.Name,
		"project":     project,
		"healthy":     healthy,
		"containers":  containers,
		"http_checks": httpChecks,
		"compose_ok":  psRes.OK,
	})
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
		lineNum := 1
		for scanner.Scan() {
			lineText := scanner.Text()
			if strings.Contains(strings.ToLower(lineText), lowerQuery) {
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
	if query != "" {
		resMap["query"] = query
		resMap["matches"] = contentMatches
		resMap["total_matches"] = len(contentMatches)
		resMap["files_searched"] = filesSearched
	} else {
		resMap["files"] = fileMatches
		resMap["total_files"] = len(fileMatches)
	}

	return jsonResult(resMap)
}

func textResult(s string) (CallToolResult, error) {
	return CallToolResult{
		Content: []ContentItem{{Type: "text", Text: s}},
	}, nil
}

func jsonResult(v interface{}) (CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
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
		if b, ok := v.(bool); ok {
			return b
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
