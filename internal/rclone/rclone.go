package rclone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type GoogleDriveTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func GetGoogleDriveAuthURL(clientID, redirectURL string) string {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURL)
	params.Set("response_type", "code")
	params.Set("scope", "https://www.googleapis.com/auth/drive.file")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
}

func ExchangeGoogleDriveCode(ctx context.Context, clientID, clientSecret, code, redirectURL string) (string, error) {
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURL)
	data.Set("grant_type", "authorization_code")
	
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("token exchange failed: %s", string(body))
	}
	
	var tokenResp GoogleDriveTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}
	
	tokenJSON, err := json.Marshal(map[string]interface{}{
		"access_token":  tokenResp.AccessToken,
		"token_type":    tokenResp.TokenType,
		"refresh_token": tokenResp.RefreshToken,
		"expiry":        time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339),
	})
	if err != nil {
		return "", err
	}
	
	return string(tokenJSON), nil
}

func EnsureGoogleDriveFolder(ctx context.Context, token, folderName string) (string, error) {
	var tokenData map[string]interface{}
	if err := json.Unmarshal([]byte(token), &tokenData); err != nil {
		return "", err
	}
	
	accessToken, ok := tokenData["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("invalid token format")
	}
	
	searchURL := "https://www.googleapis.com/drive/v3/files?q=" + url.QueryEscape(fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", folderName))
	
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	var searchResult struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	
	if err := json.Unmarshal(body, &searchResult); err != nil {
		return "", err
	}
	
	if len(searchResult.Files) > 0 {
		return searchResult.Files[0].ID, nil
	}
	
	createData := map[string]interface{}{
		"name":     folderName,
		"mimeType": "application/vnd.google-apps.folder",
	}
	createJSON, _ := json.Marshal(createData)
	
	req, err = http.NewRequestWithContext(ctx, "POST", "https://www.googleapis.com/drive/v3/files", bytes.NewReader(createJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	
	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	body, _ = io.ReadAll(resp.Body)
	
	var createResult struct {
		ID string `json:"id"`
	}
	
	if err := json.Unmarshal(body, &createResult); err != nil {
		return "", err
	}
	
	return createResult.ID, nil
}

func UploadToGoogleDrive(ctx context.Context, token, folderID, localPath, remoteName string) error {
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("rclone-upload-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	
	configPath := filepath.Join(tmpDir, "rclone.conf")
	configContent := fmt.Sprintf(`[gdrive]
type = drive
scope = drive.file
token = %s
root_folder_id = %s
`, token, folderID)
	
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return err
	}
	
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/config:ro", tmpDir),
		"-v", fmt.Sprintf("%s:/data/%s:ro", localPath, filepath.Base(localPath)),
		"rclone/rclone:latest",
		"--config", "/config/rclone.conf",
		"copy", fmt.Sprintf("/data/%s", filepath.Base(localPath)),
		fmt.Sprintf("gdrive:%s", remoteName))
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("upload failed: %s: %w", stderr.String(), err)
	}
	
	return nil
}

func DownloadFromGoogleDrive(ctx context.Context, token, remotePath string) (string, error) {
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("rclone-download-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return "", err
	}
	
	configPath := filepath.Join(tmpDir, "rclone.conf")
	configContent := fmt.Sprintf(`[gdrive]
type = drive
scope = drive.file
token = %s
`, token)
	
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return "", err
	}
	
	outputPath := filepath.Join(tmpDir, "download")
	if err := os.MkdirAll(outputPath, 0700); err != nil {
		return "", err
	}
	
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/config:ro", tmpDir),
		"-v", fmt.Sprintf("%s:/output", outputPath),
		"rclone/rclone:latest",
		"--config", "/config/rclone.conf",
		"copy", fmt.Sprintf("gdrive:%s", remotePath),
		"/output")
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download failed: %s: %w", stderr.String(), err)
	}
	
	files, err := os.ReadDir(outputPath)
	if err != nil || len(files) == 0 {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("no files downloaded")
	}
	
	downloadedFile := filepath.Join(outputPath, files[0].Name())
	finalPath := filepath.Join(os.TempDir(), files[0].Name())
	
	if err := os.Rename(downloadedFile, finalPath); err != nil {
		data, err := os.ReadFile(downloadedFile)
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", err
		}
		if err := os.WriteFile(finalPath, data, 0600); err != nil {
			os.RemoveAll(tmpDir)
			return "", err
		}
	}
	
	os.RemoveAll(tmpDir)
	return finalPath, nil
}

func UploadToCloudflareR2(ctx context.Context, accountID, accessKeyID, secretAccessKey, bucket, localPath, remoteName string) error {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("rclone-r2-upload-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	
	configPath := filepath.Join(tmpDir, "rclone.conf")
	configContent := fmt.Sprintf(`[r2]
type = s3
provider = Cloudflare
access_key_id = %s
secret_access_key = %s
endpoint = %s
acl = private
`, accessKeyID, secretAccessKey, endpoint)
	
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return err
	}
	
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/config:ro", tmpDir),
		"-v", fmt.Sprintf("%s:/data/%s:ro", localPath, filepath.Base(localPath)),
		"rclone/rclone:latest",
		"--config", "/config/rclone.conf",
		"copy", fmt.Sprintf("/data/%s", filepath.Base(localPath)),
		fmt.Sprintf("r2:%s/%s", bucket, remoteName))
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("upload failed: %s: %w", stderr.String(), err)
	}
	
	return nil
}

func DownloadFromCloudflareR2(ctx context.Context, accountID, accessKeyID, secretAccessKey, bucket, remotePath string) (string, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("rclone-r2-download-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return "", err
	}
	
	configPath := filepath.Join(tmpDir, "rclone.conf")
	configContent := fmt.Sprintf(`[r2]
type = s3
provider = Cloudflare
access_key_id = %s
secret_access_key = %s
endpoint = %s
acl = private
`, accessKeyID, secretAccessKey, endpoint)
	
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return "", err
	}
	
	outputPath := filepath.Join(tmpDir, "download")
	if err := os.MkdirAll(outputPath, 0700); err != nil {
		return "", err
	}
	
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/config:ro", tmpDir),
		"-v", fmt.Sprintf("%s:/output", outputPath),
		"rclone/rclone:latest",
		"--config", "/config/rclone.conf",
		"copy", fmt.Sprintf("r2:%s/%s", bucket, remotePath),
		"/output")
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download failed: %s: %w", stderr.String(), err)
	}
	
	files, err := os.ReadDir(outputPath)
	if err != nil || len(files) == 0 {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("no files downloaded")
	}
	
	downloadedFile := filepath.Join(outputPath, files[0].Name())
	finalPath := filepath.Join(os.TempDir(), files[0].Name())
	
	if err := os.Rename(downloadedFile, finalPath); err != nil {
		data, err := os.ReadFile(downloadedFile)
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", err
		}
		if err := os.WriteFile(finalPath, data, 0600); err != nil {
			os.RemoveAll(tmpDir)
			return "", err
		}
	}
	
	os.RemoveAll(tmpDir)
	return finalPath, nil
}
