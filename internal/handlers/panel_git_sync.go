package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"panel/internal/db"
	"panel/internal/gitx"
	"panel/internal/handlers/utils"

	"github.com/gofiber/fiber/v2"
)

// ErrGitSyncPaused is returned when a Git pull is attempted for an app whose Git sync is switched off.
var ErrGitSyncPaused = errors.New("git sync is off for this app (deploys use the workspace files); turn it on first")

// GitSyncStatus describes which source an app deploys from.
type GitSyncStatus struct {
	AppID          string `json:"app_id"`
	Configured     bool   `json:"configured"`      // a repository is stored for the app
	Active         bool   `json:"active"`          // Git is the deploy source (pull on deploy / webhook)
	Paused         bool   `json:"paused"`          // repository stored but not used
	Source         string `json:"source"`          // "git" or "files"
	RepoURL        string `json:"repo_url"`        // credentials stripped
	Branch         string `json:"branch"`          //
	CheckoutExists bool   `json:"checkout_exists"` //
	CheckoutDirty  bool   `json:"checkout_dirty"`  // uncommitted edits in the Git checkout
}

// GitSyncResult is the outcome of turning Git sync on or off.
type GitSyncResult struct {
	Status      GitSyncStatus `json:"status"`
	Changed     bool          `json:"changed"`
	Copied      int           `json:"files_copied"`
	KeptNewer   int           `json:"files_kept_newer_in_workspace"`
	Unchanged   int           `json:"files_unchanged"`
	Notes       []string      `json:"notes,omitempty"`
	NextStepMsg string        `json:"next_step,omitempty"`
}

// GitSyncStatus reports the current source mode for an app.
func (p *Panel) GitSyncStatus(ctx context.Context, appID string) GitSyncStatus {
	st := GitSyncStatus{AppID: appID, Source: "files"}
	cfg, err := p.DB.GetAppGitConfig(ctx, appID)
	if err != nil {
		if p.DB.GetAppSourceType(ctx, appID) == "git" {
			st.Active, st.Source = true, "git"
		}
		return st
	}
	st.Configured = true
	st.Paused = cfg.SyncPaused
	st.Active = !cfg.SyncPaused
	if st.Active {
		st.Source = "git"
	}
	st.RepoURL = gitx.SafeRepoURL(cfg.RepoURL)
	st.Branch = cfg.Branch
	repoDir := p.AppCheckoutPath(appID)
	st.CheckoutExists = gitx.RepoExists(repoDir)
	if st.CheckoutExists {
		st.CheckoutDirty = gitx.IsDirty(ctx, repoDir)
	}
	return st
}

// SetGitSync turns Git sync on or off without touching the stored repository configuration.
//
// Off: the workspace becomes the single deploy source. Files from the Git checkout that are missing from the workspace
// (or older there) are copied over once so nothing deployed so far is lost; newer workspace files always win.
// On: the next deploy/webhook pulls from the remote again. Refused while the checkout has uncommitted edits unless force.
func (p *Panel) SetGitSync(ctx context.Context, appID string, enabled, force bool) (GitSyncResult, error) {
	var res GitSyncResult
	cfg, err := p.DB.GetAppGitConfig(ctx, appID)
	if err != nil {
		return res, errors.New("no Git repository is configured for this app; connect one in the panel (Git tab) first")
	}

	v, _ := p.ComposeMu.LoadOrStore(appID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	if !mu.TryLock() {
		return res, errors.New("a deploy or restart is in progress for this app; try again when it finishes")
	}
	defer mu.Unlock()
	if run := p.GetDeployRun(appID); run != nil {
		run.Mu.Lock()
		running := run.Running
		run.Mu.Unlock()
		if running {
			return res, errors.New("a deployment job is still running for this app; try again when it finishes")
		}
	}

	repoDir := p.AppCheckoutPath(appID)
	repoExists := gitx.RepoExists(repoDir)

	if enabled == !cfg.SyncPaused {
		res.Status = p.GitSyncStatus(ctx, appID)
		if enabled {
			res.Notes = append(res.Notes, "Git sync was already on; nothing changed.")
		} else {
			res.Notes = append(res.Notes, "Git sync was already off; nothing changed.")
		}
		return res, nil
	}

	if !enabled {
		if repoExists {
			copied, kept, same, cerr := copyCheckoutIntoWorkspace(repoDir, p.Store.Path(appID))
			if cerr != nil {
				return res, fmt.Errorf("could not carry the Git checkout files over to the workspace: %w", cerr)
			}
			res.Copied, res.KeptNewer, res.Unchanged = copied, kept, same
			res.Notes = append(res.Notes,
				fmt.Sprintf("Copied %d file(s) from the Git checkout into the workspace; kept %d newer workspace file(s); %d already identical.", copied, kept, same))
		} else {
			res.Notes = append(res.Notes, "No Git checkout exists yet, so there was nothing to copy; the workspace is used as is.")
		}
		if err := p.DB.SetAppGitSyncPaused(ctx, appID, true); err != nil {
			return res, err
		}
		_ = p.DB.SetAppSourceType(ctx, appID, "files")
		res.NextStepMsg = "Git sync is off. `nd push` and file edits now apply to the workspace; run `nd deploy` to build from it. Webhooks are ignored."
	} else {
		if repoExists && !force && gitx.IsDirty(ctx, repoDir) {
			return res, errors.New("the Git checkout has uncommitted local edits that the next Git sync would discard; " +
				"turn Git sync on with force to confirm (workspace files are kept but no longer deployed)")
		}
		if err := p.DB.SetAppGitSyncPaused(ctx, appID, false); err != nil {
			return res, err
		}
		_ = p.DB.SetAppSourceType(ctx, appID, "git")
		res.Notes = append(res.Notes, "Workspace files are kept on disk but are no longer deployed while Git sync is on.")
		res.NextStepMsg = "Git sync is on. The next deploy (or webhook push) pulls " + cfg.Branch + " from the repository."
	}

	res.Changed = true
	p.InvalidateAfterAppWorkspaceChange(appID)
	_ = p.SyncAppCaddyOverrideCtx(ctx, appID)
	res.Status = p.GitSyncStatus(ctx, appID)
	return res, nil
}

// copyCheckoutIntoWorkspace copies regular files from the Git checkout into the workspace.
// It never deletes anything and never overwrites a workspace file that is newer than the checkout copy.
// The .git directory and the panel-managed root .env are skipped.
func copyCheckoutIntoWorkspace(repoDir, wsDir string) (copied, keptNewer, unchanged int, err error) {
	if err = os.MkdirAll(wsDir, 0o750); err != nil {
		return
	}
	err = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(repoDir, path)
		if rerr != nil || rel == "." {
			return rerr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(wsDir, rel), 0o750)
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and special files are not carried over
		}
		if rel == ".env" {
			return nil
		}
		dst := filepath.Join(wsDir, rel)
		srcInfo, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if dstInfo, serr := os.Stat(dst); serr == nil {
			if dstInfo.IsDir() {
				return nil
			}
			if same, _ := sameFileContent(path, dst, srcInfo, dstInfo); same {
				unchanged++
				return nil
			}
			if !dstInfo.ModTime().Before(srcInfo.ModTime()) {
				keptNewer++
				return nil
			}
		}
		if cerr := copyFilePreserve(path, dst, srcInfo); cerr != nil {
			return cerr
		}
		copied++
		return nil
	})
	return
}

func sameFileContent(a, b string, ai, bi os.FileInfo) (bool, error) {
	if ai.Size() != bi.Size() {
		return false, nil
	}
	fa, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	fb, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(fa, fb), nil
}

func copyFilePreserve(src, dst string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".nd-copy-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err = out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// AppGitSyncToggle handles the overview-tab button: POST /apps/:id/git-sync (enabled=1|0, force=1).
func (p *Panel) AppGitSyncToggle(c *fiber.Ctx) error {
	id := c.Params("id")
	back := "/apps/" + id + "?tab=overview"
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		if WantsJSON(c) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"ok": false, "error": "not found"})
		}
		return c.Status(fiber.StatusNotFound).SendString("not found")
	}
	if u, ok := currentUser(c); ok {
		allowed, aerr := p.CanAccessApp(c.UserContext(), u.ID, u.Role, id, db.CollabRoleDeveloper)
		if aerr != nil || !allowed {
			if WantsJSON(c) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"ok": false, "error": "forbidden"})
			}
			return c.Status(fiber.StatusForbidden).SendString("forbidden")
		}
	} else {
		if WantsJSON(c) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"ok": false, "error": "unauthorized"})
		}
		return c.Redirect("/login")
	}

	enabled := c.FormValue("enabled") == "1" || strings.EqualFold(c.FormValue("enabled"), "true")
	force := c.FormValue("force") == "1" || strings.EqualFold(c.FormValue("force"), "true")
	res, err := p.SetGitSync(c.UserContext(), id, enabled, force)
	if err != nil {
		if WantsJSON(c) {
			confirmNeeded := strings.Contains(err.Error(), "with force to confirm")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"ok":             false,
				"error":          err.Error(),
				"warning":        err.Error(),
				"confirm_needed": confirmNeeded,
			})
		}
		utils.SetFlashError(c, err.Error())
		return c.Redirect(back)
	}
	if res.Changed {
		action := "git_sync_off"
		flash := "gitSyncOff"
		if enabled {
			action, flash = "git_sync_on", "gitSyncOn"
		}
		p.RecordAuditLog(c, action, "app", id, strings.Join(res.Notes, " "))
		utils.SetFlash(c, flash)
	}
	if WantsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":       true,
			"git_sync": enabled,
			"result":   res,
		})
	}
	return c.Redirect(back)
}
