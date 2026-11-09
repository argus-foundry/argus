package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drn/argus/internal/backendtier"
	"github.com/drn/argus/internal/claudeaccount"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/usagebudget"
)

func TestHandleUsageReturnsCachedWindowsWithoutAuthLookups(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	oldCodex, oldClaude := codexUsageSnapshot, claudeUsageSnapshot
	codexUsageSnapshot = func() (backendtier.Reading, bool) {
		return backendtier.Reading{Percentage: 0, LastProbedAt: now}, true // depleted hold may have no reset
	}
	claudeUsageSnapshot = func(account string, _ time.Time) (usagebudget.Reading, bool) {
		if account != "work" {
			return usagebudget.Reading{}, false
		}
		return usagebudget.Reading{
			FiveHour:     &usagebudget.Window{Percentage: 0, ResetAt: now.Add(time.Hour), LastProbedAt: now},
			SevenDay:     &usagebudget.Window{Percentage: 66, ResetAt: now.Add(48 * time.Hour), LastProbedAt: now},
			LastProbedAt: now,
		}, true
	}
	t.Cleanup(func() { codexUsageSnapshot, claudeUsageSnapshot = oldCodex, oldClaude })

	claudeLookups, codexLookups := 0, 0
	stubAuth(t, func(context.Context, string) (claudeaccount.Status, error) {
		claudeLookups++
		return claudeaccount.Status{}, nil
	})
	stubCodexLogin(t, func(context.Context, string) (bool, error) {
		codexLookups++
		return false, nil
	})

	srv, d := accountsServer(t, twoAccountsTOML)
	handler := authMiddleware("test-token", d, nil, srv.routes())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/usage", nil))
	testutil.Equal(t, w.Code, http.StatusUnauthorized)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, authedReq("GET", "/api/usage", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var got providerUsageJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Codex.WeeklyPct, 0.0)
	testutil.Equal(t, got.Codex.WeeklyResetsAt, "")
	var work *claudeUsageAccountJSON
	for i := range got.Claude {
		if got.Claude[i].Name == "work" {
			work = &got.Claude[i]
		}
	}
	testutil.True(t, work != nil && work.Usage != nil)
	testutil.Equal(t, *work.Usage.FiveHourPct, 0.0)
	testutil.Equal(t, *work.Usage.WeeklyPct, 66.0)
	testutil.Equal(t, claudeLookups, 0)
	testutil.Equal(t, codexLookups, 0)
}

func TestHandleUsageRepresentsMissingSnapshots(t *testing.T) {
	oldCodex, oldClaude := codexUsageSnapshot, claudeUsageSnapshot
	codexUsageSnapshot = func() (backendtier.Reading, bool) { return backendtier.Reading{}, false }
	claudeUsageSnapshot = func(string, time.Time) (usagebudget.Reading, bool) {
		return usagebudget.Reading{}, false
	}
	t.Cleanup(func() { codexUsageSnapshot, claudeUsageSnapshot = oldCodex, oldClaude })

	srv, _ := accountsServer(t, twoAccountsTOML)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/usage", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var got struct {
		Claude []claudeUsageAccountJSON `json:"claude"`
		Codex  *codexUsageJSON          `json:"codex"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Nil(t, got.Codex)
	testutil.True(t, len(got.Claude) > 0)
	for _, account := range got.Claude {
		testutil.Nil(t, account.Usage)
	}
}
