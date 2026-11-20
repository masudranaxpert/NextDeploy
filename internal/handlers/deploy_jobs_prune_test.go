package handlers

import (
	"testing"
	"time"
)

func TestPruneDeployJobsKeepsRunningAndLatest(t *testing.T) {
	now := time.Now()
	old := now.Add(-5 * time.Hour)
	jobs := map[string]*DeployJobRecord{
		"a1": {JobID: "a1", AppID: "a", CreatedAt: old, FinishedAt: old},
		"a2": {JobID: "a2", AppID: "a", CreatedAt: old.Add(time.Minute), FinishedAt: old.Add(time.Minute)},
		"b1": {JobID: "b1", AppID: "b", CreatedAt: old, Running: true},
		"c1": {JobID: "c1", AppID: "c", CreatedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour)},
		"c2": {JobID: "c2", AppID: "c", CreatedAt: now, FinishedAt: now},
	}
	pruneDeployJobsLocked(jobs, now)
	for _, gone := range []string{"a1"} {
		if _, ok := jobs[gone]; ok {
			t.Fatalf("%s should be pruned", gone)
		}
	}
	for _, kept := range []string{"a2", "b1", "c1", "c2"} {
		if _, ok := jobs[kept]; !ok {
			t.Fatalf("%s must be kept", kept)
		}
	}
}
