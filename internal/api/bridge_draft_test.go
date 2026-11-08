package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/uxlog"
)

const draftEventBody = `{"events":[{"type":"draft","text":"fix the bug","cursor":11,"generation":"launch","session_id":"session","boot":"old","seq":1}]}`

func postDraftPull(s *Server, id, generation, boot string) (int, map[string]any) {
	w := httptest.NewRecorder()
	body := `{"generation":"` + generation + `","boot":"` + boot + `"}`
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+id+"/bridge/draft", body))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestBridgeDraftIngestExposesOnlyBoolean(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", draftEventBody))
	testutil.Equal(t, w.Code, 200)

	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/bridge", ""))
	testutil.True(t, strings.Contains(w.Body.String(), `"has_draft":true`))
	testutil.False(t, strings.Contains(w.Body.String(), "fix the bug"))

	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("GET", "/api/bridge/states", ""))
	testutil.True(t, strings.Contains(w.Body.String(), `"has_draft":true`))
	testutil.False(t, strings.Contains(w.Body.String(), "fix the bug"))
}

func TestBridgeDraftPull(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", draftEventBody))
	testutil.Equal(t, w.Code, 200)

	t.Run("wrong generation gets nothing", func(t *testing.T) {
		code, out := postDraftPull(s, task.ID, "someone-else", "new")
		testutil.Equal(t, code, 200)
		testutil.Nil(t, out["draft"])
	})
	t.Run("same boot gets nothing", func(t *testing.T) {
		_, out := postDraftPull(s, task.ID, "launch", "old")
		testutil.Nil(t, out["draft"])
	})
	t.Run("new boot gets it once", func(t *testing.T) {
		code, out := postDraftPull(s, task.ID, "launch", "new")
		testutil.Equal(t, code, 200)
		d, _ := out["draft"].(map[string]any)
		testutil.Equal(t, d["text"].(string), "fix the bug")
		testutil.Equal(t, d["cursor"].(float64), float64(11))
		_, out = postDraftPull(s, task.ID, "launch", "new")
		testutil.Nil(t, out["draft"])
	})
	t.Run("bad body", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/draft", `{"boot":""}`))
		testutil.Equal(t, w.Code, 400)
	})
	t.Run("unknown task", func(t *testing.T) {
		code, _ := postDraftPull(s, "nope", "launch", "new")
		testutil.Equal(t, code, 404)
	})
}

func TestBridgeDraftKeepPlaceOff(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	testutil.NoError(t, s.db.SetConfigValue("ui.rerender_keep_place", "false"))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", draftEventBody))
	testutil.Equal(t, w.Code, 200)
	st, ok := s.bridgeHub.Get(task.ID)
	if ok {
		testutil.False(t, st.HasDraft)
	}

	// A draft stored while the setting was on is not handed back once it is off.
	testutil.NoError(t, s.db.SetConfigValue("ui.rerender_keep_place", "true"))
	s.routes().ServeHTTP(httptest.NewRecorder(), authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", draftEventBody))
	testutil.NoError(t, s.db.SetConfigValue("ui.rerender_keep_place", "false"))
	_, out := postDraftPull(s, task.ID, "launch", "new")
	testutil.Nil(t, out["draft"])
}

// draftKickSession reads as idle at an 80-col start so only the draft decides.
type draftKickSession struct{ *bridgeTestSession }

func (draftKickSession) InitialPTYSize() (int, int) { return 80, 24 }
func (draftKickSession) IsIdle() bool               { return true }

func TestMaybeKickRerender_DefersOnBridgeDraft(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ux.log")
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	logged := func() string {
		b, err := os.ReadFile(logPath)
		testutil.NoError(t, err)
		return string(b)
	}

	s, r, task := bridgeAuthorityServer(t)
	r.SessionRunner = agent.NewRunner(nil)
	// Same fake, but with the idle/size answers the resize path asks for.
	s.runner = &draftKickRunner{bridgeTestRunner: r, sess: draftKickSession{r.session}}
	s.bridgeHub.Ingest(task.ID, nil, time.Now())
	s.routes().ServeHTTP(httptest.NewRecorder(), authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", draftEventBody))

	testutil.False(t, s.maybeKickRerender(task.ID, 24, 120))
	testutil.True(t, strings.Contains(logged(), "unsent composer draft"))
	if s.isRedundantResize(task.ID, 120) {
		t.Fatal("draft deferral must invalidate the cols cache so a later resize re-evaluates")
	}

	// With the setting off the draft gate is skipped entirely.
	before := strings.Count(logged(), "unsent composer draft")
	testutil.NoError(t, s.db.SetConfigValue("ui.rerender_keep_place", "false"))
	s.maybeKickRerender(task.ID, 24, 130)
	testutil.Equal(t, strings.Count(logged(), "unsent composer draft"), before)
}

type draftKickRunner struct {
	*bridgeTestRunner
	sess draftKickSession
}

func (r *draftKickRunner) Get(id string) agent.SessionHandle {
	if id == r.id {
		return r.sess
	}
	return nil
}
