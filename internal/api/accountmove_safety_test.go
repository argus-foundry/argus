package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestMoveAccountMissingSourceRequiresReadyTarget(t *testing.T) {
	r := &moveTestRunner{}
	f := newCodexAPIFixture(t, r, false)
	testutil.NoError(t, os.Remove(f.rollout))
	sink := installAPISink(t)
	w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
	testutil.Equal(t, w.Code, http.StatusBadRequest)
	stored, err := f.d.Get(f.task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, stored.Account, "")
	testutil.Equal(t, stored.SessionID, apiCodexSID)
	testutil.Nil(t, r.started)
	testutil.Nil(t, r.kicked)
	testutil.Equal(t, len(accountEvents(sink)), 0)
}

func TestMoveAccountMissingSourceStartsFresh(t *testing.T) {
	for _, source := range []string{"missing row", "missing rollout"} {
		t.Run(source, func(t *testing.T) {
			r := &moveTestRunner{}
			f := newCodexAPIFixture(t, r, true)
			if source == "missing row" {
				_, err := openStateDB(t, filepath.Join(os.Getenv("HOME"), ".codex", "state_5.sqlite")).Exec(`DELETE FROM threads`)
				testutil.NoError(t, err)
			} else {
				testutil.NoError(t, os.Remove(f.rollout))
			}
			w := postMove(f.srv, f.task.ID, `{"account":"cx"}`)
			testutil.Equal(t, w.Code, http.StatusOK)
			testutil.Equal(t, decodeBody(t, w)["resumed"], any(false))
			testutil.False(t, r.startResume)
			stored, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, stored.SessionID, "")
			testutil.NotNil(t, r.started)
			if r.started == nil {
				return
			}
			cmd, cleanup, err := agent.BuildCmd(r.started, f.d.Config(), r.startResume)
			testutil.NoError(t, err)
			if cleanup != nil {
				defer cleanup()
			}
			if cmd != nil {
				script := strings.Join(cmd.Args, " ")
				testutil.False(t, strings.Contains(script, "resume"))
				testutil.False(t, strings.Contains(script, apiCodexSID))
			}
		})
	}
}

func TestMoveAccountCanceledWhileQueued(t *testing.T) {
	r := &moveTestRunner{live: &moveTestSession{idle: true}}
	f := newCodexAPIFixture(t, r, true)
	sink := installAPISink(t)
	synctest.Test(t, func(t *testing.T) {
		release := f.srv.taskLocks.Lock(f.task.ID)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req := authedReq("POST", "/api/tasks/"+f.task.ID+"/account", `{"account":"cx"}`).WithContext(ctx)
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { defer close(done); f.srv.routes().ServeHTTP(w, req) }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		returned := false
		select {
		case <-done:
			returned = true
		default:
			t.Error("canceled request remained queued")
		}
		release()
		<-done
		if returned {
			testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
		}
		stored, err := f.d.Get(f.task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, stored.Account, "")
		testutil.Nil(t, r.kicked)
		testutil.Nil(t, r.started)
		testutil.Equal(t, len(accountEvents(sink)), 0)
		var count int
		testutil.NoError(t, openStateDB(t, filepath.Join(f.dstHome, "state_5.sqlite")).QueryRow(`SELECT COUNT(*) FROM threads`).Scan(&count))
		testutil.Equal(t, count, 0)
	})
}
