package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestScheduleHandlers_ModelRejection(t *testing.T) {
	for _, source := range []string{"explicit", "project", "default", "routing"} {
		t.Run(source, func(t *testing.T) {
			for _, once := range []bool{false, true} {
				t.Run(map[bool]string{false: "recurring", true: "once"}[once], func(t *testing.T) {
					srv, d := testServer(t)
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
					body := map[string]any{"name": "schedule", "project": "proj", "prompt": "p", "backend": backend, "model": "gpt-9"}
					if once {
						body["run_once_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
					} else {
						body["schedule"] = "@daily"
					}
					raw, err := json.Marshal(body)
					testutil.NoError(t, err)
					w := httptest.NewRecorder()
					srv.routes().ServeHTTP(w, authedReq("POST", "/api/schedules", string(raw)))
					testutil.Equal(t, w.Code, http.StatusUnprocessableEntity)
					var rejection modelRejection
					testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &rejection))
					testutil.Equal(t, rejection.Code, "model_not_allowed")
					testutil.Equal(t, rejection.Backend, "cl")
					testutil.Equal(t, rejection.RequestedModel, "gpt-9")
					testutil.DeepEqual(t, rejection.AllowedModels, agent.KnownModels("claude"))
					rows, err := d.Schedules()
					testutil.NoError(t, err)
					testutil.Equal(t, len(rows), 0)
					body["model"] = "opus"
					raw, err = json.Marshal(body)
					testutil.NoError(t, err)
					w = httptest.NewRecorder()
					srv.routes().ServeHTTP(w, authedReq("POST", "/api/schedules", string(raw)))
					testutil.Equal(t, w.Code, http.StatusCreated)
					var saved scheduleJSON
					testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
					before, err := d.GetSchedule(saved.ID)
					testutil.NoError(t, err)
					w = httptest.NewRecorder()
					srv.routes().ServeHTTP(w, authedReq("PUT", "/api/schedules/"+saved.ID, `{"model":"gpt-9"}`))
					testutil.Equal(t, w.Code, http.StatusUnprocessableEntity)
					after, err := d.GetSchedule(saved.ID)
					testutil.NoError(t, err)
					testutil.DeepEqual(t, after, before)
					w = httptest.NewRecorder()
					srv.routes().ServeHTTP(w, authedReq("PUT", "/api/schedules/"+saved.ID, `{"model":"sonnet"}`))
					testutil.Equal(t, w.Code, http.StatusOK)
					after, err = d.GetSchedule(saved.ID)
					testutil.NoError(t, err)
					testutil.Equal(t, after.Model, "sonnet")
					testutil.Equal(t, after.LastRunAt, model.ScheduledTask{}.LastRunAt)
				})
			}
		})
	}
}

func TestScheduleHandlers_BackendChangeRevalidatesModel(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.SetBackend("free", config.Backend{Command: "pi"}))
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
	before := &model.ScheduledTask{Name: "existing", Project: "proj", Backend: "free", Model: "provider/model", Prompt: "p", Schedule: "@daily", LastError: "previous error", LastRunAt: time.Now().Add(-time.Hour)}
	testutil.NoError(t, d.AddSchedule(before))
	saved, err := d.GetSchedule(before.ID)
	testutil.NoError(t, err)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("PUT", "/api/schedules/"+before.ID, `{"backend":"cl"}`))
	testutil.Equal(t, w.Code, http.StatusUnprocessableEntity)
	after, err := d.GetSchedule(before.ID)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, saved)
	w = httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("PUT", "/api/schedules/"+before.ID, `{"backend":"cl","model":""}`))
	testutil.Equal(t, w.Code, http.StatusOK)
	after, err = d.GetSchedule(before.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, after.Model, "")
	testutil.Equal(t, after.LastRunAt, saved.LastRunAt)
}
