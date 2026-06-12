package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/backup"
	"panel/internal/db"
	"panel/internal/dockerx"
	"panel/internal/workspace"
)

type ImportDeps struct {
	DB                *db.Store
	Store             *workspace.Store
	AdminOwnerID      int64
	WorkspaceRoot     func(appID string) string
	ComposeFilePath   func(app db.App) string
	ComposePaths      func(app db.App) []string
	ProjectName       func(app db.App) string
	EnvFiles          func(appID string) []string
	AfterAppImported  func(ctx context.Context, app db.App) error
	OnProgress        func(msg string)
	DeployAfterImport bool
}

func RunImport(ctx context.Context, bundlePath string, deleteAfter bool, deps ImportDeps) error {
	if deps.DB == nil || deps.Store == nil {
		return fmt.Errorf("missing dependencies")
	}
	if _, err := os.Stat(bundlePath); err != nil {
		return fmt.Errorf("bundle not found: %w", err)
	}

	emit := func(msg string) {
		if deps.OnProgress != nil {
			deps.OnProgress(msg)
		}
	}

	workDir := filepath.Join(StagingRoot(), "import-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	emit("extracting bundle")
	if err := extractBundle(ctx, bundlePath, workDir); err != nil {
		return err
	}
	if _, err := readManifestFromDir(workDir); err != nil {
		return err
	}
	snap, err := readSnapshotFromDir(workDir)
	if err != nil {
		return err
	}

	registrySeen := map[string]struct{}{}
	for _, r := range snap.Registries {
		key := r.ServerAddress + "|" + r.Username
		if _, ok := registrySeen[key]; ok {
			continue
		}
		registrySeen[key] = struct{}{}
		id := deps.AdminOwnerID
		uid := &id
		_, _ = deps.DB.AddPrivateRegistry(ctx, db.PrivateRegistry{
			UserID: uid, Name: r.Name, ServerAddress: r.ServerAddress,
			Username: r.Username, PasswordEncrypted: r.PasswordEncrypted,
			CreatedAt: time.Now().UTC(),
		})
	}

	for _, as := range snap.Apps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		emit("importing " + as.Name)
		if _, err := deps.DB.GetApp(ctx, as.ID); err == nil {
			return fmt.Errorf("app %s already exists; remove it or use a fresh panel", as.ID)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := os.MkdirAll(deps.Store.Path(as.ID), 0750); err != nil {
			return err
		}
		if err := deps.Store.WriteMeta(as.ID, as.Name); err != nil {
			return err
		}
		if err := deps.DB.CreateApp(ctx, as.ID, as.Name, deps.AdminOwnerID); err != nil {
			_ = os.RemoveAll(deps.Store.Path(as.ID))
			return err
		}
		if as.ComposeFile != "" && as.ComposeFile != "docker-compose.yml" {
			_ = deps.DB.UpdateComposeFile(ctx, as.ID, as.ComposeFile)
		}
		if strings.TrimSpace(as.SourceType) != "" {
			_ = deps.DB.SetAppSourceType(ctx, as.ID, as.SourceType)
		}

		archivePath := filepath.Join(workDir, filepath.FromSlash(as.Archive))
		app := db.App{ID: as.ID, Name: as.Name, ComposeFile: as.ComposeFile, OwnerID: deps.AdminOwnerID}
		composePath := deps.ComposeFilePath(app)
		workspaceRoot := deps.WorkspaceRoot(as.ID)
		if err := backup.RestoreFullWithVolumes(ctx, as.Name, composePath, workspaceRoot, archivePath, func(msg string) {
			emit(as.Name + ": " + msg)
		}); err != nil {
			return fmt.Errorf("restore %s: %w", as.Name, err)
		}

		if env, ok := snap.PanelEnvs[as.ID]; ok && strings.TrimSpace(env) != "" {
			_ = deps.DB.UpdatePanelEnv(ctx, as.ID, env)
		}
		for _, d := range snap.Domains {
			if d.AppID != as.ID {
				continue
			}
			_, _ = deps.DB.CreateAppDomain(ctx, db.AppDomain{
				AppID: d.AppID, Domain: d.Domain, Service: d.Service, Port: d.Port,
				EnableHTTPS: d.EnableHTTPS, EnableWWW: d.EnableWWW,
				ServeStatic: d.ServeStatic, StaticPath: d.StaticPath, StaticURLPrefix: d.StaticURLPrefix,
				ServeMedia: d.ServeMedia, MediaPath: d.MediaPath, MediaURLPrefix: d.MediaURLPrefix,
				RouteRulesJSON: d.RouteRulesJSON,
				CreatedAt: time.Now().UTC(),
			})
		}
		for _, g := range snap.GitConfigs {
			if g.AppID != as.ID {
				continue
			}
			_ = deps.DB.UpsertAppGitConfig(ctx, db.AppGitConfig{
				AppID: g.AppID, GitProviderID: g.GitProviderID, Provider: g.Provider,
				RepoURL: g.RepoURL, RepoFullName: g.RepoFullName, Branch: g.Branch,
				AuthMode: g.AuthMode, Token: g.Token, AppGitID: g.AppGitID,
				InstallationID: g.InstallationID, PrivateKeyPEM: g.PrivateKeyPEM,
				WebhookSecret: g.WebhookSecret, AutoDeploy: g.AutoDeploy, LastDeployRef: g.LastDeployRef,
				CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			})
		}
		if deps.AfterAppImported != nil {
			if err := deps.AfterAppImported(ctx, app); err != nil {
				emit("post-import warning: " + err.Error())
			}
		}
		if deps.DeployAfterImport {
			dir := deps.WorkspaceRoot(as.ID)
			paths := deps.ComposePaths(app)
			project := deps.ProjectName(app)
			res := dockerx.ComposeUp(ctx, dir, paths, project, nil, deps.EnvFiles(as.ID))
			if !res.OK {
				emit("deploy " + as.Name + ": " + res.Output)
			} else {
				emit("deployed " + as.Name)
			}
		}
	}

	if deleteAfter {
		_ = os.Remove(bundlePath)
	}
	emit("import complete")
	return nil
}
