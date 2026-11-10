package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"nd/internal/client"
	"nd/internal/config"
)

func TestPHPRulesAndPowerCLI(t *testing.T) {
	var saved phpSiteRouting
	var stoppedCalled, startedCalled, scanApplyCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/php/sites/mysite/stop":
			stoppedCalled = true
			_, _ = w.Write([]byte(`{"ok":true,"status":"stopped"}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/php/sites/mysite/start":
			startedCalled = true
			_, _ = w.Write([]byte(`{"ok":true,"status":"running"}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/php/sites/mysite/routing":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "routing": saved})
		case r.Method == "PUT" && r.URL.Path == "/api/v1/php/sites/mysite/routing":
			var next phpSiteRouting
			if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
				t.Fatal(err)
			}
			saved = next
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/php/sites/mysite/routing/scan-htaccess":
			var body struct {
				Apply bool `json:"apply"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			scanApplyCalled = body.Apply
			_, _ = w.Write([]byte(`{"ok":true,"result":{"files_scanned":1,"files":[".htaccess"],"rules":[{"id":"ht-1","kind":"rewrite","match_type":"regex","pattern":"^blog/(.*)$","target":"/post.php?slug=$1","enabled":true}],"diagnostics":[{"file":".htaccess","line":2,"severity":"warning","directive":"Options","message":"ignored"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cl := client.New(config.Config{ServerURL: srv.URL, Token: "test-tok"}, "")

	if err := RunPHP(cl, []string{"stop", "mysite"}); err != nil || !stoppedCalled {
		t.Fatalf("stop failed: err=%v called=%v", err, stoppedCalled)
	}
	if err := RunPHP(cl, []string{"start", "mysite"}); err != nil || !startedCalled {
		t.Fatalf("start failed: err=%v called=%v", err, startedCalled)
	}
	if err := RunPHP(cl, []string{"rules", "add", "mysite", "redir", "prefix", "/old", "/new", "--status", "301"}); err != nil {
		t.Fatalf("rules add: %v", err)
	}
	if len(saved.Rules) != 1 || saved.Rules[0].Target != "/new" || saved.Rules[0].Status != 301 {
		t.Fatalf("unexpected saved rules after add: %+v", saved.Rules)
	}
	if err := RunPHP(cl, []string{"rules", "fallback", "mysite", "strict_404"}); err != nil {
		t.Fatalf("rules fallback: %v", err)
	}
	if saved.FallbackMode != "strict_404" {
		t.Fatalf("unexpected fallback mode: %q", saved.FallbackMode)
	}
	if err := RunPHP(cl, []string{"rules", "scan", "mysite", "--apply"}); err != nil || !scanApplyCalled {
		t.Fatalf("rules scan: err=%v apply=%v", err, scanApplyCalled)
	}
	if err := RunPHP(cl, []string{"rules", "rm", "mysite", "1"}); err != nil {
		t.Fatalf("rules rm: %v", err)
	}
	if len(saved.Rules) != 0 {
		t.Fatalf("expected 0 rules after rm, got %+v", saved.Rules)
	}
	if err := RunPHP(cl, []string{"rules", "unknown", "mysite"}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected error on unknown subcommand, got %v", err)
	}
}

func TestPHPCreateDBGrantCronAndBatchManifestCLI(t *testing.T) {
	var createdPayload map[string]any
	var grantPayload map[string]any
	var cronPayload map[string]string
	var batchSaved phpSiteRouting

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/php/sites":
			_ = json.NewDecoder(r.Body).Decode(&createdPayload)
			_, _ = w.Write([]byte(`{"ok":true,"id":"blog-abc1","app_id":"blog-abc1","name":"blog","domain":"blog.example.com","db_name":"u1_appdb","db_user":"u1_appdb","db_password":"secret"}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/php/sites/blog-abc1":
			_, _ = w.Write([]byte(`{"ok":true,"app_id":"blog-abc1","databases":[{"id":1,"db_name":"u1_shop"}],"db_users":[{"id":2,"user_name":"u1_alice"}],"grants":[]}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/php/sites/blog-abc1/db-users/2/grants":
			_ = json.NewDecoder(r.Body).Decode(&grantPayload)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/php/sites/blog-abc1/cron":
			_ = json.NewDecoder(r.Body).Decode(&cronPayload)
			_, _ = w.Write([]byte(`{"ok":true,"id":7}`))
		case r.Method == "PUT" && r.URL.Path == "/api/v1/php/sites/blog-abc1/routing":
			_ = json.NewDecoder(r.Body).Decode(&batchSaved)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cl := client.New(config.Config{ServerURL: srv.URL, Token: "test-tok"}, "")

	// 1. Create with custom domain, no-default-domain, and --db
	if err := RunPHP(cl, []string{"create", "blog", "blog.example.com", "--no-default-domain", "--db", "appdb"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if createdPayload["domain"] != "blog.example.com" || createdPayload["no_default_domain"] != true || createdPayload["db_name"] != "appdb" {
		t.Fatalf("unexpected create payload: %+v", createdPayload)
	}

	// 2. DB grant by short name
	if err := RunPHP(cl, []string{"db", "grant", "blog-abc1", "alice", "shop", "--access", "all"}); err != nil {
		t.Fatalf("db grant failed: %v", err)
	}
	if grantPayload["grants"] == nil {
		t.Fatalf("expected grants payload, got %+v", grantPayload)
	}

	// 3. Cron add with --schedule & --command
	if err := RunPHP(cl, []string{"cron", "add", "blog-abc1", "--schedule", "*/5 * * * *", "--command", "php artisan schedule:run"}); err != nil {
		t.Fatalf("cron add failed: %v", err)
	}
	if cronPayload["schedule"] != "*/5 * * * *" || cronPayload["command"] != "php artisan schedule:run" {
		t.Fatalf("unexpected cron payload: %+v", cronPayload)
	}

	// 4. Batch rules import from JSON file & manifest import
	tmp := t.TempDir()
	rulesFile := tmp + "/rules.json"
	if err := os.WriteFile(rulesFile, []byte(`{"fallback_mode":"strict_404","rules":[{"kind":"redir","match_type":"prefix","pattern":"/legacy","target":"/docs","status":301,"enabled":true,"preserve_query":true}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RunPHP(cl, []string{"rules", "import", "blog-abc1", rulesFile}); err != nil {
		t.Fatalf("rules import failed: %v", err)
	}
	if batchSaved.FallbackMode != "strict_404" || len(batchSaved.Rules) != 1 || !batchSaved.Rules[0].PreserveQuery {
		t.Fatalf("unexpected imported rules: %+v", batchSaved)
	}

	manifestFile := tmp + "/manifest.json"
	if err := os.WriteFile(manifestFile, []byte(`{"sites":[{"name":"shop","type":"php","php_version":"8.3","domain":"shop.example.com","no_default_domain":true,"create_db":true,"db_name":"shopdb","routing":{"rules":[{"kind":"block","match_type":"prefix","pattern":"/.git/","status":404,"enabled":true}]}}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RunPHP(cl, []string{"import-manifest", manifestFile}); err != nil {
		t.Fatalf("import-manifest failed: %v", err)
	}
	if batchSaved.Rules[0].Kind != "block" {
		t.Fatalf("expected manifest routing to save block rule, got %+v", batchSaved.Rules)
	}
}
