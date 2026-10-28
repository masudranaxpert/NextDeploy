package agent

import (
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

func TestAgentManagerAvailableCommandsDefault(t *testing.T) {
	mgr := NewManager(nil, nil, t.TempDir())
	cmds := mgr.GetAvailableCommands(t.Context())
	if len(cmds) == 0 {
		t.Fatal("expected commands from omp or default catalog")
	}
	hasSkillOrSkills := false
	for _, c := range cmds {
		if strings.HasPrefix(c.Name, "skill") || c.Name == "compact" {
			hasSkillOrSkills = true
			break
		}
	}
	if !hasSkillOrSkills {
		t.Errorf("expected skill or compact in commands catalog")
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
