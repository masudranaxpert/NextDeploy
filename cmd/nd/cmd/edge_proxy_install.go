package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nd/internal/edge"
	"nd/internal/ui"
)

type osReleaseInfo struct {
	ID              string
	IDLike          string
	VersionID       string
	VersionCodename string
	Family          string // debian | rhel | alpine
}

type angieRepoPlan struct {
	OSFamily     string
	KeyURL       string
	KeyDestPath  string
	RepoFilePath string
	RepoContent  string
	InstallCmd   string
}

func validateProxyInstallChoice(proxyName string, experimental bool, adoptExisting bool) (string, error) {
	eng := strings.ToLower(strings.TrimSpace(proxyName))
	if eng == "" {
		eng = edge.EngineCaddy
	}
	switch eng {
	case edge.EngineCaddy:
		return edge.EngineCaddy, nil
	case edge.EngineAngie:
		if adoptExisting {
			return "", errors.New("--adopt-existing is only supported with --proxy caddy")
		}
		return edge.EngineAngie, nil
	case edge.EngineFerron:
		if !experimental {
			return "", errors.New("--proxy ferron is experimental; pass --experimental to opt in")
		}
		if adoptExisting {
			return "", errors.New("--adopt-existing is only supported with --proxy caddy")
		}
		return edge.EngineFerron, nil
	default:
		return "", fmt.Errorf("unsupported --proxy %q (expected caddy, angie, or ferron)", proxyName)
	}
}

func edgeAngieInstalledMarker(paths edge.Paths) string {
	return filepath.Join(filepath.Dir(paths.ConfigFile), "angie-installed-by-nd")
}

func edgeFerronInstalledMarker(paths edge.Paths) string {
	return filepath.Join(filepath.Dir(paths.ConfigFile), "ferron-installed-by-nd")
}

func edgeInstalledMarkerForEngine(paths edge.Paths, engineName string) string {
	switch engineName {
	case edge.EngineAngie:
		return edgeAngieInstalledMarker(paths)
	case edge.EngineFerron:
		return edgeFerronInstalledMarker(paths)
	default:
		return edgeCaddyInstalledMarker(paths)
	}
}

func ferronAdminTokenPath(paths edge.Paths) string {
	return filepath.Join(filepath.Dir(paths.ConfigFile), "ferron-admin-token")
}

func parseOSReleaseInfo(osReleasePath string) (osReleaseInfo, error) {
	raw, err := os.ReadFile(osReleasePath)
	if err != nil {
		return osReleaseInfo{}, fmt.Errorf("cannot read %s to verify supported Linux distro: %w", osReleasePath, err)
	}
	var info osReleaseInfo
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ID="):
			info.ID = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID="), `"'`))
		case strings.HasPrefix(line, "ID_LIKE="):
			info.IDLike = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), `"'`))
		case strings.HasPrefix(line, "VERSION_ID="):
			info.VersionID = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"'`)
		case strings.HasPrefix(line, "VERSION_CODENAME="):
			info.VersionCodename = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "VERSION_CODENAME="), `"'`))
		}
	}
	if info.ID == "alpine" {
		info.Family = "alpine"
		return info, nil
	}
	combined := " " + info.ID + " " + info.IDLike + " "
	for _, deb := range []string{" debian ", " ubuntu "} {
		if strings.Contains(combined, deb) {
			info.Family = "debian"
			return info, nil
		}
	}
	for _, rhel := range []string{" rhel ", " fedora ", " centos ", " rocky ", " almalinux ", " ol ", " amzn "} {
		if strings.Contains(combined, rhel) {
			info.Family = "rhel"
			return info, nil
		}
	}
	return info, fmt.Errorf("unsupported Linux distribution %q (supported: Debian/Ubuntu, RHEL/Rocky/Alma/Fedora, Alpine)", info.ID)
}

func buildAngieRepoPlan(info osReleaseInfo) (angieRepoPlan, error) {
	switch info.Family {
	case "debian":
		distroID := info.ID
		if distroID != "debian" && distroID != "ubuntu" {
			if strings.Contains(" "+info.IDLike+" ", " ubuntu ") {
				distroID = "ubuntu"
			} else {
				distroID = "debian"
			}
		}
		ver := info.VersionID
		codename := info.VersionCodename
		if ver == "" || codename == "" {
			// Fallback to runtime os-release shell expansion if fields were omitted.
			ver = "${VERSION_ID}"
			codename = "${VERSION_CODENAME}"
		}
		repoLine := fmt.Sprintf("deb [signed-by=/etc/apt/keyrings/angie-signing.gpg] https://download.angie.software/angie/%s/%s %s main", distroID, ver, codename)
		script := fmt.Sprintf(`set -e
export DEBIAN_FRONTEND=noninteractive
. /etc/os-release
apt-get update -qq
apt-get install -y -qq ca-certificates curl
install -d -m 0755 /etc/apt/keyrings
curl -fsSL 'https://angie.software/keys/angie-signing.gpg' -o /etc/apt/keyrings/angie-signing.gpg
chmod 0644 /etc/apt/keyrings/angie-signing.gpg
printf '%%s\n' %q > /etc/apt/sources.list.d/angie.list
apt-get update -qq
apt-get install -y -qq angie`, repoLine)
		return angieRepoPlan{
			OSFamily:     "debian",
			KeyURL:       "https://angie.software/keys/angie-signing.gpg",
			KeyDestPath:  "/etc/apt/keyrings/angie-signing.gpg",
			RepoFilePath: "/etc/apt/sources.list.d/angie.list",
			RepoContent:  repoLine + "\n",
			InstallCmd:   script,
		}, nil

	case "alpine":
		minorVer := alpineMajorMinor(info.VersionID)
		repoURL := fmt.Sprintf("https://download.angie.software/angie/alpine/v%s/main", minorVer)
		script := fmt.Sprintf(`set -e
apk update
apk add --no-cache curl ca-certificates
curl -fsSL 'https://angie.software/keys/angie-signing.rsa' -o /etc/apk/keys/angie-signing.rsa
grep -qxF %q /etc/apk/repositories || printf '%%s\n' %q >> /etc/apk/repositories
apk update
apk add --no-cache angie`, repoURL, repoURL)
		return angieRepoPlan{
			OSFamily:     "alpine",
			KeyURL:       "https://angie.software/keys/angie-signing.rsa",
			KeyDestPath:  "/etc/apk/keys/angie-signing.rsa",
			RepoFilePath: "/etc/apk/repositories",
			RepoContent:  repoURL + "\n",
			InstallCmd:   script,
		}, nil

	case "rhel":
		slug := "almalinux"
		switch info.ID {
		case "rocky":
			slug = "rocky"
		case "fedora":
			slug = "fedora"
		}
		repoContent := fmt.Sprintf(`[angie]
name=Angie repo
baseurl=https://download.angie.software/angie/%s/$releasever/
gpgcheck=1
enabled=1
gpgkey=https://angie.software/keys/angie-signing.gpg.asc
`, slug)
		script := fmt.Sprintf(`set -e
cat <<'EOF' > /etc/yum.repos.d/angie.repo
%sEOF
if command -v dnf >/dev/null 2>&1; then
  dnf install -y angie
else
  yum install -y angie
fi`, repoContent)
		return angieRepoPlan{
			OSFamily:     "rhel",
			KeyURL:       "https://angie.software/keys/angie-signing.gpg.asc",
			RepoFilePath: "/etc/yum.repos.d/angie.repo",
			RepoContent:  repoContent,
			InstallCmd:   script,
		}, nil

	default:
		return angieRepoPlan{}, fmt.Errorf("unsupported OS family %q for Angie", info.Family)
	}
}

func alpineMajorMinor(ver string) string {
	parts := strings.Split(strings.TrimSpace(ver), ".")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		return parts[0] + "." + parts[1]
	}
	return "3.21"
}

func installAngiePackage(ctx context.Context, ops *edgeOps, info osReleaseInfo) error {
	plan, err := buildAngieRepoPlan(info)
	if err != nil {
		return err
	}
	if out, err := ops.runCmd(ctx, "bash", "-c", plan.InstallCmd); err != nil {
		return fmt.Errorf("install angie on %s failed: %w\n%s", info.ID, err, strings.TrimSpace(out))
	}
	return nil
}

type ferronInstallPlan struct {
	OSFamily     string
	KeyURL       string
	RepoFilePath string
	InstallCmd   string
}

func buildFerronInstallPlan(info osReleaseInfo) (ferronInstallPlan, error) {
	switch info.Family {
	case "debian":
		codename := info.VersionCodename
		if codename == "" {
			codename = "$(lsb_release -cs)"
		}
		script := fmt.Sprintf(`set -e
export DEBIAN_FRONTEND=noninteractive
. /etc/os-release
apt-get update -qq
apt-get install -y -qq ca-certificates curl gnupg2 lsb-release debian-archive-keyring
install -d -m 0755 /usr/share/keyrings
curl -fsSL 'https://deb.ferron.sh/signing.pgp' | gpg --dearmor --yes -o /usr/share/keyrings/ferron-keyring.gpg
chmod 0644 /usr/share/keyrings/ferron-keyring.gpg
printf 'deb [signed-by=/usr/share/keyrings/ferron-keyring.gpg] https://deb.ferron.sh %s main\n' > /etc/apt/sources.list.d/ferron.list
apt-get update -qq
apt-get install -y -qq ferron3`, codename)
		return ferronInstallPlan{
			OSFamily:     "debian",
			KeyURL:       "https://deb.ferron.sh/signing.pgp",
			RepoFilePath: "/etc/apt/sources.list.d/ferron.list",
			InstallCmd:   script,
		}, nil

	case "rhel":
		script := `set -e
curl -fsSL 'https://rpm.ferron.sh/ferron.repo' -o /etc/yum.repos.d/ferron.repo
if command -v dnf >/dev/null 2>&1; then
  dnf install -y ferron3
else
  yum install -y ferron3
fi`
		return ferronInstallPlan{
			OSFamily:     "rhel",
			RepoFilePath: "/etc/yum.repos.d/ferron.repo",
			InstallCmd:   script,
		}, nil

	case "alpine":
		script := `set -e
export FERRON_INSTALL_METHOD=archive
curl -fsSL 'https://get.ferron.sh/v3' | bash`
		return ferronInstallPlan{
			OSFamily:   "alpine",
			InstallCmd: script,
		}, nil

	default:
		return ferronInstallPlan{}, fmt.Errorf("unsupported OS family %q for Ferron", info.Family)
	}
}

func installFerronPackage(ctx context.Context, ops *edgeOps, info osReleaseInfo) error {
	plan, err := buildFerronInstallPlan(info)
	if err != nil {
		return err
	}
	if out, err := ops.runCmd(ctx, "bash", "-c", plan.InstallCmd); err != nil {
		return fmt.Errorf("install ferron on %s failed: %w\n%s", info.ID, err, strings.TrimSpace(out))
	}
	return nil
}

func ensureFerronAdminToken(ops *edgeOps) (string, error) {
	tokenPath := ferronAdminTokenPath(ops.paths)
	if raw, err := os.ReadFile(tokenPath); err == nil {
		if tok := strings.TrimSpace(string(raw)); tok != "" {
			return tok, nil
		}
	}
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate ferron admin token: %w", err)
	}
	tok := hex.EncodeToString(buf[:])
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return "", err
	}
	if err := edge.WriteFileAtomic(tokenPath, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

func ferronRootConfigPath(ep edge.EnginePaths) string {
	return filepath.Join(filepath.Dir(ep.ManagedDir), "ferron.conf")
}

func renderFerronServiceUnit(ferronBin string, mainConfig string) string {
	if strings.TrimSpace(ferronBin) == "" {
		ferronBin = "/usr/sbin/ferron"
	}
	return fmt.Sprintf(`[Unit]
Description=Ferron web server (managed by nd-edge)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run -c %s --config-adapter ferronconf
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`, ferronBin, filepath.ToSlash(mainConfig))
}

func renderFerronDropin(ferronBin, mainConfig, adminToken string) string {
	if strings.TrimSpace(ferronBin) == "" {
		ferronBin = "/usr/sbin/ferron"
	}
	return fmt.Sprintf(`# Managed by nd edge (ferron). DO NOT EDIT.
[Service]
Environment="ND_FERRON_ADMIN_TOKEN=%s"
ExecStart=
ExecStart=%s run -c %s --config-adapter ferronconf
ExecReload=
ExecReload=/bin/kill -HUP $MAINPID
`, adminToken, ferronBin, filepath.ToSlash(mainConfig))
}

func ensureFerronSystemIntegration(ops *edgeOps, ep edge.EnginePaths) error {
	tok, err := ensureFerronAdminToken(ops)
	if err != nil {
		return err
	}

	rootConf := ferronRootConfigPath(ep)
	if err := os.MkdirAll(filepath.Dir(rootConf), 0o750); err != nil {
		return err
	}
	rootBody := fmt.Sprintf("# Managed by nd edge (ferron). DO NOT EDIT.\ninclude %q\n", filepath.ToSlash(ep.MainConfig))
	if err := edge.WriteFileAtomic(rootConf, []byte(rootBody), 0o644); err != nil {
		return err
	}

	ferronBin := "/usr/sbin/ferron"
	if ops.lookPath != nil {
		if resolved, lErr := ops.lookPath(ep.Binary); lErr == nil && strings.TrimSpace(resolved) != "" {
			ferronBin = resolved
		}
	}

	unitDir := filepath.Dir(ops.unitPath)
	baseUnit := filepath.Join(unitDir, "ferron.service")
	hasDistroUnit := false
	for _, sysUnit := range []string{"/lib/systemd/system/ferron.service", "/usr/lib/systemd/system/ferron.service"} {
		if _, sErr := os.Stat(sysUnit); sErr == nil {
			hasDistroUnit = true
			break
		}
	}
	if !hasDistroUnit {
		if _, sErr := os.Stat(baseUnit); errors.Is(sErr, os.ErrNotExist) {
			if err := edge.WriteFileAtomic(baseUnit, []byte(renderFerronServiceUnit(ferronBin, ep.MainConfig)), 0o644); err != nil {
				return err
			}
		}
	}

	dropinPath := filepath.Join(unitDir, "ferron.service.d", "nd-edge.conf")
	if err := os.MkdirAll(filepath.Dir(dropinPath), 0o755); err != nil {
		return err
	}
	return edge.WriteFileAtomic(dropinPath, []byte(renderFerronDropin(ferronBin, ep.MainConfig, tok)), 0o600)
}

func renderSystemdUnitForEngine(binaryPath, serviceUnit string) string {
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = edgeDefaultBinaryPath
	}
	if strings.TrimSpace(serviceUnit) == "" {
		serviceUnit = "caddy.service"
	}
	return fmt.Sprintf(`[Unit]
Description=NextDeploy edge sync
After=network-online.target %s
Wants=network-online.target %s

[Service]
ExecStart=%s edge run
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, serviceUnit, serviceUnit, binaryPath)
}

func runnerForEngine(ops *edgeOps, engineName string, ep edge.EnginePaths) edge.Runner {
	switch engineName {
	case edge.EngineAngie:
		if ops.angie != nil {
			return ops.angie
		}
		return &edge.AngieRunner{
			Binary:      ep.Binary,
			MainConfig:  ep.MainConfig,
			ServiceUnit: ep.ServiceUnit,
			PIDFile:     edge.DefaultAngiePIDFile,
		}
	case edge.EngineFerron:
		if ops.ferron != nil {
			return ops.ferron
		}
		return &edge.FerronRunner{
			Binary:         ep.Binary,
			MainConfig:     ep.MainConfig,
			ServiceUnit:    ep.ServiceUnit,
			AdminURL:       edge.DefaultFerronAdminURL,
			AdminTokenFile: ferronAdminTokenPath(ops.paths),
			PIDFile:        edge.DefaultFerronPIDFile,
		}
	default:
		return edge.RunnerFromCaddyRunner(ops.caddy, ep.MainConfig, "")
	}
}

func buildEdgeEngineWithOps(ops *edgeOps, engineName string, cfg *edge.Config) (edge.Engine, error) {
	switch strings.ToLower(strings.TrimSpace(engineName)) {
	case "", edge.EngineCaddy:
		return edge.NewCaddyEngineWithConfig(ops.paths, cfg, ops.caddy), nil
	case edge.EngineAngie:
		ep := ops.paths.EnginePathsFor(edge.EngineAngie)
		return edge.NewAngieEngineWithConfig(ep, cfg, runnerForEngine(ops, edge.EngineAngie, ep)), nil
	case edge.EngineFerron:
		ep := ops.paths.EnginePathsFor(edge.EngineFerron)
		return edge.NewFerronEngineWithConfig(ep, cfg, runnerForEngine(ops, edge.EngineFerron, ep)), nil
	default:
		return nil, fmt.Errorf("unknown proxy engine %q", engineName)
	}
}

func applyEnginePermsAndSELinux(ops *edgeOps, ep edge.EnginePaths) {
	if ops.chownGroup != nil && ep.ServiceUser != "" {
		if ep.ManagedDir != "" {
			_ = ops.chownGroup(ep.ManagedDir, ep.ServiceUser, true)
		}
		if ep.CertsDir != "" {
			_ = ops.chownGroup(ep.CertsDir, ep.ServiceUser, true)
		}
	}
	if ops.getenforce != nil && strings.EqualFold(ops.getenforce(), "Enforcing") {
		if ep.MainConfig != "" {
			_, _ = ops.runCmd(context.Background(), "restorecon", "-R", filepath.Dir(ep.MainConfig))
		}
		if ep.CertsDir != "" {
			_, _ = ops.runCmd(context.Background(), "restorecon", "-R", ep.CertsDir)
		}
	}
}

func findPortConflictForEngine(ssOutput, targetEngine string) (port string, process string, conflict bool) {
	for _, line := range strings.Split(ssOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
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
		if strings.EqualFold(proc, targetEngine) {
			continue
		}
		if proc == "" {
			proc = "unknown process"
		}
		return p, proc, true
	}
	return "", "", false
}

func runEdgeInstallAngie(ctx context.Context, ops *edgeOps, opts edgeInstallOpts, panelURL, token, caPin string) error {
	return runEdgeInstallForEngine(ctx, ops, edge.EngineAngie, opts, panelURL, token, caPin)
}

func runEdgeInstallForEngine(ctx context.Context, ops *edgeOps, engineName string, opts edgeInstallOpts, panelURL, token, caPin string) error {
	fmt.Fprintln(ops.stdout, "==> [1/10] Checking system preconditions...")
	if ops.getEUID() != 0 {
		return errors.New("nd edge install must be run as root")
	}
	if ops.goos != "linux" {
		return fmt.Errorf("unsupported OS %q: nd edge install only supports Linux", ops.goos)
	}
	osInfo, err := parseOSReleaseInfo(ops.osReleasePath)
	if err != nil {
		return err
	}
	if !ops.hasSystemd() {
		return errors.New("systemd is required on the Shield VPS")
	}

	var conflictingSvc string
	if ssOut, ssErr := ops.runCmd(ctx, "ss", "-ltnp"); ssErr == nil {
		if port, proc, conflict := findPortConflictForEngine(ssOut, engineName); conflict {
			switch strings.ToLower(proc) {
			case edge.EngineCaddy, edge.EngineAngie, edge.EngineFerron:
				conflictingSvc = strings.ToLower(proc)
			default:
				return fmt.Errorf("port %s is already in use by %s; stop or remove %s before installing nd-edge", port, proc, proc)
			}
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

	ep := ops.paths.EnginePathsFor(engineName)
	fmt.Fprintf(ops.stdout, "==> [2/10] Ensuring %s is installed...\n", engineName)
	installedByND := false
	if _, err := ops.lookPath(ep.Binary); err != nil {
		switch engineName {
		case edge.EngineAngie:
			err = installAngiePackage(ctx, ops, osInfo)
		case edge.EngineFerron:
			err = installFerronPackage(ctx, ops, osInfo)
		default:
			err = fmt.Errorf("unsupported proxy engine %q", engineName)
		}
		if err != nil {
			return err
		}
		installedByND = true
	}

	tmpCfg := &edge.Config{
		ProxyEngine: engineName,
		ACMEEmail:   strings.TrimSpace(opts.acmeEmail),
		DNSGateOff:  opts.dnsGateOff,
	}
	eng, err := buildEdgeEngineWithOps(ops, engineName, tmpCfg)
	if err != nil {
		return err
	}
	proxyVer, _ := eng.Runner().Version(ctx)

	fmt.Fprintf(ops.stdout, "==> [3/10] Preparing %s layout and permissions...\n", ep.ManagedDir)
	if err := ensureEngineInstallLayout(ops, eng, tmpCfg); err != nil {
		return err
	}
	if engineName == edge.EngineFerron {
		if err := ensureFerronSystemIntegration(ops, ep); err != nil {
			return err
		}
	}

	fmt.Fprintf(ops.stdout, "==> [4/10] Validating %s configuration...\n", engineName)
	if valOut, valErr := eng.Runner().Validate(ctx, ep.MainConfig); valErr != nil {
		validateLabel := engineName + " validate"
		if engineName == edge.EngineAngie {
			validateLabel = "angie -t"
		}
		return fmt.Errorf("%s failed: %w\n%s", validateLabel, valErr, valOut)
	}
	applyEnginePermsAndSELinux(ops, ep)

	fmt.Fprintln(ops.stdout, "==> [5/10] Configuring firewall rules...")
	configureShieldFirewall(ctx, ops)

	fmt.Fprintln(ops.stdout, "==> [6/10] Writing /etc/nd-edge/config.json...")
	cfg, err := loadOrInitEngineConfig(ops, tmpCfg)
	if err != nil {
		return err
	}
	if installedByND {
		_ = edge.WriteFileAtomic(edgeInstalledMarkerForEngine(ops.paths, engineName), []byte("1\n"), 0o600)
	}

	fmt.Fprintln(ops.stdout, "==> [7/10] Registering with NextDeploy panel...")
	addedPanel, initialSnap, err := registerAndVerifyPanel(ctx, ops, cfg, addPanelParams{
		panelURL:      panelURL,
		token:         token,
		panelCASHA256: caPin,
		insecureHTTP:  opts.insecureHTTP,
		force:         opts.force,
		caddyVersion:  proxyVer,
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

	fmt.Fprintln(ops.stdout, "==> [8/10] Installing nd-edge systemd service...")
	if err := edge.WriteFileAtomic(ops.unitPath, []byte(renderSystemdUnitForEngine(ops.binaryPath, ep.ServiceUnit)), 0o644); err != nil {
		return err
	}
	if engineName == edge.EngineAngie {
		if err := writeAngieSystemdDropIn(ops); err != nil {
			return fmt.Errorf("angie systemd drop-in: %w", err)
		}
		warnAngieOpenSSL(ctx, ops)
	}
	if out, err := ops.runCmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(out))
	}

	fmt.Fprintf(ops.stdout, "==> [9/10] Ensuring port ownership and starting %s...\n", engineName)
	if conflictingSvc != "" {
		_, _ = ops.runCmd(ctx, "systemctl", "stop", conflictingSvc)
	}
	if out, err := ops.runCmd(ctx, "systemctl", "enable", "--now", engineName); err != nil {
		if conflictingSvc != "" {
			_, _ = ops.runCmd(ctx, "systemctl", "start", conflictingSvc)
		}
		return fmt.Errorf("systemctl enable --now %s: %w: %s", engineName, err, strings.TrimSpace(out))
	}
	if conflictingSvc != "" {
		_, _ = ops.runCmd(ctx, "systemctl", "disable", conflictingSvc)
	}
	applyEnginePermsAndSELinux(ops, ep)

	fmt.Fprintln(ops.stdout, "==> [10/10] Running initial reconcile and starting nd-edge...")
	reconcileSummary := ""
	if summary, recErr := runSyncReconcileLocked(ctx, ops, cfg, false); recErr == nil {
		reconcileSummary = summary
	} else if !errors.Is(recErr, edge.ErrLockHeld) {
		fmt.Fprintf(ops.stderr, "WARNING: initial reconcile reported: %v\n", recErr)
	}
	if out, err := ops.runCmd(ctx, "systemctl", "enable", "--now", "nd-edge"); err != nil {
		return fmt.Errorf("systemctl enable --now nd-edge: %w: %s", err, strings.TrimSpace(out))
	}
	applyEnginePermsAndSELinux(ops, ep)

	ui.Success("NextDeploy Edge Shield installed (proxy=%s, edge_id=%s, panel=%s)", engineName, cfg.EdgeID, addedPanel.PanelName)
	if reconcileSummary != "" {
		fmt.Fprintln(ops.stdout, reconcileSummary)
	}
	return nil
}

func ensureEngineInstallLayout(ops *edgeOps, eng edge.Engine, cfg *edge.Config) error {
	ep := eng.Paths()
	for _, d := range []string{
		filepath.Dir(ep.MainConfig),
		ep.ManagedDir,
		ep.UserDir,
		ep.CertsDir,
		ep.ManualCertsDir,
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

	placeholderName, placeholderBody, healthName, healthBody := engineInstallTemplates(eng)
	userPlaceholder := filepath.Join(ep.UserDir, placeholderName)
	if _, err := os.Stat(userPlaceholder); errors.Is(err, os.ErrNotExist) {
		if err := edge.WriteFileAtomic(userPlaceholder, []byte(placeholderBody), 0o644); err != nil {
			return err
		}
	}

	cfStore := edge.LoadOrInitCFIPStore(ops.paths.CFIPsFile)
	globalFiles, err := eng.RenderGlobal(cfg, cfStore.Ranges(), false)
	if err != nil {
		return err
	}
	commonFiles, err := eng.RenderCommon()
	if err != nil {
		return err
	}
	for p, content := range globalFiles {
		if err := edge.WriteFileAtomic(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	for p, content := range commonFiles {
		if err := edge.WriteFileAtomic(p, []byte(content), 0o644); err != nil {
			return err
		}
	}

	if _, err := edge.ReadDirLink(ep.PanelsLink); err != nil {
		gen0Panels := ep.PanelGenDir(0)
		if err := os.MkdirAll(gen0Panels, 0o750); err != nil {
			return err
		}
		if err := edge.WriteFileAtomic(filepath.Join(gen0Panels, placeholderName), []byte(placeholderBody), 0o640); err != nil {
			return err
		}
		if err := edge.WriteFileAtomic(filepath.Join(gen0Panels, healthName), []byte(healthBody), 0o640); err != nil {
			return err
		}
		if err := edge.SwapDirLinkAtomic(ep.PanelsLink, gen0Panels); err != nil {
			return err
		}
	}
	if _, err := edge.ReadDirLink(ep.OriginsLink); err != nil {
		gen0Origins := ep.OriginGenDir(0)
		if err := os.MkdirAll(gen0Origins, 0o750); err != nil {
			return err
		}
		if err := edge.SwapDirLinkAtomic(ep.OriginsLink, gen0Origins); err != nil {
			return err
		}
	}
	return nil
}

func engineInstallTemplates(eng edge.Engine) (placeholderName, placeholderBody, healthName, healthBody string) {
	switch eng.Name() {
	case edge.EngineFerron:
		return edge.PlaceholderFerronFileName, edge.RenderPlaceholderFerron(), edge.HealthFerronFileName, edge.RenderHealthProbeFerron()
	case edge.EngineAngie:
		return edge.PlaceholderAngieFileName, edge.RenderPlaceholderAngie(), edge.HealthAngieFileName, edge.RenderHealthProbeAngie()
	default:
		return edge.PlaceholderFileName, edge.RenderPlaceholderCaddy(), edge.HealthFileName, edge.RenderHealthProbeCaddy()
	}
}

func loadOrInitEngineConfig(ops *edgeOps, initCfg *edge.Config) (*edge.Config, error) {
	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		if !errors.Is(err, edge.ErrConfigNotFound) {
			return nil, err
		}
		edgeID, idErr := newEdgeUUIDv4()
		if idErr != nil {
			return nil, idErr
		}
		cfg = &edge.Config{EdgeID: edgeID}
	}
	cfg.ProxyEngine = initCfg.EngineName()
	if initCfg.ACMEEmail != "" {
		cfg.ACMEEmail = initCfg.ACMEEmail
	}
	cfg.DNSGateOff = initCfg.DNSGateOff
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

// writeAngieSystemdDropIn aligns the packaged angie.service with the pid file nd edge renders
// (edge.DefaultAngiePIDFile). Packages differ (Ubuntu 20.04's Angie 1.10 uses /run/angie.pid
// in PIDFile, ExecReload and ExecStop): a mismatch makes systemd time out on start and makes
// every `systemctl reload angie` fail, so config changes would never reach Angie. Reload and
// stop signal $MAINPID instead of reading a path. The runtime directory is kept between
// restarts so `angie -t` can open the pid file while stopped.
func writeAngieSystemdDropIn(ops *edgeOps) error {
	dir := filepath.Join(filepath.Dir(ops.unitPath), "angie.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := "# Managed by nd edge.\n[Service]\nPIDFile=" + edge.DefaultAngiePIDFile + "\nRuntimeDirectory=angie\nRuntimeDirectoryPreserve=yes\n" +
		"ExecReload=\nExecReload=/bin/kill -s HUP $MAINPID\nExecStop=\nExecStop=/bin/kill -s TERM $MAINPID\n"
	if err := edge.WriteFileAtomic(filepath.Join(dir, "nd-edge.conf"), []byte(body), 0o644); err != nil {
		return err
	}
	if err := fixAngieLogrotatePID(); err != nil {
		ui.Warn("could not update %s: %v", angieLogrotatePath, err)
	}
	return nil
}
