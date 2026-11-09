package api

import (
	"net/http"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskPRLink_StoreFailureIs500(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Close())
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			w := prReq(srv, method, "id", `{"url":"https://github.com/o/r/pull/1"}`)
			t.Logf("closed-store response %d %s", w.Code, w.Body.String())
			testutil.Equal(t, w.Code, http.StatusInternalServerError)
		})
	}
}

func TestTaskPRLink_NoLinkDeleteIsSilent(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "noop", Name: "n"}))
	sink := installAPISink(t)
	w := prReq(srv, "DELETE", "noop", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	t.Logf("empty link/cache DELETE emitted %d events: %+v", len(sink.events()), sink.events())
	testutil.Equal(t, len(sink.events()), 0)
}

func TestTaskPRLink_ForeignCacheProjectsUnknown(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "foreign", Name: "n"}))
	testutil.NoError(t, d.SetManualPR("foreign", linkURL))
	testutil.NoError(t, d.SetMetaBatch("foreign", "pr", map[string]string{"state": "merged-closed", "lifecycle": "merged", "url": "https://github.com/o/r/pull/1"}))
	got, err := srv.prLinkProjection("foreign")
	testutil.NoError(t, err)
	testutil.Equal(t, got.URL, linkURL)
	testutil.Equal(t, got.State, "unknown")
	testutil.Equal(t, got.Lifecycle, model.PRLifecycleUnknown)
	testutil.Nil(t, got.CheckedAt)
}

func TestTaskPRLink_NoLinkDeletePreservesAutomaticCache(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "auto", Name: "n"}))
	testutil.NoError(t, d.SetMetaBatch("auto", "pr", map[string]string{"state": "approved", "url": linkURL, "lifecycle": "open"}))
	sink := installAPISink(t)
	for i := 0; i < 2; i++ {
		w := prReq(srv, "DELETE", "auto", "")
		testutil.Equal(t, w.Code, http.StatusOK)
	}
	got, err := srv.prLinkProjection("auto")
	testutil.NoError(t, err)
	testutil.Equal(t, got.State, "approved")
	testutil.Equal(t, got.URL, linkURL)
	testutil.Equal(t, len(sink.events()), 0)
}
