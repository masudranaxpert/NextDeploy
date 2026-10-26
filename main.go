package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/handlers"
	"panel/internal/mcp"
	"panel/internal/perflog"
	"panel/internal/phphosting"
	"panel/internal/sysinfo"
	"panel/internal/workspace"

	fws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

//go:embed all:web/dist
var webRoot embed.FS

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "migrate" {
		runMigrateCLI(os.Args[2:])
		return
	}

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

	if err := database.ClaimOrphanedRegistries(context.Background()); err != nil {
		log.Printf("registry ownership migration: %v", err)
	}

	root := os.Getenv("WORKSPACES_ROOT")
	if root == "" {
		root = filepath.Join(dataDir, "workspaces")
	}
	if err := os.MkdirAll(root, 0750); err != nil {
		log.Fatal(err)
	}

	distFS, err := fs.Sub(webRoot, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	spaIndex, _ := fs.ReadFile(distFS, "index.html")

	app := fiber.New(fiber.Config{
		AppName:                      "NextDeploy",
		ServerHeader:                 "NextDeploy",
		BodyLimit:                    2 * 1024 * 1024 * 1024, // 2 GiB (keep in sync with handlers.maxVolumeRestoreBytes)
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
		IdleTimeout:                  2 * time.Minute, // drop idle keep-alive connections; uploads are not cut short
	})

	p := &handlers.Panel{
		DB:             database,
		Store:          workspace.NewStore(root),
		WorkspacesRoot: root,
		SPAIndexHTML:   spaIndex,
	}
	p.PHP = phphosting.NewManager(database, root)
	p.PHP.CgroupParent = p.UserCgroupParent
	// Initialize deployRuns map to prevent race condition
	p.InitDeployRuns()
	go p.InitBandwidthQoS()

	app.Use(perflog.Middleware())
	app.Use(func(c *fiber.Ctx) error {
		sysinfo.RecordRequest()
		return c.Next()
	})
	app.Use(p.SecurityHeadersMiddleware)
	// The server-wide body limit is 2 GiB for uploads and restores. Everything else, above all the
	// routes anyone can reach without logging in, only needs a small body.
	app.Use(func(c *fiber.Ctx) error {
		path := c.Path()
		public := strings.HasPrefix(path, "/webhooks/") || path == "/login" || path == "/setup"
		cl := c.Request().Header.ContentLength()
		if public && (cl > 5<<20 || cl < 0 && c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead) {
			return c.SendStatus(fiber.StatusRequestEntityTooLarge)
		}
		if cl > 64<<20 {
			for _, big := range []string{"upload", "restore", "archive", "import", "/migrate", "/files", "/backup", "/volumes", "workspace"} {
				if strings.Contains(path, big) {
					return c.Next()
				}
			}
			return c.SendStatus(fiber.StatusRequestEntityTooLarge)
		}
		return c.Next()
	})
	app.Use(compress.New())
	app.Use(logger.New(logger.Config{
		Format: "${green}[${time}]${reset} ${cyan}${status}${reset} ${magenta}${method}${reset} ${yellow}${path}${reset} ${white}${latency}${reset}\n",
	}))

	// Serve Vite hashed static assets from embedded web/dist
	if assetsFS, err := fs.Sub(distFS, "assets"); err == nil {
		app.Use("/assets", func(c *fiber.Ctx) error {
			c.Set("Cache-Control", "public, max-age=31536000, immutable")
			return c.Next()
		})
		app.Use("/assets", gzipAssets(assetsFS))
		app.Use("/assets", filesystem.New(filesystem.Config{
			Root:   http.FS(assetsFS),
			Browse: false,
		}))
	}
	// Serve public root brand assets (icons, logos, favicons) directly from embedded web/dist
	app.Use(func(c *fiber.Ctx) error {
		p := strings.TrimPrefix(c.Path(), "/")
		if strings.HasPrefix(p, "nextdeploy") || strings.HasPrefix(p, "apple-touch") || p == "favicon.ico" {
			lookup := p
			data, err := fs.ReadFile(distFS, lookup)
			if err != nil && p == "favicon.ico" {
				lookup = "nextdeploy.ico"
				data, err = fs.ReadFile(distFS, lookup)
			}
			if err == nil {
				switch {
				case strings.HasSuffix(lookup, ".svg"):
					c.Set("Content-Type", "image/svg+xml")
				case strings.HasSuffix(lookup, ".png"):
					c.Set("Content-Type", "image/png")
				case strings.HasSuffix(lookup, ".ico"):
					c.Set("Content-Type", "image/x-icon")
				}
				c.Set("Cache-Control", "public, max-age=86400")
				return c.Send(data)
			}
		}
		return c.Next()
	})

	if err := p.SyncRootStackComposeOnStart(); err != nil {
		log.Printf("nextdeploy root compose sync skipped: %v", err)
	}
	p.StartBackgroundJobs()
	go p.ReapplyUserCgroupLimitsOnStart()
	go p.ReapplyAppCgroupLimitsOnStart()
	go p.WarmUsage()
	go p.StartBackupWorker()
	if p.PHP != nil {
		p.PHP.StartCronScheduler(context.Background())
		p.PHP.StartPMAJanitor(context.Background())
		p.PHP.StartSymlinkSweeper(context.Background())
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			p.PHP.Reconcile(ctx)
			p.ReconcileProxyRoutes(ctx)
		}()
	}

	// Auth routes (no session middleware)
	app.Get("/setup", p.SetupPage)
	app.Post("/setup", p.SetupPost)
	app.Get("/login", p.LoginPage)
	app.Post("/login", p.LoginPost)
	app.Post("/logout", p.Logout)
	app.Post("/webhooks/github/provider", p.ProviderGitHubWebhook)
	app.Post("/webhooks/github/:id", p.GitHubWebhook)
	app.Post("/webhooks/gitlab/:id", p.GitHubWebhook)
	app.Post("/webhooks/:id", p.GitHubWebhook)
	app.Get("/migrate/download/:token", p.MigrateDownload)
	app.Get("/api/panel/version", p.SPAVersion)

	// Model Context Protocol (MCP) server endpoints for external AI coding agents
	mcpServer := mcp.NewServer(p)
	mcpServer.RegisterRoutes(app)

	// PHP sites: hand files written through the panel/API over to the site's UID.
	app.Use("/api/v1/apps/:id/files", p.PHPOwnershipMiddleware)
	app.Use("/api/v1/apps/:id/workspace/archive", p.PHPOwnershipMiddleware)

	// External HTTP REST API endpoints for agent workspace uploads & downloads
	app.Post("/api/v1/apps/:id/workspace/archive", p.APIAuthMiddleware, p.UploadWorkspaceArchive)
	app.Get("/api/v1/apps/:id/workspace/archive", p.APIAuthMiddleware, p.DownloadWorkspaceArchive)
	app.Get("/api/v1/metrics/history", p.APIAuthMiddleware, p.MetricsHistory)

	// CLI REST API — apps, manifest, sessions
	app.Get("/api/v1/apps", p.APIAuthMiddleware, p.APIAppsList)
	app.Delete("/api/v1/apps/:id", p.APIAuthMiddleware, p.APIAppDelete)
	app.Post("/api/v1/apps/:id/delete", p.APIAuthMiddleware, p.APIAppDelete)
	app.Get("/api/v1/apps/:id/manifest", p.APIAuthMiddleware, p.APIManifest)
	app.Get("/api/v1/apps/:id/files", p.APIAuthMiddleware, p.APIAppFilesList)
	app.Get("/api/v1/apps/:id/files/content", p.APIAuthMiddleware, p.APIAppFileContent)
	app.Post("/api/v1/apps/:id/files/content", p.APIAuthMiddleware, p.APIAppFileSave)
	app.Put("/api/v1/apps/:id/files/content", p.APIAuthMiddleware, p.APIAppFileSave)
	app.Delete("/api/v1/apps/:id/files", p.APIAuthMiddleware, p.APIAppFileDelete)
	app.Post("/api/v1/apps/:id/files/delete", p.APIAuthMiddleware, p.APIAppFileDelete)
	app.Post("/api/v1/cli/sessions", p.APIAuthMiddleware, p.APICliHeartbeat)
	app.Delete("/api/v1/cli/sessions/:id", p.APIAuthMiddleware, p.APICliSessionDelete)
	app.Get("/api/v1/cli/sessions", p.APIAuthMiddleware, p.APICliSessionsList)
	app.Get("/api/v1/cli/whoami", p.APIAuthMiddleware, p.APICliWhoami)

	// Edge Shield Node integration endpoints (token-authenticated)
	app.Get("/api/v1/edge/verify-domain", p.EdgeVerifyDomain)
	app.Get("/api/v1/edge/setup.sh", p.GetEdgeShieldSetupScript)
	app.Post("/api/v1/edge/shield-heartbeat", p.EdgeShieldHeartbeat)
	app.Get("/api/v1/edge/shield-heartbeat", p.EdgeShieldHeartbeat)

	// Session authentication middleware for panel routes
	app.Use(p.AuthMiddleware)

	// Unified SPA bootstrap JSON endpoint
	app.Get("/api/panel/bootstrap", p.SPABootstrap)

	app.Use("/apps/:id", p.AppAccessMiddleware)
	app.Use("/monitor", p.RequireAdminMiddleware)
	app.Use("/terminal", p.RequireAdminMiddleware)
	app.Use("/nextdeploy", p.RequireAdminMiddleware)
	app.Use("/caddy", p.RequireAdminMiddleware)
	app.Use("/proxy", p.RequireAdminMiddleware)
	app.Use("/proxy-options", p.RequireAdminMiddleware)
	app.Use("/proxy-routes", p.RequireAdminMiddleware)
	app.Use("/migrate", p.RequireAdminMiddleware)
	app.Use("/users", p.RequireAdminMiddleware)
	app.Use("/settings", p.RequireAdminMiddleware)
	app.Use("/cli-sessions", p.RequireAdminMiddleware)
	app.Use("/php-instance", p.RequireAdminMiddleware)
	app.Use("/shield-proxies", p.RequireAdminMiddleware)
	app.Use("/apps/:id/files", p.PHPOwnershipMiddleware)
	app.Use("/apps/:id/upload", p.PHPOwnershipMiddleware)
	app.Use("/apps/:id/upload-zip", p.PHPOwnershipMiddleware)

	app.Get("/", func(c *fiber.Ctx) error {
		return c.Redirect("/overview")
	})
	app.Get("/overview", p.ServeSPA)
	app.Get("/monitor", p.ServeSPA)
	app.Use("/monitor/ws", p.WSUpgrade)
	app.Get("/monitor/ws", fws.New(p.MonitorWebSocket))
	app.Get("/terminal", p.ServeSPA)
	app.Use("/terminal/ws", p.WSUpgrade)
	app.Get("/terminal/ws", fws.New(p.VPSTerminalWebSocket))
	app.Get("/nextdeploy", p.ServeSPA)
	app.Get("/nextdeploy/apply-status", p.NextDeployApplyStatus)
	app.Get("/containers", p.ServeSPA)
	app.Get("/images", p.ServeSPA)
	app.Get("/volumes", p.ServeSPA)
	app.Get("/volumes/browse", p.VolumeBrowse)
	app.Get("/volumes/download", p.VolumeDownload)
	app.Get("/volumes/restore/status", p.RequireAdminMiddleware, p.VolumeRestoreStatus)
	app.Post("/volumes/restore", p.RequireAdminMiddleware, p.VolumeRestore)
	app.Post("/volumes/remove", p.GlobalVolumeRemove)
	app.Post("/images/remove", p.GlobalImageRemove)
	app.Post("/images/prune", p.RequireAdminMiddleware, p.GlobalImagePrune)
	app.Post("/containers/restart", p.GlobalContainerRestart)
	app.Post("/containers/remove", p.GlobalContainerRemove)
	app.Post("/containers/remove-selected", p.GlobalContainerRemoveSelected)
	app.Post("/containers/prune", p.RequireAdminMiddleware, p.GlobalContainerPrune)
	app.Post("/settings/cleanup/run", p.RequireAdminMiddleware, p.ManualCleanupRun)
	app.Post("/settings/tmp/clean", p.RequireAdminMiddleware, p.TempCleanupRun)
	app.Get("/settings/tmp/info", p.RequireAdminMiddleware, p.TempCleanupInfo)
	app.Get("/settings/audit-logs", p.RequireAdminMiddleware, p.AuditLogsPageOrJSON)
	app.Get("/api/audit-logs", p.AuditLogsPageOrJSON) // scoped per user inside the handler
	app.Get("/migrate", p.ServeSPA)
	app.Post("/migrate/export", p.MigrateExportStart)
	app.Get("/migrate/exports/:id/status", p.MigrateExportStatus)
	app.Post("/migrate/exports/:id/delete", p.MigrateExportDelete)
	app.Get("/migrate/estimate", p.MigrateEstimate)
	app.Get("/settings", p.RequireAdminMiddleware, p.SettingsPageOrJSON)
	app.Get("/api/v1/settings", p.RequireAdminMiddleware, p.SettingsPageOrJSON)
	app.Post("/settings", p.RequireAdminMiddleware, p.SettingsSave)
	app.Get("/shield-proxies", p.RequireAdminMiddleware, p.ServeSPA)
	app.Get("/api/v1/shield-nodes", p.RequireAdminMiddleware, p.ListShieldNodes)
	app.Post("/api/v1/shield-nodes", p.RequireAdminMiddleware, p.CreateShieldNode)
	app.Put("/api/v1/shield-nodes/:id", p.RequireAdminMiddleware, p.UpdateShieldNode)
	app.Delete("/api/v1/shield-nodes/:id", p.RequireAdminMiddleware, p.DeleteShieldNode)
	app.Post("/api/v1/shield-nodes/:id/test", p.RequireAdminMiddleware, p.TestShieldNode)
	app.Post("/api/v1/shield-nodes/:id/reassign", p.RequireAdminMiddleware, p.ReassignShieldNodeDomains)
	app.Get("/registries", p.ServeSPA)
	app.Post("/registries", p.AddRegistry)
	app.Post("/registries/:id/delete", p.DeleteRegistry)
	app.Get("/mcp-docs", mcpServer.MCPDocsPage)
	app.Post("/mcp-docs/tokens", mcpServer.CreateAPITokenPost)
	app.Post("/mcp-docs/tokens/:id/delete", mcpServer.DeleteAPITokenPost)
	app.Post("/mcp-docs/tokens/:id/toggle-reveal", mcpServer.ToggleAPITokenEnvRevealPost)
	app.Post("/mcp-docs/tokens/:id/toggle-container-exec", mcpServer.ToggleAPITokenContainerExecPost)
	app.Post("/mcp-docs/tokens/:id/toggle-exec", mcpServer.ToggleAPITokenServerExecPost)
	app.Post("/mcp-docs/tokens/:id/toggle-app-delete", mcpServer.ToggleAPITokenAppDeletePost)
	app.Post("/mcp-docs/toggle-status", mcpServer.ToggleMCPStatusPost)
	app.Get("/cli-sessions", p.RequireAdminMiddleware, p.CLISessionsPage)
	app.Post("/cli-sessions/tokens", p.RequireAdminMiddleware, p.CreateCLITokenPost)
	app.Post("/cli-sessions/tokens/:id/delete", p.RequireAdminMiddleware, p.DeleteCLITokenPost)
	app.Post("/cli-sessions/tokens/:id/toggle-reveal", p.RequireAdminMiddleware, p.ToggleCLITokenEnvRevealPost)
	app.Post("/cli-sessions/tokens/:id/toggle-container-exec", p.RequireAdminMiddleware, p.ToggleCLITokenContainerExecPost)
	app.Post("/cli-sessions/tokens/:id/toggle-exec", p.RequireAdminMiddleware, p.ToggleCLITokenServerExecPost)
	app.Post("/cli-sessions/tokens/:id/toggle-app-delete", p.RequireAdminMiddleware, p.ToggleCLITokenAppDeletePost)
	app.Get("/apps", p.ServeSPA)
	app.Post("/apps", p.CreateApp)
	// These URLs only accept POST (form upload). GET from the address bar redirects to Files tab.
	app.Get("/apps/:id/upload-zip", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", c.Params("id")))
	})
	app.Get("/apps/:id/upload", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=files", c.Params("id")))
	})
	app.Get("/apps/:id/compose-view", p.ComposeFileView)
	app.Get("/apps/:id/file", p.WorkspaceFile)
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
	app.Post("/apps/:id/files/create", p.BrowseCreate)
	app.Post("/apps/:id/files/delete", p.BrowseDelete)
	app.Post("/apps/:id/files/rename", p.BrowseRename)
	app.Post("/apps/:id/files/upload", p.BrowseUpload)
	app.Post("/apps/:id/files/url-upload", p.BrowseUrlUpload)
	app.Post("/apps/:id/files/move", p.BrowseMove)
	app.Post("/apps/:id/files/copy", p.BrowseCopy)
	app.Post("/apps/:id/files/mkdir", p.BrowseMkdir)
	app.Post("/apps/:id/files/chmod", p.BrowseChmod)
	app.Post("/apps/:id/files/zip", p.BrowseZip)
	app.Post("/apps/:id/files/unzip", p.BrowseUnzip)
	app.Get("/apps/:id/partials/deploy-progress", p.DeployProgressPartial)
	app.Get("/apps/:id/logs", p.AppLogsREST)
	app.Use("/apps/:id/ws/logs", p.WSUpgrade)
	app.Get("/apps/:id/ws/logs", fws.New(p.AppLogWebSocket))
	app.Post("/apps/:id/exec", p.AppExec)
	app.Use("/apps/:id/ws/terminal", p.WSUpgrade)
	app.Get("/apps/:id/ws/terminal", fws.New(p.TerminalWebSocket))
	app.Post("/apps/:id/deploy-logs/clear", p.ClearDeployLogs)
	app.Get("/apps/:id/deploy-logs/:logId", p.DeployLogGet)
	app.Post("/apps/:id/deploy-logs/:logId/delete", p.DeployLogDelete)
	app.Post("/apps/:id/deploy-logs/:logId/rollback", p.RollbackDeployment)
	app.Post("/apps/:id/deployments/:depId/rollback", p.RollbackDeployment)
	app.Post("/apps/:id/slots/pin", p.PinSlot)
	app.Post("/apps/:id/slots/promote", p.PromoteSlot)
	// DELETE is POST-only; GET from the address bar redirects (avoids blank / confused responses).
	app.Get("/apps/:id/delete", func(c *fiber.Ctx) error {
		return c.Redirect("/apps", fiber.StatusFound)
	})
	// Register after all /apps/:id/... subpaths so :id is not swallowed by a shorter route.
	app.Get("/apps/:id", p.ServeSPA)
	app.Post("/apps/:id/upload-zip", p.UploadZip)
	app.Post("/apps/:id/upload", p.UploadFile)
	app.Post("/apps/:id/compose/up", p.ComposeUp)
	app.Post("/apps/:id/compose/down", p.ComposeDown)
	app.Post("/apps/:id/compose/restart", p.ComposeRestart)
	app.Post("/apps/:id/compose/redeploy", p.ComposeRedeploy)
	app.Post("/apps/:id/compose-file", p.SaveAppCompose)
	app.Post("/apps/:id/env", p.SaveAppEnv)
	app.Post("/apps/:id/delete", p.DeleteApp)
	app.Post("/apps/:id/collaborators", p.AddCollaborator)
	app.Post("/apps/:id/collaborators/:uid/delete", p.DeleteCollaborator)
	app.Post("/apps/:id/transfer", p.TransferAppOwnership)
	app.Get("/apps/:id/limits", p.AppLimitsGet)
	app.Post("/apps/:id/limits", p.AppLimitsSet)
	app.Post("/apps/:id/containers/start", p.ContainerStartOp)
	app.Post("/apps/:id/containers/stop", p.ContainerStopOp)
	app.Post("/apps/:id/containers/restart", p.ContainerRestartOp)
	app.Post("/apps/:id/containers/remove", p.ContainerRemoveOp)
	app.Post("/apps/:id/containers/remove-selected", p.ContainerRemoveSelectedOp)

	// Domains (Caddy routing per app)
	app.Post("/apps/:id/domains", p.AppDomainCreate)
	app.Post("/apps/:id/domains/:did/edit", p.AppDomainEdit)
	app.Post("/apps/:id/domains/:did/delete", p.AppDomainDelete)
	app.Post("/apps/:id/domains/:did/primary", p.AppDomainSetPrimary)
	app.Post("/apps/:id/preview", p.AppPreviewSave)
	app.Get("/apps/:id/domains/:did/labels", p.AppDomainLabels)
	app.Get("/apps/:id/domains/:did/dns-check", p.AppDomainDNSCheck)

	// PHP hosting
	app.Get("/php", func(c *fiber.Ctx) error {
		return c.Redirect("/php-sites", fiber.StatusFound)
	})
	app.Get("/php-sites", p.ServeSPA)
	app.Get("/php-instance", p.RequireAdminMiddleware, p.ServeSPA)
	app.Get("/api/php/status", p.PHPStatus)
	app.Get("/api/php/instance", p.RequireAdminMiddleware, p.PHPInstance)
	app.Post("/api/php/instance/module", p.RequireAdminMiddleware, p.PHPSetModule)
	app.Post("/api/php/instance/mysql", p.RequireAdminMiddleware, p.PHPMySQLAction)
	app.Post("/api/php/instance/versions", p.RequireAdminMiddleware, p.PHPVersionAction)
	app.Get("/api/php/instance/versions/:version/build", p.RequireAdminMiddleware, p.PHPBuildStatus)
	app.Post("/api/php/instance/containers/restart", p.RequireAdminMiddleware, p.PHPRestartContainer)
	app.Post("/api/php/instance/settings", p.RequireAdminMiddleware, p.PHPInstanceSettings)
	app.Post("/api/php/instance/user-limits", p.RequireAdminMiddleware, p.PHPUserLimits)
	app.Post("/api/php/sites", p.PHPCreateSite)
	app.Get("/apps/:id/php", p.PHPSiteDetail)
	app.Post("/apps/:id/php/settings", p.PHPSiteSettings)
	app.Post("/apps/:id/php/restart", p.PHPSiteRestart)
	app.Post("/apps/:id/php/fix-permissions", p.PHPSiteFixPermissions)
	app.Get("/apps/:id/php/provision", p.PHPSiteProvision)
	app.Get("/apps/:id/php/logs", p.PHPLogs)
	app.Post("/apps/:id/php/databases", p.PHPDatabaseCreate)
	app.Post("/apps/:id/php/db-users", p.PHPDBUserCreate)
	app.Post("/apps/:id/php/db-users/:uid/grants", p.PHPDBUserGrants)
	app.Post("/apps/:id/php/db-users/:uid/password", p.PHPDBUserPassword)
	app.Post("/apps/:id/php/db-users/:uid/delete", p.PHPDBUserDelete)
	app.Post("/apps/:id/php/databases/:dbid/delete", p.PHPDatabaseDelete)
	app.Get("/apps/:id/php/databases/:dbid/export", p.PHPDatabaseExport)
	app.Post("/apps/:id/php/databases/:dbid/import", p.PHPDatabaseImport)
	app.Post("/apps/:id/php/pma/open", p.PHPPMAOpen)
	app.Get("/apps/:id/php/security", p.PHPSecurityGet)
	app.Post("/apps/:id/php/security", p.PHPSecuritySave)
	app.Post("/api/php/instance/pma", p.RequireAdminMiddleware, p.PHPPMAAction)
	app.Post("/api/php/instance/pma/open", p.RequireAdminMiddleware, p.PHPPMAOpenAll)
	app.All("/phpmyadmin", p.PMAProxy)
	app.All("/phpmyadmin/*", p.PMAProxy)
	app.Post("/apps/:id/php/cron", p.PHPCronCreate)
	app.Post("/apps/:id/php/cron/:jid", p.PHPCronUpdate)
	app.Post("/apps/:id/php/cron/:jid/delete", p.PHPCronDelete)
	app.Post("/apps/:id/php/cron/:jid/run", p.PHPCronRun)

	// Caddy global management
	app.Get("/caddy", p.ServeSPA)
	app.Get("/proxy-options", p.ServeSPA)
	app.Get("/proxy-routes", p.ServeSPA)
	app.Get("/proxy", p.ServeSPA)
	app.Get("/mcp", p.ServeSPA)
	app.Get("/audit-logs", p.ServeSPA)
	app.Post("/caddy/config", p.CaddySaveConfig)
	app.Post("/caddy/container", p.CaddyContainerAction)
	app.Get("/caddy/logs", p.CaddyLogs)
	app.Get("/caddy/overview", p.CaddyOverview)
	app.Get("/caddy/options", p.CaddyOptionsGet)
	app.Post("/caddy/options", p.CaddyOptionsSave)
	app.Get("/caddy/routes", p.CaddyRoutesList)
	app.Post("/caddy/routes", p.CaddyRouteSave)
	app.Post("/caddy/routes/:id", p.CaddyRouteSave)
	app.Post("/caddy/routes/:id/toggle", p.CaddyRouteToggle)
	app.Post("/caddy/routes/:id/delete", p.CaddyRouteDelete)
	app.Post("/nextdeploy/panel", p.SaveNextDeployPanelConfig)
	app.Post("/nextdeploy/shared-volumes", p.SaveNextDeploySharedVolumes)

	// Git providers (GitHub App + GitLab OAuth only — no manual token POST)
	app.Get("/git", p.ServeSPA)
	app.Post("/git/github/start", p.GitHubAppManifestStart)
	app.Get("/git/github/callback", p.GitHubAppManifestCallback)
	app.Get("/git/github/setup", p.GitHubAppSetup)
	app.Get("/git/:pid/github/install", p.GitHubProviderInstall)
	app.Post("/git/:pid/github/refresh-installation", p.GitHubProviderRefreshInstall)
	app.Post("/git/gitlab/start", p.GitLabOAuthStart)
	app.Get("/git/gitlab/callback", p.GitLabOAuthCallback)
	app.Get("/git/providers", p.APIAuthMiddleware, p.GitProvidersList)
	app.Get("/api/v1/git/providers", p.APIAuthMiddleware, p.GitProvidersList)
	app.Post("/git/providers", p.APIAuthMiddleware, p.GitProviderCreate)
	app.Post("/api/v1/git/providers", p.APIAuthMiddleware, p.GitProviderCreate)
	app.Post("/git/:pid/update", p.GitProviderUpdate)
	app.Post("/git/:pid/delete", p.GitProviderDelete)
	app.Get("/api/v1/git/webhooks", p.APIAuthMiddleware, p.APIWebhookDeliveries)
	app.Get("/git/webhooks", p.APIAuthMiddleware, p.APIWebhookDeliveries)

	// App source type switching
	app.Post("/apps/:id/switch-source", p.AppSwitchSource)
	app.Post("/apps/:id/git-sync", p.AppGitSyncToggle)
	app.Get("/apps/:id/git/providers/:pid/repos", p.AppGitProviderRepos)
	app.Get("/apps/:id/git/providers/:pid/branches", p.AppGitProviderBranches)
	app.Get("/apps/:id/git/remote-branches", p.PublicGitRemoteBranches)

	// User management
	app.Get("/users", p.ServeSPA)
	app.Post("/users", p.UserCreate)
	app.Get("/users/:id/edit", p.ServeSPA)
	app.Post("/users/:id/delete", p.UserDelete)
	app.Post("/users/:id/password", p.UserChangePassword)
	app.Post("/users/:id/role", p.UserChangeRole)
	app.Post("/users/:id/status", p.UserChangeStatus)
	app.Post("/users/:id/limits", p.UserChangeLimits)

	// Backup destinations
	app.Get("/backup", p.ServeSPA)
	app.Get("/backup/destinations", p.BackupDestinationsList)
	app.Post("/backup/destinations", p.BackupDestinationCreate)
	app.Post("/backup/destinations/:id/delete", p.BackupDestinationDelete)
	app.Get("/backup/gdrive/auth-url", p.BackupGDriveOAuthURL)
	app.Get("/backup/gdrive/callback", p.BackupGDriveCallback)

	// App backup operations
	app.Post("/apps/:id/backup/:destid", p.AppBackupManual)
	app.Get("/apps/:id/backup/history", p.AppBackupHistory)
	app.Get("/apps/:id/backup/history/:historyid/log", p.AppBackupHistoryLog)
	app.Get("/apps/:id/backup/history/:historyid/drivelink", p.AppBackupDriveLink)
	app.Get("/apps/:id/backup/restore-status", p.AppBackupRestoreStatus)
	app.Post("/apps/:id/backup/restore/:historyid", p.AppBackupRestore)
	app.Post("/apps/:id/backup/schedule/:scheduleid/edit", p.AppBackupScheduleUpdate)
	app.Post("/apps/:id/backup/schedule/:destid", p.AppBackupScheduleCreate)
	app.Get("/apps/:id/backup/schedules", p.AppBackupScheduleList)
	app.Post("/apps/:id/backup/schedule/:scheduleid/toggle", p.AppBackupScheduleToggle)
	app.Post("/apps/:id/backup/schedule/:scheduleid/delete", p.AppBackupScheduleDelete)

	// SPA Fallback: for any unhandled GET request that is not an API route, serve the SPA
	app.Use(func(c *fiber.Ctx) error {
		if c.Method() == fiber.MethodGet && !strings.HasPrefix(c.Path(), "/api/") {
			return p.ServeSPA(c)
		}
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Not found"})
	})

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("listening on %s data=%s workspaces=%s", addr, dataDir, root)
	log.Fatal(app.Listen(addr))
}
