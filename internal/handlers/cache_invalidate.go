package handlers

import (
	"panel/internal/sysinfo"
	"panel/internal/volumex"
)

func (p *Panel) InvalidateAfterUserChange() {
	p.InvalidateMonitorResourceCache()
}

func (p *Panel) InvalidateAfterAppWorkspaceChange(appID string) {
	if appID == "" {
		return
	}
	p.InvalidateAppStorageCache(appID)
}

func (p *Panel) InvalidateAfterDockerChange() {
	volumex.InvalidateSharedMatcher()
	sysinfo.InvalidateDiskCache()
	go p.refreshMonitorSnapshot()
}

func (p *Panel) InvalidateAfterAppDeployChange(appID string) {
	p.InvalidateAfterAppWorkspaceChange(appID)
	p.InvalidateAfterDockerChange()
}
