package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nd/internal/client"
	"nd/internal/config"
)

func TestCLI_BackupLogAndSchedules(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/backups/history/42/log":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":     true,
				"log":    "Backup completed successfully in 12s",
				"status": "completed",
			})
		case "/api/v1/backups/schedules/10":
			if r.Method == http.MethodDelete {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
			} else {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	// Test runBackupLog
	if err := runBackupLog(cl, []string{"42"}); err != nil {
		t.Fatalf("runBackupLog failed: %v", err)
	}

	// Test runBackupSchedule delete
	if err := runBackupSchedule(cl, []string{"rm", "10"}); err != nil {
		t.Fatalf("runBackupSchedule rm failed: %v", err)
	}
}

func TestCLI_ShieldListAndReassign(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/shield-nodes":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"nodes": []map[string]interface{}{
					{
						"id":               int64(1),
						"name":             "edge-lon1",
						"ip":               "192.0.2.1",
						"status":           "active",
						"assigned_domains": 3,
					},
				},
				"origin_ip": "198.51.100.1",
			})
		case "/api/v1/shield-nodes/1/reassign":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["target_shield_id"] != float64(2) {
				t.Errorf("expected target_shield_id=2, got %v", body["target_shield_id"])
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	if err := runShieldList(cl, nil); err != nil {
		t.Fatalf("runShieldList failed: %v", err)
	}

	if err := runShieldReassign(cl, []string{"1", "2"}); err != nil {
		t.Fatalf("runShieldReassign failed: %v", err)
	}
}

func TestCLI_ShieldTest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/shield-nodes/1/test":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":        true,
				"reachable": true,
			})
		case "/api/v1/shield-nodes/2/test":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":        false,
				"reachable": false,
				"error":     "TCP port 80 unreachable on 192.0.2.2: dial tcp 192.0.2.2:80: i/o timeout",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	if err := runShieldTest(cl, []string{"1"}); err != nil {
		t.Fatalf("expected node 1 test to succeed, got %v", err)
	}

	if err := runShieldTest(cl, []string{"2"}); err == nil {
		t.Fatalf("expected node 2 test to fail, but it returned nil")
	}
}

func TestCLI_ProxyList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/proxy/routes" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"routes": []map[string]interface{}{
					{
						"id":         int64(100),
						"domain":     "api.example.com",
						"upstream":   "http://10.0.0.5:8000",
						"route_type": "proxy",
						"enabled":    true,
					},
				},
			})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	if err := runProxyList(cl, nil); err != nil {
		t.Fatalf("runProxyList failed: %v", err)
	}
}

func TestCLI_VolumeRestore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "vol_restore_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tarFile := filepath.Join(tmpDir, "backup.tar.gz")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{Name: "data.txt", Size: 4, Mode: 0644})
	_, _ = tw.Write([]byte("test"))
	_ = tw.Close()
	_ = gw.Close()
	if err := os.WriteFile(tarFile, buf.Bytes(), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	pollCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/volumes/restore":
			err := r.ParseMultipartForm(10 << 20)
			if err != nil {
				t.Errorf("ParseMultipartForm failed: %v", err)
			}
			name := r.FormValue("name")
			if name != "my-vol" {
				t.Errorf("expected name=my-vol, got %q", name)
			}
			vol := r.FormValue("volume")
			if vol != "my-vol" {
				t.Errorf("expected volume=my-vol, got %q", vol)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":     true,
				"job_id": "job-12345",
			})
		case "/api/v1/volumes/restore/status":
			pollCount++
			w.Header().Set("Content-Type", "application/json")
			if pollCount < 2 {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "extracting",
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "completed",
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	if err := runVolumeRestore(cl, []string{"my-vol", tarFile}); err != nil {
		t.Fatalf("runVolumeRestore failed: %v", err)
	}
}

func TestCLI_PHPDBExportImport(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "php_db_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sqlDump := "-- MySQL dump\nCREATE TABLE test (id INT);\n"
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write([]byte(sqlDump))
	_ = gw.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/php/sites/site-1/databases/1/export":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(gzBuf.Bytes())
		case "/api/v1/php/sites/site-1/databases/1/import":
			ct := r.Header.Get("Content-Type")
			if ct == "" {
				t.Errorf("missing Content-Type")
			}
			data, _ := io.ReadAll(r.Body)
			if len(data) == 0 {
				t.Errorf("empty import body")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	// Export to uncompressed .sql file
	exportFile := filepath.Join(tmpDir, "dump.sql")
	if err := runPHPDB(cl, []string{"export", "site-1", "1", exportFile}); err != nil {
		t.Fatalf("runPHPDB export failed: %v", err)
	}
	content, err := os.ReadFile(exportFile)
	if err != nil || string(content) != sqlDump {
		t.Fatalf("unexpected export content: %q, err: %v", string(content), err)
	}

	// Import from .sql file
	if err := runPHPDB(cl, []string{"import", "site-1", "1", exportFile}); err != nil {
		t.Fatalf("runPHPDB import failed: %v", err)
	}
}

func TestCLI_EnvUnset(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			params := req["params"].(map[string]interface{})
			name := params["name"].(string)
			args := params["arguments"].(map[string]interface{})
			if name != "env_unset" {
				t.Errorf("expected env_unset, got %s", name)
			}
			if args["app_id"] != "test-app" {
				t.Errorf("expected app_id=test-app, got %v", args["app_id"])
			}
			keys := args["keys"].([]interface{})
			if len(keys) != 1 || keys[0] != "OLD_KEY" {
				t.Errorf("expected keys=[OLD_KEY], got %v", keys)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{"text": "Environment variable \"OLD_KEY\" unset successfully"},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	if err := RunEnv(cl, []string{"unset", "test-app", "OLD_KEY"}); err != nil {
		t.Fatalf("RunEnv unset failed: %v", err)
	}
}

func TestCLI_LinkedProjectDisambiguation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "linked_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origCwd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer os.Chdir(origCwd)

	_ = config.SaveProject(".", "linked-app")

	var lastAction string
	var lastAppID string
	var lastService string

	var lastScheduleAppID string
	var lastScheduleDestID int64
	var lastScheduleCron string
	var lastScheduleRetention int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			params := req["params"].(map[string]interface{})
			lastAction = params["name"].(string)
			args := params["arguments"].(map[string]interface{})
			lastAppID = fmt.Sprint(args["app_id"])
			if s, ok := args["service"]; ok {
				lastService = fmt.Sprint(s)
			} else {
				lastService = ""
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"ok": true,
					"content": []map[string]interface{}{
						{"text": "success"},
					},
				},
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/backups/restore/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
			return
		}
		if r.URL.Path == "/api/v1/backups/schedules" && r.Method == http.MethodPost {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			lastScheduleAppID = fmt.Sprint(payload["app_id"])
			if d, ok := payload["dest_id"].(float64); ok {
				lastScheduleDestID = int64(d)
			}
			lastScheduleCron = fmt.Sprint(payload["cron"])
			if ret, ok := payload["retention"].(float64); ok {
				lastScheduleRetention = int(ret)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/backups/schedules/") && r.Method == http.MethodDelete {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	// 1. nd stop redis in linked project -> app=linked-app, service=redis
	if err := RunStop(cl, []string{"redis"}); err != nil {
		t.Fatalf("RunStop redis failed: %v", err)
	}
	if lastAppID != "linked-app" || lastService != "redis" {
		t.Errorf("expected linked-app / redis, got %s / %s", lastAppID, lastService)
	}

	// 2. nd restart web in linked project -> app=linked-app, service=web
	if err := RunRestart(cl, []string{"web"}); err != nil {
		t.Fatalf("RunRestart web failed: %v", err)
	}
	if lastAppID != "linked-app" || lastService != "web" {
		t.Errorf("expected linked-app / web, got %s / %s", lastAppID, lastService)
	}

	// 3. nd backup restore 42 in linked project -> app=linked-app, histID=42
	if err := runBackupRestore(cl, []string{"42"}); err != nil {
		t.Fatalf("runBackupRestore 42 failed: %v", err)
	}

	// 4. nd env unset OLD_VAR in linked project -> app=linked-app, keys=[OLD_VAR]
	if err := RunEnv(cl, []string{"unset", "OLD_VAR"}); err != nil {
		t.Fatalf("RunEnv unset failed in linked project: %v", err)
	}
	if lastAction != "env_unset" || lastAppID != "linked-app" {
		t.Errorf("expected env_unset on linked-app, got action=%s app=%s", lastAction, lastAppID)
	}

	// 5. nd env get OLD_VAR in linked project -> app=linked-app
	if err := RunEnv(cl, []string{"get", "OLD_VAR"}); err != nil {
		t.Fatalf("RunEnv get failed in linked project: %v", err)
	}
	if lastAction != "env_reveal" || lastAppID != "linked-app" {
		t.Errorf("expected env_reveal on linked-app, got action=%s app=%s", lastAction, lastAppID)
	}

	// 6. nd env set FOO=BAR in linked project -> app=linked-app
	if err := RunEnv(cl, []string{"set", "FOO=BAR"}); err != nil {
		t.Fatalf("RunEnv set failed in linked project: %v", err)
	}
	if lastAction != "env_set" || lastAppID != "linked-app" {
		t.Errorf("expected env_set on linked-app, got action=%s app=%s", lastAction, lastAppID)
	}

	// 7. nd backup schedule add 5 "0 3 * * *" in linked project -> app=linked-app, dest=5
	if err := runBackupSchedule(cl, []string{"add", "5", "0 3 * * *", "--retention", "14"}); err != nil {
		t.Fatalf("runBackupSchedule add failed in linked project: %v", err)
	}
	if lastScheduleAppID != "linked-app" || lastScheduleDestID != 5 || lastScheduleCron != "0 3 * * *" || lastScheduleRetention != 14 {
		t.Errorf("expected linked-app / dest=5 / retention=14, got %s / %d / %d", lastScheduleAppID, lastScheduleDestID, lastScheduleRetention)
	}

	// 8. nd backup schedule rm 77 in linked project
	if err := runBackupSchedule(cl, []string{"rm", "77"}); err != nil {
		t.Fatalf("runBackupSchedule rm failed: %v", err)
	}
}

func TestCLI_ProxyLogsToggleAndOptions(t *testing.T) {
	var seenTail string
	var toggledTo *bool
	var savedRealIP string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/proxy/logs":
			seenTail = r.URL.Query().Get("tail")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    true,
				"lines": []string{"2026/11/13 INFO caddy started", "2026/11/13 INFO serving TLS"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/proxy/routes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"routes": []map[string]any{{
					"id":           7,
					"domain":       "api.example.com",
					"target":       "http://app:3000",
					"kind":         "proxy",
					"enable_https": true,
					"enabled":      true,
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/proxy/routes/7/toggle":
			var body struct {
				Enabled bool `json:"enabled"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			toggledTo = &body.Enabled
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/proxy/options":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"options": map[string]any{
					"profile":  "balanced",
					"real_ip":  "none",
					"idle":     "2m",
					"trusted_proxies": []string{},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/proxy/options":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			savedRealIP, _ = body["real_ip"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "test-token"}, "test-sess")

	out, err := captureOutput(func() error {
		return RunProxy(cl, []string{"logs", "-n", "20"})
	})
	if err != nil {
		t.Fatalf("proxy logs failed: %v", err)
	}
	if seenTail != "20" {
		t.Fatalf("expected query ?tail=20, got %q", seenTail)
	}
	if !strings.Contains(out, "caddy started") || strings.Contains(out, `"lines"`) {
		t.Fatalf("expected plain log lines, got: %s", out)
	}

	if err := RunProxy(cl, []string{"toggle", "7"}); err != nil {
		t.Fatalf("proxy toggle failed: %v", err)
	}
	if toggledTo == nil || *toggledTo != false {
		t.Fatalf("expected toggle on enabled route #7 to disable, got %v", toggledTo)
	}

	if err := RunProxy(cl, []string{"options", "set", "--real-ip", "cloudflare"}); err != nil {
		t.Fatalf("proxy options set failed: %v", err)
	}
	if savedRealIP != "cloudflare" {
		t.Fatalf("expected real_ip=cloudflare, got %q", savedRealIP)
	}
}
