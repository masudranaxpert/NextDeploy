package backup

import (
	"panel/internal/handlers"
	"panel/internal/handlers/utils"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) BackupPage(c *fiber.Ctx) error {
	redirectURL := strings.TrimRight(h.P.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
	flash := utils.ReadFlash(c)
	if flash == "" && c.Query("saved") == "1" {
		flash = "saved"
	}
	data := fiber.Map{
		"Nav":         "backup",
		"Flash":       flash,
		"Error":       utils.ReadFlashError(c),
		"RedirectURL": redirectURL,
	}
	return c.Render("pages/backup", handlers.WithUser(c, data), "layouts/shell")
}
