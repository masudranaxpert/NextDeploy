package rclone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config represents rclone remote configuration
type Config struct {
	Name     string
	Type     string
	Settings map[string]string
}

// GDriveConfig represents Google Drive OAuth configuration
type GDriveConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Token        string `json:"token"`
	RootFolderID string `json:"root_folder_id,omitempty"`
}

// R2Config represents Cloudflare R2 configuration
type R2Config struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
}

// CreateGDriveRemote creates a Google Drive remote configuration
func CreateGDriveRemote(ctx context.Context, name string, config GDriveConfig) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	
	configPath := filepath.Join(configDir, "rclone.conf")
	
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("[%s]\n", name))
	buf.WriteString("type = drive\n")
	buf.WriteString(fmt.Sprintf("client_id = %s\n", config.ClientID))
	buf.WriteString(fmt.Sprintf("client_secret = %s\n", config.ClientSecret))
	buf.WriteString(fmt.Sprintf("token = %s\n", config.Token))
	buf.WriteString("scope = drive\n")
	if config.RootFolderID != "" {
		buf.WriteString(fmt.Sprintf("root_folder_id = %s\n", config.RootFolderID))
	}
	buf.WriteString("\n")
	
	return appendConfig(configPath, buf.String())
}

// CreateR2Remote creates a Cloudflare R2 remote configuration
func CreateR2Remote(ctx context.Context, name string, config R2Config) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	
	configPath := filepath.Join(configDir, "rclone.conf")
	
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("[%s]\n", name))
	buf.WriteString("type = s3\n")
	buf.WriteString("provider = Cloudflare\n")
	buf.WriteString(fmt.Sprintf("access_key_id = %s\n", config.AccessKeyID))
	buf.WriteString(fmt.Sprintf("secret_access_key = %s\n", config.SecretAccessKey))
	buf.WriteString(fmt.Sprintf("endpoint = %s\n", config.Endpoint))
	buf.WriteString("acl = private\n")
	buf.WriteString("\n")
	
	return appendConfig(configPath, buf.String())
}

// Upload uploads a file to remote storage
func Upload(ctx context.Context, remoteName, localPath, remotePath string) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	
	cmd := exec.CommandContext(ctx, "rclone", "copy",
		"--config", filepath.Join(configDir, "rclone.conf"),
		localPath,
		fmt.Sprintf("%s:%s", remoteName, remotePath))
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone upload failed: %s: %w", stderr.String(), err)
	}
	
	return nil
}

// Download downloads a file from remote storage
func Download(ctx context.Context, remoteName, remotePath, localPath string) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	
	cmd := exec.CommandContext(ctx, "rclone", "copy",
		"--config", filepath.Join(configDir, "rclone.conf"),
		fmt.Sprintf("%s:%s", remoteName, remotePath),
		localPath)
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone download failed: %s: %w", stderr.String(), err)
	}
	
	return nil
}

// List lists files in remote storage
func List(ctx context.Context, remoteName, remotePath string) ([]string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return nil, err
	}
	
	cmd := exec.CommandContext(ctx, "rclone", "lsf",
		"--config", filepath.Join(configDir, "rclone.conf"),
		fmt.Sprintf("%s:%s", remoteName, remotePath))
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rclone list failed: %s: %w", stderr.String(), err)
	}
	
	var files []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	
	return files, nil
}

// Delete deletes a file from remote storage
func Delete(ctx context.Context, remoteName, remotePath string) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	
	cmd := exec.CommandContext(ctx, "rclone", "deletefile",
		"--config", filepath.Join(configDir, "rclone.conf"),
		fmt.Sprintf("%s:%s", remoteName, remotePath))
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone delete failed: %s: %w", stderr.String(), err)
	}
	
	return nil
}

// DeleteRemote removes a remote configuration
func DeleteRemote(ctx context.Context, name string) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	
	configPath := filepath.Join(configDir, "rclone.conf")
	
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	
	lines := strings.Split(string(content), "\n")
	var newLines []string
	skip := false
	
	for _, line := range lines {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			remoteName := strings.Trim(line, "[]")
			skip = (remoteName == name)
		}
		
		if !skip {
			newLines = append(newLines, line)
		}
		
		if skip && strings.TrimSpace(line) == "" {
			skip = false
		}
	}
	
	return os.WriteFile(configPath, []byte(strings.Join(newLines, "\n")), 0600)
}

// GetOAuthURL generates OAuth URL for Google Drive
func GetOAuthURL(clientID, redirectURL string) string {
	return fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?"+
		"client_id=%s&"+
		"redirect_uri=%s&"+
		"response_type=code&"+
		"scope=https://www.googleapis.com/auth/drive&"+
		"access_type=offline&"+
		"prompt=consent",
		clientID, redirectURL)
}

// ExchangeOAuthCode exchanges OAuth code for token
func ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURL string) (string, error) {
	cmd := exec.CommandContext(ctx, "rclone", "authorize", "drive",
		clientID, clientSecret, code)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("oauth exchange failed: %s: %w", stderr.String(), err)
	}
	
	return strings.TrimSpace(stdout.String()), nil
}

// CreateFolder creates a folder in Google Drive
func CreateFolder(ctx context.Context, remoteName, folderName string) (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	
	cmd := exec.CommandContext(ctx, "rclone", "mkdir",
		"--config", filepath.Join(configDir, "rclone.conf"),
		fmt.Sprintf("%s:%s", remoteName, folderName))
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("create folder failed: %s: %w", stderr.String(), err)
	}
	
	folderID, err := getFolderID(ctx, remoteName, folderName)
	if err != nil {
		return "", err
	}
	
	return folderID, nil
}

// getFolderID gets the folder ID from Google Drive
func getFolderID(ctx context.Context, remoteName, folderPath string) (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	
	cmd := exec.CommandContext(ctx, "rclone", "lsjson",
		"--config", filepath.Join(configDir, "rclone.conf"),
		fmt.Sprintf("%s:%s", remoteName, folderPath))
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("get folder ID failed: %s: %w", stderr.String(), err)
	}
	
	var items []struct {
		ID   string `json:"ID"`
		Name string `json:"Name"`
	}
	
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		return "", err
	}
	
	if len(items) > 0 {
		return items[0].ID, nil
	}
	
	return "", fmt.Errorf("folder not found")
}

func getConfigDir() (string, error) {
	configDir := os.Getenv("RCLONE_CONFIG_DIR")
	if configDir == "" {
		configDir = "/data/rclone"
	}
	
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	
	return configDir, nil
}

func appendConfig(configPath, content string) error {
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	
	_, err = f.WriteString(content)
	return err
}
