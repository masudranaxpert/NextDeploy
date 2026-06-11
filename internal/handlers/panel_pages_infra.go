package handlers

import (
	"context"
	"panel/internal/handlers/utils"
	"fmt"
	"os"
	"sort"
	"strings"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/sandbox"
	"panel/internal/sysinfo"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)


func (p *Panel) Overview(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok {
		return c.Redirect("/login")
	}

	si := sysinfo.Collect(c.UserContext())

	var appCount int
	var allocatedMemMB int
	var allocatedCPUs float64
	var maxApps int
	var maxMemoryMB int
	var maxCPUs float64
	var appPercent, memPercent, cpuPercent float64

	var totalContainers int
	var totalImages int
	var totalVolumes int
	var storageUsed, storageMax string
	var storagePercent float64
	var usedMemBytes int64
	var usedCPUs float64
	var usedMemPercent, usedCPUPercent float64

	ctx := c.UserContext()

	if u.Role == db.RoleAdmin {
		apps, err := p.DB.ListApps(ctx)
		if err == nil {
			appCount = len(apps)
		}
		if rows, rerr := dockerapi.ListContainers(ctx); rerr == "" {
			totalContainers = len(rows)
		}
		if vols, rerr := volumex.List(ctx); rerr == "" {
			totalVolumes = len(vols)
		}
		if imgs, rerr := dockerapi.ListImages(ctx); rerr == "" {
			totalImages = len(imgs)
		}
	} else {
		maxApps = u.MaxApps
		maxMemoryMB = u.MaxMemoryMB
		maxCPUs = u.MaxCPUs

		apps, err := p.DB.ListAppsForUser(ctx, u.ID)
		if err == nil {
			appCount = len(apps)
			if maxApps > 0 {
				appPercent = (float64(appCount) / float64(maxApps)) * 100.0
			}

			// Compose project names are app-name slugs (plus legacy variants),
			// not app IDs — match containers through the candidate set.
			projectToApp := make(map[string]string)
			candidatesByApp := make(map[string][]string)
			for _, app := range apps {
				cands := p.ComposeProjectCandidates(ctx, app, app.ID)
				candidatesByApp[app.ID] = cands
				for _, proj := range cands {
					projectToApp[proj] = app.ID
				}
			}

			runningApps := make(map[string]bool)
			seenImages := make(map[string]bool)
			if rows, rerr := dockerapi.ListContainers(ctx); rerr == "" {
				for _, row := range rows {
					if appID, ok := projectToApp[row.ComposeProject]; ok {
						totalContainers++
						if row.Image != "" {
							seenImages[row.Image] = true
						}
						state := strings.ToLower(row.State)
						if state == "running" || state == "restarting" {
							runningApps[appID] = true
						}
					}
				}
			}
			totalImages = len(seenImages)

			for _, app := range apps {
				projCandidates := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, candidatesByApp[app.ID]...)
				appVols, _ := volumex.ListForApp(ctx, app.ID, projCandidates)
				totalVolumes += len(appVols)

				if runningApps[app.ID] {
					composePath := p.composeOverridePath(ctx, app.ID)
					if _, serr := os.Stat(composePath); serr != nil {
						composePath = p.composeFilePath(ctx, app, app.ID)
					}
					if data, rerr := os.ReadFile(composePath); rerr == nil {
						memLimit, cpuLimit, perr := sandbox.GetComposeResources(data)
						if perr == nil {
							allocatedMemMB += memLimit
							allocatedCPUs += cpuLimit
						}
					}
				}
			}


			if maxMemoryMB > 0 && allocatedMemMB > maxMemoryMB {
				allocatedMemMB = maxMemoryMB
			}
			if maxCPUs > 0 && allocatedCPUs > maxCPUs {
				allocatedCPUs = maxCPUs
			}
			if maxMemoryMB > 0 {
				memPercent = (float64(allocatedMemMB) / float64(maxMemoryMB)) * 100.0
			}
			if maxCPUs > 0 {
				cpuPercent = (allocatedCPUs / maxCPUs) * 100.0
			}

			// Live usage from docker stats, scoped to this user's containers.
			projectSet := make(map[string]bool, len(projectToApp))
			for proj := range projectToApp {
				projectSet[proj] = true
			}
			if usage, uerr := dockerapi.ListContainerUsageForProjects(ctx, projectSet); uerr == "" {
				for _, row := range usage {
					if strings.ToLower(row.State) != "running" {
						continue
					}
					usedMemBytes += int64(row.MemUsage)
					usedCPUs += row.CPUPercent / 100.0
				}
			}
			if maxMemoryMB > 0 {
				usedMemPercent = (float64(usedMemBytes) / (float64(maxMemoryMB) * 1024 * 1024)) * 100.0
				if usedMemPercent > 100 {
					usedMemPercent = 100
				}
			}
			if maxCPUs > 0 {
				usedCPUPercent = (usedCPUs / maxCPUs) * 100.0
				if usedCPUPercent > 100 {
					usedCPUPercent = 100
				}
			}

			var usedBytes int64
			for _, app := range apps {
				usedBytes += p.AppStorageBytes(app.ID)
			}
			maxBytes := int64(u.MaxStorageMB) * 1024 * 1024
			storageUsed = HumanStorage(usedBytes)
			storageMax = HumanStorage(maxBytes)
			if maxBytes > 0 {
				storagePercent = (float64(usedBytes) / float64(maxBytes)) * 100.0
				if storagePercent > 100 {
					storagePercent = 100
				}
			}
		}
	}

	return c.Render("pages/overview", WithUser(c, fiber.Map{
		"Nav":             "overview",
		"Title":           "Overview",
		"Sys":             si,
		"IsAdmin":         u.Role == db.RoleAdmin,
		"AppCount":        appCount,
		"MaxApps":         maxApps,
		"AppPercent":      appPercent,
		"AllocatedMemGB":  float64(allocatedMemMB) / 1024.0,
		"MaxMemoryGB":     float64(maxMemoryMB) / 1024.0,
		"MemPercent":      memPercent,
		"AllocatedCPUs":   allocatedCPUs,
		"MaxCPUs":         maxCPUs,
		"CPUPercent":      cpuPercent,
		"UsedMemHuman":    HumanStorage(usedMemBytes),
		"UsedMemPercent":  usedMemPercent,
		"UsedCPUs":        usedCPUs,
		"UsedCPUPercent":  usedCPUPercent,
		"TotalContainers": totalContainers,
		"TotalImages":     totalImages,
		"TotalVolumes":    totalVolumes,
		"StorageUsed":     storageUsed,
		"StorageMax":      storageMax,
		"StoragePercent":  storagePercent,
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

// ResourceOwner identifies which panel user owns a Docker resource (via the
// owning app's compose project). Used by admin views of containers/images/volumes.
type ResourceOwner struct {
	Name  string
	Admin bool
}

// ownersByUserID maps panel user ids to display info for owner badges.
func (p *Panel) ownersByUserID(ctx context.Context) map[int64]ResourceOwner {
	users, err := p.DB.ListUsers(ctx)
	if err != nil {
		return nil
	}
	m := make(map[int64]ResourceOwner, len(users))
	for _, usr := range users {
		m[usr.ID] = ResourceOwner{Name: usr.Username, Admin: usr.Role == db.RoleAdmin}
	}
	return m
}

// ownersByProject maps compose project names to the owning panel user.
func (p *Panel) ownersByProject(ctx context.Context) map[string]ResourceOwner {
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return nil
	}
	byUser := p.ownersByUserID(ctx)
	byProject := make(map[string]ResourceOwner)
	for _, app := range apps {
		owner, ok := byUser[app.OwnerID]
		if !ok {
			continue
		}
		for _, proj := range p.ComposeProjectCandidates(ctx, app, app.ID) {
			if _, exists := byProject[proj]; !exists {
				byProject[proj] = owner
			}
		}
	}
	return byProject
}

func (p *Panel) Containers(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	rows, errMsg := dockerapi.ListContainers(ctx)
	isAdmin := u.Role == db.RoleAdmin

	ownerByProject := map[string]ResourceOwner{}
	if isAdmin {
		if m := p.ownersByProject(ctx); m != nil {
			ownerByProject = m
		}
	} else {
		apps, err := p.DB.ListAppsForUser(ctx, u.ID)
		if err == nil {
			allowedProjects := make(map[string]bool)
			for _, app := range apps {
				for _, proj := range p.ComposeProjectCandidates(ctx, app, app.ID) {
					allowedProjects[proj] = true
				}
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
		"Nav":            "containers",
		"Title":          "Containers",
		"Containers":     rows,
		"DockerError":    errMsg,
		"IsAdmin":        isAdmin,
		"OwnerByProject": ownerByProject,
	}), "layouts/shell")
}

type imageListItem struct {
	dockerapi.ImageRow
	Owner ResourceOwner
}


func imageMatchesApp(img dockerapi.ImageRow, projects []string, imageProjects map[string]string, projectSet map[string]bool) bool {
	for imgName, proj := range imageProjects {
		if projectSet[proj] && imgName != "" && strings.Contains(img.Tags, imgName) {
			return true
		}
	}
	for _, proj := range projects {
		proj = strings.TrimSpace(proj)
		if proj == "" {
			continue
		}
		alt := strings.ReplaceAll(proj, "-", "_")
		for _, t := range img.RepoTags {
			if t == "" || t == "<none>" {
				continue
			}
			repo := imageRepoBase(t)
			if repo == proj || repo == alt || strings.HasPrefix(repo, proj+"_") || strings.HasPrefix(repo, alt+"_") || strings.HasPrefix(repo, proj+"-") || strings.HasPrefix(repo, alt+"-") {
				return true
			}
		}
	}
	return false
}

func (p *Panel) ImagesPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	rows, errMsg := dockerapi.ListImages(ctx)
	isAdmin := u.Role == db.RoleAdmin

	allContainers, _ := dockerapi.ListContainers(ctx)
	// image name -> compose project using it (for ownership attribution)
	imageProjects := make(map[string]string)
	for _, cRow := range allContainers {
		img := strings.TrimSpace(cRow.Image)
		if img != "" && cRow.ComposeProject != "" {
			imageProjects[img] = cRow.ComposeProject
		}
	}

	items := make([]imageListItem, 0, len(rows))

	if isAdmin {
		apps, _ := p.DB.ListApps(ctx)
		byUser := p.ownersByUserID(ctx)
		type appMatch struct {
			projects   []string
			projectSet map[string]bool
			owner      ResourceOwner
		}
		matches := make([]appMatch, 0, len(apps))
		for _, app := range apps {
			cands := p.ComposeProjectCandidates(ctx, app, app.ID)
			projects := append([]string{app.ID}, cands...)
			set := make(map[string]bool, len(projects))
			for _, proj := range projects {
				set[proj] = true
			}
			matches = append(matches, appMatch{projects: projects, projectSet: set, owner: byUser[app.OwnerID]})
		}
		for _, img := range rows {
			item := imageListItem{ImageRow: img}
			for _, m := range matches {
				if imageMatchesApp(img, m.projects, imageProjects, m.projectSet) {
					item.Owner = m.owner
					break
				}
			}
			items = append(items, item)
		}
	} else {
		apps, err := p.DB.ListAppsForUser(ctx, u.ID)
		if err == nil {
			for _, app := range apps {
				cands := p.ComposeProjectCandidates(ctx, app, app.ID)
				projects := append([]string{app.ID}, cands...)
				set := make(map[string]bool, len(projects))
				for _, proj := range projects {
					set[proj] = true
				}
				for _, img := range rows {
					if imageMatchesApp(img, projects, imageProjects, set) {
						items = append(items, imageListItem{ImageRow: img})
					}
				}
			}
			// de-duplicate images matched by multiple apps
			seen := make(map[string]bool, len(items))
			deduped := items[:0]
			for _, it := range items {
				if !seen[it.ID] {
					seen[it.ID] = true
					deduped = append(deduped, it)
				}
			}
			items = deduped
		}
	}

	return c.Render("pages/images", WithUser(c, fiber.Map{
		"Nav":         "images",
		"Title":       "Images",
		"Images":      items,
		"DockerError": errMsg,
		"IsAdmin":     isAdmin,
	}), "layouts/shell")
}

func (p *Panel) VolumesPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := currentUser(c)
	names, errMsg := volumex.List(ctx)
	isAdmin := u.Role == db.RoleAdmin

	ownerByVolume := map[string]ResourceOwner{}
	if isAdmin {
		apps, _ := p.DB.ListApps(ctx)
		byUser := p.ownersByUserID(ctx)
		for _, app := range apps {
			owner, ok := byUser[app.OwnerID]
			if !ok {
				continue
			}
			projects := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
			for _, n := range names {
				if _, taken := ownerByVolume[n]; taken {
					continue
				}
				for _, proj := range projects {
					proj = strings.TrimSpace(proj)
					if proj == "" {
						continue
					}
					alt := strings.ReplaceAll(proj, "-", "_")
					if n == proj || n == alt || strings.HasPrefix(n, proj+"_") || strings.HasPrefix(n, alt+"_") {
						ownerByVolume[n] = owner
						break
					}
				}
			}
		}
	} else {
		apps, err := p.DB.ListAppsForUser(ctx, u.ID)
		if err == nil {
			var filtered []string
			seen := make(map[string]bool)
			for _, app := range apps {
				projCandidates := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
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
		"Nav":           "volumes",
		"Title":         "Volumes",
		"Volumes":       names,
		"VolumeError":   errMsg,
		"IsAdmin":       isAdmin,
		"OwnerByVolume": ownerByVolume,
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
