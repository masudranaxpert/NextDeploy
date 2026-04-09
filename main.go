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
	engine.AddFunc("composeFriendly", handlers.FriendlyComposeMsg)
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

	app := fiber.New(fiber.Config{
		AppName:      "NextDeploy",
		ServerHeader: "NextDeploy",
		Views:        engine,
		// Default Fiber body limit is small; ZIP uploads need a higher cap or the connection can abort.
		BodyLimit: 512 * 1024 * 1024,
	})

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

	// Auth routes (no middleware)
	app.Get("/setup", p.SetupPage)
	app.Post("/setup", p.SetupPost)
	app.Get("/login", p.LoginPage)
	app.Post("/login", p.LoginPost)
	app.Post("/logout", p.Logout)
	app.Post("/webhooks/github/provider", p.ProviderGitHubWebhook)
	app.Post("/webhooks/github/:id", p.GitHubWebhook)

	// All other routes require authentication
	app.Use(p.AuthMiddleware)
	app.Use("/monitor", p.RequireAdminMiddleware)
	app.Use("/partials/monitor", p.RequireAdminMiddleware)
	app.Use("/terminal", p.RequireAdminMiddleware)
	app.Use("/nextdeploy", p.RequireAdminMiddleware)
	app.Use("/containers", p.RequireAdminMiddleware)
	app.Use("/images", p.RequireAdminMiddleware)
	app.Use("/volumes", p.RequireAdminMiddleware)
	app.Use("/settings", p.RequireAdminMiddleware)
	app.Use("/caddy", p.RequireAdminMiddleware)
	app.Use("/git", p.RequireAdminMiddleware)

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
	app.Post("/volumes/restore", p.VolumeRestore)
	app.Post("/volumes/remove", p.GlobalVolumeRemove)
	app.Post("/images/remove", p.GlobalImageRemove)
	app.Post("/images/prune", p.GlobalImagePrune)
	app.Post("/containers/restart", p.GlobalContainerRestart)
	app.Post("/containers/remove", p.GlobalContainerRemove)
	app.Post("/containers/remove-selected", p.GlobalContainerRemoveSelected)
	app.Post("/containers/prune", p.GlobalContainerPrune)
	app.Post("/settings/cleanup/run", p.ManualCleanupRun)
	app.Post("/settings/tmp/clean", p.TempCleanupRun)
	app.Get("/settings/tmp/info", p.TempCleanupInfo)
	app.Get("/settings", p.SettingsPage)
	app.Post("/settings", p.SettingsSave)
	app.Get("/apps", p.AppsPage)
	app.Post("/apps", p.CreateApp)
	// These URLs only accept POST (form upload). GET from the address bar redirects to Files tab.
	app.Get("/apps/:id/upload-zip", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", c.Params("id")))
	})
	app.Get("/apps/:id/upload", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", c.Params("id")))
	})
	app.Get("/apps/:id/compose-view", p.ComposeFileView)
	app.Get("/apps/:id/compose-preview", p.ComposeFileModal)
	app.Get("/apps/:id/file", p.WorkspaceFile)
	app.Get("/apps/:id/file-preview", p.WorkspaceFileModal)
	app.Get("/apps/:id/files/tree", p.WorkspaceFilesTree)
	app.Get("/apps/:id/files/blob", p.WorkspaceFilesBlob)
	app.Post("/apps/:id/files/save", p.WorkspaceFileSave)
	app.Get("/apps/:id/files/download-zip", p.WorkspaceFilesDownloadZip)
	app.Get("/apps/:id/git/tree", p.GitRepoTree)
	app.Get("/apps/:id/git/blob", p.GitRepoBlob)
	app.Get("/apps/:id/git/raw", p.GitRepoRaw)
	app.Post("/apps/:id/git", p.GitConfigSave)
	app.Post("/apps/:id/git/delete", p.GitConfigDelete)
	app.Post("/apps/:id/git/sync", p.GitSync)
	app.Get("/apps/:id/partials/browser", p.BrowsePartial)
	app.Post("/apps/:id/files/delete", p.BrowseDelete)
	app.Get("/apps/:id/partials/compose", p.AppComposePartial)
	app.Get("/apps/:id/partials/deploy-progress", p.DeployProgressPartial)
	app.Get("/apps/:id/partials/logs", p.AppLogPartial)
	app.Use("/apps/:id/ws/logs", p.WSUpgrade)
	app.Get("/apps/:id/ws/logs", fws.New(p.AppLogWebSocket))
	app.Post("/apps/:id/exec", p.AppExec)
	app.Use("/apps/:id/ws/terminal", p.WSUpgrade)
	app.Get("/apps/:id/ws/terminal", fws.New(p.TerminalWebSocket))
	app.Post("/apps/:id/deploy-logs/clear", p.ClearDeployLogs)
	app.Get("/apps/:id/deploy-logs/:logId", p.DeployLogGet)
	app.Post("/apps/:id/deploy-logs/:logId/delete", p.DeployLogDelete)
	// DELETE is POST-only; GET from the address bar redirects (avoids blank / confused responses).
	app.Get("/apps/:id/delete", func(c *fiber.Ctx) error {
		return c.Redirect("/apps", fiber.StatusFound)
	})
	// Register after all /apps/:id/... subpaths so :id is not swallowed by a shorter route.
	app.Get("/apps/:id", p.AppShow)
	app.Post("/apps/:id/upload-zip", p.UploadZip)
	app.Post("/apps/:id/upload", p.UploadFile)
	app.Post("/apps/:id/compose/up", p.ComposeUp)
	app.Post("/apps/:id/compose/down", p.ComposeDown)
	app.Post("/apps/:id/compose/restart", p.ComposeRestart)
	app.Post("/apps/:id/compose/redeploy", p.ComposeRedeploy)
	app.Post("/apps/:id/compose-file", p.SaveAppCompose)
	app.Post("/apps/:id/env", p.SaveAppEnv)
	app.Post("/apps/:id/delete", p.DeleteApp)
	app.Post("/apps/:id/containers/restart", p.ContainerRestartOp)
	app.Post("/apps/:id/containers/remove", p.ContainerRemoveOp)
	app.Post("/apps/:id/containers/remove-selected", p.ContainerRemoveSelectedOp)

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
	app.Get("/git", p.GitProvidersPage)
	app.Post("/git/github/start", p.GitHubAppManifestStart)
	app.Get("/git/github/callback", p.GitHubAppManifestCallback)
	app.Get("/git/github/setup", p.GitHubAppSetup)
	app.Get("/git/:pid/github/install", p.GitHubProviderInstall)
	app.Post("/git/:pid/github/refresh-installation", p.GitHubProviderRefreshInstall)
	app.Post("/git/gitlab/start", p.GitLabOAuthStart)
	app.Get("/git/gitlab/callback", p.GitLabOAuthCallback)
	app.Post("/git/:pid/update", p.GitProviderUpdate)
	app.Post("/git/:pid/delete", p.GitProviderDelete)

	// App source type switching
	app.Post("/apps/:id/switch-source", p.AppSwitchSource)
	app.Get("/apps/:id/git/providers/:pid/repos", p.AppGitProviderRepos)
	app.Get("/apps/:id/git/providers/:pid/branches", p.AppGitProviderBranches)

	// User management
	app.Get("/users", p.UsersPage)
	app.Post("/users", p.UserCreate)
	app.Post("/users/:id/delete", p.UserDelete)
	app.Post("/users/:id/password", p.UserChangePassword)
	app.Post("/users/:id/role", p.UserChangeRole)

	// Backup destinations
	app.Get("/backup/destinations", p.BackupDestinationsList)
	app.Post("/backup/destinations", p.BackupDestinationCreate)
	app.Post("/backup/destinations/:id/delete", p.BackupDestinationDelete)
	app.Get("/backup/gdrive/auth-url", p.BackupGDriveOAuthURL)
	app.Get("/backup/gdrive/callback", p.BackupGDriveCallback)

	// App backup operations
	app.Post("/apps/:id/backup/:destid", p.AppBackupManual)
	app.Get("/apps/:id/backup/history", p.AppBackupHistory)
	app.Post("/apps/:id/backup/restore/:historyid", p.AppBackupRestore)
	app.Post("/apps/:id/backup/schedule/:destid", p.AppBackupScheduleCreate)
	app.Get("/apps/:id/backup/schedules", p.AppBackupScheduleList)
	app.Post("/apps/:id/backup/schedule/:scheduleid/toggle", p.AppBackupScheduleToggle)
	app.Post("/apps/:id/backup/schedule/:scheduleid/delete", p.AppBackupScheduleDelete)

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("listening on %s data=%s workspaces=%s", addr, dataDir, root)
	log.Fatal(app.Listen(addr))
}
