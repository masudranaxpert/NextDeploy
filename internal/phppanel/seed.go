package phppanel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// serversideup/php FPM images (https://hub.docker.com/r/serversideup/php) bundle Composer,
// common extensions (mysqli, pdo_mysql, etc.), and use listen = 9000 (all interfaces) in their
// pool config — ideal behind main Caddy docker-proxy. Use plain -fpm-alpine, not fpm-nginx.
const composeSeed = `networks:
  nextdeploy:
    external: true
    name: NextDeploy

volumes:
  mysql_data: {}

services:
  php_fpm_74:
    image: serversideup/php:7.4-fpm-alpine
    restart: unless-stopped
    networks: [nextdeploy]
    environment:
      PHP_OPCACHE_ENABLE: "1"
    volumes:
      - ./sites:/var/www/sites
      - .:${APP_WORKSPACE_ROOT}
    working_dir: /var/www/sites

  php_fpm_81:
    image: serversideup/php:8.1-fpm-alpine
    restart: unless-stopped
    networks: [nextdeploy]
    environment:
      PHP_OPCACHE_ENABLE: "1"
    volumes:
      - ./sites:/var/www/sites
      - .:${APP_WORKSPACE_ROOT}
    working_dir: /var/www/sites

  php_fpm_82:
    image: serversideup/php:8.2-fpm-alpine
    restart: unless-stopped
    networks: [nextdeploy]
    environment:
      PHP_OPCACHE_ENABLE: "1"
    volumes:
      - ./sites:/var/www/sites
      - .:${APP_WORKSPACE_ROOT}
    working_dir: /var/www/sites

  php_fpm_83:
    image: serversideup/php:8.3-fpm-alpine
    restart: unless-stopped
    networks: [nextdeploy]
    environment:
      PHP_OPCACHE_ENABLE: "1"
    volumes:
      - ./sites:/var/www/sites
      - .:${APP_WORKSPACE_ROOT}
    working_dir: /var/www/sites

  php_mysql:
    image: mysql:8.0
    restart: unless-stopped
    networks: [nextdeploy]
    volumes:
      - mysql_data:/var/lib/mysql
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-changeme_please}
      MYSQL_ROOT_HOST: "%"
    command: >
      --default-authentication-plugin=mysql_native_password
      --character-set-server=utf8mb4
      --collation-server=utf8mb4_unicode_ci

  php_pma:
    image: phpmyadmin:latest
    restart: unless-stopped
    networks: [nextdeploy]
    depends_on:
      - php_mysql
    volumes:
      - ./.nextdeploy/phpmyadmin/config.user.inc.php:/etc/phpmyadmin/config.user.inc.php:ro
    environment:
      PMA_HOST: php_mysql
      PMA_PORT: 3306
      PMA_USER: root
      PMA_PASSWORD: ${MYSQL_ROOT_PASSWORD:-changeme_please}
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-changeme_please}
      PMA_ARBITRARY: 0
      UPLOAD_LIMIT: 256M
`

// oldFPMImages maps legacy image lines → serversideup/php FPM alpine tags.
var oldFPMImages = map[string]string{
	"image: php:7.4-fpm-alpine":           "image: serversideup/php:7.4-fpm-alpine",
	"image: php:8.1-fpm-alpine":           "image: serversideup/php:8.1-fpm-alpine",
	"image: php:8.2-fpm-alpine":           "image: serversideup/php:8.2-fpm-alpine",
	"image: php:8.3-fpm-alpine":           "image: serversideup/php:8.3-fpm-alpine",
	"image: webdevops/php-fpm:7.4-alpine": "image: serversideup/php:7.4-fpm-alpine",
	"image: webdevops/php-fpm:8.1-alpine": "image: serversideup/php:8.1-fpm-alpine",
	"image: webdevops/php-fpm:8.2-alpine": "image: serversideup/php:8.2-fpm-alpine",
	"image: webdevops/php-fpm:8.3-alpine": "image: serversideup/php:8.3-fpm-alpine",
	"image: adhocore/phpfpm:7.4":          "image: serversideup/php:7.4-fpm-alpine",
	"image: adhocore/phpfpm:8.1":          "image: serversideup/php:8.1-fpm-alpine",
	"image: adhocore/phpfpm:8.2":          "image: serversideup/php:8.2-fpm-alpine",
	"image: adhocore/phpfpm:8.3":          "image: serversideup/php:8.3-fpm-alpine",
}

func shouldStripLegacyFPMCommandLine(next string) bool {
	if strings.Contains(next, "docker-php-ext-install") {
		return true
	}
	// Adhocore-era wrapper: sed listen then php-fpm (serversideup already listens on all interfaces).
	if strings.Contains(next, "php-fpm") && strings.Contains(next, "sed -i") {
		return true
	}
	return false
}

// stripLegacyFPMCommandBlocks removes obsolete `command: >` blocks from FPM services
// (runtime ext compile or redundant listen patching) once serversideup/php is used.
func stripLegacyFPMCommandBlocks(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "command: >" && i+1 < len(lines) {
			next := lines[i+1]
			if shouldStripLegacyFPMCommandLine(next) {
				i++
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// MigrateCompose upgrades legacy PHP Panel compose.yml files to serversideup/php FPM images
// and drops slow or redundant command overrides. Safe to call repeatedly — no-op if unchanged.
func MigrateCompose(workspaceRoot string) error {
	composePath := filepath.Join(workspaceRoot, DefaultComposeFile)
	existing, err := os.ReadFile(composePath)
	if err != nil {
		return nil // not a PHP panel workspace or compose missing — skip silently
	}
	content := string(existing)

	updated := content
	for old, replacement := range oldFPMImages {
		updated = strings.ReplaceAll(updated, old, replacement)
	}
	updated = stripLegacyFPMCommandBlocks(updated)

	if updated == content {
		return nil
	}
	return os.WriteFile(composePath, []byte(updated), 0640)
}

func SeedWorkspace(workspaceRoot, appName string) error {
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "sites"), 0750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, DefaultComposeFile), []byte(composeSeed), 0640); err != nil {
		return err
	}
	envPath := filepath.Join(workspaceRoot, ".nextdeploy", "panel.compose.env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0750); err != nil {
		return err
	}
	if err := EnsurePHPMyAdminConfig(workspaceRoot); err != nil {
		return err
	}
	workspaceID := strings.TrimSpace(filepath.Base(filepath.Clean(workspaceRoot)))
	workspaceContainerRoot := fmt.Sprintf("/data/workspaces/%s", workspaceID)
	env := fmt.Sprintf(
		"MYSQL_ROOT_PASSWORD=%s\nPMA_PORT=8081\nAPP_NAME=%s\nAPP_WORKSPACE_ROOT=%s\n",
		defaultRootPassword,
		strings.TrimSpace(appName),
		workspaceContainerRoot,
	)
	if err := os.WriteFile(envPath, []byte(env), 0600); err != nil {
		return err
	}
	indexPath := filepath.Join(workspaceRoot, "sites", "index", "public_html")
	if err := os.MkdirAll(indexPath, 0750); err != nil {
		return err
	}
	defaultIndex := `<?php
$host = 'php_mysql';
$port = '3306';
echo "<h1>PHP Panel ready</h1>";
echo "<p>Connected stack is ready for multi-site hosting.</p>";
echo "<p>Current PHP version: " . PHP_VERSION . "</p>";
`
	return os.WriteFile(filepath.Join(indexPath, "index.php"), []byte(defaultIndex), 0640)
}

func EnsurePHPMyAdminConfig(workspaceRoot string) error {
	pmaConfigPath := filepath.Join(workspaceRoot, ".nextdeploy", "phpmyadmin", "config.user.inc.php")
	if err := os.MkdirAll(filepath.Dir(pmaConfigPath), 0750); err != nil {
		return err
	}
	if st, err := os.Stat(pmaConfigPath); err == nil && !st.IsDir() {
		return nil
	}
	pmaConfig := `<?php
$cfg['LoginCookieValidity'] = 3600;
ini_set('session.gc_maxlifetime', '3600');
`
	return os.WriteFile(pmaConfigPath, []byte(pmaConfig), 0640)
}
