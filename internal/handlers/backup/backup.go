package backup

import "panel/internal/handlers"

type Handler struct {
	P         *handlers.Panel
	backupSem chan struct{}
}

func New(p *handlers.Panel) *Handler {
	return &Handler{P: p, backupSem: make(chan struct{}, 2)}
}
