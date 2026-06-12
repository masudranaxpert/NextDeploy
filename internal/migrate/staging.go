package migrate

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	BundleVersion   = 1
	BundleType      = "panel-migrate"
	BundleExtension = ".nd-migrate"
	OrphanAge       = 2 * time.Hour
	LinkTTL         = 24 * time.Hour
)

const (
	manifestName = "manifest.json"
	snapshotName = "panel-snapshot.json"
	appsDirName  = "apps"
)

func dataDir() string {
	if d := strings.TrimSpace(os.Getenv("DATA_DIR")); d != "" {
		return d
	}
	return "./data"
}

func StagingRoot() string {
	return filepath.Join(dataDir(), "migrate-staging")
}

func IncomingDir() string {
	return filepath.Join(dataDir(), "migrate-incoming")
}

func ExportWorkDir(exportID int64) string {
	return filepath.Join(StagingRoot(), "export-"+strconv.FormatInt(exportID, 10))
}
