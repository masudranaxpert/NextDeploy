package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dev"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/handlers"
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

// hasAppAccess validates that the user has permission to access the specified app.
func (h *Handler) hasAppAccess(ctx context.Context, u db.User, appID string) (db.App, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return db.App{}, errors.New("app_id is required")
	}
	app, err := h.p.DB.GetApp(ctx, appID)
	if err != nil {
		return db.App{}, fmt.Errorf("app %q not found", appID)
	}
	if u.Role == db.RoleAdmin {
		return app, nil
	}
	if app.OwnerID == u.ID {
		return app, nil
	}
	collabs, err := h.p.DB.ListCollaborators(ctx, appID)
	if err == nil {
		for _, c := range collabs {
			if c.UserID == u.ID {
				return app, nil
			}
		}
	}
	return db.App{}, errors.New("forbidden: you do not have access to this app")
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
	app, err := h.hasAppAccess(ctx, u, appID)
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
	if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
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
	if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
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

func (h *Handler) handleFileWrite(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	content := getStringArg(args, "content")
	app, err := h.hasAppAccess(ctx, u, appID)
	if err != nil {
		return errorResult(err)
	}
	full, err := h.p.Store.SafeFilePath(appID, path)
	if err != nil {
		return errorResult(fmt.Errorf("invalid path: %w", err))
	}
	if filepath.Base(path) == ".nextdeploy.generated.compose.yml" {
		return errorResult(errors.New(".nextdeploy.generated.compose.yml is managed by NextDeploy; edit docker-compose.yml instead"))
	}
	cleanRel := filepath.ToSlash(strings.Trim(path, "/"))
	if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == "compose.yml" || cleanRel == "compose.yaml" || cleanRel == app.ComposeFile {
		if err := sandbox.CheckComposeSecurity([]byte(content)); err != nil {
			return errorResult(fmt.Errorf("compose security validation failed: %w", err))
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return errorResult(err)
	}
	if err := os.WriteFile(full, []byte(content), 0640); err != nil {
		return errorResult(err)
	}
	h.p.InvalidateAfterAppWorkspaceChange(appID)
	if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == app.ComposeFile {
		_ = h.p.SyncAppCaddyOverrideCtx(ctx, appID)
	}
	return textResult(fmt.Sprintf("Saved %d bytes to %s", len(content), path))
}

func (h *Handler) handleFileDelete(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	path := getStringArg(args, "path")
	if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
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
	if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
		return errorResult(err)
	}
	panelEnv, _ := h.p.DB.GetPanelEnv(ctx, appID)
	parsed := parseDotEnv(panelEnv)
	return jsonResult(map[string]interface{}{
		"raw":   panelEnv,
		"env":   parsed,
		"count": len(parsed),
	})
}

func (h *Handler) handleEnvSet(ctx context.Context, u db.User, args map[string]interface{}) (CallToolResult, error) {
	appID := getStringArg(args, "app_id")
	key := strings.TrimSpace(getStringArg(args, "key"))
	value := getStringArg(args, "value")
	if key == "" {
		return errorResult(errors.New("key is required"))
	}
	if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
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
	app, err := h.hasAppAccess(ctx, u, appID)
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
	app, err := h.hasAppAccess(ctx, u, appID)
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
	app, err := h.hasAppAccess(ctx, u, appID)
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
			if _, err := h.hasAppAccess(ctx, u, job.AppID); err != nil {
				return errorResult(err)
			}
			return jsonResult(job)
		}
	}
	if appID != "" {
		if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
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
	app, err := h.hasAppAccess(ctx, u, appID)
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
	if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
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
	if _, err := h.hasAppAccess(ctx, u, appID); err != nil {
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
	app, err := h.hasAppAccess(ctx, u, appID)
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
