package handlers

import (
	"encoding/json"
	"fmt"
	"strings"

	"panel/internal/rclone"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) BackupDestinationsList(c *fiber.Ctx) error {
	dests, err := p.DB.ListBackupDestinations(c.UserContext())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"destinations": dests})
}

func (p *Panel) BackupDestinationCreate(c *fiber.Ctx) error {
	provider := strings.TrimSpace(c.FormValue("provider"))
	name := strings.TrimSpace(c.FormValue("name"))

	if provider == "" || name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "provider and name required"})
	}

	var config string
	var err error

	switch provider {
	case "gdrive":
		clientID := strings.TrimSpace(c.FormValue("client_id"))
		clientSecret := strings.TrimSpace(c.FormValue("client_secret"))
		if clientID == "" || clientSecret == "" {
			return c.Status(400).JSON(fiber.Map{"error": "client_id and client_secret required"})
		}
		configBytes, err := json.Marshal(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
		})
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		config = string(configBytes)

	case "r2":
		accountID := strings.TrimSpace(c.FormValue("account_id"))
		accessKeyID := strings.TrimSpace(c.FormValue("access_key_id"))
		secretAccessKey := strings.TrimSpace(c.FormValue("secret_access_key"))
		bucket := strings.TrimSpace(c.FormValue("bucket"))
		if accountID == "" || accessKeyID == "" || secretAccessKey == "" || bucket == "" {
			return c.Status(400).JSON(fiber.Map{"error": "all R2 fields required"})
		}
		configBytes, err := json.Marshal(map[string]string{
			"account_id":        accountID,
			"access_key_id":     accessKeyID,
			"secret_access_key": secretAccessKey,
			"bucket":            bucket,
		})
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		config = string(configBytes)

	default:
		return c.Status(400).JSON(fiber.Map{"error": "unsupported provider"})
	}

	id, err := p.DB.CreateBackupDestination(c.UserContext(), name, provider, string(config))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"id": id, "message": "destination created"})
}

func (p *Panel) BackupDestinationDelete(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	if err := p.DB.DeleteBackupDestination(c.UserContext(), int64(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "destination deleted"})
}

func (p *Panel) BackupGDriveOAuthURL(c *fiber.Ctx) error {
	clientID := strings.TrimSpace(c.Query("client_id"))
	clientSecret := strings.TrimSpace(c.Query("client_secret"))
	if clientID == "" || clientSecret == "" {
		return c.Status(400).JSON(fiber.Map{"error": "client_id and client_secret required"})
	}

	redirectURL := strings.TrimRight(p.panelBaseURL(c), "/") + "/backup/gdrive/callback"
	authURL := rclone.GetGoogleDriveAuthURL(clientID, redirectURL)

	state := randomState()
	if err := p.DB.SetSetting(c.UserContext(), "gdrive_oauth:"+state, clientID+"\n"+clientSecret); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"auth_url":     authURL + "&state=" + state,
		"redirect_url": redirectURL,
	})
}

func (p *Panel) BackupGDriveCallback(c *fiber.Ctx) error {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	if code == "" || state == "" {
		return c.Redirect("/settings?tab=backup&error=missing+callback+data")
	}

	creds := p.DB.GetSetting(c.UserContext(), "gdrive_oauth:"+state)
	if creds == "" {
		return c.Redirect("/settings?tab=backup&error=invalid+state")
	}
	_ = p.DB.SetSetting(c.UserContext(), "gdrive_oauth:"+state, "")

	parts := strings.SplitN(creds, "\n", 2)
	if len(parts) != 2 {
		return c.Redirect("/settings?tab=backup&error=corrupted+state")
	}

	redirectURL := strings.TrimRight(p.panelBaseURL(c), "/") + "/backup/gdrive/callback"
	token, err := rclone.ExchangeGoogleDriveCode(c.UserContext(), parts[0], parts[1], code, redirectURL)
	if err != nil {
		return c.Redirect("/settings?tab=backup&error=" + err.Error())
	}

	folderID, err := rclone.EnsureGoogleDriveFolder(c.UserContext(), token, "nextdeploy")
	if err != nil {
		return c.Redirect("/settings?tab=backup&error=" + err.Error())
	}

	config, _ := json.Marshal(map[string]string{
		"client_id":     parts[0],
		"client_secret": parts[1],
		"token":         token,
		"folder_id":     folderID,
	})

	name := fmt.Sprintf("Google Drive %s", randomState()[:6])
	_, err = p.DB.CreateBackupDestination(c.UserContext(), name, "gdrive", string(config))
	if err != nil {
		return c.Redirect("/settings?tab=backup&error=" + err.Error())
	}

	return c.Redirect("/settings?tab=backup&saved=1")
}
