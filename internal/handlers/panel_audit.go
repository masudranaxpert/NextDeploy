package handlers

import (
	"context"
	"log"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

// RecordAuditLog asynchronously records an audit event in the database.
func (p *Panel) RecordAuditLog(c *fiber.Ctx, action, targetType, targetID, details string) {
	userID := int64(0)
	username := "system"

	if c != nil {
		if u, ok := c.Locals("auth_user").(db.User); ok {
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
