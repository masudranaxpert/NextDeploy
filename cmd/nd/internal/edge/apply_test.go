package edge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeCaddyRunner struct {
	validateFn func(ctx context.Context, configPath string) (string, error)
	reloadFn   func(ctx context.Context, configPath, adminAddr string) (time.Duration, error)
	adminErr   error

	validateCalls int
	reloadCalls   int
}

func (f *fakeCaddyRunner) Validate(ctx context.Context, configPath string) (string, error) {
	f.validateCalls++
	if f.validateFn != nil {
		return f.validateFn(ctx, configPath)
	}
	return "Valid configuration", nil
}

func (f *fakeCaddyRunner) Reload(ctx context.Context, configPath, adminAddr string) (time.Duration, error) {
	f.reloadCalls++
	if f.reloadFn != nil {
		return f.reloadFn(ctx, configPath, adminAddr)
	}
	return 10 * time.Millisecond, nil
}

func (f *fakeCaddyRunner) CheckAdmin(_ context.Context, _ string) error {
	return f.adminErr
}

func TestApplierAtomicSwapValidateFailRollbackPruneAndDrift(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())
	caddy := &fakeCaddyRunner{}
	applier := NewApplier(paths, caddy)
	meta := &ApplyMeta{}

	baseInput := ApplyInput{
		AdminAddr:   DefaultAdminAddr,
		GlobalOpts:  []byte(RenderGlobalOpts(DefaultCloudflareRanges)),
		CommonCaddy: []byte(RenderCommonCaddy()),
		Panels: []PanelRenderArtifact{
			{PanelID: "panel-a", CaddyBytes: []byte("# panel-a v1\n"), Changed: true},
		},
	}

	// 1. Apply gen 1
	res, err := applier.Apply(ctx, baseInput, meta, now)
	if err != nil || !res.Applied || res.Generation != 1 {
		t.Fatalf("gen 1 apply failed: res=%+v err=%v", res, err)
	}
	target1, err := ReadDirLink(paths.PanelsLink)
	if err != nil || filepath.Base(target1) != "panels.gen-000001" {
		t.Fatalf("unexpected panels symlink target: %q err=%v", target1, err)
	}

	// 2. Identical re-apply is a NoOp (zero reload!)
	caddy.reloadCalls = 0
	res, err = applier.Apply(ctx, baseInput, meta, now.Add(time.Minute))
	if err != nil || !res.NoOp || caddy.reloadCalls != 0 {
		t.Fatalf("expected NoOp without reload, got res=%+v reloads=%d err=%v", res, caddy.reloadCalls, err)
	}

	// 3. E-CADDY-7: manual edit on active panel file is detected as drift and overwritten.
	if err := os.WriteFile(filepath.Join(target1, "panel-a.caddy"), []byte("# hand edited\n"), 0o640); err != nil {
		t.Fatalf("tamper panel-a.caddy: %v", err)
	}
	res, err = applier.Apply(ctx, baseInput, meta, now.Add(2*time.Minute))
	if err != nil || !res.Applied || len(res.DriftedFiles) == 0 {
		t.Fatalf("expected drift overwrite apply, got res=%+v err=%v", res, err)
	}

	// 4. E-CADDY-2: caddy validate fails -> never swaps symlink.
	prevLink, _ := ReadDirLink(paths.PanelsLink)
	caddy.validateFn = func(_ context.Context, _ string) (string, error) {
		return "syntax error", errors.New("validate error: syntax error")
	}
	badInput := baseInput
	badInput.Panels = []PanelRenderArtifact{
		{PanelID: "panel-a", CaddyBytes: []byte("# panel-a bad\n"), Changed: true},
	}
	_, err = applier.Apply(ctx, badInput, meta, now.Add(3*time.Minute))
	if err == nil {
		t.Fatalf("expected validate failure")
	}
	curLink, _ := ReadDirLink(paths.PanelsLink)
	if curLink != prevLink {
		t.Fatalf("symlink swapped despite validate failure: %q != %q", curLink, prevLink)
	}
	caddy.validateFn = nil

	// 5. E-CADDY-3: validate ok but reload fails -> rollback symlink + reload old.
	reloadAttempt := 0
	caddy.reloadFn = func(_ context.Context, _ string, _ string) (time.Duration, error) {
		reloadAttempt++
		if reloadAttempt == 1 {
			return 0, errors.New("bind :443 permission denied")
		}
		return 5 * time.Millisecond, nil // rollback reload succeeds
	}
	v2Input := baseInput
	v2Input.Panels = []PanelRenderArtifact{
		{PanelID: "panel-a", CaddyBytes: []byte("# panel-a v2\n"), Changed: true},
	}
	_, err = applier.Apply(ctx, v2Input, meta, now.Add(4*time.Minute))
	if err == nil {
		t.Fatalf("expected reload failure")
	}
	curLinkAfterRB, _ := ReadDirLink(paths.PanelsLink)
	if curLinkAfterRB != prevLink {
		t.Fatalf("expected rollback to %q, got %q", prevLink, curLinkAfterRB)
	}
	if reloadAttempt != 2 {
		t.Fatalf("expected 2 reload attempts (fail + rollback), got %d", reloadAttempt)
	}
	caddy.reloadFn = nil
	meta.ConsecutiveFailures = 0
	meta.NextRetryAt = nil

	// 6. E-STATE-5: multiple applies prune old generations keeping last 3.
	for i := 3; i <= 6; i++ {
		in := baseInput
		in.Panels = []PanelRenderArtifact{
			{PanelID: "panel-a", CaddyBytes: []byte(string(rune('a'+i)) + "\n"), Changed: true},
		}
		if _, err := applier.Apply(ctx, in, meta, now.Add(time.Duration(4+i)*time.Minute)); err != nil {
			t.Fatalf("apply %d failed: %v", i, err)
		}
	}
	entries, _ := os.ReadDir(paths.CaddyNDDir)
	var genCount int
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "panels.gen-") {
			genCount++
		}
	}
	if genCount != 3 {
		t.Fatalf("expected 3 retained generations, got %d", genCount)
	}
}

func TestApplierOffendingPanelIsolationEMP5(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())

	caddy := &fakeCaddyRunner{
		validateFn: func(_ context.Context, configPath string) (string, error) {
			raw, _ := os.ReadFile(configPath)
			// Find panels dir from import line and inspect if panel-bad is present.
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "import ") && strings.Contains(line, ".panels.validate.") {
					glob := strings.TrimSpace(strings.TrimPrefix(line, "import "))
					dir := filepath.Dir(glob)
					if _, err := os.Stat(filepath.Join(dir, "panel-bad.caddy")); err == nil {
						return "bad directive", errors.New("panel-bad syntax error")
					}
				}
			}
			return "ok", nil
		},
	}

	applier := NewApplier(paths, caddy)
	meta := &ApplyMeta{}
	res, err := applier.Apply(ctx, ApplyInput{
		AdminAddr:   DefaultAdminAddr,
		GlobalOpts:  []byte(RenderGlobalOpts(DefaultCloudflareRanges)),
		CommonCaddy: []byte(RenderCommonCaddy()),
		Panels: []PanelRenderArtifact{
			{PanelID: "panel-good", CaddyBytes: []byte("# good\n"), Changed: false},
			{PanelID: "panel-bad", CaddyBytes: []byte("# bad\n"), Changed: true},
		},
	}, meta, now)
	if err != nil || !res.Applied {
		t.Fatalf("expected E-MP-5 isolation to succeed for panel-good, got res=%+v err=%v", res, err)
	}
	if _, isolated := res.IsolatedPanels["panel-bad"]; !isolated {
		t.Fatalf("expected panel-bad to be isolated, got %+v", res.IsolatedPanels)
	}
	curDir, _ := ReadDirLink(paths.PanelsLink)
	if _, err := os.Stat(filepath.Join(curDir, "panel-good.caddy")); err != nil {
		t.Fatalf("expected panel-good.caddy to be applied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(curDir, "panel-bad.caddy")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected panel-bad.caddy to be excluded, err=%v", err)
	}
}

func TestApplierValidationStagesGlobalOptsCommonAndUserSites(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())

	// Seed Caddyfile with user sites outside markers (adopted Shield).
	userSite := "user.example.com {\n\trespond \"ok\" 200\n}\n"
	if err := WriteFileAtomic(paths.Caddyfile, []byte(userSite), 0o644); err != nil {
		t.Fatalf("seed Caddyfile: %v", err)
	}

	var validatedContent string
	var stagedFilesExisted bool
	var liveUntouchedDuringValidate bool
	caddy := &fakeCaddyRunner{
		validateFn: func(_ context.Context, configPath string) (string, error) {
			raw, err := os.ReadFile(configPath)
			if err != nil {
				return "", err
			}
			validatedContent = string(raw)

			// Live GlobalOpts/CommonCaddy must NOT exist yet on first validate.
			_, gErr := os.Stat(paths.GlobalOpts)
			_, cErr := os.Stat(paths.CommonCaddy)
			liveUntouchedDuringValidate = errors.Is(gErr, os.ErrNotExist) && errors.Is(cErr, os.ErrNotExist)

			// Staged global.opts and common.caddy referenced inside valCaddyfile MUST exist on disk.
			var checkedG, checkedC bool
			for _, line := range strings.Split(validatedContent, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "import ") {
					target := strings.TrimSpace(strings.TrimPrefix(line, "import "))
					if strings.Contains(target, ".global.opts.validate.") {
						if _, err := os.Stat(target); err == nil {
							checkedG = true
						}
					}
					if strings.Contains(target, ".common.caddy.validate.") {
						if _, err := os.Stat(target); err == nil {
							checkedC = true
						}
					}
				}
			}
			stagedFilesExisted = checkedG && checkedC
			return "ok", nil
		},
	}

	applier := NewApplier(paths, caddy)
	meta := &ApplyMeta{}
	res, err := applier.Apply(ctx, ApplyInput{
		AdminAddr:   DefaultAdminAddr,
		GlobalOpts:  []byte(RenderGlobalOpts(DefaultCloudflareRanges)),
		CommonCaddy: []byte(RenderCommonCaddy()),
		Panels: []PanelRenderArtifact{
			{PanelID: "panel-a", CaddyBytes: []byte("# panel-a\n"), Changed: true},
		},
	}, meta, now)
	if err != nil || !res.Applied {
		t.Fatalf("apply failed: res=%+v err=%v", res, err)
	}
	if !liveUntouchedDuringValidate || !stagedFilesExisted {
		t.Fatalf("expected staged global.opts/common.caddy during validate with live untouched; liveUntouched=%v stagedExisted=%v", liveUntouchedDuringValidate, stagedFilesExisted)
	}
	if !strings.Contains(validatedContent, "user.example.com") || !strings.Contains(validatedContent, ManagedStartMarker) {
		t.Fatalf("expected validated Caddyfile to merge user sites + managed region, got:\n%s", validatedContent)
	}
	finalCaddyfile, _ := os.ReadFile(paths.Caddyfile)
	if !strings.Contains(string(finalCaddyfile), "user.example.com") {
		t.Fatalf("expected user sites preserved in final Caddyfile")
	}
}

func TestApplierFirstApplyReloadFailureCleansDanglingLinks(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())

	caddy := &fakeCaddyRunner{
		reloadFn: func(_ context.Context, _ string, _ string) (time.Duration, error) {
			return 0, errors.New("reload failed on first apply")
		},
	}
	applier := NewApplier(paths, caddy)
	meta := &ApplyMeta{}
	_, err := applier.Apply(ctx, ApplyInput{
		AdminAddr:   DefaultAdminAddr,
		GlobalOpts:  []byte(RenderGlobalOpts(DefaultCloudflareRanges)),
		CommonCaddy: []byte(RenderCommonCaddy()),
		Panels: []PanelRenderArtifact{
			{PanelID: "panel-a", CaddyBytes: []byte("# panel-a\n"), Changed: true},
		},
	}, meta, now)
	if err == nil {
		t.Fatalf("expected reload error")
	}
	if caddy.reloadCalls != 1 {
		t.Fatalf("expected 1 reload call on first-apply failure (no bogus rollback reload), got %d", caddy.reloadCalls)
	}
	if _, statErr := os.Lstat(paths.PanelsLink); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected PanelsLink removed on first-apply rollback, got statErr=%v", statErr)
	}
	if _, statErr := os.Lstat(paths.OriginsLink); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected OriginsLink removed on first-apply rollback, got statErr=%v", statErr)
	}
}

func TestApplierAppliesCaddyGroupOwnershipBeforeReload(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	paths := NewPaths(t.TempDir())

	var events []string
	caddy := &fakeCaddyRunner{
		reloadFn: func(_ context.Context, _ string, _ string) (time.Duration, error) {
			events = append(events, "reload")
			return 5 * time.Millisecond, nil
		},
	}
	applier := NewApplier(paths, caddy)
	applier.chownGroup = func(p, group string, recursive bool) error {
		events = append(events, "chown:"+filepath.Base(p)+":"+group)
		return nil
	}

	res, err := applier.Apply(ctx, ApplyInput{
		AdminAddr:   DefaultAdminAddr,
		CaddyGroup:  "caddy",
		GlobalOpts:  []byte(RenderGlobalOpts(DefaultCloudflareRanges)),
		CommonCaddy: []byte(RenderCommonCaddy()),
		Panels: []PanelRenderArtifact{
			{
				PanelID:    "panel-a",
				CaddyBytes: []byte("# panel-a\n"),
				PEMBytes:   []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"),
				Changed:    true,
			},
		},
	}, &ApplyMeta{}, now)
	if err != nil || !res.Applied {
		t.Fatalf("apply failed: res=%+v err=%v", res, err)
	}

	reloadIdx := -1
	var sawPanelGen, sawOriginGen bool
	for i, ev := range events {
		if ev == "reload" {
			reloadIdx = i
		}
		if strings.HasPrefix(ev, "chown:panels.gen-000001:caddy") {
			sawPanelGen = true
		}
		if strings.HasPrefix(ev, "chown:origins.gen-000001:caddy") {
			sawOriginGen = true
		}
	}
	if !sawPanelGen || !sawOriginGen || reloadIdx <= 0 {
		t.Fatalf("expected generation dirs chowned before reload, got events=%v", events)
	}
	for i := reloadIdx + 1; i < len(events); i++ {
		if strings.HasPrefix(events[i], "chown:") {
			t.Fatalf("unexpected chown after reload in events=%v", events)
		}
	}
}
