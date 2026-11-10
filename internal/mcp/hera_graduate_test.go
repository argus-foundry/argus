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
	for _, tc := range []string{"unbound", "spoof", "worker", "self", "cross orchestrator", "codex", "missing backend", "no session", "busy", "unconfigured", "success"} {
		t.Run(tc, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			w, role, b := residentTestWorker(t, d, o.ID, "work")
			w.Backend, w.SessionID = "claude", "conversation"
			testutil.NoError(t, d.Update(w))
			s.SetHeraGraduationPreflight(func(*model.Task) error {
				// Guards AND preflight are covered by the mutation lock.
				locked := residentWorkerMutationMu.TryLock()
				if locked {
					residentWorkerMutationMu.Unlock()
				}
				testutil.False(t, locked)
				if tc == "busy" {
					return fmt.Errorf("session is busy")
				}
				return nil
			})
			caller, target := res.ID, "work"
			switch tc {
			case "unbound":
				caller = ""
			case "spoof":
				caller = "unknown"
			case "worker":
				caller = w.ID
			case "self":
				target = "resident"
			case "cross orchestrator":
				other, err := d.CreateHeraOrchestrator("other", "")
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
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_graduate_worker", fmt.Sprintf(`{"role":%q,"cwd":%q,"id":%q}`, target, res.Worktree, res.ID))
			testutil.Equal(t, isErr, tc != "success")
			if tc == "success" {
				testutil.Contains(t, text, "graduation pending restart")
				_, err := d.HeraLiveBindingByRole(role.ID)
				testutil.ErrorIs(t, err, db.ErrHeraNotFound)
				// Real graduation is unreachable by delete, even force.
				resp := s.toolHeraDeleteWorker(1, json.RawMessage(`{"role":"work","force":true}`), res.ID)
				testutil.True(t, callResult(t, resp).IsError)
				_, err = d.Get(w.ID)
				testutil.NoError(t, err)
			} else {
				live, err := d.HeraLiveBindingByRole(b.RoleID)
				testutil.NoError(t, err)
				testutil.Equal(t, live.ID, b.ID)
			}
		})
	}
}

func TestGraduateSerializesConcurrentDelete(t *testing.T) {
	s, d, res, o := residentHeraFixture(t)
	w, _, _ := residentTestWorker(t, d, o.ID, "work")
	w.Backend, w.SessionID = "claude", "conversation"
	testutil.NoError(t, d.Update(w))
	entered, release := make(chan struct{}), make(chan struct{})
	s.SetHeraGraduationPreflight(func(*model.Task) error { close(entered); <-release; return nil })
	gradDone, deleteDone := make(chan *Response, 1), make(chan *Response, 1)
	go func() { gradDone <- s.toolHeraGraduateWorker(1, json.RawMessage(`{"role":"work"}`), res.ID) }()
	<-entered
	go func() {
		deleteDone <- s.toolHeraDeleteWorker(2, json.RawMessage(`{"role":"work","force":true}`), res.ID)
	}()
	close(release)
	testutil.False(t, callResult(t, <-gradDone).IsError)
	testutil.True(t, callResult(t, <-deleteDone).IsError)
	live, err := d.HeraLiveBindingByTask(w.ID)
	testutil.NoError(t, err)
	role, err := d.HeraRole(live.RoleID)
	testutil.NoError(t, err)
	testutil.Equal(t, role.Kind, db.HeraKindCoordinator)
	task, err := d.Get(w.ID)
	testutil.NoError(t, err)
	testutil.False(t, task.Archived)
}
