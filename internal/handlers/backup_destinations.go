package handlers

import (
	"encoding/json"
	"fmt"
	"panel/internal/db"
	"strconv"
	"strings"

	"panel/internal/rclone"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) BackupDestinationsList(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	var userID *int64
	if u.Role != db.RoleAdmin {
		val := u.ID
		userID = &val
	}
	dests, err := p.DB.ListBackupDestinations(c.UserContext(), userID)
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

			clientID := strings.TrimSpace(configMap["client_id"])
			clientSecret := strings.TrimSpace(configMap["client_secret"])
			if clientID == "" {
				clientID = rclone.RcloneDefaultGDriveClientID
				clientSecret = rclone.RcloneDefaultGDriveClientSecret
			}

			aboutInfo, newToken, aboutErr := rclone.GetGoogleDriveAboutInfo(c.UserContext(), clientID, clientSecret, token)
			if newToken != token && newToken != "" {
				configMap["token"] = newToken
				_ = p.DB.UpdateBackupDestinationConfig(c.UserContext(), dests[i].ID, string(mustMarshal(configMap)))
			}
			if aboutErr != nil {
				// Fall back to saved identity if available
				savedEmail := configMap["email"]
				if savedEmail != "" {
					dests[i].Config = string(mustMarshal(map[string]interface{}{
						"folder_id":     configMap["folder_id"],
						"auth_mode":     configMap["auth_mode"],
						"email":         savedEmail,
						"display_name":  configMap["display_name"],
						"storage_limit": configMap["storage_limit"],
						"storage_used":  configMap["storage_used"],
					}))
					continue
				}
				// Attach error hint so UI can show reconnect prompt
				dests[i].Config = string(mustMarshal(map[string]interface{}{
					"folder_id":  configMap["folder_id"],
					"auth_mode":  configMap["auth_mode"],
					"info_error": aboutErr.Error(),
				}))
				continue
			}

			email := aboutInfo.User.EmailAddress
			displayName := aboutInfo.User.DisplayName
			storageLimit := aboutInfo.StorageQuota.Limit
			storageUsed := aboutInfo.StorageQuota.Usage

			if email != "" && email != "Authorized" {
				configMap["email"] = email
				configMap["display_name"] = displayName
				if storageLimit != "0" && storageLimit != "" {
					configMap["storage_limit"] = storageLimit
					configMap["storage_used"] = storageUsed
				}
				_ = p.DB.UpdateBackupDestinationConfig(c.UserContext(), dests[i].ID, string(mustMarshal(configMap)))
			} else {
				if email == "" || email == "Authorized" {
					email = configMap["email"]
				}
				if displayName == "" || displayName == "Authorized" {
					displayName = configMap["display_name"]
				}
				if storageLimit == "0" || storageLimit == "" {
					storageLimit = configMap["storage_limit"]
				}
				if storageUsed == "0" || storageUsed == "" {
					storageUsed = configMap["storage_used"]
				}
			}

			dests[i].Config = string(mustMarshal(map[string]interface{}{
				"folder_id":     configMap["folder_id"],
				"auth_mode":     configMap["auth_mode"],
				"email":         email,
				"display_name":  displayName,
				"storage_limit": storageLimit,
				"storage_used":  storageUsed,
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

func (p *Panel) BackupDestinationCreate(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	val := u.ID
	userID := &val // every destination is owned by its creator, admins included
	provider := strings.TrimSpace(c.FormValue("provider"))
	name := strings.TrimSpace(c.FormValue("name"))

	if provider == "" || name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "provider and name required"})
	}

	var config string
	var err error

	switch provider {
	case "gdrive":
		rawToken := strings.TrimSpace(c.FormValue("token"))
		rawCode := strings.TrimSpace(c.FormValue("code"))
		clientID := strings.TrimSpace(c.FormValue("client_id"))
		clientSecret := strings.TrimSpace(c.FormValue("client_secret"))
		folderID := strings.TrimSpace(c.FormValue("folder_id"))

		if rawCode != "" {
			cleanCode := rclone.ExtractGoogleAuthCode(rawCode)
			redirectURI := strings.TrimSpace(c.FormValue("redirect_uri"))
			authMode := "rclone_quick"
			if clientID == "" || clientSecret == "" {
				clientID = rclone.RcloneDefaultGDriveClientID
				clientSecret = rclone.RcloneDefaultGDriveClientSecret
				redirectURI = rclone.RcloneDefaultGDriveRedirectURI
			} else {
				authMode = "custom_oauth"
				if redirectURI == "" {
					redirectURI = strings.TrimRight(p.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
				}
			}
			token, terr := rclone.ExchangeGoogleDriveCode(c.UserContext(), clientID, clientSecret, cleanCode, redirectURI)
			if terr != nil {
				return c.Status(400).JSON(fiber.Map{"error": "Google Drive token exchange failed: " + terr.Error()})
			}
			if folderID == "" {
				if fid, ferr := rclone.EnsureGoogleDriveFolder(c.UserContext(), token, "nextdeploy"); ferr == nil && fid != "" {
					folderID = fid
				}
			}
			cfgMap := map[string]string{
				"client_id":     clientID,
				"client_secret": clientSecret,
				"token":         token,
				"folder_id":     folderID,
				"auth_mode":     authMode,
			}
			configBytes, merr := json.Marshal(cfgMap)
			if merr != nil {
				return c.Status(500).JSON(fiber.Map{"error": merr.Error()})
			}
			config = string(configBytes)
		} else if rawToken != "" {
			cleanToken, terr := rclone.ExtractRcloneTokenJSON(rawToken)
			if terr != nil {
				return c.Status(400).JSON(fiber.Map{"error": terr.Error()})
			}
			if folderID == "" {
				if ensuredID, ferr := rclone.EnsureGoogleDriveFolder(c.UserContext(), cleanToken, "nextdeploy"); ferr == nil && ensuredID != "" {
					folderID = ensuredID
				}
			}
			cfgMap := map[string]string{
				"token":     cleanToken,
				"folder_id": folderID,
				"auth_mode": "rclone_token",
			}
			if clientID != "" && clientSecret != "" {
				cfgMap["client_id"] = clientID
				cfgMap["client_secret"] = clientSecret
				cfgMap["auth_mode"] = "custom_oauth"
			}
			configBytes, merr := json.Marshal(cfgMap)
			if merr != nil {
				return c.Status(500).JSON(fiber.Map{"error": merr.Error()})
			}
			config = string(configBytes)
		} else {
			if clientID == "" || clientSecret == "" {
				return c.Status(400).JSON(fiber.Map{"error": "Provide an authorization code, Rclone Drive token JSON, or Google OAuth Client ID and Secret"})
			}
			configBytes, merr := json.Marshal(map[string]string{
				"client_id":     clientID,
				"client_secret": clientSecret,
				"auth_mode":     "custom_oauth",
			})
			if merr != nil {
				return c.Status(500).JSON(fiber.Map{"error": merr.Error()})
			}
			config = string(configBytes)
		}

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

	// Reconnect/Update if destination id was specified
	if editID := strings.TrimSpace(c.FormValue("id")); editID != "" {
		if idNum, parseErr := strconv.ParseInt(editID, 10, 64); parseErr == nil && idNum > 0 {
			if existing, gerr := p.DB.GetBackupDestination(c.UserContext(), idNum); gerr == nil && existing.ID > 0 {
				if u.Role == db.RoleAdmin || (existing.UserID != nil && *existing.UserID == u.ID) {
					if uerr := p.DB.UpdateBackupDestinationConfig(c.UserContext(), idNum, config); uerr == nil {
						return c.JSON(fiber.Map{"id": idNum, "message": "destination updated"})
					}
				}
			}
		}
	}

	id, err := p.DB.CreateBackupDestination(c.UserContext(), userID, name, provider, string(config))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"id": id, "message": "destination created"})
}

func (p *Panel) BackupDestinationDelete(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	existing, err := p.DB.GetBackupDestination(c.UserContext(), int64(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	if u.Role != db.RoleAdmin && (existing.UserID == nil || *existing.UserID != u.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := p.DB.DeleteBackupDestination(c.UserContext(), int64(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "destination deleted"})
}

// BackupDestinationTest tests active connectivity without modifying destination metadata.
func (p *Panel) BackupDestinationTest(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	dest, err := p.DB.GetBackupDestination(c.UserContext(), int64(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}
	if u.Role != db.RoleAdmin && (dest.UserID == nil || *dest.UserID != u.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(dest.Config), &configMap); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid destination configuration"})
	}

	switch dest.Provider {
	case "gdrive":
		token := configMap["token"]
		if token == "" {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "Google Drive destination has no token configured"})
		}
		clientID := configMap["client_id"]
		clientSecret := configMap["client_secret"]
		about, newToken, testErr := rclone.TestGoogleDrive(c.UserContext(), clientID, clientSecret, token)
		if testErr != nil {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": testErr.Error()})
		}
		if about.User.EmailAddress != "" && about.User.EmailAddress != "Authorized" {
			configMap["email"] = about.User.EmailAddress
			configMap["display_name"] = about.User.DisplayName
			if about.StorageQuota.Limit != "0" && about.StorageQuota.Limit != "" {
				configMap["storage_limit"] = about.StorageQuota.Limit
				configMap["storage_used"] = about.StorageQuota.Usage
			}
			_ = p.DB.UpdateBackupDestinationConfig(c.UserContext(), dest.ID, string(mustMarshal(configMap)))
		} else if newToken != token && newToken != "" {
			configMap["token"] = newToken
			_ = p.DB.UpdateBackupDestinationConfig(c.UserContext(), dest.ID, string(mustMarshal(configMap)))
		}
		return c.JSON(fiber.Map{
			"ok":            true,
			"message":       fmt.Sprintf("Connected as %s (%s)", about.User.DisplayName, about.User.EmailAddress),
			"email":         about.User.EmailAddress,
			"display_name":  about.User.DisplayName,
			"storage_used":  about.StorageQuota.Usage,
			"storage_limit": about.StorageQuota.Limit,
			"folder_id":     configMap["folder_id"],
		})

	case "r2":
		accountID := configMap["account_id"]
		accessKeyID := configMap["access_key_id"]
		secretAccessKey := configMap["secret_access_key"]
		bucket := configMap["bucket"]
		if accountID == "" || accessKeyID == "" || secretAccessKey == "" || bucket == "" {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": "incomplete R2 configuration"})
		}
		if testErr := rclone.TestCloudflareR2(c.UserContext(), accountID, accessKeyID, secretAccessKey, bucket); testErr != nil {
			return c.Status(400).JSON(fiber.Map{"ok": false, "error": testErr.Error()})
		}
		return c.JSON(fiber.Map{
			"ok":      true,
			"message": fmt.Sprintf("Successfully connected to Cloudflare R2 bucket %q", bucket),
			"bucket":  bucket,
		})

	default:
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "unsupported destination provider"})
	}
}

// BackupGDriveQuickAuthURL generates an OAuth URL using Rclone's default client credentials
func (p *Panel) BackupGDriveQuickAuthURL(c *fiber.Ctx) error {
	authURL := rclone.GetGoogleDriveAuthURLWithScope(
		rclone.RcloneDefaultGDriveClientID,
		rclone.RcloneDefaultGDriveRedirectURI,
		"https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile",
	)
	return c.JSON(fiber.Map{
		"ok":           true,
		"auth_url":     authURL,
		"redirect_uri": rclone.RcloneDefaultGDriveRedirectURI,
		"client_id":    rclone.RcloneDefaultGDriveClientID,
	})
}

// BackupGDriveExchangeCode exchanges an authorization code or redirected URL for tokens and creates/updates the destination
func (p *Panel) BackupGDriveExchangeCode(c *fiber.Ctx) error {
	u, ok := CurrentUser(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}
	val := u.ID
	userID := &val

	rawCode := strings.TrimSpace(c.FormValue("code"))
	name := strings.TrimSpace(c.FormValue("name"))
	folderID := strings.TrimSpace(c.FormValue("folder_id"))

	if rawCode == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Authorization code or redirected URL is required"})
	}
	cleanCode := rclone.ExtractGoogleAuthCode(rawCode)
	if cleanCode == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Failed to parse authorization code from the provided input"})
	}

	clientID := strings.TrimSpace(c.FormValue("client_id"))
	clientSecret := strings.TrimSpace(c.FormValue("client_secret"))
	redirectURI := strings.TrimSpace(c.FormValue("redirect_uri"))

	authMode := "rclone_quick"
	if clientID == "" || clientSecret == "" {
		clientID = rclone.RcloneDefaultGDriveClientID
		clientSecret = rclone.RcloneDefaultGDriveClientSecret
		redirectURI = rclone.RcloneDefaultGDriveRedirectURI
	} else {
		authMode = "custom_oauth"
		if redirectURI == "" {
			redirectURI = strings.TrimRight(p.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
		}
	}

	token, err := rclone.ExchangeGoogleDriveCode(c.UserContext(), clientID, clientSecret, cleanCode, redirectURI)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Google Drive token exchange failed: " + err.Error()})
	}

	if folderID == "" {
		if fid, ferr := rclone.EnsureGoogleDriveFolder(c.UserContext(), token, "nextdeploy"); ferr == nil && fid != "" {
			folderID = fid
		}
	}

	cfgMap := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"token":         token,
		"folder_id":     folderID,
		"auth_mode":     authMode,
	}
	configBytes, merr := json.Marshal(cfgMap)
	if merr != nil {
		return c.Status(500).JSON(fiber.Map{"error": merr.Error()})
	}

	if name == "" {
		name = "Google Drive"
	}

	// Update existing if ID passed (reconnect flow)
	if editID := strings.TrimSpace(c.FormValue("id")); editID != "" {
		if idNum, parseErr := strconv.ParseInt(editID, 10, 64); parseErr == nil && idNum > 0 {
			if existing, gerr := p.DB.GetBackupDestination(c.UserContext(), idNum); gerr == nil && existing.ID > 0 {
				if u.Role == db.RoleAdmin || (existing.UserID != nil && *existing.UserID == u.ID) {
					if uerr := p.DB.UpdateBackupDestinationConfig(c.UserContext(), idNum, string(configBytes)); uerr == nil {
						return c.JSON(fiber.Map{
							"ok":        true,
							"id":        idNum,
							"message":   "Google Drive reconnected successfully",
							"folder_id": folderID,
						})
					}
				}
			}
		}
	}

	id, err := p.DB.CreateBackupDestination(c.UserContext(), userID, name, "gdrive", string(configBytes))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"ok":        true,
		"id":        id,
		"message":   "Google Drive connected successfully",
		"folder_id": folderID,
	})
}

func (p *Panel) BackupGDriveOAuthURL(c *fiber.Ctx) error {
	clientID := strings.TrimSpace(c.Query("client_id"))
	clientSecret := strings.TrimSpace(c.Query("client_secret"))
	if clientID == "" || clientSecret == "" {
		return c.Status(400).JSON(fiber.Map{"error": "client_id and client_secret required"})
	}

	redirectURL := strings.TrimRight(p.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
	authURL := rclone.GetGoogleDriveAuthURL(clientID, redirectURL)

	state := RandomState()
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
		SetFlashError(c, "Missing callback data")
		return c.Redirect("/backup")
	}

	creds := p.DB.GetSetting(c.UserContext(), "gdrive_oauth:"+state)
	if creds == "" {
		SetFlashError(c, "Invalid or expired OAuth state")
		return c.Redirect("/backup")
	}
	_ = p.DB.SetSetting(c.UserContext(), "gdrive_oauth:"+state, "")

	parts := strings.SplitN(creds, "\n", 2)
	if len(parts) != 2 {
		SetFlashError(c, "Corrupted OAuth state")
		return c.Redirect("/backup")
	}

	redirectURL := strings.TrimRight(p.PanelBaseURL(c), "/") + "/backup/gdrive/callback"
	token, err := rclone.ExchangeGoogleDriveCode(c.UserContext(), parts[0], parts[1], code, redirectURL)
	if err != nil {
		SetFlashError(c, "Google Drive auth failed: "+err.Error())
		return c.Redirect("/backup")
	}

	folderID, err := rclone.EnsureGoogleDriveFolder(c.UserContext(), token, "nextdeploy")
	if err != nil {
		SetFlashError(c, "Could not create Drive folder: "+err.Error())
		return c.Redirect("/backup")
	}

	config, _ := json.Marshal(map[string]string{
		"client_id":     parts[0],
		"client_secret": parts[1],
		"token":         token,
		"folder_id":     folderID,
	})

	name := fmt.Sprintf("Google Drive %s", RandomState()[:6])
	u, ok := CurrentUser(c)
	var userID *int64
	if ok {
		val := u.ID
		userID = &val
	}
	_, err = p.DB.CreateBackupDestination(c.UserContext(), userID, name, "gdrive", string(config))
	if err != nil {
		SetFlashError(c, "Failed to save destination: "+err.Error())
		return c.Redirect("/backup")
	}

	SetFlash(c, "saved")
	return c.Redirect("/backup")
}
