package backup

import (
	"encoding/json"
	"fmt"
	"panel/internal/db"
	"panel/internal/handlers"
	"panel/internal/handlers/utils"
	"strings"

	"panel/internal/rclone"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) BackupDestinationsList(c *fiber.Ctx) error {
	u, ok := handlers.CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	var userID *int64
	if u.Role != db.RoleAdmin {
		val := u.ID
		userID = &val
	}
	dests, err := h.P.DB.ListBackupDestinations(c.UserContext(), userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	// Credentials (client_secret, token, access keys) stay server-side; the
	// JSON response only carries display fields the UI actually renders.
	for i := range dests {
		var configMap map[string]string
		if err := json.Unmarshal([]byte(dests[i].Config), &configMap); err != nil {
			dests[i].Config = "{}"
			continue
		}

		switch dests[i].Provider {
		case "r2":
			dests[i].Config = string(mustMarshal(map[string]interface{}{
				"account_id": configMap["account_id"],
				"bucket":     configMap["bucket"],
			}))

		case "gdrive":
			token, hasToken := configMap["token"]
			if !hasToken || token == "" {
				dests[i].Config = string(mustMarshal(map[string]interface{}{
					"folder_id": configMap["folder_id"],
				}))
				continue
			}

			// Proactively refresh token if it is expired or close to expiry
			if rclone.IsTokenExpired(token) {
				if newToken, refreshErr := rclone.RefreshGoogleDriveToken(
					c.UserContext(),
					configMap["client_id"],
					configMap["client_secret"],
					token,
				); refreshErr == nil {
					token = newToken
					configMap["token"] = newToken
					_ = h.P.DB.UpdateBackupDestinationConfig(c.UserContext(), dests[i].ID, string(mustMarshal(configMap)))
				}
			}

			aboutInfo, aboutErr := rclone.GetGoogleDriveAboutInfo(c.UserContext(), token)
			if aboutErr != nil {
				// Attach error hint so UI can show reconnect prompt
				dests[i].Config = string(mustMarshal(map[string]interface{}{
					"folder_id":  configMap["folder_id"],
					"info_error": aboutErr.Error(),
				}))
				continue
			}
			dests[i].Config = string(mustMarshal(map[string]interface{}{
				"folder_id":     configMap["folder_id"],
				"email":         aboutInfo.User.EmailAddress,
				"display_name":  aboutInfo.User.DisplayName,
				"storage_limit": aboutInfo.StorageQuota.Limit,
				"storage_used":  aboutInfo.StorageQuota.Usage,
			}))

		default:
			dests[i].Config = "{}"
		}
	}

	return c.JSON(fiber.Map{"destinations": dests})
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (h *Handler) BackupDestinationCreate(c *fiber.Ctx) error {
	u, ok := handlers.CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	var userID *int64
	if u.Role != db.RoleAdmin {
		val := u.ID
		userID = &val
	}
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

	id, err := h.P.DB.CreateBackupDestination(c.UserContext(), userID, name, provider, string(config))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"id": id, "message": "destination created"})
}

func (h *Handler) BackupDestinationDelete(c *fiber.Ctx) error {
	u, ok := handlers.CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	existing, err := h.P.DB.GetBackupDestination(c.UserContext(), int64(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	if u.Role != db.RoleAdmin && (existing.UserID == nil || *existing.UserID != u.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := h.P.DB.DeleteBackupDestination(c.UserContext(), int64(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "destination deleted"})
}

func (h *Handler) BackupGDriveOAuthURL(c *fiber.Ctx) error {
	clientID := strings.TrimSpace(c.Query("client_id"))
	clientSecret := strings.TrimSpace(c.Query("client_secret"))
	if clientID == "" || clientSecret == "" {
		return c.Status(400).JSON(fiber.Map{"error": "client_id and client_secret required"})
	}

	redirectURL := strings.TrimRight(h.P.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
	authURL := rclone.GetGoogleDriveAuthURL(clientID, redirectURL)

	state := utils.RandomState()
	if err := h.P.DB.SetSetting(c.UserContext(), "gdrive_oauth:"+state, clientID+"\n"+clientSecret); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"auth_url":     authURL + "&state=" + state,
		"redirect_url": redirectURL,
	})
}

func (h *Handler) BackupGDriveCallback(c *fiber.Ctx) error {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	if code == "" || state == "" {
		utils.SetFlashError(c, "Missing callback data")
		return c.Redirect("/backup")
	}

	creds := h.P.DB.GetSetting(c.UserContext(), "gdrive_oauth:"+state)
	if creds == "" {
		utils.SetFlashError(c, "Invalid or expired OAuth state")
		return c.Redirect("/backup")
	}
	_ = h.P.DB.SetSetting(c.UserContext(), "gdrive_oauth:"+state, "")

	parts := strings.SplitN(creds, "\n", 2)
	if len(parts) != 2 {
		utils.SetFlashError(c, "Corrupted OAuth state")
		return c.Redirect("/backup")
	}

	redirectURL := strings.TrimRight(h.P.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
	token, err := rclone.ExchangeGoogleDriveCode(c.UserContext(), parts[0], parts[1], code, redirectURL)
	if err != nil {
		utils.SetFlashError(c, "Google Drive auth failed: "+err.Error())
		return c.Redirect("/backup")
	}

	folderID, err := rclone.EnsureGoogleDriveFolder(c.UserContext(), token, "nextdeploy")
	if err != nil {
		utils.SetFlashError(c, "Could not create Drive folder: "+err.Error())
		return c.Redirect("/backup")
	}

	config, _ := json.Marshal(map[string]string{
		"client_id":     parts[0],
		"client_secret": parts[1],
		"token":         token,
		"folder_id":     folderID,
	})

	name := fmt.Sprintf("Google Drive %s", utils.RandomState()[:6])
	u, ok := handlers.CurrentUser(c)
	var userID *int64
	if ok && u.Role != db.RoleAdmin {
		val := u.ID
		userID = &val
	}
	_, err = h.P.DB.CreateBackupDestination(c.UserContext(), userID, name, "gdrive", string(config))
	if err != nil {
		utils.SetFlashError(c, "Failed to save destination: "+err.Error())
		return c.Redirect("/backup")
	}

	utils.SetFlash(c, "saved")
	return c.Redirect("/backup")
}
