package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

const cliPage = "<!DOCTYPE html><html><body>x</body></html>"

// stubDaemon emulates POST /api/artifacts/publish on the local daemon.
func stubDaemon(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (artifactCLIEnv, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		respond(w, r)
	}))
	t.Cleanup(srv.Close)
	return artifactCLIEnv{
		daemon: func() (string, string, error) { return srv.URL, "daemon-master-token", nil },
		http:   srv.Client(),
	}, &n
}

func writeCLIPage(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	testutil.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestArtifactCommand_PublishSuccess(t *testing.T) {
	var gotAuth, gotPath, gotAccount, gotTitle, gotUpdate, gotFile, gotFilename string
	env, n := stubDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.Method + " " + r.URL.Path
		testutil.NoError(t, r.ParseMultipartForm(1<<20))
		gotAccount, gotTitle, gotUpdate = r.FormValue("account"), r.FormValue("title"), r.FormValue("update")
		f, hdr, err := r.FormFile("file")
		testutil.NoError(t, err)
		b, _ := io.ReadAll(f)
		gotFile, gotFilename = string(b), hdr.Filename
		writeJSONTest(w, http.StatusOK, map[string]any{"status": "published", "account": "personal", "url": "https://claude.ai/artifact/S1"})
	})
	path := writeCLIPage(t, "page.html", cliPage)
	var out, errOut bytes.Buffer
	code := artifactCommand([]string{"publish", path, "--account", "personal", "--title", "Weekly", "--update", "https://claude.ai/artifact/S1"}, env, &out, &errOut)
	testutil.Equal(t, code, 0)
	testutil.Equal(t, out.String(), "https://claude.ai/artifact/S1\n")
	testutil.Equal(t, n.Load(), int32(1))
	testutil.Equal(t, gotAuth, "Bearer daemon-master-token")
	testutil.Equal(t, gotPath, "POST /api/artifacts/publish")
	testutil.Equal(t, gotAccount, "personal")
	testutil.Equal(t, gotTitle, "Weekly")
	testutil.Equal(t, gotUpdate, "https://claude.ai/artifact/S1")
	testutil.Equal(t, gotFile, cliPage)
	testutil.Equal(t, gotFilename, "page.html")
}

func TestArtifactCommand_Unavailable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		available []string
		want      string
	}{
		{"others available", []string{"personal", "spare"}, "personal, spare"},
		{"none available", nil, "no other configured account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := stubDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSONTest(w, http.StatusOK, map[string]any{"status": "unavailable", "account": "default", "available_accounts": tc.available})
			})
			var out, errOut bytes.Buffer
			code := artifactCommand([]string{"publish", writeCLIPage(t, "p.html", cliPage), "--account", "default"}, env, &out, &errOut)
			testutil.Equal(t, code, 3)
			testutil.Contains(t, errOut.String(), `"default" is unavailable`)
			testutil.Contains(t, errOut.String(), tc.want)
			testutil.Equal(t, out.String(), "")
		})
	}
}

func TestArtifactCommand_Failures(t *testing.T) {
	daemonErr := func(code int, msg string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) { writeJSONTest(w, code, map[string]any{"error": msg}) }
	}
	for _, tc := range []struct {
		name     string
		args     func() []string
		respond  func(http.ResponseWriter, *http.Request)
		daemon   func() (string, string, error)
		wantCode int
		want     string
		wantReqs int32
	}{
		{name: "no subcommand", args: func() []string { return nil }, wantCode: 2, want: "usage"},
		{name: "unknown subcommand", args: func() []string { return []string{"list"} }, wantCode: 2, want: "unknown artifact subcommand"},
		{name: "missing file", args: func() []string { return []string{"publish", "--account", "x"} }, wantCode: 2, want: "usage"},
		{name: "missing account", args: func() []string { return []string{"publish", writeCLIPage(t, "p.html", cliPage)} }, wantCode: 2, want: "--account is required"},
		{name: "flag missing value", args: func() []string { return []string{"publish", "p.html", "--account"} }, wantCode: 2, want: "--account requires a value"},
		{name: "unknown flag", args: func() []string { return []string{"publish", "p.html", "--bogus", "x"} }, wantCode: 2, want: "unknown flag"},
		{name: "two files", args: func() []string { return []string{"publish", "a.html", "b.html", "--account", "x"} }, wantCode: 2, want: "exactly one file"},
		{name: "invalid local file", args: func() []string {
			return []string{"publish", writeCLIPage(t, "notes.md", cliPage), "--account", "x"}
		}, wantCode: 1, want: ".html or .htm"},
		{name: "daemon not reachable", args: func() []string {
			return []string{"publish", writeCLIPage(t, "p.html", cliPage), "--account", "x"}
		}, daemon: func() (string, string, error) { return "", "", errors.New("connect to daemon: no socket") }, wantCode: 1, want: "no socket"},
		{name: "daemon reports error", args: func() []string {
			return []string{"publish", writeCLIPage(t, "p.html", cliPage), "--account", "work"}
		}, respond: daemonErr(http.StatusBadRequest, `no [artifact_tokens] entry for account "work"`), wantCode: 1, want: "[artifact_tokens]", wantReqs: 1},
		{name: "daemon upstream failure", args: func() []string {
			return []string{"publish", writeCLIPage(t, "p.html", cliPage), "--account", "x"}
		}, respond: daemonErr(http.StatusBadGateway, "re-run claude setup-token"), wantCode: 1, want: "claude setup-token", wantReqs: 1},
		{name: "daemon non-json", args: func() []string {
			return []string{"publish", writeCLIPage(t, "p.html", cliPage), "--account", "x"}
		}, respond: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, wantCode: 1, want: "HTTP 500", wantReqs: 1},
		{name: "unexpected status field", args: func() []string {
			return []string{"publish", writeCLIPage(t, "p.html", cliPage), "--account", "x"}
		}, respond: func(w http.ResponseWriter, _ *http.Request) {
			writeJSONTest(w, http.StatusOK, map[string]any{"status": "weird"})
		}, wantCode: 1, want: "unexpected", wantReqs: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			respond := tc.respond
			if respond == nil {
				respond = func(w http.ResponseWriter, _ *http.Request) { t.Error("daemon should not be called") }
			}
			env, n := stubDaemon(t, respond)
			if tc.daemon != nil {
				env.daemon = tc.daemon
			}
			var out, errOut bytes.Buffer
			code := artifactCommand(tc.args(), env, &out, &errOut)
			testutil.Equal(t, code, tc.wantCode)
			testutil.Contains(t, errOut.String(), tc.want)
			testutil.Equal(t, n.Load(), tc.wantReqs)
		})
	}
}

func TestArtifactCommand_DaemonUnreachableHTTP(t *testing.T) {
	env := artifactCLIEnv{
		daemon: func() (string, string, error) { return "http://127.0.0.1:1", "tok", nil },
		http:   http.DefaultClient,
	}
	var out, errOut bytes.Buffer
	code := artifactCommand([]string{"publish", writeCLIPage(t, "p.html", cliPage), "--account", "x"}, env, &out, &errOut)
	testutil.Equal(t, code, 1)
	testutil.Contains(t, errOut.String(), "publish")
}

func writeJSONTest(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// TestRealArtifactCLIEnv_NoDaemon points HOME at an empty dir so discovery
// fails cleanly without ever dialing the live daemon's socket.
func TestRealArtifactCLIEnv_NoDaemon(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	env := realArtifactCLIEnv()
	testutil.True(t, env.http.Timeout > 0)
	_, _, err := env.daemon()
	testutil.Error(t, err)
}
