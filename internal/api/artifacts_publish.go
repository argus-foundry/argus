package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/argus-foundry/argus/internal/claudeartifacts"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ArtifactPublisher publishes a page to claude.ai as a named account.
// *claudeartifacts.Publisher satisfies it; the daemon wires the same instance
// the MCP artifact_publish tool uses.
type ArtifactPublisher interface {
	Publish(ctx context.Context, account string, in claudeartifacts.PublishInput) (claudeartifacts.Result, error)
	AvailableAccounts(ctx context.Context, exclude string) []string
}

// SetArtifactPublisher wires POST /api/artifacts/publish. Without it the
// endpoint answers 503.
func (s *Server) SetArtifactPublisher(p ArtifactPublisher) {
	s.artifactPublisher = p
}

// maxPublishBody bounds the multipart body: the page cap plus room for the
// form fields and multipart framing.
const maxPublishBody = claudeartifacts.MaxContentBytes + 1<<20

// publishArtifactResponse is the publish endpoint's 200 body. Status is
// "published" (URL set) or "unavailable" (AvailableAccounts lists the
// accounts whose probe succeeded; omitted when none did).
type publishArtifactResponse struct {
	Status            string   `json:"status"`
	Account           string   `json:"account"`
	URL               string   `json:"url,omitempty"`
	Slug              string   `json:"slug,omitempty"`
	Version           string   `json:"version,omitempty"`
	Title             string   `json:"title,omitempty"`
	AvailableAccounts []string `json:"available_accounts,omitempty"`
}

// handlePublishArtifact publishes an uploaded HTML page to claude.ai as the
// form's account (add-argus-artifact-publish). The body is multipart/form-data
// with fields account (required), title, update (an existing artifact's link)
// and the page as file. Authenticated like every /api/* route. Nothing is
// registered as a task artifact here: the caller already holds the file.
func (s *Server) handlePublishArtifact(w http.ResponseWriter, r *http.Request) {
	if s.artifactPublisher == nil {
		writeErr(w, http.StatusServiceUnavailable, "artifact publishing not configured", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublishBody)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "expected a multipart/form-data body", err)
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck // best-effort temp-file cleanup

	account := strings.TrimSpace(r.FormValue("account"))
	if account == "" {
		writeErr(w, http.StatusBadRequest, "account is required", nil)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file is required", nil)
		return
	}
	defer file.Close() //nolint:errcheck // read-only upload part
	content, err := io.ReadAll(io.LimitReader(file, claudeartifacts.MaxContentBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read file", err)
		return
	}

	in := claudeartifacts.PublishInput{
		Name:      hdr.Filename,
		Content:   content,
		Title:     r.FormValue("title"),
		UpdateURL: strings.TrimSpace(r.FormValue("update")),
	}
	res, err := s.artifactPublisher.Publish(r.Context(), account, in)
	switch {
	case err == nil:
		uxlog.Log("[artifacts] api publish ok: account=%s slug=%s update=%t", account, res.Slug, in.UpdateURL != "")
		writeJSON(w, http.StatusOK, publishArtifactResponse{
			Status: "published", Account: account, URL: res.URL, Slug: res.Slug, Version: res.Version, Title: res.Title,
		})
	case errors.Is(err, claudeartifacts.ErrAccountUnavailable):
		available := s.artifactPublisher.AvailableAccounts(r.Context(), account)
		uxlog.Log("[artifacts] api publish unavailable: account=%s available=%v", account, available)
		writeJSON(w, http.StatusOK, publishArtifactResponse{Status: "unavailable", Account: account, AvailableAccounts: available})
	case errors.Is(err, claudeartifacts.ErrInvalidInput), errors.Is(err, claudeartifacts.ErrNoToken):
		uxlog.Log("[artifacts] api publish rejected: account=%s err=%v", account, err)
		writeErr(w, http.StatusBadRequest, "", err)
	default:
		uxlog.Log("[artifacts] api publish failed: account=%s err=%v", account, err)
		writeErr(w, http.StatusBadGateway, "", err)
	}
}
