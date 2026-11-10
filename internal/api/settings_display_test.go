package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSettingsDisplay_RoundTripEveryStyle(t *testing.T) {
	srv, d := testServer(t)
	h := authMiddleware(srv.token, d, nil, srv.routes())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq("GET", "/api/settings", ""))
	testutil.Contains(t, w.Body.String(), `"display":{"spinner_style":"progress"}`)

	for _, style := range []string{"progress", "dots", "braille", "classic"} {
		t.Run(style, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, authedReq("PUT", "/api/settings", `{"display":{"spinner_style":"`+style+`"}}`))
			testutil.Equal(t, w.Code, http.StatusOK)
			var resp settingsResponse
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			testutil.Equal(t, resp.Display.SpinnerStyle, style)
			testutil.Contains(t, w.Body.String(), `"bridge"`)
			testutil.Contains(t, w.Body.String(), `"defaults"`)

			testutil.Equal(t, d.Config().UI.SpinnerStyle, style)
			g := httptest.NewRecorder()
			h.ServeHTTP(g, authedReq("GET", "/api/settings", ""))
			testutil.Contains(t, g.Body.String(), `"display":{"spinner_style":"`+style+`"}`)
		})
	}
}

func TestSettingsDisplay_InvalidAndOmitted(t *testing.T) {
	srv, d := testServer(t)
	h := authMiddleware(srv.token, d, nil, srv.routes())
	testutil.NoError(t, d.SetConfigValue("ui.spinner", "dots"))
	testutil.NoError(t, d.SetConfigValue("defaults.share_project", "keep"))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq("PUT", "/api/settings", `{"defaults":{"share_project":"lost"},"display":{"spinner_style":"scribe"}}`))
	testutil.Equal(t, w.Code, http.StatusBadRequest)
	testutil.Equal(t, w.Body.String(), "{\"code\":\"invalid_spinner_style\",\"error\":\"invalid spinner style\"}\n")
	testutil.Equal(t, d.Config().UI.SpinnerStyle, "dots")
	testutil.Equal(t, d.Config().Defaults.ShareProject, "keep")

	w = httptest.NewRecorder()
	h.ServeHTTP(w, authedReq("PUT", "/api/settings", `{"kb":{"enabled":true}}`))
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Equal(t, d.Config().UI.SpinnerStyle, "dots")
	testutil.Equal(t, d.Config().Defaults.ShareProject, "keep")

	w = httptest.NewRecorder()
	h.ServeHTTP(w, authedReq("PUT", "/api/settings", `{"display":{}}`))
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Equal(t, d.Config().UI.SpinnerStyle, "dots")
}

func TestSettingsDisplay_RequiresAuth(t *testing.T) {
	srv, d := testServer(t)
	h := authMiddleware(srv.token, d, nil, srv.routes())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PUT", "/api/settings", nil))
	testutil.Equal(t, w.Code, http.StatusUnauthorized)
}
