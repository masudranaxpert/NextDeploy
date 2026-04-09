package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

const gitlabAPIBase = "https://gitlab.com"

func (p *Panel) gitlabCallbackURL(c *fiber.Ctx) string {
	return strings.TrimRight(p.panelBaseURL(c), "/") + "/git/gitlab/callback"
}

func (p *Panel) uniqueGitLabProviderName(ctx context.Context, base string) string {
	if base == "" {
		base = "GitLab"
	}
	providers, err := p.DB.ListGitProviders(ctx)
	if err != nil {
		return base
	}
	taken := make(map[string]bool, len(providers))
	for _, gp := range providers {
		taken[strings.ToLower(strings.TrimSpace(gp.Name))] = true
	}
	if !taken[strings.ToLower(base)] {
		return base
	}
	for i := 2; i <= 99; i++ {
		candidate := fmt.Sprintf("%s %d", base, i)
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%s", base, randomState()[:6])
}

func (p *Panel) GitLabOAuthStart(c *fiber.Ctx) error {
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	clientSecret := strings.TrimSpace(c.FormValue("client_secret"))
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = p.uniqueGitLabProviderName(c.UserContext(), "GitLab")
	}
	if clientID == "" || clientSecret == "" {
		return c.Redirect("/git?error=Application+ID+and+Secret+are+required")
	}

	state := randomState()
	stateVal := fmt.Sprintf("%s\n%s\n%s", clientID, clientSecret, name)
	if err := p.DB.SetSetting(c.UserContext(), "gitlab_oauth_state:"+state, stateVal); err != nil {
		return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
	}
	if err := p.DB.SetSetting(c.UserContext(), "gitlab_client:"+state, clientID+"\n"+clientSecret); err != nil {
		return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
	}

	params := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {p.gitlabCallbackURL(c)},
		"response_type": {"code"},
		"state":         {state},
		"scope":         {"read_user api read_repository"},
	}
	return c.Redirect(gitlabAPIBase + "/oauth/authorize?" + params.Encode())
}

func (p *Panel) GitLabOAuthCallback(c *fiber.Ctx) error {
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	errParam := strings.TrimSpace(c.Query("error"))
	if errParam != "" {
		desc := strings.TrimSpace(c.Query("error_description"))
		if desc == "" {
			desc = errParam
		}
		return c.Redirect("/git?error=" + url.QueryEscape("GitLab denied: "+desc))
	}
	if code == "" || state == "" {
		return c.Redirect("/git?error=Missing+callback+data")
	}

	stateVal := strings.TrimSpace(p.DB.GetSetting(c.UserContext(), "gitlab_oauth_state:"+state))
	if stateVal == "" {
		return c.Redirect("/git?error=Unknown+or+expired+state")
	}
	_ = p.DB.SetSetting(c.UserContext(), "gitlab_oauth_state:"+state, "")

	parts := strings.SplitN(stateVal, "\n", 3)
	if len(parts) != 3 {
		return c.Redirect("/git?error=Corrupted+state")
	}
	clientID, clientSecret, name := parts[0], parts[1], parts[2]

	ctx, cancel := context.WithTimeout(c.UserContext(), 20*time.Second)
	defer cancel()

	tokenResp, err := exchangeGitLabCode(ctx, clientID, clientSecret, code, p.gitlabCallbackURL(c))
	if err != nil {
		return c.Redirect("/git?error=" + url.QueryEscape("Token exchange failed: "+err.Error()))
	}

	expiresAt := time.Now().Unix() + int64(tokenResp.ExpiresIn)

	providers, _ := p.DB.ListGitProviders(c.UserContext())
	var existingID int64
	for _, gp := range providers {
		if strings.EqualFold(strings.TrimSpace(gp.Name), name) && gp.Provider == "gitlab" {
			existingID = gp.ID
			break
		}
	}

	if existingID != 0 {
		if err := p.DB.UpdateGitProviderTokens(c.UserContext(), existingID, tokenResp.AccessToken, tokenResp.RefreshToken, expiresAt); err != nil {
			return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
		}
		if err := p.DB.SetSetting(c.UserContext(), fmt.Sprintf("gitlab_client:%d", existingID), clientID+"\n"+clientSecret); err != nil {
			return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
		}
	} else {
		providerID, err := p.DB.CreateGitProviderWithRefresh(c.UserContext(), name, "gitlab", tokenResp.AccessToken, tokenResp.RefreshToken, expiresAt, "GitLab OAuth App")
		if err != nil {
			return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
		}
		if err := p.DB.SetSetting(c.UserContext(), fmt.Sprintf("gitlab_client:%d", providerID), clientID+"\n"+clientSecret); err != nil {
			return c.Redirect("/git?error=" + url.QueryEscape(err.Error()))
		}
	}
	_ = p.DB.SetSetting(c.UserContext(), "gitlab_client:"+state, "")
	return c.Redirect("/git?saved=1")
}

type gitlabProject struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	FullName          string `json:"name_with_namespace"`
	PathWithNamespace string `json:"path_with_namespace"`
	HTTPURLToRepo     string `json:"http_url_to_repo"`
	DefaultBranch     string `json:"default_branch"`
	Visibility        string `json:"visibility"`
}

type gitlabBranch struct {
	Name string `json:"name"`
}

func gitlabAPIRequest(ctx context.Context, method, endpoint, token string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Panel) AppGitLabProviderRepos(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	pid, err := strconv.ParseInt(c.Params("pid"), 10, 64)
	if err != nil || pid <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid provider"})
	}
	token, err := p.EnsureFreshGitLabToken(c.UserContext(), pid)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "token refresh failed: " + err.Error()})
	}
	if token == "" {
		return c.Status(400).JSON(fiber.Map{"error": "no token — reconnect provider"})
	}
	endpoint := gitlabAPIBase + "/api/v4/projects?membership=true&per_page=100&order_by=last_activity_at&sort=desc"
	body, status, err := gitlabAPIRequest(c.UserContext(), http.MethodGet, endpoint, token)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if status >= 300 {
		return c.Status(status).JSON(fiber.Map{"error": "GitLab API error: " + strings.TrimSpace(string(body))})
	}
	var projects []gitlabProject
	if err := json.Unmarshal(body, &projects); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "parse error"})
	}
	type repoRow struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		CloneURL      string `json:"clone_url"`
		HTTPURLToRepo string `json:"http_url_to_repo"`
		DefaultBranch string `json:"default_branch"`
	}
	rows := make([]repoRow, 0, len(projects))
	for _, pr := range projects {
		db := pr.DefaultBranch
		if db == "" {
			db = "main"
		}
		rows = append(rows, repoRow{
			ID:            pr.ID,
			Name:          pr.Name,
			FullName:      pr.PathWithNamespace,
			CloneURL:      pr.HTTPURLToRepo,
			HTTPURLToRepo: pr.HTTPURLToRepo,
			DefaultBranch: db,
		})
	}
	return c.JSON(fiber.Map{"repos": rows})
}

func (p *Panel) AppGitLabProviderBranches(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	pid, err := strconv.ParseInt(c.Params("pid"), 10, 64)
	if err != nil || pid <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid provider"})
	}
	token, err := p.EnsureFreshGitLabToken(c.UserContext(), pid)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "token refresh failed: " + err.Error()})
	}
	if token == "" {
		return c.Status(400).JSON(fiber.Map{"error": "no token"})
	}
	repoPath := url.PathEscape(strings.TrimSpace(c.Query("repo")))
	if repoPath == "" {
		return c.Status(400).JSON(fiber.Map{"error": "repo required"})
	}
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/repository/branches?per_page=100", gitlabAPIBase, repoPath)
	body, status, err := gitlabAPIRequest(c.UserContext(), http.MethodGet, endpoint, token)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if status >= 300 {
		return c.Status(status).JSON(fiber.Map{"error": "GitLab API error: " + strings.TrimSpace(string(body))})
	}
	var branches []gitlabBranch
	if err := json.Unmarshal(body, &branches); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "parse error"})
	}
	return c.JSON(fiber.Map{"branches": branches})
}

type gitlabTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func exchangeGitLabCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (gitlabTokenResponse, error) {
	params := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		gitlabAPIBase+"/oauth/token", strings.NewReader(params.Encode()))
	if err != nil {
		return gitlabTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return gitlabTokenResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out gitlabTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return gitlabTokenResponse{}, fmt.Errorf("unexpected response: %s", strings.TrimSpace(string(body)))
	}
	if out.Error != "" {
		msg := out.ErrorDesc
		if msg == "" {
			msg = out.Error
		}
		return gitlabTokenResponse{}, fmt.Errorf("%s", msg)
	}
	if out.AccessToken == "" {
		return gitlabTokenResponse{}, fmt.Errorf("no access_token in response")
	}
	return out, nil
}


func (p *Panel) refreshGitLabToken(ctx context.Context, provider db.GitProvider) (string, error) {
	now := time.Now().Unix()
	if provider.ExpiresAt > now+60 {
		return provider.Token, nil
	}
	if provider.RefreshToken == "" {
		return "", fmt.Errorf("no refresh token available")
	}

	v, _ := p.gitlabTokenMu.LoadOrStore(provider.ID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	freshProvider, err := p.DB.GetGitProvider(ctx, provider.ID)
	if err != nil {
		return "", err
	}
	if freshProvider.ExpiresAt > now+60 {
		return freshProvider.Token, nil
	}

	clientCreds := p.DB.GetSetting(ctx, fmt.Sprintf("gitlab_client:%d", provider.ID))
	if clientCreds == "" {
		return "", fmt.Errorf("client credentials not found - reconnect provider")
	}
	parts := strings.SplitN(clientCreds, "\n", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid credentials - reconnect provider")
	}

	params := url.Values{
		"client_id":     {parts[0]},
		"client_secret": {parts[1]},
		"refresh_token": {freshProvider.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		gitlabAPIBase+"/oauth/token", strings.NewReader(params.Encode()))
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

	var tokenResp gitlabTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	if tokenResp.Error != "" {
		if tokenResp.Error == "invalid_grant" {
			return "", fmt.Errorf("refresh token invalid - reconnect provider")
		}
		return "", fmt.Errorf("refresh failed: %s", tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("no access token in response")
	}

	newExpiresAt := time.Now().Unix() + int64(tokenResp.ExpiresIn)
	if err := p.DB.UpdateGitProviderTokens(ctx, provider.ID, tokenResp.AccessToken, tokenResp.RefreshToken, newExpiresAt); err != nil {
		return "", err
	}
	return tokenResp.AccessToken, nil
}

func (p *Panel) EnsureFreshGitLabToken(ctx context.Context, providerID int64) (string, error) {
	provider, err := p.DB.GetGitProvider(ctx, providerID)
	if err != nil {
		return "", err
	}

	if provider.Provider != "gitlab" {
		return provider.Token, nil
	}

	return p.refreshGitLabToken(ctx, provider)
}
