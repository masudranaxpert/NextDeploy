package dev

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidTarget(t *testing.T) {
	cases := []struct {
		target string
		want   bool
	}{
		{"/app", true},
		{"/workspace", true},
		{"/var/www/html", true},
		{"/usr/src/app", true},
		{"/home/node/app", true},
		{"/", false},
		{"/etc", false},
		{"/etc/nginx", false},
		{"/usr", false},
		{"/usr/local/bin", false},
		{"/usr/bin", false},
		{"/var", false},
		{"/var/run", false},
		{"/var/lib/postgresql", false},
		{"/var/lib/postgresql/data", false},
		{"/var/lib/mysql", false},
		{"/data/db", false},
		{"/bitnami/redis", false},
		{"app", false},
		{"/app:ro", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := ValidTarget(tc.target); got != tc.want {
			t.Errorf("ValidTarget(%q) = %v, want %v", tc.target, got, tc.want)
		}
	}
}

func TestApplyMissingServiceDoesNotOverreach(t *testing.T) {
	services := map[string]interface{}{
		"worker": map[string]interface{}{
			"build": ".",
		},
		"db": map[string]interface{}{
			"image": "postgres:16",
		},
	}
	// Target "web" which doesn't exist
	Apply(services, DevMount{Enabled: true, Service: "web"})

	worker := services["worker"].(map[string]interface{})
	if _, hasVols := worker["volumes"]; hasVols {
		t.Fatalf("expected worker to NOT be mounted when missing target service 'web' was requested, got %v", worker["volumes"])
	}
}

func TestApplyDevCommandOverride(t *testing.T) {
	// Single service: command applies
	singleSvc := map[string]interface{}{
		"web": map[string]interface{}{
			"build":   ".",
			"command": "npm start",
		},
	}
	Apply(singleSvc, DevMount{
		Enabled:    true,
		DevCommand: "npm run dev",
	})
	web := singleSvc["web"].(map[string]interface{})
	if cmd, ok := web["command"].(string); !ok || cmd != "npm run dev" {
		t.Fatalf("expected command 'npm run dev', got %v", web["command"])
	}

	// Multiple services without explicit target: command does NOT overwrite worker
	multiSvc := map[string]interface{}{
		"web": map[string]interface{}{
			"build":   ".",
			"command": "uvicorn main:app",
		},
		"worker": map[string]interface{}{
			"build":   ".",
			"command": "celery -A app worker",
		},
	}
	Apply(multiSvc, DevMount{
		Enabled:    true,
		DevCommand: "uvicorn main:app --reload",
	})
	worker := multiSvc["worker"].(map[string]interface{})
	if cmd, _ := worker["command"].(string); cmd != "celery -A app worker" {
		t.Fatalf("worker command should NOT have been overwritten, got %v", cmd)
	}

	// Multiple services with explicit target: ONLY web gets overridden
	Apply(multiSvc, DevMount{
		Enabled:    true,
		Service:    "web",
		DevCommand: "uvicorn main:app --reload",
	})
	webMulti := multiSvc["web"].(map[string]interface{})
	if cmd, _ := webMulti["command"].(string); cmd != "uvicorn main:app --reload" {
		t.Fatalf("web command should have been overridden, got %v", cmd)
	}
	workerMulti := multiSvc["worker"].(map[string]interface{})
	if cmd, _ := workerMulti["command"].(string); cmd != "celery -A app worker" {
		t.Fatalf("worker command should NOT have been touched, got %v", cmd)
	}
}

func TestApplyTargetsBuildServicesOnly(t *testing.T) {
	services := map[string]interface{}{
		"web": map[string]interface{}{
			"build": ".",
		},
		"db": map[string]interface{}{
			"image": "postgres:16",
		},
	}
	Apply(services, DevMount{Enabled: true})

	web := services["web"].(map[string]interface{})
	db := services["db"].(map[string]interface{})

	if vols, ok := web["volumes"].([]interface{}); !ok || len(vols) == 0 || vols[0] != "./:/app" {
		t.Fatalf("expected web to have bind mount, got %v", web["volumes"])
	}
	if _, hasVols := db["volumes"]; hasVols {
		t.Fatalf("expected db to have no volumes, got %v", db["volumes"])
	}
}

func TestApplyTargetsBuiltImageReusers(t *testing.T) {
	services := map[string]interface{}{
		"web": map[string]interface{}{
			"build": ".",
			"image": "myapp:v1",
		},
		"worker": map[string]interface{}{
			"image": "myapp:v1",
		},
		"db": map[string]interface{}{
			"image": "postgres:16",
		},
	}
	Apply(services, DevMount{Enabled: true, HostRoot: "/host/data/workspaces/app1"})

	web := services["web"].(map[string]interface{})
	worker := services["worker"].(map[string]interface{})
	db := services["db"].(map[string]interface{})

	expected := "/host/data/workspaces/app1:/app"
	if vols, ok := web["volumes"].([]interface{}); !ok || len(vols) == 0 || vols[0] != expected {
		t.Fatalf("expected web to have %q, got %v", expected, web["volumes"])
	}
	if vols, ok := worker["volumes"].([]interface{}); !ok || len(vols) == 0 || vols[0] != expected {
		t.Fatalf("expected worker to reuse image mount %q, got %v", expected, worker["volumes"])
	}
	if _, hasVols := db["volumes"]; hasVols {
		t.Fatalf("expected db to have no volumes, got %v", db["volumes"])
	}
}

func TestApplyPreservesDependencies(t *testing.T) {
	services := map[string]interface{}{
		"web": map[string]interface{}{
			"build": ".",
		},
	}
	doc := map[string]interface{}{}
	Apply(services, DevMount{
		Enabled:       true,
		AppID:         "app1",
		PreservePaths: []string{"node_modules", ".venv", "vendor"},
	}, doc)

	web := services["web"].(map[string]interface{})
	vols := web["volumes"].([]interface{})
	expected := []string{
		"./:/app",
		"nddev_app1_web_node_modules:/app/node_modules",
		"nddev_app1_web_venv:/app/.venv",
		"nddev_app1_web_vendor:/app/vendor",
	}

	if len(vols) != len(expected) {
		t.Fatalf("expected %d volumes, got %d: %v", len(expected), len(vols), vols)
	}
	for i, exp := range expected {
		if vols[i] != exp {
			t.Errorf("vol[%d] = %v, want %v", i, vols[i], exp)
		}
	}

	topVols := doc["volumes"].(map[string]interface{})
	for _, expNamed := range []string{"nddev_app1_web_node_modules", "nddev_app1_web_venv", "nddev_app1_web_vendor"} {
		if _, ok := topVols[expNamed]; !ok {
			t.Errorf("expected top-level volume %q in doc, got %v", expNamed, topVols)
		}
	}

	// Test idempotency: re-applying should not duplicate
	Apply(services, DevMount{
		Enabled:       true,
		AppID:         "app1",
		PreservePaths: []string{"node_modules", ".venv", "vendor"},
	}, doc)
	volsAgain := web["volumes"].([]interface{})
	if len(volsAgain) != len(expected) {
		t.Fatalf("idempotency failed, got duplicate volumes: %v", volsAgain)
	}
}

func TestDetectPreservePaths(t *testing.T) {
	tmp := t.TempDir()

	// Empty dir
	if paths := DetectPreservePaths(tmp); len(paths) != 0 {
		t.Fatalf("expected 0 paths for empty dir, got %v", paths)
	}

	// Python uv detection
	if err := os.WriteFile(filepath.Join(tmp, "uv.lock"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	paths := DetectPreservePaths(tmp)
	for _, expected := range []string{".venv", "venv", "env"} {
		found := false
		for _, p := range paths {
			if p == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected preserve path %q for uv.lock, got %v", expected, paths)
		}
	}

	// Monorepo subdirectories
	frontendDir := filepath.Join(tmp, "frontend")
	if err := os.MkdirAll(frontendDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frontendDir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	paths = DetectPreservePaths(tmp)
	foundFrontendNodeModules := false
	for _, p := range paths {
		if p == "frontend/node_modules" {
			foundFrontendNodeModules = true
			break
		}
	}
	if !foundFrontendNodeModules {
		t.Fatalf("expected frontend/node_modules in monorepo, got %v", paths)
	}
}
