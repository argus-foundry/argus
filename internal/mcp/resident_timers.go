package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/db"
)

var residentTimerToolDefs = []Tool{
	{Name: "resident_timer_set", Description: "Resident only: set a purposeful follow-up that wakes your own session through task_inbox. Supply exactly one of at (RFC3339 with offset) or after (Go duration, e.g. 2h30m), and a note (max 4096 bytes). Keep the id with its open loop; cancel when closed. Max 64 active timers.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"at": map[string]interface{}{"type": "string"}, "after": map[string]interface{}{"type": "string"}, "note": map[string]interface{}{"type": "string"}}, "required": []string{"note"}}},
	{Name: "resident_timer_schedule", Description: "Resident only: schedule a purposeful recurring wake-up in your own task_inbox. Use five-field cron, @daily, @weekly or @every 30m; timezone is an IANA name such as America/Los_Angeles. Minimum interval 15 minutes (clock-minute combinations are checked conservatively); missed fires coalesce. No tasks are created.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"expression": map[string]interface{}{"type": "string"}, "timezone": map[string]interface{}{"type": "string"}, "note": map[string]interface{}{"type": "string"}}, "required": []string{"expression", "timezone", "note"}}},
	{Name: "resident_timer_list", Description: "Resident only: list your active follow-up timers and recurring wake-ups. Reconcile these ids with open-loops memory on startup.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{Name: "resident_timer_cancel", Description: "Resident only: cancel a timer by numeric id when its open loop closes. Stops future fires and retracts an unsubmitted queued fire; a wake-up already submitted cannot be recalled.", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "integer"}}, "required": []string{"id"}}},
}

func (s *Server) toolResidentTimer(id interface{}, name string, args json.RawMessage, caller string) *Response {
	if !s.boundResident(caller) {
		return residentToolError(id, "resident timers require the authenticated resident connection")
	}
	d, ok := s.taskDB.(*db.DB)
	if !ok {
		return toolError(id, "resident timer store unavailable")
	}
	var p struct {
		At         string `json:"at"`
		After      string `json:"after"`
		Note       string `json:"note"`
		Expression string `json:"expression"`
		Timezone   string `json:"timezone"`
		ID         int64  `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(id, "invalid timer arguments")
	}
	var result any
	var err error
	now := time.Now()
	switch name {
	case "resident_timer_list":
		result, err = d.ResidentTimers()
	case "resident_timer_cancel":
		err = d.CancelResidentTimer(p.ID)
		if err == nil {
			s.cancelResidentTimerWakeups(d, caller)
		}
		result = fmt.Sprintf("Cancelled timer #%d", p.ID)
	case "resident_timer_set":
		if (p.At == "") == (p.After == "") {
			return toolError(id, "supply exactly one of at or after")
		}
		var due time.Time
		if p.At != "" {
			due, err = time.Parse(time.RFC3339Nano, p.At)
		} else {
			var delay time.Duration
			delay, err = time.ParseDuration(p.After)
			due = now.Add(delay)
		}
		if err == nil {
			result, err = d.SetResidentTimer(p.Note, due, "", "", now)
		}
	case "resident_timer_schedule":
		if p.Expression == "" {
			return toolError(id, "expression is required")
		}
		result, err = d.SetResidentTimer(p.Note, time.Time{}, p.Expression, p.Timezone, now)
	}
	if err != nil {
		residentMCPLog("[resident-timer] %s failed: %v", name, err)
		return toolError(id, err.Error())
	}
	if timer, ok := result.(*db.ResidentTimer); ok {
		residentMCPLog("[resident-timer] set id=%d due=%s recurring=%t", timer.ID, timer.DueAt.Format(time.RFC3339), timer.Expression != "")
	} else {
		residentMCPLog("[resident-timer] %s succeeded id=%d", name, p.ID)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return toolError(id, err.Error())
	}
	return toolResult(id, string(data))
}

// Cancel the live registration immediately, as task_message_ack does. Keep
// durable receipts until the daemon's pass so an unavailable nudger retries.
func (s *Server) cancelResidentTimerWakeups(d *db.DB, caller string) {
	if s.nudger == nil {
		return
	}
	pending, err := d.PendingResidentTimerDeliveries()
	if err != nil {
		residentMCPLog("[resident-timer] cancel wakeup lookup deferred: %v", err)
		return
	}
	for _, p := range pending {
		if !p.Cancelled || p.Message.To != caller {
			continue
		}
		if err := s.nudger.Cancel(caller, p.Message.ID); err != nil {
			residentMCPLog("[resident-timer] cancel wakeup deferred message=%s: %v", p.Message.ID, err)
		} else {
			residentMCPLog("[resident-timer] cancel wakeup message=%s", p.Message.ID)
		}
	}
}
