package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const gitlabAPIBase = "https://gitlab.com"

// gitlabCallbackURL returns the OAuth redirect_uri for this panel instance.
func (p *Panel) gitlabCallbackURL(c *fiber.Ctx) string {
	return strings.TrimRight(p.panelBaseURL(c), "/") + "/git/gitlab/callback"
}

// GitLabOAuthStart initiates the GitLab OAuth Authorization Code flow.
// POST /git/gitlab/start  — form fields: client_id, client_secret, name (optional)
func (p *Panel) GitLabOAuthStart(c *fiber.Ctx) error {
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	clientSecret := strings.TrimSpace(c.FormValue("client_secret"))
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = "GitLab"
	}
	if clientID == "" || clientSecret == "" {
		return c.Redirect("/git?error=Application+ID+and+Secret+are+required")
	}

	state := randomState()
	// Store client_id + client_secret + name under this state key.
	stateVal := fmt.Sprintf("%s\n%s\n%s", clientID, clientSecret, name)
	if err := p.DB.SetSetting(c.UserContext(), "gitlab_oauth_state:"+state, stateVal); err != nil {
		return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
	}

	params := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {p.gitlabCallbackURL(c)},
		"response_type": {"code"},
		"state":         {state},
		"scope":         {"read_user api read_repository"},
	}
	target := gitlabAPIBase + "/oauth/authorize?" + params.Encode()
	return c.Redirect(target)
}

// GitLabOAuthCallback handles the redirect from GitLab after user authorization.
// GET /git/gitlab/callback?code=...&state=...
func (p *Panel) GitLabOAuthCallback(c *fiber.Ctx) error {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	errParam := strings.TrimSpace(c.Query("error"))
	if errParam != "" {
		desc := strings.TrimSpace(c.Query("error_description"))
		if desc == "" {
			desc = errParam
		}
		return c.Redirect("/git?error=" + url.QueryEscape("GitLab denied access: "+desc))
	}
	if code == "" || state == "" {
		return c.Redirect("/git?error=Missing+GitLab+callback+data")
	}

	stateVal := strings.TrimSpace(p.DB.GetSetting(c.UserContext(), "gitlab_oauth_state:"+state))
	if stateVal == "" {
		return c.Redirect("/git?error=Unknown+or+expired+GitLab+OAuth+state")
	}
	// Clear state immediately (one-time use).
	_ = p.DB.SetSetting(c.UserContext(), "gitlab_oauth_state:"+state, "")

	parts := strings.SplitN(stateVal, "\n", 3)
	if len(parts) != 3 {
		return c.Redirect("/git?error=Corrupted+OAuth+state")
	}
	clientID, clientSecret, name := parts[0], parts[1], parts[2]

	ctx, cancel := context.WithTimeout(c.UserContext(), 20*time.Second)
	defer cancel()

	token, err := exchangeGitLabCode(ctx, clientID, clientSecret, code, p.gitlabCallbackURL(c))
	if err != nil {
		return c.Redirect("/git?error=" + url.QueryEscape("GitLab token exchange failed: "+err.Error()))
	}

	// Check if a provider with this name already exists and update it, else create.
	providers, _ := p.DB.ListGitProviders(c.UserContext())
	var existingID int64
	for _, gp := range providers {
		if strings.EqualFold(strings.TrimSpace(gp.Name), name) && gp.Provider == "gitlab" {
			existingID = gp.ID
			break
		}
	}

	if existingID != 0 {
		if err := p.DB.UpdateGitProvider(c.UserContext(), existingID, name, "gitlab", token, "GitLab OAuth App"); err != nil {
			return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
		}
	} else {
		if _, err := p.DB.CreateGitProvider(c.UserContext(), name, "gitlab", token, "GitLab OAuth App"); err != nil {
			return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
		}
	}

	return c.Redirect("/git?saved=1")
}

type gitlabTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func exchangeGitLabCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (string, error) {
	params := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		gitlabAPIBase+"/oauth/token",
		strings.NewReader(params.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out gitlabTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("unexpected response: %s", strings.TrimSpace(string(body)))
	}
	if out.Error != "" {
		msg := out.ErrorDesc
		if msg == "" {
			msg = out.Error
		}
		return "", fmt.Errorf("%s", msg)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response: %s", strings.TrimSpace(string(body)))
	}
	return out.AccessToken, nil
}
