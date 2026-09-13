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

// AuditLogsPage renders paginated audit logs.
func (p *Panel) AuditLogsPage(c *fiber.Ctx) error {
	limit := 20
	page := c.QueryInt("page", 1)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	total, err := p.DB.CountAuditLogs(c.UserContext())
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	logs, err := p.DB.ListAuditLogs(c.UserContext(), limit, offset)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
		offset = (page - 1) * limit
		logs, err = p.DB.ListAuditLogs(c.UserContext(), limit, offset)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
	}

	// Sliding window of page numbers around the current page (max 7 entries).
	windowStart := page - 3
	if windowStart < 1 {
		windowStart = 1
	}
	windowEnd := windowStart + 6
	if windowEnd > totalPages {
		windowEnd = totalPages
		windowStart = windowEnd - 6
		if windowStart < 1 {
			windowStart = 1
		}
	}
	pages := make([]int, 0, windowEnd-windowStart+1)
	for i := windowStart; i <= windowEnd; i++ {
		pages = append(pages, i)
	}

	showFrom := 0
	showTo := 0
	if total > 0 {
		showFrom = offset + 1
		showTo = offset + len(logs)
	}

	return c.Render("pages/audit_logs", withUser(c, fiber.Map{
		"Nav":        "audit-logs",
		"Title":      "Audit Logs",
		"Logs":       logs,
		"Page":       page,
		"TotalPages": totalPages,
		"TotalLogs":  total,
		"HasPrev":    page > 1,
		"HasNext":    page < totalPages,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
		"Pages":      pages,
		"ShowFrom":   showFrom,
		"ShowTo":     showTo,
		"FirstInWin": windowStart,
		"LastInWin":  windowEnd,
		"PageSize":   limit,
	}), "layouts/shell")
}
