package git

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"panel/internal/handlers/utils"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/gitx"

	"github.com/gofiber/fiber/v2"
)

const githubAPIBase = "https://api.github.com"

type githubManifestRequest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	Description        string            `json:"description,omitempty"`
	RedirectURL        string            `json:"redirect_url"`
	SetupURL           string            `json:"setup_url,omitempty"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
	HookAttributes     struct {
		URL    string `json:"url"`
		Active bool   `json:"active"`
	} `json:"hook_attributes"`
}

type githubManifestConversion struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
}

type githubAppInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

type githubInstallationDetail struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

func randomState() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("state-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func uniqueGitHubAppName() string {
	now := time.Now().UTC().Format("2006-01-02")
	suffix := randomState()
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	return fmt.Sprintf("NextDeploy-%s-%s", now, strings.ToLower(suffix))
}

func (h *Handler) githubManifestCallbackURL(c *fiber.Ctx) string {
	return strings.TrimRight(h.p.PanelBaseURL(c), "/") + "/git/github/callback"
}

func (h *Handler) githubSetupURL(c *fiber.Ctx) string {
	return strings.TrimRight(h.p.PanelBaseURL(c), "/") + "/git/github/setup"
}

func (h *Handler) githubManifestWebhookURL(c *fiber.Ctx) string {
	base := strings.TrimRight(h.p.PanelBaseURL(c), "/")
	return base + "/webhooks/github/provider"
}

func (h *Handler) buildGitHubManifest(c *fiber.Ctx, providerName string) githubManifestRequest {
	manifest := githubManifestRequest{
		Name:        providerName,
		URL:         strings.TrimRight(h.p.PanelBaseURL(c), "/"),
		Description: "NextDeploy GitHub App",
		RedirectURL: h.githubManifestCallbackURL(c),
		SetupURL:    h.githubSetupURL(c),
		Public:      false,
		DefaultPermissions: map[string]string{
			"contents":         "read",
			"metadata":         "read",
			"administration":   "write",
			"repository_hooks": "write",
		},
		DefaultEvents: []string{"push"},
	}
	manifest.HookAttributes.URL = h.githubManifestWebhookURL(c)
	manifest.HookAttributes.Active = true
	return manifest
}

func githubAppInstallationURL(appSlug, state string) string {
	appSlug = strings.TrimSpace(appSlug)
	if appSlug == "" {
		return ""
	}
	target := "https://github.com/apps/" + appSlug + "/installations/new"
	if strings.TrimSpace(state) == "" {
		return target
	}
	return target + "?state=" + url.QueryEscape(state)
}

func githubAppInstallURL(appName string) string {
	base := "https://github.com/settings/apps/new"
	if strings.TrimSpace(appName) == "" {
		return base
	}
	return base + "?name=" + url.QueryEscape(appName)
}

func convertGitHubManifestCode(ctx context.Context, code string) (githubManifestConversion, error) {
	var out githubManifestConversion
	if strings.TrimSpace(code) == "" {
		return out, fmt.Errorf("missing manifest code")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubAPIBase+"/app-manifests/"+url.PathEscape(code)+"/conversions", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("github manifest exchange failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func refreshGitHubProviderInstallation(ctx context.Context, detail db.GitHubProviderDetail) (db.GitHubProviderDetail, error) {
	if strings.TrimSpace(detail.InstallationID) != "" {
		jwt, err := gitx.MintGitHubInstallationToken(ctx, detail.GitHubAppID, detail.InstallationID, detail.PrivateKeyPEM)
		if err == nil {
			_ = jwt
			return detail, nil
		}
	}
	appJWT, err := gitxAppJWT(detail.GitHubAppID, detail.PrivateKeyPEM)
	if err != nil {
		return detail, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBase+"/app/installations", nil)
	if err != nil {
		return detail, err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return detail, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return detail, fmt.Errorf("github installation lookup failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var installs []githubAppInstallation
	if err := json.Unmarshal(body, &installs); err != nil {
		return detail, err
	}
	if len(installs) == 0 {
		return detail, nil
	}
	detail.InstallationID = fmt.Sprintf("%d", installs[0].ID)
	detail.AccountLogin = strings.TrimSpace(installs[0].Account.Login)
	return detail, nil
}

func fetchGitHubInstallation(ctx context.Context, detail db.GitHubProviderDetail, installationID string) (githubInstallationDetail, error) {
	var out githubInstallationDetail
	appJWT, err := gitxAppJWT(detail.GitHubAppID, detail.PrivateKeyPEM)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBase+"/app/installations/"+url.PathEscape(strings.TrimSpace(installationID)), nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("github installation verify failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (h *Handler) GitHubAppManifestStart(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = uniqueGitHubAppName()
	}
	state := randomState()
	if err := h.p.DB.SetSetting(c.UserContext(), "github_manifest_state:"+state, name); err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	manifest := h.buildGitHubManifest(c, name)
	body, err := json.Marshal(manifest)
	if err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	target := githubAppInstallURL(name)
	return c.Status(fiber.StatusOK).Type("html").SendString(`
<html><body>
<form id="gh-manifest-form" method="post" action="` + target + `">
  <input type="hidden" name="state" value="` + state + `">
  <input type="hidden" name="manifest" value='` + htmlEscapeAttr(string(body)) + `'>
</form>
<script>document.getElementById('gh-manifest-form').submit();</script>
</body></html>`)
}

func (h *Handler) GitHubAppSetup(c *fiber.Ctx) error {
	installationID := strings.TrimSpace(c.Query("installation_id"))
	setupAction := strings.TrimSpace(c.Query("setup_action"))
	state := strings.TrimSpace(c.Query("state"))

	if installationID == "" {
		utils.SetFlashError(c, "Missing GitHub installation data")
		return c.Redirect("/git")
	}

	if setupAction == "install" && state == "" {
		utils.SetFlash(c, "saved")
		return c.Redirect("/git")
	}

	if state == "" {
		utils.SetFlashError(c, "Missing GitHub installation state")
		return c.Redirect("/git")
	}

	detail, err := h.p.DB.GetGitHubProviderDetailByManifestState(c.UserContext(), state)
	if err != nil {
		utils.SetFlashError(c, "Unknown GitHub installation state")
		return c.Redirect("/git")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 20*time.Second)
	defer cancel()
	verified, err := fetchGitHubInstallation(ctx, detail, installationID)
	if err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	detail.InstallationID = fmt.Sprintf("%d", verified.ID)
	detail.AccountLogin = strings.TrimSpace(verified.Account.Login)
	detail.ManifestState = ""
	if err := h.p.DB.UpsertGitHubProviderDetail(c.UserContext(), detail); err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	utils.SetFlash(c, "saved")
	return c.Redirect("/git")
}

func (h *Handler) GitHubAppManifestCallback(c *fiber.Ctx) error {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	if code == "" || state == "" {
		utils.SetFlashError(c, "Missing GitHub manifest callback data")
		return c.Redirect("/git")
	}
	providerName := strings.TrimSpace(h.p.DB.GetSetting(c.UserContext(), "github_manifest_state:"+state))
	if providerName == "" {
		utils.SetFlashError(c, "Invalid or expired manifest state")
		return c.Redirect("/git")
	}
	_ = h.p.DB.SetSetting(c.UserContext(), "github_manifest_state:"+state, "")

	ctx, cancel := context.WithTimeout(c.UserContext(), 20*time.Second)
	defer cancel()
	converted, err := convertGitHubManifestCode(ctx, code)
	if err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}

	providerID, err := h.p.DB.CreateGitProvider(c.UserContext(), providerName, "github", "", "GitHub App")
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		providers, listErr := h.p.DB.ListGitProviders(c.UserContext())
		if listErr == nil {
			for _, gp := range providers {
				if strings.EqualFold(strings.TrimSpace(gp.Name), providerName) {
					providerID = gp.ID
					err = nil
					break
				}
			}
		}
	}
	if err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}

	detail := db.GitHubProviderDetail{
		ProviderID:         providerID,
		GitHubAppID:        fmt.Sprintf("%d", converted.ID),
		ClientID:           converted.ClientID,
		ClientSecret:       converted.ClientSecret,
		PrivateKeyPEM:      converted.PEM,
		WebhookSecret:      converted.WebhookSecret,
		AppSlug:            converted.Slug,
		ManifestState:      "",
		CreatedViaManifest: true,
	}
	if refreshed, rerr := refreshGitHubProviderInstallation(c.UserContext(), detail); rerr == nil {
		detail = refreshed
	}
	if err := h.p.DB.UpsertGitHubProviderDetail(c.UserContext(), detail); err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	utils.SetFlash(c, "saved")
	return c.Redirect("/git")
}

func (h *Handler) GitHubProviderRefreshInstall(c *fiber.Ctx) error {
	return h.GitHubProviderInstall(c)
}

func (h *Handler) GitHubProviderInstall(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("pid"), 10, 64)
	if err != nil {
		utils.SetFlashError(c, "Invalid provider ID")
		return c.Redirect("/git")
	}
	provider, err := h.p.DB.GetGitProvider(c.UserContext(), id)
	if err != nil {
		utils.SetFlashError(c, "Provider not found")
		return c.Redirect("/git")
	}
	if provider.Provider != "github" {
		utils.SetFlashError(c, "This provider is not GitHub")
		return c.Redirect("/git")
	}
	detail, err := h.p.DB.GetGitHubProviderDetail(c.UserContext(), id)
	if err != nil {
		utils.SetFlashError(c, "GitHub App details not found")
		return c.Redirect("/git")
	}
	if strings.TrimSpace(detail.AppSlug) == "" {
		utils.SetFlashError(c, "GitHub App slug is missing")
		return c.Redirect("/git")
	}
	detail.ManifestState = "gh_setup:" + randomState()
	if err := h.p.DB.UpsertGitHubProviderDetail(c.UserContext(), detail); err != nil {
		utils.SetFlashError(c, err.Error())
		return c.Redirect("/git")
	}
	target := githubAppInstallationURL(detail.AppSlug, detail.ManifestState)
	if target == "" {
		utils.SetFlashError(c, "Could not build GitHub install URL")
		return c.Redirect("/git")
	}
	return c.Redirect(target)
}

func (h *Handler) ProviderGitHubWebhook(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusOK)
}

func htmlEscapeAttr(v string) string {
	var b bytes.Buffer
	for _, r := range v {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (h *Handler) appWebhookURL(c *fiber.Ctx, appID string) string {
	return gitx.WebhookURL(h.p.PanelBaseURL(c), appID)
}

func gitxAppJWT(appID, privateKeyPEM string) (string, error) {
	return gitx.SignGitHubAppJWT(appID, privateKeyPEM, time.Now().UTC())
}
