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

func TestIsProtectedImageMatchesNameNotTag(t *testing.T) {
	prunable := []string{
		"masudranaxpert/nextdeploy:backup-20261118-angie503",
		"masudranaxpert/nextdeploy:backup-20261118-ferronhdr",
		"masudranaxpert/nextdeploy:latest",
		"busybox:1",
		"alpine:3.19",
		"myapp:angie",
		"someone/angie-tools:1",
	}
	for _, ref := range prunable {
		if isProtectedImage([]string{ref}) {
			t.Errorf("%s must be prunable", ref)
		}
	}
	kept := []string{
		"alpine:3.20", "docker.io/alpine:3.20", "busybox:stable", "docker.io/library/busybox:stable",
		"docker.angie.software/angie:1.12.4", "ferronserver/ferron:3.0.0-rc.11-alpine",
		"masudranaxpert/nextdeploy-php:8.3", "phpmyadmin:5-apache", "mariadb:11.4", "caddy-docker-proxy:2.9",
		"registry.local:5000/team/mariadb:11",
	}
	for _, ref := range kept {
		if !isProtectedImage([]string{ref}) {
			t.Errorf("%s must be protected", ref)
		}
	}
}
