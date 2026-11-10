package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"nd/internal/client"
	"nd/internal/ui"
)

func grantPHPDBAccess(cl *client.Client, appID, userRef, dbRef, access string) error {
	access = strings.ToLower(strings.TrimSpace(access))
	if access == "" {
		access = "all"
	}
	if access != "all" && access != "read" {
		return fmt.Errorf("access must be 'all' or 'read' (got %q)", access)
	}

	data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to inspect site DB metadata: %s", client.JSONError(data))
	}

	var siteInfo struct {
		Databases []struct {
			ID     int64  `json:"id"`
			DBName string `json:"db_name"`
		} `json:"databases"`
		DBUsers []struct {
			ID       int64  `json:"id"`
			UserName string `json:"user_name"`
			DBUser   string `json:"db_user"`
		} `json:"db_users"`
		Grants []struct {
			UserID int64  `json:"user_id"`
			DBID   int64  `json:"db_id"`
			Access string `json:"access"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(data, &siteInfo); err != nil {
		return fmt.Errorf("failed to parse site metadata: %w", err)
	}

	var targetUserID int64
	var targetUserName string
	for _, u := range siteInfo.DBUsers {
		uname := u.UserName
		if uname == "" {
			uname = u.DBUser
		}
		if strconv.FormatInt(u.ID, 10) == userRef || strings.EqualFold(uname, userRef) || strings.HasSuffix(strings.ToLower(uname), "_"+strings.ToLower(userRef)) {
			targetUserID = u.ID
			targetUserName = uname
			break
		}
	}
	if targetUserID == 0 {
		return fmt.Errorf("database user %q not found on site %s", userRef, appID)
	}

	var targetDBID int64
	var targetDBName string
	for _, d := range siteInfo.Databases {
		if strconv.FormatInt(d.ID, 10) == dbRef || strings.EqualFold(d.DBName, dbRef) || strings.HasSuffix(strings.ToLower(d.DBName), "_"+strings.ToLower(dbRef)) {
			targetDBID = d.ID
			targetDBName = d.DBName
			break
		}
	}
	if targetDBID == 0 {
		return fmt.Errorf("database %q not found on site %s", dbRef, appID)
	}

	type grantItem struct {
		DBID   int64  `json:"db_id"`
		Access string `json:"access"`
	}
	var nextGrants []grantItem
	replaced := false
	for _, g := range siteInfo.Grants {
		if g.UserID != targetUserID {
			continue
		}
		if g.DBID == targetDBID {
			nextGrants = append(nextGrants, grantItem{DBID: targetDBID, Access: access})
			replaced = true
		} else {
			nextGrants = append(nextGrants, grantItem{DBID: g.DBID, Access: g.Access})
		}
	}
	if !replaced {
		nextGrants = append(nextGrants, grantItem{DBID: targetDBID, Access: access})
	}

	postData, postStatus, postErr := cl.Do("POST", fmt.Sprintf("/api/v1/php/sites/%s/db-users/%d/grants", appID, targetUserID), map[string]any{
		"grants": nextGrants,
	})
	if postErr != nil {
		return postErr
	}
	if postStatus >= 400 {
		return fmt.Errorf("grant failed: %s", client.JSONError(postData))
	}
	ui.Success("Granted %s access on %s to user %s", access, ui.Bold(targetDBName), ui.Bold(targetUserName))
	return nil
}

type phpManifestSite struct {
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	PHPVersion      string          `json:"php_version"`
	Domain          string          `json:"domain"`
	NoDefaultDomain bool            `json:"no_default_domain"`
	CreateDB        bool            `json:"create_db"`
	DBName          string          `json:"db_name"`
	Routing         *phpSiteRouting `json:"routing,omitempty"`
	Htaccess        string          `json:"htaccess,omitempty"`
}

type phpSiteManifest struct {
	Sites []phpManifestSite `json:"sites"`
}

func runPHPImportManifest(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: nd php import-manifest <manifest.json> [--dry-run]")
		fmt.Println("\nManifest JSON format:")
		fmt.Println(`  { "sites": [{ "name": "shop", "type": "php", "php_version": "8.3", "domain": "shop.example.com", "create_db": true, "routing": { ... } }] }`)
		return nil
	}

	filePath := ""
	dryRun := false
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		} else if !strings.HasPrefix(a, "-") && filePath == "" {
			filePath = a
		}
	}
	if filePath == "" {
		return fmt.Errorf("manifest JSON file path is required")
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("could not read manifest %s: %w", filePath, err)
	}

	var manifest phpSiteManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		// Also accept top-level array of site definitions directly.
		var direct []phpManifestSite
		if arrErr := json.Unmarshal(raw, &direct); arrErr != nil {
			return fmt.Errorf("invalid manifest JSON: %w", err)
		}
		manifest.Sites = direct
	}
	if len(manifest.Sites) == 0 {
		return fmt.Errorf("manifest contains zero sites")
	}

	for idx, s := range manifest.Sites {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("site[%d]: name is required", idx)
		}
		sType := s.Type
		if sType == "" {
			sType = "php"
		}
		if dryRun {
			fmt.Printf("[dry-run] #%d name=%s type=%s php=%s domain=%q no_default=%v db=%v/%q\n",
				idx+1, s.Name, sType, s.PHPVersion, s.Domain, s.NoDefaultDomain, s.CreateDB, s.DBName)
			continue
		}

		payload := map[string]any{
			"name":              s.Name,
			"type":              sType,
			"php_version":       s.PHPVersion,
			"domain":            s.Domain,
			"no_default_domain": s.NoDefaultDomain,
			"create_db":         s.CreateDB,
			"db_name":           s.DBName,
		}
		data, status, err := cl.Do("POST", "/api/v1/php/sites", payload)
		if err != nil {
			return fmt.Errorf("site %q failed: %w", s.Name, err)
		}
		if status >= 400 {
			return fmt.Errorf("site %q failed: %s", s.Name, client.JSONError(data))
		}
		var resp struct {
			ID         string `json:"id"`
			AppID      string `json:"app_id"`
			Domain     string `json:"domain"`
			DBName     string `json:"db_name"`
			DBUser     string `json:"db_user"`
			DBPassword string `json:"db_password"`
		}
		_ = json.Unmarshal(data, &resp)
		appID := resp.AppID
		if appID == "" {
			appID = resp.ID
		}
		ui.Success("[%d/%d] Provisioned %s (%s) domain=%s", idx+1, len(manifest.Sites), s.Name, ui.Cyan(appID), resp.Domain)
		if resp.DBName != "" {
			fmt.Printf("    DB: %s | User: %s | Pass: %s\n", resp.DBName, resp.DBUser, resp.DBPassword)
		}

		if s.Routing != nil {
			if err := savePHPRouting(cl, appID, *s.Routing); err != nil {
				return fmt.Errorf("site %q created (%s), but routing failed: %w", s.Name, appID, err)
			}
			fmt.Printf("    Applied %d routing rule(s)\n", len(s.Routing.Rules))
		} else if strings.TrimSpace(s.Htaccess) != "" {
			conv, cErr := convertHtaccessViaAPI(cl, appID, s.Htaccess, ".htaccess")
			if cErr != nil {
				return fmt.Errorf("site %q created (%s), but .htaccess conversion failed: %w", s.Name, appID, cErr)
			}
			routing := phpSiteRouting{FallbackMode: conv.FallbackMode, FallbackEntry: conv.FallbackEntry, Rules: conv.Rules}
			if err := savePHPRouting(cl, appID, routing); err != nil {
				return fmt.Errorf("site %q created (%s), but saving converted rules failed: %w", s.Name, appID, err)
			}
			fmt.Printf("    Converted & applied %d .htaccess rule(s)\n", len(conv.Rules))
		}
	}
	return nil
}
