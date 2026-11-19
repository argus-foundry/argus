package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestOpenBoardDecisions(t *testing.T) {
	for _, tc := range []struct {
		name           string
		enabled        bool
		status         int
		response, want string
	}{{"opens", true, 200, `{"path":"/decide?role=7","ticket":"secret-ticket"}`, "Decisions opened"}, {"empty", true, 409, `{"code":"no_decisions"}`, "No open decisions"}, {"disabled", false, 200, `{}`, "Turn the API on"}, {"failure", true, 500, `{}`, "Could not open"}, {"bad link", true, 200, `{"path":"https://evil","ticket":"s"}`, "Could not open"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			d := testDB(t)
			testutil.NoError(t, d.SetConfigValue("api.enabled", fmt.Sprint(tc.enabled)))
			app := New(d, agent.NewRunner(nil), false)
			var opened string
			old := browserOpener
			browserOpener = func(url string) error { opened = url; return nil }
			t.Cleanup(func() { browserOpener = old })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				testutil.Equal(t, r.URL.Path, "/api/hera/roles/7/board/decision-link")
				testutil.Equal(t, r.Header.Get("Authorization"), "Bearer fake")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.response)
			}))
			t.Cleanup(server.Close)
			app.maintenanceClientFactory = func() (*localMaintenanceClient, error) {
				return &localMaintenanceClient{baseURL: server.URL, token: "fake", hc: server.Client()}, nil
			}
			testutil.Contains(t, app.openBoardDecisions(7, 0), tc.want)
			if tc.name == "opens" {
				testutil.Equal(t, opened, server.URL+"/decide?role=7#t=secret-ticket")
			} else {
				testutil.Equal(t, opened, "")
			}
			testutil.Equal(t, strings.Contains(app.openBoardDecisions(0, 0), "No open decisions"), true)
		})
	}
}
