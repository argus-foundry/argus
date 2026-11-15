package api

import (
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/sanitize"
	"github.com/argus-foundry/argus/internal/uxlog"
)

type residentApprovalSignals struct {
	mu                            sync.Mutex
	enabled                       func(string) bool // installed before the watcher starts
	task, generation              string
	answeredUntil, candidateSince time.Time
}

// SetResidentPermissionHook configures detection only for launches carrying the
// new hook. Existing/old supervisor sessions keep native waiting behavior.
func (s *Server) SetResidentPermissionHook(enabled func(string) bool) {
	s.residentApproval.enabled = enabled
}

func (s *Server) ResidentPermissionAnswered(id string) {
	a := &s.residentApproval
	a.mu.Lock()
	defer a.mu.Unlock()
	a.task, a.generation = id, agent.SessionBridgeGeneration(s.runner.Get(id))
	a.answeredUntil = time.Now().Add(15 * time.Second)
}

func (s *Server) suppressResidentPermission(id, generation string, permission bool, now time.Time) bool {
	a := &s.residentApproval
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.task != id || a.generation != generation {
		a.task, a.generation, a.answeredUntil, a.candidateSince = id, generation, time.Time{}, time.Time{}
	}
	if !permission {
		a.candidateSince = time.Time{}
		return false
	}
	if now.Before(a.answeredUntil) {
		return true
	}
	if a.candidateSince.IsZero() {
		a.candidateSince = now
	}
	return now.Sub(a.candidateSince) < 10*time.Second
}

func (s *Server) filterResidentApprovalWaiting(state *idleWatcherState, needs map[string]bool, tailOf func(string) []byte) {
	if s.residentApproval.enabled == nil || s.db == nil {
		return
	}
	r, err := s.db.ResidentTask()
	if err != nil || r == nil || !s.residentApproval.enabled(r.ID) {
		return
	}
	permission := false
	if st, live := s.bridgeState(r.ID, time.Now()); live {
		permission = st.NeedsInputKind == bridge.KindPermission
	} else {
		text := sanitize.StripANSI(string(tailOf(r.ID)))
		permission = strings.Contains(text, "Bash command") || strings.Contains(text, "Permission rule") || strings.Contains(text, "requires permission") || strings.Contains(text, "Do you want to proceed?")
	}
	if s.suppressResidentPermission(r.ID, agent.SessionBridgeGeneration(s.runner.Get(r.ID)), permission, time.Now()) {
		delete(needs, r.ID)
		// The generic detector is sticky. Never retain the flashed prompt as an
		// outstanding question after the hook's answer dismissed it.
		delete(state.needsInputSince, r.ID)
		delete(state.scraperNeeds, r.ID)
		uxlog.Log("[resident-approval] transient permission waiting suppressed task=%s", r.ID)
	}
}
