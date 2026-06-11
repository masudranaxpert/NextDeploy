package handlers

import (
	"sync"
	"time"
)

const (
	VolRestoreStatusExtracting = "extracting"
	VolRestoreStatusCompleted  = "completed"
	VolRestoreStatusFailed     = "failed"
)

type VolumeRestoreJob struct {
	Mu             sync.Mutex
	ID             string
	Volume         string
	Status         string
	ErrMsg         string
	ExtractPercent int
}

func (j *VolumeRestoreJob) SetExtractPct(p int) {
	j.Mu.Lock()
	defer j.Mu.Unlock()
	if p > j.ExtractPercent {
		j.ExtractPercent = p
	}
}

func (j *VolumeRestoreJob) Finish(errMsg string) {
	j.Mu.Lock()
	defer j.Mu.Unlock()
	j.ExtractPercent = 100
	if errMsg != "" {
		j.Status = VolRestoreStatusFailed
		j.ErrMsg = errMsg
	} else {
		j.Status = VolRestoreStatusCompleted
	}
}

func (j *VolumeRestoreJob) Snapshot() (status, errMsg string, pct int, volume string) {
	j.Mu.Lock()
	defer j.Mu.Unlock()
	return j.Status, j.ErrMsg, j.ExtractPercent, j.Volume
}

func (p *Panel) PutVolRestoreJob(j *VolumeRestoreJob) {
	p.VolRestoreMu.Lock()
	defer p.VolRestoreMu.Unlock()
	if p.volRestoreJobs == nil {
		p.volRestoreJobs = make(map[string]*VolumeRestoreJob)
	}
	p.volRestoreJobs[j.ID] = j
}

func (p *Panel) GetVolRestoreJob(id string) *VolumeRestoreJob {
	p.VolRestoreMu.Lock()
	defer p.VolRestoreMu.Unlock()
	return p.volRestoreJobs[id]
}

func (p *Panel) ExpireVolRestoreJob(id string, after time.Duration) {
	time.AfterFunc(after, func() {
		p.VolRestoreMu.Lock()
		delete(p.volRestoreJobs, id)
		p.VolRestoreMu.Unlock()
	})
}
