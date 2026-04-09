package rclone

import (
	"bufio"
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
	"regexp"
	"strings"
	"time"
)

func rclonePath() string {
	if p := strings.TrimSpace(os.Getenv("RCLONE_BIN")); p != "" {
		return p
	}
	return "rclone"
}

func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	const k = 1024
	f := float64(n)
	switch {
	case n < k*k:
		return fmt.Sprintf("%.1f KiB", f/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MiB", f/(k*k))
	default:
		return fmt.Sprintf("%.2f GiB", f/(k*k*k))
	}
}

func skipRcloneNoiseLine(line string) bool {
	if strings.Contains(line, " DEBUG :") {
		return true
	}
	if strings.Contains(line, " NOTICE: Config file") {
		return true
	}
	if strings.Contains(line, "fs cache:") {
		return true
	}
	if strings.Contains(line, "go routines active") {
		return true
	}
	if strings.Contains(line, "Setting ") && strings.Contains(line, "environment variable") {
		return true
	}
	return false
}

// readRcloneStderrFiltered streams stderr: returns raw (for verification) and compact (no DEBUG noise).
// onFlush receives redacted compact text periodically for live DB updates.
func readRcloneStderrFiltered(r io.Reader, onFlush func(redactedCompact string)) (raw string, compact string) {
	var rawB, compactB strings.Builder
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)
	lastFlush := time.Now()
	for scanner.Scan() {
		line := scanner.Text()
		rawB.WriteString(line)
		rawB.WriteByte('\n')
		if skipRcloneNoiseLine(line) {
			continue
		}
		compactB.WriteString(line)
		compactB.WriteByte('\n')
		if onFlush != nil && time.Since(lastFlush) > 750*time.Millisecond {
			onFlush(filterSensitiveData(strings.TrimSpace(compactB.String())))
			lastFlush = time.Now()
		}
	}
	if onFlush != nil {
		onFlush(filterSensitiveData(strings.TrimSpace(compactB.String())))
	}
	return rawB.String(), compactB.String()
}

// RemoteObjectExists checks that a file is present on the remote (used to validate history rows).
func RemoteObjectExists(ctx context.Context, provider string, config map[string]string, remotePath string) bool {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return false
	}
	var cmd *exec.Cmd
	switch provider {
	case "gdrive":
		cmd = exec.CommandContext(ctx, rclonePath(), "ls", "gdrive:"+remotePath)
		cmd.Env = append(os.Environ(), rcloneEnvGoogleDrive(
			config["client_id"], config["client_secret"], config["token"], config["folder_id"])...)
	case "r2":
		endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", config["account_id"])
		cmd = exec.CommandContext(ctx, rclonePath(), "ls", fmt.Sprintf("r2:%s/%s", config["bucket"], remotePath))
		cmd.Env = append(os.Environ(), rcloneEnvR2(config["account_id"], config["access_key_id"], config["secret_access_key"], endpoint)...)
	default:
		return false
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(out.String()) != ""
}

// DeleteRemoteObject removes one exact backup object from the remote.
// Missing files are treated as already deleted.
func DeleteRemoteObject(ctx context.Context, provider string, config map[string]string, remotePath string) error {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return nil
	}
	if !RemoteObjectExists(ctx, provider, config, remotePath) {
		return nil
	}
	var cmd *exec.Cmd
	switch provider {
	case "gdrive":
		// --drive-use-trash=false bypasses Google Drive trash and permanently deletes the file immediately.
		cmd = exec.CommandContext(ctx, rclonePath(), "deletefile", "gdrive:"+remotePath, "--drive-use-trash=false")
		cmd.Env = append(os.Environ(), rcloneEnvGoogleDrive(
			config["client_id"], config["client_secret"], config["token"], config["folder_id"])...)
	case "r2":
		endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", config["account_id"])
		cmd = exec.CommandContext(ctx, rclonePath(), "deletefile", fmt.Sprintf("r2:%s/%s", config["bucket"], remotePath))
		cmd.Env = append(os.Environ(), rcloneEnvR2(config["account_id"], config["access_key_id"], config["secret_access_key"], endpoint)...)
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("delete remote object: %s", strings.TrimSpace(out.String()))
	}
	return nil
}

func rcloneEnvGoogleDrive(clientID, clientSecret, token, folderID string) []string {
	out := []string{
		"RCLONE_CONFIG_GDRIVE_TYPE=drive",
		"RCLONE_CONFIG_GDRIVE_SCOPE=drive.file",
		fmt.Sprintf("RCLONE_CONFIG_GDRIVE_CLIENT_ID=%s", clientID),
		fmt.Sprintf("RCLONE_CONFIG_GDRIVE_CLIENT_SECRET=%s", clientSecret),
		fmt.Sprintf("RCLONE_CONFIG_GDRIVE_TOKEN=%s", token),
	}
	if strings.TrimSpace(folderID) != "" {
		out = append(out, fmt.Sprintf("RCLONE_CONFIG_GDRIVE_ROOT_FOLDER_ID=%s", folderID))
	}
	return out
}

func rcloneEnvR2(accountID, accessKeyID, secretAccessKey, endpoint string) []string {
	return []string{
		"RCLONE_CONFIG_R2_TYPE=s3",
		"RCLONE_CONFIG_R2_PROVIDER=Cloudflare",
		fmt.Sprintf("RCLONE_CONFIG_R2_ACCESS_KEY_ID=%s", accessKeyID),
		fmt.Sprintf("RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=%s", secretAccessKey),
		fmt.Sprintf("RCLONE_CONFIG_R2_ENDPOINT=%s", endpoint),
		"RCLONE_CONFIG_R2_ACL=private",
	}
}

var rcloneZeroTransferredRE = regexp.MustCompile(`(?i)Transferred:\s+0(\.\d+)?\s*(B|Byte|bytes|KiB|MiB|GiB|TiB)\s*/\s*0(\.\d+)?\s*(B|Byte|bytes|KiB|MiB|GiB|TiB)`)

// lastTransferredLine returns the last stderr line containing "Transferred:" (final stats, not intermediate progress).
func lastTransferredLine(stderrStr, stdoutStr string) string {
	combined := stderrStr + "\n" + stdoutStr
	var last string
	for _, line := range strings.Split(combined, "\n") {
		if strings.Contains(line, "Transferred:") {
			last = strings.TrimSpace(line)
		}
	}
	return last
}

func verifyRcloneTransferred(stderrStr, stdoutStr string) error {
	combined := stderrStr + "\n" + stdoutStr
	if strings.Contains(combined, "There was nothing to transfer") {
		return fmt.Errorf("no files were transferred - upload failed")
	}
	last := lastTransferredLine(stderrStr, stdoutStr)
	if last == "" {
		return fmt.Errorf("upload verification failed: missing transfer stats in rclone output")
	}
	// Only the final Transferred line counts (avoids false positives from intermediate "0 B / X MiB" stats).
	if rcloneZeroTransferredRE.MatchString(last) {
		return fmt.Errorf("upload verification failed - 0 bytes transferred")
	}
	return nil
}

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

// UploadToGoogleDrive uploads one file; onProgress receives redacted compact stderr snapshots during transfer (optional).
func UploadToGoogleDrive(ctx context.Context, clientID, clientSecret, token, folderID, localPath, remoteName string, onProgress func(string)) (string, error) {
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("local file not found: %w", err)
	}
	remoteDir := filepath.Dir(remoteName)
	header := fmt.Sprintf("[upload] → gdrive:%s | %s local", remoteDir, humanBytes(fileInfo.Size()))

	cmd := exec.CommandContext(ctx, rclonePath(),
		"copy", localPath, fmt.Sprintf("gdrive:%s", remoteDir),
		"--log-level", "INFO",
		"--stats", "4s",
		"--retries", "3",
	)
	cmd.Env = append(os.Environ(), rcloneEnvGoogleDrive(clientID, clientSecret, token, folderID)...)
	cmd.Stdout = io.Discard

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return header + "\n", err
	}
	if err := cmd.Start(); err != nil {
		return header + "\n", err
	}
	rawStderr, compactStderr := readRcloneStderrFiltered(stderrPipe, onProgress)
	waitErr := cmd.Wait()

	outLog := header + "\n" + strings.TrimSpace(filterSensitiveData(compactStderr))
	if waitErr != nil {
		return outLog + "\n", fmt.Errorf("upload failed: %w — %s", waitErr, filterSensitiveData(truncateStr(rawStderr, 2000)))
	}
	if err := verifyRcloneTransferred(rawStderr, ""); err != nil {
		return outLog + "\n", err
	}
	return outLog + "\nDone.", nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func filterSensitiveData(input string) string {
	re := regexp.MustCompile(`"access_token":"[^"]*"`)
	output := re.ReplaceAllString(input, `"access_token":"[REDACTED]"`)
	re = regexp.MustCompile(`"refresh_token":"[^"]*"`)
	output = re.ReplaceAllString(output, `"refresh_token":"[REDACTED]"`)
	re = regexp.MustCompile(`client_secret="[^"]*"`)
	output = re.ReplaceAllString(output, `client_secret="[REDACTED]"`)
	re = regexp.MustCompile(`(RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=)\S+`)
	output = re.ReplaceAllString(output, `${1}[REDACTED]`)
	re = regexp.MustCompile(`(RCLONE_CONFIG_GDRIVE_TOKEN=)\S+`)
	output = re.ReplaceAllString(output, `${1}{...REDACTED...}`)
	re = regexp.MustCompile(`ya29\.[A-Za-z0-9_-]+`)
	output = re.ReplaceAllString(output, `[TOKEN_REDACTED]`)
	re = regexp.MustCompile(`1//[A-Za-z0-9_-]+`)
	output = re.ReplaceAllString(output, `[REFRESH_TOKEN_REDACTED]`)
	re = regexp.MustCompile(`GOCSPX-[A-Za-z0-9_-]+`)
	output = re.ReplaceAllString(output, `[CLIENT_SECRET_REDACTED]`)
	return output
}

func DownloadFromGoogleDrive(ctx context.Context, clientID, clientSecret, token, folderID, remotePath string) (string, error) {
	base := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if base == "" {
		base = os.TempDir()
	}
	// One directory per download so the caller can os.RemoveAll(dir) after restore (avoids /tmp leaks).
	jobDir := filepath.Join(base, "rclone-temp", fmt.Sprintf("dl-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(jobDir, 0700); err != nil {
		return "", err
	}

	outDir := filepath.Join(jobDir, "out")
	if err := os.MkdirAll(outDir, 0700); err != nil {
		os.RemoveAll(jobDir)
		return "", err
	}

	// remotePath is relative to RCLONE_CONFIG_GDRIVE_ROOT_FOLDER_ID (same layout as upload).
	cmd := exec.CommandContext(ctx, rclonePath(), "copy", fmt.Sprintf("gdrive:%s", remotePath), outDir)
	cmd.Env = append(os.Environ(), rcloneEnvGoogleDrive(clientID, clientSecret, token, folderID)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.RemoveAll(jobDir)
		return "", fmt.Errorf("download failed: %s: %w", stderr.String(), err)
	}

	files, err := os.ReadDir(outDir)
	if err != nil || len(files) == 0 {
		os.RemoveAll(jobDir)
		return "", fmt.Errorf("no files downloaded")
	}

	src := filepath.Join(outDir, files[0].Name())
	finalPath := filepath.Join(jobDir, files[0].Name())
	if err := os.Rename(src, finalPath); err != nil {
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			os.RemoveAll(jobDir)
			return "", rerr
		}
		if err := os.WriteFile(finalPath, data, 0600); err != nil {
			os.RemoveAll(jobDir)
			return "", err
		}
	}
	_ = os.RemoveAll(outDir)
	return finalPath, nil
}

func UploadToCloudflareR2(ctx context.Context, accountID, accessKeyID, secretAccessKey, bucket, localPath, remoteName string, onProgress func(string)) (string, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("local file not found: %w", err)
	}
	remoteDir := filepath.Dir(remoteName)
	header := fmt.Sprintf("[upload] → r2:%s/%s | %s local", bucket, remoteDir, humanBytes(fileInfo.Size()))

	cmd := exec.CommandContext(ctx, rclonePath(),
		"copy", localPath, fmt.Sprintf("r2:%s/%s", bucket, remoteDir),
		"--log-level", "INFO",
		"--stats", "4s",
		"--retries", "3",
	)
	cmd.Env = append(os.Environ(), rcloneEnvR2(accountID, accessKeyID, secretAccessKey, endpoint)...)
	cmd.Stdout = io.Discard

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return header + "\n", err
	}
	if err := cmd.Start(); err != nil {
		return header + "\n", err
	}
	rawStderr, compactStderr := readRcloneStderrFiltered(stderrPipe, onProgress)
	waitErr := cmd.Wait()

	outLog := header + "\n" + strings.TrimSpace(filterSensitiveData(compactStderr))
	if waitErr != nil {
		return outLog + "\n", fmt.Errorf("upload failed: %w — %s", waitErr, filterSensitiveData(truncateStr(rawStderr, 2000)))
	}
	if err := verifyRcloneTransferred(rawStderr, ""); err != nil {
		return outLog + "\n", err
	}
	return outLog + "\nDone.", nil
}

func DownloadFromCloudflareR2(ctx context.Context, accountID, accessKeyID, secretAccessKey, bucket, remotePath string) (string, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	base := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if base == "" {
		base = os.TempDir()
	}
	jobDir := filepath.Join(base, "rclone-temp", fmt.Sprintf("r2-dl-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(jobDir, 0700); err != nil {
		return "", err
	}

	outDir := filepath.Join(jobDir, "out")
	if err := os.MkdirAll(outDir, 0700); err != nil {
		os.RemoveAll(jobDir)
		return "", err
	}

	cmd := exec.CommandContext(ctx, rclonePath(), "copy", fmt.Sprintf("r2:%s/%s", bucket, remotePath), outDir)
	cmd.Env = append(os.Environ(), rcloneEnvR2(accountID, accessKeyID, secretAccessKey, endpoint)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.RemoveAll(jobDir)
		return "", fmt.Errorf("download failed: %s: %w", stderr.String(), err)
	}

	files, err := os.ReadDir(outDir)
	if err != nil || len(files) == 0 {
		os.RemoveAll(jobDir)
		return "", fmt.Errorf("no files downloaded")
	}

	src := filepath.Join(outDir, files[0].Name())
	finalPath := filepath.Join(jobDir, files[0].Name())
	if err := os.Rename(src, finalPath); err != nil {
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			os.RemoveAll(jobDir)
			return "", rerr
		}
		if err := os.WriteFile(finalPath, data, 0600); err != nil {
			os.RemoveAll(jobDir)
			return "", err
		}
	}
	_ = os.RemoveAll(outDir)
	return finalPath, nil
}

type GoogleDriveAboutInfo struct {
	User struct {
		EmailAddress string `json:"emailAddress"`
		DisplayName  string `json:"displayName"`
	} `json:"user"`
	StorageQuota struct {
		Limit string `json:"limit"`
		Usage string `json:"usage"`
	} `json:"storageQuota"`
}

// RefreshGoogleDriveToken uses the refresh_token to get a new access_token.
// clientID and clientSecret are required. Returns updated token JSON string.
func RefreshGoogleDriveToken(ctx context.Context, clientID, clientSecret, tokenJSON string) (string, error) {
	var tokenData map[string]interface{}
	if err := json.Unmarshal([]byte(tokenJSON), &tokenData); err != nil {
		return "", err
	}
	refreshToken, _ := tokenData["refresh_token"].(string)
	if refreshToken == "" {
		return "", fmt.Errorf("no refresh_token available")
	}

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")

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
		return "", fmt.Errorf("token refresh failed: %s", string(body))
	}

	var tr GoogleDriveTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", err
	}

	// Preserve existing refresh_token if Google doesn't return a new one
	if tr.RefreshToken == "" {
		tr.RefreshToken = refreshToken
	}
	updated, err := json.Marshal(map[string]interface{}{
		"access_token":  tr.AccessToken,
		"token_type":    tr.TokenType,
		"refresh_token": tr.RefreshToken,
		"expiry":        time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).Format(time.RFC3339),
	})
	if err != nil {
		return "", err
	}
	return string(updated), nil
}

// IsTokenExpired returns true if the token's expiry is within 60 seconds.
func IsTokenExpired(tokenJSON string) bool {
	var tokenData map[string]interface{}
	if err := json.Unmarshal([]byte(tokenJSON), &tokenData); err != nil {
		return true
	}
	expiryStr, _ := tokenData["expiry"].(string)
	if expiryStr == "" {
		return true
	}
	expiry, err := time.Parse(time.RFC3339, expiryStr)
	if err != nil {
		return true
	}
	return time.Until(expiry) < 60*time.Second
}

func GetGoogleDriveAboutInfo(ctx context.Context, token string) (*GoogleDriveAboutInfo, error) {
	var tokenData map[string]interface{}
	if err := json.Unmarshal([]byte(token), &tokenData); err != nil {
		return nil, err
	}

	accessToken, ok := tokenData["access_token"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid token format")
	}

	doRequest := func(at string) (*GoogleDriveAboutInfo, int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/drive/v3/about?fields=user,storageQuota", nil)
		if err != nil {
			return nil, 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+at)
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, nil, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return nil, resp.StatusCode, body, nil
		}
		var info GoogleDriveAboutInfo
		if err := json.Unmarshal(body, &info); err != nil {
			return nil, resp.StatusCode, body, err
		}
		return &info, resp.StatusCode, body, nil
	}

	info, status, body, err := doRequest(accessToken)
	if err != nil {
		return nil, err
	}
	if info != nil {
		return info, nil
	}

	// Token expired (401) — try refresh if we have client credentials
	if status == 401 {
		clientID, _ := tokenData["client_id"].(string)
		clientSecret, _ := tokenData["client_secret"].(string)
		if clientID != "" && clientSecret != "" {
			newToken, refreshErr := RefreshGoogleDriveToken(ctx, clientID, clientSecret, token)
			if refreshErr == nil {
				var newTokenData map[string]interface{}
				if json.Unmarshal([]byte(newToken), &newTokenData) == nil {
					if newAt, ok := newTokenData["access_token"].(string); ok {
						info2, _, _, err2 := doRequest(newAt)
						if err2 == nil && info2 != nil {
							return info2, nil
						}
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("failed to get about info (HTTP %d): %s", status, string(body))
}

func SearchGoogleDriveFile(ctx context.Context, token, folderID, filePath string) (string, error) {
	var tokenData map[string]interface{}
	if err := json.Unmarshal([]byte(token), &tokenData); err != nil {
		return "", err
	}
	
	accessToken, ok := tokenData["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("invalid token format")
	}
	
	// Split path into parts (e.g., "apps/volumes/file.tar.gz" -> ["apps", "volumes", "file.tar.gz"])
	parts := strings.Split(filePath, "/")
	currentFolderID := folderID
	
	// Navigate through folders
	for i, part := range parts {
		isLastPart := i == len(parts)-1
		
		var mimeTypeQuery string
		if isLastPart {
			// Last part is the file
			mimeTypeQuery = ""
		} else {
			// Intermediate parts are folders
			mimeTypeQuery = " and mimeType='application/vnd.google-apps.folder'"
		}
		
		query := fmt.Sprintf("name='%s' and '%s' in parents and trashed=false%s", part, currentFolderID, mimeTypeQuery)
		searchURL := "https://www.googleapis.com/drive/v3/files?q=" + url.QueryEscape(query) + "&fields=files(id,name,webViewLink)"
		
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
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}

		var searchResult struct {
			Files []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				WebViewLink string `json:"webViewLink"`
			} `json:"files"`
		}
		
		if err := json.Unmarshal(body, &searchResult); err != nil {
			return "", err
		}
		
		if len(searchResult.Files) == 0 {
			return "", fmt.Errorf("file not found: %s", part)
		}
		
		if isLastPart {
			// Return the webViewLink for the file
			return searchResult.Files[0].WebViewLink, nil
		}
		
		// Move to next folder
		currentFolderID = searchResult.Files[0].ID
	}
	
	return "", fmt.Errorf("file not found")
}

