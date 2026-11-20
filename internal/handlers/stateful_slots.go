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

// stopOtherSlotsForStateful stops every slot except targetSlot when the app has stateful
// services, so switching slots (promote, rollback) never runs the database twice on the same
// data. It returns a restore function that starts currentSlot again if the switch fails; it is a
// no-op when nothing was stopped.
func (p *Panel) stopOtherSlotsForStateful(ctx context.Context, app db.App, appID, targetSlot, currentSlot, dir string, envFiles []string) func() {
	if len(p.statefulServicesOf(ctx, app, appID)) == 0 {
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
