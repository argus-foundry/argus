package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentGraduateGuards(t *testing.T) {
	const residentOnly = "is resident only"
	const notWorker = "target must be a live worker of the resident orchestrator, never itself"
	for _, tc := range []struct{ name, reason string }{
		{"unbound", residentOnly},
		{"spoof", residentOnly},
		// cwd naming the resident is refused centrally before dispatch.
		{"spoof cwd", "resident identity requires the resident's own bound connection"},
		// Naming the resident's task id in any task-id argument is refused centrally.
		{"spoof id", "resident identity requires the resident's own bound connection"},
		// Non-resident bound callers target a SIBLING so only boundResident's
		// IsResident check refuses (a worker's own binding would pass the
		// standing-binding lookup and the self-target checks).
		{"worker targets sibling", residentOnly},
		{"worker targets itself", residentOnly},
		{"ordinary coordinator", residentOnly},
		{"self", notWorker},
		{"cross orchestrator", notWorker},
		{"codex", "only Claude workers can graduate"},
		{"missing backend", "only Claude workers can graduate"},
		{"no session", "no resumable session id"},
		{"busy", "session is busy"},
		{"unconfigured", "resident graduation not configured"},
		{"success", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			w, role, b := residentTestWorker(t, d, o.ID, "work")
			w.Backend, w.SessionID = "claude", "conversation"
			testutil.NoError(t, d.Update(w))
			sib, _, sibBinding := residentTestWorker(t, d, o.ID, "sibling")
			sib.Backend, sib.SessionID = "claude", "conversation"
			testutil.NoError(t, d.Update(sib))
			s.SetHeraGraduationPreflight(func(*model.Task) error {
				// Guards AND preflight are covered by the mutation lock.
				locked := residentWorkerMutationMu.TryLock()
				if locked {
					residentWorkerMutationMu.Unlock()
				}
				testutil.False(t, locked)
				if tc.name == "busy" {
					return fmt.Errorf("session is busy")
				}
				return nil
			})
			caller, target := res.ID, "work"
			args := map[string]interface{}{}
			switch tc.name {
			case "unbound":
				caller = ""
			case "spoof":
				caller = "unknown"
			case "spoof cwd":
				caller = ""
				args["cwd"] = res.Worktree
			case "spoof id":
				caller = ""
				args["id"] = res.ID
			case "worker targets sibling":
				caller, target = w.ID, "sibling"
			case "worker targets itself":
				caller = w.ID
			case "ordinary coordinator":
				caller = seedCoordinator(t, s, d, "other", "/wt/coord").ID
			case "self":
				target = "resident"
			case "cross orchestrator":
				other, err := d.CreateHeraOrchestrator("elsewhere", "")
				testutil.NoError(t, err)
				_, r, _ := residentTestWorker(t, d, other.ID, "other-worker")
				target = fmt.Sprint(r.ID)
			case "codex":
				w.Backend = "codex"
				testutil.NoError(t, d.Update(w))
			case "missing backend":
				w.Backend = "missing"
				testutil.NoError(t, d.Update(w))
			case "no session":
				w.SessionID = ""
				testutil.NoError(t, d.Update(w))
			case "unconfigured":
				s.SetHeraGraduationPreflight(nil)
			}
			args["role"] = target
			raw, _ := json.Marshal(args)
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_graduate_worker", string(raw))
			if tc.reason == "" {
				testutil.False(t, isErr)
				testutil.Contains(t, text, "graduation pending restart")
				_, err := d.HeraLiveBindingByRole(role.ID)
				testutil.ErrorIs(t, err, db.ErrHeraNotFound)
				// Real graduation is unreachable by delete, even force.
				resp := s.toolHeraDeleteWorker(1, json.RawMessage(`{"role":"work","force":true}`), res.ID)
				testutil.True(t, callResult(t, resp).IsError)
				_, err = d.Get(w.ID)
				testutil.NoError(t, err)
				return
			}
			testutil.True(t, isErr)
			testutil.Contains(t, text, tc.reason)
			for _, want := range []*db.HeraBinding{b, sibBinding} {
				live, err := d.HeraLiveBindingByRole(want.RoleID)
				testutil.NoError(t, err)
				testutil.Equal(t, live.ID, want.ID)
			}
		})
	}
}

func TestGraduateSerializesConcurrentDelete(t *testing.T) {
	s, d, res, o := residentHeraFixture(t)
	w, _, _ := residentTestWorker(t, d, o.ID, "work")
	w.Backend, w.SessionID = "claude", "conversation"
	testutil.NoError(t, d.Update(w))
	s.SetHeraGraduationPreflight(func(*model.Task) error { return nil })
	// The lock must span the membership COMMIT itself, not just the guards:
	// TryLock inside the commit seam is deterministic evidence.
	commitLocked := false
	orig := heraGraduateCommit
	t.Cleanup(func() { heraGraduateCommit = orig })
	heraGraduateCommit = func(d *db.DB, bindingID int64) (*db.MoveHeraBindingResult, error) {
		if residentWorkerMutationMu.TryLock() {
			residentWorkerMutationMu.Unlock()
		} else {
			commitLocked = true
		}
		return orig(d, bindingID)
	}
	testutil.False(t, callResult(t, s.toolHeraGraduateWorker(1, json.RawMessage(`{"role":"work"}`), res.ID)).IsError)
	testutil.True(t, commitLocked)
	// After the commit, delete sees the graduated worker as gone, even forced.
	testutil.True(t, callResult(t, s.toolHeraDeleteWorker(2, json.RawMessage(`{"role":"work","force":true}`), res.ID)).IsError)
	live, err := d.HeraLiveBindingByTask(w.ID)
	testutil.NoError(t, err)
	role, err := d.HeraRole(live.RoleID)
	testutil.NoError(t, err)
	testutil.Equal(t, role.Kind, db.HeraKindCoordinator)
	task, err := d.Get(w.ID)
	testutil.NoError(t, err)
	testutil.False(t, task.Archived)
}
