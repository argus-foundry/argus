package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskContext_AllCategoryDTOValues(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	testutil.Equal(t, postMeasure(t, s, task.ID, `{"tokens":100,"window":200000,"categories":{"messages":1,"memory":2,"tools":3,"agents":4,"skills":5,"system":6,"compaction":7}}`, 1), 200)
	_, body := getContext(t, s, task.ID)
	cats := body["context"].(map[string]any)["categories"].(map[string]any)
	for i, key := range []string{"messages", "memory", "tools", "agents", "skills", "system", "compaction"} {
		t.Run(key, func(t *testing.T) { eqAny(t, cats[key], float64(i+1)) })
	}
}

func TestTaskContext_EveryNegativeCategory(t *testing.T) {
	for _, key := range []string{"messages", "memory", "tools", "agents", "skills", "system", "compaction"} {
		t.Run(key, func(t *testing.T) {
			s, _, task := bridgeAuthorityServer(t)
			testutil.Equal(t, postMeasure(t, s, task.ID, `{"tokens":100,"categories":{"messages":50}}`, 1), 200)
			testutil.Equal(t, postMeasure(t, s, task.ID, fmt.Sprintf(`{"tokens":5,"categories":{"%s":-1}}`, key), 2), 200)
			st, _ := s.bridgeHub.Get(task.ID)
			testutil.Equal(t, *st.Context.Tokens, int64(100))
			if t.Failed() {
				return
			}
			testutil.Equal(t, *st.Context.Categories.Messages, int64(50))
		})
	}
}

func TestTaskContext_FreshHeartbeatKeepsOldMeasurementLive(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	old := time.Now().Add(-10 * time.Minute)
	testutil.Equal(t, s.bridgeHub.Ingest(task.ID, []bridge.Event{{Type: bridge.TypeMeasure, Generation: "launch", SessionID: "session", Boot: "b", Seq: 1, Context: &bridge.Context{Tokens: ptr64(7), Window: 100}}}, old), 1)
	testutil.Equal(t, s.bridgeHub.Ingest(task.ID, []bridge.Event{{Type: bridge.TypeHeartbeat, Generation: "launch", SessionID: "session", Boot: "b", Seq: 2}}, time.Now()), 1)
	code, body := getContext(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["live"], true)
	eqAny(t, body["context"].(map[string]any)["used_tokens"], float64(7))
	at, err := time.Parse(time.RFC3339Nano, body["measured_at"].(string))
	testutil.NoError(t, err)
	testutil.True(t, at.Equal(old))
	st, ok := s.bridgeHub.Get(task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, *st.Context.Tokens, int64(7))
	testutil.True(t, st.LastMeasureAt.Equal(old))
}

func TestTaskContext_ReplacedAccountCannotReuseContext(t *testing.T) {
	s, r, task := bridgeAuthorityServer(t)
	testutil.NoError(t, s.db.SetTaskAccount(task.ID, "account-a", false))
	testutil.Equal(t, postMeasure(t, s, task.ID, `{"tokens":100,"categories":{"messages":50,"agents":9}}`, 1), 200)
	_, body := getContext(t, s, task.ID)
	eqAny(t, body["live"], true)
	testutil.NoError(t, s.db.SetTaskAccount(task.ID, "account-b", false))
	r.session.generation = "account-b-launch"
	code, body := getContext(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["live"], false)
	testutil.Nil(t, body["context"])
	testutil.Nil(t, body["measured_at"])
	eqAny(t, body["session_id"], "")
	event := `{"events":[{"type":"session.measure","generation":"account-b-launch","session_id":"account-b-session","boot":"b2","seq":1,"context":{"tokens":20,"categories":{"messages":3,"skills":4}}}]}`
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", event))
	testutil.Equal(t, w.Code, 200)
	var counts map[string]int
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &counts))
	testutil.Equal(t, counts["accepted"], 1)
	current, err := s.db.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, current.Account, "account-b")
	st, ok := s.bridgeHub.Get(task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, st.Generation, "account-b-launch")
	testutil.Equal(t, st.SessionID, "account-b-session")
	testutil.Equal(t, *st.Context.Tokens, int64(20))
	testutil.Equal(t, *st.Context.Categories.Messages, int64(3))
	testutil.Equal(t, *st.Context.Categories.Skills, int64(4))
	testutil.Nil(t, st.Context.Categories.Agents)
	code, body = getContext(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["live"], true)
	eqAny(t, body["session_id"], "account-b-session")
	ctx := body["context"].(map[string]any)
	eqAny(t, ctx["used_tokens"], float64(20))
	cats := ctx["categories"].(map[string]any)
	eqAny(t, cats["messages"], float64(3))
	eqAny(t, cats["skills"], float64(4))
	testutil.Nil(t, cats["agents"])
}
