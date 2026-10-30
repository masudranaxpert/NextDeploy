package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateAndClampCompose_Success(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    ports:
      - "80"
    volumes:
      - web_data:/var/www/html
      - ./relative/path:/data
    deploy:
      resources:
        limits:
          cpus: "1.5"
          memory: "512M"
volumes:
  web_data:
`
	clampedBytes, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(clampedBytes, &doc); err != nil {
		t.Fatalf("Failed to parse output YAML: %v", err)
	}

	services := doc["services"].(map[string]interface{})
	web := services["web"].(map[string]interface{})
	deploy := web["deploy"].(map[string]interface{})
	resources := deploy["resources"].(map[string]interface{})
	limits := resources["limits"].(map[string]interface{})

	// Check clamped values (cpus: "1.5" <= 2.0, memory: "512M" <= 1024M)
	if limits["cpus"] != "1.50" {
		t.Errorf("Expected cpus to be 1.50, got %v", limits["cpus"])
	}
	if limits["memory"] != "512M" {
		t.Errorf("Expected memory to be 512M, got %v", limits["memory"])
	}
}

func TestValidateAndClampCompose_ClampingEnforced(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    deploy:
      resources:
        limits:
          cpus: "3.5"
          memory: "2048M"
`
	clampedBytes, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(clampedBytes, &doc); err != nil {
		t.Fatalf("Failed to parse output YAML: %v", err)
	}

	services := doc["services"].(map[string]interface{})
	web := services["web"].(map[string]interface{})
	deploy := web["deploy"].(map[string]interface{})
	resources := deploy["resources"].(map[string]interface{})
	limits := resources["limits"].(map[string]interface{})

	// Check clamped values (cpus clamped to 2.00, memory to 1024M)
	if limits["cpus"] != "2.00" {
		t.Errorf("Expected cpus to be clamped to 2.00, got %v", limits["cpus"])
	}
	if limits["memory"] != "1024M" {
		t.Errorf("Expected memory to be clamped to 1024M, got %v", limits["memory"])
	}
}

func TestValidateAndClampCompose_HostBindBlock(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
`
	_, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err == nil {
		t.Fatal("Expected error due to host bind mount, got nil")
	}
}

func TestValidateAndClampCompose_PrivilegedBlock(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    privileged: true
`
	_, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err == nil {
		t.Fatal("Expected error due to privileged mode, got nil")
	}
}

func TestValidateAndClampCompose_CapabilitiesBlock(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    cap_add:
      - SYS_ADMIN
`
	_, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err == nil {
		t.Fatal("Expected error due to cap_add, got nil")
	}
}

func TestValidateAndClampCompose_SecurityOptBlock(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    security_opt:
      - seccomp:unconfined
`
	_, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err == nil {
		t.Fatal("Expected error due to security_opt, got nil")
	}
}

func TestValidateAndClampCompose_PortBindingBlock(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    ports:
      - "8390:4560"
      - "80"
`
	clampedBytes, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(clampedBytes, &doc); err != nil {
		t.Fatalf("Failed to parse output YAML: %v", err)
	}

	services := doc["services"].(map[string]interface{})
	web := services["web"].(map[string]interface{})

	if _, exists := web["ports"]; exists {
		t.Error("Expected 'ports' block to be deleted")
	}

	exposeRaw, exists := web["expose"]
	if !exists {
		t.Fatal("Expected 'expose' block to exist")
	}
	exposeList, ok := exposeRaw.([]interface{})
	if !ok {
		t.Fatalf("Expected 'expose' to be a list, got %T", exposeRaw)
	}

	expected := map[string]bool{
		"4560": true,
		"80":   true,
	}
	for _, exp := range exposeList {
		expStr := fmt.Sprintf("%v", exp)
		delete(expected, expStr)
	}

	if len(expected) > 0 {
		t.Errorf("Missing expected exposed ports: %v, got: %v", expected, exposeList)
	}
}

func TestValidateAndClampCompose_ExternalNetworkBlock(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
networks:
  malicious_net:
    external: true
`
	_, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err == nil {
		t.Fatal("Expected error due to custom external network, got nil")
	}
}

func TestValidateAndClampCompose_DirectoryTraversalBlock(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    volumes:
      - ../../../../var/run/docker.sock:/var/run/docker.sock
`
	_, err := ValidateAndClampCompose([]byte(composeYAML), 2.0, 1024)
	if err == nil {
		t.Fatal("Expected error due to directory traversal mount, got nil")
	}
}

func TestGetComposeResources(t *testing.T) {
	composeYAML := `
services:
  web:
    image: nginx:alpine
    deploy:
      resources:
        limits:
          cpus: "1.5"
          memory: "512M"
  db:
    image: postgres:alpine
    deploy:
      resources:
        limits:
          cpus: "0.5"
          memory: "256M"
`
	mem, cpu, err := GetComposeResources([]byte(composeYAML))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if mem != 768 {
		t.Errorf("Expected 768M RAM, got %d", mem)
	}
	if cpu != 2.0 {
		t.Errorf("Expected 2.0 CPUs, got %.2f", cpu)
	}
}

func TestValidateAndClampCompose_NamespaceEscapesBlocked(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"pid_host", "services:\n  app:\n    image: alpine\n    pid: host\n"},
		{"ipc_host", "services:\n  app:\n    image: alpine\n    ipc: host\n"},
		{"uts_host", "services:\n  app:\n    image: alpine\n    uts: host\n"},
		{"userns_host", "services:\n  app:\n    image: alpine\n    userns_mode: host\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateAndClampCompose([]byte(tc.yaml), 2.0, 1024)
			if err == nil {
				t.Fatalf("expected error blocking %s, got nil", tc.name)
			}
		})
	}
}

func TestValidateAndClampCompose_BuildContextEscapeBlocked(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"build_root", "services:\n  app:\n    build: /\n"},
		{"build_traversal", "services:\n  app:\n    build: ../../etc\n"},
		{"build_context_abs", "services:\n  app:\n    build:\n      context: /var/run\n"},
		{"build_context_traversal", "services:\n  app:\n    build:\n      context: ../root\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateAndClampCompose([]byte(tc.yaml), 2.0, 1024)
			if err == nil {
				t.Fatalf("expected error blocking %s, got nil", tc.name)
			}
		})
	}
}

func TestValidateComposeSecurity_SymlinkBindMountBlocked(t *testing.T) {
	wsDir := t.TempDir()
	outsideDir := t.TempDir()

	// 1. Create escaping symlink inside workspace pointing to outside directory
	escapingLink := filepath.Join(wsDir, "escape_link")
	if err := os.Symlink(outsideDir, escapingLink); err != nil {
		t.Fatal(err)
	}

	// 2. Create escaping symlink pointing to /var/run/docker.sock (or / if socket doesn't exist)
	targetSock := "/var/run/docker.sock"
	if _, err := os.Stat(targetSock); err != nil {
		targetSock = "/"
	}
	sockLink := filepath.Join(wsDir, "sock_link")
	if err := os.Symlink(targetSock, sockLink); err != nil {
		t.Fatal(err)
	}

	// 3. Compose mounting the escaping symlink
	composeBad := `
services:
  web:
    image: nginx:alpine
    volumes:
      - ./escape_link:/data
`
	if _, err := ValidateComposeSecurity([]byte(composeBad), wsDir); err == nil {
		t.Fatal("expected compose with escaping symlink to be blocked, but it passed")
	} else if !strings.Contains(err.Error(), "resolves via symlink") {
		t.Fatalf("expected symlink escape security error, got: %v", err)
	}

	composeSock := `
services:
  web:
    image: nginx:alpine
    volumes:
      - ./sock_link:/var/run/docker.sock
`
	if _, err := ValidateComposeSecurity([]byte(composeSock), wsDir); err == nil {
		t.Fatal("expected compose with escaping socket symlink to be blocked, but it passed")
	} else if !strings.Contains(err.Error(), "resolves via symlink") {
		t.Fatalf("expected symlink escape security error, got: %v", err)
	}

	// 4. Broken escaping symlink (pointing to non-existent path outside workspace)
	brokenLink := filepath.Join(wsDir, "broken_escape_link")
	if err := os.Symlink("/var/run/nonexistent_socket_target", brokenLink); err != nil {
		t.Fatal(err)
	}
	composeBroken := `
services:
  web:
    image: nginx:alpine
    volumes:
      - ./broken_escape_link:/data
`
	if _, err := ValidateComposeSecurity([]byte(composeBroken), wsDir); err == nil {
		t.Fatal("expected compose with broken escaping symlink to be blocked, but it passed")
	} else if !strings.Contains(err.Error(), "resolves via symlink") {
		t.Fatalf("expected symlink escape security error for broken symlink, got: %v", err)
	}

	// 5. Safe internal directory should succeed
	safeDir := filepath.Join(wsDir, "safe_dir")
	if err := os.MkdirAll(safeDir, 0755); err != nil {
		t.Fatal(err)
	}
	composeGood := `
services:
  web:
    image: nginx:alpine
    volumes:
      - ./safe_dir:/data
`
	if _, err := ValidateComposeSecurity([]byte(composeGood), wsDir); err != nil {
		t.Fatalf("expected safe compose to pass, got: %v", err)
	}
}

