// Package phphosting runs PHP sites on top of Docker: one PHP-FPM container per
// (user, PHP version) with one FPM pool per site, a shared MariaDB container and a
// tiny route-holder container per site that carries the Caddy labels.
package phphosting

import (
	"fmt"
	"strings"
)

const (
	DefaultImageRepo = "masudranaxpert/nextdeploy-php"

	MySQLContainer = "nd-mysql"
	MySQLVolume    = "nd-mysql-data"
	DefaultMySQL   = "mariadb:11.4"
	RedisContainer = "nd-redis"
	RedisVolume    = "nd-redis-data"
	DefaultRedis   = "redis:7-alpine"
	DefaultDBNet   = "nd-db"
	DefaultIngress = "NextDeploy"

	// ContainerWorkspaces is where workspaces are visible to Caddy and to PHP
	// containers. Caddy mounts the host workspaces dir here (see docker-compose.yml),
	// and PHP containers mount each site at the same path so SCRIPT_FILENAME matches.
	ContainerWorkspaces = "/data/workspaces"
	RuntimeDirName      = ".nd-php"

	SiteTypePHP       = "php"
	SiteTypeWordPress = "wordpress"
	SiteTypeStatic    = "static"

	DefaultPHPVersion = "8.3"
)

// SupportedVersions lists the PHP versions an admin can install.
var SupportedVersions = []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

func IsSupportedVersion(v string) bool {
	for _, s := range SupportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

func IsValidSiteType(t string) bool {
	return t == SiteTypePHP || t == SiteTypeWordPress || t == SiteTypeStatic
}

func ImageName(version string) string { return ImageRepo() + ":" + version }

func versionTag(version string) string { return strings.ReplaceAll(version, ".", "") }

// RuntimeContainer is the PHP-FPM container for one user and PHP version.
func RuntimeContainer(ownerID int64, version string) string {
	return fmt.Sprintf("nd-php%s-u%d", versionTag(version), ownerID)
}

// RouteContainer is the tiny container that carries the Caddy labels of one site.
func RouteContainer(appID string) string { return "nd-phpr-" + appID }

func runtimeKey(ownerID int64, version string) string {
	return fmt.Sprintf("%d/%s", ownerID, version)
}
