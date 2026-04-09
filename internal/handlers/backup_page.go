package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) BackupPage(c *fiber.Ctx) error {
	redirectURL := strings.TrimRight(p.panelBaseURL(c), "/") + "/backup/gdrive/callback"
	flash := readFlash(c)
	// Legacy: ?saved=1 from old bookmarks
	if flash == "" && c.Query("saved") == "1" {
		flash = "saved"
	}
	data := fiber.Map{
		"Nav":         "backup",
		"Flash":       flash,
		"Error":       readFlashError(c),
		"RedirectURL": redirectURL,
	}
	return c.Render("pages/backup", data, "layouts/shell")
}
