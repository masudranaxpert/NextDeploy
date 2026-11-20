package migrate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// verifyBundle reads the whole bundle once and checks that it is complete: the gzip stream ends
// cleanly (a truncated upload or download fails here) and every app archive the snapshot
// references is present. It runs before an import resets the panel, so a broken bundle can
// never leave the panel wiped and half restored.
func verifyBundle(ctx context.Context, bundlePath string, members []string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("bundle is not a valid .nd-migrate file: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("bundle is damaged or incomplete (was the upload or download cut off?): %w", err)
		}
		seen[cleanMember(h.Name)] = true
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return fmt.Errorf("bundle is damaged or incomplete (was the upload or download cut off?): %w", err)
		}
	}
	// Read to the end of the gzip stream so its checksum is verified too.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return fmt.Errorf("bundle is damaged or incomplete: %w", err)
	}
	var missing []string
	for _, m := range members {
		if m = cleanMember(m); m != "" && !seen[m] {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("bundle is missing %d app archive(s): %s", len(missing), strings.Join(missing, ", "))
	}
	return nil
}

func cleanMember(name string) string {
	name = strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(name)), "/")
	if name == "." {
		return ""
	}
	return name
}
