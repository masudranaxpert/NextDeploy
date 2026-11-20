package handlers

import (
	"context"
	"log"
	"sync"
	"time"

	"panel/internal/dockerapi"
)

// The proxy container is recreated with `docker compose up -d` after panel updates and settings
// changes. When that fails half-way (port still held, mount error, timeout) compose leaves the
// container stopped or removed, and the `unless-stopped` restart policy does not bring back a
// container that was stopped on purpose — every site stays down until someone notices (an hour
// on one production server). The watchdog checks every two minutes and brings the proxy back,
// unless an admin stopped it with the Stop button.

const settingProxyStoppedByAdmin = "proxy_stopped_by_admin"

var (
	proxyWatchMu       sync.Mutex
	proxyWatchRecreate time.Time
	proxyWatchMissing  int
)

// setProxyStoppedByAdmin remembers whether the proxy was stopped on purpose.
func (p *Panel) setProxyStoppedByAdmin(ctx context.Context, stopped bool) {
	val := "0"
	if stopped {
		val = "1"
	}
	_ = p.DB.SetSetting(ctx, settingProxyStoppedByAdmin, val)
}

// ensureProxyRunning starts a stopped proxy container, or re-applies the root compose when the
// container is gone.
func (p *Panel) ensureProxyRunning(ctx context.Context) {
	if p.DB == nil || p.DB.GetSetting(ctx, settingProxyStoppedByAdmin) == "1" {
		return
	}
	name := p.proxyServiceKey()
	exists, err := dockerapi.ContainerExists(ctx, name)
	if err != nil {
		return // Docker itself is unreachable; nothing to fix from here
	}
	if !exists {
		proxyWatchMu.Lock()
		proxyWatchMissing++
		// Twice in a row (4 minutes) so an in-flight engine switch or recreate can finish first,
		// and at most every 10 minutes.
		due := proxyWatchMissing >= 2 && time.Since(proxyWatchRecreate) > 10*time.Minute
		if due {
			proxyWatchRecreate = time.Now()
		}
		proxyWatchMu.Unlock()
		if due && p.nextDeployComposePath() != "" {
			log.Printf("proxy watchdog: container %s is missing; re-creating it from the root compose", name)
			p.applyRootStackComposeBackground(p.nextDeployComposePath(), name)
		}
		return
	}
	proxyWatchMu.Lock()
	proxyWatchMissing = 0
	proxyWatchMu.Unlock()
	info, err := dockerapi.InspectContainerReadiness(ctx, name)
	if err != nil || info.Running || info.Status == "restarting" || info.Status == "created" {
		return
	}
	log.Printf("proxy watchdog: container %s is %s; starting it", name, info.Status)
	if err := dockerapi.StartContainerByName(ctx, name); err != nil {
		log.Printf("proxy watchdog: start %s: %v", name, err)
		return
	}
	_ = dockerapi.EnsureProxySharedNetwork(ctx, name)
}
