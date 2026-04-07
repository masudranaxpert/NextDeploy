package handlers

import "github.com/gofiber/fiber/v2"

func respondAppNotFound(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).SendString("app not found")
}
