package caddy

import (
	"strings"
	"testing"
)

const devBase = `services:
  web:
    build: .
    ports: ["3000:3000"]
  worker:
    build: ./worker
    volumes:
      - ./data:/var/data
  db:
    image: postgres:16
`

func TestDevMountTargetsBuildServicesOnly(t *testing.T) {
	out, err := GenerateMergedCompose([]byte(devBase), "proj", nil, "", "", DevMount{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Count(s, "./:/app") != 2 {
		t.Fatalf("want the mount on web and worker but not db, got:\n%s", s)
	}
	if !strings.Contains(s, "./data:/var/data") {
		t.Fatalf("existing volume was dropped:\n%s", s)
	}
}

func TestDevMountHonoursExplicitServiceAndTarget(t *testing.T) {
	out, err := GenerateMergedCompose([]byte(devBase), "proj", nil, "", "", DevMount{Enabled: true, Service: "db", Target: "/src"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), "./:/src") != 1 {
		t.Fatalf("explicitly named service was not mounted:\n%s", out)
	}
}

func TestDevMountDisabledInjectsNothing(t *testing.T) {
	out, err := GenerateMergedCompose([]byte(devBase), "proj", nil, "", "", DevMount{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "./:/app") {
		t.Fatalf("mount injected while dev mode is off:\n%s", out)
	}
}

func TestDevMountFallsBackOnUnusableTarget(t *testing.T) {
	for _, target := range []string{"", "/", "relative/path", "/app:ro"} {
		out, err := GenerateMergedCompose([]byte(devBase), "proj", nil, "", "", DevMount{Enabled: true, Service: "web", Target: target})
		if err != nil {
			t.Fatalf("target %q: %v", target, err)
		}
		if !strings.Contains(string(out), "./:"+DefaultDevTarget) {
			t.Fatalf("target %q did not fall back to the default:\n%s", target, out)
		}
	}
}

func TestDevMountSkipsExistingTargetWithOptions(t *testing.T) {
	base := []byte(`services:
  web:
    build: .
    volumes:
      - ./:/app:ro
`)
	out, err := GenerateMergedCompose(base, "proj", nil, "", "", DevMount{Enabled: true, Service: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), "./:/app") != 1 {
		t.Fatalf("expected existing :ro mount to count as the same target:\n%s", out)
	}
}

func TestDevMountSkipsExistingLongFormTarget(t *testing.T) {
	base := []byte(`services:
  web:
    build: .
    volumes:
      - type: bind
        source: ./src
        target: /app
`)
	out, err := GenerateMergedCompose(base, "proj", nil, "", "", DevMount{Enabled: true, Service: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "./:/app") {
		t.Fatalf("expected long-form target /app to block the short mount:\n%s", out)
	}
}

func TestDevMountIsIdempotent(t *testing.T) {
	once, err := GenerateMergedCompose([]byte(devBase), "proj", nil, "", "", DevMount{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	twice, err := GenerateMergedCompose(once, "proj", nil, "", "", DevMount{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(twice), "./:/app") != 2 {
		t.Fatalf("re-generating duplicated the mount:\n%s", twice)
	}
}
