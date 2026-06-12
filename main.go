package main

import (
	"encoding/base64"
	"encoding/json"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/db"
	"panel/internal/handlers"
	"panel/internal/handlers/audit"
	"panel/internal/handlers/backup"
	"panel/internal/handlers/compose"
	"panel/internal/handlers/filebrowser"
	"panel/internal/handlers/git"
	"panel/internal/perflog"
	"panel/internal/workspace"

	fws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html/v2"
)

//go:embed web/templates web/static
var webRoot embed.FS

func main() {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		log.Fatal(err)
	}

	dbPath := filepath.Join(dataDir, "panel.db")
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	root := os.Getenv("WORKSPACES_ROOT")
	if root == "" {
		root = filepath.Join(dataDir, "workspaces")
	}
	if err := os.MkdirAll(root, 0750); err != nil {
		log.Fatal(err)
	}

	templatesFS, err := fs.Sub(webRoot, "web/templates")
	if err != nil {
		log.Fatal(err)
	}
	staticFS, err := fs.Sub(webRoot, "web/static")
	if err != nil {
		log.Fatal(err)
	}

	engine := html.NewFileSystem(http.FS(templatesFS), ".html")
	engine.Reload(strings.EqualFold(os.Getenv("PANEL_DEV"), "true"))
	engine.AddFunc("urlquery", func(s string) string {
		return url.QueryEscape(s)
	})
	engine.AddFunc("bytesize", func(n int64) string {
		return workspace.FormatByteSize(n)
	})
	engine.AddFunc("composeFriendly", compose.FriendlyComposeMsg)
	engine.AddFunc("dict", func(values ...interface{}) (map[string]interface{}, error) {
		if len(values)%2 != 0 {
			return nil, fmt.Errorf("dict requires even number of arguments")
		}
		m := make(map[string]interface{}, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict keys must be strings")
			}
			m[key] = values[i+1]
		}
		return m, nil
	})
	engine.AddFunc("jsq", func(s string) string {
		b, _ := json.Marshal(s)
		return string(b)
	})
	engine.AddFunc("b64enc", func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	})
	engine.AddFunc("initials", func(s string) string {
		if len(s) == 0 {
			return "?"
		}
		if len(s) >= 2 {
			return strings.ToUpper(s[:2])
		}
		return strings.ToUpper(s[:1])
	})
	engine.AddFunc("lower", strings.ToLower)
	engine.AddFunc("sub", func(a, b int) int { return a - b })

	app := fiber.New(fiber.Config{
		AppName:      "NextDeploy",
		ServerHeader: "NextDeploy",
		Views:        engine,
		BodyLimit:                    2 * 1024 * 1024 * 1024, // 2 GiB (keep in sync with handlers.maxVolumeRestoreBytes)
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
	})

	app.Use(perflog.Middleware())
	app.Use(logger.New(logger.Config{
		Format:      "${green}[${time}]${reset} ${cyan}${status}${reset} ${magenta}${method}${reset} ${yellow}${path}${reset} ${white}${latency}${reset}\n",
	}))

	app.Use("/static", filesystem.New(filesystem.Config{
		Root:   http.FS(staticFS),
		Browse: false,
	}))

	p := &handlers.Panel{
		DB:             database,
		Store:          workspace.NewStore(root),
		WorkspacesRoot: root,
	}
	// Initialize deployRuns map to prevent race condition
	p.InitDeployRuns()
	
	if err := p.SyncRootStackComposeOnStart(); err != nil {
		log.Printf("nextdeploy root compose sync skipped: %v", err)
	}
	p.StartBackgroundJobs()
	go p.ReapplyUserCgroupLimitsOnStart()

	gitH := git.New(p)
	fbH := filebrowser.New(p)
	backH := backup.New(p)
	compH := compose.New(p)
	p.GitSyncer = gitH

	go backH.StartBackupWorker()

	// Auth routes (no middleware)
	app.Get("/setup", p.SetupPage)
	app.Post("/setup", p.SetupPost)
	app.Get("/login", p.LoginPage)
	app.Post("/login", p.LoginPost)
	app.Post("/logout", p.Logout)
	app.Post("/webhooks/github/provider", gitH.ProviderGitHubWebhook)
	app.Post("/webhooks/github/:id", gitH.GitHubWebhook)

	// All other routes require authentication
	app.Use(p.AuthMiddleware)
	app.Use("/apps/:id", p.AppAccessMiddleware)
	app.Use("/monitor", p.RequireAdminMiddleware)
	app.Use("/partials/monitor", p.RequireAdminMiddleware)
	app.Use("/terminal", p.RequireAdminMiddleware)
	app.Use("/nextdeploy", p.RequireAdminMiddleware)
	app.Use("/caddy", p.RequireAdminMiddleware)

	app.Get("/", func(c *fiber.Ctx) error {
		return c.Redirect("/overview")
	})
	app.Get("/overview", p.Overview)
	app.Get("/monitor", p.MonitorPage)
	app.Get("/partials/monitor", p.MonitorPartial)
	app.Use("/monitor/ws", p.WSUpgrade)
	app.Get("/monitor/ws", fws.New(p.MonitorWebSocket))
	app.Get("/terminal", p.VPSTerminalPage)
	app.Use("/terminal/ws", p.WSUpgrade)
	app.Get("/terminal/ws", fws.New(p.VPSTerminalWebSocket))
	app.Get("/nextdeploy", p.NextDeployPage)
	app.Get("/nextdeploy/apply-status", p.NextDeployApplyStatus)
	app.Get("/containers", p.Containers)
	app.Get("/images", p.ImagesPage)
	app.Get("/volumes", p.VolumesPage)
	app.Get("/volumes/browse", p.VolumeBrowse)
	app.Get("/volumes/download", p.VolumeDownload)
	app.Get("/volumes/restore/status", p.RequireAdminMiddleware, backH.VolumeRestoreStatus)
	app.Post("/volumes/restore", p.RequireAdminMiddleware, backH.VolumeRestore)
	app.Post("/volumes/remove", compH.GlobalVolumeRemove)
	app.Post("/images/remove", compH.GlobalImageRemove)
	app.Post("/images/prune", p.RequireAdminMiddleware, compH.GlobalImagePrune)
	app.Post("/containers/restart", compH.GlobalContainerRestart)
	app.Post("/containers/remove", compH.GlobalContainerRemove)
	app.Post("/containers/remove-selected", compH.GlobalContainerRemoveSelected)
	app.Post("/containers/prune", p.RequireAdminMiddleware, compH.GlobalContainerPrune)
	app.Post("/settings/cleanup/run", p.RequireAdminMiddleware, p.ManualCleanupRun)
	app.Post("/settings/tmp/clean", p.RequireAdminMiddleware, p.TempCleanupRun)
	app.Get("/settings/tmp/info", p.RequireAdminMiddleware, p.TempCleanupInfo)
	app.Get("/settings/audit-logs", p.RequireAdminMiddleware, audit.AuditLogsPage(database))
	app.Get("/settings", p.RequireAdminMiddleware, p.SettingsPage)
	app.Post("/settings", p.RequireAdminMiddleware, p.SettingsSave)
	app.Get("/registries", p.RegistriesPage)
	app.Post("/registries", p.AddRegistry)
	app.Post("/registries/:id/delete", p.DeleteRegistry)
	app.Get("/apps", p.AppsPage)
	app.Post("/apps", p.CreateApp)
	// These URLs only accept POST (form upload). GET from the address bar redirects to Files tab.
	app.Get("/apps/:id/upload-zip", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", c.Params("id")))
	})
	app.Get("/apps/:id/upload", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", c.Params("id")))
	})
	app.Get("/apps/:id/compose-view", compH.ComposeFileView)
	app.Get("/apps/:id/compose-preview", compH.ComposeFileModal)
	app.Get("/apps/:id/file", fbH.WorkspaceFile)
	app.Get("/apps/:id/file-preview", fbH.WorkspaceFileModal)
	app.Get("/apps/:id/files/tree", fbH.WorkspaceFilesTree)
	app.Get("/apps/:id/files/blob", fbH.WorkspaceFilesBlob)
	app.Post("/apps/:id/files/save", fbH.WorkspaceFileSave)
	app.Get("/apps/:id/files/download-zip", fbH.WorkspaceFilesDownloadZip)
	app.Get("/apps/:id/git/tree", gitH.GitRepoTree)
	app.Get("/apps/:id/git/blob", gitH.GitRepoBlob)
	app.Get("/apps/:id/git/raw", gitH.GitRepoRaw)
	app.Post("/apps/:id/git", gitH.GitConfigSave)
	app.Post("/apps/:id/git/delete", gitH.GitConfigDelete)
	app.Post("/apps/:id/git/sync", gitH.GitSync)
	app.Get("/apps/:id/partials/browser", fbH.BrowsePartial)
	app.Post("/apps/:id/files/create", fbH.BrowseCreate)
	app.Post("/apps/:id/files/delete", fbH.BrowseDelete)
	app.Post("/apps/:id/files/rename", fbH.BrowseRename)
	app.Post("/apps/:id/files/upload", fbH.BrowseUpload)
	app.Post("/apps/:id/files/url-upload", fbH.BrowseUrlUpload)
	app.Post("/apps/:id/files/move", fbH.BrowseMove)
	app.Post("/apps/:id/files/copy", fbH.BrowseCopy)
	app.Post("/apps/:id/files/mkdir", fbH.BrowseMkdir)
	app.Post("/apps/:id/files/zip", fbH.BrowseZip)
	app.Post("/apps/:id/files/unzip", fbH.BrowseUnzip)
	app.Get("/apps/:id/partials/compose", compH.AppComposePartial)
	app.Get("/apps/:id/partials/terminal-containers", compH.TerminalContainersPartial)
	app.Get("/apps/:id/partials/deploy-progress", compH.DeployProgressPartial)
	app.Get("/apps/:id/partials/logs", compH.AppLogPartial)
	app.Use("/apps/:id/ws/logs", p.WSUpgrade)
	app.Get("/apps/:id/ws/logs", fws.New(p.AppLogWebSocket))
	app.Post("/apps/:id/exec", compH.AppExec)
	app.Use("/apps/:id/ws/terminal", p.WSUpgrade)
	app.Get("/apps/:id/ws/terminal", fws.New(p.TerminalWebSocket))
	app.Post("/apps/:id/deploy-logs/clear", compH.ClearDeployLogs)
	app.Get("/apps/:id/deploy-logs/:logId", compH.DeployLogGet)
	app.Post("/apps/:id/deploy-logs/:logId/delete", compH.DeployLogDelete)
	// DELETE is POST-only; GET from the address bar redirects (avoids blank / confused responses).
	app.Get("/apps/:id/delete", func(c *fiber.Ctx) error {
		return c.Redirect("/apps", fiber.StatusFound)
	})
	// Register after all /apps/:id/... subpaths so :id is not swallowed by a shorter route.
	app.Get("/apps/:id", p.AppShow)
	app.Post("/apps/:id/upload-zip", compH.UploadZip)
	app.Post("/apps/:id/upload", compH.UploadFile)
	app.Post("/apps/:id/compose/up", compH.ComposeUp)
	app.Post("/apps/:id/compose/down", compH.ComposeDown)
	app.Post("/apps/:id/compose/restart", compH.ComposeRestart)
	app.Post("/apps/:id/compose/redeploy", compH.ComposeRedeploy)
	app.Post("/apps/:id/compose-file", p.SaveAppCompose)
	app.Post("/apps/:id/env", p.SaveAppEnv)
	app.Post("/apps/:id/delete", compH.DeleteApp)
	app.Post("/apps/:id/collaborators", p.AddCollaborator)
	app.Post("/apps/:id/collaborators/:uid/delete", p.DeleteCollaborator)
	app.Post("/apps/:id/transfer", p.TransferAppOwnership)
	app.Post("/apps/:id/containers/start", compH.ContainerStartOp)
	app.Post("/apps/:id/containers/stop", compH.ContainerStopOp)
	app.Post("/apps/:id/containers/restart", compH.ContainerRestartOp)
	app.Post("/apps/:id/containers/remove", compH.ContainerRemoveOp)
	app.Post("/apps/:id/containers/remove-selected", compH.ContainerRemoveSelectedOp)

	// Domains (Caddy routing per app)
	app.Get("/apps/:id/domains", p.AppDomainPartial)
	app.Post("/apps/:id/domains", p.AppDomainCreate)
	app.Post("/apps/:id/domains/:did/edit", p.AppDomainEdit)
	app.Post("/apps/:id/domains/:did/delete", p.AppDomainDelete)
	app.Get("/apps/:id/domains/:did/labels", p.AppDomainLabels)
	app.Get("/apps/:id/domains/:did/dns-check", p.AppDomainDNSCheck)

	// Caddy global management
	app.Get("/caddy", p.CaddyPage)
	app.Post("/caddy/config", p.CaddySaveConfig)
	app.Post("/caddy/container", p.CaddyContainerAction)
	app.Get("/caddy/logs", p.CaddyLogs)
	app.Post("/nextdeploy/panel", p.SaveNextDeployPanelConfig)
	app.Post("/nextdeploy/shared-volumes", p.SaveNextDeploySharedVolumes)

	// Git providers (GitHub App + GitLab OAuth only — no manual token POST)
	app.Get("/git", gitH.GitProvidersPage)
	app.Post("/git/github/start", gitH.GitHubAppManifestStart)
	app.Get("/git/github/callback", gitH.GitHubAppManifestCallback)
	app.Get("/git/github/setup", gitH.GitHubAppSetup)
	app.Get("/git/:pid/github/install", gitH.GitHubProviderInstall)
	app.Post("/git/:pid/github/refresh-installation", gitH.GitHubProviderRefreshInstall)
	app.Post("/git/gitlab/start", gitH.GitLabOAuthStart)
	app.Get("/git/gitlab/callback", gitH.GitLabOAuthCallback)
	app.Post("/git/:pid/update", gitH.GitProviderUpdate)
	app.Post("/git/:pid/delete", gitH.GitProviderDelete)

	// App source type switching
	app.Post("/apps/:id/switch-source", gitH.AppSwitchSource)
	app.Get("/apps/:id/git/providers/:pid/repos", gitH.AppGitProviderRepos)
	app.Get("/apps/:id/git/providers/:pid/branches", gitH.AppGitProviderBranches)

	// User management
	app.Get("/users", p.UsersPage)
	app.Post("/users", p.UserCreate)
	app.Get("/users/:id/edit", p.UserEditPage)
	app.Post("/users/:id/delete", p.UserDelete)
	app.Post("/users/:id/password", p.UserChangePassword)
	app.Post("/users/:id/role", p.UserChangeRole)
	app.Post("/users/:id/status", p.UserChangeStatus)
	app.Post("/users/:id/limits", p.UserChangeLimits)

	// Backup destinations
	app.Get("/backup", backH.BackupPage)
	app.Get("/backup/destinations", backH.BackupDestinationsList)
	app.Post("/backup/destinations", backH.BackupDestinationCreate)
	app.Post("/backup/destinations/:id/delete", backH.BackupDestinationDelete)
	app.Get("/backup/gdrive/auth-url", backH.BackupGDriveOAuthURL)
	app.Get("/backup/gdrive/callback", backH.BackupGDriveCallback)

	// App backup operations
	app.Post("/apps/:id/backup/:destid", backH.AppBackupManual)
	app.Get("/apps/:id/backup/history", backH.AppBackupHistory)
	app.Get("/apps/:id/backup/history/:historyid/log", backH.AppBackupHistoryLog)
	app.Get("/apps/:id/backup/history/:historyid/drivelink", backH.AppBackupDriveLink)
	app.Get("/apps/:id/backup/restore-status", backH.AppBackupRestoreStatus)
	app.Post("/apps/:id/backup/restore/:historyid", backH.AppBackupRestore)
	app.Post("/apps/:id/backup/schedule/:scheduleid/edit", backH.AppBackupScheduleUpdate)
	app.Post("/apps/:id/backup/schedule/:destid", backH.AppBackupScheduleCreate)
	app.Get("/apps/:id/backup/schedules", backH.AppBackupScheduleList)
	app.Post("/apps/:id/backup/schedule/:scheduleid/toggle", backH.AppBackupScheduleToggle)
	app.Post("/apps/:id/backup/schedule/:scheduleid/delete", backH.AppBackupScheduleDelete)

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("listening on %s data=%s workspaces=%s", addr, dataDir, root)
	log.Fatal(app.Listen(addr))
}
