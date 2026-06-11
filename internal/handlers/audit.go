package handlers

import (
	"context"
	"log"
	"panel/internal/db"
	"time"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) RecordAuditLog(c *fiber.Ctx, action, targetType, targetID, details string) {
	userID := int64(0)
	username := "system"

	if c != nil {
		if u, ok := c.Locals(contextUserKey).(db.User); ok {
			userID = u.ID
			username = u.Username
		} else {
			username = "anonymous"
		}
	}

	ipAddress := ""
	userAgent := ""
	if c != nil {
		ipAddress = c.IP()
		userAgent = c.Get("User-Agent")
	}

	auditLog := db.AuditLog{
		UserID:     userID,
		Username:   username,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		IPAddress:  ipAddress,
		UserAgent:  userAgent,
		Details:    details,
		CreatedAt:  time.Now(),
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.DB.CreateAuditLog(ctx, auditLog); err != nil {
			log.Printf("failed to write audit log: %v", err)
		}
	}()
}

func (p *Panel) AuditLogsPage(c *fiber.Ctx) error {
	logs, err := p.DB.ListAuditLogs(c.UserContext())
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.Render("pages/audit_logs", withUser(c, fiber.Map{
		"Nav":   "settings",
		"Title": "Audit Logs",
		"Logs":  logs,
	}), "layouts/shell")
}
