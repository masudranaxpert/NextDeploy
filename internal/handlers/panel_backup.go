package handlers

import (
	"strings"

	"panel/internal/handlers/utils"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) BackupPage(c *fiber.Ctx) error {
	redirectURL := strings.TrimRight(p.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
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
	return c.Render("pages/backup", withUser(c, data), "layouts/shell")
}
