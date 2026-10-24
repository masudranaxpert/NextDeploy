package phphosting

import "time"

// Provision tracks a long running site setup (WordPress install).
type Provision struct {
	AppID     string            `json:"app_id"`
	Status    string            `json:"status"` // running | done | failed
	Log       string            `json:"log"`
	Error     string            `json:"error,omitempty"`
	Secrets   map[string]string `json:"secrets,omitempty"` // shown once
	StartedAt time.Time         `json:"started_at"`
}

func (m *Manager) ProvisionStart(appID string) *Provision {
	m.provMu.Lock()
	defer m.provMu.Unlock()
	p := &Provision{AppID: appID, Status: "running", StartedAt: time.Now()}
	m.prov[appID] = p
	return p
}

func (m *Manager) ProvisionLog(p *Provision, line string) {
	m.provMu.Lock()
	p.Log += line + "\n"
	m.provMu.Unlock()
}

func (m *Manager) ProvisionFinish(p *Provision, err error, secrets map[string]string) {
	m.provMu.Lock()
	defer m.provMu.Unlock()
	if err != nil {
		p.Status, p.Error = "failed", err.Error()
		return
	}
	p.Status, p.Secrets = "done", secrets
}

// ProvisionState returns the provisioning state; secrets are handed out only once.
func (m *Manager) ProvisionState(appID string, consumeSecrets bool) (Provision, bool) {
	m.provMu.Lock()
	defer m.provMu.Unlock()
	p, ok := m.prov[appID]
	if !ok {
		return Provision{}, false
	}
	cp := *p
	if consumeSecrets {
		p.Secrets = nil
	} else {
		cp.Secrets = nil
	}
	return cp, true
}
