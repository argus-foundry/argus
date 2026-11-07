package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/bridge"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

// keepKickSession is an idle 80-column session whose output tail is fixed, so
// the daemon's rendered-composer read sees exactly what a test paints.
type keepKickSession struct {
	transcriptKickSession
	tail  []byte
	reads *atomic.Int64
}

func (s keepKickSession) RecentOutputTail(int) []byte { return s.tail }

// PTYSize is asked only by the rendered-composer read, so counting it counts
// the screen reads (BlockedOnPrompt also reads the tail, so that can't).
func (s keepKickSession) PTYSize() (int, int) {
	s.reads.Add(1)
	return 80, 24
}
func (keepKickSession) Resize(uint16, uint16) error { return nil }
func (keepKickSession) BridgeGeneration() string    { return "g1" }

type keepKickRunner struct {
	*transcriptKickRunner
	sess keepKickSession
}

func (r *keepKickRunner) Get(string) agent.SessionHandle { return r.sess }

// composerTail paints a Claude-style composer: prompt marker is "❯" + NBSP.
func composerTail(body string) []byte {
	return []byte("\x1b[?1049h\x1b[2J\x1b[7;1H❯ " + body + "\x1b[7;20H")
}

func keepPlaceServer(t *testing.T, tail []byte) (*Server, *model.Task, *keepKickRunner, *atomic.Int64) {
	t.Helper()
	srv, d := testServer(t)
	home := os.Getenv("HOME")
	reads := &atomic.Int64{}
	r := &keepKickRunner{
		transcriptKickRunner: &transcriptKickRunner{Runner: agent.NewRunner(nil)},
		sess:                 keepKickSession{tail: tail, reads: reads},
	}
	srv.runner = r
	task := &model.Task{Name: "kp", Backend: "claude", Worktree: t.TempDir(), SessionID: "11111111-1111-7111-9111-111111111111"}
	testutil.NoError(t, d.Add(task))
	seedAPITranscript(t, home, task.Worktree, task.SessionID, time.Time{})
	return srv, task, r, reads
}

func TestResizeKeepPlace_Gates(t *testing.T) {
	tests := []struct {
		name         string
		tail         []byte
		cs           kickClientState
		wantKick     bool
		wantDeferred string
	}{
		{"scrolled up defers", nil, kickClientState{ScrolledUp: true}, false, "scrolled"},
		{"client flag defers", nil, kickClientState{ComposerDirty: true}, false, "draft"},
		{"screen text defers without a flag", composerTail("fix the bug"), kickClientState{}, false, "draft"},
		{"screen empty beats a stale client flag", composerTail(""), kickClientState{ComposerDirty: true}, true, ""},
		{"composer not found falls through to the flag", []byte("no composer"), kickClientState{ComposerDirty: true}, false, "draft"},
		{"draft outranks scroll", nil, kickClientState{ScrolledUp: true, ComposerDirty: true}, false, "draft"},
		{"nothing set kicks", nil, kickClientState{}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, task, r, _ := keepPlaceServer(t, tc.tail)
			kicked, deferred := srv.evalKickRerender(task.ID, 24, 120, tc.cs)
			testutil.Equal(t, kicked, tc.wantKick)
			testutil.Equal(t, deferred, tc.wantDeferred)
			testutil.Equal(t, r.kicks, map[bool]int{true: 1, false: 0}[tc.wantKick])
			if tc.wantDeferred != "" && srv.isRedundantResize(task.ID, 120) {
				t.Fatal("a deferral must invalidate the cols cache so a same-size resize re-evaluates")
			}
		})
	}
}

func TestResizeKeepPlace_RetryAfterClear(t *testing.T) {
	srv, task, r, _ := keepPlaceServer(t, nil)
	kicked, deferred := srv.evalKickRerender(task.ID, 24, 120, kickClientState{ScrolledUp: true})
	testutil.False(t, kicked)
	testutil.Equal(t, deferred, "scrolled")
	kicked, deferred = srv.evalKickRerender(task.ID, 24, 120, kickClientState{})
	testutil.True(t, kicked)
	testutil.Equal(t, deferred, "")
	testutil.Equal(t, r.kicks, 1)
	srv.kickDeferMu.Lock()
	_, held := srv.kickDeferSince[task.ID]
	srv.kickDeferMu.Unlock()
	testutil.False(t, held) // a kick clears the deferral clock
}

// The client flag is the only signal under the 10-minute per-width cap; the
// scroll gate and a screen-confirmed draft are not.
func TestResizeKeepPlace_FlagCap(t *testing.T) {
	tests := []struct {
		name         string
		tail         []byte
		cs           kickClientState
		age          time.Duration
		cols         uint16
		wantKick     bool
		wantDeferred string
	}{
		{"flag under the cap holds", nil, kickClientState{ComposerDirty: true}, agent.KickDeferCap - time.Minute, 120, false, "draft"},
		{"flag past the cap releases", nil, kickClientState{ComposerDirty: true}, agent.KickDeferCap + time.Minute, 120, true, ""},
		{"a clock at another width does not count", nil, kickClientState{ComposerDirty: true}, agent.KickDeferCap + time.Minute, 100, false, "draft"},
		{"scroll is never capped", nil, kickClientState{ScrolledUp: true}, 3 * agent.KickDeferCap, 120, false, "scrolled"},
		{"screen-confirmed draft is never capped", composerTail("typed"), kickClientState{ComposerDirty: true}, 3 * agent.KickDeferCap, 120, false, "draft"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, task, _, _ := keepPlaceServer(t, tc.tail)
			srv.kickDeferMu.Lock()
			srv.kickDeferSince[task.ID] = kickDeferClock{cols: tc.cols, at: time.Now().Add(-tc.age)}
			srv.kickDeferMu.Unlock()
			kicked, deferred := srv.evalKickRerender(task.ID, 24, 120, tc.cs)
			testutil.Equal(t, kicked, tc.wantKick)
			testutil.Equal(t, deferred, tc.wantDeferred)
		})
	}
}

func TestResizeKeepPlace_SettingOffIgnoresClientState(t *testing.T) {
	srv, task, _, reads := keepPlaceServer(t, composerTail("typed"))
	testutil.NoError(t, srv.db.SetConfigValue("ui.rerender_keep_place", "false"))
	kicked, deferred := srv.evalKickRerender(task.ID, 24, 120, kickClientState{ScrolledUp: true, ComposerDirty: true})
	testutil.True(t, kicked)
	testutil.Equal(t, deferred, "")
	testutil.Equal(t, reads.Load(), int64(0)) // no screen read either
}

// The rendered-composer read costs a log-tail emulation, so it runs only when a
// kick is genuinely being evaluated and nothing cheaper answered.
func TestResizeKeepPlace_ScreenReadOnlyOnEvaluationPath(t *testing.T) {
	t.Run("margin not crossed", func(t *testing.T) {
		srv, task, _, reads := keepPlaceServer(t, composerTail("typed"))
		kicked, _ := srv.evalKickRerender(task.ID, 24, 85, kickClientState{})
		testutil.False(t, kicked)
		testutil.Equal(t, reads.Load(), int64(0))
	})
	t.Run("unchanged cols", func(t *testing.T) {
		srv, task, _, reads := keepPlaceServer(t, composerTail("typed"))
		srv.isRedundantResize(task.ID, 120) // seeds the cache at 120
		srv.evalKickRerender(task.ID, 24, 120, kickClientState{})
		testutil.Equal(t, reads.Load(), int64(0))
	})
	t.Run("draft-aware bridge answers first", func(t *testing.T) {
		srv, task, _, reads := keepPlaceServer(t, composerTail("typed"))
		srv.bridgeHub.Ingest(task.ID, []bridge.Event{{Type: bridge.TypeDraft, DraftAware: true, Boot: "b", Generation: "g1", SessionID: "s1", Seq: 1}}, time.Now())
		kicked, _ := srv.evalKickRerender(task.ID, 24, 120, kickClientState{})
		testutil.True(t, kicked) // aware + empty: authoritative "no draft"
		testutil.Equal(t, reads.Load(), int64(0))
	})
	t.Run("evaluated kick reads once", func(t *testing.T) {
		srv, task, _, reads := keepPlaceServer(t, composerTail(""))
		srv.evalKickRerender(task.ID, 24, 120, kickClientState{})
		testutil.Equal(t, reads.Load(), int64(1))
	})
}

func TestHandleResize_ReportsDeferred(t *testing.T) {
	srv, task, _, _ := keepPlaceServer(t, nil)
	post := func(body string) map[string]any {
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/resize", body))
		testutil.Equal(t, w.Code, http.StatusOK)
		var out map[string]any
		testutil.NoError(t, json.NewDecoder(strings.NewReader(w.Body.String())).Decode(&out))
		return out
	}
	out := post(`{"cols":120,"rows":24,"scrolled_up":true}`)
	testutil.Equal(t, out["rerendered"], false)
	testutil.Equal(t, out["deferred"], "scrolled")
	out = post(`{"cols":120,"rows":24}`) // old-client shape: same size, nothing set
	testutil.Equal(t, out["rerendered"], true)
	testutil.Equal(t, out["deferred"], "")
}
