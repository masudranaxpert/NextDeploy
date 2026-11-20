package cmd

import "testing"

func TestParseAIFlags(t *testing.T) {
	v, wait, err := parseAIEnableFlags([]string{"--version", "v19.0.0", "--no-wait"})
	if err != nil || v != "v19.0.0" || wait {
		t.Fatalf("enable flags: %q %v %v", v, wait, err)
	}
	if _, _, err := parseAIEnableFlags([]string{"--bogus"}); err == nil {
		t.Fatal("unknown enable flag must fail")
	}
	purge, yes, err := parseAIDisableFlags([]string{"--purge", "-y"})
	if err != nil || !purge || !yes {
		t.Fatalf("disable flags: %v %v %v", purge, yes, err)
	}
}

func TestAIDisableNeedsYes(t *testing.T) {
	if err := runAIDisable(nil, false, false, false); err == nil {
		t.Fatal("disable without --yes must refuse before calling the panel")
	}
}
