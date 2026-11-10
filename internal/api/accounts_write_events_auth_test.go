package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type accountCommitSink struct{ f func(model.Event) }

func (s accountCommitSink) Emit(e model.Event) { s.f(e) }
func TestAccountDefaults_ExplicitBuiltinPreserved(t *testing.T) {
	srv := defaultsServer(t, "")
	testutil.NoError(t, srv.db.SetDefaultAccount("work"))
	w := serveAccountRequest(srv, acctReq("PUT", "/api/projects/argus/account", `{"account":"default"}`, "device"))
	testutil.Equal(t, w.Code, http.StatusOK)
	raw, err := srv.db.GetConfigValue("accounts.projects")
	testutil.NoError(t, err)
	testutil.Equal(t, raw, `{"argus":"default"}`)
	testutil.Equal(t, srv.db.Config().ProjectAccounts["argus"], "default")
	testutil.Equal(t, srv.db.Config().ResolveAccount("", "argus"), "default")
	var got accountDefaultJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.DeepEqual(t, got.Explicit, sp("default"))
	testutil.Equal(t, got.Effective, "default")
	w = serveAccountRequest(srv, acctReq("PUT", "/api/account-defaults", `{"account":"default"}`, "device"))
	testutil.Equal(t, w.Code, http.StatusOK)
	stored, err := srv.db.GetConfigValue("accounts.default")
	testutil.NoError(t, err)
	testutil.Equal(t, stored, "default")
	testutil.Equal(t, srv.db.Config().DefaultAccount, "default")
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.DeepEqual(t, got.Explicit, sp("default"))
	testutil.Equal(t, got.Effective, "default")
}
func TestAccountDefaults_EventAfterCommit(t *testing.T) {
	srv := defaultsServer(t, "")
	var observed []string
	prev := events.SetSink(accountCommitSink{func(e model.Event) {
		if e.Type == model.EventTypeConfigChanged {
			observed = append(observed, srv.db.Config().DefaultAccount)
		}
	}})
	t.Cleanup(func() { events.SetSink(prev) })
	testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/account-defaults", `{"account":"work"}`, "device")).Code, http.StatusOK)
	testutil.DeepEqual(t, observed, []string{"work"})
}
func TestAccountWrite_AuthMiddleware(t *testing.T) {
	for _, principal := range []string{"master", "device", "scope", "invalid", "missing"} {
		t.Run(principal, func(t *testing.T) {
			for _, route := range []struct {
				method, path, body string
				crud               bool
				good               int
			}{
				{"POST", "/api/accounts", `{"name":"new","claude_config_dir":"/tmp/qa-new"}`, true, 201},
				{"PATCH", "/api/accounts/work", `{"label":"new"}`, true, 200},
				{"DELETE", "/api/accounts/work", "", true, 204},
				{"PUT", "/api/account-defaults", `{"account":"work"}`, false, 200},
				{"PUT", "/api/projects/argus/account", `{"account":"work"}`, false, 200},
				{"GET", "/api/account-defaults", "", false, 200},
			} {
				t.Run(route.method+route.path, func(t *testing.T) {
					srv := defaultsServer(t, "")
					token := "test-token"
					switch principal {
					case "device":
						var err error
						token, _, err = MintToken(srv.db, "qa")
						testutil.NoError(t, err)
					case "scope":
						var err error
						token, _, err = MintTokenWithScope(srv.db, "qa", "qa-plugin")
						testutil.NoError(t, err)
					case "invalid":
						token = "bad"
					case "missing":
						token = ""
					}
					r := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
					r.Header.Set("X-Argus-Auth", "master")
					if token != "" {
						r.Header.Set("Authorization", "Bearer "+token)
					}
					w := httptest.NewRecorder()
					authMiddleware("test-token", srv.db, nil, srv.routes()).ServeHTTP(w, r)
					want := route.good
					if principal == "missing" || principal == "invalid" {
						want = 401
					} else if route.crud && principal != "master" {
						want = 403
					}
					testutil.Equal(t, w.Code, want)
				})
			}
		})
	}
}

func TestAccountDefaults_PersistedEventFrames(t *testing.T) {
	srv := defaultsServer(t, "")
	prev := events.SetSink(srv)
	t.Cleanup(func() { events.SetSink(prev) })
	ch, unsubscribe := srv.eventBus.subscribe()
	defer unsubscribe()
	w := serveAccountRequest(srv, acctReq("PUT", "/api/projects/argus/account", `{"account":"work"}`, "device"))
	testutil.Equal(t, w.Code, 200)
	persisted, err := srv.db.EventsSince(0, 0)
	testutil.NoError(t, err)
	testutil.Equal(t, len(persisted), 1)
	if len(persisted) != 1 {
		return
	}
	ev := persisted[0]
	testutil.Equal(t, ev.Type, model.EventTypeConfigChanged)
	testutil.Equal(t, string(ev.Payload), `{"project":"argus","resource":"account_defaults"}`)
	testutil.Equal(t, ev.TaskID, "")
	testutil.True(t, ev.ID > 0)
	select {
	case live := <-ch:
		testutil.Equal(t, live.ID, ev.ID)
	default:
		t.Error("no live event")
	}
	w = httptest.NewRecorder()
	writeSSEEvent(w, w, *ev)
	testutil.Contains(t, w.Body.String(), "event: config.changed\ndata: ")
	testutil.Contains(t, w.Body.String(), `"resource":"account_defaults"`)
}
