package cmd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"nd/internal/edge"
	"nd/internal/ui"
)

const (
	edgeSystemdUnitPath       = "/etc/systemd/system/nd-edge.service"
	edgeDefaultBinaryPath     = "/usr/local/bin/nd"
	edgeMinDiskFreeBytes      = 1 << 30 // 1 GB
	edgeClockWarnSkew         = 5 * time.Minute
	edgeClockAbortSkew        = 1 * time.Hour
	legacyCatchAllStartMarker = "# === NextDeploy Edge Shield Catch-all"
	legacyCatchAllEndMarker   = "# === End NextDeploy Edge Shield Catch-all ==="
)

// edgeOps abstracts OS/systemd/caddy/network interactions for nd edge commands so
// install, doctor, sync, upgrade, and uninstall are deterministic and unit-testable.
type edgeOps struct {
	paths          edge.Paths
	unitPath       string
	binaryPath     string
	osReleasePath  string
	stdin          io.Reader
	stdout         io.Writer
	stderr         io.Writer
	caddy          edge.CaddyRunner
	systemd        edge.SystemdRunner
	httpClient     *http.Client
	now            func() time.Time
	getEUID        func() int
	goos           string
	goarch         string
	hasSystemd     func() bool
	diskFreeBytes  func(path string) (uint64, error)
	runCmd         func(ctx context.Context, name string, args ...string) (string, error)
	lookPath       func(file string) (string, error)
	chownGroup     func(path, group string, recursive bool) error
	getenforce     func() string
	signalReload   func(pid int) error
	hostname       func() (string, error)
	localPublicIPs func() ([]string, []string)
}

func defaultEdgeOps() *edgeOps {
	return &edgeOps{
		paths:         edge.DefaultPaths(),
		unitPath:      edgeSystemdUnitPath,
		binaryPath:    edgeDefaultBinaryPath,
		osReleasePath: "/etc/os-release",
		stdin:         os.Stdin,
		stdout:        os.Stdout,
		stderr:        os.Stderr,
		caddy:         &edge.ExecCaddyRunner{},
		systemd:       edge.ExecSystemdRunner{},
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		now:           func() time.Time { return time.Now().UTC() },
		getEUID:       edgeGetEUID,
		goos:          runtime.GOOS,
		goarch:        runtime.GOARCH,
		hasSystemd:    defaultHasSystemd,
		diskFreeBytes: edgeDiskFreeBytes,
		runCmd:        defaultRunCmd,
		lookPath:      exec.LookPath,
		chownGroup:    edgeChownGroup,
		getenforce:    defaultGetenforce,
		signalReload:  edgeSendSIGHUP,
		hostname:      os.Hostname,
		localPublicIPs: collectLocalPublicIPs,
	}
}

func defaultHasSystemd() bool {
	if st, err := os.Stat("/run/systemd/system"); err == nil && st.IsDir() {
		return true
	}
	return exec.Command("systemctl", "--version").Run() == nil
}

func defaultRunCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func defaultGetenforce() string {
	out, err := exec.Command("getenforce").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RunEdge dispatches all `nd edge <subcommand>` operations.
func RunEdge(args []string) error {
	return runEdgeWithOps(defaultEdgeOps(), args)
}

func runEdgeWithOps(ops *edgeOps, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printEdgeHelp(ops.stdout)
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "install":
		return runEdgeInstall(ops, rest)
	case "add-panel":
		return runEdgeAddPanel(ops, rest)
	case "remove-panel":
		return runEdgeRemovePanel(ops, rest)
	case "list", "ls":
		return runEdgeList(ops, rest)
	case "status":
		return runEdgeStatus(ops, rest)
	case "sync":
		return runEdgeSync(ops, rest)
	case "run":
		return runEdgeDaemon(ops, rest)
	case "doctor":
		return runEdgeDoctor(ops, rest)
	case "set-token":
		return runEdgeSetToken(ops, rest)
	case "set-url":
		return runEdgeSetURL(ops, rest)
	case "upgrade":
		return runEdgeUpgrade(ops, rest)
	case "uninstall":
		return runEdgeUninstall(ops, rest)
	default:
		return fmt.Errorf("unknown edge command %q. Run 'nd edge --help' for usage", sub)
	}
}

func printEdgeHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: nd edge <command> [args...]")
	fmt.Fprintln(w, "\nManage NextDeploy Multi-Panel Edge Shield on a Shield VPS:")
	fmt.Fprintln(w, "  install       --panel-url URL [--token TOKEN | --token-stdin] [flags]  Install Caddy + nd-edge daemon")
	fmt.Fprintln(w, "  add-panel     --panel-url URL [--token TOKEN | --token-stdin] [flags]  Register an additional panel")
	fmt.Fprintln(w, "  remove-panel  <panel_id|name> [--purge-certs]                          Unregister and remove one panel")
	fmt.Fprintln(w, "  list          [--json]                                                 List registered panels & sync state")
	fmt.Fprintln(w, "  status        [--json] [host]                                          Show per-host TLS & routing status")
	fmt.Fprintln(w, "  sync          [--panel ID] [--force]                                   Trigger immediate reconciliation")
	fmt.Fprintln(w, "  run                                                                    Run nd-edge daemon (systemd ExecStart)")
	fmt.Fprintln(w, "  doctor                                                                 Run Shield health & firewall diagnostics")
	fmt.Fprintln(w, "  set-token     <panel_id|name> [--token TOKEN | --token-stdin]          Update bearer token for a panel")
	fmt.Fprintln(w, "  set-url       <panel_id|name> --panel-url URL [flags]                  Update panel API URL")
	fmt.Fprintln(w, "  upgrade       [--url URL] [--sha256 HEX]                               Self-update nd binary safely")
	fmt.Fprintln(w, "  uninstall     [--all] [--purge-certs]                                  Remove nd-edge and restore Caddyfile")
	fmt.Fprintln(w, "\nToken input also honors ND_EDGE_TOKEN environment variable.")
}

// --- Shared Flag & Token Helpers ---

func resolveEdgeToken(tokenFlag string, tokenStdin bool, stdin io.Reader) (string, error) {
	if tokenFlag != "" && tokenStdin {
		return "", errors.New("cannot specify both --token and --token-stdin")
	}
	if tokenStdin {
		if stdin == nil {
			stdin = os.Stdin
		}
		raw, err := io.ReadAll(io.LimitReader(stdin, 16*1024))
		if err != nil {
			return "", fmt.Errorf("read token from stdin: %w", err)
		}
		tok := strings.TrimSpace(string(raw))
		if tok == "" {
			return "", errors.New("empty token received on stdin")
		}
		return tok, nil
	}
	if tok := strings.TrimSpace(tokenFlag); tok != "" {
		return tok, nil
	}
	if tok := strings.TrimSpace(os.Getenv("ND_EDGE_TOKEN")); tok != "" {
		return tok, nil
	}
	return "", errors.New("shield token required: use --token-stdin, --token, or ND_EDGE_TOKEN")
}

func validatePanelURL(rawURL string, insecureHTTP bool, warnOut io.Writer) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return "", errors.New("--panel-url is required")
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid --panel-url %q", rawURL)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return trimmed, nil
	case "http":
		if !insecureHTTP {
			return "", fmt.Errorf("panel URL %q uses plain HTTP; use https:// or pass --insecure-http", trimmed)
		}
		if warnOut != nil {
			fmt.Fprintf(warnOut, "WARNING: Using insecure http:// panel URL (%s); bearer token will not be encrypted in transit.\n", trimmed)
		}
		return trimmed, nil
	default:
		return "", fmt.Errorf("unsupported URL scheme %q in --panel-url (expected https)", u.Scheme)
	}
}

func normalizeCAPin(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) != 64 {
		return "", fmt.Errorf("invalid --panel-ca-sha256 %q: expected 64-char hex sha256", raw)
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", fmt.Errorf("invalid --panel-ca-sha256 %q: %w", raw, err)
	}
	return s, nil
}

func newEdgeUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func edgeConfigLockPath(paths edge.Paths) string {
	dir := filepath.Dir(paths.LockFile)
	if dir == "" || dir == "." {
		return paths.LockFile + ".cfg"
	}
	return filepath.Join(dir, "nd-edge-config.lock")
}

func edgeCaddyInstalledMarker(paths edge.Paths) string {
	return filepath.Join(filepath.Dir(paths.ClaimsFile), "caddy-installed-by-nd")
}

// --- 1. `nd edge install` (§4.1) ---

type edgeInstallOpts struct {
	panelURL      string
	token         string
	tokenStdin    bool
	acmeEmail     string
	adoptExisting bool
	panelCASHA256 string
	insecureHTTP  bool
	dnsGateOff    bool
	force         bool
}

func parseEdgeInstallArgs(args []string) (edgeInstallOpts, error) {
	var o edgeInstallOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--panel-url" && i+1 < len(args):
			o.panelURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--panel-url="):
			o.panelURL = strings.TrimPrefix(a, "--panel-url=")
		case a == "--token" && i+1 < len(args):
			o.token = args[i+1]
			i++
		case strings.HasPrefix(a, "--token="):
			o.token = strings.TrimPrefix(a, "--token=")
		case a == "--token-stdin":
			o.tokenStdin = true
		case a == "--acme-email" && i+1 < len(args):
			o.acmeEmail = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--acme-email="):
			o.acmeEmail = strings.TrimSpace(strings.TrimPrefix(a, "--acme-email="))
		case a == "--adopt-existing":
			o.adoptExisting = true
		case a == "--panel-ca-sha256" && i+1 < len(args):
			o.panelCASHA256 = args[i+1]
			i++
		case strings.HasPrefix(a, "--panel-ca-sha256="):
			o.panelCASHA256 = strings.TrimPrefix(a, "--panel-ca-sha256=")
		case a == "--insecure-http":
			o.insecureHTTP = true
		case a == "--force":
			o.force = true
		case a == "--dns-gate" && i+1 < len(args):
			off, err := parseOnOff(args[i+1])
			if err != nil {
				return o, fmt.Errorf("--dns-gate: %w", err)
			}
			o.dnsGateOff = off
			i++
		case strings.HasPrefix(a, "--dns-gate="):
			off, err := parseOnOff(strings.TrimPrefix(a, "--dns-gate="))
			if err != nil {
				return o, fmt.Errorf("--dns-gate: %w", err)
			}
			o.dnsGateOff = off
		default:
			return o, fmt.Errorf("unknown flag: %s", a)
		}
	}
	return o, nil
}

func parseOnOff(val string) (off bool, err error) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "on", "true", "1":
		return false, nil
	case "off", "false", "0":
		return true, nil
	default:
		return false, fmt.Errorf("expected 'on' or 'off', got %q", val)
	}
}

func runEdgeInstall(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge install --panel-url URL [--token TOKEN | --token-stdin] [--acme-email E] [--adopt-existing] [--panel-ca-sha256 S] [--insecure-http] [--dns-gate on|off] [--force]")
		return nil
	}

	opts, err := parseEdgeInstallArgs(args)
	if err != nil {
		return err
	}
	panelURL, err := validatePanelURL(opts.panelURL, opts.insecureHTTP, ops.stderr)
	if err != nil {
		return err
	}
	token, err := resolveEdgeToken(opts.token, opts.tokenStdin, ops.stdin)
	if err != nil {
		return err
	}
	caPin, err := normalizeCAPin(opts.panelCASHA256)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Step 1: Preconditions (root, systemd, OS, ports 80/443, clock skew, disk > 1GB).
	fmt.Fprintln(ops.stdout, "==> [1/10] Checking system preconditions...")
	if ops.getEUID() != 0 {
		return errors.New("nd edge install must be run as root")
	}
	if ops.goos != "linux" {
		return fmt.Errorf("unsupported OS %q: nd edge install only supports Linux", ops.goos)
	}
	osFamily, err := detectSupportedOSFamily(ops.osReleasePath)
	if err != nil {
		return err
	}
	if !ops.hasSystemd() {
		return errors.New("systemd is required on the Shield VPS")
	}
	if ssOut, ssErr := ops.runCmd(ctx, "ss", "-ltnp"); ssErr == nil {
		if port, proc, conflict := findPortConflictInSS(ssOut); conflict {
			return fmt.Errorf("port %s is already in use by %s; stop or remove %s before installing nd-edge", port, proc, proc)
		}
	}
	if skew, skewErr := checkPanelClockSkew(ctx, panelURL, caPin, ops.httpClient, ops.now()); skewErr == nil {
		if skew > edgeClockAbortSkew {
			return fmt.Errorf("system clock skew is %s (>1h); sync clock via NTP/chrony before installing", skew.Round(time.Second))
		}
		if skew > edgeClockWarnSkew {
			fmt.Fprintf(ops.stderr, "WARNING: system clock skew is %s (>5m); ACME/TLS may fail.\n", skew.Round(time.Second))
		}
	}
	if freeBytes, diskErr := ops.diskFreeBytes("/"); diskErr == nil && freeBytes < edgeMinDiskFreeBytes {
		return fmt.Errorf("insufficient disk space: %.1f MB free (require > 1 GB)", float64(freeBytes)/(1024*1024))
	}

	// Step 2: Install Caddy from official repo if missing.
	fmt.Fprintln(ops.stdout, "==> [2/10] Ensuring Caddy is installed...")
	caddyInstalledByND := false
	if _, err := ops.lookPath("caddy"); err != nil {
		if err := installCaddyPackage(ctx, ops, osFamily); err != nil {
			return err
		}
		caddyInstalledByND = true
	}
	caddyVer := detectCaddyVersionWithOps(ctx, ops)

	// Step 3 & 4: Inspect Caddyfile, handle admin addr / legacy catch-all / --adopt-existing.
	fmt.Fprintln(ops.stdout, "==> [3/10] Checking Caddy admin API and Caddyfile...")
	existingCaddyfileBytes, readErr := os.ReadFile(ops.paths.Caddyfile)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", ops.paths.Caddyfile, readErr)
	}
	plan, err := prepareInstallCaddyfile(string(existingCaddyfileBytes), ops.paths, opts.acmeEmail, opts.adoptExisting)
	if err != nil {
		return err
	}
	for _, w := range plan.Warnings {
		fmt.Fprintf(ops.stderr, "WARNING: %s\n", w)
	}

	// Step 5: Create directories, global.opts, common.caddy, placeholder generation, permissions & SELinux.
	fmt.Fprintln(ops.stdout, "==> [4/10] Preparing /etc/caddy/nd layout and permissions...")
	if err := ensureInstallLayout(ctx, ops, plan); err != nil {
		return err
	}

	// Validate candidate Caddyfile BEFORE replacing live Caddyfile (restore backup on failure).
	fmt.Fprintln(ops.stdout, "==> [5/10] Validating Caddyfile...")
	var backupPath string
	if plan.HasUserSites || plan.LegacyStripped || len(bytes.TrimSpace(existingCaddyfileBytes)) > 0 {
		if !bytes.Equal(bytes.TrimSpace(existingCaddyfileBytes), bytes.TrimSpace([]byte(plan.Content))) {
			backupPath = fmt.Sprintf("%s.nd-backup.%s", ops.paths.Caddyfile, ops.now().Format("20060102T150405Z"))
			if err := edge.WriteFileAtomic(backupPath, existingCaddyfileBytes, 0o644); err != nil {
				return fmt.Errorf("backup Caddyfile: %w", err)
			}
			fmt.Fprintf(ops.stdout, "  Backed up existing Caddyfile to %s\n", backupPath)
		}
	}

	valTmp := filepath.Join(filepath.Dir(ops.paths.Caddyfile), ".Caddyfile.nd-install.validate")
	if err := edge.WriteFileAtomic(valTmp, []byte(plan.Content), 0o644); err != nil {
		return err
	}
	if valOut, valErr := ops.caddy.Validate(ctx, valTmp); valErr != nil {
		_ = os.Remove(valTmp)
		if backupPath != "" {
			_ = edge.WriteFileAtomic(ops.paths.Caddyfile, existingCaddyfileBytes, 0o644)
		}
		return fmt.Errorf("caddy validate failed: %w\n%s", valErr, valOut)
	}
	_ = os.Remove(valTmp)

	if err := edge.WriteFileAtomic(ops.paths.Caddyfile, []byte(plan.Content), 0o644); err != nil {
		return err
	}
	if valOut, valErr := ops.caddy.Validate(ctx, ops.paths.Caddyfile); valErr != nil {
		if backupPath != "" {
			_ = edge.WriteFileAtomic(ops.paths.Caddyfile, existingCaddyfileBytes, 0o644)
		}
		return fmt.Errorf("caddy validate failed on %s: %w\n%s", ops.paths.Caddyfile, valErr, valOut)
	}

	caddyUser := "caddy"
	if ops.systemd != nil {
		if u := ops.systemd.CaddyUser(ctx); u != "" {
			caddyUser = u
		}
	}
	applyInstallPermsAndSELinux(ops, caddyUser)

	// Step 6: Firewall (ufw / firewalld) + cloud firewall reminder.
	fmt.Fprintln(ops.stdout, "==> [6/10] Configuring firewall rules...")
	configureShieldFirewall(ctx, ops)

	// Step 7: Write /etc/nd-edge/config.json (idempotent edge_id).
	fmt.Fprintln(ops.stdout, "==> [7/10] Writing /etc/nd-edge/config.json...")
	cfg, err := loadOrInitEdgeConfig(ops, plan, caddyUser, opts.dnsGateOff)
	if err != nil {
		return err
	}
	if caddyInstalledByND {
		_ = edge.WriteFileAtomic(edgeCaddyInstalledMarker(ops.paths), []byte("1\n"), 0o600)
	}

	// Step 8: Register panel & verify first /routes fetch (add-panel flow §4.2).
	fmt.Fprintln(ops.stdout, "==> [8/10] Registering with NextDeploy panel...")
	addedPanel, initialSnap, err := registerAndVerifyPanel(ctx, ops, cfg, addPanelParams{
		panelURL:      panelURL,
		token:         token,
		panelCASHA256: caPin,
		insecureHTTP:  opts.insecureHTTP,
		force:         opts.force,
		caddyVersion:  caddyVer,
	})
	if err != nil {
		return err
	}
	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		return err
	}
	if err := saveInitialPanelState(ops.paths, addedPanel, initialSnap, ops.now()); err != nil {
		return err
	}

	// Step 9: Install nd-edge.service.
	fmt.Fprintln(ops.stdout, "==> [9/10] Installing nd-edge systemd service...")
	if err := edge.WriteFileAtomic(ops.unitPath, []byte(renderSystemdUnit(ops.binaryPath)), 0o644); err != nil {
		return err
	}
	if out, err := ops.runCmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(out))
	}

	// Step 10: Enable Caddy, run synchronous initial reconcile, start nd-edge, print summary.
	fmt.Fprintln(ops.stdout, "==> [10/10] Starting services and running initial reconcile...")
	if out, err := ops.runCmd(ctx, "systemctl", "enable", "--now", "caddy"); err != nil {
		return fmt.Errorf("systemctl enable --now caddy: %w: %s", err, strings.TrimSpace(out))
	}

	// If nd-edge is already running (idempotent re-install), stop briefly or signal reload after reconcile.
	reconcileSummary := ""
	if summary, recErr := runSyncReconcileLocked(ctx, ops, cfg, false); recErr == nil {
		reconcileSummary = summary
	} else if !errors.Is(recErr, edge.ErrLockHeld) {
		fmt.Fprintf(ops.stderr, "WARNING: initial reconcile reported: %v\n", recErr)
	}

	if out, err := ops.runCmd(ctx, "systemctl", "enable", "--now", "nd-edge"); err != nil {
		return fmt.Errorf("systemctl enable --now nd-edge: %w: %s", err, strings.TrimSpace(out))
	}
	if reconcileSummary == "" {
		_, _ = ops.runCmd(ctx, "systemctl", "reload", "nd-edge")
	}

	applyInstallPermsAndSELinux(ops, caddyUser)
	ui.Success("NextDeploy Edge Shield installed (edge_id=%s, panel=%s)", cfg.EdgeID, addedPanel.PanelName)
	if reconcileSummary != "" {
		fmt.Fprintln(ops.stdout, reconcileSummary)
	}
	return nil
}

func detectSupportedOSFamily(osReleasePath string) (string, error) {
	raw, err := os.ReadFile(osReleasePath)
	if err != nil {
		return "", fmt.Errorf("cannot read %s to verify supported Linux distro: %w", osReleasePath, err)
	}
	var id, idLike string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID=") {
			id = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID="), `"'`))
		} else if strings.HasPrefix(line, "ID_LIKE=") {
			idLike = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), `"'`))
		}
	}
	combined := " " + id + " " + idLike + " "
	for _, deb := range []string{" debian ", " ubuntu "} {
		if strings.Contains(combined, deb) {
			return "debian", nil
		}
	}
	for _, rhel := range []string{" rhel ", " fedora ", " centos ", " rocky ", " almalinux ", " ol ", " amzn "} {
		if strings.Contains(combined, rhel) {
			return "rhel", nil
		}
	}
	return "", fmt.Errorf("unsupported Linux distribution %q (supported: Debian/Ubuntu, RHEL/Rocky/Alma/Fedora)", id)
}

func findPortConflictInSS(ssOutput string) (port string, process string, conflict bool) {
	for _, line := range strings.Split(ssOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// `ss -ltnp` puts Local Address:Port in field 3 (when State is field 0) or field 2.
		var localAddr string
		for _, f := range fields {
			if strings.HasSuffix(f, ":80") || strings.HasSuffix(f, ":443") {
				localAddr = f
				break
			}
		}
		if localAddr == "" {
			continue
		}
		p := "80"
		if strings.HasSuffix(localAddr, ":443") {
			p = "443"
		}
		proc := extractSSProcessName(line)
		if strings.EqualFold(proc, "caddy") {
			continue
		}
		if proc == "" {
			proc = "unknown process"
		}
		return p, proc, true
	}
	return "", "", false
}

func extractSSProcessName(line string) string {
	idx := strings.Index(line, `users:(("`)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(`users:(("`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func checkPanelClockSkew(ctx context.Context, panelURL, caPin string, baseClient *http.Client, localNow time.Time) (time.Duration, error) {
	c := baseClient
	if caPin != "" {
		pin := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(caPin), "sha256:"))
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Verified against explicit caPin below.
				MinVersion:         tls.VersionTLS12,
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					for _, der := range rawCerts {
						sum := sha256.Sum256(der)
						if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(pin)) == 1 {
							return nil
						}
					}
					return errors.New("pinned sha256 mismatch")
				},
			},
		}
		c = &http.Client{Timeout: 10 * time.Second, Transport: tr}
	} else if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, panelURL+"/", nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	dateHdr := strings.TrimSpace(resp.Header.Get("Date"))
	if dateHdr == "" {
		return 0, errors.New("missing Date header")
	}
	remoteTime, err := http.ParseTime(dateHdr)
	if err != nil {
		return 0, err
	}
	diff := localNow.Sub(remoteTime)
	if diff < 0 {
		diff = -diff
	}
	return diff, nil
}

func installCaddyPackage(ctx context.Context, ops *edgeOps, osFamily string) error {
	switch osFamily {
	case "debian":
		script := `set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl gnupg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
apt-get update -qq
apt-get install -y -qq caddy`
		if out, err := ops.runCmd(ctx, "bash", "-c", script); err != nil {
			return fmt.Errorf("install caddy on Debian/Ubuntu failed: %w\n%s", err, strings.TrimSpace(out))
		}
		return nil
	case "rhel":
		pm := "dnf"
		if _, err := ops.lookPath("dnf"); err != nil {
			pm = "yum"
		}
		script := fmt.Sprintf(`set -e
%s install -y 'dnf-command(copr)' || true
%s copr enable -y @caddy/caddy
%s install -y caddy`, pm, pm, pm)
		if out, err := ops.runCmd(ctx, "bash", "-c", script); err != nil {
			return fmt.Errorf("install caddy on RHEL/Fedora failed: %w\n%s", err, strings.TrimSpace(out))
		}
		return nil
	default:
		return fmt.Errorf("unsupported OS family %q", osFamily)
	}
}

func detectCaddyVersionWithOps(ctx context.Context, ops *edgeOps) string {
	out, err := ops.runCmd(ctx, "caddy", "version")
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// --- Caddyfile Parsing, Legacy Stripping, and Adoption ---

type caddyfileInstallPlan struct {
	Content            string
	AdminAddr          string
	ACMEEmail          string
	SkipTrustedProxies bool
	PreservedGlobal    string
	LegacyStripped     bool
	HasUserSites       bool
	Warnings           []string
}

func prepareInstallCaddyfile(raw string, paths edge.Paths, acmeEmailFlag string, adoptExisting bool) (caddyfileInstallPlan, error) {
	var plan caddyfileInstallPlan
	plan.AdminAddr = edge.DefaultAdminAddr
	plan.ACMEEmail = strings.TrimSpace(acmeEmailFlag)

	cleaned, legacyStripped := stripLegacyNextDeploy(raw)
	plan.LegacyStripped = legacyStripped

	outside, hadManaged := stripManagedRegion(cleaned)

	// Extract any top-level global options block `{ ... }` from `outside`.
	restWithoutGlobal, gInfo, hasGlobal := extractTopLevelGlobalBlock(outside)
	if hasGlobal {
		if gInfo.adminOff {
			return plan, errors.New("existing Caddyfile disables the admin API ('admin off'); nd-edge requires 'admin localhost:2019' (or an explicit admin address) to reload routes")
		}
		if gInfo.adminAddr != "" {
			plan.AdminAddr = gInfo.adminAddr
		}
		if plan.ACMEEmail == "" && gInfo.email != "" {
			plan.ACMEEmail = gInfo.email
		}
		if gInfo.hasTrustedProxies {
			plan.SkipTrustedProxies = true
			plan.Warnings = append(plan.Warnings, "existing servers/trusted_proxies block detected in Caddyfile; skipping nd-edge trusted_proxies in global.opts")
		}
		plan.PreservedGlobal = gInfo.remainingDirectives
	}

	trimmedRemaining := strings.TrimSpace(restWithoutGlobal)
	if hasNonCommentCaddyContent(trimmedRemaining) {
		plan.HasUserSites = true
		if !hadManaged && !adoptExisting {
			return plan, errors.New("existing user sites found in Caddyfile; re-run with --adopt-existing to back up and preserve them")
		}
	}

	region := edge.RenderCaddyfileManagedRegion(paths, plan.AdminAddr, plan.ACMEEmail, paths.PanelsLink, paths.UserCaddyDir)
	if trimmedRemaining != "" {
		plan.Content = region + "\n" + strings.TrimLeft(restWithoutGlobal, "\r\n")
		if !strings.HasSuffix(plan.Content, "\n") {
			plan.Content += "\n"
		}
	} else {
		plan.Content = region
	}

	return plan, nil
}

func stripLegacyNextDeploy(raw string) (string, bool) {
	stripped := false
	text := strings.ReplaceAll(raw, "\r\n", "\n")

	// 1. Strip `# === NextDeploy Edge Shield Catch-all` ... `# === End NextDeploy Edge Shield Catch-all ===`.
	for {
		start := strings.Index(text, legacyCatchAllStartMarker)
		if start < 0 {
			break
		}
		stripped = true
		end := strings.Index(text[start:], legacyCatchAllEndMarker)
		if end >= 0 {
			after := start + end + len(legacyCatchAllEndMarker)
			if after < len(text) && text[after] == '\n' {
				after++
			}
			text = text[:start] + text[after:]
		} else {
			text = text[:start]
		}
	}

	// 2. Strip legacy `on_demand_tls { ask ... }` blocks (especially referencing /api/v1/edge/verify-domain).
	lines := strings.Split(text, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if (trimmed == "# Global On-Demand TLS with NextDeploy verification" || trimmed == "# NextDeploy verification") &&
			i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "on_demand_tls") {
			stripped = true
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "on_demand_tls") && strings.Contains(trimmed, "{") {
			blockEnd := findClosingBraceLine(lines, i)
			blockBody := strings.Join(lines[i:blockEnd+1], "\n")
			if strings.Contains(blockBody, "/api/v1/edge/verify-domain") || stripped {
				stripped = true
				i = blockEnd + 1
				continue
			}
		}
		out = append(out, lines[i])
		i++
	}

	result := strings.Join(out, "\n")
	// 3. If a top-level `{ ... }` global options block is now empty (only comments/whitespace), remove it.
	if withoutGlobal, gInfo, hasGlobal := extractTopLevelGlobalBlock(result); hasGlobal {
		if strings.TrimSpace(gInfo.remainingDirectives) == "" && gInfo.adminAddr == "" && gInfo.email == "" && !gInfo.adminOff {
			result = strings.TrimLeft(withoutGlobal, "\n")
		}
	}
	return result, stripped
}

func stripManagedRegion(raw string) (string, bool) {
	start := strings.Index(raw, edge.ManagedStartMarker)
	end := strings.Index(raw, edge.ManagedEndMarker)
	if start >= 0 && end > start {
		after := end + len(edge.ManagedEndMarker)
		if after < len(raw) && raw[after] == '\n' {
			after++
		}
		return raw[:start] + raw[after:], true
	}
	return raw, false
}

type parsedGlobalOptions struct {
	adminAddr           string
	adminOff            bool
	email               string
	hasTrustedProxies   bool
	remainingDirectives string
}

func extractTopLevelGlobalBlock(raw string) (string, parsedGlobalOptions, bool) {
	var info parsedGlobalOptions
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	// Find first non-empty, non-comment line; in Caddyfile syntax a global options block
	// starts with `{` alone on its line.
	openIdx := -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if t == "{" {
			openIdx = i
		}
		break
	}
	if openIdx < 0 {
		return raw, info, false
	}

	closeIdx := findClosingBraceLine(lines, openIdx)
	if closeIdx <= openIdx {
		return raw, info, false
	}

	innerLines := lines[openIdx+1 : closeIdx]
	var rem []string
	for _, l := range innerLines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "trusted_proxies") {
			info.hasTrustedProxies = true
		}
		if t == "admin off" || strings.HasPrefix(t, "admin off ") || strings.HasPrefix(t, "admin off\t") {
			info.adminOff = true
			continue
		}
		if strings.HasPrefix(t, "admin ") || strings.HasPrefix(t, "admin\t") {
			fields := strings.Fields(t)
			if len(fields) >= 2 {
				info.adminAddr = fields[1]
			}
			continue
		}
		if strings.HasPrefix(t, "email ") || strings.HasPrefix(t, "email\t") {
			fields := strings.Fields(t)
			if len(fields) >= 2 {
				info.email = fields[1]
			}
			continue
		}
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		rem = append(rem, l)
	}
	info.remainingDirectives = strings.Join(rem, "\n")

	var before []string
	before = append(before, lines[:openIdx]...)
	before = append(before, lines[closeIdx+1:]...)
	return strings.Join(before, "\n"), info, true
}

func findClosingBraceLine(lines []string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth <= 0 && i > openIdx {
			return i
		}
	}
	return openIdx
}

func hasNonCommentCaddyContent(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			return true
		}
	}
	return false
}

func ensureInstallLayout(ctx context.Context, ops *edgeOps, plan caddyfileInstallPlan) error {
	for _, d := range []string{
		filepath.Dir(ops.paths.Caddyfile),
		ops.paths.CaddyNDDir,
		ops.paths.UserCaddyDir,
	} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	for _, d := range []string{
		filepath.Dir(ops.paths.ConfigFile),
		ops.paths.StateDir,
		filepath.Dir(ops.paths.ClaimsFile),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	userPlaceholder := filepath.Join(ops.paths.UserCaddyDir, edge.PlaceholderFileName)
	if _, err := os.Stat(userPlaceholder); errors.Is(err, os.ErrNotExist) {
		if err := edge.WriteFileAtomic(userPlaceholder, []byte(edge.RenderPlaceholderCaddy()), 0o640); err != nil {
			return err
		}
	}

	var globalOptsContent string
	if plan.SkipTrustedProxies {
		var b strings.Builder
		b.WriteString("# nd-edge skipped trusted_proxies (already defined in adopted Caddyfile)\n")
		if strings.TrimSpace(plan.PreservedGlobal) != "" {
			b.WriteString(strings.TrimSpace(plan.PreservedGlobal) + "\n")
		}
		globalOptsContent = b.String()
	} else {
		cfStore := edge.LoadOrInitCFIPStore(ops.paths.CFIPsFile)
		globalOptsContent = edge.RenderGlobalOpts(cfStore.Ranges())
		if strings.TrimSpace(plan.PreservedGlobal) != "" {
			globalOptsContent += "\n" + strings.TrimSpace(plan.PreservedGlobal) + "\n"
		}
	}
	if err := edge.WriteFileAtomic(ops.paths.GlobalOpts, []byte(globalOptsContent), 0o640); err != nil {
		return err
	}
	if err := edge.WriteFileAtomic(ops.paths.CommonCaddy, []byte(edge.RenderCommonCaddy()), 0o640); err != nil {
		return err
	}

	// Ensure panels & origins symlinks exist so initial Caddyfile validates cleanly.
	if _, err := edge.ReadDirLink(ops.paths.PanelsLink); err != nil {
		gen0Panels := ops.paths.PanelGenDir(0)
		if err := os.MkdirAll(gen0Panels, 0o750); err != nil {
			return err
		}
		if err := edge.WriteFileAtomic(filepath.Join(gen0Panels, edge.PlaceholderFileName), []byte(edge.RenderPlaceholderCaddy()), 0o640); err != nil {
			return err
		}
		if err := edge.SwapDirLinkAtomic(ops.paths.PanelsLink, gen0Panels); err != nil {
			return err
		}
	}
	if _, err := edge.ReadDirLink(ops.paths.OriginsLink); err != nil {
		gen0Origins := ops.paths.OriginGenDir(0)
		if err := os.MkdirAll(gen0Origins, 0o750); err != nil {
			return err
		}
		if err := edge.SwapDirLinkAtomic(ops.paths.OriginsLink, gen0Origins); err != nil {
			return err
		}
	}
	_ = ctx
	return nil
}

func applyInstallPermsAndSELinux(ops *edgeOps, caddyUser string) {
	if ops.chownGroup != nil {
		_ = ops.chownGroup(ops.paths.CaddyNDDir, caddyUser, true)
	}
	if ops.getenforce != nil && strings.EqualFold(ops.getenforce(), "Enforcing") {
		_, _ = ops.runCmd(context.Background(), "restorecon", "-R", filepath.Dir(ops.paths.Caddyfile))
	}
}

func configureShieldFirewall(ctx context.Context, ops *edgeOps) {
	if out, err := ops.runCmd(ctx, "ufw", "status"); err == nil && strings.Contains(out, "Status: active") {
		_, _ = ops.runCmd(ctx, "ufw", "allow", "80/tcp")
		_, _ = ops.runCmd(ctx, "ufw", "allow", "443/tcp")
		_, _ = ops.runCmd(ctx, "ufw", "allow", "443/udp")
		fmt.Fprintln(ops.stdout, "  Opened 80/tcp, 443/tcp, 443/udp in ufw.")
	} else if out, err := ops.runCmd(ctx, "firewall-cmd", "--state"); err == nil && strings.TrimSpace(out) == "running" {
		_, _ = ops.runCmd(ctx, "firewall-cmd", "--permanent", "--add-port=80/tcp", "--add-port=443/tcp", "--add-port=443/udp")
		_, _ = ops.runCmd(ctx, "firewall-cmd", "--reload")
		fmt.Fprintln(ops.stdout, "  Opened 80/tcp, 443/tcp, 443/udp in firewalld.")
	}
	fmt.Fprintln(ops.stdout, "  Reminder: ensure your cloud provider security group / firewall allows inbound 80/tcp, 443/tcp, and 443/udp.")
}

func loadOrInitEdgeConfig(ops *edgeOps, plan caddyfileInstallPlan, caddyUser string, dnsGateOff bool) (*edge.Config, error) {
	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		if !errors.Is(err, edge.ErrConfigNotFound) {
			return nil, err
		}
		edgeID, idErr := newEdgeUUIDv4()
		if idErr != nil {
			return nil, idErr
		}
		cfg = &edge.Config{
			EdgeID: edgeID,
		}
	}
	if plan.ACMEEmail != "" {
		cfg.ACMEEmail = plan.ACMEEmail
	}
	if plan.AdminAddr != "" {
		cfg.AdminAddr = plan.AdminAddr
	}
	cfg.DNSGateOff = dnsGateOff
	cfg.CaddyUser = caddyUser
	cfg.TrustedProxies = plan.SkipTrustedProxies
	if cfg.ShieldHostname == "" && ops.hostname != nil {
		if hn, hErr := ops.hostname(); hErr == nil {
			cfg.ShieldHostname = hn
		}
	}
	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func renderSystemdUnit(binaryPath string) string {
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = edgeDefaultBinaryPath
	}
	return fmt.Sprintf(`[Unit]
Description=NextDeploy edge sync
After=network-online.target caddy.service
Wants=network-online.target caddy.service

[Service]
ExecStart=%s edge run
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, binaryPath)
}

// --- 2. `nd edge add-panel` (§4.2) ---

type addPanelParams struct {
	panelURL      string
	token         string
	panelCASHA256 string
	insecureHTTP  bool
	force         bool
	caddyVersion  string
}

func runEdgeAddPanel(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge add-panel --panel-url URL [--token TOKEN | --token-stdin] [--force] [--panel-ca-sha256 S] [--insecure-http]")
		return nil
	}

	var (
		panelURL     string
		tokenFlag    string
		tokenStdin   bool
		caPinRaw     string
		insecureHTTP bool
		force        bool
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--panel-url" && i+1 < len(args):
			panelURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--panel-url="):
			panelURL = strings.TrimPrefix(a, "--panel-url=")
		case a == "--token" && i+1 < len(args):
			tokenFlag = args[i+1]
			i++
		case strings.HasPrefix(a, "--token="):
			tokenFlag = strings.TrimPrefix(a, "--token=")
		case a == "--token-stdin":
			tokenStdin = true
		case a == "--panel-ca-sha256" && i+1 < len(args):
			caPinRaw = args[i+1]
			i++
		case strings.HasPrefix(a, "--panel-ca-sha256="):
			caPinRaw = strings.TrimPrefix(a, "--panel-ca-sha256=")
		case a == "--insecure-http":
			insecureHTTP = true
		case a == "--force":
			force = true
		default:
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	cleanURL, err := validatePanelURL(panelURL, insecureHTTP, ops.stderr)
	if err != nil {
		return err
	}
	token, err := resolveEdgeToken(tokenFlag, tokenStdin, ops.stdin)
	if err != nil {
		return err
	}
	caPin, err := normalizeCAPin(caPinRaw)
	if err != nil {
		return err
	}

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return fmt.Errorf("load config (run 'nd edge install' first): %w", err)
	}

	panelCfg, snap, err := registerAndVerifyPanel(ctx, ops, cfg, addPanelParams{
		panelURL:      cleanURL,
		token:         token,
		panelCASHA256: caPin,
		insecureHTTP:  insecureHTTP,
		force:         force,
		caddyVersion:  detectCaddyVersionWithOps(ctx, ops),
	})
	if err != nil {
		return err
	}

	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		return err
	}
	if err := saveInitialPanelState(ops.paths, panelCfg, snap, ops.now()); err != nil {
		return err
	}

	signalDaemonReload(ctx, ops)
	ui.Success("Registered panel %q (%s, shield_node_id=%d, routes=%d)", panelCfg.PanelName, panelCfg.PanelID, panelCfg.ShieldNodeID, len(snap.Routes))
	return nil
}

func registerAndVerifyPanel(ctx context.Context, ops *edgeOps, cfg *edge.Config, params addPanelParams) (edge.PanelConfig, *edge.RoutesResponse, error) {
	tempPanel := edge.PanelConfig{
		URL:           params.panelURL,
		Token:         params.token,
		PanelCASHA256: params.panelCASHA256,
		InsecureHTTP:  params.insecureHTTP,
	}
	client := edge.NewPanelClient(tempPanel, edge.ParseIPStrings(cfg.ShieldIPs), nil)

	hn := cfg.ShieldHostname
	if hn == "" && ops.hostname != nil {
		hn, _ = ops.hostname()
	}
	var ipv4, ipv6 []string
	if ops.localPublicIPs != nil {
		ipv4, ipv6 = ops.localPublicIPs()
	}

	regResp, err := client.Register(ctx, edge.RegisterRequest{
		EdgeID:      cfg.EdgeID,
		EdgeVersion: Version,
		CaddyVer:    params.caddyVersion,
		Hostname:    hn,
		PublicIPv4:  ipv4,
		PublicIPv6:  ipv6,
		Force:       params.force,
	})
	if err != nil {
		return edge.PanelConfig{}, nil, fmt.Errorf("register with panel %s: %w", params.panelURL, err)
	}

	// §4.2: First /routes fetch MUST succeed before saving anything to config.json.
	snap, _, err := client.FetchRoutes(ctx, "")
	if err != nil {
		return edge.PanelConfig{}, nil, fmt.Errorf("initial /routes verification failed for panel %s: %w", params.panelURL, err)
	}

	now := ops.now()
	regAt := now
	if existing, _ := cfg.FindPanel(regResp.PanelID); existing != nil && !existing.RegisteredAt.IsZero() {
		regAt = existing.RegisteredAt
	}

	panelCfg := edge.PanelConfig{
		PanelID:         regResp.PanelID,
		PanelName:       regResp.PanelName,
		ShieldNodeID:    regResp.ShieldNodeID,
		URL:             params.panelURL,
		Token:           params.token,
		PollIntervalSec: regResp.PollIntervalSec,
		RegisteredAt:    regAt,
		PanelCASHA256:   params.panelCASHA256,
		InsecureHTTP:    params.insecureHTTP,
	}
	cfg.UpsertPanel(panelCfg)
	return panelCfg, snap, nil
}

func saveInitialPanelState(paths edge.Paths, panelCfg edge.PanelConfig, snap *edge.RoutesResponse, now time.Time) error {
	st := edge.NewPanelState(panelCfg.PanelID, panelCfg.PanelName)
	if snap != nil {
		st.LastGood = snap
		st.LastGoodETag = snap.ETag
		st.LastGoodAt = now
		st.LastSyncAttemptAt = now
		st.LastSyncOKAt = now
		st.SyncState = edge.SyncStateOK
	}
	return edge.SavePanelState(paths.StateFile(panelCfg.PanelID), st)
}

// --- 3. `nd edge remove-panel` (§4.3) ---

func runEdgeRemovePanel(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge remove-panel <panel_id|name> [--purge-certs]")
		return nil
	}

	var target string
	var purgeCerts bool
	for _, a := range args {
		if a == "--purge-certs" {
			purgeCerts = true
		} else if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		} else if target == "" {
			target = strings.TrimSpace(a)
		}
	}
	if target == "" {
		return errors.New("panel_id or name required: nd edge remove-panel <panel_id|name>")
	}

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}

	removed, ok := cfg.RemovePanel(target)
	if !ok {
		return fmt.Errorf("panel %q not found in config", target)
	}

	// Best-effort unregister on the panel.
	cl := edge.NewPanelClient(removed, nil, nil)
	_ = cl.Unregister(ctx, edge.UnregisterRequest{
		EdgeID: cfg.EdgeID,
		Reason: "panel_removed",
	})

	// Collect hosts for --purge-certs before deleting state file.
	var removedHosts []string
	var keepHosts map[string]struct{}
	if purgeCerts {
		removedHosts = collectPanelHosts(ops.paths, removed.PanelID)
		keepHosts = make(map[string]struct{})
		for _, p := range cfg.Panels {
			for _, h := range collectPanelHosts(ops.paths, p.PanelID) {
				keepHosts[h] = struct{}{}
			}
		}
	}

	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		return err
	}

	// Release claims & drop state file.
	panelsDir, _ := edge.ReadDirLink(ops.paths.PanelsLink)
	if claims, clErr := edge.LoadClaims(ops.paths.ClaimsFile, panelsDir, ops.now()); clErr == nil && claims != nil {
		claims.ReleasePanelClaims(removed.PanelID)
		_ = edge.SaveClaims(ops.paths.ClaimsFile, claims)
	}
	_ = os.Remove(ops.paths.StateFile(removed.PanelID))

	// If daemon is NOT holding the lock, reconcile synchronously to remove <panel_id>.caddy + .pem now.
	if _, recErr := runSyncReconcileLocked(ctx, ops, cfg, false); recErr != nil {
		if errors.Is(recErr, edge.ErrLockHeld) {
			signalDaemonReload(ctx, ops)
		}
	}

	if purgeCerts && len(removedHosts) > 0 {
		purgeUnusedHostCerts(ops.paths.CaddyCertsDir, removedHosts, keepHosts)
	}

	ui.Success("Removed panel %q (%s)", removed.PanelName, removed.PanelID)
	return nil
}

func collectPanelHosts(paths edge.Paths, panelID string) []string {
	set := make(map[string]struct{})
	if st, err := edge.LoadPanelState(paths.StateFile(panelID), panelID, "", time.Now().UTC()); err == nil && st != nil {
		for h := range st.HostStates {
			set[h] = struct{}{}
		}
		if st.LastGood != nil {
			for _, r := range st.LastGood.Routes {
				for _, h := range r.Hosts {
					if norm, verr := edge.NormalizeAndValidateHost(h, ""); verr == nil {
						set[norm] = struct{}{}
					}
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func purgeUnusedHostCerts(certsDir string, removedHosts []string, keepHosts map[string]struct{}) {
	if certsDir == "" {
		return
	}
	certBase := filepath.Join(certsDir, "certificates")
	entries, err := os.ReadDir(certBase)
	if err != nil {
		return
	}
	for _, host := range removedHosts {
		if _, keep := keepHosts[host]; keep {
			continue
		}
		for _, issuerDir := range entries {
			if !issuerDir.IsDir() {
				continue
			}
			_ = os.RemoveAll(filepath.Join(certBase, issuerDir.Name(), host))
		}
	}
}

// --- 4. `nd edge list` ---

func runEdgeList(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge list [--json]")
		return nil
	}
	jsonOut, _ := extractJSONFlag(args)

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}

	type panelListItem struct {
		PanelID      string `json:"panel_id"`
		PanelName    string `json:"panel_name"`
		ShieldNodeID int64  `json:"shield_node_id"`
		URL          string `json:"url"`
		SyncState    string `json:"sync_state"`
		HostCount    int    `json:"host_count"`
		Conflicts    int    `json:"conflicts"`
		LastSyncOKAt string `json:"last_sync_ok_at,omitempty"`
		LastError    string `json:"last_error,omitempty"`
	}

	items := make([]panelListItem, 0, len(cfg.Panels))
	for _, p := range cfg.Panels {
		st, _ := edge.LoadPanelState(ops.paths.StateFile(p.PanelID), p.PanelID, p.PanelName, ops.now())
		item := panelListItem{
			PanelID:      p.PanelID,
			PanelName:    p.PanelName,
			ShieldNodeID: p.ShieldNodeID,
			URL:          p.URL,
			SyncState:    edge.SyncStatePending,
		}
		if st != nil {
			item.SyncState = st.SyncState
			item.HostCount = len(st.HostStates)
			item.Conflicts = len(st.Conflicts)
			item.LastError = st.LastError
			if !st.LastSyncOKAt.IsZero() {
				item.LastSyncOKAt = st.LastSyncOKAt.Format(time.RFC3339)
			}
		}
		items = append(items, item)
	}

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]interface{}{
			"edge_id": cfg.EdgeID,
			"panels":  items,
		}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}

	if len(items) == 0 {
		fmt.Fprintln(ops.stdout, "No panels registered. Add one with: nd edge add-panel --panel-url <url> --token-stdin")
		return nil
	}

	fmt.Fprintf(ops.stdout, "%-36s %-16s %-12s %-7s %-10s %s\n", "PANEL ID", "NAME", "SYNC STATE", "HOSTS", "CONFLICTS", "URL")
	fmt.Fprintln(ops.stdout, strings.Repeat("─", 104))
	for _, it := range items {
		fmt.Fprintf(ops.stdout, "%-36s %-16s %-12s %-7d %-10d %s\n",
			it.PanelID, it.PanelName, it.SyncState, it.HostCount, it.Conflicts, it.URL)
	}
	return nil
}

// --- 5. `nd edge status` ---

func runEdgeStatus(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge status [--json] [host]")
		return nil
	}
	jsonOut, cleanArgs := extractJSONFlag(args)
	hostFilter := ""
	if len(cleanArgs) > 0 {
		hostFilter = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(cleanArgs[0])), ".")
	}

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	meta, _ := edge.LoadApplyMeta(ops.paths.ApplyMeta)
	if meta == nil {
		meta = &edge.ApplyMeta{}
	}

	caddyActive := false
	caddyDetail := ""
	if ops.systemd != nil {
		caddyActive, caddyDetail, _ = ops.systemd.IsActive(context.Background(), "caddy.service")
	}

	type hostRow struct {
		PanelID      string `json:"panel_id"`
		PanelName    string `json:"panel_name"`
		Host         string `json:"host"`
		State        string `json:"state"`
		Detail       string `json:"detail,omitempty"`
		CertNotAfter string `json:"cert_not_after,omitempty"`
		Issuer       string `json:"issuer,omitempty"`
	}

	var rows []hostRow
	for _, p := range cfg.Panels {
		st, _ := edge.LoadPanelState(ops.paths.StateFile(p.PanelID), p.PanelID, p.PanelName, ops.now())
		if st == nil {
			continue
		}
		for _, hs := range st.HostStates {
			if hostFilter != "" && !strings.EqualFold(hs.Host, hostFilter) {
				continue
			}
			exp := ""
			if hs.CertNotAfter != nil {
				exp = hs.CertNotAfter.UTC().Format(time.RFC3339)
			}
			rows = append(rows, hostRow{
				PanelID:      p.PanelID,
				PanelName:    p.PanelName,
				Host:         hs.Host,
				State:        hs.State,
				Detail:       hs.Detail,
				CertNotAfter: exp,
				Issuer:       hs.Issuer,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Host != rows[j].Host {
			return rows[i].Host < rows[j].Host
		}
		return rows[i].PanelID < rows[j].PanelID
	})

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]interface{}{
			"edge_id":          cfg.EdgeID,
			"caddy_running":    caddyActive,
			"caddy_status":     caddyDetail,
			"current_gen":      meta.CurrentGen,
			"last_apply_at":    meta.LastApplyAt,
			"last_apply_error": meta.LastApplyError,
			"hosts":            rows,
		}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}

	fmt.Fprintf(ops.stdout, "Edge ID: %s | Panels: %d | Gen: %d | Caddy running: %v\n",
		cfg.EdgeID, len(cfg.Panels), meta.CurrentGen, caddyActive)
	if meta.LastApplyError != "" {
		fmt.Fprintf(ops.stdout, "Last Apply Error: %s\n", meta.LastApplyError)
	}
	if len(rows) == 0 {
		fmt.Fprintln(ops.stdout, "No hosts matched.")
		return nil
	}

	fmt.Fprintf(ops.stdout, "\n%-32s %-16s %-18s %-22s %s\n", "HOST", "PANEL", "STATE", "CERT EXPIRY", "DETAIL")
	fmt.Fprintln(ops.stdout, strings.Repeat("─", 104))
	for _, r := range rows {
		exp := r.CertNotAfter
		if exp == "" {
			exp = "-"
		}
		fmt.Fprintf(ops.stdout, "%-32s %-16s %-18s %-22s %s\n", r.Host, r.PanelName, r.State, exp, r.Detail)
	}
	return nil
}

// --- 6. `nd edge sync` ---

func runEdgeSync(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge sync [--panel ID] [--force]")
		return nil
	}
	var panelFilter string
	var force bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--panel" && i+1 < len(args) {
			panelFilter = strings.TrimSpace(args[i+1])
			i++
		} else if strings.HasPrefix(a, "--panel=") {
			panelFilter = strings.TrimSpace(strings.TrimPrefix(a, "--panel="))
		} else if a == "--force" {
			force = true
		} else {
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	if panelFilter != "" {
		if p, _ := cfg.FindPanel(panelFilter); p == nil {
			return fmt.Errorf("panel %q not found in config", panelFilter)
		}
	}

	ctx := context.Background()
	lock, lockErr := edge.AcquireLock(ops.paths.LockFile)
	if errors.Is(lockErr, edge.ErrLockHeld) {
		if !force && signalDaemonReload(ctx, ops) {
			ui.Success("Signaled running nd-edge daemon (SIGHUP) for immediate reconciliation")
			return nil
		}
		if force {
			return errors.New("nd-edge daemon is currently running; stop it briefly ('systemctl stop nd-edge') or wait for next poll to run forced reconcile, or omit --force to trigger SIGHUP")
		}
		return errors.New("nd-edge daemon holds lock but could not signal reload")
	}
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = lock.Release() }()

	// Daemon is not running: poll requested panel(s) and reconcile synchronously.
	now := ops.now()
	panelStates := make(map[string]*edge.PanelState, len(cfg.Panels))
	for _, p := range cfg.Panels {
		st, _ := edge.LoadPanelState(ops.paths.StateFile(p.PanelID), p.PanelID, p.PanelName, now)
		if panelFilter == "" || p.PanelID == panelFilter || p.PanelName == panelFilter {
			cl := edge.NewPanelClient(p, edge.ParseIPStrings(cfg.ShieldIPs), nil)
			out := edge.PollPanelOnce(ctx, cl, st, cfg.ShieldHostname, force, now)
			if out.Err != nil {
				fmt.Fprintf(ops.stderr, "WARNING: panel %s poll failed: %v\n", p.PanelName, out.Err)
			}
			_ = edge.SavePanelState(ops.paths.StateFile(p.PanelID), st)
		}
		panelStates[p.PanelID] = st
	}

	summary, err := reconcileWithLoadedStates(ctx, ops, cfg, panelStates, force, now)
	if err != nil {
		return err
	}
	ui.Success("Sync complete: %s", summary)
	return nil
}

func runSyncReconcileLocked(ctx context.Context, ops *edgeOps, cfg *edge.Config, force bool) (string, error) {
	lock, err := edge.AcquireLock(ops.paths.LockFile)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Release() }()

	now := ops.now()
	panelStates := make(map[string]*edge.PanelState, len(cfg.Panels))
	for _, p := range cfg.Panels {
		st, _ := edge.LoadPanelState(ops.paths.StateFile(p.PanelID), p.PanelID, p.PanelName, now)
		panelStates[p.PanelID] = st
	}
	return reconcileWithLoadedStates(ctx, ops, cfg, panelStates, force, now)
}

func reconcileWithLoadedStates(ctx context.Context, ops *edgeOps, cfg *edge.Config, panelStates map[string]*edge.PanelState, force bool, now time.Time) (string, error) {
	panelsDir, _ := edge.ReadDirLink(ops.paths.PanelsLink)
	claims, _ := edge.LoadClaims(ops.paths.ClaimsFile, panelsDir, now)
	meta, _ := edge.LoadApplyMeta(ops.paths.ApplyMeta)
	cfIPs := edge.LoadOrInitCFIPStore(ops.paths.CFIPsFile)
	dnsGate := edge.NewDNSGate(edge.DNSGateOptions{
		CFIPs:     cfIPs,
		CachePath: ops.paths.DNSCacheFile,
		ACMEEmail: cfg.ACMEEmail,
		Off:       cfg.DNSGateOff,
	})
	applier := edge.NewApplier(ops.paths, ops.caddy)
	probe := edge.NewCertProbe(ops.paths.CaddyCertsDir, ops.systemd)
	rec := edge.NewReconciler(ops.paths, dnsGate, applier, probe, ops.systemd, cfIPs)

	out, err := rec.ReconcileOnce(ctx, cfg, panelStates, claims, meta, force, false, now)
	if err != nil {
		return "", err
	}

	var rendered, pendingDNS, conflicts int
	for _, p := range cfg.Panels {
		st := panelStates[p.PanelID]
		if st == nil {
			continue
		}
		conflicts += len(st.Conflicts)
		for _, hs := range st.HostStates {
			switch hs.State {
			case edge.StateActive, edge.StateIssuing, edge.StateDNSDrift:
				rendered++
			case edge.StatePendingDNS, edge.StatePendingCAA:
				pendingDNS++
			}
		}
	}
	return fmt.Sprintf("gen=%d applied=%v hosts_rendered=%d pending_dns=%d conflicts=%d",
		out.Generation, out.Applied, rendered, pendingDNS, conflicts), nil
}

func signalDaemonReload(ctx context.Context, ops *edgeOps) bool {
	if _, err := ops.runCmd(ctx, "systemctl", "reload", "nd-edge"); err == nil {
		return true
	}
	if raw, err := os.ReadFile(ops.paths.PIDFile); err == nil {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 0 {
			if ops.signalReload != nil && ops.signalReload(pid) == nil {
				return true
			}
		}
	}
	return false
}

// --- 7. `nd edge run` ---

func runEdgeDaemon(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge run")
		return nil
	}
	caddyVer := detectCaddyVersionWithOps(context.Background(), ops)
	d, lock, err := edge.NewDaemon(edge.DaemonOptions{
		Paths:        ops.paths,
		EdgeVersion:  Version,
		CaddyVersion: caddyVer,
		Caddy:        ops.caddy,
		Systemd:      ops.systemd,
		HTTPClient:   ops.httpClient,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Release()
		_ = os.Remove(ops.paths.PIDFile)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reloadCh, stopCh, cleanupSignals := edgeDaemonSignalNotify()
	defer cleanupSignals()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-reloadCh:
				if err := d.ReloadConfig(); err != nil {
					fmt.Fprintf(ops.stderr, "[nd-edge] SIGHUP ReloadConfig failed: %v\n", err)
				}
			case <-stopCh:
				cancel()
				return
			}
		}
	}()

	return d.Run(ctx)
}

// --- 8. `nd edge doctor` ---

type doctorResult struct {
	Name   string
	Status string // PASS, WARN, FAIL, INFO
	Detail string
}

func runEdgeDoctor(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge doctor")
		return nil
	}

	ctx := context.Background()
	var results []doctorResult
	add := func(name, status, detail string) {
		results = append(results, doctorResult{Name: name, Status: status, Detail: detail})
	}

	// 1. Root & OS & systemd
	if ops.getEUID() == 0 {
		add("root", "PASS", "running as root")
	} else {
		add("root", "WARN", "not running as root (some checks/repairs require root)")
	}
	if ops.hasSystemd() {
		add("systemd", "PASS", "systemd active")
	} else {
		add("systemd", "FAIL", "systemd not detected")
	}

	// 2. Config, state, claims validity
	cfg, cfgErr := edge.LoadConfig(ops.paths.ConfigFile)
	if cfgErr != nil {
		add("config.json", "FAIL", cfgErr.Error())
	} else {
		add("config.json", "PASS", fmt.Sprintf("valid (edge_id=%s, panels=%d)", cfg.EdgeID, len(cfg.Panels)))
	}

	if _, metaErr := edge.LoadApplyMeta(ops.paths.ApplyMeta); metaErr != nil {
		add("apply-meta.json", "WARN", metaErr.Error())
	} else {
		add("apply-meta.json", "PASS", "valid")
	}

	panelsDir, _ := edge.ReadDirLink(ops.paths.PanelsLink)
	if _, clErr := edge.LoadClaims(ops.paths.ClaimsFile, panelsDir, ops.now()); clErr != nil {
		add("claims.json", "WARN", clErr.Error())
	} else {
		add("claims.json", "PASS", "valid")
	}

	if cfg != nil {
		stateIssues := 0
		for _, p := range cfg.Panels {
			if _, stErr := edge.LoadPanelState(ops.paths.StateFile(p.PanelID), p.PanelID, p.PanelName, ops.now()); stErr != nil {
				stateIssues++
				add("state:"+p.PanelID, "WARN", stErr.Error())
			}
		}
		if stateIssues == 0 {
			add("panel-states", "PASS", fmt.Sprintf("%d panel state file(s) valid", len(cfg.Panels)))
		}
	}

	// 3. Caddy binary, service, admin endpoint, Caddyfile directives
	if caddyVer := detectCaddyVersionWithOps(ctx, ops); caddyVer != "" {
		add("caddy-binary", "PASS", caddyVer)
	} else {
		add("caddy-binary", "FAIL", "caddy binary not found or failed to run")
	}

	if ops.systemd != nil {
		if active, detail, _ := ops.systemd.IsActive(ctx, "caddy.service"); active {
			add("caddy.service", "PASS", "active")
		} else {
			add("caddy.service", "FAIL", detail)
		}
		if active, detail, _ := ops.systemd.IsActive(ctx, "nd-edge.service"); active {
			add("nd-edge.service", "PASS", "active")
		} else {
			add("nd-edge.service", "WARN", detail)
		}
	}

	adminAddr := edge.DefaultAdminAddr
	if cfg != nil && cfg.AdminAddr != "" {
		adminAddr = cfg.AdminAddr
	}
	if ops.caddy != nil {
		if err := ops.caddy.CheckAdmin(ctx, adminAddr); err != nil {
			add("caddy-admin", "FAIL", fmt.Sprintf("%s unreachable: %v", adminAddr, err))
		} else {
			add("caddy-admin", "PASS", fmt.Sprintf("%s reachable", adminAddr))
		}
	}

	// Directive check + caddy validate
	if rawCF, err := os.ReadFile(ops.paths.Caddyfile); err != nil {
		add("Caddyfile", "FAIL", err.Error())
	} else {
		cfText := string(rawCF)
		if strings.Contains(cfText, legacyCatchAllStartMarker) || strings.Contains(cfText, "on_demand_tls") {
			add("caddy-directives", "FAIL", "legacy catch-all or on_demand_tls directives still present")
		} else if !strings.Contains(cfText, edge.ManagedStartMarker) || !strings.Contains(cfText, edge.ManagedEndMarker) {
			add("caddy-directives", "FAIL", "nd-edge managed markers missing from Caddyfile")
		} else if ops.caddy != nil {
			if valOut, valErr := ops.caddy.Validate(ctx, ops.paths.Caddyfile); valErr != nil {
				add("caddy-directives", "FAIL", fmt.Sprintf("caddy validate failed: %v %s", valErr, valOut))
			} else {
				add("caddy-directives", "PASS", "managed markers present and caddy validate passed")
			}
		} else {
			add("caddy-directives", "PASS", "managed markers present")
		}
	}

	// 4. Clock skew vs registered panels
	if cfg != nil && len(cfg.Panels) > 0 {
		checkedSkew := false
		for _, p := range cfg.Panels {
			if skew, err := checkPanelClockSkew(ctx, p.URL, p.PanelCASHA256, ops.httpClient, ops.now()); err == nil {
				checkedSkew = true
				if skew > edgeClockAbortSkew {
					add("clock-skew", "FAIL", fmt.Sprintf("%s vs %s (>1h)", skew.Round(time.Second), p.PanelName))
				} else if skew > edgeClockWarnSkew {
					add("clock-skew", "WARN", fmt.Sprintf("%s vs %s (>5m)", skew.Round(time.Second), p.PanelName))
				} else {
					add("clock-skew", "PASS", fmt.Sprintf("%s vs %s", skew.Round(time.Second), p.PanelName))
				}
				break
			}
		}
		if !checkedSkew {
			add("clock-skew", "WARN", "could not reach any panel to measure clock skew")
		}
	}

	// 5. Disk free
	if freeBytes, err := ops.diskFreeBytes("/"); err == nil {
		gb := float64(freeBytes) / (1024 * 1024 * 1024)
		if freeBytes < edgeMinDiskFreeBytes {
			add("disk-free", "FAIL", fmt.Sprintf("%.2f GB free (<1 GB)", gb))
		} else {
			add("disk-free", "PASS", fmt.Sprintf("%.2f GB free", gb))
		}
	}

	// 6. Firewall / UDP 443 check
	fwDetail := "no local firewall active; verify cloud firewall allows 80/tcp, 443/tcp, 443/udp"
	fwStatus := "INFO"
	if out, err := ops.runCmd(ctx, "ufw", "status"); err == nil && strings.Contains(out, "Status: active") {
		has80 := strings.Contains(out, "80/tcp") || strings.Contains(out, "80 ")
		has443TCP := strings.Contains(out, "443/tcp") || strings.Contains(out, "443 ")
		has443UDP := strings.Contains(out, "443/udp")
		if has80 && has443TCP && has443UDP {
			fwStatus, fwDetail = "PASS", "ufw allows 80/tcp, 443/tcp, 443/udp"
		} else {
			fwStatus, fwDetail = "WARN", "ufw active but missing one of 80/tcp, 443/tcp, 443/udp (HTTP/3 needs 443/udp)"
		}
	} else if out, err := ops.runCmd(ctx, "firewall-cmd", "--list-ports"); err == nil {
		if strings.Contains(out, "80/tcp") && strings.Contains(out, "443/tcp") && strings.Contains(out, "443/udp") {
			fwStatus, fwDetail = "PASS", "firewalld allows 80/tcp, 443/tcp, 443/udp"
		} else {
			fwStatus, fwDetail = "WARN", "firewalld missing one of 80/tcp, 443/tcp, 443/udp"
		}
	}
	add("firewall-udp443", fwStatus, fwDetail)

	// 7. External port 80 hint (E-ACME-9)
	add("external-port80", "INFO", "Verify external TCP port 80/443 reachability from your panel ('nd shield test <id>') for HTTP-01 ACME issuance")

	failures := 0
	for _, r := range results {
		if r.Status == "FAIL" {
			failures++
		}
		fmt.Fprintf(ops.stdout, "[%-4s] %-18s %s\n", r.Status, r.Name, r.Detail)
	}
	if failures > 0 {
		return fmt.Errorf("doctor found %d failing check(s)", failures)
	}
	return nil
}

// --- 9. `nd edge set-token` & `nd edge set-url` ---

func runEdgeSetToken(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge set-token <panel_id|name> [--token TOKEN | --token-stdin]")
		return nil
	}
	var target, tokenFlag string
	var tokenStdin bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--token" && i+1 < len(args):
			tokenFlag = args[i+1]
			i++
		case strings.HasPrefix(a, "--token="):
			tokenFlag = strings.TrimPrefix(a, "--token=")
		case a == "--token-stdin":
			tokenStdin = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag: %s", a)
		default:
			if target == "" {
				target = strings.TrimSpace(a)
			}
		}
	}
	if target == "" {
		return errors.New("panel_id or name required: nd edge set-token <panel_id|name> --token-stdin")
	}
	newToken, err := resolveEdgeToken(tokenFlag, tokenStdin, ops.stdin)
	if err != nil {
		return err
	}

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	p, idx := cfg.FindPanel(target)
	if p == nil || idx < 0 {
		return fmt.Errorf("panel %q not found in config", target)
	}

	updated := *p
	updated.Token = newToken
	cl := edge.NewPanelClient(updated, edge.ParseIPStrings(cfg.ShieldIPs), nil)
	if _, _, err := cl.FetchRoutes(ctx, ""); err != nil {
		return fmt.Errorf("new token verification failed: %w", err)
	}

	cfg.Panels[idx] = updated
	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		return err
	}
	signalDaemonReload(ctx, ops)
	ui.Success("Updated token for panel %q (%s)", updated.PanelName, updated.PanelID)
	return nil
}

func runEdgeSetURL(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge set-url <panel_id|name> --panel-url URL [--panel-ca-sha256 S] [--insecure-http]")
		return nil
	}
	var target, panelURL, caPinRaw string
	var insecureHTTP, insecureSet, caSet bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--panel-url" && i+1 < len(args):
			panelURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--panel-url="):
			panelURL = strings.TrimPrefix(a, "--panel-url=")
		case a == "--panel-ca-sha256" && i+1 < len(args):
			caPinRaw = args[i+1]
			caSet = true
			i++
		case strings.HasPrefix(a, "--panel-ca-sha256="):
			caPinRaw = strings.TrimPrefix(a, "--panel-ca-sha256=")
			caSet = true
		case a == "--insecure-http":
			insecureHTTP = true
			insecureSet = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag: %s", a)
		default:
			if target == "" {
				target = strings.TrimSpace(a)
			}
		}
	}
	if target == "" {
		return errors.New("panel_id or name required: nd edge set-url <panel_id|name> --panel-url URL")
	}

	cleanURL, err := validatePanelURL(panelURL, insecureHTTP, ops.stderr)
	if err != nil {
		return err
	}
	caPin, err := normalizeCAPin(caPinRaw)
	if err != nil {
		return err
	}

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	p, idx := cfg.FindPanel(target)
	if p == nil || idx < 0 {
		return fmt.Errorf("panel %q not found in config", target)
	}

	updated := *p
	updated.URL = cleanURL
	if insecureSet {
		updated.InsecureHTTP = insecureHTTP
	} else if strings.HasPrefix(cleanURL, "https://") {
		updated.InsecureHTTP = false
	}
	if caSet {
		updated.PanelCASHA256 = caPin
	}

	cl := edge.NewPanelClient(updated, edge.ParseIPStrings(cfg.ShieldIPs), nil)
	if _, _, err := cl.FetchRoutes(ctx, ""); err != nil {
		return fmt.Errorf("new panel URL verification failed: %w", err)
	}

	cfg.Panels[idx] = updated
	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		return err
	}
	signalDaemonReload(ctx, ops)
	ui.Success("Updated URL for panel %q (%s) -> %s", updated.PanelName, updated.PanelID, updated.URL)
	return nil
}

// --- 10. `nd edge upgrade` (§4.5) ---

func runEdgeUpgrade(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge upgrade [--url URL] [--sha256 HEX]")
		return nil
	}
	var downloadURL, expectedSHA string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--url" && i+1 < len(args):
			downloadURL = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--url="):
			downloadURL = strings.TrimSpace(strings.TrimPrefix(a, "--url="))
		case a == "--sha256" && i+1 < len(args):
			expectedSHA = strings.ToLower(strings.TrimSpace(args[i+1]))
			i++
		case strings.HasPrefix(a, "--sha256="):
			expectedSHA = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(a, "--sha256=")))
		default:
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	httpClient := ops.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	ctx := context.Background()
	if downloadURL == "" {
		var releaseSHA string
		var err error
		downloadURL, releaseSHA, err = resolveLatestNDDownload(ctx, httpClient, ops.goos, ops.goarch)
		if err != nil {
			return err
		}
		if expectedSHA == "" {
			expectedSHA = releaseSHA
		}
	}

	targetPath := ops.binaryPath
	if targetPath == "" {
		if exe, err := os.Executable(); err == nil {
			targetPath = exe
		}
	}
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "nd-edge/"+Version)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned HTTP %d", downloadURL, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(dir, ".nd-upgrade-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp binary: %w", err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		_ = tmp.Close()
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA != "" {
		if subtle.ConstantTimeCompare([]byte(actualSHA), []byte(expectedSHA)) != 1 {
			return fmt.Errorf("sha256 mismatch: got %s, want %s (keeping existing binary)", actualSHA, expectedSHA)
		}
	}

	if ops.goos != "windows" {
		_ = os.Chmod(tmpPath, 0o755)
	}

	// Smoke test downloaded binary before replacing active executable (§4.5).
	if out, err := ops.runCmd(ctx, tmpPath, "version"); err != nil {
		return fmt.Errorf("smoke test ('%s version') failed: %w\n%s", tmpPath, err, strings.TrimSpace(out))
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("atomic rename %s -> %s: %w", tmpPath, targetPath, err)
	}
	cleanupTmp = false

	_, _ = ops.runCmd(ctx, "systemctl", "restart", "nd-edge")
	ui.Success("Upgraded nd binary at %s (sha256=%s)", targetPath, actualSHA)
	return nil
}

func resolveLatestNDDownload(ctx context.Context, httpClient *http.Client, goos, goarch string) (downloadURL, expectedSHA string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/masudranaxpert/NextDeploy/releases/latest", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "nd-edge/"+Version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github release lookup returned HTTP %d", resp.StatusCode)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}

	targetName := fmt.Sprintf("nd-%s-%s", goos, goarch)
	if goos == "windows" {
		targetName += ".exe"
	}
	var checksumURL string
	for _, asset := range rel.Assets {
		if asset.Name == targetName {
			downloadURL = asset.BrowserDownloadURL
		}
		lower := strings.ToLower(asset.Name)
		if lower == "sha256sums" || lower == "sha256sums.txt" || lower == "checksums.txt" {
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if downloadURL == "" {
		return "", "", fmt.Errorf("no release asset found for %s (%s)", targetName, rel.TagName)
	}

	if checksumURL != "" {
		if cReq, cErr := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil); cErr == nil {
			if cResp, dErr := httpClient.Do(cReq); dErr == nil {
				defer cResp.Body.Close()
				if cResp.StatusCode == http.StatusOK {
					sc := bufio.NewScanner(io.LimitReader(cResp.Body, 256*1024))
					for sc.Scan() {
						fields := strings.Fields(sc.Text())
						if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == targetName {
							expectedSHA = strings.ToLower(fields[0])
							break
						}
					}
				}
			}
		}
	}
	return downloadURL, expectedSHA, nil
}

// --- 11. `nd edge uninstall` (§4.4) ---

func runEdgeUninstall(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		fmt.Fprintln(ops.stdout, "Usage: nd edge uninstall [--all] [--purge-certs]")
		return nil
	}
	var all, purgeCerts bool
	for _, a := range args {
		switch a {
		case "--all":
			all = true
		case "--purge-certs":
			purgeCerts = true
		default:
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	ctx := context.Background()
	cfg, cfgErr := edge.LoadConfig(ops.paths.ConfigFile)
	if cfgErr == nil && cfg != nil {
		if len(cfg.Panels) > 0 && !all {
			return fmt.Errorf("cannot uninstall while %d panel(s) remain registered; run 'nd edge remove-panel' first or pass --all", len(cfg.Panels))
		}
		for _, p := range cfg.Panels {
			cl := edge.NewPanelClient(p, nil, nil)
			_ = cl.Unregister(ctx, edge.UnregisterRequest{
				EdgeID: cfg.EdgeID,
				Reason: "uninstall",
			})
		}
	}

	// Stop & disable nd-edge.service.
	_, _ = ops.runCmd(ctx, "systemctl", "disable", "--now", "nd-edge")
	_ = os.Remove(ops.unitPath)
	_, _ = ops.runCmd(ctx, "systemctl", "daemon-reload")

	// Restore backup Caddyfile if adopted, otherwise strip nd-edge managed region.
	caddyInstalledByND := false
	if _, err := os.Stat(edgeCaddyInstalledMarker(ops.paths)); err == nil {
		caddyInstalledByND = true
	}

	restoredBackup := findLatestCaddyfileBackup(ops.paths.Caddyfile)
	if restoredBackup != "" {
		if b, err := os.ReadFile(restoredBackup); err == nil {
			if err := edge.WriteFileAtomic(ops.paths.Caddyfile, b, 0o644); err != nil {
				return err
			}
			_ = os.Remove(restoredBackup)
			if _, valErr := ops.caddy.Validate(ctx, ops.paths.Caddyfile); valErr == nil {
				_, _ = ops.runCmd(ctx, "systemctl", "reload", "caddy")
			}
		}
	} else if raw, err := os.ReadFile(ops.paths.Caddyfile); err == nil {
		outside, _ := stripManagedRegion(string(raw))
		if hasNonCommentCaddyContent(strings.TrimSpace(outside)) {
			if err := edge.WriteFileAtomic(ops.paths.Caddyfile, []byte(strings.TrimLeft(outside, "\r\n")), 0o644); err != nil {
				return err
			}
			if _, valErr := ops.caddy.Validate(ctx, ops.paths.Caddyfile); valErr == nil {
				_, _ = ops.runCmd(ctx, "systemctl", "reload", "caddy")
			}
		} else {
			_ = os.Remove(ops.paths.Caddyfile)
			if caddyInstalledByND {
				_, _ = ops.runCmd(ctx, "systemctl", "disable", "--now", "caddy")
			}
		}
	}

	if purgeCerts {
		_ = os.RemoveAll(ops.paths.CaddyCertsDir)
	}

	_ = os.RemoveAll(ops.paths.CaddyNDDir)
	_ = os.RemoveAll(filepath.Dir(ops.paths.ConfigFile))
	_ = os.RemoveAll(filepath.Dir(ops.paths.ClaimsFile))
	_ = os.Remove(ops.paths.LockFile)
	_ = os.Remove(ops.paths.PIDFile)
	_ = os.Remove(edgeConfigLockPath(ops.paths))

	ui.Success("Uninstalled nd-edge from Shield VPS")
	return nil
}

func findLatestCaddyfileBackup(caddyfilePath string) string {
	dir := filepath.Dir(caddyfilePath)
	prefix := filepath.Base(caddyfilePath) + ".nd-backup."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var matches []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			matches = append(matches, filepath.Join(dir, e.Name()))
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

func collectLocalPublicIPs() (ipv4 []string, ipv6 []string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, nil
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(ipNet.IP)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
			continue
		}
		if addr.Is4() {
			ipv4 = append(ipv4, addr.String())
		} else if addr.Is6() {
			ipv6 = append(ipv6, addr.String())
		}
	}
	sort.Strings(ipv4)
	sort.Strings(ipv6)
	return ipv4, ipv6
}
