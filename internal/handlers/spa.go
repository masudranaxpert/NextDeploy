package handlers

import (
	"os"
	"strconv"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/sysinfo"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

// ServeSPA serves the embedded React SPA index.html for client-side routing.
func (p *Panel) ServeSPA(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if len(p.SPAIndexHTML) > 0 {
		c.Type("html", "utf-8")
		return c.Send(p.SPAIndexHTML)
	}
	// Fallback to reading from disk during local dev if not embedded
	for _, candidate := range []string{"web/dist/index.html", "dist/index.html", "index.html"} {
		if b, err := os.ReadFile(candidate); err == nil && len(b) > 0 {
			c.Type("html", "utf-8")
			return c.Send(b)
		}
	}
	return c.Status(fiber.StatusServiceUnavailable).SendString("NextDeploy UI bundle not found. Run 'npm run build' first.")
}

// SPABootstrap returns a unified JSON payload for the React SPA control plane.
// GET /api/panel/bootstrap
func (p *Panel) SPABootstrap(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"ok": false, "error": "unauthorized"})
	}

	isAdmin := u.Role == db.RoleAdmin
	sys := sysinfo.Collect(ctx)

	var apps []db.App
	if isAdmin {
		apps, _ = p.DB.ListApps(ctx)
	} else {
		apps, _ = p.DB.ListAppsForUser(ctx, u.ID)
	}

	type appSummary struct {
		db.App
		Domains       []db.AppDomain          `json:"domains"`
		Services      []string                `json:"services"`
		GitConfig     *db.AppGitConfig        `json:"git_config,omitempty"`
		DeployLogs    []db.DeployLog          `json:"deploy_logs"`
		Collaborators []db.CollaboratorDetail `json:"collaborators"`
		Volumes       []string                `json:"volumes"`
		BackupHistory []db.BackupHistory      `json:"backup_history"`
		Schedules     []db.BackupSchedule     `json:"backup_schedules"`
		EnvContent    string                  `json:"env_content"`
		Preview       PreviewConfig           `json:"preview"`
		PreviewBase   string                  `json:"preview_host_base"`
		Ports         []db.AllocatedPort      `json:"ports"`
	}

	allVolNames, _ := volumex.List(ctx)
	allProjects := p.AllPanelComposeProjects(ctx)
	matcher := volumex.SharedMatcher(ctx)

	appList := make([]appSummary, 0, len(apps))
	for _, a := range apps {
		doms, _ := p.DB.ListAppDomains(ctx, a.ID)
		svcs := p.LoadComposeServices(ctx, a.ID)
		var gitPtr *db.AppGitConfig
		if gcfg, err := p.DB.GetAppGitConfig(ctx, a.ID); err == nil {
			gitPtr = &gcfg
		}
		dlogs, _ := p.DB.ListDeployLogs(ctx, a.ID, 10)
		collabs, _ := p.DB.ListCollaboratorsWithUsers(ctx, a.ID)
		projCands := append([]string{a.ID, strings.ReplaceAll(a.ID, "-", "_"), a.Name}, p.ComposeProjectCandidates(ctx, a, a.ID)...)
		q := p.AppVolumeQuery(ctx, a, allProjects, projCands...)
		appVols, _ := matcher.ListForAppFromNames(ctx, q, allVolNames)
		bHist, _ := p.DB.ListBackupHistory(ctx, a.ID, 25)
		bSched, _ := p.DB.ListBackupSchedules(ctx, a.ID)
		envStr, _ := p.DB.GetPanelEnv(ctx, a.ID)
		pvCfg := p.previewConfig(ctx, a.ID)
		appPorts, _ := p.DB.GetAppAllocatedPorts(ctx, a.ID)

		appList = append(appList, appSummary{
			App:           a,
			Domains:       doms,
			Services:      svcs,
			GitConfig:     gitPtr,
			DeployLogs:    dlogs,
			Collaborators: collabs,
			Volumes:       appVols,
			BackupHistory: bHist,
			Schedules:     bSched,
			EnvContent:    envStr,
			Preview:       pvCfg,
			PreviewBase:   previewHostBase(a, pvCfg, sys.PublicIP),
			Ports:         appPorts,
		})
	}

	containers, dockerErr := dockerapi.ListContainers(ctx)
	images, _ := dockerapi.ListImages(ctx)
	var ownerPtr *int64
	if !isAdmin {
		ownerPtr = &u.ID
	}
	gitProviders, _ := p.DB.ListGitProviders(ctx, ownerPtr)
	ghDetails, _ := p.DB.ListGitHubProviderDetails(ctx)
	ghDetailMap := make(map[int64]db.GitHubProviderDetail)
	for _, d := range ghDetails {
		ghDetailMap[d.ProviderID] = d
	}

	type spaGitProviderItem struct {
		db.GitProvider
		AuthMethod      string `json:"auth_method"`
		InstallationID  string `json:"installation_id,omitempty"`
		AppSlug         string `json:"app_slug,omitempty"`
		AccountLogin    string `json:"account_login,omitempty"`
		InstallURL      string `json:"install_url,omitempty"`
		NeedsInstall    bool   `json:"needs_install,omitempty"`
		ReposAccessible int    `json:"repos_accessible"`
	}
	var enrichedGitProviders []spaGitProviderItem
	for _, gp := range gitProviders {
		it := spaGitProviderItem{GitProvider: gp}
		d := ghDetailMap[gp.ID]
		if gp.Provider == "github" {
			if d.GitHubAppID != "" || d.AppSlug != "" {
				it.AuthMethod = "GitHub App"
				it.InstallationID = d.InstallationID
				it.AppSlug = d.AppSlug
				it.AccountLogin = d.AccountLogin
				if strings.TrimSpace(d.InstallationID) == "" {
					it.NeedsInstall = true
				}
				it.InstallURL = "/git/" + strconv.FormatInt(gp.ID, 10) + "/github/install"
			} else {
				it.AuthMethod = "Fine-Grained PAT"
			}
		} else if gp.Provider == "gitlab" {
			if gp.RefreshToken != "" || gp.ExpiresAt > 0 {
				it.AuthMethod = "OAuth 2.0"
			} else {
				it.AuthMethod = "Deploy Token"
			}
		} else {
			it.AuthMethod = "Token"
		}
		if !it.NeedsInstall {
			it.ReposAccessible = p.fetchAccessibleReposCount(ctx, gp, d)
		}
		enrichedGitProviders = append(enrichedGitProviders, it)
	}
	webhookDeliveries, _ := p.DB.ListWebhookDeliveries(ctx, 10)
	registries, _ := p.DB.ListPrivateRegistries(ctx, ownerPtr)
	backupDests, _ := p.DB.ListBackupDestinations(ctx, ownerPtr)
	apiTokens, _ := p.DB.ListAPITokensForUser(ctx, u.ID)

	var users []db.User
	var cliSessions []db.CLISession
	var auditLogs []db.AuditLog
	var settings map[string]string
	var caddyCfg map[string]string
	var caddyUp bool
	var caddyStatus string
	var migrateExports []db.MigrateExport

	if isAdmin {
		users, _ = p.DB.ListUsers(ctx)
		cliSessions, _ = p.DB.ListCLISessions()
		auditLogs, _ = p.DB.ListAuditLogs(ctx, 100, 0)
		settings, _ = p.DB.GetAllSettings(ctx)
		caddyCfg, _ = p.DB.GetAllCaddyConfig(ctx)
		adminAPI := strings.TrimSpace(caddyCfg["admin_api"])
		if adminAPI == "" {
			adminAPI = "http://caddy:2019"
		}
		caddyUp, caddyStatus = caddy.AdminStatus(ctx, adminAPI)
		if !caddyUp && (strings.Contains(caddyStatus, "lookup caddy") || strings.Contains(caddyStatus, "dial tcp") || strings.Contains(caddyStatus, "server misbehaving") || strings.Contains(caddyStatus, "no such host")) {
			if err := dockerapi.EnsureCaddySharedNetwork(ctx); err == nil {
				caddyUp, caddyStatus = caddy.AdminStatus(ctx, adminAPI)
			}
		}
		migrateExports, _ = p.DB.ListRecentMigrateExports(ctx, 8)
	}

	usedStorage := p.UserStorageBytes(ctx, u.ID)

	return c.JSON(fiber.Map{
		"ok": true,
		"user": fiber.Map{
			"id":                       u.ID,
			"username":                 u.Username,
			"role":                     u.Role,
			"status":                   u.Status,
			"max_apps":                 u.MaxApps,
			"max_memory_mb":            u.MaxMemoryMB,
			"max_cpus":                 u.MaxCPUs,
			"max_storage_mb":           u.MaxStorageMB,
			"bandwidth_limit_mbps":     u.BandwidthLimitMbps,
			"allow_domain_file_server": u.AllowDomainFileServer,
			"storage_used_bytes":       usedStorage,
			"storage_used_human":       HumanStorage(usedStorage),
		},
		"sys":                 sys,
		"docker_error":        dockerErr,
		"apps":                appList,
		"containers":          containers,
		"images":              images,
		"volumes":             allVolNames,
		"git_providers":       enrichedGitProviders,
		"webhook_deliveries":  webhookDeliveries,
		"registries":          registries,
		"backup_destinations": backupDests,
		"api_tokens":          apiTokens,
		"cli_sessions":        cliSessions,
		"users":               users,
		"audit_logs":          auditLogs,
		"settings":            settings,
		"migrate_exports":     migrateExports,
		"caddy": fiber.Map{
			"up":     caddyUp,
			"status": caddyStatus,
			"config": caddyCfg,
		},
		"cgroup": fiber.Map{
			"mode":      p.cgroupMode(),
			"supported": p.cgroupMode() != "",
		},
		"qos": fiber.Map{
			"wan_interface": func() string {
				if p.QoS != nil {
					return p.QoS.WANInterface()
				}
				return ""
			}(),
			"enabled": p.QoS != nil,
		},
		"mcp_enabled":       p.DB.GetSetting(ctx, "mcp_enabled") != "0",
		"telemetry_history": sysinfo.GetTelemetryHistory(),
		"public_ip":         sys.PublicIP,
		"server_time":       time.Now().UTC().Format(time.RFC3339),
	})
}

// MetricsHistory returns the time-series ring buffer of CPU, RAM, Disk and Net metrics.
// GET /api/v1/metrics/history
func (p *Panel) MetricsHistory(c *fiber.Ctx) error {
	history := sysinfo.GetTelemetryHistory()
	return c.JSON(fiber.Map{
		"ok":      true,
		"history": history,
	})
}
