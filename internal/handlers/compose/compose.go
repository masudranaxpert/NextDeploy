package compose

import "panel/internal/handlers"

type Handler struct {
	P *handlers.Panel
}

func New(p *handlers.Panel) *Handler {
	return &Handler{P: p}
}
