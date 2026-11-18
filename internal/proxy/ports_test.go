package proxy

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseComposeEnv(t *testing.T) {
	dotenv := "# comment\nAPP_PORT=3000\nexport DB_PORT=\"5432\"\nQUOTED='hello'\n"
	panelEnv := "APP_PORT=8080\nEXTRA=ok\n"

	got := ParseComposeEnv(dotenv, panelEnv)
	if got["APP_PORT"] != "8080" {
		t.Fatalf("expected panelEnv to override dotenv APP_PORT, got %q", got["APP_PORT"])
	}
	if got["DB_PORT"] != "5432" || got["QUOTED"] != "hello" || got["EXTRA"] != "ok" {
		t.Fatalf("unexpected parsed env map: %+v", got)
	}
}

func TestExtractAndPartitionHostPorts(t *testing.T) {
	inputYAML := `
services:
  web:
    image: mynode:latest
    ports:
      - "4013:3000"
      - "127.0.0.1:${WEB_PORT:-8081}:8081"
      - "5000:5000/udp"
      - "9000"
  db:
    image: postgres:16
    ports:
      - target: 5432
        published: 54325
        protocol: tcp
  pgadmin:
    image: dpage/pgadmin4:latest
    ports:
      - "5050:80"
  tools:
    image: busybox
    profiles: ["debug"]
    ports:
      - "9999:9999"
`
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(inputYAML), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	services := doc["services"].(map[string]interface{})

	env := ParseComposeEnv("WEB_PORT=18081\n")
	bindings, err := ExtractAndPartitionHostPorts(services, true, env)
	if err != nil {
		t.Fatalf("ExtractAndPartitionHostPorts: %v", err)
	}

	if len(bindings) != 5 {
		t.Fatalf("expected 5 bindings (skipping 9000 and profiled tools), got %d: %+v", len(bindings), bindings)
	}

	byHost := map[int]HostPortBinding{}
	for _, b := range bindings {
		byHost[b.HostPort] = b
	}
	if byHost[4013].Direct || byHost[18081].Direct {
		t.Fatalf("expected web HTTP ports 4013 and 18081 to have Direct=false: %+v", byHost)
	}
	if !byHost[5000].Direct || byHost[5000].Protocol != "udp" {
		t.Fatalf("expected UDP 5000 to have Direct=true: %+v", byHost[5000])
	}
	if !byHost[54325].Direct {
		t.Fatalf("expected postgres 54325 to have Direct=true: %+v", byHost[54325])
	}
	if byHost[5050].Direct {
		t.Fatalf("expected pgadmin4 admin UI port 5050 to have Direct=false: %+v", byHost[5050])
	}

	// Direct ports remain on services when keepDirect=true; HTTP ports stripped.
	dbSvc := services["db"].(map[string]interface{})
	dbPorts := dbSvc["ports"].([]interface{})
	if len(dbPorts) != 1 || dbPorts[0] != "54325:5432" {
		t.Fatalf("expected direct postgres port kept on db service, got %+v", dbSvc["ports"])
	}
	webSvc := services["web"].(map[string]interface{})
	webPorts := webSvc["ports"].([]interface{})
	if len(webPorts) != 2 {
		t.Fatalf("expected web ports to keep 5000:5000/udp and standalone 9000, got %+v", webPorts)
	}
}

func TestExtractAndStripHostPortsAndParseErrors(t *testing.T) {
	inputYAML := `
services:
  cache:
    image: redis:7-alpine
    ports:
      - "6379:6379"
`
	var doc map[string]interface{}
	_ = yaml.Unmarshal([]byte(inputYAML), &doc)
	services := doc["services"].(map[string]interface{})

	bindings, err := ExtractAndStripHostPorts(services)
	if err != nil {
		t.Fatalf("ExtractAndStripHostPorts: %v", err)
	}
	if len(bindings) != 1 || !bindings[0].Direct {
		t.Fatalf("expected 1 direct redis binding, got %+v", bindings)
	}
	cacheSvc := services["cache"].(map[string]interface{})
	if _, hasPorts := cacheSvc["ports"]; hasPorts {
		t.Fatalf("ExtractAndStripHostPorts should strip all host ports, got %+v", cacheSvc["ports"])
	}

	b, ok, err := ParsePortString("web", "localhost:8080:80")
	if err != nil || !ok || b.BindIP != "127.0.0.1" || b.HostPort != 8080 || b.ContainerPort != 80 {
		t.Fatalf("ParsePortString localhost failed: b=%+v ok=%v err=%v", b, ok, err)
	}
	for _, bad := range []string{"bad:80", "80:bad", "invalid-ip:80:80", "1:2:3:4"} {
		if _, _, err := ParsePortString("web", bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
