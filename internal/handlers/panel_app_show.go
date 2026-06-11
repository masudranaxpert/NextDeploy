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
	"panel/internal/volumex"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

// appShowTabNeedsCompose is true when the tab template reads ComposeRows / container state from the initial handler.
func appShowTabNeedsCompose(tab string) bool {
	switch tab {
	case "overview", "logs", "terminal", "containers":
		return true
	default:
		return false
	}
}

func (p *Panel) AppShow(c *fiber.Ctx) error {
	id := c.Params("id")
	htmxTabPartial := strings.EqualFold(c.Get("HX-Request"), "true") && c.Query("partial") == "tab"
	appShowFlash := utils.ReadFlash(c) // read once; cookie is cleared after this call

	reqCtx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()

	app, err := p.RequireAppAccess(c, id, db.CollabRoleViewer)
	if err != nil {
		return err
	}
	tab := c.Query("tab", "overview")
	isGitApp, gitCfg, hasGitCfg := p.AppGitMetadata(reqCtx, id)
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
		children, err = p.Store.ListChildren(id, rel)
		if err != nil {
			children = nil
		}
	}
	parent := p.Store.ParentRel(rel)
	sourcePath := p.composeWorkspaceRoot(reqCtx, id)
	hasDF := false
	if tab == "overview" || tab == "files" || !htmxTabPartial {
		hasDF, _ = p.Store.HasDockerArtifacts(sourcePath)
	}

	composePath := p.composeFilePath(reqCtx, app, id)
	composeDisplay := workspace.NormalizeComposeRel(app.ComposeFile)
	var composeName string
	hasComp := false
	if st, err := os.Stat(composePath); err == nil && !st.IsDir() {
		hasComp = true
		composeName = composeDisplay
	}

	storagePath := filepath.ToSlash(sourcePath)

	envContent := ""
	if tab == "environment" {
		envContent = p.panelEnvForUI(reqCtx, id)
	}

	var composeRows []dockerx.ComposePsRow
	var composePsMsg string
	if hasComp && appShowTabNeedsCompose(tab) {
		_, rows, pr := p.composeProjectAndPS(reqCtx, app, id)
		if pr.OK {
			composeRows = rows
		} else {
			composePsMsg = pr.Output
		}
	}

	var deployLogs []db.DeployLog
	var appVols []string
	var appVolErr string
	deployBusy := false
	var liveOut, liveAct string
	var liveRun bool
	var appDomains []db.AppDomain
	var domainServices []string
	var gitProviders []db.GitProvider
	gitHubProviderMap := map[int64]db.GitHubProviderDetail{}

	if tab == "deployment" {
		deployLogs, _ = p.DB.ListDeployLogs(reqCtx, id, 5)
		deployBusy = c.Query("busy") == "1"
		liveOut, liveAct, liveRun = p.DeploySnapshot(id)
	}
	if tab == "volumes" || tab == "backup" {
		volProjects := p.composeProjectCandidates(reqCtx, app, id)
		if hasComp {
			if active, _, pr := p.composeProjectAndPS(reqCtx, app, id); pr.OK && strings.TrimSpace(active) != "" {
				volProjects = dedupeStringsPreserveOrder(append([]string{active}, volProjects...))
			}
		}
		appVols, appVolErr = volumex.ListForApp(reqCtx, id, volProjects)
	}
	if tab == "domains" {
		appDomains, _ = p.DB.ListAppDomains(reqCtx, id)
		for i := range appDomains {
			sanitizeDomainRecord(&appDomains[i])
		}
		domainServices = p.loadComposeServices(reqCtx, id)
	}

	panelDomain := ""
	if tab == "git" || tab == "deployment" {
		panelDomain = p.DB.GetSetting(reqCtx, settingPanelDomain)
	}
	if tab == "git" {
		gitProviders, _ = p.DB.ListGitProviders(reqCtx)
		gitHubDetails, _ := p.DB.ListGitHubProviderDetails(reqCtx)
		for _, detail := range gitHubDetails {
			gitHubProviderMap[detail.ProviderID] = detail
		}
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
	var gitSaved, gitSynced bool
	var gitErrFlash string
	if tab == "git" {
		gitSaved, gitSynced, gitErrFlash = p.ConsumeGitTabFlash(c, id)
	}

	var backupDestinations []db.BackupDestination
	var backupSchedules []db.BackupSchedule
	var backupHistory []db.BackupHistory
	var backupAutoVolumeName string
	var backupAutoVolumeErr string
	if tab == "backup" {
		backupDestinations, _ = p.DB.ListBackupDestinations(reqCtx)
		backupSchedules, _ = p.DB.ListBackupSchedules(reqCtx, id)
		backupHistory, _ = p.DB.ListBackupHistory(reqCtx, id, 50)
		backupAutoVolumeName, backupAutoVolumeErr = p.resolveRequestedBackupVolume(reqCtx, app, "")
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
	if err := eng.Render(&headerBuf, utils.TmplPartialAppShowHeaderTabs, m); err != nil {
		return c.Status(500).SendString("failed to render header: " + err.Error())
	}
	if err := eng.Render(&tabBuf, appShowTabPartialName(tab), m); err != nil {
		return c.Status(500).SendString("failed to render tab: " + err.Error())
	}
	if err := eng.Render(&switchBuf, utils.TmplPartialAppShowSwitchSource, m); err != nil {
		return c.Status(500).SendString("failed to render switch modal: " + err.Error())
	}
	if htmxTabPartial {
		c.Type("html")
		return c.Send(tabBuf.Bytes())
	}
	var page strings.Builder
	page.WriteString(`<div class="mx-auto max-w-6xl min-w-0 w-full space-y-5 sm:space-y-6">`)
	page.Write(headerBuf.Bytes())
	page.WriteString(`<div id="app-tab-panel" class="min-w-0">`)
	page.Write(tabBuf.Bytes())
	page.WriteString(`</div>`)
	page.WriteString(`</div>`)
	page.Write(switchBuf.Bytes())
	m["AppPageHTML"] = template.HTML(page.String())

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
