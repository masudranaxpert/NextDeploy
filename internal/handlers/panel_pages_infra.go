package handlers

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"panel/internal/dockerapi"
	"panel/internal/sysinfo"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)


func (p *Panel) Overview(c *fiber.Ctx) error {
	if user, ok := currentUser(c); ok && user.Role == "user" {
		if app, hasApp := p.currentPHPPanelApp(c.UserContext()); hasApp && p.DB.PHPPanelEnabledForUser(c.UserContext(), user.ID) {
			return c.Redirect("/php-panel/" + app.ID)
		}
		return c.Redirect("/php-panel-blocked")
	}
	si := sysinfo.Collect(c.UserContext())
	return c.Render("pages/overview", withUser(c, fiber.Map{
		"Nav":   "overview",
		"Title": "Overview",
		"Sys":   si,
	}), "layouts/shell")
}

func (p *Panel) PHPPanelBlockedPage(c *fiber.Ctx) error {
	return c.Render("pages/php_panel_blocked", withUser(c, fiber.Map{
		"Nav":   "templates",
		"Title": "PHP Panel access required",
	}), "layouts/shell")
}

func (p *Panel) MonitorPage(c *fiber.Ctx) error {
	sys := sysinfo.Collect(c.UserContext())
	rows, errMsg := dockerapi.ListContainerUsage(c.UserContext())
	return c.Render("pages/monitor", withUser(c, fiber.Map{
		"Nav":         "monitor",
		"Title":       "Monitor",
		"Sys":         sys,
		"UsageRows":   rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) MonitorPartial(c *fiber.Ctx) error {
	sys := sysinfo.Collect(c.UserContext())
	rows, errMsg := dockerapi.ListContainerUsage(c.UserContext())
	return c.Render(tmplPartialMonitorStats, fiber.Map{
		"Sys":         sys,
		"UsageRows":   rows,
		"DockerError": errMsg,
	})
}

func (p *Panel) Containers(c *fiber.Ctx) error {
	rows, errMsg := dockerapi.ListContainers(c.UserContext())
	return c.Render("pages/containers", withUser(c, fiber.Map{
		"Nav":         "containers",
		"Title":       "Containers",
		"Containers":  rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) ImagesPage(c *fiber.Ctx) error {
	rows, errMsg := dockerapi.ListImages(c.UserContext())
	return c.Render("pages/images", withUser(c, fiber.Map{
		"Nav":         "images",
		"Title":       "Images",
		"Images":      rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) VolumesPage(c *fiber.Ctx) error {
	names, errMsg := volumex.List(c.UserContext())
	return c.Render("pages/volumes", withUser(c, fiber.Map{
		"Nav":         "volumes",
		"Title":       "Volumes",
		"Volumes":     names,
		"VolumeError": errMsg,
	}), "layouts/shell")
}

type volRow struct {
	Name    string
	IsDir   bool
	RelPath string
}

func (p *Panel) VolumeBrowse(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Query("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	rel := c.Query("path", "")
	entries, msg := volumex.ListDir(c.UserContext(), name, rel)
	parent := volumex.ParentRel(rel)
	rows := make([]volRow, 0, len(entries))
	for _, e := range entries {
		rp := e.Name
		if rel != "" {
			rp = rel + "/" + e.Name
		}
		rows = append(rows, volRow{Name: e.Name, IsDir: e.IsDir, RelPath: rp})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsDir != rows[j].IsDir {
			return rows[i].IsDir
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return c.Render("pages/volume_browse", withUser(c, fiber.Map{
		"Nav":         "volumes",
		"Title":       name,
		"VolumeName":  name,
		"Path":        rel,
		"ParentPath":  parent,
		"VolRows":     rows,
		"BrowseError": msg,
		"Flash":       c.Query("flash", ""),
	}), "layouts/shell")
}

func (p *Panel) VolumeDownload(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Query("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	// Use background context — the defer cancel() in a normal handler fires
	// before SetBodyStreamWriter's callback runs (the callback executes after
	// the handler returns), which would kill the docker process mid-stream.
	r, err := volumex.OpenTarStream(context.Background(), name)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	safe := strings.ReplaceAll(name, `"`, "")
	c.Set("Content-Type", "application/gzip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-backup.tar.gz"`, safe))
	// SetBodyStream(-1) tells fasthttp to stream from r without a Content-Length,
	// using chunked transfer encoding. This avoids buffering large archives in memory.
	// The -1 content-size means "unknown length — stream until EOF".
	c.Context().Response.SetBodyStream(r, -1)
	return nil
}

func (p *Panel) VolumeRestore(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	fh, err := c.FormFile("backup")
	if err != nil {
		return c.Status(400).SendString("upload a .tar.gz backup")
	}
	src, err := fh.Open()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer src.Close()
	msg := volumex.RestoreTarGz(c.UserContext(), name, src)
	q := url.Values{}
	q.Set("name", name)
	if msg != "" {
		q.Set("flash", msg)
	}
	return c.Redirect("/volumes/browse?" + q.Encode())
}
