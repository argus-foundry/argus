package api

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/uxlog"
)

type residentApprovalSignals struct {
	mu                                     sync.Mutex
	enabled                                func(string) bool
	task, generation, dialog, answeredCall string
	candidateSince                         time.Time
}

// SetResidentPermissionHook configures detection only for launches carrying the
// new hook. Existing/old supervisor sessions keep native waiting behavior.
func (s *Server) SetResidentPermissionHook(enabled func(string) bool) {
	s.residentApproval.mu.Lock()
	defer s.residentApproval.mu.Unlock()
	s.residentApproval.enabled = enabled
}

func (s *Server) ResidentPermissionAnswered(id, tool string, input json.RawMessage) {
	a := &s.residentApproval
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.task != id || a.generation != agent.SessionBridgeGeneration(s.runner.Get(id)) {
		return
	}
	label := tool
	if tool == "Bash" {
		var fields struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(input, &fields) != nil {
			return
		}
		label = fields.Command
	}
	// Only the currently observed chooser can be marked answered. An unrelated
	// subagent request never starts or extends a suppression interval.
	if label != "" && a.dialog != "" && strings.Contains(a.dialog, label) {
		a.answeredCall = a.dialog
	}
}

func (s *Server) suppressResidentPermission(id, generation, dialog string, now time.Time) bool {
	a := &s.residentApproval
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.task != id || a.generation != generation {
		a.task, a.generation, a.dialog, a.answeredCall, a.candidateSince = id, generation, "", "", time.Time{}
	}
	if dialog == "" {
		a.candidateSince = time.Time{}
		a.dialog, a.answeredCall = "", ""
		return false
	}
	if a.candidateSince.IsZero() {
		a.candidateSince = now
		a.dialog = dialog
	}
	// Changing chooser content does not reset the episode's hard deadline.
	if dialog != a.dialog {
		return false
	}
	limit := 10 * time.Second
	if a.answeredCall == dialog {
		limit = 15 * time.Second
	}
	return now.Sub(a.candidateSince) < limit
}

func (s *Server) filterResidentApprovalWaiting(state *idleWatcherState, needs map[string]bool, tailOf func(string) []byte) {
	s.residentApproval.mu.Lock()
	enabled := s.residentApproval.enabled
	s.residentApproval.mu.Unlock()
	if enabled == nil || s.db == nil {
		return
	}
	r, err := s.db.ResidentTask()
	if err != nil || r == nil || !enabled(r.ID) {
		return
	}
	generation := agent.SessionBridgeGeneration(s.runner.Get(r.ID))
	dialog := ""
	if st, live := s.bridgeState(r.ID, time.Now()); live {
		if st.NeedsInputKind == bridge.KindPermission {
			cols, rows := sessionScreenSize(r.ID)
			dialog = agent.NewTailView(state.screen, tailOf(r.ID), cols, rows).PermissionPromptKey()
		}
	} else {
		cols, rows := sessionScreenSize(r.ID)
		dialog = agent.NewTailView(state.screen, tailOf(r.ID), cols, rows).PermissionPromptKey()
	}
	if s.suppressResidentPermission(r.ID, generation, dialog, time.Now()) {
		delete(needs, r.ID)
		// The generic detector is sticky. Never retain the flashed prompt as an
		// outstanding question after the hook's answer dismissed it.
		delete(state.needsInputSince, r.ID)
		delete(state.scraperNeeds, r.ID)
		uxlog.Log("[resident-approval] transient permission waiting suppressed task=%s", r.ID)
	}
}
