package handlers

import (
	"context"
	"strings"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/phphosting"
	"panel/internal/resmatch"
	"panel/internal/volumex"
)

// Tenant isolation: admins see everything, other users see only the resources that belong to
// their own apps. These helpers back the SPA bootstrap payload and the per-resource checks, so
// both views of "what is mine" stay identical.

// userApps returns the apps a user may see (own + collaborations).
func (p *Panel) userApps(ctx context.Context, u db.User) []db.App {
	apps, _ := p.DB.ListAppsForUser(ctx, u.ID)
	return apps
}

// developerApps returns apps where the user is the owner or a developer collaborator (or admin).
func (p *Panel) developerApps(ctx context.Context, u db.User) []db.App {
	if u.Role == db.RoleAdmin {
		all, _ := p.DB.ListApps(ctx)
		return all
	}
	all := p.userApps(ctx, u)
	out := make([]db.App, 0, len(all))
	for _, a := range all {
		if a.OwnerID == u.ID {
			out = append(out, a)
			continue
		}
		if collab, err := p.DB.GetCollaborator(ctx, a.ID, u.ID); err == nil && collab.Role == db.CollabRoleDeveloper {
			out = append(out, a)
		}
	}
	return out
}

// visibleContainers keeps only the containers that belong to u's apps (admins keep all).
func (p *Panel) visibleContainers(ctx context.Context, u db.User, rows []dockerapi.ContainerRow) []dockerapi.ContainerRow {
	if u.Role == db.RoleAdmin {
		return rows
	}
	apps := p.userApps(ctx, u)
	projects := map[string]bool{}
	for _, proj := range p.composeProjectsForApps(ctx, apps) {
		projects[proj] = true
	}
	// PHP sites: the owner's runtime container and each site's route holder.
	phpNames := map[string]bool{}
	if p.PHP != nil {
		for _, a := range apps {
			if site, err := p.DB.GetPHPSite(ctx, a.ID); err == nil {
				phpNames[phphosting.RouteContainer(a.ID)] = true
				if site.OwnerID == u.ID && site.PHPVersion != "" {
					phpNames[phphosting.RuntimeContainer(site.OwnerID, site.PHPVersion)] = true
				}
			}
		}
	}
	out := make([]dockerapi.ContainerRow, 0, len(rows))
	for _, r := range rows {
		if (r.ComposeProject != "" && projects[r.ComposeProject]) || phpNames[strings.TrimPrefix(r.Name, "/")] {
			out = append(out, r)
		}
	}
	return out
}

// writeableContainers keeps only the containers that u can mutate (restart, remove). Viewers cannot mutate.
func (p *Panel) writeableContainers(ctx context.Context, u db.User, rows []dockerapi.ContainerRow) []dockerapi.ContainerRow {
	if u.Role == db.RoleAdmin {
		return rows
	}
	apps := p.developerApps(ctx, u)
	projects := map[string]bool{}
	for _, proj := range p.composeProjectsForApps(ctx, apps) {
		projects[proj] = true
	}
	phpNames := map[string]bool{}
	if p.PHP != nil {
		for _, a := range apps {
			if site, err := p.DB.GetPHPSite(ctx, a.ID); err == nil {
				phpNames[phphosting.RouteContainer(a.ID)] = true
				if site.OwnerID == u.ID && site.PHPVersion != "" {
					phpNames[phphosting.RuntimeContainer(site.OwnerID, site.PHPVersion)] = true
				}
			}
		}
	}
	out := make([]dockerapi.ContainerRow, 0, len(rows))
	for _, r := range rows {
		if (r.ComposeProject != "" && projects[r.ComposeProject]) || phpNames[strings.TrimPrefix(r.Name, "/")] {
			out = append(out, r)
		}
	}
	return out
}

// imageUsageScope precomputes what is needed to decide image ownership for one user.
type imageScope struct {
	usedTags     map[string]bool
	apps         []db.App
	allProjNames []string
}

func (p *Panel) newImageScope(ctx context.Context, u db.User, allContainers []dockerapi.ContainerRow) imageScope {
	return p.imageScopeFor(ctx, p.userApps(ctx, u), allContainers)
}

// imageScopeFor matches images to a set of apps (the images their containers use, or built from them).
func (p *Panel) imageScopeFor(ctx context.Context, apps []db.App, allContainers []dockerapi.ContainerRow) imageScope {
	allowed := map[string]bool{}
	for _, a := range apps {
		allowed[a.ID] = true
	}
	for _, proj := range p.composeProjectsForApps(ctx, apps) {
		allowed[proj] = true
	}
	used := map[string]bool{}
	for _, c := range allContainers {
		if c.ComposeProject != "" && allowed[c.ComposeProject] {
			used[strings.TrimSpace(c.Image)] = true
		}
	}
	allProjects := p.AllPanelComposeProjects(ctx)
	return imageScope{usedTags: used, apps: apps, allProjNames: allProjects}
}

func (s imageScope) allows(img dockerapi.ImageRow) bool {
	for tag := range s.usedTags {
		if tag != "" && strings.Contains(img.Tags, tag) {
			return true
		}
	}
	for _, app := range s.apps {
		for _, t := range img.RepoTags {
			if t == "" || t == "<none>" {
				continue
			}
			if resmatch.MatchesImageRepo(imageRepoBase(t), app.ID, s.allProjNames) {
				return true
			}
		}
	}
	return false
}

func (p *Panel) visibleImages(ctx context.Context, u db.User, images []dockerapi.ImageRow, allContainers []dockerapi.ContainerRow) []dockerapi.ImageRow {
	if u.Role == db.RoleAdmin {
		return images
	}
	scope := p.newImageScope(ctx, u, allContainers)
	out := make([]dockerapi.ImageRow, 0, len(images))
	for _, img := range images {
		if scope.allows(img) {
			out = append(out, img)
		}
	}
	return out
}

// visibleVolumes returns the Docker volumes that belong to u's compose apps. PHP sites keep
// their data in the workspace and in the database server, never in Docker volumes.
func (p *Panel) visibleVolumes(ctx context.Context, u db.User, all []string) []string {
	if u.Role == db.RoleAdmin {
		return all
	}
	allProjects := p.AllPanelComposeProjects(ctx)
	matcher := volumex.SharedMatcher(ctx)
	seen := map[string]bool{}
	out := []string{}
	for _, app := range p.userApps(ctx, u) {
		for _, v := range p.appVolumeNames(ctx, app, all, allProjects, matcher) {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// appVolumeNames lists the Docker volumes that belong to one project. PHP sites keep their
// data in the workspace and in the database server, never in Docker volumes.
func (p *Panel) appVolumeNames(ctx context.Context, app db.App, all, allProjects []string, matcher *volumex.Matcher) []string {
	if p.PHP != nil && p.DB.IsPHPSite(ctx, app.ID) {
		return nil
	}
	cands := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
	q := p.AppVolumeQuery(ctx, app, allProjects, cands...)
	vols, _ := matcher.ListForAppFromNames(ctx, q, all)
	return vols
}

// writeableVolumes returns Docker volumes that u can delete (requires developer access on app).
func (p *Panel) writeableVolumes(ctx context.Context, u db.User, all []string) []string {
	if u.Role == db.RoleAdmin {
		return all
	}
	allProjects := p.AllPanelComposeProjects(ctx)
	matcher := volumex.SharedMatcher(ctx)
	seen := map[string]bool{}
	out := []string{}
	for _, app := range p.developerApps(ctx, u) {
		if p.PHP != nil && p.DB.IsPHPSite(ctx, app.ID) {
			continue
		}
		cands := append([]string{app.ID, strings.ReplaceAll(app.ID, "-", "_"), app.Name}, p.ComposeProjectCandidates(ctx, app, app.ID)...)
		q := p.AppVolumeQuery(ctx, app, allProjects, cands...)
		vols, _ := matcher.ListForAppFromNames(ctx, q, all)
		for _, v := range vols {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// visibleWebhookDeliveries limits git webhook deliveries to the user's own apps.
func (p *Panel) visibleWebhookDeliveries(ctx context.Context, u db.User, limit int) []db.WebhookDelivery {
	if u.Role == db.RoleAdmin {
		list, _ := p.DB.ListWebhookDeliveries(ctx, limit)
		return list
	}
	mine := map[string]bool{}
	for _, a := range p.userApps(ctx, u) {
		mine[a.ID] = true
	}
	all, _ := p.DB.ListWebhookDeliveries(ctx, 500)
	out := make([]db.WebhookDelivery, 0, limit)
	for _, d := range all {
		if mine[d.AppID] {
			out = append(out, d)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
