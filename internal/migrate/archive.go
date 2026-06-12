package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func packBundle(ctx context.Context, workDir, outPath string, manifest BundleManifest) error {
	payload := []string{snapshotName}
	appsDir := filepath.Join(workDir, appsDirName)
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return fmt.Errorf("apps dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		payload = append(payload, filepath.Join(appsDirName, e.Name()))
	}
	mb, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, manifestName), mb, 0600); err != nil {
		return err
	}
	members := append([]string{manifestName}, payload...)
	return tarCreateGz(ctx, outPath, workDir, members)
}

func extractBundleMembers(ctx context.Context, bundlePath, destDir string, members []string) error {
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return err
	}
	return tarExtractMembers(ctx, bundlePath, destDir, members)
}

func ensureBundleMember(ctx context.Context, bundlePath, destDir, member string) error {
	target := filepath.Join(destDir, filepath.FromSlash(member))
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	return extractBundleMembers(ctx, bundlePath, destDir, []string{member})
}

func readManifestFromDir(dir string) (BundleManifest, error) {
	var m BundleManifest
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Version != BundleVersion || m.Type != BundleType {
		return m, fmt.Errorf("unsupported bundle version/type")
	}
	return m, nil
}

func readSnapshotFromDir(dir string) (PanelSnapshot, error) {
	var s PanelSnapshot
	b, err := os.ReadFile(filepath.Join(dir, snapshotName))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, nil
}

func bundleFileName() string {
	return "panel-migrate-" + time.Now().UTC().Format("20060102-150405") + BundleExtension
}
