// Package dev manages workspace development mode mount injection and anti-shadowing volume preservation.
package dev

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DevMount defines the configuration for mounting the workspace into running containers.
type DevMount struct {
	// Enabled toggles the bind mount injection.
	Enabled bool
	// AppID uniquely scopes named preservation volumes (e.g. "nddev_app1_...").
	AppID string
	// Service restricts the mount to one service name. When empty, it targets every
	// service that builds from source or reuses a built image.
	Service string
	// Target is the container working directory path. Defaults to DefaultTarget (/app).
	Target string
	// HostRoot is the host-side absolute workspace directory. If empty, "./" is used.
	HostRoot string
	// DevCommand overrides the container startup command in dev mode (e.g. "npm run dev").
	DevCommand string
	// PreservePaths holds sub-paths inside Target (e.g. "node_modules", ".venv") to preserve via named volumes.
	PreservePaths []string
}

// DevVolumeName returns the deterministic, app-scoped named volume name for a preserved path.
func DevVolumeName(appID, svcKey, relPath string) string {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		appID = "app"
	}
	// Encode dots as "dot_" so hidden directories like .venv and normal dirs like venv do not collide
	r := strings.NewReplacer("/", "_", "\\", "_", ".", "dot_")
	sanitized := strings.Trim(r.Replace(relPath), "_")
	return fmt.Sprintf("nddev_%s_%s_%s", appID, svcKey, sanitized)
}

// MatchDevVolumeService finds the compose service name that owns the given dev volume name.
// It uses longest prefix matching against known service names to safely handle service names
// containing underscores (e.g. "api_worker" vs "api").
func MatchDevVolumeService(volName, volPrefix string, knownServices []string) string {
	best := ""
	for _, s := range knownServices {
		if strings.HasPrefix(volName, volPrefix+s+"_") && len(s) > len(best) {
			best = s
		}
	}
	return best
}

// DefaultTarget is the default container path used when dev mode is enabled without specifying one.
const DefaultTarget = "/app"

// ValidTarget reports whether target is a safe, usable container working directory path.
func ValidTarget(target string) bool {
	target = filepath.ToSlash(filepath.Clean(strings.TrimSpace(target)))
	if !strings.HasPrefix(target, "/") || target == "/" || strings.Contains(target, ":") {
		return false
	}
	// Disallow mounting directly over root or sensitive system directories
	if target == "/var" || target == "/usr" {
		return false
	}
	// Prefix-blocked system roots where mounting would break container OS or services
	blockedPrefixes := []string{
		"/bin", "/sbin", "/boot", "/dev", "/etc",
		"/lib", "/lib64", "/proc", "/root", "/sys", "/run",
		"/data/db", "/bitnami", "/var/run", "/var/lock", "/var/lib",
		"/usr/bin", "/usr/sbin", "/usr/lib", "/usr/lib64",
		"/usr/local/bin", "/usr/local/sbin", "/usr/include",
	}
	for _, prefix := range blockedPrefixes {
		if target == prefix || strings.HasPrefix(target, prefix+"/") {
			return false
		}
	}
	return true
}

// Apply injects the workspace bind mount and dependency preservation named volumes into selected services.
// Optional doc parameter allows registering top-level named volumes.
func Apply(services map[string]interface{}, dev DevMount, doc ...map[string]interface{}) {
	if !dev.Enabled {
		return
	}
	target := strings.TrimSpace(dev.Target)
	if !ValidTarget(target) {
		target = DefaultTarget
	}

	// 1. Identify built images across services and track the build service name
	builtImages := make(map[string]bool)
	var buildSvcName string
	var buildServiceCount int
	for svcName, rawSvc := range services {
		if s, ok := toStringMap(rawSvc); ok {
			if _, hasBuild := s["build"]; hasBuild {
				buildServiceCount++
				buildSvcName = svcName
				if img, ok := s["image"].(string); ok && strings.TrimSpace(img) != "" {
					builtImages[strings.TrimSpace(img)] = true
				}
			}
		}
	}

	// 2. Validate specific service target. If specified service is missing, do NOT fallback
	// silently to all services to avoid modifying unintended containers.
	only := strings.TrimSpace(dev.Service)
	if only != "" {
		if _, exists := services[only]; !exists {
			log.Printf("[dev] WARNING: configured dev service %q not found in compose services; skipping dev mount", only)
			return
		}
	}

	mountSource := "./"
	if hostRoot := strings.TrimSpace(dev.HostRoot); hostRoot != "" {
		mountSource = filepath.ToSlash(filepath.Clean(hostRoot))
	}

	for svcKey, rawSvc := range services {
		svc, ok := toStringMap(rawSvc)
		if !ok {
			continue
		}

		shouldMount := false
		if only != "" {
			shouldMount = (svcKey == only)
		} else {
			_, hasBuild := svc["build"]
			img, _ := svc["image"].(string)
			shouldMount = hasBuild || (img != "" && builtImages[strings.TrimSpace(img)])
		}
		if !shouldMount {
			continue
		}

		appendServiceVolume(svc, mountSource+":"+target)
		for _, p := range dev.PreservePaths {
			if p = strings.TrimSpace(p); p != "" {
				volName := DevVolumeName(dev.AppID, svcKey, p)
				containerDst := path.Join(target, p)
				appendServiceVolume(svc, volName+":"+containerDst)

				if len(doc) > 0 && doc[0] != nil {
					topVols, _ := toStringMap(doc[0]["volumes"])
					if topVols == nil {
						topVols = make(map[string]interface{})
					}
					topVols[volName] = map[string]interface{}{
						"name": volName,
					}
					doc[0]["volumes"] = topVols
				}
			}
		}

		// DevCommand must only override the command on the intended service:
		// either explicitly targeted by dev.Service, or when there is exactly one
		// service with a build: block (and only on that specific service, not on
		// secondary services like workers that reuse the built image).
		if cmd := strings.TrimSpace(dev.DevCommand); cmd != "" {
			canApplyCommand := false
			if only != "" {
				canApplyCommand = (svcKey == only)
			} else {
				canApplyCommand = (buildServiceCount == 1 && svcKey == buildSvcName)
			}
			if canApplyCommand {
				svc["command"] = cmd
			}
		}

		services[svcKey] = svc
	}
}

// DetectPreservePaths scans root and its immediate subdirectories to auto-detect
// in-container dependency and build directories across major ecosystems:
// Node (node_modules), Python/uv/venv/poetry (.venv, venv, env), PHP (vendor),
// Ruby (vendor/bundle, .bundle), Rust (target), Java/Gradle (target, build, .gradle),
// and Elixir (_build, deps).
func DetectPreservePaths(root string) []string {
	if root == "" {
		return nil
	}
	seen := make(map[string]bool)
	var paths []string

	add := func(rel string) {
		rel = strings.Trim(filepath.ToSlash(filepath.Clean(rel)), "/")
		if rel == "." || rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		paths = append(paths, rel)
	}

	inspect := func(dir string, prefix string) {
		hasFile := func(name string) bool {
			st, err := os.Stat(filepath.Join(dir, name))
			return err == nil && !st.IsDir()
		}
		hasDir := func(name string) bool {
			st, err := os.Stat(filepath.Join(dir, name))
			return err == nil && st.IsDir()
		}

		// Node.js / Bun
		if hasFile("package.json") || hasFile("pnpm-lock.yaml") || hasFile("yarn.lock") || hasFile("bun.lockb") || hasFile("bun.lock") || hasDir("node_modules") {
			add(filepath.Join(prefix, "node_modules"))
		}

		// Python (uv, venv, poetry, pipenv, standard requirements)
		if hasFile("uv.lock") || hasFile("pyproject.toml") || hasFile("requirements.txt") ||
			hasFile("requirements-dev.txt") || hasFile("Pipfile") || hasFile("Pipfile.lock") ||
			hasFile("poetry.lock") || hasFile("setup.py") || hasFile("setup.cfg") ||
			hasFile("environment.yml") || hasDir(".venv") || hasDir("venv") || hasDir("env") {
			add(filepath.Join(prefix, ".venv"))
			add(filepath.Join(prefix, "venv"))
			add(filepath.Join(prefix, "env"))
			add(filepath.Join(prefix, ".pytest_cache"))
			add(filepath.Join(prefix, ".ruff_cache"))
		}

		// PHP (Composer)
		if hasFile("composer.json") || hasFile("composer.lock") || hasDir("vendor") {
			add(filepath.Join(prefix, "vendor"))
		}

		// Ruby (Bundler)
		if hasFile("Gemfile") || hasFile("Gemfile.lock") {
			add(filepath.Join(prefix, "vendor/bundle"))
			add(filepath.Join(prefix, ".bundle"))
		}

		// Rust (Cargo)
		if hasFile("Cargo.toml") || hasFile("Cargo.lock") || hasDir("target") {
			add(filepath.Join(prefix, "target"))
		}

		// Java / Kotlin (Maven / Gradle)
		if hasFile("pom.xml") {
			add(filepath.Join(prefix, "target"))
		}
		if hasFile("build.gradle") || hasFile("build.gradle.kts") || hasFile("settings.gradle") || hasFile("gradlew") {
			add(filepath.Join(prefix, "build"))
			add(filepath.Join(prefix, ".gradle"))
		}

		// Elixir (Mix)
		if hasFile("mix.exs") || hasFile("mix.lock") {
			add(filepath.Join(prefix, "_build"))
			add(filepath.Join(prefix, "deps"))
		}
	}

	// 1. Root level
	inspect(root, "")

	// 2. 1-level deep subdirectories for monorepos (skip hidden/build artifacts)
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" ||
				name == "target" || name == "build" || name == "dist" || name == "storage" {
				continue
			}
			inspect(filepath.Join(root, name), name)
		}
	}

	return paths
}

// appendServiceVolume adds a short-syntax volume mount, skipping if already mounted.
func appendServiceVolume(service map[string]interface{}, mount string) {
	target := mount
	if i := strings.Index(mount, ":"); i >= 0 {
		target = mount[i+1:]
	}
	var list []interface{}
	if raw, ok := service["volumes"]; ok {
		existing, ok2 := raw.([]interface{})
		if !ok2 {
			return
		}
		for _, item := range existing {
			if volumeTarget(item) == target {
				return
			}
			list = append(list, item)
		}
	}
	service["volumes"] = append(list, mount)
}

// volumeTarget returns the container path from a short-, long-, or anonymous volume entry.
func volumeTarget(item interface{}) string {
	if s, ok := item.(string); ok {
		s = strings.TrimSpace(s)
		parts := strings.Split(s, ":")
		if len(parts) >= 2 {
			return strings.TrimSpace(parts[1])
		}
		return s
	}
	if m, ok := toStringMap(item); ok {
		if t, ok := m["target"].(string); ok && t != "" {
			return strings.TrimSpace(t)
		}
		if d, ok := m["destination"].(string); ok && d != "" {
			return strings.TrimSpace(d)
		}
	}
	return ""
}

func toStringMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}
