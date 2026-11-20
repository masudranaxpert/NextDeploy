package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/gitx"
	"panel/internal/phphosting"
	"panel/internal/proxy"
	"panel/internal/quota"
	"panel/internal/runutil"
	"panel/internal/sandbox"
	"panel/internal/wsmanifest"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"
)

// apiAppAccess validates authentication, app existence, RBAC role, and non-suspended status for writes.
// On failure it writes a JSON error response and returns a non-nil sentinel error.
func (p *Panel) apiAppAccess(c *fiber.Ctx, appID, requiredRole string) (db.User, db.App, error) {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return db.User{}, db.App{}, c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return db.User{}, db.App{}, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "app_id is required"})
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return db.User{}, db.App{}, c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("app %q not found", appID)})
	}
	allowed, err := p.CanAccessApp(ctx, u.ID, u.Role, appID, requiredRole)
	if err != nil {
		return db.User{}, db.App{}, c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if !allowed {
		return db.User{}, db.App{}, c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": fmt.Sprintf("forbidden: requires %s access to app %q", requiredRole, appID)})
	}
	if requiredRole != db.CollabRoleViewer && app.Status == db.AppStatusSuspended && u.Role != db.RoleAdmin {
		return db.User{}, db.App{}, c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": fmt.Sprintf("forbidden: app %q is suspended and cannot be modified", appID)})
	}
	return u, app, nil
}

// APIAppGet returns app metadata, domains, services, container status, and optional HTTP health checks.
// GET /api/v1/apps/:id?include_health=true
func (p *Panel) APIAppGet(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleViewer)
	if err != nil {
		return nil
	}
	domains, _ := p.DB.ListAppDomains(ctx, appID)
	svcs := p.LoadComposeServices(ctx, appID)
	_, psRows, _ := p.ComposeProjectAndPS(ctx, app, appID)
	out := fiber.Map{
		"app":      app,
		"domains":  domains,
		"services": svcs,
		"ps":       psRows,
	}
	if c.QueryBool("include_health", false) {
		out["health"] = p.collectAppHealth(ctx, app, appID)
		// Containers whose processes were killed for exceeding the memory limit.
		var oom []string
		for _, row := range psRows {
			if info, err := dockerapi.InspectContainerReadiness(ctx, row.Name); err == nil && info.OOMKilled {
				oom = append(oom, row.Name)
			}
		}
		if len(oom) > 0 {
			out["oom_killed"] = oom
		}
	}
	// PHP and static sites run in the owner's shared PHP runtime, not in their own containers.
	if site, err := p.DB.GetPHPSite(ctx, appID); err == nil {
		runtime := phphosting.RuntimeContainer(site.OwnerID, site.PHPVersion)
		running := false
		if info, err := dockerapi.InspectContainerReadiness(ctx, runtime); err == nil {
			running = info.Running
		}
		out["php"] = fiber.Map{
			"site_type":   site.SiteType,
			"php_version": site.PHPVersion,
			"runtime":     runtime,
			"running":     running,
		}
	}
	return c.JSON(out)
}

func (p *Panel) collectAppHealth(ctx context.Context, app db.App, appID string) map[string]interface{} {
	project, psRows, psRes := p.ComposeProjectAndPS(ctx, app, appID)
	containers := make([]map[string]interface{}, 0, len(psRows))
	allRunning := len(psRows) > 0
	for _, row := range psRows {
		if !strings.EqualFold(row.State, "running") {
			allRunning = false
		}
		containers = append(containers, map[string]interface{}{
			"service": row.Service,
			"name":    row.Name,
			"state":   row.State,
			"status":  row.Status,
		})
	}

	domains, _ := p.DB.ListAppDomains(ctx, appID)
	type httpCheckResult struct {
		Domain     string `json:"domain"`
		URL        string `json:"url"`
		StatusCode int    `json:"status_code,omitempty"`
		LatencyMS  int64  `json:"latency_ms"`
		OK         bool   `json:"ok"`
		Error      string `json:"error,omitempty"`
	}
	httpChecks := make([]httpCheckResult, 0, len(domains))
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("stopped after 3 redirects")
			}
			return nil
		},
	}

	allHTTPOK := true
	for _, d := range domains {
		targetURL := fmt.Sprintf("https://%s", d.Domain)
		start := time.Now()
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		var resp *http.Response
		if reqErr == nil {
			resp, reqErr = httpClient.Do(req)
		}
		if reqErr != nil {
			targetURL = fmt.Sprintf("http://%s", d.Domain)
			reqHTTP, _ := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
			start = time.Now()
			resp, reqErr = httpClient.Do(reqHTTP)
		}
		latency := time.Since(start).Milliseconds()

		if reqErr != nil {
			allHTTPOK = false
			httpChecks = append(httpChecks, httpCheckResult{
				Domain:    d.Domain,
				URL:       targetURL,
				LatencyMS: latency,
				OK:        false,
				Error:     reqErr.Error(),
			})
			continue
		}
		_ = resp.Body.Close()
		okStatus := resp.StatusCode >= 200 && resp.StatusCode < 400
		if !okStatus {
			allHTTPOK = false
		}
		httpChecks = append(httpChecks, httpCheckResult{
			Domain:     d.Domain,
			URL:        targetURL,
			StatusCode: resp.StatusCode,
			LatencyMS:  latency,
			OK:         okStatus,
		})
	}

	return map[string]interface{}{
		"app_id":      appID,
		"app_name":    app.Name,
		"project":     project,
		"healthy":     allRunning && (len(domains) == 0 || allHTTPOK),
		"containers":  containers,
		"http_checks": httpChecks,
		"compose_ok":  psRes.OK,
	}
}

// APIAppCreate creates a new application via CLI/API.
// POST /api/v1/apps
func (p *Panel) APIAppCreate(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req struct {
		Name           string `json:"name"`
		SourceType     string `json:"source_type"`
		RepoURL        string `json:"repo_url"`
		Branch         string `json:"branch"`
		ComposeContent string `json:"compose_content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	slug, err := p.ValidateAppSlug(req.Name)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	exists, err := p.DB.AppNameExistsForUser(ctx, slug, u.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if exists {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": fmt.Sprintf("an app with the name %q already exists", slug)})
	}
	if err := quota.CheckAppSlot(ctx, p.DB, u); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
	}

	var id string
	for {
		suffix := p.RandomAppSuffix()
		id = fmt.Sprintf("%s-%s", slug, suffix)
		if _, err := p.DB.GetApp(ctx, id); err != nil {
			break
		}
	}

	wsPath := p.Store.Path(id)
	if err := os.MkdirAll(wsPath, 0750); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed creating workspace: %v", err)})
	}
	if err := p.Store.WriteMeta(id, slug); err != nil {
		_ = os.RemoveAll(wsPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed writing meta: %v", err)})
	}
	if err := quota.CreateApp(ctx, p.DB, u, id, slug); err != nil {
		_ = os.RemoveAll(wsPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed creating app record: %v", err)})
	}

	sourceType := strings.ToLower(strings.TrimSpace(req.SourceType))
	repoURL := strings.TrimSpace(req.RepoURL)
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = "main"
	}
	if sourceType == "git" || sourceType == "github" || repoURL != "" {
		_ = p.DB.SetAppSourceType(ctx, id, "git")
		if repoURL != "" {
			cfg := db.AppGitConfig{
				AppID:         id,
				Provider:      "git",
				RepoURL:       NormalizeRepoURL(repoURL),
				RepoFullName:  RepoFullNameFromURL(repoURL),
				Branch:        NormalizeBranch(branch),
				AuthMode:      "public",
				WebhookSecret: RandomSecret(),
				AutoDeploy:    true,
			}
			_ = p.DB.UpsertAppGitConfig(ctx, cfg)
			_ = os.MkdirAll(filepath.Join(p.Store.ReservedPath(id), "repo"), 0750)
		}
	}

	if strings.TrimSpace(req.ComposeContent) != "" {
		if err := sandbox.CheckComposeSecurity([]byte(req.ComposeContent), wsPath); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("initial compose security check failed: %v", err)})
		}
		_ = os.WriteFile(filepath.Join(wsPath, "docker-compose.yml"), []byte(req.ComposeContent), 0640)
		_ = p.SyncAppCaddyOverrideCtx(ctx, id)
	}

	p.RecordAuditLog(c, "create_app", "app", id, fmt.Sprintf("Created app %s (%s) via CLI/API", slug, id))
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"ok":          true,
		"app_id":      id,
		"name":        slug,
		"status":      "created",
		"source_type": sourceType,
		"message":     fmt.Sprintf("Application %q successfully created with ID %q", slug, id),
	})
}

// APIAppComposeGet returns the effective docker-compose configuration for an app.
// GET /api/v1/apps/:id/compose?summary=true&service=web
func (p *Panel) APIAppComposeGet(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleViewer)
	if err != nil {
		return nil
	}

	_ = p.SyncAppCaddyOverrideCtx(ctx, appID)
	overridePath := p.ComposeOverridePath(ctx, appID)
	b, err := os.ReadFile(overridePath)
	if err != nil {
		cp := p.ComposeFilePath(ctx, app, appID)
		b, err = os.ReadFile(cp)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "compose file not found"})
		}
	}

	summary := c.QueryBool("summary", false)
	targetService := strings.TrimSpace(c.Query("service"))
	if summary || targetService != "" {
		var doc map[string]interface{}
		if yerr := yaml.Unmarshal(b, &doc); yerr == nil {
			services, _ := doc["services"].(map[string]interface{})
			if targetService != "" {
				svc, ok := services[targetService]
				if !ok {
					return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("service %q not found in docker-compose.yml", targetService)})
				}
				outYAML, merr := yaml.Marshal(map[string]interface{}{targetService: svc})
				if merr == nil {
					return c.JSON(fiber.Map{"app_id": appID, "service": targetService, "content": string(outYAML)})
				}
				return c.JSON(fiber.Map{"app_id": appID, "service": targetService, targetService: svc})
			}
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
			return c.JSON(fiber.Map{"app_id": appID, "services": summaryMap})
		}
	}

	return c.JSON(fiber.Map{"app_id": appID, "content": string(b)})
}

// APIAppDeploy deploys or redeploys an app and optionally waits for completion.
// POST /api/v1/apps/:id/deploy
func (p *Panel) APIAppDeploy(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	var req struct {
		Rebuild        bool `json:"rebuild"`
		GitPull        bool `json:"git_pull"`
		WaitSeconds    int  `json:"wait_seconds"`
		TimeoutSeconds int  `json:"timeout_seconds"`
		SummaryOnly    *bool `json:"summary_only"`
	}
	_ = c.BodyParser(&req)

	action := "Deploy"
	fn := dockerx.ComposeUp
	if req.Rebuild {
		action = "Redeploy (pull + up)"
		fn = dockerx.ComposePullUp
	}

	v, _ := p.ComposeMu.LoadOrStore(appID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			mu.Unlock()
		}
	}()

	var gitSyncWarning, gitSyncOut string
	if p.IsGitApp(ctx, appID) {
		repoDir := p.AppCheckoutPath(appID)
		if req.GitPull || !gitx.IsDirty(ctx, repoDir) {
			syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Minute)
			out, syncErr := p.SyncGitAppSource(syncCtx, appID)
			syncCancel()
			if syncErr != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("git sync failed before deploy: %v", syncErr)})
			}
			gitSyncOut = out
			p.InvalidateAfterAppWorkspaceChange(appID)
		} else {
			gitSyncWarning = "Git pull skipped: workspace has local changes (dirty). Deploying from current files. Use 'nd deploy --force' to force-pull from remote (discards local workspace edits)."
			gitSyncOut = gitSyncWarning
		}
	}

	if p.DB.IsPHPSite(ctx, appID) {
		if p.PHP == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "PHP hosting manager not initialized"})
		}
		sctx, scancel := context.WithTimeout(ctx, 2*time.Minute)
		defer scancel()
		if err := p.PHP.SyncSite(sctx, appID); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		p.PHP.ReloadCaddy(sctx)
		_ = p.DB.InsertDeployLog(ctx, appID, action, true, gitSyncOut)
		out := fiber.Map{
			"app_id":  appID,
			"action":  action,
			"ok":      true,
			"status":  "success",
			"message": "PHP site synced and restarted",
		}
		if gitSyncWarning != "" {
			out["git_sync_warning"] = gitSyncWarning
		}
		return c.JSON(out)
	}

	project, paths, err := p.ResolveComposeActionTarget(ctx, app, appID, action)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("compose sync failed: %v", err)})
	}
	jobID, err := p.StartComposeJob(appID, project, paths, action, fn, gitSyncOut)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to start %s job: %v", action, err)})
	}

	waitSec := req.WaitSeconds
	if waitSec <= 0 {
		waitSec = req.TimeoutSeconds
	}
	summaryOnly := true
	if req.SummaryOnly != nil {
		summaryOnly = *req.SummaryOnly
	}
	if waitSec > 0 {
		if waitSec > 300 {
			waitSec = 300
		}
		mu.Unlock()
		unlocked = true
		return c.JSON(p.waitForAPIDeployJob(ctx, jobID, appID, action, waitSec, summaryOnly, gitSyncWarning))
	}

	out := fiber.Map{
		"job_id":  jobID,
		"app_id":  appID,
		"action":  action,
		"status":  "started",
		"message": "Deployment job started in background.",
	}
	if gitSyncWarning != "" {
		out["git_sync_warning"] = gitSyncWarning
	}
	return c.JSON(out)
}

func (p *Panel) waitForAPIDeployJob(ctx context.Context, jobID, appID, action string, timeoutSec int, summaryOnly bool, gitSyncWarning string) fiber.Map {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for {
		if ctx.Err() != nil {
			return fiber.Map{"job_id": jobID, "app_id": appID, "action": action, "ok": false, "status": "cancelled", "message": ctx.Err().Error()}
		}
		job, found := p.GetDeployJob(jobID)
		if !found {
			return fiber.Map{"job_id": jobID, "app_id": appID, "action": action, "ok": false, "status": "failed", "message": "deploy job disappeared"}
		}
		if !job.Running {
			if summaryOnly && job.OK {
				res := fiber.Map{
					"job_id":     job.JobID,
					"app_id":     job.AppID,
					"action":     job.Action,
					"ok":         true,
					"running":    false,
					"status":     "success",
					"duration_s": time.Since(job.CreatedAt).Round(time.Second).Seconds(),
					"message":    "Deployment succeeded",
				}
				if gitSyncWarning != "" {
					res["git_sync_warning"] = gitSyncWarning
				}
				return res
			}
			tailLines := 60
			if summaryOnly && !job.OK {
				tailLines = 40
			}
			res := fiber.Map{
				"job_id":      job.JobID,
				"app_id":      job.AppID,
				"action":      job.Action,
				"ok":          job.OK,
				"running":     false,
				"status":      map[bool]string{true: "success", false: "failed"}[job.OK],
				"duration_s":  time.Since(job.CreatedAt).Round(time.Second).Seconds(),
				"output_tail": truncateLines(job.Output, tailLines),
			}
			if gitSyncWarning != "" {
				res["git_sync_warning"] = gitSyncWarning
			}
			return res
		}
		if time.Now().After(deadline) {
			res := fiber.Map{
				"job_id":      job.JobID,
				"app_id":      job.AppID,
				"action":      job.Action,
				"ok":          false,
				"status":      "timeout",
				"running":     true,
				"message":     fmt.Sprintf("Deployment still running after %d seconds.", timeoutSec),
				"output_tail": truncateLines(job.Output, 40),
			}
			if gitSyncWarning != "" {
				res["git_sync_warning"] = gitSyncWarning
			}
			return res
		}
		time.Sleep(1500 * time.Millisecond)
	}
}

func truncateLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

// APIAppStop stops a PHP site, single compose service container, or full compose stack.
// POST /api/v1/apps/:id/stop
func (p *Panel) APIAppStop(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	var req struct {
		Service     string `json:"service"`
		WaitSeconds int    `json:"wait_seconds"`
	}
	_ = c.BodyParser(&req)
	service := strings.TrimSpace(req.Service)

	if service == "" && p.DB.IsPHPSite(ctx, appID) {
		if p.PHP == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "PHP hosting manager not initialized"})
		}
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := p.PHP.StopSite(sctx, appID); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		p.InvalidateAfterAppDeployChange(appID)
		p.RecordAuditLog(c, "php_site_stop", "app", appID, "Stopped PHP/static site via CLI/API")
		return c.JSON(fiber.Map{"ok": true, "app_id": appID, "status": "stopped", "message": "PHP site stopped"})
	}

	if service != "" {
		project := p.ActiveComposeProjectName(ctx, app, appID)
		cid, err := dockerapi.ContainerIDForComposeService(ctx, project, service)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("container for service %q not found: %v", service, err)})
		}
		if err := dockerapi.StopContainerByName(ctx, cid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("stop failed: %v", err)})
		}
		return c.JSON(fiber.Map{"ok": true, "app_id": appID, "service": service, "message": fmt.Sprintf("Stopped container %s for service %q", cid, service)})
	}

	return p.runComposePowerAction(c, app, appID, "Stop", req.WaitSeconds, dockerx.ComposeDown)
}

// APIAppRestart restarts (or recreates) a PHP site, single compose service, or full compose stack.
// POST /api/v1/apps/:id/restart
func (p *Panel) APIAppRestart(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	var req struct {
		Service     string `json:"service"`
		Recreate    bool   `json:"recreate"`
		WaitSeconds int    `json:"wait_seconds"`
	}
	_ = c.BodyParser(&req)
	service := strings.TrimSpace(req.Service)

	if service == "" && p.DB.IsPHPSite(ctx, appID) {
		if p.PHP == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "PHP hosting manager not initialized"})
		}
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := p.PHP.SyncSite(sctx, appID); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		p.PHP.ReloadCaddy(sctx)
		p.RecordAuditLog(c, "php_site_restart", "app", appID, "Restarted PHP/static site via CLI/API")
		return c.JSON(fiber.Map{"ok": true, "app_id": appID, "status": "running", "message": "PHP site restarted"})
	}

	if service != "" {
		if req.Recreate {
			project := p.ActiveComposeProjectName(ctx, app, appID)
			var composeFiles []string
			switch {
			case strings.HasSuffix(project, "-blue"):
				_, _ = p.SyncAppSlotComposeCtx(ctx, appID, "blue", strings.TrimSpace(app.PinnedSlot) == "green")
				composeFiles = p.EffectiveComposePathsForSlot(ctx, app, appID, "blue")
			case strings.HasSuffix(project, "-green"):
				_, _ = p.SyncAppSlotComposeCtx(ctx, appID, "green", strings.TrimSpace(app.PinnedSlot) == "blue")
				composeFiles = p.EffectiveComposePathsForSlot(ctx, app, appID, "green")
			default:
				_ = p.SyncAppCaddyOverrideCtx(ctx, appID)
				composeFiles = p.EffectiveComposePaths(ctx, app, appID)
			}
			envFiles := p.ComposeEnvFiles(ctx, appID)
			dir := p.AppSourcePath(ctx, appID)
			res := dockerx.ComposeRecreate(ctx, dir, composeFiles, project, nil, envFiles, service)
			if !res.OK {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("recreate failed for service %q: %s", service, res.Output)})
			}
			_ = p.ProxySync(ctx, proxy.Reason{Kind: "app_recreate", ID: appID, Detail: service})
			p.triggerRootCaddySync()
			return c.JSON(fiber.Map{"ok": true, "app_id": appID, "service": service, "message": fmt.Sprintf("Recreated container for service %q", service)})
		}
		project := p.ActiveComposeProjectName(ctx, app, appID)
		cid, err := dockerapi.ContainerIDForComposeService(ctx, project, service)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("container for service %q not found: %v", service, err)})
		}
		if err := dockerapi.RestartContainerByName(ctx, cid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("restart failed: %v", err)})
		}
		return c.JSON(fiber.Map{"ok": true, "app_id": appID, "service": service, "message": fmt.Sprintf("Restarted container %s for service %q", cid, service)})
	}

	if req.Recreate {
		return p.runComposePowerAction(c, app, appID, "Stack recreate", req.WaitSeconds, func(c context.Context, d string, cf []string, proj string, w io.Writer, ef []string) dockerx.Result {
			return dockerx.ComposeRecreate(c, d, cf, proj, w, ef)
		})
	}
	return p.runComposePowerAction(c, app, appID, "Stack restart", req.WaitSeconds, dockerx.ComposeRestart)
}

func (p *Panel) runComposePowerAction(c *fiber.Ctx, app db.App, appID, action string, waitSec int, fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result) error {
	ctx := c.UserContext()
	v, _ := p.ComposeMu.LoadOrStore(appID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	project, paths, err := p.ResolveComposeActionTarget(ctx, app, appID, action)
	if err != nil {
		mu.Unlock()
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("compose sync failed: %v", err)})
	}
	jobID, err := p.StartComposeJob(appID, project, paths, action, fn, "")
	mu.Unlock()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to start %s job: %v", action, err)})
	}
	if waitSec <= 0 {
		waitSec = 60
	} else if waitSec > 300 {
		waitSec = 300
	}
	res := p.waitForAPIDeployJob(ctx, jobID, appID, action, waitSec, true, "")
	if okVal, _ := res["ok"].(bool); !okVal {
		return c.Status(fiber.StatusBadRequest).JSON(res)
	}
	return c.JSON(res)
}

// APIAppLogs returns container logs for an app or specific service.
// GET /api/v1/apps/:id/logs?service=&tail=30&filter_health=true&level=
func (p *Panel) APIAppLogs(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleViewer)
	if err != nil {
		return nil
	}

	service := strings.TrimSpace(c.Query("service"))
	tail := c.QueryInt("tail", 0)
	if tail <= 0 {
		tail = c.QueryInt("lines", 30)
	}
	if tail <= 0 {
		tail = 30
	}
	filterHealth := c.QueryBool("filter_health", true)
	level := strings.ToLower(strings.TrimSpace(c.Query("level")))

	var logRef string
	if p.PHP != nil && p.DB.IsPHPSite(ctx, appID) {
		if site, serr := p.DB.GetPHPSite(ctx, appID); serr == nil {
			if site.SiteType == phphosting.SiteTypeStatic {
				return c.JSON(fiber.Map{"app_id": appID, "service": "static", "logs": "Static sites do not have a running container."})
			}
			logRef = p.PHP.RuntimeName(site)
		}
	}

	if logRef == "" {
		project, composeRows, composeRes := p.ComposeProjectAndPS(ctx, app, appID)
		if service != "" {
			if composeRes.OK && p.ComposeServiceInRows(composeRows, service) {
				if cid, rerr := dockerapi.ContainerIDForComposeService(ctx, project, service); rerr == nil && cid != "" {
					logRef = cid
				}
			}
			if logRef == "" {
				logRef = service
			}
		} else if len(composeRows) > 0 {
			logRef = composeRows[0].Name
		} else {
			logRef = project
		}
	}

	raw, ferr := dockerapi.FetchContainerLogsText(ctx, logRef, tail)
	if ferr != nil && strings.TrimSpace(raw) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("failed fetching container logs: %v", ferr)})
	}

	if filterHealth || level != "" {
		lines := strings.Split(raw, "\n")
		var filtered []string
		for _, l := range lines {
			trimmed := strings.TrimSpace(l)
			if trimmed == "" {
				continue
			}
			lower := strings.ToLower(trimmed)
			if filterHealth && (strings.Contains(lower, "/healthz") || strings.Contains(lower, "/health") ||
				strings.Contains(lower, "get /ping") || strings.Contains(lower, "head / http") ||
				strings.Contains(lower, "kube-probe") || strings.Contains(lower, "docker-healthcheck")) {
				continue
			}
			if level == "error" && !containsAny(lower, "error", "fatal", "exception", "panic", "failed") {
				continue
			}
			if level == "warn" && !containsAny(lower, "warn", "error", "fatal", "exception", "panic", "failed") {
				continue
			}
			filtered = append(filtered, l)
		}
		raw = strings.Join(filtered, "\n")
	}

	return c.JSON(fiber.Map{"app_id": appID, "service": service, "logs": raw})
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// APIAppDeployLogs streams incremental deploy job output or returns historical deploy logs.
// GET /api/v1/apps/:id/deploy-logs?job_id=&since_offset=0&wait_seconds=0&limit=5
func (p *Panel) APIAppDeployLogs(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	jobID := strings.TrimSpace(c.Query("job_id"))
	sinceOffset := c.QueryInt("since_offset", 0)
	waitSeconds := c.QueryInt("wait_seconds", 0)
	limit := c.QueryInt("limit", 5)

	if _, _, err := p.apiAppAccess(c, appID, db.CollabRoleViewer); err != nil {
		return nil
	}

	if jobID == "" {
		if latestJob, ok := p.LatestDeployJobForApp(appID); ok && (latestJob.Running || sinceOffset > 0) {
			jobID = latestJob.JobID
		}
	}

	if jobID != "" {
		job, found := p.GetDeployJob(jobID)
		if !found || job.AppID != appID {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("job %q not found for app %s", jobID, appID)})
		}
		if waitSeconds > 30 {
			waitSeconds = 30
		}
		deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
		for {
			curJob, ok := p.GetDeployJob(jobID)
			if !ok {
				break
			}
			out := curJob.Output
			curLen := len(out)
			if sinceOffset > curLen {
				sinceOffset = curLen
			}
			if curLen > sinceOffset || !curJob.Running || waitSeconds <= 0 || time.Now().After(deadline) {
				chunk := ""
				if curLen > sinceOffset {
					chunk = out[sinceOffset:]
				}
				return c.JSON(fiber.Map{
					"job_id":       curJob.JobID,
					"app_id":       curJob.AppID,
					"action":       curJob.Action,
					"running":      curJob.Running,
					"ok":           curJob.OK,
					"since_offset": sinceOffset,
					"next_offset":  curLen,
					"new_output":   chunk,
				})
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	liveOut, liveAction, liveRunning := p.DeploySnapshot(appID)
	history, _ := p.DB.ListDeployLogs(ctx, appID, limit)
	return c.JSON(fiber.Map{
		"app_id":       appID,
		"live_running": liveRunning,
		"live_action":  liveAction,
		"live_output":  liveOut,
		"history":      history,
	})
}

// APIAppExec executes a command inside an app container or PHP runtime.
// POST /api/v1/apps/:id/exec
func (p *Panel) APIAppExec(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	u, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	if tok, ok := c.Locals("api_token").(db.APIToken); ok && tok.ID > 0 {
		if !tok.AllowContainerExec {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "permission denied: container_exec is restricted. Enable 'Allow Container Exec' for this API token in NextDeploy Panel under CLI Sessions (/cli-sessions).",
			})
		}
	}

	var req struct {
		Command        string   `json:"command"`
		Args           []string `json:"args"`
		Service        string   `json:"service"`
		WorkDir        string   `json:"work_dir"`
		Stdin          string   `json:"stdin"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	command := strings.TrimSpace(req.Command)
	if command == "" && len(req.Args) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "command or args is required"})
	}
	if command == "" {
		command = strings.Join(req.Args, " ")
	}
	service := strings.TrimSpace(req.Service)
	workDir := strings.TrimSpace(req.WorkDir)
	var stdinReader io.Reader
	if req.Stdin != "" {
		stdinReader = strings.NewReader(req.Stdin)
	}
	timeoutSec := req.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 60
	} else if timeoutSec > 3600 {
		timeoutSec = 3600
	}

	var targetContainer, execUser string
	matchedService := service
	if p.PHP != nil {
		if site, serr := p.DB.GetPHPSite(ctx, appID); serr == nil {
			if site.SiteType == "static" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "static sites have no PHP runtime to execute commands in"})
			}
			targetContainer = p.PHP.RuntimeName(site)
			matchedService = "php"
			execUser = fmt.Sprintf("%d:%d", site.UID, site.UID)
			if workDir == "" {
				workDir = p.PHP.DocRootFor(site)
			}
		}
	}

	if targetContainer == "" {
		project, composeRows, composeRes := p.ComposeProjectAndPS(ctx, app, appID)
		if service != "" {
			if composeRes.OK && p.ComposeServiceInRows(composeRows, service) {
				if cid, rerr := dockerapi.ContainerIDForComposeService(ctx, project, service); rerr == nil && cid != "" {
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
			if targetContainer == "" && p.ContainerBelongsToApp(ctx, appID, service) {
				targetContainer = service
			}
			if targetContainer == "" {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("service or container %q not found for app %q", service, appID)})
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
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("no running container found for app %q. Deploy the application stack first", appID)})
			}
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var res dockerx.Result
	if len(req.Args) > 0 {
		if execUser != "" {
			res = dockerx.DockerExecUserArgs(execCtx, targetContainer, execUser, req.Args, workDir, stdinReader)
		} else {
			res = dockerx.DockerExecArgs(execCtx, targetContainer, req.Args, workDir, stdinReader)
		}
	} else {
		if execUser != "" {
			res = dockerx.DockerExecUserWorkDirWithStdin(execCtx, targetContainer, execUser, command, workDir, stdinReader)
		} else {
			res = dockerx.DockerExecWorkDirWithStdin(execCtx, targetContainer, command, workDir, stdinReader)
		}
	}

	_ = u
	p.RecordAuditLog(c, "cli_container_exec", "app", appID, fmt.Sprintf("container_exec on %s (service=%s ok=%v): %s", app.Name, matchedService, res.OK, command))
	return c.JSON(fiber.Map{
		"app_id":    appID,
		"container": targetContainer,
		"service":   matchedService,
		"command":   command,
		"ok":        res.OK,
		"exit_code": res.ExitCode,
		"output":    res.Output,
	})
}

// APISystemExec executes a host shell command on the panel host (admin + allow_server_exec required).
// POST /api/v1/system/exec
func (p *Panel) APISystemExec(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden: server_exec requires admin role"})
	}
	if tok, ok := c.Locals("api_token").(db.APIToken); ok && tok.ID > 0 {
		if !tok.AllowServerExec {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "permission denied: server_exec is restricted. Enable 'Allow Server Exec' for this API token in NextDeploy Panel under CLI Sessions (/cli-sessions).",
			})
		}
	}

	var req struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "command is required"})
	}
	timeoutSec := req.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 60
	} else if timeoutSec > 300 {
		timeoutSec = 300
	}

	execCtx, cancel := context.WithTimeout(c.UserContext(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var res runutil.Result
	if runtime.GOOS == "windows" {
		res = runutil.Run(execCtx, ".", nil, "cmd", "/c", command)
	} else {
		res = runutil.Run(execCtx, ".", nil, "sh", "-c", command)
	}
	p.RecordAuditLog(c, "cli_server_exec", "system", "server", fmt.Sprintf("server_exec (ok=%v): %s", res.OK, command))
	return c.JSON(fiber.Map{
		"command":   command,
		"ok":        res.OK,
		"exit_code": res.ExitCode,
		"output":    res.Output,
	})
}

// APIAppEnvList lists env keys (safe) or reveals decrypted values when reveal=true (requires developer + allow_env_reveal).
// GET /api/v1/apps/:id/env?reveal=true&keys=K1,K2
func (p *Panel) APIAppEnvList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	reveal := c.QueryBool("reveal", false)

	requiredRole := db.CollabRoleViewer
	if reveal {
		requiredRole = db.CollabRoleDeveloper
	}
	_, app, err := p.apiAppAccess(c, appID, requiredRole)
	if err != nil {
		return nil
	}

	panelEnv, _ := p.DB.GetPanelEnv(ctx, appID)
	parsed := parseDotEnvMap(panelEnv)

	if !reveal {
		keys := make([]string, 0, len(parsed))
		for k := range parsed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return c.JSON(fiber.Map{"app_id": appID, "keys": keys, "count": len(keys)})
	}

	if tok, ok := c.Locals("api_token").(db.APIToken); ok && tok.ID > 0 {
		if !tok.AllowEnvReveal {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "permission denied: env reveal is restricted. Enable 'Allow Env Reveal' for this API token in NextDeploy Panel under CLI Sessions (/cli-sessions).",
			})
		}
	}

	outEnv := parsed
	if rawKeys := strings.TrimSpace(c.Query("keys")); rawKeys != "" {
		parts := strings.Split(rawKeys, ",")
		outEnv = make(map[string]string, len(parts))
		for _, k := range parts {
			k = strings.TrimSpace(k)
			if val, exists := parsed[k]; exists && k != "" {
				outEnv[k] = val
			}
		}
	}

	p.RecordAuditLog(c, "cli_env_reveal", "app", appID, fmt.Sprintf("Revealed %d environment secret(s) for app %s", len(outEnv), app.Name))
	return c.JSON(fiber.Map{"app_id": appID, "env": outEnv, "count": len(outEnv)})
}

// APIAppEnvSet sets one or multiple environment variables for an app.
// POST /api/v1/apps/:id/env
func (p *Panel) APIAppEnvSet(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	if _, _, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper); err != nil {
		return nil
	}

	var req struct {
		Key       string                 `json:"key"`
		Value     string                 `json:"value"`
		Variables map[string]interface{} `json:"variables"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	updates := make(map[string]string)
	if k := strings.TrimSpace(req.Key); k != "" {
		updates[k] = req.Value
	}
	for k, v := range req.Variables {
		k = strings.TrimSpace(k)
		if k != "" {
			updates[k] = fmt.Sprint(v)
		}
	}
	if len(updates) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "either 'key' and 'value' or 'variables' must be provided"})
	}

	cur, _ := p.DB.GetPanelEnv(ctx, appID)
	for k, v := range updates {
		cur = setDotEnvKeyVal(cur, k, v)
	}
	if err := p.DB.UpdatePanelEnv(ctx, appID, cur); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed updating panel env: %v", err)})
	}

	root := p.ComposeWorkspaceRoot(ctx, appID)
	_ = p.SyncWorkspaceEnvFromPanel(appID, root, cur)
	_ = p.SyncAppCaddyOverrideCtx(ctx, appID)

	updatedKeys := make([]string, 0, len(updates))
	for k := range updates {
		updatedKeys = append(updatedKeys, k)
	}
	sort.Strings(updatedKeys)
	return c.JSON(fiber.Map{
		"ok":           true,
		"app_id":       appID,
		"updated_keys": updatedKeys,
		"count":        len(updatedKeys),
		"message":      fmt.Sprintf("Successfully set %d environment variable(s)", len(updatedKeys)),
	})
}

func parseDotEnvMap(s string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			k := strings.TrimSpace(line[:idx])
			v := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
			out[k] = v
		}
	}
	return out
}

func setDotEnvKeyVal(envText, key, val string) string {
	lines := strings.Split(envText, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			lines[i] = fmt.Sprintf("%s=%s", key, val)
			return strings.Join(lines, "\n")
		}
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines[len(lines)-1] = fmt.Sprintf("%s=%s", key, val)
	} else {
		lines = append(lines, fmt.Sprintf("%s=%s", key, val))
	}
	return strings.Join(lines, "\n")
}

// APIAppDomainsList returns custom domains configured for an app.
// GET /api/v1/apps/:id/domains
func (p *Panel) APIAppDomainsList(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleViewer)
	if err != nil {
		return nil
	}
	domains, err := p.DB.ListAppDomains(ctx, app.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to list domains: %v", err)})
	}
	type domainItem struct {
		ID           int64  `json:"id"`
		AppID        string `json:"app_id"`
		Domain       string `json:"domain"`
		Service      string `json:"service"`
		Port         int    `json:"port"`
		EnableHTTPS  bool   `json:"enable_https"`
		EnableWWW    bool   `json:"enable_www"`
		ShieldNodeID *int64 `json:"shield_node_id,omitempty"`
		CreatedAt    string `json:"created_at"`
	}
	out := make([]domainItem, 0, len(domains))
	for _, d := range domains {
		out = append(out, domainItem{
			ID:           d.ID,
			AppID:        d.AppID,
			Domain:       d.Domain,
			Service:      d.Service,
			Port:         d.Port,
			EnableHTTPS:  d.EnableHTTPS,
			EnableWWW:    d.EnableWWW,
			ShieldNodeID: d.ShieldNodeID,
			CreatedAt:    d.CreatedAt.Format(time.RFC3339),
		})
	}
	return c.JSON(out)
}

func sanitizeAPIDomain(raw string) (string, error) {
	d := strings.TrimSpace(raw)
	if strings.Contains(d, "://") {
		if u, err := url.Parse(d); err == nil && u.Host != "" {
			d = u.Host
		} else {
			parts := strings.Split(d, "://")
			d = parts[len(parts)-1]
		}
	}
	if idx := strings.IndexAny(d, "/\\?#:"); idx != -1 {
		d = d[:idx]
	}
	d = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(proxy.CleanQuotedValue(d))), ".")
	if d == "" {
		return "", errors.New("domain name cannot be empty")
	}
	if len(d) > 253 {
		return "", errors.New("domain name too long (maximum 253 characters)")
	}
	for _, r := range d {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.') {
			return "", fmt.Errorf("invalid character %q in domain", r)
		}
	}
	for _, l := range strings.Split(d, ".") {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return "", errors.New("invalid domain label")
		}
	}
	return d, nil
}

// APIAppDomainAdd maps a custom domain to an application service and applies proxy routes.
// POST /api/v1/apps/:id/domains
func (p *Panel) APIAppDomainAdd(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	u, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	var req struct {
		Domain       string `json:"domain"`
		Service      string `json:"service"`
		Port         int    `json:"port"`
		EnableHTTPS  *bool  `json:"enable_https"`
		EnableWWW    bool   `json:"enable_www"`
		Direct       bool   `json:"direct"`
		ShieldNodeID *int64 `json:"shield_node_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	domain, err := sanitizeAPIDomain(req.Domain)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("invalid domain %q: %v", req.Domain, err)})
	}

	service := proxy.CleanQuotedValue(strings.TrimSpace(req.Service))
	if service == "" {
		if svcs := p.LoadComposeServices(ctx, app.ID); len(svcs) > 0 {
			service = svcs[0]
		} else {
			service = "web"
		}
	}
	port := req.Port
	if port <= 0 || port > 65535 {
		port = 80
	}
	enableHTTPS := true
	if req.EnableHTTPS != nil {
		enableHTTPS = *req.EnableHTTPS
	}

	if req.Direct && req.ShieldNodeID != nil && *req.ShieldNodeID > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "direct and shield_node_id are mutually exclusive"})
	}
	if req.ShieldNodeID != nil && *req.ShieldNodeID < 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "shield_node_id must be a positive integer"})
	}

	var shieldIDPtr *int64
	if u.Role == db.RoleAdmin {
		if !req.Direct && req.ShieldNodeID != nil && *req.ShieldNodeID > 0 {
			idCopy := *req.ShieldNodeID
			shieldIDPtr = &idCopy
		}
	} else {
		assigned := p.ResolveAppShield(ctx, app)
		if req.ShieldNodeID != nil && *req.ShieldNodeID > 0 {
			if assigned == nil || assigned.ID != *req.ShieldNodeID {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privileges required to select a custom shield node"})
			}
			idCopy := *req.ShieldNodeID
			shieldIDPtr = &idCopy
		} else if req.Direct {
			if assigned != nil {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin privileges required to bypass assigned shield node"})
			}
		} else if assigned != nil {
			shieldIDPtr = &assigned.ID
		}
	}

	if shieldIDPtr != nil {
		node, err := p.DB.GetShieldNode(ctx, *shieldIDPtr)
		if err != nil || node == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("shield node %d not found", *shieldIDPtr)})
		}
		if node.Status != "connected" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("shield node '%s' is %s; only connected nodes can route traffic", node.Name, node.Status)})
		}
		enableHTTPS = true
	}

	existing, _ := p.DB.ListAppDomains(ctx, app.ID)
	for _, ex := range existing {
		if strings.EqualFold(ex.Domain, domain) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": fmt.Sprintf("domain %q is already configured for application %s (ID %d)", domain, app.ID, ex.ID)})
		}
	}

	domainID, err := p.DB.CreateAppDomain(ctx, db.AppDomain{
		AppID:              app.ID,
		Domain:             domain,
		Service:            service,
		Port:               port,
		EnableHTTPS:        enableHTTPS,
		EnableWWW:          req.EnableWWW,
		RouteRulesJSON:     "[]",
		ShieldNodeID:       shieldIDPtr,
		PendingOriginApply: shieldIDPtr != nil,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to save domain: %v", err)})
	}

	_ = p.SyncAndApplyBackground(ctx, app.ID)
	p.RecordAuditLog(c, "domain_add", "app", app.ID, fmt.Sprintf("Added domain %s (%s:%d) via CLI/API", domain, service, port))
	return c.JSON(fiber.Map{
		"ok":             true,
		"message":        fmt.Sprintf("Domain %s mapped to service %s:%d (HTTPS: %t, WWW: %t)", domain, service, port, enableHTTPS, req.EnableWWW),
		"domain_id":      domainID,
		"app_id":         app.ID,
		"domain":         domain,
		"service":        service,
		"port":           port,
		"enable_https":   enableHTTPS,
		"enable_www":     req.EnableWWW,
		"shield_node_id": shieldIDPtr,
		"direct":         shieldIDPtr == nil,
	})
}

// APIAppDomainDelete removes a domain from an app by domain ID or hostname.
// DELETE /api/v1/apps/:id/domains/:did or DELETE /api/v1/apps/:id/domains?domain=...
func (p *Panel) APIAppDomainDelete(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	target := strings.TrimSpace(c.Params("did"))
	if target == "" {
		target = strings.TrimSpace(c.Query("domain"))
	}
	if target == "" {
		var req struct {
			Domain string `json:"domain"`
		}
		if _ = c.BodyParser(&req); req.Domain != "" {
			target = strings.TrimSpace(req.Domain)
		}
	}
	if target == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "domain name or domain_id is required"})
	}

	domains, err := p.DB.ListAppDomains(ctx, app.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to list domains: %v", err)})
	}

	var found *db.AppDomain
	targetID, idErr := strconv.ParseInt(target, 10, 64)
	cleanTarget, _ := sanitizeAPIDomain(target)
	for i := range domains {
		d := &domains[i]
		if (idErr == nil && d.ID == targetID) || strings.EqualFold(d.Domain, target) || (cleanTarget != "" && strings.EqualFold(d.Domain, cleanTarget)) {
			found = d
			break
		}
	}
	if found == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("domain %q not found on application %s", target, app.ID)})
	}

	if err := p.DB.DeleteAppDomain(ctx, found.ID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to delete domain: %v", err)})
	}

	_ = p.SyncAndApplyBackground(ctx, app.ID)
	p.RecordAuditLog(c, "domain_delete", "app", app.ID, fmt.Sprintf("Deleted domain %s (ID %d) via CLI/API", found.Domain, found.ID))
	return c.JSON(fiber.Map{
		"ok":        true,
		"message":   fmt.Sprintf("Domain %s (ID %d) successfully deleted", found.Domain, found.ID),
		"domain_id": found.ID,
		"domain":    found.Domain,
		"app_id":    app.ID,
	})
}

// APIAppWorkspaceApply applies batched file writes and deletes with path/compose validation.
// POST /api/v1/apps/:id/workspace/apply
func (p *Panel) APIAppWorkspaceApply(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	type writeItem struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	var req struct {
		Writes  []writeItem `json:"writes"`
		Deletes []string    `json:"deletes"`
		DryRun  bool        `json:"dry_run"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	if len(req.Writes) == 0 && len(req.Deletes) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace apply requires at least one write or delete operation"})
	}

	isPHP := p.DB.IsPHPSite(ctx, appID)
	resolvePHPRel := func(raw string) string {
		if !isPHP {
			return raw
		}
		clean := filepath.ToSlash(strings.Trim(raw, "/"))
		if !strings.HasPrefix(clean, "htdocs/") && clean != "htdocs" {
			return "htdocs/" + clean
		}
		return clean
	}

	for _, d := range req.Deletes {
		d = strings.TrimSpace(d)
		base := filepath.Base(d)
		if d == "" || base == ".nextdeploy.generated.compose.yml" || base == ".panel-meta" || base == ".nextdeploy" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("cannot delete protected or empty path %q", d)})
		}
		if _, err := p.Store.SafeFilePath(appID, resolvePHPRel(d)); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("invalid delete path %q: %v", d, err)})
		}
	}

	for _, w := range req.Writes {
		wPath := strings.TrimSpace(w.Path)
		base := filepath.Base(wPath)
		if wPath == "" || base == ".nextdeploy.generated.compose.yml" || base == ".panel-meta" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("cannot write protected or empty path %q", w.Path)})
		}
		if _, err := p.Store.SafeFilePath(appID, resolvePHPRel(wPath)); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("invalid write path %q: %v", w.Path, err)})
		}
		cleanRel := filepath.ToSlash(strings.Trim(wPath, "/"))
		if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == "compose.yml" || cleanRel == "compose.yaml" || cleanRel == app.ComposeFile {
			if err := sandbox.CheckComposeSecurity([]byte(w.Content), p.Store.Path(appID)); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("compose security validation failed for %s: %v", w.Path, err)})
			}
		}
	}

	if req.DryRun {
		return c.JSON(fiber.Map{
			"ok":      true,
			"app_id":  appID,
			"dry_run": true,
			"writes":  len(req.Writes),
			"deletes": req.Deletes,
		})
	}

	var deletedPaths []string
	for _, d := range req.Deletes {
		d = strings.TrimSpace(d)
		if err := p.Store.RemoveRel(appID, resolvePHPRel(d)); err != nil && !os.IsNotExist(err) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed deleting %s: %v", d, err)})
		}
		deletedPaths = append(deletedPaths, d)
	}

	var writtenPaths []string
	for _, w := range req.Writes {
		wPath := strings.TrimSpace(w.Path)
		var full string
		var pErr error
		if p.IsGitApp(ctx, appID) {
			full, pErr = p.Store.SafeGitRepoFilePath(appID, wPath)
		} else {
			full, pErr = p.Store.SafeFilePath(appID, resolvePHPRel(wPath))
		}
		if pErr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("invalid write path %q: %v", w.Path, pErr)})
		}
		if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		if err := os.WriteFile(full, []byte(w.Content), 0640); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		cleanRel := filepath.ToSlash(strings.Trim(wPath, "/"))
		if cleanRel == "docker-compose.yml" || cleanRel == "docker-compose.yaml" || cleanRel == app.ComposeFile {
			_ = p.SyncAppCaddyOverrideCtx(ctx, appID)
		}
		writtenPaths = append(writtenPaths, wPath)
	}

	if isPHP && p.PHP != nil {
		p.PHP.DebounceFixOwnership(appID)
	}
	wsmanifest.InvalidateManifestCache(appID, "")
	p.InvalidateAfterAppWorkspaceChange(appID)

	return c.JSON(fiber.Map{
		"ok":            true,
		"app_id":        appID,
		"written":       writtenPaths,
		"deleted":       deletedPaths,
		"written_count": len(writtenPaths),
		"deleted_count": len(deletedPaths),
		"message":       fmt.Sprintf("Successfully applied: %d written, %d deleted", len(writtenPaths), len(deletedPaths)),
	})
}

// APIAppGitSyncMode toggles Git sync on or off for an app via CLI/API.
// POST /api/v1/apps/:id/git/sync-mode
func (p *Panel) APIAppGitSyncMode(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := strings.TrimSpace(c.Params("id"))
	_, app, err := p.apiAppAccess(c, appID, db.CollabRoleDeveloper)
	if err != nil {
		return nil
	}

	var req struct {
		Action  string `json:"action"`
		Enabled *bool  `json:"enabled"`
		Force   bool   `json:"force"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	var enabled bool
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "on":
		enabled = true
	case "off":
		enabled = false
	case "":
		if req.Enabled == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "action ('on' or 'off') or enabled boolean is required"})
		}
		enabled = *req.Enabled
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("unknown action %q: use on or off", req.Action)})
	}

	res, err := p.SetGitSync(ctx, appID, enabled, req.Force)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if res.Changed {
		state := map[bool]string{true: "on", false: "off"}[enabled]
		p.RecordAuditLog(c, "git_sync_"+state, "app", appID, fmt.Sprintf("Git sync turned %s for app %s. %s", state, app.Name, strings.Join(res.Notes, " ")))
	}
	return c.JSON(res)
}
