package handlers

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/runutil"

	"github.com/gofiber/fiber/v2"
)


func (p *Panel) ComposeUp(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Deploy", dockerx.ComposeUp)
}

func (p *Panel) ComposeDown(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stop", dockerx.ComposeDown)
}

func (p *Panel) ComposeRestart(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stack restart", dockerx.ComposeRestart)
}

func (p *Panel) ComposeRedeploy(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Redeploy (pull + up)", dockerx.ComposePullUp)
}

func (p *Panel) GlobalImageRemove(c *fiber.Ctx) error {
	imageID := strings.TrimSpace(c.FormValue("image_id"))
	if imageID == "" {
		return c.Status(400).SendString("image_id required")
	}
	p.RecordAuditLog(c, "remove_image", "image", imageID, "Removed Docker image")
	if err := dockerapi.RemoveImageByID(c.UserContext(), imageID); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/images")
}

func (p *Panel) GlobalContainerRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	p.RecordAuditLog(c, "remove_container", "container", name, "Removed Docker container")
	if err := dockerapi.RemoveContainerByName(c.UserContext(), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/containers")
}

// GlobalContainerRemoveSelected force-removes every container whose name was posted (checkboxes, same key "name").
func (p *Panel) GlobalContainerRemoveSelected(c *fiber.Ctx) error {
	var names []string
	c.Request().PostArgs().VisitAll(func(key, val []byte) {
		if string(key) != "name" {
			return
		}
		if n := strings.TrimSpace(string(val)); n != "" {
			names = append(names, n)
		}
	})
	if len(names) == 0 {
		return c.Status(400).SendString("no containers selected")
	}
	ctx := c.UserContext()
	p.RecordAuditLog(c, "remove_selected_containers", "container", strings.Join(names, ", "), "Removed selected containers")
	for _, name := range names {
		_ = dockerapi.RemoveContainerByName(ctx, name)
	}
	return c.Redirect("/containers")
}

func (p *Panel) GlobalContainerRestart(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	p.RecordAuditLog(c, "restart_container", "container", name, "Restarted Docker container")
	if err := dockerapi.RestartContainerByName(c.UserContext(), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/containers")
}

func (p *Panel) GlobalVolumeRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	p.RecordAuditLog(c, "remove_volume", "volume", name, "Removed Docker volume")
	if err := dockerapi.RemoveVolumeByName(c.UserContext(), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/volumes")
}

// GlobalImagePrune removes all unused (dangling) images.
func (p *Panel) GlobalImagePrune(c *fiber.Ctx) error {
	p.RecordAuditLog(c, "prune_images", "system", "docker", "Pruned unused Docker images")
	if err := dockerapi.PruneImages(c.UserContext()); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/images")
}

// GlobalContainerPrune removes all stopped containers.
func (p *Panel) GlobalContainerPrune(c *fiber.Ctx) error {
	p.RecordAuditLog(c, "prune_containers", "system", "docker", "Pruned stopped Docker containers")
	if err := dockerapi.PruneContainers(c.UserContext()); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/containers")
}

func (p *Panel) enqueueCompose(c *fiber.Ctx, action string, fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result) error {
	id := c.Params("id")
	
	v, _ := p.composeMu.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return respondAppNotFound(c)
	}
	var gitSyncPreamble string
	if p.isGitApp(c.UserContext(), id) {
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Minute)
		out, err := p.syncGitAppSource(ctx, id)
		if err != nil {
			cancel()
			msg := "[error]\nGit sync failed.\n\n" + err.Error()
			if strings.TrimSpace(out) != "" {
				msg += "\n\n" + out
			}
			_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
		}
		cancel()
		gitSyncPreamble = strings.TrimSpace(out)
		if gitSyncPreamble == "" {
			gitSyncPreamble = "Repository sync completed."
		}
	}
	cp := p.composeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		hasDockerfile, hasCompose := p.Store.HasDockerArtifacts(id)
		if hasDockerfile && !hasCompose {
			defaultCompose := []byte("services:\n  app:\n    build: .\n    restart: unless-stopped\n")
			if werr := os.WriteFile(cp, defaultCompose, 0644); werr != nil {
				msg := "[error]\nFailed to generate default compose file from Dockerfile: " + werr.Error()
				_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
				return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
			}
		} else {
			msg := "[error]\nCompose file not found. Set path on Overview or upload the file / sync the repository first."
			_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
			return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
		}
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		msg := "[error]\n" + err.Error()
		_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
	}
	projCtx, projCancel := context.WithTimeout(c.UserContext(), 90*time.Second)
	project := p.activeComposeProjectName(projCtx, app, id)
	projCancel()
	if action == "Deploy" || action == "Redeploy (pull + up)" {
		downCtx, downCancel := context.WithTimeout(c.UserContext(), 5*time.Minute)
		p.stopOtherComposeStacks(downCtx, app, id, project)
		downCancel()
	}
	p.RecordAuditLog(c, "compose_"+strings.ToLower(strings.ReplaceAll(action, " ", "_")), "app", id, "Triggered compose action: "+action)
	if err := p.startComposeJob(id, project, p.effectiveComposePaths(c.UserContext(), app, id), action, fn, gitSyncPreamble); err != nil {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment&busy=1", id))
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (p *Panel) DeployProgressPartial(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).SendString("not found")
	}
	r := p.getDeployRun(id)
	r.mu.Lock()
	out := r.Output.String()
	running := r.Running
	act := r.Action
	r.mu.Unlock()
	return c.Render(tmplPartialDeployProgress, fiber.Map{
		"ID":          id,
		"LiveOutput":  out,
		"LiveRunning": running,
		"LiveAction":  act,
		"OOBOnly":     c.Query("oob") == "1",
	})
}

func formatOut(r dockerx.Result) string {
	return runutil.StatusText(runutil.Result{OK: r.OK, Output: r.Output})
}
