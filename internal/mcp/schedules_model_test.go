package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestScheduleTools_ModelRejection(t *testing.T) {
	for _, source := range []string{"explicit", "project", "default", "routing"} {
		t.Run(source, func(t *testing.T) {
			for _, once := range []bool{false, true} {
				t.Run(map[bool]string{false: "recurring", true: "once"}[once], func(t *testing.T) {
					t.Setenv("HOME", t.TempDir())
					d, err := db.OpenInMemory()
					testutil.NoError(t, err)
					t.Cleanup(func() { _ = d.Close() })
					testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
					proj := config.Project{Path: t.TempDir(), Branch: "HEAD"}
					backend := ""
					switch source {
					case "explicit":
						backend = "cl"
					case "project":
						proj.Backend = "cl"
					case "default":
						testutil.NoError(t, d.SetConfigValue("defaults.backend", "cl"))
					case "routing":
						testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{{Backend: "cl", Probe: config.ProbeNone}}))
					}
					testutil.NoError(t, d.SetProject("proj", proj))
					s := testServer()
					s.SetScheduleManager(d, &mockScheduleRunner{})
					body := map[string]any{"name": "schedule", "project": "proj", "prompt": "p", "backend": backend, "model": "gpt-9"}
					if once {
						body["run_once_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
					} else {
						body["schedule"] = "@daily"
					}
					raw, err := json.Marshal(body)
					testutil.NoError(t, err)
					result := s.toolScheduleCreate(1, raw).Result.(ToolCallResult)
					testutil.True(t, result.IsError)
					testutil.Contains(t, result.Content[0].Text, "model is not allowed for resolved backend")
					testutil.Contains(t, result.Content[0].Text, "cl")
					rows, err := d.Schedules()
					testutil.NoError(t, err)
					testutil.Equal(t, len(rows), 0)
					body["model"] = "opus"
					raw, err = json.Marshal(body)
					testutil.NoError(t, err)
					result = s.toolScheduleCreate(2, raw).Result.(ToolCallResult)
					testutil.False(t, result.IsError)
					rows, err = d.Schedules()
					testutil.NoError(t, err)
					testutil.Equal(t, len(rows), 1)
					before := rows[0]
					raw, err = json.Marshal(map[string]any{"id": before.ID, "model": "gpt-9"})
					testutil.NoError(t, err)
					result = s.toolScheduleUpdate(3, raw).Result.(ToolCallResult)
					testutil.True(t, result.IsError)
					testutil.Contains(t, result.Content[0].Text, "model is not allowed")
					after, err := d.GetSchedule(before.ID)
					testutil.NoError(t, err)
					testutil.DeepEqual(t, after, before)
					raw, err = json.Marshal(map[string]any{"id": before.ID, "model": "sonnet"})
					testutil.NoError(t, err)
					result = s.toolScheduleUpdate(4, raw).Result.(ToolCallResult)
					testutil.False(t, result.IsError)
					after, err = d.GetSchedule(before.ID)
					testutil.NoError(t, err)
					testutil.Equal(t, after.Model, "sonnet")
					testutil.Equal(t, after.LastRunAt, before.LastRunAt)
				})
			}
		})
	}
}

func TestScheduleTools_BackendChangeRevalidatesModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.NoError(t, d.SetBackend("free", config.Backend{Command: "pi"}))
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
	before := &model.ScheduledTask{Name: "existing", Project: "proj", Backend: "free", Model: "provider/model", Prompt: "p", Schedule: "@daily", LastError: "previous error", LastRunAt: time.Now().Add(-time.Hour)}
	testutil.NoError(t, d.AddSchedule(before))
	saved, err := d.GetSchedule(before.ID)
	testutil.NoError(t, err)
	s := testServer()
	s.SetScheduleManager(d, &mockScheduleRunner{})
	raw, err := json.Marshal(map[string]any{"id": before.ID, "backend": "cl"})
	testutil.NoError(t, err)
	result := s.toolScheduleUpdate(1, raw).Result.(ToolCallResult)
	testutil.True(t, result.IsError)
	after, err := d.GetSchedule(before.ID)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, saved)
	raw, err = json.Marshal(map[string]any{"id": before.ID, "backend": "cl", "model": ""})
	testutil.NoError(t, err)
	result = s.toolScheduleUpdate(2, raw).Result.(ToolCallResult)
	testutil.False(t, result.IsError)
	after, err = d.GetSchedule(before.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, after.Model, "")
	testutil.Equal(t, after.LastRunAt, saved.LastRunAt)
}
