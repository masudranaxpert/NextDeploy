package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
