package git

import (
	"context"
	"panel/internal/handlers"
)

type Handler struct {
	p *handlers.Panel
}

func New(p *handlers.Panel) *Handler {
	return &Handler{p: p}
}

func (h *Handler) SyncGitAppSource(ctx context.Context, appID string) (string, error) {
	return h.syncGitAppSource(ctx, appID)
}
