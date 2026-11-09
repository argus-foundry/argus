package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBridgeMixedInvalidMeasurementKeepsLifecycle(t *testing.T) {
	for _, typ := range []string{bridge.TypeNeedsInput, bridge.TypeCleared, bridge.TypeTurnStart, bridge.TypeTurnComplete, bridge.TypeHeartbeat} {
		for _, invalidFirst := range []bool{true, false} {
			name := typ + "/invalid-last"
			if invalidFirst {
				name = typ + "/invalid-first"
			}
			t.Run(name, func(t *testing.T) {
				s, _, task := bridgeAuthorityServer(t)
				sink := &ctxSink{}
				prev := events.SetSink(sink)
				t.Cleanup(func() { events.SetSink(prev) })
				testutil.Equal(t, postMeasure(t, s, task.ID, `{"tokens":100,"categories":{"messages":50}}`, 1), 200)
				busy := typ != bridge.TypeTurnStart
				needs := typ == bridge.TypeCleared
				s.bridgeHub.Ingest(task.ID, []bridge.Event{{Type: bridge.TypeHeartbeat, Generation: "launch", Boot: "b", SessionID: "session", Seq: 2, Busy: &busy, NeedsInput: &needs, Kind: "permission", Confidence: bridge.ConfidencePending}}, time.Now())
				valid := bridge.Event{Type: typ, Generation: "launch", Boot: "b", SessionID: "session", Seq: 4, Kind: "permission", Confidence: bridge.ConfidencePending}
				if typ == bridge.TypeHeartbeat {
					valid.Busy = &busy
					yes := true
					valid.NeedsInput = &yes
				}
				invalid := bridge.Event{Type: bridge.TypeMeasure, Generation: "launch", Boot: "b", SessionID: "session", Seq: 3, Context: &bridge.Context{Tokens: ptr64(-1)}}
				batch := []bridge.Event{invalid, valid}
				if !invalidFirst {
					valid.Seq = 3
					invalid.Seq = 4
					batch = []bridge.Event{valid, invalid}
				}
				data, err := json.Marshal(bridge.Batch{Events: batch})
				testutil.NoError(t, err)
				w := httptest.NewRecorder()
				s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", string(data)))
				testutil.Equal(t, w.Code, 200)
				var counts map[string]int
				testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &counts))
				testutil.Equal(t, counts["accepted"], 1)
				testutil.Equal(t, counts["received"], 2)
				// Exercise the same JSON snapshot and override path the daemon TUI reads.
				data, err = json.Marshal(s.BridgeStates())
				testutil.NoError(t, err)
				var states map[string]bridge.State
				testutil.NoError(t, json.Unmarshal(data, &states))
				st, ok := states[task.ID]
				testutil.True(t, ok)
				testutil.Equal(t, *st.Context.Tokens, int64(100))
				testutil.Equal(t, *st.Context.Categories.Messages, int64(50))
				testutil.Equal(t, st.LastSeq, valid.Seq)
				wantNeeds := typ == bridge.TypeNeedsInput || typ == bridge.TypeHeartbeat
				wantBusy := typ != bridge.TypeTurnComplete
				testutil.Equal(t, st.NeedsInput, wantNeeds)
				testutil.Equal(t, st.Busy, wantBusy)
				testutil.Equal(t, len(bridge.Override(nil, states, bridge.AwaitingInput)) > 0, wantNeeds)
				testutil.Equal(t, len(bridge.Override(nil, states, bridge.Quiescent)) > 0, !wantBusy || wantNeeds)
				testutil.Equal(t, len(sink.ofType(model.EventTypeTaskContextChanged)), 1)
			})
		}
	}
}
