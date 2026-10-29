package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/db"

	"golang.org/x/sync/errgroup"
)

type ExportDeps struct {
	DB             *db.Store
	WorkspaceRoot  func(appID string) string
	VolumeNames    func(ctx context.Context, app db.App) ([]string, error)
	SourcePanelURL string
	AppendLog      func(exportID int64, msg string)
	QuiescePanel   func(ctx context.Context, log func(string)) (restore func(context.Context), err error)
	PrepareApp     func(ctx context.Context, app db.App, logf func(string)) (cleanup func(), err error)
}

type exportAppMeta struct {
	snap         AppSnapshot
	domains      []DomainSnapshot
	gitConfigs   []GitSnapshot
	panelEnv     string
	phpSite      *PHPSiteSnapshot
	phpDatabases []PHPDatabaseSnapshot
	phpCronJobs  []PHPCronJobSnapshot
}

func RunExport(ctx context.Context, exportID int64, appIDs []string, deps ExportDeps) (plainToken string, err error) {
	if deps.DB == nil {
		return "", fmt.Errorf("nil database")
	}
	if len(appIDs) == 0 {
		return "", fmt.Errorf("no apps selected")
	}

	workDir := ExportWorkDir(exportID)
	_ = os.RemoveAll(workDir)
	if err := os.MkdirAll(filepath.Join(workDir, appsDirName), 0700); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(workDir)
		}
	}()

	log := newSafeLogger(func(msg string) {
		if deps.AppendLog != nil {
			deps.AppendLog(exportID, msg)
		}
	})

	var restorePanel func(context.Context)
	var restoreOnce sync.Once
	runRestore := func(msg string) {
		restoreOnce.Do(func() {
			if restorePanel == nil {
				return
			}
			if msg != "" {
				log.log(msg)
			}
			restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			restorePanel(restoreCtx)
		})
	}
	if deps.QuiescePanel != nil {
		log.log("quiescing panel before export")
		var qerr error
		restorePanel, qerr = deps.QuiescePanel(ctx, log.log)
		if qerr != nil {
			return "", fmt.Errorf("quiesce panel: %w", qerr)
		}
		defer func() {
			if err != nil {
				runRestore("restoring apps after failed export")
			}
		}()
	}

	apps := make([]db.App, 0, len(appIDs))
	for _, appID := range appIDs {
		app, gerr := deps.DB.GetApp(ctx, appID)
		if gerr != nil {
			return "", fmt.Errorf("app %s: %w", appID, gerr)
		}
		apps = append(apps, app)
	}

	meta := make([]exportAppMeta, len(apps))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(ParallelWorkers())

	for i, app := range apps {
		i, app := i, app
		g.Go(func() error {
			log.log("archiving " + app.Name + " (" + app.ID + ")")
			if deps.PrepareApp != nil {
				cleanup, perr := deps.PrepareApp(gctx, app, func(msg string) {
					log.log(app.Name + ": " + msg)
				})
				if perr != nil {
					return fmt.Errorf("prepare %s: %w", app.Name, perr)
				}
				if cleanup != nil {
					defer cleanup()
				}
			}
			sourceDir := deps.WorkspaceRoot(app.ID)
			vols, verr := deps.VolumeNames(gctx, app)
			if verr != nil {
				return verr
			}
			destPath := filepath.Join(workDir, appsDirName, app.ID+".tar.gz")
			if err := exportAppArchive(gctx, app.Name, sourceDir, vols, destPath, func(msg string) {
				log.log(app.Name + ": " + msg)
			}); err != nil {
				return fmt.Errorf("archive %s: %w", app.Name, err)
			}

			srcType := deps.DB.GetAppSourceType(gctx, app.ID)
			meta[i].snap = AppSnapshot{
				ID:          app.ID,
				Name:        app.Name,
				ComposeFile: app.ComposeFile,
				OwnerID:     app.OwnerID,
				Status:      app.Status,
				SourceType:  srcType,
				Archive:     filepath.ToSlash(filepath.Join(appsDirName, app.ID+".tar.gz")),
			}
			if deps.DB.IsPHPSite(gctx, app.ID) {
				if site, serr := deps.DB.GetPHPSite(gctx, app.ID); serr == nil {
					meta[i].phpSite = &PHPSiteSnapshot{
						AppID:        site.AppID,
						OwnerID:      site.OwnerID,
						UID:          site.UID,
						SiteType:     site.SiteType,
						PHPVersion:   site.PHPVersion,
						RootDir:      site.RootDir,
						SettingsJSON: site.SettingsJSON,
						CreatedAt:    site.CreatedAt.UTC().Format(time.RFC3339),
						SecurityJSON: deps.DB.GetPHPSecurityJSON(gctx, app.ID),
					}
				}
				if dbs, derr := deps.DB.ListPHPDatabases(gctx, app.ID); derr == nil {
					for _, d := range dbs {
						meta[i].phpDatabases = append(meta[i].phpDatabases, PHPDatabaseSnapshot{
							AppID:     d.AppID,
							DBName:    d.DBName,
							DBUser:    d.DBUser,
							CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
						})
					}
				}
				if crons, cerr := deps.DB.ListPHPCronJobs(gctx, app.ID); cerr == nil {
					for _, c := range crons {
						meta[i].phpCronJobs = append(meta[i].phpCronJobs, PHPCronJobSnapshot{
							AppID:      c.AppID,
							Schedule:   c.Schedule,
							Command:    c.Command,
							Enabled:    c.Enabled,
							LastRunAt:  c.LastRunAt,
							LastStatus: c.LastStatus,
							LastOutput: c.LastOutput,
							CreatedAt:  c.CreatedAt.UTC().Format(time.RFC3339),
						})
					}
				}
			}
			domains, _ := deps.DB.ListAppDomains(gctx, app.ID)
			for _, d := range domains {
				meta[i].domains = append(meta[i].domains, DomainSnapshot{
					AppID: d.AppID, Domain: d.Domain, Service: d.Service, Port: d.Port,
					EnableHTTPS: d.EnableHTTPS, EnableWWW: d.EnableWWW,
					ServeStatic: d.ServeStatic, StaticPath: d.StaticPath, StaticURLPrefix: d.StaticURLPrefix,
					ServeMedia: d.ServeMedia, MediaPath: d.MediaPath, MediaURLPrefix: d.MediaURLPrefix,
					RouteRulesJSON: d.RouteRulesJSON,
				})
			}
			if env, eerr := deps.DB.GetPanelEnv(gctx, app.ID); eerr == nil && strings.TrimSpace(env) != "" {
				meta[i].panelEnv = env
			}
			if gitCfg, gerr := deps.DB.GetAppGitConfig(gctx, app.ID); gerr == nil && strings.TrimSpace(gitCfg.RepoURL) != "" {
				meta[i].gitConfigs = append(meta[i].gitConfigs, GitSnapshot{
					AppID: gitCfg.AppID, GitProviderID: gitCfg.GitProviderID, Provider: gitCfg.Provider,
					RepoURL: gitCfg.RepoURL, RepoFullName: gitCfg.RepoFullName, Branch: gitCfg.Branch,
					AuthMode: gitCfg.AuthMode, Token: gitCfg.Token, AppGitID: gitCfg.AppGitID,
					InstallationID: gitCfg.InstallationID, PrivateKeyPEM: gitCfg.PrivateKeyPEM,
					WebhookSecret: gitCfg.WebhookSecret, AutoDeploy: gitCfg.AutoDeploy, SyncPaused: gitCfg.SyncPaused, LastDeployRef: gitCfg.LastDeployRef,
				})
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return "", err
	}

	runRestore("restarting apps (bundle packing continues)")

	snap := PanelSnapshot{PanelEnvs: map[string]string{}, SourcePanel: deps.SourcePanelURL}
	manifest := newBundleManifest(exportID, appIDs)
	ownerSet := map[int64]struct{}{}
	for _, m := range meta {
		snap.Apps = append(snap.Apps, m.snap)
		ownerSet[m.snap.OwnerID] = struct{}{}
		snap.Domains = append(snap.Domains, m.domains...)
		snap.GitConfigs = append(snap.GitConfigs, m.gitConfigs...)
		if strings.TrimSpace(m.panelEnv) != "" {
			snap.PanelEnvs[m.snap.ID] = m.panelEnv
		}
		if m.phpSite != nil {
			snap.PHPSites = append(snap.PHPSites, *m.phpSite)
		}
		snap.PHPDatabases = append(snap.PHPDatabases, m.phpDatabases...)
		snap.PHPCronJobs = append(snap.PHPCronJobs, m.phpCronJobs...)
	}

	users, uerr := deps.DB.ListUsers(ctx)
	if uerr == nil {
		for _, u := range users {
			snap.Users = append(snap.Users, userToSnapshot(u))
		}
	}

	providerSeen := map[int64]struct{}{}
	for _, appID := range appIDs {
		collabs, _ := deps.DB.ListCollaborators(ctx, appID)
		for _, c := range collabs {
			snap.Collaborators = append(snap.Collaborators, CollaboratorSnapshot{
				AppID: c.AppID, UserID: c.UserID, Role: c.Role,
			})
		}
		for _, g := range snap.GitConfigs {
			if g.AppID != appID || g.GitProviderID <= 0 {
				continue
			}
			providerSeen[g.GitProviderID] = struct{}{}
		}
	}
	for pid := range providerSeen {
		p, perr := deps.DB.GetGitProvider(ctx, pid)
		if perr != nil {
			continue
		}
		ps := GitProviderSnapshot{
			ID: p.ID, UserID: p.UserID, Name: p.Name, Provider: p.Provider,
			Token: p.Token, RefreshToken: p.RefreshToken, ExpiresAt: p.ExpiresAt, Notes: p.Notes,
			CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if d, derr := deps.DB.GetGitHubProviderDetail(ctx, pid); derr == nil {
			ps.GitHubDetail = &GitHubProviderSnap{
				GitHubAppID: d.GitHubAppID, ClientID: d.ClientID, ClientSecret: d.ClientSecret,
				PrivateKeyPEM: d.PrivateKeyPEM, WebhookSecret: d.WebhookSecret,
				InstallationID: d.InstallationID, AccountLogin: d.AccountLogin, AppSlug: d.AppSlug,
				ManifestState: d.ManifestState, CreatedViaManifest: d.CreatedViaManifest,
			}
		}
		snap.GitProviders = append(snap.GitProviders, ps)
	}

	for ownerID := range ownerSet {
		regs, _ := deps.DB.ListPrivateRegistries(ctx, &ownerID)
		for _, r := range regs {
			snap.Registries = append(snap.Registries, RegistrySnapshot{
				UserID: r.UserID, Name: r.Name, ServerAddress: r.ServerAddress,
				Username: r.Username, PasswordEncrypted: r.PasswordEncrypted,
			})
		}
	}

	sb, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workDir, snapshotName), sb, 0600); err != nil {
		return "", err
	}

	bundlePath := filepath.Join(StagingRoot(), bundleFileName())
	log.log("packing bundle")
	if err := packBundle(ctx, workDir, bundlePath, manifest); err != nil {
		return "", err
	}
	_ = os.RemoveAll(workDir)

	st, err := os.Stat(bundlePath)
	if err != nil {
		return "", err
	}

	plain, hash, err := NewDownloadToken()
	if err != nil {
		_ = os.Remove(bundlePath)
		return "", err
	}
	expires := time.Now().UTC().Add(LinkTTL)
	if err := deps.DB.UpdateMigrateExportReady(ctx, exportID, bundlePath, st.Size(), plain, hash, expires); err != nil {
		_ = os.Remove(bundlePath)
		return "", err
	}
	log.log("export ready — previous bundles will be replaced")
	return plain, nil
}
