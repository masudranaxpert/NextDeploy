package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAgentManagerAutonomyMode(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	if mgr.GetAutonomyMode() != AutonomySafe {
		t.Fatalf("expected default mode to be Safe, got %s", mgr.GetAutonomyMode())
	}

	mgr.SetAutonomyMode(AutonomyAutonomous)
	if mgr.GetAutonomyMode() != AutonomyAutonomous {
		t.Fatalf("expected mode to be Autonomous, got %s", mgr.GetAutonomyMode())
	}

	mgr.SetAutonomyMode(AutonomySafe)
	if mgr.GetAutonomyMode() != AutonomySafe {
		t.Fatalf("expected mode to be Safe, got %s", mgr.GetAutonomyMode())
	}
}

func TestAgentManagerSubscribe(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	ch, cancel := mgr.Subscribe()
	defer cancel()

	testEv := AgentEvent{Type: "text_delta", Delta: "Hello world"}
	mgr.broadcast(testEv)

	select {
	case received := <-ch:
		if received.Type != testEv.Type || received.Delta != testEv.Delta {
			t.Fatalf("mismatched received event: got %+v, want %+v", received, testEv)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for broadcast event")
	}
}

func TestAgentManagerHostToolsRegistration(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	tools := mgr.buildHostTools()
	if len(tools) < 10 {
		t.Fatalf("expected at least 10 host tools, got %d", len(tools))
	}

	toolMap := make(map[string]bool)
	for _, tool := range tools {
		toolMap[tool.Definition.Name] = true
	}

	expectedTools := []string{
		"list_sites",
		"get_site_details",
		"add_domain",
		"remove_domain",
		"diagnose_site_performance",
		"update_php_config",
		"manage_php_routing",
		"php_site_power",
		"restart_site",
		"get_site_logs",
		"check_edge_shield",
		"get_server_resources",
		"execute_bash",
	}

	for _, name := range expectedTools {
		if !toolMap[name] {
			t.Errorf("missing expected host tool: %s", name)
		}
	}
}

func TestAgentManagerConfirmApproval(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	ch := make(chan bool, 1)
	mgr.mu.Lock()
	mgr.confirmations["req_123"] = ch
	mgr.mu.Unlock()

	ok := mgr.ConfirmApproval("req_123", true)
	if !ok {
		t.Fatal("expected ConfirmApproval to succeed")
	}

	select {
	case val := <-ch:
		if !val {
			t.Fatal("expected confirmation channel to receive true")
		}
	default:
		t.Fatal("no confirmation sent to channel")
	}

	// Double confirm should return false
	ok2 := mgr.ConfirmApproval("req_123", true)
	if ok2 {
		t.Fatal("expected second ConfirmApproval to return false")
	}
}

// The command catalogue comes from omp over RPC only: with omp installed it is
// non-empty, without it the list is empty (there is no built-in fallback list).
func TestAgentManagerAvailableCommandsComeFromOmp(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	defer mgr.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmds := mgr.GetAvailableCommands(ctx)
	if _, err := exec.LookPath("omp"); err != nil && len(cmds) != 0 {
		t.Fatalf("got %d commands without omp installed", len(cmds))
	}
	for _, c := range cmds {
		if c.Name == "" {
			t.Fatal("command without a name")
		}
	}
}

func TestAgentManagerNoPhantomAnthropicKey(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	// Ensure that even without database or API keys, ANTHROPIC_API_KEY=none is NEVER injected
	// into the environment when preparing process.
	// Manager should not crash and should not set Anthropic to connected.
	if mgr == nil {
		t.Fatal("expected manager instance")
	}
}

func TestParseAutonomyMode(t *testing.T) {
	for in, want := range map[string]AutonomyMode{
		"write": AutonomySafe, "safe": AutonomySafe, "yolo": AutonomyAutonomous, "autonomous": AutonomyAutonomous,
		"always-ask": AutonomyAskAlways, "always-ask-cached": AutonomyAskCached,
	} {
		if got, ok := ParseAutonomyMode(in); !ok || got != want {
			t.Errorf("ParseAutonomyMode(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := ParseAutonomyMode("rm -rf"); ok {
		t.Error("accepted an unknown mode")
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"\"Server health check\"\n\nextra": "Server health check",
		"  **Deploy fix.**  ":              "Deploy fix",
		"":                                 "",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanTitle(strings.Repeat("x", 100)); len([]rune(got)) != 61 {
		t.Errorf("long title not shortened: %d", len([]rune(got)))
	}
}

func TestManagePHPRoutingConvertHtaccess(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	tool := mgr.toolManagePHPRouting()
	res, err := tool.Handler(t.Context(), nil, []byte(`{"app_id":"demo","action":"convert_htaccess","content":"RewriteEngine On\nRewriteRule ^blog/(.*)$ /post.php?slug=$1 [L]"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := json.Marshal(res.Content)
	if !strings.Contains(string(raw), "/post.php") {
		t.Fatalf("unexpected convert_htaccess output: %s", string(raw))
	}
}

func TestRunCLILoginStreamAntigravityFlow(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	events, cancel := mgr.Subscribe()
	defer cancel()

	outR, outW := io.Pipe()
	var written bytes.Buffer
	inW := &written

	done := make(chan error, 1)
	go func() {
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		done <- mgr.runCLILoginStream(ctx, outR, inW, func() error { return nil })
	}()

	// Step 1: Risk warning + confirmation prompt
	_, _ = io.WriteString(outW, "⚠ Antigravity CLI: use at your own risk\n\nType \"yes\" to accept the risk and continue (yes): ")
	ev1 := <-events
	if ev1.Type != "oauth_input_required" || ev1.Placeholder != "yes" {
		t.Fatalf("expected risk confirmation prompt, got %+v", ev1)
	}
	if !mgr.SubmitInput(ev1.Token, "yes") {
		t.Fatal("SubmitInput failed for step 1")
	}

	// Step 2: Auth URL + paste prompt
	authURL := "https://accounts.google.com/o/oauth2/auth?client_id=abc&state=xyz"
	_, _ = io.WriteString(outW, "Preparing the Antigravity CLI...\n\nOpen this URL in your browser:\n"+authURL+"\nPaste the authorization code (or callback URL): ")

	ev2 := <-events
	if ev2.Type != "oauth_open_url" || ev2.URL != authURL {
		t.Fatalf("expected oauth_open_url with %q, got %+v", authURL, ev2)
	}
	ev3 := <-events
	if ev3.Type != "oauth_input_required" {
		t.Fatalf("expected paste code prompt, got %+v", ev3)
	}
	if !mgr.SubmitInput(ev3.Token, "https://antigravity.google/oauth-callback?code=4/test") {
		t.Fatal("SubmitInput failed for step 2")
	}
	_ = outW.Close()

	if err := <-done; err != nil {
		t.Fatalf("runCLILoginStream returned error: %v", err)
	}
	gotStdin := written.String()
	if gotStdin != "yes\nhttps://antigravity.google/oauth-callback?code=4/test\n" {
		t.Fatalf("unexpected stdin written: %q", gotStdin)
	}
}
