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

	reqCtx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()

	app, err := p.DB.GetApp(reqCtx, id)
	if err != nil {
		return respondAppNotFound(c)
	}
	tab := c.Query("tab", "overview")
	isGitApp, gitCfg, hasGitCfg := p.appGitMetadata(reqCtx, id)
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
		liveOut, liveAct, liveRun = p.deploySnapshot(id)
	}
	if tab == "volumes" {
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
		gitSaved, gitSynced, gitErrFlash = p.consumeGitTabFlash(c, id)
	}

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
