package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func usageStart(ts, turn string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"task_started","turn_id":%q}}`+"\n", ts, turn)
}

func usageCount(ts string, ti, tc, to, li, lc, lo int64) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d},"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d}}}}`+"\n",
		ts, ti, tc, to, li, lc, lo)
}

func twoTurns() string {
	return usageStart("2026-11-07T10:00:00Z", "turn1") + usageCount("2026-11-07T10:00:01Z", 1000, 500, 100, 1000, 500, 100) +
		usageStart("2026-11-07T10:01:00Z", "turn2") + usageCount("2026-11-07T10:01:01Z", 1700, 900, 150, 700, 400, 50)
}

func getUsage(t *testing.T, s *Server, id, query string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+id+"/usage"+query, ""))
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func turnIDs(body map[string]any) []string {
	var ids []string
	for _, v := range body["turns"].([]any) {
		ids = append(ids, v.(map[string]any)["turn_id"].(string))
	}
	return ids
}

func TestTaskUsage_StandaloneTotalsAndTurns(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "thr1", "/w/cx", 1, twoTurns())

	code, body := getUsage(t, e.s, e.task.ID, "")
	testutil.Equal(t, code, 200)
	eqAny(t, body["task_id"], e.task.ID)
	eqAny(t, body["session_id"], "thr1")
	eqAny(t, body["provider"], "openai")
	eqAny(t, body["source"], "codex_rollout")
	eqAny(t, body["measured_at"], "2026-11-07T10:01:01Z")
	testutil.True(t, body["next_cursor"] == nil)
	tot := body["totals"].(map[string]any)
	eqAny(t, tot["input_tokens"], float64(1700))
	eqAny(t, tot["cached_input_tokens"], float64(900))
	eqAny(t, tot["output_tokens"], float64(150))
	testutil.Equal(t, strings.Join(turnIDs(body), ","), "turn2,turn1")
	t1 := body["turns"].([]any)[1].(map[string]any)
	eqAny(t, t1["input_tokens"], float64(1000))
	eqAny(t, t1["cached_input_tokens"], float64(500))

	st, err := e.s.db.TaskUsageSession(e.task.ID, "thr1")
	testutil.NoError(t, err)
	testutil.Equal(t, *st.InputTokens, int64(1700))
	turns, err := e.s.db.TaskUsageTurns(e.task.ID, "thr1", 10, false, 0, "")
	testutil.NoError(t, err)
	testutil.Equal(t, len(turns), 2)
}

func TestTaskUsage_TurnPagination(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "thr1", "/w/cx", 1, twoTurns())

	_, p1 := getUsage(t, e.s, e.task.ID, "?limit=1")
	testutil.Equal(t, strings.Join(turnIDs(p1), ","), "turn2")
	cur, _ := p1["next_cursor"].(string)
	testutil.True(t, cur != "")
	_, p2 := getUsage(t, e.s, e.task.ID, "?limit=1&before="+cur)
	testutil.Equal(t, strings.Join(turnIDs(p2), ","), "turn1")
	testutil.True(t, p2["next_cursor"] == nil)
	testutil.True(t, p2["totals"] != nil)

	for _, q := range []string{"?limit=0", "?limit=abc", "?limit=-1", "?before=!!!", "?before=" + "bm9jb2xvbg"} {
		code, _ := getUsage(t, e.s, e.task.ID, q)
		testutil.Equal(t, code, 400)
	}
	code, _ := getUsage(t, e.s, e.task.ID, "?limit=100000")
	testutil.Equal(t, code, 200)
}

func TestTaskUsage_ReplacedSessionsStaySeparate(t *testing.T) {
	e := newCodexCtxEnv(t, "thr-old")
	e.addThread(t, "thr-old", "/w/cx", 1, twoTurns())
	e.addThread(t, "thr-new", "/w/cx", 2, usageStart("2026-11-07T11:00:00Z", "turnN")+usageCount("2026-11-07T11:00:01Z", 50, 0, 5, 50, 0, 5))
	_, old := getUsage(t, e.s, e.task.ID, "")
	eqAny(t, old["session_id"], "thr-old")

	e.task.SessionID = "thr-new"
	testutil.NoError(t, e.s.db.Update(e.task))
	_, cur := getUsage(t, e.s, e.task.ID, "")
	eqAny(t, cur["session_id"], "thr-new")
	eqAny(t, cur["totals"].(map[string]any)["input_tokens"], float64(50))
	testutil.Equal(t, strings.Join(turnIDs(cur), ","), "turnN")

	_, prev := getUsage(t, e.s, e.task.ID, "?session_id=thr-old")
	eqAny(t, prev["session_id"], "thr-old")
	eqAny(t, prev["totals"].(map[string]any)["input_tokens"], float64(1700))
	testutil.Equal(t, len(turnIDs(prev)), 2)

	code, _ := getUsage(t, e.s, e.task.ID, "?session_id=someone-elses")
	testutil.Equal(t, code, 404)
}

func TestTaskUsage_OtherTasksSessionIs404(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	getUsage(t, e.s, e.task.ID, "")
	other := &model.Task{Name: "other", Backend: "codex-test", Worktree: "/w/other", Status: model.StatusInProgress}
	testutil.NoError(t, e.s.db.Add(other))
	code, _ := getUsage(t, e.s, other.ID, "?session_id=thr1")
	testutil.Equal(t, code, 404)
}

func TestTaskUsage_UnsupportedProviderIsUnavailableNotZero(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	code, body := getUsage(t, s, task.ID, "")
	testutil.Equal(t, code, 200)
	testutil.True(t, body["totals"] == nil)
	testutil.Equal(t, len(body["turns"].([]any)), 0)
	eqAny(t, body["provider"], "anthropic")
	testutil.True(t, body["measured_at"] == nil)
}

func TestTaskUsage_NoMeasurementYet(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	code, body := getUsage(t, e.s, e.task.ID, "")
	testutil.Equal(t, code, 200)
	testutil.True(t, body["totals"] == nil)
	testutil.Equal(t, len(body["turns"].([]any)), 0)
	eqAny(t, body["provider"], "openai")
}

func TestTaskUsage_MeasuredZeroVersusNull(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	body := usageStart("2026-11-07T10:00:00Z", "z1") + usageCount("2026-11-07T10:00:01Z", 0, 0, 0, 0, 0, 0) +
		usageStart("2026-11-07T10:01:00Z", "n1")
	e.addThread(t, "thr1", "/w/cx", 1, body)
	_, got := getUsage(t, e.s, e.task.ID, "")
	eqAny(t, got["totals"].(map[string]any)["input_tokens"], float64(0))
	turns := got["turns"].([]any)
	testutil.True(t, turns[0].(map[string]any)["input_tokens"] == nil)
	eqAny(t, turns[1].(map[string]any)["input_tokens"], float64(0))
}

func TestTaskUsage_ReaderFailureKeepsStoredCounters(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	p := e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	getUsage(t, e.s, e.task.ID, "")
	testutil.NoError(t, os.Remove(p))
	code, body := getUsage(t, e.s, e.task.ID, "")
	testutil.Equal(t, code, 200)
	eqAny(t, body["totals"].(map[string]any)["input_tokens"], float64(1700))
	testutil.Equal(t, len(turnIDs(body)), 2)
}

func TestTaskUsage_StaleObservationDoesNotRollBack(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	p := e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	getUsage(t, e.s, e.task.ID, "")
	older := usageStart("2026-11-07T09:00:00Z", "turn1") + usageCount("2026-11-07T09:00:01Z", 10, 0, 1, 10, 0, 1)
	testutil.NoError(t, os.WriteFile(p, []byte(older), 0o600))
	_, body := getUsage(t, e.s, e.task.ID, "")
	eqAny(t, body["totals"].(map[string]any)["input_tokens"], float64(1700))
	eqAny(t, body["measured_at"], "2026-11-07T10:01:01Z")
}

func TestTaskUsage_RepeatedIngestDoesNotDoubleAccrue(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	for i := 0; i < 3; i++ {
		_, body := getUsage(t, e.s, e.task.ID, "")
		eqAny(t, body["totals"].(map[string]any)["input_tokens"], float64(1700))
	}
	turns, err := e.s.db.TaskUsageTurns(e.task.ID, "thr1", 10, false, 0, "")
	testutil.NoError(t, err)
	testutil.Equal(t, len(turns), 2)
	testutil.Equal(t, *turns[0].InputTokens, int64(700))
}

func TestTaskUsage_RequiresAuthAnd404(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	code, _ := getUsage(t, e.s, "nope", "")
	testutil.Equal(t, code, 404)
	h := authMiddleware(e.s.token, e.s.db, e.s.push, e.s.routes(), "/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks/"+e.task.ID+"/usage", nil))
	testutil.Equal(t, w.Code, 401)
}

func TestTaskUsage_EventOnChangeOnly(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	sink := &ctxSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	p := e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	state := newIdleWatcherState()
	ids := []string{e.task.ID}

	e.s.emitReaderUsageChanges(state, ids)
	e.s.emitReaderUsageChanges(state, ids)
	evs := sink.ofType(model.EventTypeTaskUsageChanged)
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0].TaskID, e.task.ID)
	testutil.Equal(t, string(evs[0].Payload), `{"measured_at":"2026-11-07T10:01:01Z","session_id":"thr1","turn_id":"turn2"}`)
	testutil.False(t, strings.Contains(string(evs[0].Payload), "1700"))

	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	testutil.NoError(t, err)
	_, err = f.WriteString(usageStart("2026-11-07T10:02:00Z", "turn3") + usageCount("2026-11-07T10:02:01Z", 1800, 900, 160, 100, 0, 10))
	testutil.NoError(t, err)
	testutil.NoError(t, f.Close())
	e.s.emitReaderUsageChanges(state, ids)
	evs = sink.ofType(model.EventTypeTaskUsageChanged)
	testutil.Equal(t, len(evs), 2)
	testutil.Equal(t, string(evs[1].Payload), `{"measured_at":"2026-11-07T10:02:01Z","session_id":"thr1","turn_id":"turn3"}`)

	e.s.emitReaderUsageChanges(state, nil)
	testutil.Equal(t, len(sink.ofType(model.EventTypeTaskUsageChanged)), 2)
}

func TestTaskUsage_TaskDeleteRemovesStore(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	getUsage(t, e.s, e.task.ID, "")
	testutil.NoError(t, e.s.db.Delete(e.task.ID))
	st, err := e.s.db.TaskUsageSession(e.task.ID, "thr1")
	testutil.NoError(t, err)
	testutil.True(t, st == nil)
	turns, err := e.s.db.TaskUsageTurns(e.task.ID, "thr1", 10, false, 0, "")
	testutil.NoError(t, err)
	testutil.Equal(t, len(turns), 0)
}
