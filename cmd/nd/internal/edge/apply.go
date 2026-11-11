package edge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	caddyValidateTimeout = 60 * time.Second
	caddyReloadTimeout   = 90 * time.Second
	maxValidateErrBytes  = 2048
	generationsToKeep    = 3
)

// CaddyRunner abstracts `caddy validate`, `caddy reload`, and admin reachability
// so unit tests can inject validation/reload failures and hangs (E-CADDY-2/3/4).
type CaddyRunner interface {
	Validate(ctx context.Context, configPath string) (output string, err error)
	Reload(ctx context.Context, configPath, adminAddr string) (duration time.Duration, err error)
	CheckAdmin(ctx context.Context, adminAddr string) error
}

// SystemdRunner abstracts systemd interactions for Caddy health and journalctl
// ACME error inspection.
type SystemdRunner interface {
	IsActive(ctx context.Context, unit string) (active bool, statusDetail string, err error)
	Start(ctx context.Context, unit string) error
	CaddyUser(ctx context.Context) string
	JournalCaddyErrors(ctx context.Context, since string) ([]byte, error)
}

// ExecCaddyRunner invokes the real `caddy` binary.
type ExecCaddyRunner struct {
	Binary string
}

func (r *ExecCaddyRunner) bin() string {
	if r.Binary != "" {
		return r.Binary
	}
	return "caddy"
}

// Validate runs `caddy validate --config <configPath> --adapter caddyfile` (60s timeout).
func (r *ExecCaddyRunner) Validate(ctx context.Context, configPath string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, caddyValidateTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, r.bin(), "validate", "--config", configPath, "--adapter", "caddyfile")
	out, err := cmd.CombinedOutput()
	trimmed := truncateStr(string(out), maxValidateErrBytes)
	if err != nil {
		return trimmed, fmt.Errorf("caddy validate failed: %w: %s", err, trimmed)
	}
	return trimmed, nil
}

// Reload runs `caddy reload --config <configPath> --adapter caddyfile --address <adminAddr>` (90s timeout).
func (r *ExecCaddyRunner) Reload(ctx context.Context, configPath, adminAddr string) (time.Duration, error) {
	if adminAddr == "" {
		adminAddr = DefaultAdminAddr
	}
	cctx, cancel := context.WithTimeout(ctx, caddyReloadTimeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(cctx, r.bin(), "reload", "--config", configPath, "--adapter", "caddyfile", "--address", adminAddr)
	out, err := cmd.CombinedOutput()
	dur := time.Since(start)
	if err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			adminErr := r.CheckAdmin(ctx, adminAddr)
			return dur, fmt.Errorf("caddy reload timed out after %s (admin check: %v)", caddyReloadTimeout, adminErr)
		}
		return dur, fmt.Errorf("caddy reload failed: %w: %s", err, truncateStr(string(out), maxValidateErrBytes))
	}
	return dur, nil
}

// CheckAdmin verifies Caddy's admin endpoint is reachable.
func (r *ExecCaddyRunner) CheckAdmin(ctx context.Context, adminAddr string) error {
	if adminAddr == "" {
		adminAddr = DefaultAdminAddr
	}
	// adminAddr may be "localhost:2019" or "http://localhost:2019".
	hostPort := strings.TrimPrefix(strings.TrimPrefix(adminAddr, "http://"), "https://")
	d := &netDialChecker{}
	return d.CheckTCP(ctx, hostPort, 3*time.Second)
}

// ExecSystemdRunner invokes systemctl / journalctl on Linux.
type ExecSystemdRunner struct{}

func (ExecSystemdRunner) IsActive(ctx context.Context, unit string) (bool, string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "systemctl", "is-active", unit).CombinedOutput()
	state := strings.TrimSpace(string(out))
	if err == nil && state == "active" {
		return true, "active", nil
	}
	statusOut, _ := exec.CommandContext(cctx, "systemctl", "status", unit, "--no-pager", "-n", "10").CombinedOutput()
	detail := strings.TrimSpace(string(statusOut))
	if detail == "" {
		detail = state
	}
	return false, truncateStr(detail, 1024), err
}

func (ExecSystemdRunner) Start(ctx context.Context, unit string) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "systemctl", "start", unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl start %s: %w: %s", unit, err, truncateStr(string(out), 512))
	}
	return nil
}

func (ExecSystemdRunner) CaddyUser(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "systemctl", "show", "-p", "User", "--value", "caddy.service").Output()
	if err != nil {
		return "caddy"
	}
	u := strings.TrimSpace(string(out))
	if u == "" {
		return "caddy"
	}
	return u
}

func (ExecSystemdRunner) JournalCaddyErrors(ctx context.Context, since string) ([]byte, error) {
	if since == "" {
		since = "-60min"
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(cctx, "journalctl", "-u", "caddy", "-o", "json", "--since", since).Output()
}

// PanelRenderArtifact holds the rendered files for one panel in a generation.
type PanelRenderArtifact struct {
	PanelID    string
	CaddyBytes []byte
	PEMBytes   []byte // nil when ca_status == "missing"
	Changed    bool   // true if panel snapshot changed since last apply (for E-MP-5 isolation)
}

// ApplyInput contains all files for an atomic Caddy generation swap (§5.5).
type ApplyInput struct {
	AdminAddr   string
	ACMEEmail   string
	CaddyGroup  string
	GlobalOpts  []byte
	CommonCaddy []byte
	Panels      []PanelRenderArtifact
	Force       bool
}

// ApplyResult describes the outcome of Applier.Apply.
type ApplyResult struct {
	Applied         bool
	NoOp            bool
	DriftedFiles    []string
	Generation      int
	ReloadDuration  time.Duration
	SlowReloadWarn  bool
	IsolatedPanels  map[string]string // panel_id -> error when excluded by E-MP-5
}

// Applier manages atomic generation directories, validation, symlink swaps,
// Caddy reload, rollback, and old-generation pruning (§5.5).
type Applier struct {
	paths      Paths
	caddy      CaddyRunner
	chownGroup func(path, group string, recursive bool) error
}

// NewApplier constructs an Applier.
func NewApplier(paths Paths, caddy CaddyRunner) *Applier {
	if caddy == nil {
		caddy = &ExecCaddyRunner{}
	}
	return &Applier{
		paths:      paths,
		caddy:      caddy,
		chownGroup: ChownGroup,
	}
}

func (a *Applier) applyGroup(path, group string, recursive bool) {
	if group == "" || a.chownGroup == nil {
		return
	}
	_ = a.chownGroup(path, group, recursive)
}

// DesiredHash returns a deterministic SHA-256 hash of the desired rendered state
// (used for §5.5 failure backoff tracking).
func DesiredHash(in ApplyInput) string {
	h := sha256.New()
	h.Write(in.GlobalOpts)
	h.Write([]byte{0})
	h.Write(in.CommonCaddy)
	h.Write([]byte{0})
	sorted := make([]PanelRenderArtifact, len(in.Panels))
	copy(sorted, in.Panels)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PanelID < sorted[j].PanelID })
	for _, p := range sorted {
		h.Write([]byte(p.PanelID))
		h.Write([]byte{0})
		h.Write(p.CaddyBytes)
		h.Write([]byte{0})
		h.Write(p.PEMBytes)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Apply performs the §5.5 atomic generation swap and Caddy reload, with
// E-MP-5 offending-panel isolation and E-CADDY-3 automatic rollback.
func (a *Applier) Apply(ctx context.Context, in ApplyInput, meta *ApplyMeta, now time.Time) (ApplyResult, error) {
	var res ApplyResult
	if meta == nil {
		meta = &ApplyMeta{}
	}

	desiredHash := DesiredHash(in)

	// Check §5.5 #5 failure backoff unless desired state changed or Force is set.
	if !in.Force && meta.ConsecutiveFailures >= 2 && meta.FailedDesiredHash == desiredHash {
		return res, fmt.Errorf("apply paused after %d consecutive failures for unchanged desired state: %s", meta.ConsecutiveFailures, meta.LastApplyError)
	}
	if !in.Force && meta.NextRetryAt != nil && now.Before(*meta.NextRetryAt) && meta.FailedDesiredHash == desiredHash {
		return res, fmt.Errorf("apply backoff active until %s: %s", meta.NextRetryAt.Format(time.RFC3339), meta.LastApplyError)
	}

	// Ensure base directories and placeholder in user dir exist (E-CADDY-11).
	if err := a.ensureBaseDirs(in.CaddyGroup); err != nil {
		return res, err
	}

	// Detect whether rendered bytes or on-disk files drifted from desired state (E-CADDY-7).
	drifted, identical := a.compareCurrentGeneration(in)
	res.DriftedFiles = drifted
	if identical && !in.Force {
		res.NoOp = true
		return res, nil
	}

	// Build candidate panel set; if caddy validate fails with multiple panels,
	// isolate the newest changed offending panel (E-MP-5).
	activePanels := make([]PanelRenderArtifact, len(in.Panels))
	copy(activePanels, in.Panels)
	sort.Slice(activePanels, func(i, j int) bool { return activePanels[i].PanelID < activePanels[j].PanelID })

	var nextGen int
	var panelDir, originDir string

	for {
		nextGen = a.nextGenerationNumber(meta.CurrentGen)
		panelDir = a.paths.PanelGenDir(nextGen)
		originDir = a.paths.OriginGenDir(nextGen)

		if err := a.stageGeneration(panelDir, originDir, in, activePanels); err != nil {
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}

		// Stage temp copies of global.opts, common.caddy, panels, and Caddyfile so
		// `caddy validate` tests the complete candidate config before any live file
		// is touched (fixes clean-Shield first apply & stale-opts validation).
		valCaddyfile := filepath.Join(a.paths.CaddyNDDir, fmt.Sprintf(".Caddyfile.validate.%d", nextGen))
		valGlobalOpts := filepath.Join(a.paths.CaddyNDDir, fmt.Sprintf(".global.opts.validate.%d", nextGen))
		valCommonCaddy := filepath.Join(a.paths.CaddyNDDir, fmt.Sprintf(".common.caddy.validate.%d", nextGen))
		valPanelsDir := filepath.Join(a.paths.CaddyNDDir, fmt.Sprintf(".panels.validate.%d", nextGen))
		cleanupValidation := func() {
			_ = os.Remove(valCaddyfile)
			_ = os.Remove(valGlobalOpts)
			_ = os.Remove(valCommonCaddy)
			_ = os.RemoveAll(valPanelsDir)
		}

		if err := WriteFileAtomic(valGlobalOpts, in.GlobalOpts, 0o644); err != nil {
			cleanupValidation()
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}
		if err := WriteFileAtomic(valCommonCaddy, in.CommonCaddy, 0o644); err != nil {
			cleanupValidation()
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}
		if err := a.stageValidationCopy(valPanelsDir, panelDir, originDir); err != nil {
			cleanupValidation()
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}

		valPaths := a.paths
		valPaths.GlobalOpts = valGlobalOpts
		valPaths.CommonCaddy = valCommonCaddy
		valRegion := RenderCaddyfileManagedRegion(valPaths, in.AdminAddr, in.ACMEEmail, valPanelsDir, a.paths.UserCaddyDir)
		valContent := valRegion
		if existingBytes, readErr := os.ReadFile(a.paths.Caddyfile); readErr == nil {
			valContent = mergeManagedCaddyfile(string(existingBytes), valRegion)
		}
		if err := WriteFileAtomic(valCaddyfile, []byte(valContent), 0o644); err != nil {
			cleanupValidation()
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}

		_, valErr := a.caddy.Validate(ctx, valCaddyfile)
		cleanupValidation()

		if valErr == nil {
			break
		}

		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)

		// E-MP-5: if multiple panels exist and one is marked Changed, isolate it and retry.
		offenderIdx := findOffenderIndex(activePanels)
		if len(activePanels) > 1 && offenderIdx >= 0 {
			offender := activePanels[offenderIdx]
			if res.IsolatedPanels == nil {
				res.IsolatedPanels = make(map[string]string)
			}
			res.IsolatedPanels[offender.PanelID] = valErr.Error()
			activePanels = append(activePanels[:offenderIdx], activePanels[offenderIdx+1:]...)
			continue
		}

		a.recordFailure(meta, desiredHash, valErr, now)
		return res, valErr
	}

	// Ensure global.opts, common.caddy, and Caddyfile are in place before swap.
	if err := WriteFileAtomic(a.paths.GlobalOpts, in.GlobalOpts, 0o644); err != nil {
		a.recordFailure(meta, desiredHash, err, now)
		return res, err
	}
	a.applyGroup(a.paths.GlobalOpts, in.CaddyGroup, false)
	if err := WriteFileAtomic(a.paths.CommonCaddy, in.CommonCaddy, 0o644); err != nil {
		a.recordFailure(meta, desiredHash, err, now)
		return res, err
	}
	a.applyGroup(a.paths.CommonCaddy, in.CaddyGroup, false)
	if err := a.ensureManagedCaddyfile(in.AdminAddr, in.ACMEEmail); err != nil {
		a.recordFailure(meta, desiredHash, err, now)
		return res, err
	}

	// Capture previous targets for rollback on reload failure (E-CADDY-3).
	prevPanelsTarget, _ := ReadDirLink(a.paths.PanelsLink)
	prevOriginsTarget, _ := ReadDirLink(a.paths.OriginsLink)

	if err := SwapDirLinkAtomic(a.paths.OriginsLink, originDir); err != nil {
		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)
		a.recordFailure(meta, desiredHash, err, now)
		return res, err
	}
	a.applyGroup(a.paths.OriginsLink, in.CaddyGroup, false)
	if err := SwapDirLinkAtomic(a.paths.PanelsLink, panelDir); err != nil {
		if prevOriginsTarget != "" {
			_ = SwapDirLinkAtomic(a.paths.OriginsLink, prevOriginsTarget)
		} else {
			_ = RemoveDirLink(a.paths.OriginsLink)
		}
		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)
		a.recordFailure(meta, desiredHash, err, now)
		return res, err
	}
	a.applyGroup(a.paths.PanelsLink, in.CaddyGroup, false)

	dur, reloadErr := a.caddy.Reload(ctx, a.paths.Caddyfile, in.AdminAddr)
	if reloadErr != nil {
		// Rollback symlinks (or remove if first apply so no dangling symlinks remain)
		// and reload previous generation when one existed (E-CADDY-3).
		if prevOriginsTarget != "" {
			_ = SwapDirLinkAtomic(a.paths.OriginsLink, prevOriginsTarget)
		} else {
			_ = RemoveDirLink(a.paths.OriginsLink)
		}
		if prevPanelsTarget != "" {
			_ = SwapDirLinkAtomic(a.paths.PanelsLink, prevPanelsTarget)
			_, rbReloadErr := a.caddy.Reload(ctx, a.paths.Caddyfile, in.AdminAddr)
			if rbReloadErr != nil {
				reloadErr = fmt.Errorf("%w (rollback reload also failed: %v)", reloadErr, rbReloadErr)
			}
		} else {
			_ = RemoveDirLink(a.paths.PanelsLink)
		}
		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)
		a.recordFailure(meta, desiredHash, reloadErr, now)
		return res, reloadErr
	}

	// Success: record generation & prune old generations keeping last 3 (E-STATE-5).
	meta.CurrentGen = nextGen
	nowUTC := now.UTC()
	meta.LastApplyAt = &nowUTC
	meta.LastApplyError = ""
	meta.LastReloadDuration = dur.Milliseconds()
	meta.ConsecutiveFailures = 0
	meta.FailedDesiredHash = ""
	meta.NextRetryAt = nil

	a.pruneOldGenerations(nextGen)

	res.Applied = true
	res.Generation = nextGen
	res.ReloadDuration = dur
	res.SlowReloadWarn = dur > 10*time.Second
	return res, nil
}

func (a *Applier) ensureBaseDirs(caddyGroup string) error {
	for _, d := range []string{
		filepath.Dir(a.paths.Caddyfile),
		a.paths.CaddyNDDir,
		a.paths.UserCaddyDir,
	} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	userPlaceholder := filepath.Join(a.paths.UserCaddyDir, PlaceholderFileName)
	if _, err := os.Stat(userPlaceholder); errors.Is(err, os.ErrNotExist) {
		if err := WriteFileAtomic(userPlaceholder, []byte(RenderPlaceholderCaddy()), 0o644); err != nil {
			return err
		}
	}
	a.applyGroup(a.paths.CaddyNDDir, caddyGroup, false)
	a.applyGroup(a.paths.UserCaddyDir, caddyGroup, true)
	return nil
}

func (a *Applier) ensureManagedCaddyfile(adminAddr, acmeEmail string) error {
	desiredRegion := RenderCaddyfileManagedRegion(a.paths, adminAddr, acmeEmail, a.paths.PanelsLink, a.paths.UserCaddyDir)
	existingBytes, err := os.ReadFile(a.paths.Caddyfile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WriteFileAtomic(a.paths.Caddyfile, []byte(desiredRegion), 0o644)
		}
		return err
	}
	existing := string(existingBytes)
	updated := mergeManagedCaddyfile(existing, desiredRegion)
	if updated == existing {
		return nil
	}
	return WriteFileAtomic(a.paths.Caddyfile, []byte(updated), 0o644)
}

func mergeManagedCaddyfile(existing, desiredRegion string) string {
	startIdx := strings.Index(existing, ManagedStartMarker)
	endIdx := strings.Index(existing, ManagedEndMarker)
	if startIdx >= 0 && endIdx > startIdx {
		afterEnd := endIdx + len(ManagedEndMarker)
		if afterEnd < len(existing) && existing[afterEnd] == '\n' {
			afterEnd++
		}
		return existing[:startIdx] + desiredRegion + existing[afterEnd:]
	}
	if strings.TrimSpace(existing) == "" {
		return desiredRegion
	}
	return desiredRegion + "\n" + existing
}

func (a *Applier) compareCurrentGeneration(in ApplyInput) (drifted []string, identical bool) {
	if b, err := os.ReadFile(a.paths.GlobalOpts); err != nil || !bytes.Equal(b, in.GlobalOpts) {
		if err == nil {
			drifted = append(drifted, a.paths.GlobalOpts)
		}
		return drifted, false
	}
	if b, err := os.ReadFile(a.paths.CommonCaddy); err != nil || !bytes.Equal(b, in.CommonCaddy) {
		if err == nil {
			drifted = append(drifted, a.paths.CommonCaddy)
		}
		return drifted, false
	}

	curPanelsDir, err := ReadDirLink(a.paths.PanelsLink)
	if err != nil || curPanelsDir == "" {
		return nil, false
	}
	curOriginsDir, err := ReadDirLink(a.paths.OriginsLink)
	if err != nil || curOriginsDir == "" {
		return nil, false
	}

	// Check exact file set in current panels dir.
	entries, err := os.ReadDir(curPanelsDir)
	if err != nil {
		return nil, false
	}
	expectedCaddy := make(map[string][]byte, len(in.Panels)+1)
	expectedCaddy[PlaceholderFileName] = []byte(RenderPlaceholderCaddy())
	expectedPEM := make(map[string][]byte, len(in.Panels))
	for _, p := range in.Panels {
		expectedCaddy[p.PanelID+".caddy"] = p.CaddyBytes
		if len(p.PEMBytes) > 0 {
			expectedPEM[p.PanelID+".pem"] = p.PEMBytes
		}
	}

	seenCaddy := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		seenCaddy[name] = true
		want, ok := expectedCaddy[name]
		if !ok {
			return nil, false
		}
		got, readErr := os.ReadFile(filepath.Join(curPanelsDir, name))
		if readErr != nil || !bytes.Equal(got, want) {
			drifted = append(drifted, filepath.Join(curPanelsDir, name))
			return drifted, false
		}
	}
	if len(seenCaddy) != len(expectedCaddy) {
		return nil, false
	}

	originEntries, err := os.ReadDir(curOriginsDir)
	if err != nil {
		return nil, false
	}
	seenPEM := make(map[string]bool)
	for _, e := range originEntries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		seenPEM[name] = true
		want, ok := expectedPEM[name]
		if !ok {
			return nil, false
		}
		got, readErr := os.ReadFile(filepath.Join(curOriginsDir, name))
		if readErr != nil || !bytes.Equal(got, want) {
			drifted = append(drifted, filepath.Join(curOriginsDir, name))
			return drifted, false
		}
	}
	if len(seenPEM) != len(expectedPEM) {
		return nil, false
	}

	return nil, true
}

func (a *Applier) stageGeneration(panelDir, originDir string, in ApplyInput, panels []PanelRenderArtifact) error {
	if err := os.MkdirAll(panelDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", panelDir, err)
	}
	if err := os.MkdirAll(originDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", originDir, err)
	}
	if err := WriteFileAtomic(filepath.Join(panelDir, PlaceholderFileName), []byte(RenderPlaceholderCaddy()), 0o640); err != nil {
		return err
	}
	for _, p := range panels {
		if err := WriteFileAtomic(filepath.Join(panelDir, p.PanelID+".caddy"), p.CaddyBytes, 0o640); err != nil {
			return err
		}
		if len(p.PEMBytes) > 0 {
			if err := WriteFileAtomic(filepath.Join(originDir, p.PanelID+".pem"), p.PEMBytes, 0o640); err != nil {
				return err
			}
		}
	}
	_ = syncDir(panelDir)
	_ = syncDir(originDir)
	// Ensure Caddy's unprivileged service group can traverse and read staged generations before reload.
	a.applyGroup(panelDir, in.CaddyGroup, true)
	a.applyGroup(originDir, in.CaddyGroup, true)
	return nil
}

func (a *Applier) stageValidationCopy(valPanelsDir, panelDir, originDir string) error {
	if err := os.MkdirAll(valPanelsDir, 0o750); err != nil {
		return err
	}
	entries, err := os.ReadDir(panelDir)
	if err != nil {
		return err
	}
	oldPrefix := filepath.ToSlash(a.paths.OriginsLink) + "/"
	newPrefix := filepath.ToSlash(originDir) + "/"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(panelDir, e.Name()))
		if err != nil {
			return err
		}
		rewritten := strings.ReplaceAll(string(b), oldPrefix, newPrefix)
		if err := WriteFileAtomic(filepath.Join(valPanelsDir, e.Name()), []byte(rewritten), 0o640); err != nil {
			return err
		}
	}
	return nil
}

func (a *Applier) nextGenerationNumber(currentGen int) int {
	maxGen := currentGen
	entries, err := os.ReadDir(a.paths.CaddyNDDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			for _, prefix := range []string{"panels.gen-", "origins.gen-"} {
				if strings.HasPrefix(e.Name(), prefix) {
					if n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), prefix)); err == nil && n > maxGen {
						maxGen = n
					}
				}
			}
		}
	}
	return maxGen + 1
}

func (a *Applier) pruneOldGenerations(keepCurrent int) {
	entries, err := os.ReadDir(a.paths.CaddyNDDir)
	if err != nil {
		return
	}
	genSet := make(map[int]struct{})
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, prefix := range []string{"panels.gen-", "origins.gen-"} {
			if strings.HasPrefix(e.Name(), prefix) {
				if n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), prefix)); err == nil {
					genSet[n] = struct{}{}
				}
			}
		}
	}
	gens := make([]int, 0, len(genSet))
	for g := range genSet {
		gens = append(gens, g)
	}
	sort.Ints(gens)
	if len(gens) <= generationsToKeep {
		return
	}
	toDelete := gens[:len(gens)-generationsToKeep]
	for _, g := range toDelete {
		if g == keepCurrent {
			continue
		}
		_ = os.RemoveAll(a.paths.PanelGenDir(g))
		_ = os.RemoveAll(a.paths.OriginGenDir(g))
	}
}

func (a *Applier) recordFailure(meta *ApplyMeta, desiredHash string, err error, now time.Time) {
	if meta.FailedDesiredHash == desiredHash {
		meta.ConsecutiveFailures++
	} else {
		meta.FailedDesiredHash = desiredHash
		meta.ConsecutiveFailures = 1
	}
	meta.LastApplyError = err.Error()

	backoff := time.Minute
	for i := 1; i < meta.ConsecutiveFailures && backoff < 30*time.Minute; i++ {
		backoff *= 2
	}
	if backoff > 30*time.Minute {
		backoff = 30 * time.Minute
	}
	next := now.UTC().Add(backoff)
	meta.NextRetryAt = &next
}

func findOffenderIndex(panels []PanelRenderArtifact) int {
	for i := len(panels) - 1; i >= 0; i-- {
		if panels[i].Changed {
			return i
		}
	}
	return len(panels) - 1
}

type netDialChecker struct{}

func (netDialChecker) CheckTCP(ctx context.Context, addr string, timeout time.Duration) error {
	d := netDialer{timeout: timeout}
	return d.Check(ctx, addr)
}

type netDialer struct {
	timeout time.Duration
}

func (d netDialer) Check(ctx context.Context, addr string) error {
	dialer := &net.Dialer{Timeout: d.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func truncateStr(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max]
}
