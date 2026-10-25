package handlers

import (
	"context"
	"sort"
	"strings"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/phphosting"
)

// ownerRef says who a project or Docker resource belongs to, so an admin can tell at a glance
// whose container, image or volume it is. System marks the panel's own infrastructure.
type ownerRef struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	System bool   `json:"system,omitempty"`
}

var systemOwner = ownerRef{Name: "System", Role: "system", System: true}

// resourceOwners maps Docker resources (containers and images by id, volumes by name) to owners.
type resourceOwners struct {
	Containers map[string]ownerRef `json:"containers"`
	Images     map[string]ownerRef `json:"images"`
	Volumes    map[string]ownerRef `json:"volumes"`
	// Git providers, registries and backup destinations by id; "Global" ones are shared by all.
	Providers    map[string]ownerRef `json:"providers"`
	Registries   map[string]ownerRef `json:"registries"`
	Destinations map[string]ownerRef `json:"destinations"`
}

var globalOwner = ownerRef{Name: "Global", Role: "system", System: true}

func scopeOwner(lookup map[int64]ownerRef, userID *int64) ownerRef {
	if userID == nil {
		return globalOwner
	}
	return ownerOf(lookup, *userID)
}

func ownerLookup(users []db.User) map[int64]ownerRef {
	m := make(map[int64]ownerRef, len(users))
	for _, u := range users {
		m[u.ID] = ownerRef{ID: u.ID, Name: u.Username, Role: u.Role}
	}
	return m
}

func ownerOf(lookup map[int64]ownerRef, id int64) ownerRef {
	if o, ok := lookup[id]; ok {
		return o
	}
	return ownerRef{ID: id, Name: "deleted user", Role: db.RoleUser}
}

// ownerName is the username of a user id, for audit lines about things that no longer exist.
func (p *Panel) ownerName(ctx context.Context, id int64) string {
	if u, err := p.DB.GetUserByID(ctx, id); err == nil {
		return u.Username
	}
	return "deleted user"
}

// buildResourceOwners attributes every container, image and volume to the user who owns the
// project it belongs to, or to System when no project claims it. Admin sessions only.
func (p *Panel) buildResourceOwners(ctx context.Context, apps []db.App, users []db.User, containers []dockerapi.ContainerRow, images []dockerapi.ImageRow, volumesByApp map[string][]string, volumeNames []string) resourceOwners {
	lookup := ownerLookup(users)
	out := resourceOwners{
		Containers: make(map[string]ownerRef, len(containers)),
		Images:     make(map[string]ownerRef, len(images)),
		Volumes:    make(map[string]ownerRef, len(volumeNames)),
	}
	out.Providers = map[string]ownerRef{}
	out.Registries = map[string]ownerRef{}
	out.Destinations = map[string]ownerRef{}

	byProject := map[string]ownerRef{}
	byName := map[string]ownerRef{}
	appsByOwner := map[int64][]db.App{}
	for _, a := range apps {
		o := ownerOf(lookup, a.OwnerID)
		appsByOwner[a.OwnerID] = append(appsByOwner[a.OwnerID], a)
		byProject[a.ID] = o
		for _, proj := range p.composeProjectsForApps(ctx, []db.App{a}) {
			byProject[proj] = o
		}
		byName[phphosting.RouteContainer(a.ID)] = o
		for _, v := range volumesByApp[a.ID] {
			out.Volumes[v] = o
		}
	}
	if p.PHP != nil {
		if sites, err := p.DB.ListPHPSites(ctx); err == nil {
			for _, s := range sites {
				if s.PHPVersion != "" {
					byName[phphosting.RuntimeContainer(s.OwnerID, s.PHPVersion)] = ownerOf(lookup, s.OwnerID)
				}
			}
		}
	}

	for _, c := range containers {
		if o, ok := byName[strings.TrimPrefix(c.Name, "/")]; ok {
			out.Containers[c.ID] = o
		} else if o, ok := byProject[c.ComposeProject]; ok && c.ComposeProject != "" {
			out.Containers[c.ID] = o
		} else {
			out.Containers[c.ID] = systemOwner
		}
	}
	for _, v := range volumeNames {
		if _, ok := out.Volumes[v]; !ok {
			out.Volumes[v] = systemOwner
		}
	}

	ownerIDs := make([]int64, 0, len(appsByOwner))
	for id := range appsByOwner {
		ownerIDs = append(ownerIDs, id)
	}
	sort.Slice(ownerIDs, func(i, j int) bool { return ownerIDs[i] < ownerIDs[j] })
	scopes := make(map[int64]imageScope, len(ownerIDs))
	for _, id := range ownerIDs {
		scopes[id] = p.imageScopeFor(ctx, appsByOwner[id], containers)
	}
	for _, img := range images {
		out.Images[img.ID] = systemOwner
		for _, id := range ownerIDs {
			if scopes[id].allows(img) {
				out.Images[img.ID] = ownerOf(lookup, id)
				break
			}
		}
	}
	return out
}
