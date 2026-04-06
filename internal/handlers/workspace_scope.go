package handlers

import (
	"fmt"
	"path"
	"strings"

	"panel/internal/workspace"
)

func normalizeWorkspaceScopeRel(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" {
		return "", nil
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+raw), "/")
	if cleaned == "." {
		return "", nil
	}
	parts := strings.Split(cleaned, "/")
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch part {
		case "", ".":
			continue
		case "..", ".panel-meta", workspace.ReservedDir:
			return "", fmt.Errorf("invalid path")
		default:
			safe = append(safe, part)
		}
	}
	return strings.Join(safe, "/"), nil
}

func joinWorkspaceScope(base, rel string) (string, error) {
	baseNorm, err := normalizeWorkspaceScopeRel(base)
	if err != nil {
		return "", err
	}
	relNorm, err := normalizeWorkspaceScopeRel(rel)
	if err != nil {
		return "", err
	}
	if relNorm == "" {
		return baseNorm, nil
	}
	if baseNorm == "" {
		return relNorm, nil
	}
	full := path.Clean(baseNorm + "/" + relNorm)
	if full == baseNorm || strings.HasPrefix(full, baseNorm+"/") {
		return full, nil
	}
	return "", fmt.Errorf("invalid path")
}

func trimWorkspaceScope(full, base string) string {
	fullNorm, err := normalizeWorkspaceScopeRel(full)
	if err != nil {
		return ""
	}
	baseNorm, err := normalizeWorkspaceScopeRel(base)
	if err != nil || baseNorm == "" {
		return fullNorm
	}
	if fullNorm == baseNorm {
		return ""
	}
	return strings.TrimPrefix(fullNorm, baseNorm+"/")
}
