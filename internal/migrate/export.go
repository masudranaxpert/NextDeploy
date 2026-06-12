package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/backup"
	"panel/internal/db"
)

type ExportDeps struct {
	DB             *db.Store
	WorkspaceRoot  func(appID string) string
	VolumeNames    func(ctx context.Context, app db.App) ([]string, error)
	SourcePanelURL string
	AppendLog      func(exportID int64, msg string)
	QuiescePanel   func(ctx context.Context, log func(string)) (restore func(context.Context), err error)
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

	logf := func(msg string) {
		if deps.AppendLog != nil {
			deps.AppendLog(exportID, msg)
		}
	}

	var restorePanel func(context.Context)
	var restoreOnce sync.Once
	runRestore := func(msg string) {
		restoreOnce.Do(func() {
			if restorePanel == nil {
				return
			}
			if msg != "" {
				logf(msg)
			}
			restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			restorePanel(restoreCtx)
		})
	}
	if deps.QuiescePanel != nil {
		logf("quiescing panel before export")
		var qerr error
		restorePanel, qerr = deps.QuiescePanel(ctx, logf)
		if qerr != nil {
			return "", fmt.Errorf("quiesce panel: %w", qerr)
		}
		defer func() {
			if err != nil {
				runRestore("restoring apps after failed export")
			}
		}()
	}

	snap := PanelSnapshot{PanelEnvs: map[string]string{}, SourcePanel: deps.SourcePanelURL}
	manifest := newBundleManifest(exportID, appIDs)
	ownerSet := map[int64]struct{}{}

	for _, appID := range appIDs {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		app, gerr := deps.DB.GetApp(ctx, appID)
		if gerr != nil {
			return "", fmt.Errorf("app %s: %w", appID, gerr)
		}
		logf("archiving " + app.Name + " (" + app.ID + ")")

		sourceDir := deps.WorkspaceRoot(app.ID)
		vols, verr := deps.VolumeNames(ctx, app)
		if verr != nil {
			return "", verr
		}
		archivePath, berr := backup.BackupFullWithVolumesOptions(ctx, app.Name, sourceDir, vols, false, func(msg string) {
			logf(app.Name + ": " + msg)
		})
		if berr != nil {
			return "", fmt.Errorf("backup %s: %w", app.Name, berr)
		}

		destName := app.ID + ".tar.gz"
		destPath := filepath.Join(workDir, appsDirName, destName)
		if err := moveArchive(archivePath, destPath); err != nil {
			return "", err
		}

		srcType := deps.DB.GetAppSourceType(ctx, app.ID)
		snap.Apps = append(snap.Apps, AppSnapshot{
			ID:          app.ID,
			Name:        app.Name,
			ComposeFile: app.ComposeFile,
			OwnerID:     app.OwnerID,
			Status:      app.Status,
			SourceType:  srcType,
			Archive:     filepath.ToSlash(filepath.Join(appsDirName, destName)),
		})
		ownerSet[app.OwnerID] = struct{}{}

		domains, _ := deps.DB.ListAppDomains(ctx, app.ID)
		for _, d := range domains {
			snap.Domains = append(snap.Domains, DomainSnapshot{
				AppID: d.AppID, Domain: d.Domain, Service: d.Service, Port: d.Port,
				EnableHTTPS: d.EnableHTTPS, EnableWWW: d.EnableWWW,
				ServeStatic: d.ServeStatic, StaticPath: d.StaticPath, StaticURLPrefix: d.StaticURLPrefix,
				ServeMedia: d.ServeMedia, MediaPath: d.MediaPath, MediaURLPrefix: d.MediaURLPrefix,
				RouteRulesJSON: d.RouteRulesJSON,
			})
		}
		if env, eerr := deps.DB.GetPanelEnv(ctx, app.ID); eerr == nil && strings.TrimSpace(env) != "" {
			snap.PanelEnvs[app.ID] = env
		}
		if gitCfg, gerr := deps.DB.GetAppGitConfig(ctx, app.ID); gerr == nil && strings.TrimSpace(gitCfg.RepoURL) != "" {
			snap.GitConfigs = append(snap.GitConfigs, GitSnapshot{
				AppID: gitCfg.AppID, GitProviderID: gitCfg.GitProviderID, Provider: gitCfg.Provider,
				RepoURL: gitCfg.RepoURL, RepoFullName: gitCfg.RepoFullName, Branch: gitCfg.Branch,
				AuthMode: gitCfg.AuthMode, Token: gitCfg.Token, AppGitID: gitCfg.AppGitID,
				InstallationID: gitCfg.InstallationID, PrivateKeyPEM: gitCfg.PrivateKeyPEM,
				WebhookSecret: gitCfg.WebhookSecret, AutoDeploy: gitCfg.AutoDeploy, LastDeployRef: gitCfg.LastDeployRef,
			})
		}
	}

	runRestore("restarting apps (bundle packing continues in background)")

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
	logf("packing bundle")
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
	if err := deps.DB.UpdateMigrateExportReady(ctx, exportID, bundlePath, st.Size(), hash, expires); err != nil {
		_ = os.Remove(bundlePath)
		return "", err
	}
	logf("export ready — previous bundles will be replaced")
	return plain, nil
}

func moveArchive(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
