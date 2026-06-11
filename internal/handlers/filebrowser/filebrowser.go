package filebrowser

import "panel/internal/handlers"

type Handler struct {
	p *handlers.Panel
}

func New(p *handlers.Panel) *Handler {
	return &Handler{p: p}
}
