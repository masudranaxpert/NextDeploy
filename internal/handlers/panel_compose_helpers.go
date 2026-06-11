package handlers

import (
	"context"
	"path/filepath"
	"strings"

	"panel/internal/dockerx"
)

func (p *Panel) ContainerBelongsToApp(ctx context.Context, appID, containerName string) bool {
	containerName = strings.TrimSpace(containerName)
	containerName = strings.TrimPrefix(containerName, "/")
	if containerName == "" {
		return false
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return false
	}
	candidates := p.composeProjectCandidates(ctx, app, appID)
	prefixMatch := false
	for _, project := range candidates {
		// Compose names containers "<project>-<service>-<n>" (v1: underscores).
		// Prefix match instead of substring so project "app" cannot claim "myapp-web-1".
		if project != "" && (containerName == project ||
			strings.HasPrefix(containerName, project+"-") ||
			strings.HasPrefix(containerName, project+"_")) {
			prefixMatch = true
			break
		}
	}

	proj, workDir, ierr := dockerx.ContainerComposeLabels(ctx, containerName)
	if ierr != nil {
		// Container not inspectable; fall back to the name prefix heuristic.
		return prefixMatch
	}
	// Compose always records the deploy directory; that label is authoritative, so a
	// legacy slug collision (two users with same-named apps) cannot cross workspaces.
	if strings.TrimSpace(workDir) != "" {
		return composeWorkspaceDirContainedInApp(filepath.Clean(p.Store.Path(appID)), workDir)
	}
	if proj != "" {
		for _, c := range candidates {
			if proj == c {
				return true
			}
		}
		return false
	}
	return prefixMatch
}

func (p *Panel) containerBelongsToApp(ctx context.Context, appID, containerName string) bool {
	return p.ContainerBelongsToApp(ctx, appID, containerName)
}

func (p *Panel) ComposeServiceBelongsToApp(ctx context.Context, appID, service string) bool {
	service = strings.TrimSpace(service)
	if service == "" {
		return false
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return false
	}
	_, rows, res := p.ComposeProjectAndPS(ctx, app, appID)
	if !res.OK {
		return false
	}
	for _, row := range rows {
		if strings.TrimSpace(row.Service) == service {
			return true
		}
	}
	return false
}

func (p *Panel) composeServiceBelongsToApp(ctx context.Context, appID, service string) bool {
	return p.ComposeServiceBelongsToApp(ctx, appID, service)
}

func (p *Panel) ComposeWorkspaceDirContainedInApp(appRoot, workDir string) bool {
	return composeWorkspaceDirContainedInApp(appRoot, workDir)
}
