package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

const tierTOML = twoAccountsTOML

func tierReq(method, url, body, ifMatch string) *http.Request {
	req := authedReq(method, url, body)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	return req
}

func getTiers(t *testing.T, srv *Server) backendTiersJSON {
	t.Helper()
	w := serve(srv, tierReq("GET", "/api/backend-tiers", "", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var got backendTiersJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	return got
}

func stubTierUsage(t *testing.T, claude map[string]usagebudget.Reading, codex *backendtier.Reading) {
	t.Helper()
	restore := agent.SetAccountUsageSource(func(config.Config) map[string]usagebudget.Reading { return claude })
	prev := tierCodexReading
	tierCodexReading = func() (backendtier.Reading, bool) {
		if codex == nil {
			return backendtier.Reading{}, false
		}
		return *codex, true
	}
	t.Cleanup(func() { restore(); tierCodexReading = prev })
}

func TestBackendTiers_Get(t *testing.T) {
	t.Run("empty database list", func(t *testing.T) {
		stubTierUsage(t, nil, nil)
		srv, _ := accountsServer(t, tierTOML)
		got := getTiers(t, srv)
		testutil.Equal(t, got.Source, "database")
		testutil.True(t, got.Editable)
		testutil.Equal(t, len(got.Tiers), 0)
		testutil.True(t, got.Revision != "")
	})

	t.Run("usage matches routing scope and null when unknown", func(t *testing.T) {
		reset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
		probed := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
		stubTierUsage(t,
			map[string]usagebudget.Reading{"personal": {Percentage: 42, ResetAt: reset, LastProbedAt: probed}},
			&backendtier.Reading{Percentage: 77, ResetAt: reset, LastProbedAt: probed})
		srv, d := accountsServer(t, tierTOML)
		testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{
			{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 90},
			{Backend: "claude", Account: "work", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
			{Backend: "codex", Account: "default", Probe: config.ProbeCodexUsage, ThresholdPct: 70},
			{Backend: "codex", Account: "personal", Probe: config.ProbeCodexUsage, ThresholdPct: 70},
			{Backend: "claude", Account: "", Probe: config.ProbeNone},
		}))
		got := getTiers(t, srv)
		testutil.Equal(t, len(got.Tiers), 5)

		u := got.Tiers[0].Usage
		testutil.True(t, u != nil)
		testutil.Equal(t, u.PercentUsed, 42.0)
		testutil.Equal(t, u.Scope, "account")
		testutil.Equal(t, *u.Account, "personal")
		testutil.True(t, u.ResetsAt.Equal(reset))
		testutil.True(t, u.MeasuredAt.Equal(probed))

		testutil.True(t, got.Tiers[1].Usage == nil)

		cx := got.Tiers[2].Usage
		testutil.True(t, cx != nil)
		testutil.Equal(t, cx.PercentUsed, 77.0)
		testutil.Equal(t, cx.Scope, "global")
		testutil.True(t, cx.Account == nil)

		testutil.True(t, got.Tiers[3].Usage == nil)
		testutil.True(t, got.Tiers[4].Usage == nil)
	})

	t.Run("blank codex account follows default_account so it is unknown", func(t *testing.T) {
		stubTierUsage(t, nil, &backendtier.Reading{Percentage: 77, ResetAt: time.Now().Add(time.Hour)})
		srv, d := accountsServer(t, tierTOML)
		testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 70}}))
		testutil.True(t, getTiers(t, srv).Tiers[0].Usage == nil)
	})

	t.Run("raw JSON keeps explicit null", func(t *testing.T) {
		stubTierUsage(t, nil, nil)
		srv, d := accountsServer(t, tierTOML)
		testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{{Backend: "claude", Account: "work", Probe: config.ProbeClaudeUsage, ThresholdPct: 80}}))

		w := serve(srv, tierReq("GET", "/api/backend-tiers", "", ""))
		var raw map[string]any
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		tier1 := raw["tiers"].([]any)[0].(map[string]any)
		v, present := tier1["usage"]
		testutil.True(t, present)
		testutil.True(t, v == nil)
	})

	t.Run("expired reading is null not zero", func(t *testing.T) {
		stubTierUsage(t, map[string]usagebudget.Reading{"personal": {Percentage: 0, ResetAt: time.Now().Add(-time.Hour)}}, nil)
		srv, d := accountsServer(t, tierTOML)
		testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 90}}))
		testutil.True(t, getTiers(t, srv).Tiers[0].Usage == nil)
	})

	t.Run("blank claude account reports the account routing scores", func(t *testing.T) {
		stubTierUsage(t, map[string]usagebudget.Reading{"work": {Percentage: 5, ResetAt: time.Now().Add(time.Hour)}}, nil)
		srv, d := accountsServer(t, tierTOML)
		testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 90}}))
		u := getTiers(t, srv).Tiers[0].Usage
		testutil.True(t, u != nil)
		testutil.Equal(t, *u.Account, "work")
	})

	t.Run("agrees with the usage endpoint", func(t *testing.T) {
		stubTierUsage(t, map[string]usagebudget.Reading{"personal": {Percentage: 12, ResetAt: time.Now().Add(time.Hour)}}, nil)
		srv, d := accountsServer(t, tierTOML)
		testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{
			{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 90},
			{Backend: "claude", Probe: config.ProbeNone},
		}))
		list := getTiers(t, srv)
		w := serve(srv, tierReq("GET", "/api/backend-tiers/usage", "", ""))
		testutil.Equal(t, w.Code, http.StatusOK)
		var got struct {
			Revision string                   `json:"revision"`
			Readings []backendTierReadingJSON `json:"readings"`
		}
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, got.Revision, list.Revision)
		testutil.Equal(t, len(got.Readings), 2)
		testutil.Equal(t, got.Readings[0].Index, 0)
		testutil.Equal(t, got.Readings[0].Usage.PercentUsed, 12.0)
		testutil.Equal(t, got.Readings[1].Index, 1)
		testutil.True(t, got.Readings[1].Usage == nil)
	})

	t.Run("requires auth", func(t *testing.T) {
		srv, _ := accountsServer(t, tierTOML)
		h := authMiddleware(srv.token, srv.db, nil, srv.routes(), "/")
		for _, c := range []struct{ method, path, body string }{
			{"GET", "/api/backend-tiers", ""},
			{"GET", "/api/backend-tiers/usage", ""},
			{"PUT", "/api/backend-tiers", `{"tiers":[]}`},
		} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, strings.NewReader(c.body)))
			testutil.Equal(t, w.Code, http.StatusUnauthorized)
		}
	})
}

func TestBackendTiers_Put(t *testing.T) {
	good := `{"tiers":[{"backend":"codex","account":"personal","probe":"codex_usage","threshold_pct":60},{"backend":"claude","account":"work","probe":"claude_usage","threshold_pct":90},{"backend":"claude","account":"","probe":"none","threshold_pct":0}]}`

	t.Run("saves in order, returns effective list, emits event", func(t *testing.T) {
		stubTierUsage(t, nil, nil)
		rs := captureEvents(t)
		srv, d := accountsServer(t, tierTOML)
		rev := getTiers(t, srv).Revision
		w := serve(srv, tierReq("PUT", "/api/backend-tiers", good, `"`+rev+`"`))
		testutil.Equal(t, w.Code, http.StatusOK)
		var got backendTiersJSON
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		testutil.Equal(t, len(got.Tiers), 3)
		testutil.Equal(t, got.Tiers[0].Backend, "codex")
		testutil.True(t, got.Revision != rev)
		testutil.Equal(t, w.Header().Get("ETag"), `"`+got.Revision+`"`)

		stored, err := d.BackendTiers()
		testutil.NoError(t, err)
		testutil.DeepEqual(t, stored, []config.BackendTier{
			{Backend: "codex", Account: "personal", Probe: "codex_usage", ThresholdPct: 60},
			{Backend: "claude", Account: "work", Probe: "claude_usage", ThresholdPct: 90},
			{Backend: "claude", Probe: "none"},
		})
		testutil.DeepEqual(t, rs.configChanged(), []string{`{"resource":"backend_tiers"}`})
	})

	t.Run("device token may edit tiers", func(t *testing.T) {
		srv, d := accountsServer(t, tierTOML)
		rev := getTiers(t, srv).Revision
		req := acctReq("PUT", "/api/backend-tiers", good, "device")
		req.Header.Set("If-Match", rev)
		testutil.Equal(t, serve(srv, req).Code, http.StatusOK)
		stored, _ := d.BackendTiers()
		testutil.Equal(t, len(stored), 3)
	})

	t.Run("empty list clears", func(t *testing.T) {
		srv, d := accountsServer(t, tierTOML)
		testutil.NoError(t, d.SetBackendTiers([]config.BackendTier{{Backend: "claude", Probe: "none"}}))
		rev := getTiers(t, srv).Revision
		w := serve(srv, tierReq("PUT", "/api/backend-tiers", `{"tiers":[]}`, rev))
		testutil.Equal(t, w.Code, http.StatusOK)
		stored, err := d.BackendTiers()
		testutil.NoError(t, err)
		testutil.Equal(t, len(stored), 0)
	})

	t.Run("second save with stale revision is 412", func(t *testing.T) {
		srv, _ := accountsServer(t, tierTOML)
		rev := getTiers(t, srv).Revision
		testutil.Equal(t, serve(srv, tierReq("PUT", "/api/backend-tiers", good, rev)).Code, http.StatusOK)
		testutil.Equal(t, serve(srv, tierReq("PUT", "/api/backend-tiers", good, rev)).Code, http.StatusPreconditionFailed)
	})

	t.Run("missing If-Match is 428 and stores nothing", func(t *testing.T) {
		srv, d := accountsServer(t, tierTOML)
		rs := captureEvents(t)
		testutil.Equal(t, serve(srv, tierReq("PUT", "/api/backend-tiers", good, "")).Code, http.StatusPreconditionRequired)
		stored, _ := d.BackendTiers()
		testutil.Equal(t, len(stored), 0)
		testutil.Equal(t, len(rs.configChanged()), 0)
	})

	for _, tc := range []struct{ name, body string }{
		{"unknown backend", `{"tiers":[{"backend":"nope","probe":"none","threshold_pct":0}]}`},
		{"unknown probe", `{"tiers":[{"backend":"claude","probe":"magic","threshold_pct":0}]}`},
		{"threshold over 100", `{"tiers":[{"backend":"claude","probe":"claude_usage","threshold_pct":101}]}`},
		{"negative threshold", `{"tiers":[{"backend":"claude","probe":"claude_usage","threshold_pct":-1}]}`},
		{"unknown account", `{"tiers":[{"backend":"claude","account":"ghost","probe":"claude_usage","threshold_pct":50}]}`},
		{"account incompatible with family", `{"tiers":[{"backend":"codex","account":"work","probe":"codex_usage","threshold_pct":50}]}`},
		{"missing tiers key", `{}`},
		{"null tiers", `{"tiers":null}`},
		{"unknown field", `{"tiers":[],"strategy":"headroom"}`},
		{"unknown tier field", `{"tiers":[{"backend":"claude","probe":"none","command":"rm"}]}`},
		{"later tier invalid keeps list intact", `{"tiers":[{"backend":"claude","probe":"none","threshold_pct":0},{"backend":"claude","probe":"bad"}]}`},
	} {
		t.Run("400 "+tc.name, func(t *testing.T) {
			srv, d := accountsServer(t, tierTOML)
			seed := []config.BackendTier{{Backend: "claude", Account: "work", Probe: "claude_usage", ThresholdPct: 90}}
			testutil.NoError(t, d.SetBackendTiers(seed))
			rs := captureEvents(t)
			rev := getTiers(t, srv).Revision
			w := serve(srv, tierReq("PUT", "/api/backend-tiers", tc.body, rev))
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			stored, err := d.BackendTiers()
			testutil.NoError(t, err)
			testutil.DeepEqual(t, stored, seed)
			testutil.Equal(t, len(rs.configChanged()), 0)
		})
	}

	t.Run("config.toml tiers are read-only", func(t *testing.T) {
		toml := tierTOML + `
[backend_routing]
[[backend_routing.tier]]
backend = "claude"
probe = "none"
`
		srv, d := accountsServer(t, toml)
		rs := captureEvents(t)
		got := getTiers(t, srv)
		testutil.Equal(t, got.Source, "config_file")
		testutil.False(t, got.Editable)
		testutil.Equal(t, len(got.Tiers), 1)

		w := serve(srv, tierReq("PUT", "/api/backend-tiers", good, got.Revision))
		testutil.Equal(t, w.Code, http.StatusConflict)
		stored, err := d.BackendTiers()
		testutil.NoError(t, err)
		testutil.Equal(t, len(stored), 0)
		testutil.Equal(t, len(rs.configChanged()), 0)
		testutil.Equal(t, len(getTiers(t, srv).Tiers), 1)
	})

	t.Run("body size is bounded", func(t *testing.T) {
		srv, _ := accountsServer(t, tierTOML)
		rev := getTiers(t, srv).Revision
		body := `{"tiers":[{"backend":"` + strings.Repeat("x", 9000) + `"}]}`
		testutil.Equal(t, serve(srv, tierReq("PUT", "/api/backend-tiers", body, rev)).Code, http.StatusBadRequest)
	})
}

func TestTierRevision(t *testing.T) {
	a := []config.BackendTier{{Backend: "claude", Probe: "none"}, {Backend: "codex", Probe: "none"}}
	b := []config.BackendTier{a[1], a[0]}
	testutil.Equal(t, backendtier.TierRevision(a), backendtier.TierRevision(append([]config.BackendTier(nil), a...)))
	testutil.True(t, backendtier.TierRevision(a) != backendtier.TierRevision(b))
	testutil.True(t, backendtier.TierRevision(nil) != backendtier.TierRevision(a))
}
