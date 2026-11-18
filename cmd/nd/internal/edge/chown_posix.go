//go:build !windows

package edge

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// ChownGroup sets group ownership on rootPath, recursively if requested. The owner is left
// untouched (uid -1): some paths belong to the proxy itself, e.g. Caddy's certificate storage
// owned by the caddy user with 0600/0700 modes, and forcing root there locks Caddy out.
func ChownGroup(rootPath, groupName string, recursive bool) error {
	if groupName == "" {
		return nil
	}
	gid, err := lookupGroupGID(groupName)
	if err != nil {
		return err
	}
	if !recursive {
		return chownPathGID(rootPath, gid)
	}
	return filepath.WalkDir(rootPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return os.Lchown(p, -1, gid)
		}
		return os.Chown(p, -1, gid)
	})
}

func lookupGroupGID(groupName string) (int, error) {
	if grp, err := user.LookupGroup(groupName); err == nil {
		return strconv.Atoi(grp.Gid)
	} else if u, uErr := user.Lookup(groupName); uErr == nil {
		// Fall back to user lookup if group name matches user primary gid.
		return strconv.Atoi(u.Gid)
	} else {
		return 0, fmt.Errorf("lookup group %q: %w", groupName, err)
	}
}

func chownPathGID(p string, gid int) error {
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.Lchown(p, -1, gid)
	}
	return os.Chown(p, -1, gid)
}
