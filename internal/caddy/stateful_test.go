package caddy

import (
	"reflect"
	"testing"
)

func TestStatefulServices(t *testing.T) {
	compose := []byte(`
services:
  app:
    build: .
    volumes: [uploads:/app/uploads]
  db:
    image: postgres:16-alpine
    volumes:
      - pgdata:/var/lib/postgresql/data
  cache:
    image: docker.io/library/redis:7@sha256:abc
    volumes:
      - type: volume
        source: redisdata
        target: /data
  scratch:
    image: mysql:8
  bound:
    image: mariadb:11
    volumes: ["./data:/var/lib/mysql"]
  replica:
    image: postgres:16
    volumes: [replica:/var/lib/postgresql/data]
    labels: {nd.stateful: "false"}
  custom:
    image: ghcr.io/me/ledger:1
    volumes: [ledger:/data]
    labels: ["nd.stateful=true"]
volumes: {uploads: {}, pgdata: {}, redisdata: {}, replica: {}, ledger: {}}
`)
	got := StatefulServices(compose)
	want := []string{"cache", "custom", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StatefulServices = %v, want %v", got, want)
	}
	if StatefulServices([]byte("not: [yaml")) != nil {
		t.Fatal("bad yaml must give nil")
	}
}
