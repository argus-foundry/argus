package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/claudeaccount"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type recSink struct {
	mu  sync.Mutex
	evs []model.Event
}

func (r *recSink) Emit(ev model.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func (r *recSink) configChanged() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.evs {
		if e.Type == model.EventTypeConfigChanged {
			out = append(out, string(e.Payload))
		}
	}
	return out
}

func captureEvents(t *testing.T) *recSink {
	t.Helper()
	rs := &recSink{}
	prev := events.SetSink(rs)
	t.Cleanup(func() { events.SetSink(prev) })
	return rs
}

func acctReq(method, url, body, auth string) *http.Request {
	req := authedReq(method, url, body)
	if auth != "" {
		req.Header.Set("X-Argus-Auth", auth)
	}
	return req
}

func serveAccountRequest(srv *Server, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

func TestAccountWrite_Create(t *testing.T) {
	t.Run("201 stores row, returns effective account, emits event", func(t *testing.T) {
		rs := captureEvents(t)
		srv, d := accountsServer(t, "")
		w := serveAccountRequest(srv, acctReq("POST", "/api/accounts", `{"name":"work","label":"Work","claude_config_dir":"/tmp/argus-w-claude","codex_home":"/tmp/argus-w-codex"}`, "master"))
		testutil.Equal(t, w.Code, http.StatusCreated)
		var got accountJSON
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, got.Name, "work")
		testutil.Equal(t, got.Label, "Work")
		testutil.True(t, got.Editable)
		testutil.Equal(t, got.Source, accountSourceJSON{Label: "database", ClaudeConfigDir: "database", CodexHome: "database"})
		testutil.Equal(t, got.Supports, accountSupportsJSON{Claude: true, Codex: true})

		rows, err := d.Accounts()
		testutil.NoError(t, err)
		testutil.DeepEqual(t, rows["work"], config.Account{Label: "Work", ClaudeConfigDir: "/tmp/argus-w-claude", CodexHome: "/tmp/argus-w-codex"})
		testutil.DeepEqual(t, rs.configChanged(), []string{`{"name":"work","resource":"accounts"}`})
	})

	for _, tc := range []struct {
		name, body, auth string
		want             int
		setup            string
	}{
		{"device token is forbidden", `{"name":"a","claude_config_dir":"/tmp/argus-a"}`, "device", 403, ""},
		{"no auth stamp is forbidden", `{"name":"a","claude_config_dir":"/tmp/argus-a"}`, "", 403, ""},
		{"reserved default name", `{"name":"default","claude_config_dir":"/tmp/argus-a"}`, "master", 400, ""},
		{"reserved name case-insensitive", `{"name":"Default","claude_config_dir":"/tmp/argus-a"}`, "master", 400, ""},
		{"bad name chars", `{"name":"a b","claude_config_dir":"/tmp/argus-a"}`, "master", 400, ""},
		{"empty name", `{"name":"","claude_config_dir":"/tmp/argus-a"}`, "master", 400, ""},
		{"relative dir", `{"name":"a","claude_config_dir":"rel/dir"}`, "master", 400, ""},
		{"no directories", `{"name":"a"}`, "master", 400, ""},
		{"tool default dir", `{"name":"a","claude_config_dir":"~/.claude"}`, "master", 400, ""},
		{"unknown field", `{"name":"a","claude_config_dir":"/tmp/argus-a","token":"x"}`, "master", 400, ""},
		{"trailing data", `{"name":"a","claude_config_dir":"/tmp/argus-a"}{}`, "master", 400, ""},
		{"oversized body", `{"name":"a","label":"` + strings.Repeat("x", 9000) + `"}`, "master", 400, ""},
		{"duplicate of config.toml account", `{"name":"work","claude_config_dir":"/tmp/argus-other"}`, "master", 409, twoAccountsTOML},
		{"duplicate differing by case", `{"name":"WORK","claude_config_dir":"/tmp/argus-other"}`, "master", 409, twoAccountsTOML},
		{"shared directory", `{"name":"x","claude_config_dir":"/tmp/argus-test-work"}`, "master", 400, twoAccountsTOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs := captureEvents(t)
			srv, d := accountsServer(t, tc.setup)
			before, err := d.Accounts()
			testutil.NoError(t, err)
			w := serveAccountRequest(srv, acctReq("POST", "/api/accounts", tc.body, tc.auth))
			testutil.Equal(t, w.Code, tc.want)
			after, err := d.Accounts()
			testutil.NoError(t, err)
			testutil.Equal(t, len(after), len(before))
			testutil.Equal(t, len(rs.configChanged()), 0)
		})
	}
}

func dbAccountServer(t *testing.T) *Server {
	t.Helper()
	srv, _ := accountsServer(t, "")
	w := serveAccountRequest(srv, acctReq("POST", "/api/accounts", `{"name":"work","label":"Work","claude_config_dir":"/tmp/argus-w-claude","codex_home":"/tmp/argus-w-codex"}`, "master"))
	testutil.Equal(t, w.Code, http.StatusCreated)
	return srv
}

func TestAccountWrite_Patch(t *testing.T) {
	t.Run("partial edit keeps unsupplied fields", func(t *testing.T) {
		rs := captureEvents(t)
		srv := dbAccountServer(t)
		w := serveAccountRequest(srv, acctReq("PATCH", "/api/accounts/work", `{"label":"Work 2"}`, "master"))
		testutil.Equal(t, w.Code, http.StatusOK)
		var got accountJSON
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, got.Label, "Work 2")
		testutil.Equal(t, got.ClaudeConfigDir, "/tmp/argus-w-claude")
		rows, err := srv.db.Accounts()
		testutil.NoError(t, err)
		testutil.DeepEqual(t, rows["work"], config.Account{Label: "Work 2", ClaudeConfigDir: "/tmp/argus-w-claude", CodexHome: "/tmp/argus-w-codex"})
		testutil.DeepEqual(t, rs.configChanged(), []string{`{"name":"work","resource":"accounts"}`, `{"name":"work","resource":"accounts"}`})
	})
	t.Run("empty string clears one directory", func(t *testing.T) {
		srv := dbAccountServer(t)
		w := serveAccountRequest(srv, acctReq("PATCH", "/api/accounts/work", `{"codex_home":""}`, "master"))
		testutil.Equal(t, w.Code, http.StatusOK)
		rows, _ := srv.db.Accounts()
		testutil.Equal(t, rows["work"].CodexHome, "")
		testutil.Equal(t, rows["work"].ClaudeConfigDir, "/tmp/argus-w-claude")
	})
	t.Run("clearing every directory is rejected", func(t *testing.T) {
		srv := dbAccountServer(t)
		w := serveAccountRequest(srv, acctReq("PATCH", "/api/accounts/work", `{"claude_config_dir":"","codex_home":""}`, "master"))
		testutil.Equal(t, w.Code, http.StatusBadRequest)
		rows, _ := srv.db.Accounts()
		testutil.Equal(t, rows["work"].ClaudeConfigDir, "/tmp/argus-w-claude")
	})
	for _, tc := range []struct {
		name, path, body, auth string
		want                   int
	}{
		{"device token forbidden", "/api/accounts/work", `{"label":"x"}`, "device", 403},
		{"empty patch", "/api/accounts/work", `{}`, "master", 400},
		{"invalid dir", "/api/accounts/work", `{"claude_config_dir":"relative"}`, "master", 400},
		{"unknown field", "/api/accounts/work", `{"name":"other"}`, "master", 400},
		{"missing account", "/api/accounts/nope", `{"label":"x"}`, "master", 404},
		{"built-in default", "/api/accounts/default", `{"label":"x"}`, "master", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := dbAccountServer(t)
			w := serveAccountRequest(srv, acctReq("PATCH", tc.path, tc.body, tc.auth))
			testutil.Equal(t, w.Code, tc.want)
			rows, _ := srv.db.Accounts()
			testutil.Equal(t, rows["work"].Label, "Work")
		})
	}
	t.Run("config.toml account is read-only and unchanged in the store", func(t *testing.T) {
		srv, d := accountsServer(t, twoAccountsTOML)
		w := serveAccountRequest(srv, acctReq("PATCH", "/api/accounts/work", `{"label":"Hacked"}`, "master"))
		testutil.Equal(t, w.Code, http.StatusConflict)
		rows, _ := d.Accounts()
		testutil.Equal(t, len(rows), 0)
	})
	t.Run("shared directory with another account", func(t *testing.T) {
		srv := dbAccountServer(t)
		testutil.Equal(t, serveAccountRequest(srv, acctReq("POST", "/api/accounts", `{"name":"two","claude_config_dir":"/tmp/argus-two"}`, "master")).Code, http.StatusCreated)
		w := serveAccountRequest(srv, acctReq("PATCH", "/api/accounts/two", `{"claude_config_dir":"/tmp/argus-w-claude"}`, "master"))
		testutil.Equal(t, w.Code, http.StatusBadRequest)
	})
}

func TestAccountWrite_Delete(t *testing.T) {
	t.Run("204 removes the row and emits event", func(t *testing.T) {
		rs := captureEvents(t)
		srv := dbAccountServer(t)
		w := serveAccountRequest(srv, acctReq("DELETE", "/api/accounts/work", "", "master"))
		testutil.Equal(t, w.Code, http.StatusNoContent)
		rows, _ := srv.db.Accounts()
		testutil.Equal(t, len(rows), 0)
		ev := rs.configChanged()
		testutil.Equal(t, ev[len(ev)-1], `{"name":"work","resource":"accounts"}`)
	})
	for _, tc := range []struct {
		name, path, auth string
		want             int
	}{
		{"device token forbidden", "/api/accounts/work", "device", 403},
		{"missing", "/api/accounts/nope", "master", 404},
		{"built-in default", "/api/accounts/default", "master", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := dbAccountServer(t)
			testutil.Equal(t, serveAccountRequest(srv, acctReq("DELETE", tc.path, "", tc.auth)).Code, tc.want)
			rows, _ := srv.db.Accounts()
			testutil.Equal(t, len(rows), 1)
		})
	}
	t.Run("config.toml account is read-only", func(t *testing.T) {
		srv, _ := accountsServer(t, twoAccountsTOML)
		testutil.Equal(t, serveAccountRequest(srv, acctReq("DELETE", "/api/accounts/work", "", "master")).Code, http.StatusConflict)
	})
}

func TestAccountList_SourceMarkers(t *testing.T) {
	stubAuthNone(t)
	srv, d := accountsServer(t, twoAccountsTOML)
	testutil.NoError(t, d.SetAccount("dbacct", config.Account{ClaudeConfigDir: "/tmp/argus-dbacct"}))
	w := serveAccountRequest(srv, authedReq("GET", "/api/accounts", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var got []accountJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	by := map[string]accountJSON{}
	for _, a := range got {
		by[a.Name] = a
	}
	testutil.Equal(t, by["default"].Source.Label, "builtin")
	testutil.False(t, by["default"].Editable)
	testutil.Equal(t, by["work"].Source.ClaudeConfigDir, "config_file")
	testutil.False(t, by["work"].Editable)
	testutil.Equal(t, by["dbacct"].Source.ClaudeConfigDir, "database")
	testutil.True(t, by["dbacct"].Editable)
	testutil.False(t, strings.Contains(w.Body.String(), "token"))
}

func stubAuthNone(t *testing.T) {
	t.Helper()
	stubAuth(t, func(context.Context, string) (claudeaccount.Status, error) { return claudeaccount.Status{}, nil })
	stubCodexLogin(t, func(context.Context, string) (bool, error) { return false, nil })
}
