package edge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// PanelState persists the last-good snapshot, mass-deletion guard counters,
// 410 tracking, and per-host state for a single panel (/var/lib/nd-edge/state/<panel_id>.json).
type PanelState struct {
	PanelID   string `json:"panel_id"`
	PanelName string `json:"panel_name,omitempty"`

	LastGood     *RoutesResponse `json:"last_good,omitempty"`
	LastGoodETag string          `json:"last_good_etag,omitempty"`
	LastGoodAt   time.Time       `json:"last_good_at,omitempty"`

	// Candidate snapshot held by the mass-deletion guard (§3.4).
	PendingSnapshot  *RoutesResponse `json:"pending_snapshot,omitempty"`
	PendingETag      string          `json:"pending_etag,omitempty"`
	PendingCount     int             `json:"pending_count,omitempty"`
	PendingFirstSeen time.Time       `json:"pending_first_seen,omitempty"`

	// Consecutive 410 Gone tracking (E-API-4: 3 consecutive 410s spanning >= 10m).
	Consecutive410Count int       `json:"consecutive_410_count,omitempty"`
	First410At          time.Time `json:"first_410_at,omitempty"`

	// Poll / sync health.
	SyncState         string    `json:"sync_state"`
	LastSyncAttemptAt time.Time `json:"last_sync_attempt_at,omitempty"`
	LastSyncOKAt      time.Time `json:"last_sync_ok_at,omitempty"`
	LastError         string    `json:"last_error,omitempty"`

	// HostFirstIssuing tracks when a host first entered issuing state (for >15m cert_error).
	HostFirstIssuing map[string]time.Time `json:"host_first_issuing,omitempty"`

	// HostStates holds the latest computed HostStatus keyed by normalized host.
	HostStates map[string]HostStatus `json:"host_states,omitempty"`

	// Conflicts holds cross-panel host conflicts for this panel.
	Conflicts []ConflictStatus `json:"conflicts,omitempty"`

	// AdmissionTimestamps records recent new-host admissions for the 50/10m per-panel throttle.
	AdmissionTimestamps []time.Time `json:"admission_timestamps,omitempty"`

	// LastAppliedETag tracks the etag of this panel included in the latest successful apply.
	LastAppliedETag string `json:"last_applied_etag,omitempty"`
}

// NewPanelState returns an initialized PanelState for panelID.
func NewPanelState(panelID, panelName string) *PanelState {
	return &PanelState{
		PanelID:          panelID,
		PanelName:        panelName,
		SyncState:        SyncStatePending,
		HostFirstIssuing: make(map[string]time.Time),
		HostStates:       make(map[string]HostStatus),
	}
}

// LoadPanelState loads a panel state file.
// Per E-STATE-1, if the JSON is corrupt, the file is quarantined to
// <path>.corrupt.<ts>, a fresh state is returned, and a *CorruptFileError
// warning is returned as the non-fatal error so the caller can log it and
// refetch without touching Caddy files.
func LoadPanelState(path, panelID, panelName string, now time.Time) (*PanelState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewPanelState(panelID, panelName), nil
		}
		return nil, fmt.Errorf("read panel state %s: %w", path, err)
	}

	var st PanelState
	if err := json.Unmarshal(raw, &st); err != nil {
		q, qErr := quarantineCorruptFile(path, now)
		if qErr != nil {
			return NewPanelState(panelID, panelName), &CorruptFileError{Path: path, Err: fmt.Errorf("parse: %v (quarantine failed: %v)", err, qErr)}
		}
		return NewPanelState(panelID, panelName), &CorruptFileError{Path: path, Quarantine: q, Err: err}
	}

	if st.PanelID == "" {
		st.PanelID = panelID
	}
	if st.PanelName == "" {
		st.PanelName = panelName
	}
	if st.SyncState == "" {
		st.SyncState = SyncStatePending
	}
	if st.HostFirstIssuing == nil {
		st.HostFirstIssuing = make(map[string]time.Time)
	}
	if st.HostStates == nil {
		st.HostStates = make(map[string]HostStatus)
	}
	return &st, nil
}

// SavePanelState writes st atomically with mode 0600.
func SavePanelState(path string, st *PanelState) error {
	if st == nil {
		return errors.New("nil panel state")
	}
	return WriteJSONAtomic(path, st, 0o600)
}

// ApplyMeta persists generation tracking and failure-backoff state across restarts.
type ApplyMeta struct {
	CurrentGen          int         `json:"current_gen"`
	LastApplyAt         *time.Time  `json:"last_apply_at,omitempty"`
	LastApplyError      string      `json:"last_apply_error,omitempty"`
	LastReloadDuration  int64       `json:"last_reload_ms,omitempty"`
	ConsecutiveFailures int         `json:"consecutive_failures,omitempty"`
	FailedDesiredHash   string      `json:"failed_desired_hash,omitempty"`
	NextRetryAt         *time.Time  `json:"next_retry_at,omitempty"`
	ShieldAdmissions    []time.Time `json:"shield_admissions,omitempty"`
}

// LoadApplyMeta loads ApplyMeta or returns a zero ApplyMeta if missing/corrupt.
func LoadApplyMeta(path string) (*ApplyMeta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ApplyMeta{}, nil
		}
		return nil, err
	}
	var m ApplyMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return &ApplyMeta{}, &CorruptFileError{Path: path, Err: err}
	}
	return &m, nil
}

// SaveApplyMeta writes ApplyMeta atomically.
func SaveApplyMeta(path string, m *ApplyMeta) error {
	if m == nil {
		return errors.New("nil apply meta")
	}
	return WriteJSONAtomic(path, m, 0o600)
}
