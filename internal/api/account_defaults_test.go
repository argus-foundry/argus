package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type defaultsBody struct {
	Global   accountDefaultJSON            `json:"global"`
	Projects map[string]accountDefaultJSON `json:"projects"`
}

func getDefaults(t *testing.T, srv *Server) defaultsBody {
	t.Helper()
	w := serveAccountRequest(srv, authedReq("GET", "/api/account-defaults", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var b defaultsBody
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &b))
	return b
}

func defaultsServer(t *testing.T, toml string) *Server {
	t.Helper()
	srv, d := accountsServer(t, toml)
	testutil.NoError(t, d.SetProject("argus", config.Project{Path: "/tmp/argus-proj"}))
	testutil.NoError(t, d.SetProject("side", config.Project{Path: "/tmp/argus-side"}))
	testutil.NoError(t, d.SetAccount("work", config.Account{ClaudeConfigDir: "/tmp/argus-w"}))
	testutil.NoError(t, d.SetAccount("personal", config.Account{ClaudeConfigDir: "/tmp/argus-p"}))
	return srv
}

func sp(s string) *string { return &s }

func TestAccountDefaults_Get(t *testing.T) {
	t.Run("nothing configured resolves to the builtin default", func(t *testing.T) {
		b := getDefaults(t, defaultsServer(t, ""))
		testutil.DeepEqual(t, b.Global, accountDefaultJSON{Effective: "default", Source: "builtin", Editable: true})
		testutil.DeepEqual(t, b.Projects["argus"], accountDefaultJSON{Effective: "default", Source: "inherited", Editable: true})
	})
	t.Run("explicit, inherited and config.toml owned stay distinguishable", func(t *testing.T) {
		srv := defaultsServer(t, "default_account = \"work\"\n[project_accounts]\nside = \"personal\"\n[accounts.work]\nclaude_config_dir = \"/tmp/argus-tw\"\n[accounts.personal]\nclaude_config_dir = \"/tmp/argus-tp\"\n")
		b := getDefaults(t, srv)
		testutil.DeepEqual(t, b.Global, accountDefaultJSON{Explicit: sp("work"), Effective: "work", Source: "config_file"})
		testutil.DeepEqual(t, b.Projects["argus"], accountDefaultJSON{Effective: "work", Source: "inherited", Editable: true})
		testutil.DeepEqual(t, b.Projects["side"], accountDefaultJSON{Explicit: sp("personal"), Effective: "personal", Source: "config_file"})
	})
}

func TestAccountDefaults_PutGlobal(t *testing.T) {
	t.Run("set then clear, stored state and events checked", func(t *testing.T) {
		rs := captureEvents(t)
		srv := defaultsServer(t, "")
		w := serveAccountRequest(srv, acctReq("PUT", "/api/account-defaults", `{"account":"work"}`, "device"))
		testutil.Equal(t, w.Code, http.StatusOK)
		var got accountDefaultJSON
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.DeepEqual(t, got, accountDefaultJSON{Explicit: sp("work"), Effective: "work", Source: "database", Editable: true})
		testutil.Equal(t, srv.db.Config().DefaultAccount, "work")

		w = serveAccountRequest(srv, acctReq("PUT", "/api/account-defaults", `{"account":null}`, "device"))
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, srv.db.Config().DefaultAccount, "")
		testutil.DeepEqual(t, rs.configChanged(), []string{`{"resource":"account_defaults"}`, `{"resource":"account_defaults"}`})
	})
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"missing property", `{}`, 400},
		{"empty string", `{"account":""}`, 400},
		{"non-string", `{"account":3}`, 400},
		{"unknown account", `{"account":"ghost"}`, 400},
		{"unknown field", `{"account":"work","x":1}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs := captureEvents(t)
			srv := defaultsServer(t, "")
			testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/account-defaults", tc.body, "device")).Code, tc.want)
			testutil.Equal(t, srv.db.Config().DefaultAccount, "")
			testutil.Equal(t, len(rs.configChanged()), 0)
		})
	}
	t.Run("config.toml owned is 409 and unchanged", func(t *testing.T) {
		srv := defaultsServer(t, "default_account = \"work\"\n[accounts.work]\nclaude_config_dir = \"/tmp/argus-tw\"\n")
		w := serveAccountRequest(srv, acctReq("PUT", "/api/account-defaults", `{"account":"personal"}`, "device"))
		testutil.Equal(t, w.Code, http.StatusConflict)
		testutil.Equal(t, srv.db.Config().DefaultAccount, "work")
	})
	t.Run("builtin default accepted as explicit", func(t *testing.T) {
		srv := defaultsServer(t, "")
		testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/account-defaults", `{"account":"default"}`, "device")).Code, http.StatusOK)
	})
}

func TestAccountDefaults_PutProject(t *testing.T) {
	t.Run("set preserves unrelated mappings, emits event with project", func(t *testing.T) {
		rs := captureEvents(t)
		srv := defaultsServer(t, "")
		testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/projects/side/account", `{"account":"personal"}`, "device")).Code, http.StatusOK)
		w := serveAccountRequest(srv, acctReq("PUT", "/api/projects/argus/account", `{"account":"work"}`, "device"))
		testutil.Equal(t, w.Code, http.StatusOK)
		var got accountDefaultJSON
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.DeepEqual(t, got, accountDefaultJSON{Explicit: sp("work"), Effective: "work", Source: "database", Editable: true})
		testutil.DeepEqual(t, srv.db.Config().ProjectAccounts, map[string]string{"side": "personal", "argus": "work"})
		testutil.Equal(t, srv.db.Config().ResolveAccount("", "argus"), "work")

		testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/projects/argus/account", `{"account":null}`, "device")).Code, http.StatusOK)
		testutil.DeepEqual(t, srv.db.Config().ProjectAccounts, map[string]string{"side": "personal"})
		ev := rs.configChanged()
		testutil.Equal(t, ev[len(ev)-1], `{"project":"argus","resource":"account_defaults"}`)
	})
	t.Run("clearing restores the global default", func(t *testing.T) {
		srv := defaultsServer(t, "")
		testutil.NoError(t, srv.db.SetDefaultAccount("work"))
		testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/projects/argus/account", `{"account":"personal"}`, "device")).Code, http.StatusOK)
		w := serveAccountRequest(srv, acctReq("PUT", "/api/projects/argus/account", `{"account":null}`, "device"))
		var got accountDefaultJSON
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.DeepEqual(t, got, accountDefaultJSON{Effective: "work", Source: "inherited", Editable: true})
	})
	t.Run("does not move existing tasks", func(t *testing.T) {
		srv := defaultsServer(t, "")
		tk := newAccountTask(t, srv, "work")
		testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/projects/argus/account", `{"account":"personal"}`, "device")).Code, http.StatusOK)
		got, err := srv.db.Get(tk)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Account, "work")
	})
	for _, tc := range []struct {
		name, path, body string
		want             int
	}{
		{"missing project", "/api/projects/ghost/account", `{"account":"work"}`, 404},
		{"missing property", "/api/projects/argus/account", `{}`, 400},
		{"unknown account", "/api/projects/argus/account", `{"account":"ghost"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := defaultsServer(t, "")
			testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", tc.path, tc.body, "device")).Code, tc.want)
			testutil.Equal(t, len(srv.db.Config().ProjectAccounts), 0)
		})
	}
	t.Run("config.toml owned mapping is 409 and unchanged", func(t *testing.T) {
		srv := defaultsServer(t, "[project_accounts]\nside = \"personal\"\n[accounts.personal]\nclaude_config_dir = \"/tmp/argus-tp\"\n")
		testutil.Equal(t, serveAccountRequest(srv, acctReq("PUT", "/api/projects/side/account", `{"account":"work"}`, "device")).Code, http.StatusConflict)
		testutil.Equal(t, srv.db.Config().ProjectAccounts["side"], "personal")
	})
}

func newAccountTask(t *testing.T, srv *Server, account string) string {
	t.Helper()
	tk := &model.Task{Name: "t", Project: "argus", Account: account, Status: model.StatusInProgress}
	testutil.NoError(t, srv.db.Add(tk))
	return tk.ID
}
