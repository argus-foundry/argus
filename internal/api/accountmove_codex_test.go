package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const apiCodexSID = "0198a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"

const apiThreadsDDL = `CREATE TABLE threads (
	id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL, source TEXT NOT NULL, model_provider TEXT NOT NULL,
	cwd TEXT NOT NULL, title TEXT NOT NULL, sandbox_policy TEXT NOT NULL,
	approval_mode TEXT NOT NULL)`

type codexAPIFixture struct {
	srv     *Server
	d       *db.DB
	task    *model.Task
	dstHome string
	rollout string
}

func openStateDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newCodexAPIFixture(t *testing.T, r *moveTestRunner, withDstDB bool) *codexAPIFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CODEX_SQLITE_HOME", "")
	s, d := testServer(t)
	home = os.Getenv("HOME")
	r.SessionRunner = agent.NewRunner(nil)
	s.runner = r
	acctRoot := t.TempDir()
	dstHome := filepath.Join(acctRoot, "cx")
	testutil.NoError(t, d.SetBackend("cxb", config.Backend{Command: "codex"}))
	testutil.NoError(t, d.SetAccount("cx", config.Account{CodexHome: dstHome}))

	srcHome := filepath.Join(home, ".codex")
	testutil.NoError(t, os.MkdirAll(srcHome, 0o700))
	wt := t.TempDir()
	rollout := filepath.Join(srcHome, "sessions", "2026", "11", "07", "rollout-"+apiCodexSID+".jsonl")
	testutil.NoError(t, os.MkdirAll(filepath.Dir(rollout), 0o700))
	header, err := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": apiCodexSID, "cwd": wt, "timestamp": "2026-11-07T10:00:00Z"}})
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(rollout, append(header, '\n'), 0600))
	src := openStateDB(t, filepath.Join(srcHome, "state_5.sqlite"))
	_, err = src.Exec(apiThreadsDDL)
	testutil.NoError(t, err)
	_, err = src.Exec(`INSERT INTO threads VALUES(?,?,1,2,'cli','openai',?,'t','s','never')`, apiCodexSID, rollout, wt)
	testutil.NoError(t, err)
	if withDstDB {
		testutil.NoError(t, os.MkdirAll(dstHome, 0o700))
		_, err = openStateDB(t, filepath.Join(dstHome, "state_5.sqlite")).Exec(apiThreadsDDL)
		testutil.NoError(t, err)
	}

	task := &model.Task{Name: "cxtask", Backend: "cxb", Worktree: wt, Branch: "br", SessionID: apiCodexSID, Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	return &codexAPIFixture{srv: s, d: d, task: task, dstHome: dstHome, rollout: rollout}
}

func accountEvents(sink *recordingSink) []map[string]any {
	var out []map[string]any
	for _, ev := range sink.events() {
		if ev.Type != model.EventTypeTaskAccountChanged {
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(ev.Payload, &p)
		out = append(out, p)
	}
	return out
}

func threadInTarget(t *testing.T, f *codexAPIFixture) bool {
	t.Helper()
	var n int
	err := openStateDB(t, filepath.Join(f.dstHome, "state_5.sqlite")).QueryRow(`SELECT COUNT(*) FROM threads WHERE id = ?`, apiCodexSID).Scan(&n)
	testutil.NoError(t, err)
	return n == 1
}

func TestMoveCodexTaskAccountAPI(t *testing.T) {
	t.Run("live idle codex task moves, resumes and emits", func(t *testing.T) {
		sink := installAPISink(t)
		r := &moveTestRunner{live: &moveTestSession{idle: true}}
		f := newCodexAPIFixture(t, r, true)
		w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		body := decodeBody(t, w)
		testutil.DeepEqual(t, body, map[string]any{"from": "default", "to": "cx", "resumed": true, "started": true})

		got, err := f.d.Get(f.task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Account, "cx")
		testutil.Equal(t, got.SessionID, apiCodexSID)
		testutil.Equal(t, got.Worktree, f.task.Worktree)
		testutil.Equal(t, got.Branch, "br")
		testutil.NotNil(t, r.kicked)
		testutil.Equal(t, r.kicked.Account, "cx")
		testutil.Equal(t, r.kicked.SessionID, apiCodexSID)
		testutil.True(t, threadInTarget(t, f))
		_, err = os.Stat(filepath.Join(f.dstHome, "sessions", "2026", "11", "07", "rollout-"+apiCodexSID+".jsonl"))
		testutil.NoError(t, err)
		_, err = os.Stat(f.rollout)
		testutil.NoError(t, err)

		evs := accountEvents(sink)
		testutil.Equal(t, len(evs), 1)
		testutil.DeepEqual(t, evs[0], map[string]any{"from": "default", "to": "cx", "resumed": true, "started": true})
	})

	t.Run("hera binding survives the move", func(t *testing.T) {
		r := &moveTestRunner{live: &moveTestSession{idle: true}}
		f := newCodexAPIFixture(t, r, true)
		o, err := f.d.CreateHeraOrchestrator("orch-mv", "")
		testutil.NoError(t, err)
		role, err := f.d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "x"})
		testutil.NoError(t, err)
		_, err = f.d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: role.ID, ArgusTaskID: f.task.ID, WorktreePath: f.task.Worktree})
		testutil.NoError(t, err)
		testutil.Equal(t, postMove(f.srv, f.task.ID, `{"account":"cx"}`).Code, http.StatusOK)
		bound, err := f.d.TaskHoldsLiveHeraWorkerOrFreelanceBinding(f.task.ID)
		testutil.NoError(t, err)
		testutil.True(t, bound)
	})

	t.Run("busy task is 409 until forced", func(t *testing.T) {
		sink := installAPISink(t)
		r := &moveTestRunner{live: &moveTestSession{idle: false}}
		f := newCodexAPIFixture(t, r, true)
		testutil.Equal(t, postMove(f.srv, f.task.ID, `{"account":"cx"}`).Code, http.StatusConflict)
		got, _ := f.d.Get(f.task.ID)
		testutil.Equal(t, got.Account, "")
		testutil.False(t, threadInTarget(t, f))
		testutil.Equal(t, len(accountEvents(sink)), 0)
		testutil.Equal(t, postMove(f.srv, f.task.ID, `{"account":"cx","force":true}`).Code, http.StatusOK)
		testutil.Equal(t, len(accountEvents(sink)), 1)
	})

	t.Run("target without a state database is 400 and changes nothing", func(t *testing.T) {
		sink := installAPISink(t)
		r := &moveTestRunner{live: &moveTestSession{idle: true}}
		f := newCodexAPIFixture(t, r, false)
		w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
		testutil.Equal(t, w.Code, http.StatusBadRequest)
		testutil.Contains(t, w.Body.String(), "Codex state database")
		got, _ := f.d.Get(f.task.ID)
		testutil.Equal(t, got.Account, "")
		testutil.Nil(t, r.kicked)
		testutil.Equal(t, len(accountEvents(sink)), 0)
	})

	t.Run("restart failure after persisting is the documented partial outcome", func(t *testing.T) {
		sink := installAPISink(t)
		r := &moveTestRunner{startErr: errors.New("spawn failed")}
		f := newCodexAPIFixture(t, r, true)
		w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		body := decodeBody(t, w)
		testutil.Equal(t, body["error"], any("restart failed"))
		testutil.Equal(t, body["code"], any("restart_failed"))
		testutil.Equal(t, body["account"], any("cx"))
		testutil.Equal(t, body["moved"], any(true))
		testutil.Equal(t, body["started"], any(false))
		got, _ := f.d.Get(f.task.ID)
		testutil.Equal(t, got.Account, "cx")
		evs := accountEvents(sink)
		testutil.Equal(t, len(evs), 1)
		testutil.DeepEqual(t, evs[0], map[string]any{"from": "default", "to": "cx", "resumed": true, "started": false})
	})

	t.Run("kick failure with the old session alive keeps the account", func(t *testing.T) {
		sink := installAPISink(t)
		r := &moveTestRunner{live: &moveTestSession{idle: true}, kickErr: errors.New("kick failed")}
		f := newCodexAPIFixture(t, r, true)
		w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		body := decodeBody(t, w)
		testutil.Equal(t, body["code"], any("restart_unknown"))
		testutil.Equal(t, body["moved"], any(true))
		testutil.Equal(t, body["rolled_back"], any(false))
		testutil.Equal(t, body["account"], any("cx"))
		got, _ := f.d.Get(f.task.ID)
		testutil.Equal(t, got.Account, "cx")
		testutil.Equal(t, got.SessionID, apiCodexSID)
		testutil.Equal(t, len(accountEvents(sink)), 1)
	})

	t.Run("closed-out hera worker is refused before any change", func(t *testing.T) {
		r := &moveTestRunner{}
		f := newCodexAPIFixture(t, r, true)
		o, err := f.d.CreateHeraOrchestrator("orch-co", "")
		testutil.NoError(t, err)
		role, err := f.d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "w", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "x"})
		testutil.NoError(t, err)
		_, err = f.d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: role.ID, ArgusTaskID: f.task.ID, WorktreePath: f.task.Worktree})
		testutil.NoError(t, err)
		testutil.NoError(t, f.d.UpsertHeraRoleStatus(role.ID, db.HeraStatusDone))
		w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Equal(t, decodeBody(t, w)["code"], any("hera_closeout"))
		got, _ := f.d.Get(f.task.ID)
		testutil.Equal(t, got.Account, "")
		testutil.Nil(t, r.started)
	})

	t.Run("moves serialize on the per-task lock", func(t *testing.T) {
		r := &moveTestRunner{live: &moveTestSession{idle: true}}
		f := newCodexAPIFixture(t, r, true)
		unlock := f.srv.taskLocks.Lock(f.task.ID)
		done := make(chan struct{})
		go func() {
			_, _ = f.srv.MoveTaskAccount(agent.AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			close(done)
		}()
		select {
		case <-done:
			t.Fatal("move ran while the task lock was held")
		case <-time.After(100 * time.Millisecond):
		}
		unlock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("move did not run after the lock was released")
		}
	})
}

func TestMoveTaskAccountAuthBoundary(t *testing.T) {
	r := &moveTestRunner{live: &moveTestSession{idle: true}}
	f := newCodexAPIFixture(t, r, true)
	plain, _, err := MintToken(f.d, "phone")
	testutil.NoError(t, err)
	h := authMiddleware("test-token", f.d, nil, f.srv.routes())
	do := func(token string) int {
		req := httptest.NewRequest("POST", "/api/tasks/"+f.task.ID+"/account", strings.NewReader(`{"account":"cx"}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	testutil.Equal(t, do(""), http.StatusUnauthorized)
	testutil.Equal(t, do("wrong"), http.StatusUnauthorized)
	got, _ := f.d.Get(f.task.ID)
	testutil.Equal(t, got.Account, "")
	testutil.Equal(t, do(plain), http.StatusOK)
	got, _ = f.d.Get(f.task.ID)
	testutil.Equal(t, got.Account, "cx")
}

func TestListBackendsAccountMove(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.SetBackend("claude", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetBackend("codex", config.Backend{Command: "codex"}))
	testutil.NoError(t, d.SetBackend("pi", config.Backend{Command: "pi"}))
	w := doReq(srv.routes(), "GET", "/api/backends", "", nil)
	testutil.Equal(t, w.Code, http.StatusOK)
	var resp struct {
		Backends []struct {
			Name        string `json:"name"`
			AccountMove struct {
				Supported             bool `json:"supported"`
				PreservesConversation bool `json:"preserves_conversation"`
			} `json:"account_move"`
		} `json:"backends"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	want := map[string]bool{"claude": true, "codex": true, "pi": false}
	seen := 0
	for _, b := range resp.Backends {
		if exp, ok := want[b.Name]; ok {
			seen++
			testutil.Equal(t, b.AccountMove.Supported, exp)
			testutil.Equal(t, b.AccountMove.PreservesConversation, exp)
		}
	}
	testutil.Equal(t, seen, 3)
}
