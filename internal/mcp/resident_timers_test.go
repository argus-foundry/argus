package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

type residentTimerCancelNudger struct {
	err   error
	calls []struct{ task, id string }
}

func (n *residentTimerCancelNudger) Nudge(string, string, string) error { return nil }
func (n *residentTimerCancelNudger) Cancel(task, id string) error {
	n.calls = append(n.calls, struct{ task, id string }{task, id})
	return n.err
}

func TestResidentTimerCancelQueuedWakeup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			s, d, res, _ := residentHeraFixture(t)
			n := &residentTimerCancelNudger{}
			if fail {
				n.err = errors.New("nudger unavailable")
			}
			s.nudger = n
			now := time.Now()
			timer, err := d.SetResidentTimer("closed loop", now.Add(time.Minute), "", "", now)
			testutil.NoError(t, err)
			_, err = d.SetResidentTimer("still open", now.Add(time.Minute), "", "", now)
			testutil.NoError(t, err)
			msgs, err := d.FireResidentTimers(res.ID, now.Add(2*time.Minute))
			testutil.NoError(t, err)
			testutil.Equal(t, len(msgs), 2)
			_, bad := postTool(t, s, "/mcp?caller="+res.ID, "resident_timer_cancel", fmt.Sprintf(`{"id":%d}`, timer.ID))
			testutil.False(t, bad)
			testutil.Equal(t, len(n.calls), 1)
			testutil.Equal(t, n.calls[0].task, res.ID)
			testutil.Equal(t, n.calls[0].id, msgs[0].ID)
			pending, err := d.PendingResidentTimerDeliveries()
			testutil.NoError(t, err)
			testutil.Equal(t, len(pending), 2)
			testutil.True(t, pending[0].Cancelled)
			testutil.False(t, pending[0].Message.ReadAt.IsZero())
			testutil.False(t, pending[1].Cancelled)
			testutil.True(t, pending[1].Message.ReadAt.IsZero())
		})
	}
}

func TestResidentTimerTools(t *testing.T) {
	s, d, res, _ := residentHeraFixture(t)
	path := "/mcp?caller=" + res.ID
	for _, tc := range []struct{ name, args string }{
		{"resident_timer_set", `{"after":"1h","note":"check Sam"}`},
		{"resident_timer_set", fmt.Sprintf(`{"at":%q,"note":"absolute"}`, time.Now().Add(time.Hour).Format(time.RFC3339))},
		{"resident_timer_schedule", `{"expression":"0 8 * * 1-5","timezone":"America/Los_Angeles","note":"day summary"}`},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			body, bad := postTool(t, s, path, tc.name, tc.args)
			testutil.False(t, bad)
			testutil.Contains(t, body, `"id"`)
		})
	}
	body, bad := postTool(t, s, path, "resident_timer_list", `{}`)
	testutil.False(t, bad)
	var timers []db.ResidentTimer
	testutil.NoError(t, json.Unmarshal([]byte(body), &timers))
	testutil.Equal(t, len(timers), 3)
	_, bad = postTool(t, s, path, "resident_timer_cancel", fmt.Sprintf(`{"id":%d}`, timers[0].ID))
	testutil.False(t, bad)
	list, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 2)
	for _, tc := range []struct{ name, args string }{
		{"resident_timer_set", `{"note":"x"}`},
		{"resident_timer_set", `{"after":"1h","at":"2026-01-01T00:00:00Z","note":"x"}`},
		{"resident_timer_set", `{"after":"-1h","note":"x"}`},
		{"resident_timer_set", `{"after":"bad","note":"x"}`},
		{"resident_timer_set", `{"at":"tomorrow","note":"x"}`},
		{"resident_timer_schedule", `{"note":"x","timezone":"UTC"}`},
		{"resident_timer_schedule", `{"note":"x","expression":"* * * * *","timezone":"UTC"}`},
		{"resident_timer_cancel", `{"id":9999}`},
		{"resident_timer_list", `{"id":{}}`},
	} {
		t.Run("invalid "+tc.name+tc.args, func(t *testing.T) { _, bad := postTool(t, s, path, tc.name, tc.args); testutil.True(t, bad) })
	}
}

func TestResidentTimerIdentity(t *testing.T) {
	s, d, res, _ := residentHeraFixture(t)
	for _, name := range []string{"resident_timer_set", "resident_timer_schedule", "resident_timer_list", "resident_timer_cancel"} {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{"/mcp", "/mcp?caller=foreign"} {
				_, bad := postTool(t, s, path, name, `{"after":"1h","note":"x","id":1}`)
				testutil.True(t, bad)
			}
			for _, token := range []string{"", "wrong"} {
				r := postRaw(t, s, "/mcp?caller="+res.ID, token, "tools/call", map[string]any{"name": name, "arguments": map[string]any{"after": "1h", "note": "x", "id": 1}})
				raw, _ := json.Marshal(r)
				testutil.Contains(t, string(raw), `"isError":true`)
			}
		})
	}
	timers, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(timers), 0)
}

func TestResidentTimerToolVisibility(t *testing.T) {
	s, _, res, _ := residentHeraFixture(t)
	for _, tc := range []struct {
		name, path, token string
		want              bool
	}{
		{"resident", "/mcp?caller=" + res.ID, armResidentToken(t, s), true},
		{"missing token", "/mcp?caller=" + res.ID, "", false},
		{"unbound", "/mcp", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := postRaw(t, s, tc.path, tc.token, "tools/list", map[string]any{})
			raw, err := json.Marshal(r.Result)
			testutil.NoError(t, err)
			var list ToolsListResult
			testutil.NoError(t, json.Unmarshal(raw, &list))
			for _, name := range []string{"resident_timer_set", "resident_timer_schedule", "resident_timer_list", "resident_timer_cancel"} {
				found := false
				for _, tool := range list.Tools {
					if tool.Name == name {
						found = true
					}
				}
				testutil.Equal(t, found, tc.want)
			}
		})
	}
}
