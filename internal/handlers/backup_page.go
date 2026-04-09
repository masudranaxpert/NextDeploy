package handlers

import "github.com/gofiber/fiber/v2"

func (p *Panel) BackupPage(c *fiber.Ctx) error {
	data := fiber.Map{
		"Nav":   "backup",
		"Flash": c.Query("saved"),
	}
	return c.Render("pages/backup", data, "layouts/shell")
}
