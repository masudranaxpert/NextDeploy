package handlers

import (
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


func (p *Panel) AppShow(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	tab := c.Query("tab", "overview")
	isGitApp := p.isGitApp(c.UserContext(), id)
	switch tab {
	case "overview", "files", "logs", "containers", "environment", "deployment", "volumes", "terminal", "domains", "git":
	default:
		tab = "overview"
	}
	if isGitApp && tab == "files" {
		tab = "git"
	}
	rel := c.Query("path", "")
	var children []workspace.FileEntry
	if !isGitApp {
		children, err = p.Store.ListChildren(id, rel)
		if err != nil {
			children = nil
		}
	}
	parent := p.Store.ParentRel(rel)
	sourcePath := p.appSourcePath(c.UserContext(), id)
	hasDF, _ := p.Store.HasDockerArtifacts(sourcePath)

	composePath := p.composeFilePath(app, id)
	composeDisplay := workspace.NormalizeComposeRel(app.ComposeFile)
	var composeName string
	hasComp := false
	if st, err := os.Stat(composePath); err == nil && !st.IsDir() {
		hasComp = true
		composeName = composeDisplay
	}

	storagePath := filepath.ToSlash(sourcePath)

	envContent := p.panelEnvForUI(c.UserContext(), id, sourcePath)

	var composeRows []dockerx.ComposePsRow
	var composePsMsg string
	if hasComp {
		ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
		project := p.activeComposeProjectName(ctx, app, id)
		rows, pr := dockerx.ComposePS(ctx, sourcePath, p.effectiveComposePaths(c.UserContext(), app, id), project, p.composeEnvFiles(ctx, id))
		cancel()
		if pr.OK {
			composeRows = rows
		} else {
			composePsMsg = pr.Output
		}
	}

	deployLogs, _ := p.DB.ListDeployLogs(c.UserContext(), id, 5)
	appVols, appVolErr := volumex.ListForApp(c.UserContext(), id)
	deployBusy := c.Query("busy") == "1"
	liveOut, liveAct, liveRun := p.deploySnapshot(id)

	// Domains tab data
	appDomains, _ := p.DB.ListAppDomains(c.UserContext(), id)
	for i := range appDomains {
		sanitizeDomainRecord(&appDomains[i])
	}
	domainServices := p.loadComposeServices(c, id)
	gitCfg, hasGitCfg := p.appGitConfig(c.UserContext(), id)
	panelDomain := p.DB.GetSetting(c.UserContext(), settingPanelDomain)
	gitProviders, _ := p.DB.ListGitProviders(c.UserContext())
	gitHubProviderDetails, _ := p.DB.ListGitHubProviderDetails(c.UserContext())
	gitHubProviderMap := map[int64]db.GitHubProviderDetail{}
	for _, detail := range gitHubProviderDetails {
		gitHubProviderMap[detail.ProviderID] = detail
	}
	appWebhookURL := ""
	if hasGitCfg {
		appWebhookURL = p.appWebhookURL(c, id)
	}
	gitRepoReady := false
	if isGitApp && hasGitCfg {
		gitRepoReady = gitx.RepoExists(filepath.Join(p.Store.ReservedPath(id), "repo"))
	}
	gitSource := "files"
	if isGitApp {
		gitSource = "git"
	}
	gitSaved, gitSynced, gitErrFlash := p.consumeGitTabFlash(c, id)

	m := fiber.Map{
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
		"DeployLiveOutput":       liveOut,
		"DeployLiveAction":       liveAct,
		"DeployJobRunning":       liveRun,
		"DeployQueueBusy":        deployBusy,
		"AppDomains":             appDomains,
		"DomainServices":         domainServices,
		"DomainSaved":            c.Query("domainSaved") == "1",
		"GitSaved":               gitSaved,
		"GitSynced":              gitSynced,
		"GitError":               gitErrFlash,
		"SourceSwitched":         c.Query("sourceSwitched") == "1",
		"SourceSwitchClearError": c.Query("switchError") == "clear",
	}

	eng, _ := c.App().Config().Views.(viewsRenderer)
	if eng == nil {
		return c.Status(500).SendString("template engine not configured")
	}
	var headerBuf, tabBuf, switchBuf bytes.Buffer
	if err := eng.Render(&headerBuf, tmplPartialAppShowHeaderTabs, m); err != nil {
		return c.Status(500).SendString("failed to render header: " + err.Error())
	}
	if err := eng.Render(&tabBuf, appShowTabPartialName(tab), m); err != nil {
		return c.Status(500).SendString("failed to render tab: " + err.Error())
	}
	if err := eng.Render(&switchBuf, tmplPartialAppShowSwitchSource, m); err != nil {
		return c.Status(500).SendString("failed to render switch modal: " + err.Error())
	}
	var page strings.Builder
	page.WriteString(`<div class="mx-auto max-w-6xl min-w-0 w-full space-y-5 sm:space-y-6">`)
	page.Write(headerBuf.Bytes())
	page.Write(tabBuf.Bytes())
	page.WriteString(`</div>`)
	page.Write(switchBuf.Bytes())
	m["AppPageHTML"] = template.HTML(page.String())

	return c.Render("pages/app_show", withUser(c, m), "layouts/shell")
}
