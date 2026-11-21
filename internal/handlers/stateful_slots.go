package handlers

import (
	"context"
	"log"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
)

// statefulServicesOf lists the app's services that keep data in a shared named volume (databases)
// and so must never run in two slots at the same time. See caddy.StatefulServices.
func (p *Panel) statefulServicesOf(ctx context.Context, app db.App, appID string) []string {
	base, _, ok := p.EffectiveBaseCompose(ctx, app, appID)
	if !ok {
		return nil
	}
	return caddy.StatefulServices(base)
}

// hasDirectHostPort reports whether any binding uses direct host-port publishing
// (which cannot overlap between Blue and Green slots).
func hasDirectHostPort(bindings []caddy.HostPortBinding) bool {
	for _, b := range bindings {
		if b.Direct {
			return true
		}
	}
	return false
}

// stopOtherSlotsForStateful stops every slot except targetSlot when the app has stateful
// services or direct host-bound ports, so switching slots (promote, rollback) never runs
// two copies on the same volume or collides on host ports. It returns a restore function
// that starts currentSlot again if the switch fails; it is a no-op when nothing was stopped.
func (p *Panel) stopOtherSlotsForStateful(ctx context.Context, app db.App, appID, targetSlot, currentSlot, dir string, envFiles []string, forceStop ...bool) func() {
	mustStop := len(p.statefulServicesOf(ctx, app, appID)) > 0
	for _, f := range forceStop {
		if f {
			mustStop = true
			break
		}
	}
	if !mustStop {
		return func() {}
	}
	for _, other := range []string{"blue", "green"} {
		if other == targetSlot {
			continue
		}
		proj := p.SlotProjectName(app, appID, other)
		paths := p.EffectiveComposePathsForSlot(ctx, app, appID, other)
		_ = dockerx.ComposeDown(ctx, dir, paths, proj, nil, envFiles)
		_ = dockerapi.RemoveContainersByComposeProject(ctx, proj)
	}
	if currentSlot == "" || currentSlot == targetSlot {
		return func() {}
	}
	return func() {
		_, _ = p.SyncAppSlotComposeCtx(ctx, appID, currentSlot, false)
		proj := p.SlotProjectName(app, appID, currentSlot)
		paths := p.EffectiveComposePathsForSlot(ctx, app, appID, currentSlot)
		if res := dockerx.ComposeUpNoBuild(ctx, dir, paths, proj, nil, envFiles); !res.OK {
			log.Printf("slot switch: could not restart slot %s of %s: %s", currentSlot, appID, res.Output)
		}
	}
}
