package mcp

import "panel/internal/wsmanifest"

// Type aliases so existing handler.go code compiles unchanged.
type ManifestEntry = wsmanifest.ManifestEntry
type ManifestResult = wsmanifest.ManifestResult
type ManifestDiffResult = wsmanifest.ManifestDiffResult

// BuildWorkspaceManifest delegates to wsmanifest to avoid import cycles.
func BuildWorkspaceManifest(wsRoot, appID, relScope string, depth int, computeHash bool, includeLocks bool, fullHash bool, customExcludes []string, maxEntries int) (ManifestResult, error) {
	return wsmanifest.BuildWorkspaceManifest(wsRoot, appID, relScope, depth, computeHash, includeLocks, fullHash, customExcludes, maxEntries)
}

// ComputeManifestDiff delegates to wsmanifest.
func ComputeManifestDiff(res ManifestResult, localFiles map[string]string) ManifestDiffResult {
	return wsmanifest.ComputeManifestDiff(res, localFiles)
}

// InvalidateManifestCache delegates to wsmanifest.
func InvalidateManifestCache(appID, relPath string) {
	wsmanifest.InvalidateManifestCache(appID, relPath)
}
