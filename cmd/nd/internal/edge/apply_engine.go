package edge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *Applier) applyForEngine(ctx context.Context, in ApplyInput, meta *ApplyMeta, now time.Time) (ApplyResult, error) {
	var res ApplyResult
	if meta == nil {
		meta = &ApplyMeta{}
	}
	ep := a.engine.Paths()
	desiredHash := DesiredHash(in)

	if !in.Force && meta.ConsecutiveFailures >= 2 && meta.FailedDesiredHash == desiredHash {
		return res, fmt.Errorf("apply paused after %d consecutive failures for unchanged desired state: %s", meta.ConsecutiveFailures, meta.LastApplyError)
	}
	if !in.Force && meta.NextRetryAt != nil && now.Before(*meta.NextRetryAt) && meta.FailedDesiredHash == desiredHash {
		return res, fmt.Errorf("apply backoff active until %s: %s", meta.NextRetryAt.Format(time.RFC3339), meta.LastApplyError)
	}

	group := strings.TrimSpace(in.CaddyGroup)
	if group == "" {
		group = ep.ServiceUser
	}
	if err := a.ensureEngineBaseDirs(ep, group); err != nil {
		return res, err
	}

	desiredFiles := mergeEngineConfigMaps(in.GlobalFiles, in.CommonFiles)
	drifted, identical := a.compareEngineGeneration(ep, desiredFiles, in.Panels)
	res.DriftedFiles = drifted
	if identical && !in.Force {
		res.NoOp = true
		return res, nil
	}

	activePanels := make([]PanelRenderArtifact, len(in.Panels))
	copy(activePanels, in.Panels)
	sort.Slice(activePanels, func(i, j int) bool { return activePanels[i].PanelID < activePanels[j].PanelID })

	var nextGen int
	var panelDir, originDir string
	runner := a.engine.Runner()

	for {
		nextGen = nextEngineGenerationNumber(ep.ManagedDir, meta.CurrentGen)
		panelDir = ep.PanelGenDir(nextGen)
		originDir = ep.OriginGenDir(nextGen)

		if err := a.stageEngineGeneration(ep, panelDir, originDir, group, activePanels); err != nil {
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}

		valMain, cleanupVal, err := a.stageEngineValidation(ep, nextGen, panelDir, originDir, desiredFiles)
		if err != nil {
			cleanupVal()
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}

		_, valErr := runner.Validate(ctx, valMain)
		cleanupVal()
		if valErr == nil {
			break
		}

		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)

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

	// Snapshot existing live files so reload failure can restore them cleanly.
	prevFiles := snapshotExistingFiles(desiredFiles)
	for path, content := range desiredFiles {
		if err := WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			restoreFilesSnapshot(prevFiles)
			_ = os.RemoveAll(panelDir)
			_ = os.RemoveAll(originDir)
			a.recordFailure(meta, desiredHash, err, now)
			return res, err
		}
		a.applyGroup(path, group, false)
	}

	prevPanelsTarget, _ := ReadDirLink(ep.PanelsLink)
	prevOriginsTarget, _ := ReadDirLink(ep.OriginsLink)

	if err := SwapDirLinkAtomic(ep.OriginsLink, originDir); err != nil {
		restoreFilesSnapshot(prevFiles)
		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)
		a.recordFailure(meta, desiredHash, err, now)
		return res, err
	}
	a.applyGroup(ep.OriginsLink, group, false)

	if err := SwapDirLinkAtomic(ep.PanelsLink, panelDir); err != nil {
		if prevOriginsTarget != "" {
			_ = SwapDirLinkAtomic(ep.OriginsLink, prevOriginsTarget)
		} else {
			_ = RemoveDirLink(ep.OriginsLink)
		}
		restoreFilesSnapshot(prevFiles)
		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)
		a.recordFailure(meta, desiredHash, err, now)
		return res, err
	}
	a.applyGroup(ep.PanelsLink, group, false)

	dur, reloadErr := runner.Reload(ctx)
	if reloadErr != nil {
		restoreFilesSnapshot(prevFiles)
		if prevOriginsTarget != "" {
			_ = SwapDirLinkAtomic(ep.OriginsLink, prevOriginsTarget)
		} else {
			_ = RemoveDirLink(ep.OriginsLink)
		}
		if prevPanelsTarget != "" {
			_ = SwapDirLinkAtomic(ep.PanelsLink, prevPanelsTarget)
			if _, rbErr := runner.Reload(ctx); rbErr != nil {
				reloadErr = fmt.Errorf("%w (rollback reload also failed: %v)", reloadErr, rbErr)
			}
		} else {
			_ = RemoveDirLink(ep.PanelsLink)
		}
		_ = os.RemoveAll(panelDir)
		_ = os.RemoveAll(originDir)
		a.recordFailure(meta, desiredHash, reloadErr, now)
		return res, reloadErr
	}

	meta.CurrentGen = nextGen
	nowUTC := now.UTC()
	meta.LastApplyAt = &nowUTC
	meta.LastApplyError = ""
	meta.LastReloadDuration = dur.Milliseconds()
	meta.ConsecutiveFailures = 0
	meta.FailedDesiredHash = ""
	meta.NextRetryAt = nil

	pruneEngineGenerations(ep, nextGen)

	res.Applied = true
	res.Generation = nextGen
	res.ReloadDuration = dur
	res.SlowReloadWarn = dur > 10*time.Second
	return res, nil
}

func mergeEngineConfigMaps(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

type engineTemplateProvider interface {
	RenderMainConfig(globalFile, commonFile, userDir, panelsDir string) string
	PlaceholderFileName() string
	PlaceholderContent() string
	HealthFileName() string
	HealthContent() string
}

func engineTemplates(eng Engine) (placeholderName, placeholderBody, healthName, healthBody string) {
	if tp, ok := eng.(engineTemplateProvider); ok {
		return tp.PlaceholderFileName(), tp.PlaceholderContent(), tp.HealthFileName(), tp.HealthContent()
	}
	return PlaceholderAngieFileName, RenderPlaceholderAngie(), HealthAngieFileName, RenderHealthProbeAngie()
}

func (a *Applier) ensureEngineBaseDirs(ep EnginePaths, group string) error {
	for _, d := range []string{
		filepath.Dir(ep.MainConfig),
		ep.ManagedDir,
		ep.UserDir,
		ep.CertsDir,
		ep.ManualCertsDir,
	} {
		if strings.TrimSpace(d) == "" {
			continue
		}
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	placeholderName, placeholderBody, _, _ := engineTemplates(a.engine)
	userPlaceholder := filepath.Join(ep.UserDir, placeholderName)
	if _, err := os.Stat(userPlaceholder); errors.Is(err, os.ErrNotExist) {
		if err := WriteFileAtomic(userPlaceholder, []byte(placeholderBody), 0o644); err != nil {
			return err
		}
	}
	a.applyGroup(ep.ManagedDir, group, false)
	a.applyGroup(ep.UserDir, group, true)
	if ep.CertsDir != "" {
		a.applyGroup(ep.CertsDir, group, false)
	}
	return nil
}

func shouldEmitEngineHealthProbe(ep EnginePaths, placeholderName string, panels []PanelRenderArtifact) bool {
	for _, p := range panels {
		if HasNonCommentCaddyContent(string(p.CaddyBytes)) {
			return false
		}
	}
	if dirHasNonCommentFiles(ep.UserDir, ep.PanelExt, placeholderName) {
		return false
	}
	return true
}

func dirHasNonCommentFiles(dir, ext string, ignoreNames ...string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	ignored := make(map[string]struct{}, len(ignoreNames))
	for _, n := range ignoreNames {
		ignored[n] = struct{}{}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		if _, skip := ignored[e.Name()]; skip {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil && HasNonCommentCaddyContent(string(b)) {
			return true
		}
	}
	return false
}

func (a *Applier) compareEngineGeneration(ep EnginePaths, desiredFiles map[string]string, panels []PanelRenderArtifact) (drifted []string, identical bool) {
	keys := make([]string, 0, len(desiredFiles))
	for k := range desiredFiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, path := range keys {
		b, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(b, []byte(desiredFiles[path])) {
			if err == nil {
				drifted = append(drifted, path)
			}
			return drifted, false
		}
	}

	curPanelsDir, err := ReadDirLink(ep.PanelsLink)
	if err != nil || curPanelsDir == "" {
		return nil, false
	}
	curOriginsDir, err := ReadDirLink(ep.OriginsLink)
	if err != nil || curOriginsDir == "" {
		return nil, false
	}

	entries, err := os.ReadDir(curPanelsDir)
	if err != nil {
		return nil, false
	}
	placeholderName, placeholderBody, healthName, healthBody := engineTemplates(a.engine)
	expectedPanels := make(map[string][]byte, len(panels)+2)
	expectedPanels[placeholderName] = []byte(placeholderBody)
	if shouldEmitEngineHealthProbe(ep, placeholderName, panels) {
		expectedPanels[healthName] = []byte(healthBody)
	}
	expectedPEM := make(map[string][]byte, len(panels))
	for _, p := range panels {
		expectedPanels[p.PanelID+ep.PanelExt] = p.CaddyBytes
		if len(p.PEMBytes) > 0 {
			expectedPEM[p.PanelID+".pem"] = p.PEMBytes
		}
	}

	seenPanels := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		seenPanels[name] = true
		want, ok := expectedPanels[name]
		if !ok {
			return nil, false
		}
		got, readErr := os.ReadFile(filepath.Join(curPanelsDir, name))
		if readErr != nil || !bytes.Equal(got, want) {
			drifted = append(drifted, filepath.Join(curPanelsDir, name))
			return drifted, false
		}
	}
	if len(seenPanels) != len(expectedPanels) {
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

func (a *Applier) stageEngineGeneration(ep EnginePaths, panelDir, originDir, group string, panels []PanelRenderArtifact) error {
	if err := os.MkdirAll(panelDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", panelDir, err)
	}
	if err := os.MkdirAll(originDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", originDir, err)
	}
	placeholderName, placeholderBody, healthName, healthBody := engineTemplates(a.engine)
	if err := WriteFileAtomic(filepath.Join(panelDir, placeholderName), []byte(placeholderBody), 0o640); err != nil {
		return err
	}
	if shouldEmitEngineHealthProbe(ep, placeholderName, panels) {
		if err := WriteFileAtomic(filepath.Join(panelDir, healthName), []byte(healthBody), 0o640); err != nil {
			return err
		}
	}
	for _, p := range panels {
		if err := WriteFileAtomic(filepath.Join(panelDir, p.PanelID+ep.PanelExt), p.CaddyBytes, 0o640); err != nil {
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
	a.applyGroup(panelDir, group, true)
	a.applyGroup(originDir, group, true)
	return nil
}

func (a *Applier) stageEngineValidation(ep EnginePaths, nextGen int, panelDir, originDir string, desiredFiles map[string]string) (string, func(), error) {
	valPanelsDir := filepath.Join(ep.ManagedDir, fmt.Sprintf(".panels.validate.%d", nextGen))
	valGlobal := filepath.Join(ep.ManagedDir, fmt.Sprintf(".global.conf.validate.%d", nextGen))
	valCommon := filepath.Join(ep.ManagedDir, fmt.Sprintf(".common.conf.validate.%d", nextGen))
	valMain := filepath.Join(ep.ManagedDir, fmt.Sprintf(".%s.conf.validate.%d", a.engine.Name(), nextGen))

	cleanup := func() {
		_ = os.Remove(valMain)
		_ = os.Remove(valGlobal)
		_ = os.Remove(valCommon)
		_ = os.RemoveAll(valPanelsDir)
	}

	if err := WriteFileAtomic(valGlobal, []byte(desiredFiles[ep.GlobalFile]), 0o644); err != nil {
		return "", cleanup, err
	}
	if err := WriteFileAtomic(valCommon, []byte(desiredFiles[ep.CommonFile]), 0o644); err != nil {
		return "", cleanup, err
	}
	if err := os.MkdirAll(valPanelsDir, 0o750); err != nil {
		return "", cleanup, err
	}

	entries, err := os.ReadDir(panelDir)
	if err != nil {
		return "", cleanup, err
	}
	oldPrefix := filepath.ToSlash(ep.OriginsLink) + "/"
	newPrefix := filepath.ToSlash(originDir) + "/"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(panelDir, e.Name()))
		if err != nil {
			return "", cleanup, err
		}
		rewritten := strings.ReplaceAll(string(b), oldPrefix, newPrefix)
		if err := WriteFileAtomic(filepath.Join(valPanelsDir, e.Name()), []byte(rewritten), 0o640); err != nil {
			return "", cleanup, err
		}
	}

	mainContent := desiredFiles[ep.MainConfig]
	if tp, ok := a.engine.(engineTemplateProvider); ok {
		mainContent = tp.RenderMainConfig(valGlobal, valCommon, ep.UserDir, valPanelsDir)
	}
	if err := WriteFileAtomic(valMain, []byte(mainContent), 0o644); err != nil {
		return "", cleanup, err
	}
	return valMain, cleanup, nil
}

type fileBackup struct {
	existed bool
	data    []byte
}

func snapshotExistingFiles(desired map[string]string) map[string]fileBackup {
	snap := make(map[string]fileBackup, len(desired))
	for path := range desired {
		if b, err := os.ReadFile(path); err == nil {
			snap[path] = fileBackup{existed: true, data: b}
		} else {
			snap[path] = fileBackup{existed: false}
		}
	}
	return snap
}

func restoreFilesSnapshot(snap map[string]fileBackup) {
	for path, fb := range snap {
		if fb.existed {
			_ = WriteFileAtomic(path, fb.data, 0o644)
		} else {
			_ = os.Remove(path)
		}
	}
}

func nextEngineGenerationNumber(managedDir string, currentGen int) int {
	maxGen := currentGen
	entries, err := os.ReadDir(managedDir)
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

func pruneEngineGenerations(ep EnginePaths, keepCurrent int) {
	entries, err := os.ReadDir(ep.ManagedDir)
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
	for _, g := range gens[:len(gens)-generationsToKeep] {
		if g == keepCurrent {
			continue
		}
		_ = os.RemoveAll(ep.PanelGenDir(g))
		_ = os.RemoveAll(ep.OriginGenDir(g))
	}
}
