package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
	return m
}

func doReq(mux http.Handler, method, url, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := authedReq(method, url, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func startCat(t *testing.T, srv *Server, d *db.DB, task *model.Task) agent.SessionHandle {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if testing.Short() {
		t.Skip("starts a real PTY-backed process")
	}
	testutil.NoError(t, d.SetBackend("sh-sleep", config.Backend{Command: "sleep 30"}))
	task.Backend = "sh-sleep"
	task.Worktree = t.TempDir()
	testutil.NoError(t, d.Update(task))
	sess, err := srv.runner.Start(task, d.Config(), 24, 80, false)
	testutil.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.runner.Stop(task.ID)
		<-sess.Done()
	})
	return sess
}

func TestTaskLocker(t *testing.T) {
	t.Run("serializes and releases entries", func(t *testing.T) {
		var l taskLocker
		var inside, maxInside int32
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				unlock := l.Lock("a")
				n := atomic.AddInt32(&inside, 1)
				if n > atomic.LoadInt32(&maxInside) {
					atomic.StoreInt32(&maxInside, n)
				}
				time.Sleep(time.Millisecond)
				atomic.AddInt32(&inside, -1)
				unlock()
			}()
		}
		wg.Wait()
		testutil.Equal(t, maxInside, int32(1))
		testutil.Equal(t, l.size(), 0)
	})
	t.Run("different ids do not block and unlock is idempotent", func(t *testing.T) {
		var l taskLocker
		u1 := l.Lock("a")
		u2 := l.Lock("b")
		u1()
		u1()
		u2()
		testutil.Equal(t, l.size(), 0)
	})
}

func TestWriteInput_ArchivedFence(t *testing.T) {
	t.Run("unknown task is task_not_found", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		srv, _ := testServer(t)
		w := doReq(srv.routes(), "POST", "/api/tasks/missing/input", "hi", nil)
		testutil.Equal(t, w.Code, http.StatusNotFound)
		testutil.Equal(t, decodeBody(t, w)["code"], any("task_not_found"))
	})

	t.Run("archived task without session is task_archived", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		srv, d := testServer(t)
		task := &model.Task{Name: "a", Status: model.StatusInProgress, Archived: true}
		testutil.NoError(t, d.Add(task))
		w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/input", "hi", nil)
		testutil.Equal(t, w.Code, http.StatusConflict)
		body := decodeBody(t, w)
		testutil.Equal(t, body["code"], any("task_archived"))
		testutil.Equal(t, body["error"], any("task is archived"))
	})

	t.Run("live non-archived task without session is no_active_session", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		srv, d := testServer(t)
		task := &model.Task{Name: "n", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/input", "hi", nil)
		testutil.Equal(t, w.Code, http.StatusNotFound)
		testutil.Equal(t, decodeBody(t, w)["code"], any("no_active_session"))
	})

	for _, origin := range []string{"", "system"} {
		name := "user"
		hdr := map[string]string{}
		if origin != "" {
			name = origin
			hdr["X-Input-Origin"] = origin
		}
		t.Run("archived live session receives zero bytes for "+name+" origin", func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			srv, d := testServer(t)
			task := &model.Task{Name: "live", Status: model.StatusInProgress}
			testutil.NoError(t, d.Add(task))
			sess := startCat(t, srv, d, task)
			mux := srv.routes()

			testutil.Equal(t, doReq(mux, "POST", "/api/tasks/"+task.ID+"/archive", "", nil).Code, http.StatusOK)
			before := sess.TotalWritten()
			w := doReq(mux, "POST", "/api/tasks/"+task.ID+"/input", "secret", hdr)
			testutil.Equal(t, w.Code, http.StatusConflict)
			testutil.Equal(t, decodeBody(t, w)["code"], any("task_archived"))
			testutil.Equal(t, sess.TotalWritten(), before)
			testutil.True(t, sess.LastInput().IsZero())
			testutil.True(t, sess.LastUserInput().IsZero())
			testutil.False(t, strings.Contains(w.Body.String(), "secret"))
		})
	}

	t.Run("allowed input returns ok with byte count", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		srv, d := testServer(t)
		task := &model.Task{Name: "ok", Status: model.StatusInProgress}
		testutil.NoError(t, d.Add(task))
		startCat(t, srv, d, task)
		w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/input", "hello", nil)
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, w.Body.String(), "{\"bytes\":\"5\",\"status\":\"ok\"}\n")
	})

}

func mkHeraTask(t *testing.T, d *db.DB, kind db.HeraRoleKind, status model.Status) (*model.Task, int64) {
	t.Helper()
	task := &model.Task{Name: "w-" + string(kind), Status: status, Project: "p"}
	testutil.NoError(t, d.Add(task))
	o, err := d.CreateHeraOrchestrator("orch-"+string(kind), "")
	testutil.NoError(t, err)
	r, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "r", Kind: kind, ArgusProject: "p", Prompt: "x"})
	testutil.NoError(t, err)
	_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: r.ID, ArgusTaskID: task.ID, WorktreePath: "/wt"})
	testutil.NoError(t, err)
	return task, r.ID
}

func readyMeta(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	meta, err := d.ListMeta(id, db.HeraMetaNamespace)
	testutil.NoError(t, err)
	for _, e := range meta {
		if e.Key == db.HeraMetaKeyReadyToClose {
			return e.Value
		}
	}
	return ""
}

func TestCloseoutGuard(t *testing.T) {
	for _, ep := range []string{"resume", "restart"} {
		t.Run(ep+": closed-out markers are refused without side effects", func(t *testing.T) {
			markers := map[string]func(*testing.T, *db.DB, string, int64){
				"ready_to_close": func(t *testing.T, d *db.DB, id string, _ int64) {
					testutil.NoError(t, d.SetMeta(id, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
				},
				"role done": func(t *testing.T, d *db.DB, _ string, role int64) {
					testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
				},
				"role failed": func(t *testing.T, d *db.DB, _ string, role int64) {
					testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusFailed))
				},
			}
			for name, mark := range markers {
				for _, kind := range []db.HeraRoleKind{db.HeraKindWorker, db.HeraKindFreelance} {
					t.Setenv("HOME", t.TempDir())
					srv, d := testServer(t)
					task, role := mkHeraTask(t, d, kind, model.StatusInReview)
					mark(t, d, task.ID, role)
					beforeMeta := readyMeta(t, d, task.ID)
					w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, "", nil)
					testutil.Equal(t, w.Code, http.StatusConflict)
					body := decodeBody(t, w)
					testutil.Equal(t, body["code"], any("hera_closeout"))
					testutil.Equal(t, body["can_force_revive"], any(true))
					got, _ := d.Get(task.ID)
					testutil.Equal(t, got.Status, model.StatusInReview)
					testutil.Equal(t, readyMeta(t, d, task.ID), beforeMeta)
					testutil.True(t, srv.runner.Get(task.ID) == nil)
					awaiting, err := d.HeraWorkerAwaitingCloseout(task.ID)
					testutil.NoError(t, err)
					testutil.True(t, awaiting)
					_ = name
				}
			}
		})

		for _, roleStatus := range []db.HeraRoleStatusValue{db.HeraStatusFailed, db.HeraStatusDone} {
			t.Run(ep+": failed forced launch preserves closure/"+string(roleStatus), func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				srv, d := testServer(t)
				task, role := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
				testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
				testutil.NoError(t, d.UpsertHeraRoleStatus(role, roleStatus))
				w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, `{"force_revive":true}`, nil)
				testutil.Equal(t, w.Code, http.StatusInternalServerError)
				testutil.Equal(t, readyMeta(t, d, task.ID), "true")
				rs, err := d.HeraRoleStatusFor(role)
				testutil.NoError(t, err)
				testutil.Equal(t, rs.Status, roleStatus)
				got, _ := d.Get(task.ID)
				testutil.Equal(t, got.Status, model.StatusInReview)
			})

		}

		t.Run(ep+": forced revival clears markers and starts", func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			srv, d := testServer(t)
			task, role := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
			testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
			testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
			if testing.Short() {
				t.Skip("starts a real PTY-backed process")
			}
			testutil.NoError(t, d.SetBackend("sh-sleep", config.Backend{Command: "sleep 30"}))
			task.Backend, task.Worktree = "sh-sleep", t.TempDir()
			testutil.NoError(t, d.Update(task))
			t.Cleanup(func() {
				if sess := srv.runner.Get(task.ID); sess != nil {
					_ = srv.runner.Stop(task.ID)
					<-sess.Done()
				}
			})
			w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, `{"force_revive":true}`, nil)
			testutil.Equal(t, w.Code, http.StatusOK)
			want := map[string]string{"resume": "resumed", "restart": "restarted"}[ep]
			testutil.Equal(t, decodeBody(t, w)["status"], any(want))
			testutil.Equal(t, readyMeta(t, d, task.ID), "false")
			rs, err := d.HeraRoleStatusFor(role)
			testutil.NoError(t, err)
			testutil.Equal(t, rs.Status, db.HeraStatusWorking)
			got, _ := d.Get(task.ID)
			testutil.Equal(t, got.Status, model.StatusInProgress)
		})

		t.Run(ep+": coordinator and unbound tasks keep normal behavior", func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			srv, d := testServer(t)
			coord, role := mkHeraTask(t, d, db.HeraKindCoordinator, model.StatusInReview)
			testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
			unbound := &model.Task{Name: "u", Status: model.StatusInReview}
			testutil.NoError(t, d.Add(unbound))
			for _, id := range []string{coord.ID, unbound.ID} {
				w := doReq(srv.routes(), "POST", "/api/tasks/"+id+"/"+ep, "", nil)
				testutil.Equal(t, w.Code, http.StatusInternalServerError)
				testutil.True(t, decodeBody(t, w)["code"] != "hera_closeout")
			}
		})

		t.Run(ep+": invalid body is 400 and unknown task is task_not_found", func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			srv, d := testServer(t)
			task := &model.Task{Name: "b", Status: model.StatusInReview}
			testutil.NoError(t, d.Add(task))
			w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, "{nope", nil)
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.Equal(t, decodeBody(t, w)["code"], any("invalid_body"))
			w = doReq(srv.routes(), "POST", "/api/tasks/missing/"+ep, "", nil)
			testutil.Equal(t, w.Code, http.StatusNotFound)
			testutil.Equal(t, decodeBody(t, w)["code"], any("task_not_found"))
		})
	}

	for _, ep := range []string{"resume", "restart"} {
		for _, st := range []model.Status{model.StatusPending, model.StatusInProgress, model.StatusInReview, model.StatusComplete} {
			t.Run(ep+"/busy/"+st.String(), func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				srv, d := testServer(t)
				task, role := mkHeraTask(t, d, db.HeraKindWorker, st)
				testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
				testutil.NoError(t, d.UpsertHeraRoleStatus(role, db.HeraStatusDone))
				runner := &guardRunner{SessionRunner: srv.runner, get: func() agent.SessionHandle { return &moveTestSession{idle: false} }}
				srv.runner = runner
				w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/"+ep, `{"force_revive":true}`, nil)
				testutil.Equal(t, w.Code, http.StatusConflict)
				testutil.Equal(t, decodeBody(t, w)["code"], any("task_running"))
				testutil.Equal(t, readyMeta(t, d, task.ID), "true")
				testutil.Equal(t, runner.calls, 0)
			})
		}
	}

	t.Run("pending restart keeps agent_restarting over the guard", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		srv, d := testServer(t)
		task, _ := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInProgress)
		testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
		srv.runner.(*agent.Runner).SetPendingRestartForTest(task.ID, true)
		w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/resume", "", nil)
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Equal(t, decodeBody(t, w)["code"], any("agent_restarting"))
		testutil.Equal(t, readyMeta(t, d, task.ID), "true")
	})

	t.Run("forced resume reattaches a live session as healed", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		srv, d := testServer(t)
		task, _ := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
		testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
		sess := startCat(t, srv, d, task)
		srv.runner = idleGuardSessionRunner(srv.runner, sess)
		w := doReq(srv.routes(), "POST", "/api/tasks/"+task.ID+"/resume", `{"force_revive":true}`, nil)
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, decodeBody(t, w)["healed"], any(true))
		testutil.Equal(t, readyMeta(t, d, task.ID), "false")
	})
}
