package handlers

import (
	"panel/internal/handlers/utils"
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/gitx"
	"panel/internal/perflog"
	"panel/internal/volumex"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

// appShowTabNeedsCompose is true when the tab template reads ComposeRows / container state from the initial handler.
func appShowTabNeedsCompose(tab string) bool {
	switch tab {
	case "overview", "logs", "containers":
		return true
	default:
		return false
	}
}

func (p *Panel) AppShow(c *fiber.Ctx) error {
	id := c.Params("id")
	htmxTabPartial := strings.EqualFold(c.Get("HX-Request"), "true") && c.Query("partial") == "tab"
	appShowFlash := utils.ReadFlash(c)

	tr := perflog.Start("AppShow")
	defer tr.Finish()
	tr.Field("app", id)
	if htmxTabPartial {
		tr.Field("mode", "htmx-partial")
	} else {
		tr.Field("mode", "full-page")
	}

	reqCtx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()

	mark := time.Now()
	app, err := p.RequireAppAccess(c, id, db.CollabRoleViewer)
	tr.StepDur("access", mark)
	if err != nil {
		return err
	}
	tab := c.Query("tab", "overview")
	tr.Field("tab", tab)
	mark = time.Now()
	isGitApp, gitCfg, hasGitCfg := p.AppGitMetadata(reqCtx, id)
	tr.StepDur("git_meta", mark)
	switch tab {
	case "overview", "files", "logs", "containers", "environment", "deployment", "volumes", "terminal", "domains", "git", "backup", "collaborators":
	default:
		tab = "overview"
	}
	if isGitApp && tab == "files" {
		tab = "git"
	}
	rel := c.Query("path", "")
	var children []workspace.FileEntry
	if !isGitApp && (tab == "files" || !htmxTabPartial) {
		mark = time.Now()
		children, err = p.Store.ListChildren(id, rel)
		tr.StepDur("files_list", mark)
		if err != nil {
			children = nil
		}
	}
	parent := p.Store.ParentRel(rel)
	sourcePath := p.composeWorkspaceRoot(reqCtx, id)
	hasDF := false
	if tab == "overview" || tab == "files" || !htmxTabPartial {
		mark = time.Now()
		hasDF, _ = p.Store.HasDockerArtifacts(id)
		tr.StepDur("docker_artifacts", mark)
	}

	mark = time.Now()
	composePath := p.composeFilePath(reqCtx, app, id)
	composeDisplay := workspace.NormalizeComposeRel(app.ComposeFile)
	var composeName string
	hasComp := false
	if st, err := os.Stat(composePath); err == nil && !st.IsDir() {
		hasComp = true
		composeName = composeDisplay
	}
	hasGenerated := false
	if st, err := os.Stat(p.composeOverridePath(reqCtx, id)); err == nil && !st.IsDir() {
		hasGenerated = true
	}
	stackReady := hasComp || hasGenerated
	tr.StepDur("compose_stat", mark)

	storagePath := filepath.ToSlash(sourcePath)

	envContent := ""
	if tab == "environment" {
		mark = time.Now()
		envContent = p.panelEnvForUI(reqCtx, id)
		tr.StepDur("env_load", mark)
	}

	var composeRows []dockerx.ComposePsRow
	var composePsMsg string
	if stackReady && appShowTabNeedsCompose(tab) {
		mark = time.Now()
		_, rows, pr := p.composeProjectAndPS(reqCtx, app, id)
		tr.StepDur("compose_ps", mark)
		if pr.OK {
			composeRows = rows
			tr.Field("compose_rows", fmt.Sprintf("%d", len(rows)))
		} else {
			composePsMsg = pr.Output
		}
	}

	var deployLogs []db.DeployLog
	var appVols []string
	var appVolErr string
	var volProjects []string
	deployBusy := false
	var liveOut, liveAct string
	var liveRun bool
	var appDomains []db.AppDomain
	var domainServices []string
	var gitProviders []db.GitProvider
	gitHubProviderMap := map[int64]db.GitHubProviderDetail{}

	if tab == "deployment" {
		mark = time.Now()
		deployLogs, _ = p.DB.ListDeployLogs(reqCtx, id, 5)
		deployBusy = c.Query("busy") == "1"
		liveOut, liveAct, liveRun = p.DeploySnapshot(id)
		tr.StepDur("deployment_data", mark)
	}
	if tab == "volumes" || tab == "backup" {
		mark = time.Now()
		volProjects = p.composeProjectCandidates(reqCtx, app, id)
		if stackReady {
			if active, _, pr := p.composeProjectAndPS(reqCtx, app, id); pr.OK && strings.TrimSpace(active) != "" {
				volProjects = dedupeStringsPreserveOrder(append([]string{active}, volProjects...))
				tr.Field("vol_project", active)
			}
		}
		appVols, appVolErr = volumex.ListForApp(reqCtx, id, volProjects)
		tr.StepDur("volumes", mark)
		tr.Field("vol_count", fmt.Sprintf("%d", len(appVols)))
	}
	if tab == "domains" {
		mark = time.Now()
		appDomains, _ = p.DB.ListAppDomains(reqCtx, id)
		for i := range appDomains {
			sanitizeDomainRecord(&appDomains[i])
		}
		domainServices = p.loadComposeServices(reqCtx, id)
		tr.StepDur("domains_data", mark)
	}

	panelDomain := ""
	if tab == "git" || tab == "deployment" {
		panelDomain = p.DB.GetSetting(reqCtx, settingPanelDomain)
	}
	var gitSaved, gitSynced bool
	var gitErrFlash string
	if tab == "git" {
		mark = time.Now()
		var userID *int64
		if u, ok := currentUser(c); ok && u.Role != db.RoleAdmin {
			val := u.ID
			userID = &val
		}
		gitProviders, _ = p.DB.ListGitProviders(reqCtx, userID)
		gitHubDetails, _ := p.DB.ListGitHubProviderDetails(reqCtx)
		for _, detail := range gitHubDetails {
			gitHubProviderMap[detail.ProviderID] = detail
		}
		gitSaved, gitSynced, gitErrFlash = p.ConsumeGitTabFlash(c, id)
		tr.StepDur("git_data", mark)
	}

	appWebhookURL := ""
	if hasGitCfg && tab == "deployment" {
		appWebhookURL = p.appWebhookURL(c, id)
	}
	gitRepoReady := false
	if tab == "git" && isGitApp && hasGitCfg {
		gitRepoReady = gitx.RepoExists(filepath.Join(p.Store.ReservedPath(id), "repo"))
	}
	gitSource := "files"
	if isGitApp {
		gitSource = "git"
	}

	var backupDestinations []db.BackupDestination
	var backupSchedules []db.BackupSchedule
	var backupHistory []db.BackupHistory
	var backupAutoVolumeName string
	var backupAutoVolumeErr string
	if tab == "backup" {
		mark = time.Now()
		var userID *int64
		if u, ok := currentUser(c); ok && u.Role != db.RoleAdmin {
			val := u.ID
			userID = &val
		}
		backupDestinations, _ = p.DB.ListBackupDestinations(reqCtx, userID)
		backupSchedules, _ = p.DB.ListBackupSchedules(reqCtx, id)
		backupHistory, _ = p.DB.ListBackupHistory(reqCtx, id, 50)
		backupAutoVolumeName, backupAutoVolumeErr = volumex.PickBackupDataVolumeName(reqCtx, app.Name, volProjects, appVols)
		tr.StepDur("backup_db", mark)
	}

	gitDeployShort, gitDeploySubject, gitDeployURL := "", "", ""
	if hasGitCfg && (tab == "overview" || tab == "deployment" || tab == "git") {
		gitDeployShort, gitDeploySubject, gitDeployURL = p.gitDeployedSummary(reqCtx, id, gitCfg)
	}

	type CollabDetail struct {
		UserID    int64
		Username  string
		Role      string
		CreatedAt time.Time
	}
	var collabs []CollabDetail
	var allUsers []db.User
	var ownerUser db.User
	if tab == "collaborators" {
		mark = time.Now()
		dbCollabs, _ := p.DB.ListCollaborators(reqCtx, id)
		for _, cb := range dbCollabs {
			u, err := p.DB.GetUserByID(reqCtx, cb.UserID)
			if err == nil {
				collabs = append(collabs, CollabDetail{
					UserID:    cb.UserID,
					Username:  u.Username,
					Role:      cb.Role,
					CreatedAt: cb.CreatedAt,
				})
			}
		}
		allUsers, _ = p.DB.ListUsers(reqCtx)
		ownerUser, _ = p.DB.GetUserByID(reqCtx, app.OwnerID)
		tr.StepDur("collaborators_db", mark)
	}

	m := withUser(c, fiber.Map{
		"Nav":                    "apps",
		"Title":                  app.Name,
		"App":                    app,
		"Tab":                    tab,
		"Path":                   rel,
		"ParentPath":             parent,
		"Children":               children,
		"HasDockerfile":          hasDF,
		"IsGitApp":               isGitApp,
		"GitSource":              gitSource,
		"HasGitConfig":           hasGitCfg,
		"GitRepoReady":           gitRepoReady,
		"GitConfig":              gitCfg,
		"GitProviders":           gitProviders,
		"GitHubProviderMap":      gitHubProviderMap,
		"PanelDomain":            panelDomain,
		"AppWebhookURL":          appWebhookURL,
		"HasCompose":             hasComp,
		"HasStack":               stackReady,
		"ComposeFile":            composeName,
		"ComposeFileSetting":     composeDisplay,
		"ID":                     id,
		"StoragePath":            storagePath,
		"UploadZipTarget":        fmt.Sprintf("/apps/%s/upload-zip", id),
		"UploadFileTarget":       fmt.Sprintf("/apps/%s/upload", id),
		"ComposeRows":            composeRows,
		"RunningCount":           countComposeOkRunning(composeRows),
		"ExitedCount":            countComposeState(composeRows, "exited"),
		"DeadCount":              countComposeState(composeRows, "dead"),
		"ComposePsMsg":           composePsMsg,
		"BrowseFlash":            "",
		"DeleteTarget":           fmt.Sprintf("/apps/%s/files/delete", id),
		"EnvContent":             envContent,
		"DeployLogs":             deployLogs,
		"AppVolumes":             appVols,
		"AppVolumeError":         appVolErr,
		"BackupAutoVolumeName":   backupAutoVolumeName,
		"BackupAutoVolumeError":  backupAutoVolumeErr,
		"DeployLiveOutput":       liveOut,
		"DeployLiveAction":       liveAct,
		"DeployJobRunning":       liveRun,
		"DeployQueueBusy":        deployBusy,
		"AppDomains":             appDomains,
		"DomainServices":         domainServices,
		"ShowDomainFileServer":   p.userCanDomainFileServer(c),
		"DomainSaved":            appShowFlash == "domainSaved" || c.Query("domainSaved") == "1",
		"GitSaved":               gitSaved,
		"GitSynced":              gitSynced,
		"GitError":               gitErrFlash,
		"GitDeployShort":         gitDeployShort,
		"GitDeploySubject":       gitDeploySubject,
		"GitDeployURL":           gitDeployURL,
		"SourceSwitched":         appShowFlash == "sourceSwitched" || c.Query("sourceSwitched") == "1",
		"SourceSwitchClearError": appShowFlash == "switchError_clear" || c.Query("switchError") == "clear",
		"BackupDestinations":     backupDestinations,
		"BackupSchedules":        backupSchedules,
		"BackupHistory":          backupHistory,
		"Collaborators":          collabs,
		"AllUsers":               allUsers,
		"OwnerUser":              ownerUser,
		"Flash":                  appShowFlash,
	})

	eng, _ := c.App().Config().Views.(viewsRenderer)
	if eng == nil {
		return c.Status(500).SendString("template engine not configured")
	}
	var headerBuf, tabBuf, switchBuf bytes.Buffer
	mark = time.Now()
	if err := eng.Render(&headerBuf, utils.TmplPartialAppShowHeaderTabs, m); err != nil {
		return c.Status(500).SendString("failed to render header: " + err.Error())
	}
	tr.StepDur("render_header", mark)
	mark = time.Now()
	if err := eng.Render(&tabBuf, appShowTabPartialName(tab), m); err != nil {
		return c.Status(500).SendString("failed to render tab: " + err.Error())
	}
	tr.StepDur("render_tab", mark)
	mark = time.Now()
	if err := eng.Render(&switchBuf, utils.TmplPartialAppShowSwitchSource, m); err != nil {
		return c.Status(500).SendString("failed to render switch modal: " + err.Error())
	}
	tr.StepDur("render_switch", mark)
	if htmxTabPartial {
		c.Type("html")
		return c.Send(tabBuf.Bytes())
	}
	mark = time.Now()
	var page strings.Builder
	page.WriteString(`<div class="mx-auto max-w-6xl min-w-0 w-full space-y-5 sm:space-y-6">`)
	page.Write(headerBuf.Bytes())
	page.WriteString(`<div id="app-tab-panel" class="min-w-0">`)
	page.Write(tabBuf.Bytes())
	page.WriteString(`</div>`)
	page.WriteString(`</div>`)
	page.Write(switchBuf.Bytes())
	m["AppPageHTML"] = template.HTML(page.String())
	tr.StepDur("render_shell", mark)

	return c.Render("pages/app_show", withUser(c, m), "layouts/shell")
}

func (p *Panel) AddCollaborator(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	collabUsername := strings.TrimSpace(c.FormValue("username"))
	role := strings.TrimSpace(c.FormValue("role"))
	if role != db.CollabRoleDeveloper && role != db.CollabRoleViewer {
		role = db.CollabRoleViewer
	}

	targetUser, err := p.DB.GetUserByUsername(ctx, collabUsername)
	if err != nil {
		utils.SetFlash(c, "Error: User not found.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if targetUser.ID == app.OwnerID {
		utils.SetFlash(c, "Error: User is already the owner of this app.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	err = p.DB.AddCollaborator(ctx, appID, targetUser.ID, role)
	if err != nil {
		utils.SetFlash(c, "Error adding collaborator: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	p.RecordAuditLog(c, "add_collaborator", "app", appID, fmt.Sprintf("Added collaborator %s with role %s", collabUsername, role))
	utils.SetFlash(c, "Collaborator added successfully.")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}

func (p *Panel) DeleteCollaborator(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	collabUserID, err := c.ParamsInt("uid")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid user ID")
	}

	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	err = p.DB.RemoveCollaborator(ctx, appID, int64(collabUserID))
	if err != nil {
		utils.SetFlash(c, "Error removing collaborator: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	p.RecordAuditLog(c, "delete_collaborator", "app", appID, fmt.Sprintf("Removed collaborator user ID %d", collabUserID))
	utils.SetFlash(c, "Collaborator removed successfully.")
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}

func (p *Panel) TransferAppOwnership(c *fiber.Ctx) error {
	ctx := c.UserContext()
	appID := c.Params("id")
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("App not found")
	}

	if u.Role != db.RoleAdmin && app.OwnerID != u.ID {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	newOwnerUsername := strings.TrimSpace(c.FormValue("username"))
	targetUser, err := p.DB.GetUserByUsername(ctx, newOwnerUsername)
	if err != nil {
		utils.SetFlash(c, "Error: User not found.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	if targetUser.ID == app.OwnerID {
		utils.SetFlash(c, "Error: User is already the owner.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	collabs, err := p.DB.ListCollaborators(ctx, appID)
	if err != nil {
		utils.SetFlash(c, "Error loading collaborators.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}
	isCollab := false
	for _, cb := range collabs {
		if cb.UserID == targetUser.ID {
			isCollab = true
			break
		}
	}
	if !isCollab {
		utils.SetFlash(c, "Error: Target user must be a collaborator of this app first.")
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	err = p.DB.TransferAppOwnership(ctx, appID, targetUser.ID)
	if err != nil {
		utils.SetFlash(c, "Error transferring ownership: "+err.Error())
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
	}

	_ = p.DB.RemoveCollaborator(ctx, appID, targetUser.ID)

	p.RecordAuditLog(c, "transfer_ownership", "app", appID, fmt.Sprintf("Transferred ownership of app %s to %s", appID, newOwnerUsername))
	utils.SetFlash(c, "Ownership transferred successfully.")

	if u.Role != db.RoleAdmin {
		return c.Redirect("/apps")
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=collaborators", appID))
}
