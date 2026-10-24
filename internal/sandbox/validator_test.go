package sandbox

import (
	"fmt"
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

