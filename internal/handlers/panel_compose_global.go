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
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err == nil && strings.TrimSpace(app.TemplateID) == "php_panel" {
		return p.enqueueComposePHPPanel(c, "Deploy")
	}
	return p.enqueueCompose(c, "Deploy", dockerx.ComposeUp)
}

func (p *Panel) ComposeDown(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stop", dockerx.ComposeDown)
}

func (p *Panel) ComposeRestart(c *fiber.Ctx) error {
	return p.enqueueCompose(c, "Stack restart", dockerx.ComposeRestart)
}

func (p *Panel) ComposeRedeploy(c *fiber.Ctx) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err == nil && strings.TrimSpace(app.TemplateID) == "php_panel" {
		return p.enqueueComposePHPPanel(c, "Redeploy (pull + up)")
	}
	return p.enqueueCompose(c, "Redeploy (pull + up)", dockerx.ComposePullUp)
}

func (p *Panel) enqueueComposePHPPanel(c *fiber.Ctx, action string) error {
	id := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), id)
	if err != nil {
		return c.Status(404).SendString("app not found")
	}
	cp := p.composeFilePath(c.UserContext(), app, id)
	if _, err := os.Stat(cp); err != nil {
		msg := "[error]\nCompose file not found. Set path on Overview or upload the file / sync the repository first."
		_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
	}
	if err := p.syncAppCaddyOverride(c, id); err != nil {
		msg := "[error]\n" + err.Error()
		_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
	}
	project := p.composeProjectName(app, id)
	rows, _ := dockerx.ComposePS(c.UserContext(), p.appSourcePath(c.UserContext(), id), p.effectiveComposePaths(c.UserContext(), app, id), project, p.composeEnvFiles(c.UserContext(), id))
	services := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.State), "running") {
			continue
		}
		service := strings.TrimSpace(row.Service)
		if service == "" || seen[service] {
			continue
		}
		seen[service] = true
		services = append(services, service)
	}
	if len(services) == 0 {
		services = []string{"php_fpm_83", "php_mysql"}
	}
	var fn func(context.Context, string, []string, string, io.Writer, []string) dockerx.Result
	switch action {
	case "Redeploy (pull + up)":
		fn = func(ctx context.Context, dir string, composePaths []string, project string, logW io.Writer, envFiles []string) dockerx.Result {
			return dockerx.ComposePullUpServices(ctx, dir, composePaths, project, logW, envFiles, services...)
		}
	default:
		fn = func(ctx context.Context, dir string, composePaths []string, project string, logW io.Writer, envFiles []string) dockerx.Result {
			return dockerx.ComposeUpServices(ctx, dir, composePaths, project, logW, envFiles, services...)
		}
	}
	if err := p.startComposeJob(id, project, p.effectiveComposePaths(c.UserContext(), app, id), action, fn, ""); err != nil {
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment&busy=1", id))
	}
	return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
}

func (p *Panel) GlobalImageRemove(c *fiber.Ctx) error {
	imageID := strings.TrimSpace(c.FormValue("image_id"))
	if imageID == "" {
		return c.Status(400).SendString("image_id required")
	}
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
	if err := dockerapi.RemoveContainerByName(c.UserContext(), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/containers")
}

func (p *Panel) GlobalVolumeRemove(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("name required")
	}
	if err := dockerapi.RemoveVolumeByName(c.UserContext(), name); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/volumes")
}

// GlobalImagePrune removes all unused (dangling) images.
func (p *Panel) GlobalImagePrune(c *fiber.Ctx) error {
	if err := dockerapi.PruneImages(c.UserContext()); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Redirect("/images")
}

// GlobalContainerPrune removes all stopped containers.
func (p *Panel) GlobalContainerPrune(c *fiber.Ctx) error {
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
		msg := "[error]\nCompose file not found. Set path on Overview or upload the file / sync the repository first."
		_ = p.DB.InsertDeployLog(c.UserContext(), id, action, false, msg)
		return c.Redirect(fmt.Sprintf("/apps/%s?tab=deployment", id))
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
