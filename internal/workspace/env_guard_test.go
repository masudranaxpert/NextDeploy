package workspace

import (
	"testing"
)

func TestIsProtectedEnvFile(t *testing.T) {
	cases := []struct {
		path      string
		protected bool
	}{
		{".env", true},
		{".env.local", true},
		{".env.production", true},
		{"sub/dir/.env", true},
		{"sub/dir/.env.prod", true},
		{"config/app.env", true},
		{".env.example", false},
		{".env.sample", false},
		{".env.template", false},
		{".env.dist", false},
		{"environment.js", false},
		{"main.go", false},
		{"docker-compose.yml", false},
	}

	for _, tc := range cases {
		got := IsProtectedEnvFile(tc.path)
		if got != tc.protected {
			t.Errorf("IsProtectedEnvFile(%q) = %v; want %v", tc.path, got, tc.protected)
		}
	}
}
