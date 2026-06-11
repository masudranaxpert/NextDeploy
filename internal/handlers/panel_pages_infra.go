package handlers

import (
	"panel/internal/handlers/utils"
	"fmt"
	"os"
	"sort"
	"strings"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/sysinfo"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)


func (p *Panel) Overview(c *fiber.Ctx) error {
	si := sysinfo.Collect(c.UserContext())
	return c.Render("pages/overview", WithUser(c, fiber.Map{
		"Nav":   "overview",
		"Title": "Overview",
		"Sys":   si,
	}), "layouts/shell")
}

func (p *Panel) MonitorPage(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	sys := sysinfo.Collect(c.UserContext())
	rows, errMsg := dockerapi.ListContainerUsage(c.UserContext())
	users, _ := p.DB.ListUsers(c.UserContext())
	var totalAllocatedMemoryMB int
	var totalAllocatedCPUs float64
	for _, u := range users {
		if u.Role != db.RoleAdmin {
			totalAllocatedMemoryMB += u.MaxMemoryMB
			totalAllocatedCPUs += u.MaxCPUs
		}
	}
	memTotal := sys.MemTotalGB
	var memPct float64
	if memTotal > 0 {
		memPct = ((float64(totalAllocatedMemoryMB) / 1024.0) / memTotal) * 100.0
	}
	cpuTotal := float64(sys.NumCPU)
	var cpuPct float64
	if cpuTotal > 0 {
		cpuPct = (totalAllocatedCPUs / cpuTotal) * 100.0
	}

	return c.Render("pages/monitor", WithUser(c, fiber.Map{
		"Nav":                    "monitor",
		"Title":                  "Monitor",
		"Sys":                    sys,
		"UsageRows":              rows,
		"DockerError":            errMsg,
		"TotalAllocatedMemoryGB": float64(totalAllocatedMemoryMB) / 1024.0,
		"TotalAllocatedCPUs":     totalAllocatedCPUs,
		"MemoryAllocatedPct":     memPct,
		"CPUAllocatedPct":        cpuPct,
	}), "layouts/shell")
}

func (p *Panel) MonitorPartial(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	sys := sysinfo.Collect(c.UserContext())
	rows, errMsg := dockerapi.ListContainerUsage(c.UserContext())
	users, _ := p.DB.ListUsers(c.UserContext())
	var totalAllocatedMemoryMB int
	var totalAllocatedCPUs float64
	for _, u := range users {
		if u.Role != db.RoleAdmin {
			totalAllocatedMemoryMB += u.MaxMemoryMB
			totalAllocatedCPUs += u.MaxCPUs
		}
	}
	memTotal := sys.MemTotalGB
	var memPct float64
	if memTotal > 0 {
		memPct = ((float64(totalAllocatedMemoryMB) / 1024.0) / memTotal) * 100.0
	}
	cpuTotal := float64(sys.NumCPU)
	var cpuPct float64
	if cpuTotal > 0 {
		cpuPct = (totalAllocatedCPUs / cpuTotal) * 100.0
	}

	return c.Render(utils.TmplPartialMonitorStats, fiber.Map{
		"Sys":                    sys,
		"UsageRows":              rows,
		"DockerError":            errMsg,
		"TotalAllocatedMemoryGB": float64(totalAllocatedMemoryMB) / 1024.0,
		"TotalAllocatedCPUs":     totalAllocatedCPUs,
		"MemoryAllocatedPct":     memPct,
		"CPUAllocatedPct":        cpuPct,
	})
}

func (p *Panel) Containers(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	rows, errMsg := dockerapi.ListContainers(ctx)

	if u.Role != db.RoleAdmin {
		apps, err := p.DB.ListAppsForUser(ctx, u.ID)
		if err == nil {
			allowedProjects := make(map[string]bool)
			for _, app := range apps {
				allowedProjects[app.ID] = true
			}
			var filtered []dockerapi.ContainerRow
			for _, row := range rows {
				if allowedProjects[row.ComposeProject] {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		} else {
			rows = nil
		}
	}

	return c.Render("pages/containers", WithUser(c, fiber.Map{
		"Nav":         "containers",
		"Title":       "Containers",
		"Containers":  rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) ImagesPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	rows, errMsg := dockerapi.ListImages(ctx)

	if u.Role != db.RoleAdmin {
		apps, err := p.DB.ListAppsForUser(ctx, u.ID)
		if err == nil {
			allContainers, _ := dockerapi.ListContainers(ctx)
			allowedProjectIDs := make(map[string]bool)
			usedImageNames := make(map[string]bool)
			for _, app := range apps {
				allowedProjectIDs[app.ID] = true
			}
			for _, cRow := range allContainers {
				if allowedProjectIDs[cRow.ComposeProject] {
					usedImageNames[strings.TrimSpace(cRow.Image)] = true
				}
			}

			var filtered []dockerapi.ImageRow
			for _, img := range rows {
				owned := false
				for tag := range usedImageNames {
					if strings.Contains(img.Tags, tag) {
						owned = true
						break
					}
				}
				if !owned {
					for _, proj := range apps {
						alt := strings.ReplaceAll(proj.ID, "-", "_")
						for _, t := range img.RepoTags {
							if t == "" || t == "<none>" {
								continue
							}
							repo := imageRepoBase(t)
							if repo == proj.ID || repo == alt || strings.HasPrefix(repo, proj.ID+"_") || strings.HasPrefix(repo, alt+"_") {
								owned = true
								break
							}
						}
						if owned {
							break
						}
					}
				}
				if owned {
					filtered = append(filtered, img)
				}
			}
			rows = filtered
		} else {
			rows = nil
		}
	}

	return c.Render("pages/images", WithUser(c, fiber.Map{
		"Nav":         "images",
		"Title":       "Images",
		"Images":      rows,
		"DockerError": errMsg,
	}), "layouts/shell")
}

func (p *Panel) VolumesPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	names, errMsg := volumex.List(ctx)

	if u.Role != db.RoleAdmin {
		apps, err := p.DB.ListAppsForUser(ctx, u.ID)
		if err == nil {
			var filtered []string
			seen := make(map[string]bool)
			for _, app := range apps {
				projCandidates := []string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}
				appVols, _ := volumex.ListForApp(ctx, app.ID, projCandidates)
				for _, v := range appVols {
					if !seen[v] {
						seen[v] = true
						filtered = append(filtered, v)
					}
				}
			}
			sort.Strings(filtered)
			names = filtered
		} else {
			names = nil
		}
	}

	return c.Render("pages/volumes", WithUser(c, fiber.Map{
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
	allowed, err := p.isVolumeAccessAllowed(c, name)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if !allowed {
		return c.Status(403).SendString("forbidden")
	}
	rel := c.Query("path", "")
	fromApp := strings.TrimSpace(c.Query("from_app", ""))

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
	return c.Render("pages/volume_browse", WithUser(c, fiber.Map{
		"Nav":         "volumes",
		"Title":       name,
		"VolumeName":  name,
		"Path":        rel,
		"ParentPath":  parent,
		"VolRows":     rows,
		"BrowseError": msg,
		"Flash":       utils.ReadFlash(c),
		"FlashError":  utils.ReadFlashError(c),
		"FromApp":     fromApp,
	}), "layouts/shell")
}

func (p *Panel) VolumeDownload(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Query("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	allowed, err := p.isVolumeAccessAllowed(c, name)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if !allowed {
		return c.Status(403).SendString("forbidden")
	}

	ctx := c.UserContext()
	tmpPath, err := volumex.BackupToTemp(ctx, name)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer os.Remove(tmpPath)

	safe := strings.ReplaceAll(name, `"`, "")
	c.Set("Content-Type", "application/gzip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-backup.tar.gz"`, safe))
	return c.SendFile(tmpPath)
}

func (p *Panel) isVolumeAccessAllowed(c *fiber.Ctx, volumeName string) (bool, error) {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return false, nil
	}
	if u.Role == db.RoleAdmin {
		return true, nil
	}
	apps, err := p.DB.ListAppsForUser(ctx, u.ID)
	if err != nil {
		return false, err
	}
	for _, app := range apps {
		projCandidates := []string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}
		appVols, _ := volumex.ListForApp(ctx, app.ID, projCandidates)
		for _, v := range appVols {
			if v == volumeName {
				return true, nil
			}
		}
	}
	return false, nil
}

func imageRepoBase(repo string) string {
	repo = strings.TrimSpace(repo)
	if i := strings.LastIndex(repo, ":"); i > 0 {
		repo = repo[:i]
	}
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	return repo
}
