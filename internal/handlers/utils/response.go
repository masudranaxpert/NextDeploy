package utils

import "github.com/gofiber/fiber/v2"

func RespondAppNotFound(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).SendString("app not found")
}
