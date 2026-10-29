package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"panel/internal/perflog"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/phphosting"
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

// The slow Docker lookups every page load needs. They are shared by all users (each user's view is
// filtered afterwards) and refreshed in the background; Docker changes made by the panel clear them.
const bootTTL = 4 * time.Second

type bootContainerList struct {
	rows []dockerapi.ContainerRow
	err  string
}

var (
	bootVolumes    swrCache[[]string]
	bootContainers swrCache[bootContainerList]
	bootImages     swrCache[[]dockerapi.ImageRow]
)

// invalidateBootCaches makes the next page load fetch Docker's state again.
func invalidateBootCaches() {
	bootVolumes.invalidate()
	bootContainers.invalidate()
	bootImages.invalidate()
}

var (
	caddyStatusMu   sync.Mutex
	caddyStatusAt   time.Time
	caddyStatusUp   bool
	caddyStatusText string
)

// SPAVersion tells an open browser tab which UI build the server has now, so it can offer a reload
// after an update instead of failing on a chunk that no longer exists. GET /api/panel/version
func (p *Panel) SPAVersion(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	sum := sha256.Sum256(p.SPAIndexHTML)
	return c.JSON(fiber.Map{"build": hex.EncodeToString(sum[:6])})
}

// SPABootstrap returns a unified JSON payload for the React SPA control plane.
// GET /api/panel/bootstrap
func (p *Panel) SPABootstrap(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Set("Pragma", "no-cache")
	ctx := c.UserContext()
	u, ok := currentUser(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"ok": false, "error": "unauthorized"})
	}

	isAdmin := u.Role == db.RoleAdmin

	// Everything below that talks to Docker, the disk or the network is independent of the rest,
	// so it runs side by side instead of one after another.
	var (
		sys           sysinfo.Snapshot
		allVolNames   []string
		allProjects   []string
		matcher       *volumex.Matcher
		allContainers []dockerapi.ContainerRow
		dockerErr     string
		allImages     []dockerapi.ImageRow
		volSizes      map[string]float64
		volHumanSizes map[string]string
		usedStorage   int64
		quotaMemUsed  int64
		quotaCPUUsed  float64
		usedBreakdown StorageUsage
		dockerVersion string
		caddyCfg      map[string]string
		caddyUp       bool
		caddyStatus   string
		wg            sync.WaitGroup
	)
	t0 := time.Now()
	run := func(name string, fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := time.Now()
			fn()
			perflog.Logf("bootstrap part %s %s", name, perflog.FormatDur(time.Since(st)))
		}()
	}
	run("sys", func() { sys = sysinfo.Collect(ctx) })
	run("volumes", func() {
		allVolNames = bootVolumes.get(bootTTL, func() []string { v, _ := volumex.List(ctx); return v })
		volSizes, volHumanSizes = volumex.VolumeSizes(ctx)
	})
	run("projects", func() { allProjects = p.AllPanelComposeProjects(ctx) })
	run("matcher", func() { matcher = volumex.SharedMatcher(ctx) })
	run("containers", func() {
		r := bootContainers.get(bootTTL, func() bootContainerList {
			rows, e := dockerapi.ListContainers(ctx)
			return bootContainerList{rows, e}
		})
		allContainers, dockerErr = r.rows, r.err
	})
	run("images", func() {
		allImages = bootImages.get(bootTTL, func() []dockerapi.ImageRow { v, _ := dockerapi.ListImages(ctx); return v })
	})
	run("storage", func() { usedBreakdown = p.UserStorage(ctx, u.ID, false); usedStorage = usedBreakdown.Total() })
	if !isAdmin {
		run("usage", func() { quotaMemUsed, quotaCPUUsed = p.userLiveUsage(ctx, u) })
	}
	if isAdmin {
		run("dockerver", func() { dockerVersion = dockerapi.ServerVersion(ctx) })
		run("caddy", func() {
			caddyCfg, _ = p.DB.GetAllCaddyConfig(ctx)
			// The proxy's admin API is asked at most every 10 seconds; a page load must not wait for it.
			caddyStatusMu.Lock()
			if time.Since(caddyStatusAt) < 10*time.Second {
				caddyUp, caddyStatus = caddyStatusUp, caddyStatusText
				caddyStatusMu.Unlock()
				return
			}
			caddyStatusMu.Unlock()
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
			caddyStatusMu.Lock()
			caddyStatusUp, caddyStatusText, caddyStatusAt = caddyUp, caddyStatus, time.Now()
			caddyStatusMu.Unlock()
		})
	}

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
		PHP           *phpAppInfo             `json:"php,omitempty"`
		Usage         appUsage                `json:"usage"`
		DiskMB        int64                   `json:"disk_mb"`
		OwnerName     string                  `json:"owner_name"`
		OwnerRole     string                  `json:"owner_role"`
	}

	phpByApp := map[string]db.PHPSite{}
	phpRunning := map[string]bool{}
	if p.PHP != nil {
		run("php", func() {
			if sites, err := p.DB.ListPHPSites(ctx); err == nil && len(sites) > 0 {
				for _, s := range sites {
					phpByApp[s.AppID] = s
				}
				for _, ct := range p.PHP.ListContainers(ctx) {
					phpRunning[ct.Name] = ct.State == "running"
				}
			}
		})
	}

	wg.Wait()
	perflog.Logf("bootstrap parallel total %s", perflog.FormatDur(time.Since(t0)))
	tLoop := time.Now()

	allUsers, _ := p.DB.ListUsers(ctx)
	usageByProject := p.projectUsage(ctx)
	owners := ownerLookup(allUsers)
	appList := make([]appSummary, len(apps))
	var appWg sync.WaitGroup
	for idx, a := range apps {
		appWg.Add(1)
		go func(i int, app db.App) {
			defer appWg.Done()
			doms, _ := p.DB.ListAppDomains(ctx, app.ID)
			svcs := p.LoadComposeServices(ctx, app.ID)
			var gitPtr *db.AppGitConfig
			if gcfg, err := p.DB.GetAppGitConfig(ctx, app.ID); err == nil {
				if !isAdmin {
					// credentials never leave the server for non-admin sessions
					gcfg.Token = ""
					gcfg.PrivateKeyPEM = ""
					if app.OwnerID != u.ID {
						if collab, err := p.DB.GetCollaborator(ctx, app.ID, u.ID); err != nil || collab.Role != db.CollabRoleDeveloper {
							gcfg.WebhookSecret = ""
						}
					}
				}
				gitPtr = &gcfg
			}
			dlogs, _ := p.DB.ListDeployLogs(ctx, app.ID, 10)
			collabs, _ := p.DB.ListCollaboratorsWithUsers(ctx, app.ID)
			projCands := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
			q := p.AppVolumeQuery(ctx, app, allProjects, projCands...)
			appVols, _ := matcher.ListForAppFromNames(ctx, q, allVolNames)
			if _, isPHP := phpByApp[app.ID]; isPHP {
				appVols = []string{} // PHP sites do not use Docker volumes
			}
			bHist, _ := p.DB.ListBackupHistory(ctx, app.ID, 25)
			bSched, _ := p.DB.ListBackupSchedules(ctx, app.ID)
			envStr, _ := p.DB.GetPanelEnv(ctx, app.ID)
			if !isAdmin && app.OwnerID != u.ID {
				// a viewer can see the project but not its secrets
				if collab, err := p.DB.GetCollaborator(ctx, app.ID, u.ID); err != nil || collab.Role != db.CollabRoleDeveloper {
					envStr = ""
				}
			}
			pvCfg := p.previewConfig(ctx, app.ID)
			appPorts, _ := p.DB.GetAppAllocatedPorts(ctx, app.ID)

			var phpInfo *phpAppInfo
			if s, ok := phpByApp[app.ID]; ok {
				state := "none"
				if s.SiteType == "static" {
					state = "stopped"
					if phpRunning[phphosting.RouteContainer(s.AppID)] {
						state = "running"
					}
				} else if phpRunning[phphosting.RuntimeContainer(s.OwnerID, s.PHPVersion)] {
					state = "running"
				} else {
					state = "stopped"
				}
				phpInfo = &phpAppInfo{SiteType: s.SiteType, PHPVersion: s.PHPVersion, RootDir: s.RootDir, UID: s.UID, State: state}
			}

			appList[i] = appSummary{
				App:           app,
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
				PreviewBase: func() string {
					targetIP, _ := p.ResolveAppPublicIP(ctx, app)
					return previewHostBase(app, pvCfg, targetIP)
				}(),
				Ports:         appPorts,
				PHP:           phpInfo,
				Usage:         p.usageForApp(ctx, app, usageByProject),
				DiskMB:        p.AppStorageBytes(app.ID) / (1024 * 1024),
				OwnerName:     ownerOf(owners, app.OwnerID).Name,
				OwnerRole:     ownerOf(owners, app.OwnerID).Role,
			}
		}(idx, a)
	}
	appWg.Wait()

	containers := p.visibleContainers(ctx, u, allContainers)
	liveByID := make(map[string]appUsage, len(containers))
	for _, r := range p.usageRows(ctx) {
		liveByID[r.ID] = appUsage{MemoryMB: int64(r.MemUsage / (1024 * 1024)), CPUPercent: r.CPUPercent}
	}
	type containerWithUsage struct {
		dockerapi.ContainerRow
		CPUPercent float64 `json:"cpu_percent"`
		MemoryMB   int64   `json:"memory_mb"`
	}
	containerList := make([]containerWithUsage, len(containers))
	for i, c := range containers {
		containerList[i] = containerWithUsage{ContainerRow: c, CPUPercent: liveByID[c.ID].CPUPercent, MemoryMB: liveByID[c.ID].MemoryMB}
	}
	images := p.visibleImages(ctx, u, allImages, allContainers)
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
		it.Token, it.RefreshToken = "", "" // provider credentials stay server-side
		enrichedGitProviders = append(enrichedGitProviders, it)
	}
	webhookDeliveries := p.visibleWebhookDeliveries(ctx, u, 10)
	registries, _ := p.DB.ListPrivateRegistries(ctx, ownerPtr)
	backupDests, _ := p.DB.ListBackupDestinations(ctx, ownerPtr)
	for i := range backupDests {
		var cfg map[string]interface{}
		if json.Unmarshal([]byte(backupDests[i].Config), &cfg) == nil {
			backupDests[i].Config = string(mustMarshal(map[string]interface{}{
				"email":         cfg["email"],
				"display_name":  cfg["display_name"],
				"storage_limit": cfg["storage_limit"],
				"storage_used":  cfg["storage_used"],
				"folder_id":     cfg["folder_id"],
				"account_id":    cfg["account_id"],
				"bucket":        cfg["bucket"],
			}))
		} else {
			backupDests[i].Config = "{}"
		}
	}
	apiTokens, _ := p.DB.ListAPITokensForUser(ctx, u.ID)

	var users []db.User
	var cliSessions []db.CLISession
	var auditLogs []db.AuditLog
	var settings map[string]string
	var migrateExports []db.MigrateExport

	var userUsage map[string]userUsageRow
	if isAdmin {
		userUsage = p.allUserUsage(ctx, allUsers)
	}
	var resOwners *resourceOwners
	if isAdmin {
		users = allUsers
		volsByApp := make(map[string][]string, len(appList))
		for _, a := range appList {
			volsByApp[a.ID] = a.Volumes
		}
		ro := p.buildResourceOwners(ctx, apps, allUsers, allContainers, allImages, volsByApp, allVolNames)
		for _, gp := range gitProviders {
			ro.Providers[strconv.FormatInt(gp.ID, 10)] = scopeOwner(owners, gp.UserID)
		}
		for _, r := range registries {
			ro.Registries[strconv.FormatInt(r.ID, 10)] = scopeOwner(owners, r.UserID)
		}
		for _, d := range backupDests {
			ro.Destinations[strconv.FormatInt(d.ID, 10)] = scopeOwner(owners, d.UserID)
		}
		resOwners = &ro
		cliSessions, _ = p.DB.ListCLISessions()
		auditLogs, _ = p.DB.ListAuditLogs(ctx, 100, 0)
		settings, _ = p.DB.GetAllSettings(ctx)
		migrateExports, _ = p.DB.ListRecentMigrateExports(ctx, 8)
	} else {
		ids := make([]string, 0, len(apps))
		for _, a := range apps {
			ids = append(ids, a.ID)
		}
		auditLogs, _ = p.DB.ListAuditLogsForUser(ctx, u.ID, ids, 100, 0)
	}

	// Host-level telemetry and the raw volume list are admin territory; everyone else sees
	// only their own resources (plus the public IP, needed for DNS instructions).
	visibleVolNames := p.visibleVolumes(ctx, u, allVolNames)
	var telemetry any = sysinfo.GetTelemetryHistory()
	respSys := any(sys)
	if !isAdmin {
		telemetry = []any{}
		respSys = sysinfo.Snapshot{PublicIP: sys.PublicIP}
	}
	for i := range registries {
		registries[i].PasswordEncrypted = "" // ciphertext is not for browsers
	}

	ownedApps := 0
	for _, a := range apps {
		if a.OwnerID == u.ID {
			ownedApps++
		}
	}
	perflog.Logf("bootstrap app loop %s, total %s", perflog.FormatDur(time.Since(tLoop)), perflog.FormatDur(time.Since(t0)))

	return c.JSON(fiber.Map{
		"ok": true,
		"quota": fiber.Map{
			"unlimited":          isAdmin,
			"max_apps":           u.MaxApps,
			"apps_used":          ownedApps,
			"max_memory_mb":      u.MaxMemoryMB,
			"memory_used_mb":     quotaMemUsed,
			"max_cpus":           u.MaxCPUs,
			"cpu_used_cores":     quotaCPUUsed,
			"max_storage_mb":     u.MaxStorageMB,
			"storage_used_mb":    usedStorage / (1024 * 1024),
			"storage_used_bytes": usedStorage,
			"storage_breakdown":  usedBreakdown,
		},
		"user_usage": userUsage,
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
		"sys":                 respSys,
		"docker_error":        dockerErr,
		"apps":                appList,
		"containers":          containerList,
		"images":              images,
		"volumes":             visibleVolNames,
		"volume_sizes":        volSizes,
		"volume_human_sizes":  volHumanSizes,
		"resource_owners":     resOwners,
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
		"docker_version":      dockerVersion,
		"caddy": fiber.Map{
			"up":     caddyUp,
			"status": caddyStatus,
			"config": caddyCfg,
			"panel_domain": func() string {
				if !isAdmin {
					return ""
				}
				return strings.TrimSpace(p.DB.GetSetting(ctx, "panel_domain"))
			}(),
			"panel_enable_https": func() bool {
				if !isAdmin {
					return true
				}
				return settingBool(p.DB.GetSetting(ctx, "panel_enable_https"), true)
			}(),
			"panel_enable_www": func() bool {
				if !isAdmin {
					return false
				}
				return settingBool(p.DB.GetSetting(ctx, "panel_enable_www"), false)
			}(),
			"shared_mount_prefix": func() string {
				if !isAdmin {
					return "/mnt/shared"
				}
				pfx := strings.TrimSpace(p.DB.GetSetting(ctx, "caddy_shared_mount_prefix"))
				if pfx == "" {
					return "/mnt/shared"
				}
				return pfx
			}(),
			"shared_volumes": func() []string {
				if !isAdmin {
					return nil
				}
				raw := strings.TrimSpace(p.DB.GetSetting(ctx, "caddy_shared_volume_names"))
				if raw == "" {
					return nil
				}
				parts := strings.Split(raw, ",")
				out := make([]string, 0, len(parts))
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if part != "" {
						out = append(out, part)
					}
				}
				return out
			}(),
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
		"php":               p.phpBootstrap(ctx),
		"mcp_enabled":       p.DB.GetSetting(ctx, "mcp_enabled") == "1",
		"telemetry_history": telemetry,
		"public_ip": func() string {
			if !isAdmin {
				if node := p.ResolveUserShield(ctx, &u); node != nil && node.IP != "" {
					return node.IP
				}
			}
			return sys.PublicIP
		}(),
		"origin_ip": func() string {
			if !isAdmin {
				return ""
			}
			return sysinfo.DetectHostIP()
		}(),
		"shield_nodes": func() []db.ShieldNode {
			nodes, _ := p.DB.ListShieldNodes(ctx)
			if nodes == nil {
				return []db.ShieldNode{}
			}
			if !isAdmin {
				userShield := p.ResolveUserShield(ctx, &u)
				var filtered []db.ShieldNode
				for _, n := range nodes {
					n.Token = ""
					if userShield == nil || n.ID == userShield.ID {
						filtered = append(filtered, n)
					}
				}
				return filtered
			}
			return nodes
		}(),
		"server_time":       time.Now().UTC().Format(time.RFC3339),
	})
}

// MetricsHistory returns the time-series ring buffer of CPU, RAM, Disk and Net metrics.
// GET /api/v1/metrics/history
func (p *Panel) MetricsHistory(c *fiber.Ctx) error {
	if u, ok := currentUser(c); !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "admin role required"})
	}
	history := sysinfo.GetTelemetryHistory()
	return c.JSON(fiber.Map{
		"ok":      true,
		"history": history,
	})
}
