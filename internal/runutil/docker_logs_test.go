package runutil

import "testing"

func TestSetDefaultComposeLoggingKeepsUserBlock(t *testing.T) {
	own := map[string]any{"driver": "syslog"}
	svc := map[string]any{"image": "x", "logging": own}
	SetDefaultComposeLogging(svc)
	if got := svc["logging"].(map[string]any)["driver"]; got != "syslog" {
		t.Fatalf("user logging block replaced: %v", svc["logging"])
	}

	bare := map[string]any{"image": "x"}
	SetDefaultComposeLogging(bare)
	opts := bare["logging"].(map[string]any)["options"].(map[string]any)
	if opts["max-size"] != "8m" || opts["max-file"] != "3" {
		t.Fatalf("default logging = %v", bare["logging"])
	}
}
