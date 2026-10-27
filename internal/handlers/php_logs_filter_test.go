package handlers

import (
	"strings"
	"testing"

	"panel/internal/db"
)

func TestFilterPHPRuntimeLogs(t *testing.T) {
	target := db.PHPSite{
		AppID:   "emwbd-63b7",
		OwnerID: 2,
		UID:     20000,
	}

	siblings := []db.PHPSite{
		{AppID: "emwbd-63b7", OwnerID: 2, UID: 20000},
		{AppID: "emwbdofficialhomepage-cd16", OwnerID: 2, UID: 20002},
		{AppID: "mazabd-8373", OwnerID: 2, UID: 20003},
	}

	sampleLogs := `[27-Oct-2026 15:41:50] NOTICE: fpm is running, pid 1
[27-Oct-2026 15:41:50] NOTICE: ready to handle connections
[27-Oct-2026 15:41:50] NOTICE: Reloading in progress ...
[27-Oct-2026 15:41:50] NOTICE: reloading: execvp("php-fpm", {"php-fpm", "-F", "-y", "/usr/local/etc/php-fpm.conf"})
[27-Oct-2026 15:41:50] NOTICE: using inherited socket fd=8, "/run/php/emwbd-63b7.sock"
[27-Oct-2026 15:41:50] NOTICE: using inherited socket fd=9, "/run/php/emwbdofficialhomepage-cd16.sock"
[27-Oct-2026 15:41:50] NOTICE: using inherited socket fd=10, "/run/php/mazabd-8373.sock"
[27-Oct-2026 15:41:50] NOTICE: Access to the script '/data/workspaces/mazabd-8373/htdocs/2026/10/27/hello-world' has been denied (see security.limit_extensions)
[27-Oct-2026 12:50:02] NOTICE: [pool site-20000] child 487 exited with code 0 after 576.284084 seconds from start
[27-Oct-2026 12:50:02] NOTICE: [pool site-20000] child 493 started
[27-Oct-2026 12:50:11] NOTICE: [pool site-20003] child 500 exited with code 0 after 570.251451 seconds from start
[27-Oct-2026 12:50:11] NOTICE: [pool site-20003] child 501 started
[27-Oct-2026 15:59:06] NOTICE: ready to handle connections`

	out := filterPHPRuntimeLogs(sampleLogs, target, siblings, 100)

	// Verify that mazabd-8373 and emwbdofficialhomepage-cd16 are completely stripped out
	if strings.Contains(out, "mazabd") {
		t.Fatalf("expected output not to contain 'mazabd', got:\n%s", out)
	}
	if strings.Contains(out, "emwbdofficialhomepage") {
		t.Fatalf("expected output not to contain 'emwbdofficialhomepage', got:\n%s", out)
	}
	if strings.Contains(out, "site-20003") {
		t.Fatalf("expected output not to contain 'site-20003', got:\n%s", out)
	}

	// Verify that target site logs and daemon logs are retained
	if !strings.Contains(out, "/run/php/emwbd-63b7.sock") {
		t.Fatalf("expected output to contain target socket, got:\n%s", out)
	}
	if !strings.Contains(out, "[pool site-20000]") {
		t.Fatalf("expected output to contain target pool, got:\n%s", out)
	}
	if !strings.Contains(out, "NOTICE: fpm is running") {
		t.Fatalf("expected output to contain daemon line 'NOTICE: fpm is running', got:\n%s", out)
	}
}
