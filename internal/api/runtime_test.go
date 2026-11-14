package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func runtimeGet(t *testing.T, srv *Server, id string) (map[string]any, string) {
	t.Helper()
	w := doReq(srv.routes(), "GET", "/api/tasks/"+id+"/runtime", "", nil)
	testutil.Equal(t, w.Code, http.StatusOK)
	body := decodeBody(t, w)
	return body, body["revision"].(string)
}

func runtimePatch(srv *Server, id, body string, hdr map[string]string) *httptest.ResponseRecorder {
	return doReq(srv.routes(), "PATCH", "/api/tasks/"+id+"/runtime", body, hdr)
}

func ifMatch(rev string) map[string]string { return map[string]string{"If-Match": `"` + rev + `"`} }

func master(h map[string]string) map[string]string {
	h["X-Argus-Auth"] = "master"
	return h
}

func runtimeChangedEvents(sink *recordingSink) []map[string]any {
	var out []map[string]any
	for _, ev := range sink.events() {
		if ev.Type != model.EventTypeTaskRuntimeChanged {
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(ev.Payload, &p)
		out = append(out, p)
	}
	return out
}

func addStopped(t *testing.T, d *db.DB, mut func(*model.Task)) *model.Task {
	t.Helper()
	task := &model.Task{Name: "rt", Status: model.StatusInReview, Backend: "claude", Prompt: "orig prompt", Branch: "br", Worktree: t.TempDir()}
	if mut != nil {
		mut(task)
	}
	testutil.NoError(t, d.Add(task))
	return task
}

func TestGetTaskRuntime(t *testing.T) {
	t.Run("missing task", func(t *testing.T) {
		srv, _ := testServer(t)
		w := doReq(srv.routes(), "GET", "/api/tasks/nope/runtime", "", nil)
		testutil.Equal(t, w.Code, http.StatusNotFound)
	})
	t.Run("stopped task shape", func(t *testing.T) {
		srv, d := testServer(t)
		task := addStopped(t, d, func(x *model.Task) { x.Model = "opus"; x.SandboxOverride = "enabled" })
		body, rev := runtimeGet(t, srv, task.ID)
		testutil.Equal(t, body["task_id"], any(task.ID))
		testutil.Equal(t, body["live"], any(false))
		testutil.Equal(t, body["busy"], any(false))
		testutil.DeepEqual(t, body["available_apply"], any([]any{"next_resume", "restart"}))
		testutil.DeepEqual(t, body["runtime"], any(map[string]any{
			"backend": "claude", "model": "opus", "sandbox_override": "enabled", "prompt": "orig prompt",
		}))
		testutil.True(t, rev != "")
	})
	t.Run("live task only offers restart", func(t *testing.T) {
		srv, d := testServer(t)
		task := &model.Task{Name: "live", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		startCat(t, srv, d, task)
		body, _ := runtimeGet(t, srv, task.ID)
		testutil.Equal(t, body["live"], any(true))
		testutil.Equal(t, body["busy"], any(true))
		testutil.DeepEqual(t, body["available_apply"], any([]any{"restart"}))
	})
	t.Run("revision is stable until a runtime field changes", func(t *testing.T) {
		srv, d := testServer(t)
		task := addStopped(t, d, nil)
		_, r1 := runtimeGet(t, srv, task.ID)
		testutil.NoError(t, d.SetStatus(task.ID, model.StatusComplete))
		_, r2 := runtimeGet(t, srv, task.ID)
		testutil.Equal(t, r1, r2)
		w := runtimePatch(srv, task.ID, `{"model":"opus","apply":"next_resume"}`, ifMatch(r1))
		testutil.Equal(t, w.Code, http.StatusOK)
		_, r3 := runtimeGet(t, srv, task.ID)
		testutil.True(t, r3 != r1)
	})
}

func TestPatchTaskRuntime_Rejections(t *testing.T) {
	setup := func(t *testing.T) (*Server, *db.DB, *model.Task, string) {
		srv, d := testServer(t)
		task := addStopped(t, d, func(x *model.Task) { x.Model = "opus" })
		_, rev := runtimeGet(t, srv, task.ID)
		return srv, d, task, rev
	}
	unchanged := func(t *testing.T, d *db.DB, id string) {
		t.Helper()
		got, err := d.Get(id)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Model, "opus")
		testutil.Equal(t, got.Prompt, "orig prompt")
		testutil.Equal(t, got.Backend, "claude")
		testutil.Equal(t, got.SandboxOverride, "")
	}
	cases := []struct {
		name string
		body string
		hdr  func(rev string) map[string]string
		want int
		code string
	}{
		{"bad json", `{`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_body"},
		{"missing apply", `{"model":"sonnet"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_apply"},
		{"bad apply", `{"model":"sonnet","apply":"now"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_apply"},
		{"no fields", `{"apply":"next_resume"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "no_changes"},
		{"unknown backend", `{"backend":"ghost","apply":"next_resume"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_runtime"},
		{"blank backend", `{"backend":"","apply":"next_resume"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_runtime"},
		{"bad override", `{"sandbox_override":"maybe","apply":"next_resume"}`, func(r string) map[string]string { return master(ifMatch(r)) }, 400, "invalid_runtime"},
		{"bad model chars", `{"model":"a b; rm","apply":"next_resume"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_runtime"},
		{"model too long", `{"model":"` + strings.Repeat("a", 201) + `","apply":"next_resume"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_runtime"},
		{"blank prompt", `{"prompt":"  ","apply":"next_resume"}`, func(r string) map[string]string { return ifMatch(r) }, 400, "invalid_runtime"},
		{"sandbox needs master", `{"sandbox_override":"enabled","apply":"next_resume"}`, func(r string) map[string]string { return ifMatch(r) }, 403, "master_required"},
		{"missing If-Match", `{"model":"sonnet","apply":"next_resume"}`, func(string) map[string]string { return map[string]string{} }, 428, "if_match_required"},
		{"stale If-Match", `{"model":"sonnet","apply":"next_resume"}`, func(string) map[string]string { return ifMatch("deadbeef") }, 412, "revision_stale"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, d, task, rev := setup(t)
			sink := installAPISink(t)
			w := runtimePatch(srv, task.ID, tc.body, tc.hdr(rev))
			testutil.Equal(t, w.Code, tc.want)
			testutil.Equal(t, decodeBody(t, w)["code"], any(tc.code))
			unchanged(t, d, task.ID)
			testutil.Equal(t, len(runtimeChangedEvents(sink)), 0)
		})
	}

	t.Run("stale response carries the current revision", func(t *testing.T) {
		srv, _, task, rev := setup(t)
		w := runtimePatch(srv, task.ID, `{"model":"sonnet","apply":"next_resume"}`, ifMatch("old"))
		testutil.Equal(t, decodeBody(t, w)["revision"], any(rev))
	})
	t.Run("missing task", func(t *testing.T) {
		srv, _ := testServer(t)
		w := runtimePatch(srv, "nope", `{"model":"x","apply":"next_resume"}`, ifMatch("r"))
		testutil.Equal(t, w.Code, http.StatusNotFound)
	})
	t.Run("family without model support rejects a model", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.SetBackend("plain", config.Backend{Command: "mycli"}))
		task := addStopped(t, d, func(x *model.Task) { x.Backend = "plain" })
		_, rev := runtimeGet(t, srv, task.ID)
		w := runtimePatch(srv, task.ID, `{"model":"gpt-6","apply":"next_resume"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusBadRequest)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Model, "")
	})
	t.Run("backend family change with saved session is a conflict", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.SetBackend("cx", config.Backend{Command: "codex"}))
		task := addStopped(t, d, func(x *model.Task) { x.SessionID = "sess-1" })
		_, rev := runtimeGet(t, srv, task.ID)
		w := runtimePatch(srv, task.ID, `{"backend":"cx","apply":"next_resume"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Equal(t, decodeBody(t, w)["code"], any("conversation_incompatible"))
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Backend, "claude")
		testutil.Equal(t, got.SessionID, "sess-1")
	})
	t.Run("restart preflight rejects an unresumable saved session without stopping", func(t *testing.T) {
		srv, d := testServer(t)
		t.Setenv("HOME", t.TempDir())
		task := addStopped(t, d, func(x *model.Task) { x.SessionID = "no-such-transcript" })
		_, rev := runtimeGet(t, srv, task.ID)
		w := runtimePatch(srv, task.ID, `{"model":"sonnet","apply":"restart"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Equal(t, decodeBody(t, w)["code"], any("resume_unavailable"))
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Model, "")
		testutil.Equal(t, got.SessionID, "no-such-transcript")
		testutil.Equal(t, got.Status, model.StatusInReview)
	})
}

func TestPatchTaskRuntime_NextResume(t *testing.T) {
	t.Run("persists, preserves unrelated fields, emits events without prompt text", func(t *testing.T) {
		srv, d := testServer(t)
		task := addStopped(t, d, func(x *model.Task) { x.SessionID = "sess-9"; x.Pinned = true })
		_, rev := runtimeGet(t, srv, task.ID)
		sink := installAPISink(t)

		w := runtimePatch(srv, task.ID, `{"model":"opus","prompt":"secret new prompt","sandbox_override":"disabled","apply":"next_resume"}`, master(ifMatch(rev)))
		testutil.Equal(t, w.Code, http.StatusOK)
		body := decodeBody(t, w)
		testutil.Equal(t, body["apply"], any("next_resume"))
		testutil.Equal(t, body["started"], any(false))
		testutil.Equal(t, body["live"], any(false))
		testutil.Equal(t, body["session_id"], any("sess-9"))
		testutil.DeepEqual(t, body["changed_fields"], any([]any{"model", "sandbox_override", "prompt"}))
		testutil.True(t, body["revision"] != any(rev))

		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Model, "opus")
		testutil.Equal(t, got.Prompt, "secret new prompt")
		testutil.Equal(t, got.SandboxOverride, "disabled")
		testutil.Equal(t, got.Name, "rt")
		testutil.Equal(t, got.Branch, "br")
		testutil.Equal(t, got.Worktree, task.Worktree)
		testutil.Equal(t, got.SessionID, "sess-9")
		testutil.Equal(t, got.Pinned, true)
		testutil.Equal(t, got.Status, model.StatusInReview)

		evs := runtimeChangedEvents(sink)
		testutil.Equal(t, len(evs), 1)
		testutil.Equal(t, evs[0]["apply"], any("next_resume"))
		testutil.Equal(t, evs[0]["started"], any(false))
		testutil.DeepEqual(t, evs[0]["changed_fields"], any([]any{"model", "sandbox_override", "prompt"}))
		for _, ev := range sink.events() {
			testutil.False(t, strings.Contains(string(ev.Payload), "secret new prompt"))
		}
		upd := updatedEvents(t, sink)
		testutil.Equal(t, len(upd), 1)
		testutil.DeepEqual(t, upd[0], []string{"runtime"})
	})
	t.Run("no-op change writes nothing and emits nothing", func(t *testing.T) {
		srv, d := testServer(t)
		task := addStopped(t, d, func(x *model.Task) { x.Model = "opus" })
		_, rev := runtimeGet(t, srv, task.ID)
		sink := installAPISink(t)
		w := runtimePatch(srv, task.ID, `{"model":"opus","apply":"next_resume"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, decodeBody(t, w)["revision"], any(rev))
		testutil.Equal(t, len(sink.events()), 0)
	})
	t.Run("same-family backend change without a session is allowed", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.SetBackend("claude2", config.Backend{Command: "claude --foo"}))
		task := addStopped(t, d, nil)
		_, rev := runtimeGet(t, srv, task.ID)
		w := runtimePatch(srv, task.ID, `{"backend":"claude2","apply":"next_resume"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusOK)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Backend, "claude2")
	})
	t.Run("stale revision after a concurrent edit changes nothing", func(t *testing.T) {
		srv, d := testServer(t)
		task := addStopped(t, d, nil)
		_, rev := runtimeGet(t, srv, task.ID)
		testutil.Equal(t, runtimePatch(srv, task.ID, `{"model":"a1","apply":"next_resume"}`, ifMatch(rev)).Code, http.StatusOK)
		w := runtimePatch(srv, task.ID, `{"model":"b2","apply":"next_resume"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusPreconditionFailed)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Model, "a1")
	})
	t.Run("live session conflicts and is untouched", func(t *testing.T) {
		srv, d := testServer(t)
		task := &model.Task{Name: "live", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		sess := startCat(t, srv, d, task)
		_, rev := runtimeGet(t, srv, task.ID)
		w := runtimePatch(srv, task.ID, `{"prompt":"p2","apply":"next_resume"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Equal(t, decodeBody(t, w)["code"], any("session_live"))
		testutil.True(t, sess.Alive())
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Prompt, "")
	})
}

func TestPatchTaskRuntime_Restart(t *testing.T) {
	t.Run("busy live restart without force is a conflict before stopping", func(t *testing.T) {
		srv, d := testServer(t)
		task := &model.Task{Name: "busy", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		sess := startCat(t, srv, d, task)
		_, rev := runtimeGet(t, srv, task.ID)
		sink := installAPISink(t)
		w := runtimePatch(srv, task.ID, `{"prompt":"p2","apply":"restart"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Equal(t, decodeBody(t, w)["code"], any("task_busy"))
		testutil.True(t, sess.Alive())
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Prompt, "")
		testutil.Equal(t, len(runtimeChangedEvents(sink)), 0)
	})
	t.Run("forced live restart persists and reports actual session state", func(t *testing.T) {
		srv, d := testServer(t)
		task := &model.Task{Name: "live", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		startCat(t, srv, d, task)
		testutil.NoError(t, d.SetStatus(task.ID, model.StatusInReview))
		_, rev := runtimeGet(t, srv, task.ID)
		sink := installAPISink(t)
		w := runtimePatch(srv, task.ID, `{"prompt":"p2","apply":"restart","force":true}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusOK)
		body := decodeBody(t, w)
		testutil.Equal(t, body["apply"], any("restart"))
		testutil.Equal(t, body["started"], any(true))
		testutil.Equal(t, body["live"], any(true))
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Prompt, "p2")
		testutil.Equal(t, got.Status, model.StatusInProgress)
		testutil.Equal(t, body["revision"], any(runtimeRevision(got)))
		evs := runtimeChangedEvents(sink)
		testutil.Equal(t, len(evs), 1)
		testutil.Equal(t, evs[0]["started"], any(true))
		t.Cleanup(func() { _ = srv.runner.Stop(task.ID) })
	})
	t.Run("stopped restart starts the session", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.SetBackend("sh-sleep", config.Backend{Command: "sleep 30"}))
		task := addStopped(t, d, func(x *model.Task) { x.Backend = "sh-sleep" })
		_, rev := runtimeGet(t, srv, task.ID)
		w := runtimePatch(srv, task.ID, `{"prompt":"p2","apply":"restart"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusOK)
		body := decodeBody(t, w)
		testutil.Equal(t, body["started"], any(true))
		testutil.Equal(t, body["live"], any(true))
		testutil.True(t, srv.runner.Get(task.ID) != nil)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Status, model.StatusInProgress)
		testutil.Equal(t, got.Prompt, "p2")
		t.Cleanup(func() { _ = srv.runner.Stop(task.ID) })
	})
	t.Run("start failure after persist reports the partial outcome", func(t *testing.T) {
		srv, d := testServer(t)
		task := addStopped(t, d, func(x *model.Task) { x.Worktree = "/nonexistent/argus-test-worktree" })
		_, rev := runtimeGet(t, srv, task.ID)
		sink := installAPISink(t)
		w := runtimePatch(srv, task.ID, `{"prompt":"p2","apply":"restart"}`, ifMatch(rev))
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		body := decodeBody(t, w)
		testutil.Equal(t, body["error"], any("restart failed"))
		testutil.Equal(t, body["code"], any("restart_failed"))
		testutil.Equal(t, body["settings_applied"], any(true))
		testutil.Equal(t, body["started"], any(false))
		testutil.Equal(t, body["live"], any(false))
		testutil.Equal(t, body["rolled_back"], any(false))
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Prompt, "p2")
		testutil.Equal(t, body["revision"], any(runtimeRevision(got)))
		evs := runtimeChangedEvents(sink)
		testutil.Equal(t, len(evs), 1)
		testutil.Equal(t, evs[0]["started"], any(false))
		testutil.True(t, srv.runner.Get(task.ID) == nil)
	})
}

func TestWriteRestartFailed(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	t.Run("rolled back shape", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.writeRestartFailed(w, task.ID, false, true, nil)
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		body := decodeBody(t, w)
		testutil.Equal(t, body["settings_applied"], any(false))
		testutil.Equal(t, body["rolled_back"], any(true))
		_, hasDetail := body["detail"]
		testutil.False(t, hasDetail)
	})
	t.Run("unknown task omits revision", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.writeRestartFailed(w, "ghost", true, false, http.ErrAbortHandler)
		body := decodeBody(t, w)
		_, hasRev := body["revision"]
		testutil.False(t, hasRev)
		testutil.Equal(t, body["detail"], any(http.ErrAbortHandler.Error()))
	})
}

func TestNormalizeIfMatch(t *testing.T) {
	for in, want := range map[string]string{`"abc"`: "abc", `W/"abc"`: "abc", ` abc `: "abc", "abc": "abc"} {
		testutil.Equal(t, normalizeIfMatch(in), want)
	}
}

func TestPatchTaskRuntime_ReviveLaunchErrors(t *testing.T) {
	for _, st := range []db.HeraRoleStatusValue{db.HeraStatusDone, db.HeraStatusFailed} {
		for _, ambiguous := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ambiguous=%t", st, ambiguous), func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				srv, d := testServer(t)
				task, role := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
				testutil.NoError(t, d.UpsertHeraRoleStatus(role, st))
				testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
				before, err := d.HeraRoleStatusFor(role)
				testutil.NoError(t, err)
				launchErr := errors.New("definite launch failure")
				if ambiguous {
					launchErr = fmt.Errorf("supervisor timeout: %w", agent.ErrStartAmbiguous)
				}
				runner := &guardRunner{SessionRunner: srv.runner, startErr: launchErr}
				srv.runner = runner
				_, rev := runtimeGet(t, srv, task.ID)
				w := runtimePatch(srv, task.ID, `{"prompt":"updated prompt","apply":"restart","force_revive":true}`, ifMatch(rev))
				testutil.Equal(t, w.Code, http.StatusInternalServerError)
				body := decodeBody(t, w)
				testutil.Equal(t, body["code"], any("restart_failed"))
				testutil.Equal(t, body["settings_applied"], any(true))
				got, err := d.Get(task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, got.Prompt, "updated prompt")
				after, err := d.HeraRoleStatusFor(role)
				testutil.NoError(t, err)
				if ambiguous {
					testutil.Equal(t, after.Status, db.HeraStatusWorking)
					testutil.Equal(t, readyMeta(t, d, task.ID), "false")
				} else {
					testutil.DeepEqual(t, after, before)
					testutil.Equal(t, readyMeta(t, d, task.ID), "true")
				}
				testutil.Equal(t, runner.calls, 1)
			})
		}
	}
}
