package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/workspace"

	"golang.org/x/sync/errgroup"
)

type ImportDeps struct {
	DB                *db.Store
	Store             *workspace.Store
	AdminOwnerID      int64
	WorkspaceRoot     func(appID string) string
	ComposePaths      func(app db.App) []string
	ProjectName       func(app db.App) string
	EnvFiles          func(appID string) []string
	AfterAppImported  func(ctx context.Context, app db.App) error
	OnProgress        func(msg string)
	DeployAfterImport bool
}

type importAppJob struct {
	snap    AppSnapshot
	app     db.App
	ownerID int64
}

func RunImport(ctx context.Context, bundlePath string, deleteAfter bool, deps ImportDeps) error {
	if deps.DB == nil || deps.Store == nil {
		return fmt.Errorf("missing dependencies")
	}
	if _, err := os.Stat(bundlePath); err != nil {
		return fmt.Errorf("bundle not found: %w", err)
	}

	log := newSafeLogger(deps.OnProgress)

	workDir := filepath.Join(StagingRoot(), "import-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	log.log("reading bundle metadata")
	if err := extractBundleMembers(ctx, bundlePath, workDir, []string{manifestName, snapshotName}); err != nil {
		return err
	}
	if _, err := readManifestFromDir(workDir); err != nil {
		return err
	}
	snap, err := readSnapshotFromDir(workDir)
	if err != nil {
		return err
	}

	if !snapshotHasUsers(snap) {
		return fmt.Errorf("bundle has no user accounts; re-export from the source panel with the latest NextDeploy version")
	}
	log.log("resetting panel (users, apps, workspaces)")
	if err := ResetPanelForImport(ctx, deps.DB, deps.Store.Root); err != nil {
		return fmt.Errorf("reset panel: %w", err)
	}
	log.log("restoring users")
	for _, us := range snap.Users {
		if err := deps.DB.InsertUserMigrate(ctx, snapshotToUser(us)); err != nil {
			return fmt.Errorf("import user %s: %w", us.Username, err)
		}
	}
	_ = deps.DB.SyncUsersIDSequence(ctx)
	for _, gp := range snap.GitProviders {
		p := db.GitProvider{
			ID: gp.ID, UserID: gp.UserID, Name: gp.Name, Provider: gp.Provider,
			Token: gp.Token, RefreshToken: gp.RefreshToken, ExpiresAt: gp.ExpiresAt, Notes: gp.Notes,
		}
		if t, perr := time.Parse(time.RFC3339, gp.CreatedAt); perr == nil {
			p.CreatedAt = t
		}
		if t, perr := time.Parse(time.RFC3339, gp.UpdatedAt); perr == nil {
			p.UpdatedAt = t
		}
		if err := deps.DB.InsertGitProviderMigrate(ctx, p); err != nil {
			return fmt.Errorf("import git provider %s: %w", gp.Name, err)
		}
		if gp.GitHubDetail != nil {
			d := gp.GitHubDetail
			_ = deps.DB.UpsertGitHubProviderDetail(ctx, db.GitHubProviderDetail{
				ProviderID: gp.ID, GitHubAppID: d.GitHubAppID, ClientID: d.ClientID,
				ClientSecret: d.ClientSecret, PrivateKeyPEM: d.PrivateKeyPEM, WebhookSecret: d.WebhookSecret,
				InstallationID: d.InstallationID, AccountLogin: d.AccountLogin, AppSlug: d.AppSlug,
				ManifestState: d.ManifestState, CreatedViaManifest: d.CreatedViaManifest,
			})
		}
	}
	if len(snap.GitProviders) > 0 {
		_ = deps.DB.SyncGitProvidersIDSequence(ctx)
	}

	gitByApp := map[string][]GitSnapshot{}
	for _, g := range snap.GitConfigs {
		gitByApp[g.AppID] = append(gitByApp[g.AppID], g)
	}
	domainsByApp := map[string][]DomainSnapshot{}
	for _, d := range snap.Domains {
		domainsByApp[d.AppID] = append(domainsByApp[d.AppID], d)
	}

	registrySeen := map[string]struct{}{}
	for _, r := range snap.Registries {
		key := r.ServerAddress + "|" + r.Username
		if _, ok := registrySeen[key]; ok {
			continue
		}
		registrySeen[key] = struct{}{}
		uid := r.UserID
		_, _ = deps.DB.AddPrivateRegistry(ctx, db.PrivateRegistry{
			UserID: uid, Name: r.Name, ServerAddress: r.ServerAddress,
			Username: r.Username, PasswordEncrypted: r.PasswordEncrypted,
			CreatedAt: time.Now().UTC(),
		})
	}

	jobs := make([]importAppJob, 0, len(snap.Apps))
	for _, as := range snap.Apps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		log.log("registering " + as.Name)
		if err := os.MkdirAll(deps.Store.Path(as.ID), 0750); err != nil {
			return err
		}
		if err := deps.Store.WriteMeta(as.ID, as.Name); err != nil {
			return err
		}
		ownerID := as.OwnerID
		if ownerID <= 0 {
			ownerID = deps.AdminOwnerID
		}
		if err := deps.DB.CreateApp(ctx, as.ID, as.Name, ownerID); err != nil {
			_ = os.RemoveAll(deps.Store.Path(as.ID))
			return err
		}
		if as.ComposeFile != "" && as.ComposeFile != "docker-compose.yml" {
			_ = deps.DB.UpdateComposeFile(ctx, as.ID, as.ComposeFile)
		}
		if strings.TrimSpace(as.SourceType) != "" {
			_ = deps.DB.SetAppSourceType(ctx, as.ID, as.SourceType)
		}
		for _, g := range gitByApp[as.ID] {
			_ = deps.DB.UpsertAppGitConfig(ctx, db.AppGitConfig{
				AppID: g.AppID, GitProviderID: g.GitProviderID, Provider: g.Provider,
				RepoURL: g.RepoURL, RepoFullName: g.RepoFullName, Branch: g.Branch,
				AuthMode: g.AuthMode, Token: g.Token, AppGitID: g.AppGitID,
				InstallationID: g.InstallationID, PrivateKeyPEM: g.PrivateKeyPEM,
				WebhookSecret: g.WebhookSecret, AutoDeploy: g.AutoDeploy, LastDeployRef: g.LastDeployRef,
				CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		if env, ok := snap.PanelEnvs[as.ID]; ok && strings.TrimSpace(env) != "" {
			_ = deps.DB.UpdatePanelEnv(ctx, as.ID, env)
		}
		jobs = append(jobs, importAppJob{
			snap: as,
			app:  db.App{ID: as.ID, Name: as.Name, ComposeFile: as.ComposeFile, OwnerID: ownerID},
			ownerID: ownerID,
		})
	}

	log.log(fmt.Sprintf("restoring %d app(s) in parallel (workers=%d)", len(jobs), ParallelWorkers()))
	sem := newSemaphore(ParallelWorkers())
	g, gctx := errgroup.WithContext(ctx)
	for _, job := range jobs {
		job := job
		g.Go(func() error {
			if err := sem.acquire(gctx); err != nil {
				return err
			}
			defer sem.release()
			log.log("importing " + job.snap.Name)
			if err := ensureBundleMember(gctx, bundlePath, workDir, job.snap.Archive); err != nil {
				return fmt.Errorf("extract archive %s: %w", job.snap.Name, err)
			}
			archivePath := filepath.Join(workDir, filepath.FromSlash(job.snap.Archive))
			workspaceRoot := deps.WorkspaceRoot(job.snap.ID)
			if err := restoreAppArchive(gctx, archivePath, workspaceRoot, func(msg string) {
				log.log(job.snap.Name + ": " + msg)
			}); err != nil {
				return fmt.Errorf("restore %s: %w", job.snap.Name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	for _, job := range jobs {
		for _, d := range domainsByApp[job.snap.ID] {
			_, _ = deps.DB.CreateAppDomain(ctx, db.AppDomain{
				AppID: d.AppID, Domain: d.Domain, Service: d.Service, Port: d.Port,
				EnableHTTPS: d.EnableHTTPS, EnableWWW: d.EnableWWW,
				ServeStatic: d.ServeStatic, StaticPath: d.StaticPath, StaticURLPrefix: d.StaticURLPrefix,
				ServeMedia: d.ServeMedia, MediaPath: d.MediaPath, MediaURLPrefix: d.MediaURLPrefix,
				RouteRulesJSON: d.RouteRulesJSON,
				CreatedAt: time.Now().UTC(),
			})
		}
		if deps.AfterAppImported != nil {
			if err := deps.AfterAppImported(ctx, job.app); err != nil {
				log.log("post-import warning: " + err.Error())
			}
		}
	}

	for _, c := range snap.Collaborators {
		_ = deps.DB.AddCollaborator(ctx, c.AppID, c.UserID, c.Role)
	}

	if deps.DeployAfterImport && len(jobs) > 0 {
		log.log(fmt.Sprintf("deploying %d app(s) in parallel", len(jobs)))
		dg, dctx := errgroup.WithContext(ctx)
		for _, job := range jobs {
			job := job
			dg.Go(func() error {
				if err := sem.acquire(dctx); err != nil {
					return err
				}
				defer sem.release()
				dir := deps.WorkspaceRoot(job.snap.ID)
				paths := deps.ComposePaths(job.app)
				project := deps.ProjectName(job.app)
				res := dockerx.ComposeUp(dctx, dir, paths, project, nil, deps.EnvFiles(job.snap.ID))
				if !res.OK {
					log.log("deploy " + job.snap.Name + ": " + res.Output)
				} else {
					log.log("deployed " + job.snap.Name)
				}
				return nil
			})
		}
		_ = dg.Wait()
	}

	if deleteAfter {
		_ = os.Remove(bundlePath)
	}
	log.log("import complete")
	return nil
}
