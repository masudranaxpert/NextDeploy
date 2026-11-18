package dockerapi

import (
	"testing"

	"github.com/docker/docker/api/types"
)

func TestMatchContainerNameExactProxyMatching(t *testing.T) {
	cases := []struct {
		names  []string
		target string
		want   bool
	}{
		{names: []string{"/caddy"}, target: "caddy", want: true},
		{names: []string{"/nextdeploy_caddy"}, target: "caddy", want: true},
		{names: []string{"/my-app-caddy-1"}, target: "caddy", want: false},
		{names: []string{"/caddy-metrics"}, target: "caddy", want: false},
		{names: []string{"/nd-proxy"}, target: "nd-proxy", want: true},
		{names: []string{"/nextdeploy_nd-proxy"}, target: "nd-proxy", want: true},
		{names: []string{"/nd-proxy-sidecar"}, target: "nd-proxy", want: false},
		{names: []string{"/blog-web-1"}, target: "blog-web-1", want: true},
		{names: []string{"/blog-web-1"}, target: "", want: false},
	}
	for _, tc := range cases {
		if got := matchContainerName(tc.names, tc.target); got != tc.want {
			t.Errorf("matchContainerName(%v, %q) = %v, want %v", tc.names, tc.target, got, tc.want)
		}
	}
}

func TestIsProtectedImageMultiProxy(t *testing.T) {
	protected := [][]string{
		{"caddy-docker-proxy:2.9"},
		{"docker.angie.software/angie:1.12.4"},
		{"angie:latest"},
		{"ferronserver/ferron:3.0.0-rc.11-alpine"},
		{"ferron:3"},
		{"nextdeploy-php:8.3"},
		{"phpmyadmin:latest"},
		{"mariadb:11"},
		{"busybox:stable"},
	}
	for _, tags := range protected {
		if !isProtectedImage(tags) {
			t.Errorf("isProtectedImage(%v) = false, want true", tags)
		}
	}

	unprotected := [][]string{
		{"node:22-bookworm"},
		{"myapp:v1.2.3"},
		{"ghcr.io/example/api:latest"},
	}
	for _, tags := range unprotected {
		if isProtectedImage(tags) {
			t.Errorf("isProtectedImage(%v) = true, want false", tags)
		}
	}
}

func TestClassifyLivePortStateMultiProxy(t *testing.T) {
	containers := []types.Container{
		{
			Names: []string{"/nd-proxy"},
			State: "running",
			Labels: map[string]string{
				"com.docker.compose.project": "nextdeploy",
				"com.docker.compose.service": "proxy",
			},
			Ports: []types.Port{
				{PublicPort: 80},
				{PublicPort: 443},
				{PublicPort: 9090},
			},
		},
		{
			Names: []string{"/blog-app-1"},
			State: "running",
			Labels: map[string]string{
				"com.docker.compose.project": "blog",
			},
			Ports: []types.Port{
				{PublicPort: 3000},
			},
		},
		{
			Names: []string{"/stopped-worker"},
			State: "exited",
			Labels: map[string]string{
				"com.docker.compose.project": "stopped",
			},
			Ports: []types.Port{
				{PublicPort: 9999},
			},
		},
	}

	st := classifyLivePortState(containers)
	if st.ProxyContainer != "nd-proxy" {
		t.Fatalf("ProxyContainer = %q, want nd-proxy", st.ProxyContainer)
	}
	if !st.CaddyBoundPorts[9090] || st.CaddyBoundPorts[80] || st.CaddyBoundPorts[443] {
		t.Fatalf("unexpected CaddyBoundPorts = %v", st.CaddyBoundPorts)
	}
	if st.DirectHostPorts[9090] {
		t.Fatalf("nd-proxy gateway port 9090 leaked into DirectHostPorts")
	}
	if !st.DirectHostPorts[3000] || st.DirectHostPorts[9999] {
		t.Fatalf("unexpected DirectHostPorts = %v", st.DirectHostPorts)
	}
	if !st.RunningProjects["nextdeploy"] || !st.RunningProjects["blog"] || st.RunningProjects["stopped"] {
		t.Fatalf("unexpected RunningProjects = %v", st.RunningProjects)
	}
}
