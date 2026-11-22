package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// GitSignFunc signs data in namespace with the agent signing key and returns
// an armored SSHSIG signature (add-agent-signing-key). The daemon wires it from
// a gitsign.KeyHolder; taskID is attribution only.
type GitSignFunc func(ctx context.Context, taskID, namespace string, data []byte) ([]byte, error)

// SetGitSigner wires POST /api/git/sign. Without it the endpoint answers 503.
func (s *Server) SetGitSigner(f GitSignFunc) {
	s.gitSigner = f
}

// maxGitSignBody bounds the JSON body: a commit object (message included) is
// far smaller, and base64 adds a third.
const maxGitSignBody = 16 << 20

// gitSignNamespace is the only namespace the endpoint signs.
const gitSignNamespace = "git"

type gitSignRequest struct {
	Namespace string `json:"namespace"`
	Data      string `json:"data"`
	TaskID    string `json:"task_id"`
}

// handleGitSign signs a commit or tag payload for the `argus -Y sign` helper
// that agent sessions use as gpg.ssh.program. Authenticated like every /api/*
// route. Bad bodies and non-git namespaces are refused before the signer
// runs. Every request logs its task and outcome, never the data, the
// signature, or key material.
func (s *Server) handleGitSign(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxGitSignBody)
	var req gitSignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logGitSign("", "invalid JSON body")
		writeErr(w, http.StatusBadRequest, "expected a JSON body {namespace, data, task_id}", nil)
		return
	}
	task := logTaskID(req.TaskID)
	if req.Namespace != gitSignNamespace {
		logGitSign(task, "namespace refused")
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("namespace %q refused: only %q may be signed", req.Namespace, gitSignNamespace), nil)
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		logGitSign(task, "data is not standard base64")
		writeErr(w, http.StatusBadRequest, "data must be standard base64", nil)
		return
	}
	if s.gitSigner == nil {
		cause := "agent commit signing not configured (git.agent_commit_signing is not agent-key)"
		logGitSign(task, cause)
		writeErr(w, http.StatusServiceUnavailable, cause, nil)
		return
	}
	sig, err := s.gitSigner(r.Context(), req.TaskID, req.Namespace, data)
	if err == nil && len(sig) == 0 {
		err = errors.New("signer returned an empty signature")
	}
	if err != nil {
		logGitSign(task, err.Error())
		writeErr(w, http.StatusServiceUnavailable, "sign", err)
		return
	}
	logGitSign(task, "")
	writeJSON(w, http.StatusOK, map[string]string{"signature": string(sig)})
}

// logGitSign records one signing outcome; an empty cause means success. It
// writes to uxlog and to slog: the daemon never calls uxlog.Init, and runDaemon
// points slog at ~/.argus/daemon.log, so slog is what lands there.
func logGitSign(task, cause string) {
	var line string
	if cause == "" {
		line = fmt.Sprintf("[agent-git] sign task=%s ok", task)
	} else {
		line = fmt.Sprintf("[agent-git] sign task=%s err=%s", task, oneLine(cause))
	}
	uxlog.Log("%s", line)
	slog.Info(line)
}

// logTaskID makes a caller-supplied task ID safe for a single log line.
func logTaskID(id string) string {
	id = oneLine(id)
	if len(id) > 128 {
		id = id[:128]
	}
	if id == "" {
		return "-"
	}
	return id
}

// oneLine replaces control characters and whitespace runs so a value cannot
// forge extra log lines.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
}
