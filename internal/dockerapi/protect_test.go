package dockerapi

import "testing"

func TestIsProtectedImage(t *testing.T) {
	if !isProtectedImage([]string{"masudranaxpert/nextdeploy-php:8.2"}) {
		t.Fatal("php runtime image must be protected")
	}
	if isProtectedImage([]string{"cloak-traffic-engine-5381-green-app:latest"}) {
		t.Fatal("app slot image must be prunable")
	}
}
