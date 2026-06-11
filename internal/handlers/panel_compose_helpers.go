package handlers

import (
	"context"
	"path/filepath"
	"strings"

	"panel/internal/dockerx"
)

func (p *Panel) ContainerBelongsToApp(ctx context.Context, appID, containerName string) bool {
	containerName = strings.TrimSpace(containerName)
	if containerName == "" {
		return false
	}
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		return strings.Contains(containerName, appID)
	}
	candidates := p.composeProjectCandidates(ctx, app, appID)
	for _, project := range candidates {
		if project != "" && strings.Contains(containerName, project) {
			return true
		}
	}
	proj, workDir, ierr := dockerx.ContainerComposeLabels(ctx, containerName)
	if ierr != nil {
		return false
	}
	appRoot := filepath.Clean(p.AppSourcePath(ctx, appID))
	if composeWorkspaceDirContainedInApp(appRoot, workDir) {
		return true
	}
	for _, c := range candidates {
		if proj != "" && proj == c {
			return true
		}
	}
	return false
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
