package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/claudeartifacts"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const (
	pubTokenPersonal = "tok-ok-PERSONAL-secret"
	pubTokenWork     = "tok-gone-WORK-secret"
	pubTokenOther    = "tok-gone-OTHER-secret"
	publishPage      = "<!DOCTYPE html><html><body>report</body></html>"
)

// fakeArtifactsEndpoint stands in for the claude.ai artifacts endpoint: a
// "tok-ok" bearer publishes, a "tok-gone" bearer gets the exhausted-account
// refusal. It counts every request it sees.
func fakeArtifactsEndpoint(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		var body struct {
			ID     any `json:"id"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-gone") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "not found: the artifact tools on this server aren't available to this session")
			return
		}
		payload := map[string]any{"slug": "SLUG1", "url": "https://claude.ai/artifact/SLUG1", "version": "v1", "title": "T"}
		if body.Params.Name == "get_artifact" {
			payload = map[string]any{"slug": body.Params.Arguments["slug"], "version": "v1"}
		}
		text, _ := json.Marshal(payload)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(text)}},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func testPublishServer(t *testing.T, withTasks bool) (*Server, *mockArtifactStore, *atomic.Int32) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	var s *Server
	store := &mockArtifactStore{}
	if withTasks {
		s, _, _ = testServerWithTasks()
		s.SetArtifactManager(store)
	} else {
		s = testServer()
	}
	endpoint, n := fakeArtifactsEndpoint(t)
	tokens := map[string]string{
		"src-personal": pubTokenPersonal,
		"src-work":     pubTokenWork,
		"src-other":    pubTokenOther,
	}
	cfg := config.Config{ArtifactTokens: map[string]string{
		"personal": "env://src-personal",
		"default":  "env://src-work",
		"other":    "env://src-other",
	}}
	s.SetArtifactPublisher(&claudeartifacts.Publisher{
		Client: &claudeartifacts.Client{HTTP: endpoint.Client(), BaseURL: endpoint.URL},
		Config: func() config.Config { return cfg },
		Resolve: func(_ config.SecretsConfig, source string) (string, bool) {
			v, ok := tokens[strings.TrimPrefix(source, "env://")]
			return v, ok
		},
	})
	return s, store, n
}

// captureLog redirects the standard logger (the MCP server's log sink) for
// the rest of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &buf
}

func callTool(t *testing.T, s *Server, name string, args map[string]any) ToolCallResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	resp := doRequest(t, s, "tools/call", ToolCallParams{Name: name, Arguments: raw})
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	var cr ToolCallResult
	_ = json.Unmarshal(b, &cr)
	return cr
}

type publishResultJSON struct {
	Status            string   `json:"status"`
	Account           string   `json:"account"`
	URL               string   `json:"url"`
	Slug              string   `json:"slug"`
	Artifact          string   `json:"artifact"`
	AvailableAccounts []string `json:"available_accounts"`
	Message           string   `json:"message"`
}

func decodePublishResult(t *testing.T, cr ToolCallResult) publishResultJSON {
	t.Helper()
	var r publishResultJSON
	if err := json.Unmarshal([]byte(cr.Content[0].Text), &r); err != nil {
		t.Fatalf("result is not JSON: %v (%s)", err, cr.Content[0].Text)
	}
	return r
}

func assertNoToken(t *testing.T, texts ...string) {
	t.Helper()
	for _, s := range texts {
		for _, tok := range []string{pubTokenPersonal, pubTokenWork, pubTokenOther} {
			testutil.False(t, strings.Contains(s, tok))
		}
	}
}

func TestArtifactPublish_Success(t *testing.T) {
	s, store, n := testPublishServer(t, true)
	logs := captureLog(t)
	src := writeTempFile(t, "report.html", publishPage)

	cr := callTool(t, s, "artifact_publish", map[string]any{"path": src, "account": "personal", "title": "Report", "id": "abc123"})
	testutil.False(t, cr.IsError)
	r := decodePublishResult(t, cr)
	testutil.Equal(t, r.Status, "published")
	testutil.Equal(t, r.Account, "personal")
	testutil.Equal(t, r.URL, "https://claude.ai/artifact/SLUG1")
	testutil.Equal(t, r.Slug, "SLUG1")
	testutil.Equal(t, n.Load(), int32(1))
	testutil.Equal(t, len(store.saved), 0) // nothing registered on success
	testutil.Contains(t, logs.String(), "[artifacts] publish ok")
	assertNoToken(t, cr.Content[0].Text, logs.String())
}

func TestArtifactPublish_Update(t *testing.T) {
	s, _, n := testPublishServer(t, true)
	src := writeTempFile(t, "report.html", publishPage)
	cr := callTool(t, s, "artifact_publish", map[string]any{
		"path": src, "account": "personal", "update": "https://claude.ai/artifact/SLUG1", "id": "abc123",
	})
	testutil.False(t, cr.IsError)
	testutil.Equal(t, decodePublishResult(t, cr).URL, "https://claude.ai/artifact/SLUG1")
	testutil.Equal(t, n.Load(), int32(2)) // get_artifact + publish_version
}

func TestArtifactPublish_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(src string) map[string]any
		want string
	}{
		{"no token configured", func(src string) map[string]any {
			return map[string]any{"path": src, "account": "work", "id": "abc123"}
		}, "[artifact_tokens]"},
		{"missing path", func(string) map[string]any { return map[string]any{"account": "personal"} }, "path is required"},
		{"missing account", func(src string) map[string]any { return map[string]any{"path": src} }, "account is required"},
		{"non-html file", func(string) map[string]any {
			return map[string]any{"path": writeTempFile(t, "notes.md", publishPage), "account": "personal", "id": "abc123"}
		}, ".html or .htm"},
		{"missing file", func(string) map[string]any {
			return map[string]any{"path": "/nonexistent/x.html", "account": "personal", "id": "abc123"}
		}, "x.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, n := testPublishServer(t, true)
			logs := captureLog(t)
			src := writeTempFile(t, "report.html", publishPage)
			cr := callTool(t, s, "artifact_publish", tc.args(src))
			testutil.True(t, cr.IsError)
			testutil.Contains(t, cr.Content[0].Text, tc.want)
			testutil.Equal(t, n.Load(), int32(0))
			testutil.Equal(t, len(store.saved), 0)
			assertNoToken(t, cr.Content[0].Text, logs.String())
		})
	}
}

func TestArtifactPublish_UnavailableRegistersAndListsAccounts(t *testing.T) {
	s, store, _ := testPublishServer(t, true)
	logs := captureLog(t)
	src := writeTempFile(t, "report.html", publishPage)

	cr := callTool(t, s, "artifact_publish", map[string]any{"path": src, "account": "default", "title": "Weekly", "cwd": "/tmp/worktrees/myapp/fix-login"})
	testutil.False(t, cr.IsError) // a result, not a tool error
	r := decodePublishResult(t, cr)
	testutil.Equal(t, r.Status, "unavailable")
	testutil.Equal(t, r.Account, "default")
	testutil.Equal(t, r.Artifact, "report.html")
	// "other" is also refused, so only "personal" is offered.
	testutil.DeepEqual(t, r.AvailableAccounts, []string{"personal"})

	testutil.Equal(t, len(store.saved), 1)
	testutil.Equal(t, store.saved[0].TaskID, "abc123")
	testutil.Equal(t, store.saved[0].Filename, "report.html")
	testutil.Equal(t, store.saved[0].Name, "Weekly")
	testutil.Equal(t, store.saved[0].Type, model.ArtifactHTML)
	got, err := os.ReadFile(filepath.Join(agent.ArtifactsDir("abc123"), "report.html"))
	testutil.NoError(t, err)
	testutil.Equal(t, string(got), publishPage)

	testutil.Contains(t, logs.String(), "[artifacts] publish unavailable")
	assertNoToken(t, cr.Content[0].Text, logs.String())
}

// TestArtifactPublish_RequiresCallingTask: the daemon only reads a file on
// behalf of a known task, so it can confine the read to that task's sandbox.
func TestArtifactPublish_RequiresCallingTask(t *testing.T) {
	for _, tc := range []struct {
		name      string
		withTasks bool
		args      map[string]any
		want      string
	}{
		{"no task management wired", false, map[string]any{"account": "personal", "id": "abc123"}, "calling task"},
		{"no id or cwd", true, map[string]any{"account": "personal"}, "calling task"},
		{"unknown task id", true, map[string]any{"account": "personal", "id": "nope"}, "task not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, n := testPublishServer(t, tc.withTasks)
			tc.args["path"] = writeTempFile(t, "report.html", publishPage)
			cr := callTool(t, s, "artifact_publish", tc.args)
			testutil.True(t, cr.IsError)
			testutil.Contains(t, cr.Content[0].Text, tc.want)
			testutil.Equal(t, n.Load(), int32(0))
			testutil.Equal(t, len(store.saved), 0)
		})
	}
}

// confineFixture gives the calling task (abc123) a worktree and a temp-style
// root as its only writable roots, a deny-read dir inside the worktree, and
// an "outside" dir no root covers.
type confineFixture struct {
	worktree, tmpRoot, outside, denied string
}

func confineTask(t *testing.T, s *Server, sandboxed bool) confineFixture {
	t.Helper()
	base := t.TempDir()
	f := confineFixture{
		worktree: filepath.Join(base, "wt"),
		tmpRoot:  filepath.Join(base, "tmp"),
		outside:  filepath.Join(base, "outside"),
	}
	f.denied = filepath.Join(f.worktree, "secrets")
	for _, d := range []string{f.worktree, f.tmpRoot, f.outside, f.denied} {
		testutil.NoError(t, os.MkdirAll(d, 0o700))
	}
	tasks := s.taskDB.(*mockTaskDB).tasks
	tasks[0].Sandboxed = sandboxed
	tasks[0].Worktree = f.worktree
	s.pathPolicy = func(got *model.Task) (write, deny []string) {
		if got.ID != "abc123" {
			return []string{got.Worktree}, nil
		}
		return []string{f.worktree, f.tmpRoot}, []string{f.denied}
	}
	return f
}

func writeAt(t *testing.T, path, content string) string {
	t.Helper()
	testutil.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	testutil.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// confineCases are the path shapes every agent-path tool must handle the
// same way. An empty want means allowed.
func confineCases(t *testing.T, name string) []struct {
	name string
	path func(f confineFixture) string
	want string
} {
	return []struct {
		name string
		path func(f confineFixture) string
		want string
	}{
		{"inside worktree", func(f confineFixture) string { return writeAt(t, filepath.Join(f.worktree, "out", name), publishPage) }, ""},
		{"temp root", func(f confineFixture) string { return writeAt(t, filepath.Join(f.tmpRoot, name), publishPage) }, ""},
		{"outside the roots", func(f confineFixture) string { return writeAt(t, filepath.Join(f.outside, name), publishPage) }, "outside task abc123"},
		{"symlink final component", func(f confineFixture) string {
			target := writeAt(t, filepath.Join(f.outside, "id_ed25519"+filepath.Ext(name)), publishPage)
			link := filepath.Join(f.worktree, name)
			testutil.NoError(t, os.Symlink(target, link))
			return link
		}, "is a symlink;"},
		{"symlinked parent pointing outside", func(f confineFixture) string {
			writeAt(t, filepath.Join(f.outside, name), publishPage)
			testutil.NoError(t, os.Symlink(f.outside, filepath.Join(f.worktree, "sub")))
			return filepath.Join(f.worktree, "sub", name)
		}, "outside task abc123"},
		{"dot-dot escape", func(f confineFixture) string {
			writeAt(t, filepath.Join(f.outside, name), publishPage)
			return filepath.Join(f.worktree, "..", "outside", name)
		}, "outside task abc123"},
		{"hard-linked file", func(f confineFixture) string {
			target := writeAt(t, filepath.Join(f.outside, name), publishPage)
			link := filepath.Join(f.worktree, name)
			testutil.NoError(t, os.Link(target, link))
			return link
		}, "more than one hard link"},
		{"under a deny-read path", func(f confineFixture) string { return writeAt(t, filepath.Join(f.denied, name), publishPage) }, "will not read it"},
		{"missing file", func(f confineFixture) string { return filepath.Join(f.worktree, "nope"+filepath.Ext(name)) }, "no such file"},
	}
}

func TestArtifactPublish_ConfinesPath(t *testing.T) {
	for _, sandboxed := range []bool{true, false} {
		for _, tc := range confineCases(t, "report.html") {
			t.Run(fmt.Sprintf("sandboxed=%t/%s", sandboxed, tc.name), func(t *testing.T) {
				s, store, n := testPublishServer(t, true)
				f := confineTask(t, s, sandboxed)
				cr := callTool(t, s, "artifact_publish", map[string]any{"path": tc.path(f), "account": "personal", "id": "abc123"})
				if tc.want == "" {
					testutil.False(t, cr.IsError)
					testutil.Equal(t, decodePublishResult(t, cr).Status, "published")
					testutil.Equal(t, n.Load(), int32(1))
					return
				}
				testutil.True(t, cr.IsError)
				testutil.Contains(t, cr.Content[0].Text, tc.want)
				testutil.Equal(t, n.Load(), int32(0))
				testutil.Equal(t, len(store.saved), 0)
			})
		}
	}
}

// TestArtifactPublish_SpoofedTaskConfinedToItsRoots: the caller's id is
// self-reported, so naming another task only ever widens the read to THAT
// task's own roots, never to an arbitrary path.
func TestArtifactPublish_SpoofedTaskConfinedToItsRoots(t *testing.T) {
	s, _, n := testPublishServer(t, true)
	f := confineTask(t, s, true)
	other := filepath.Join(t.TempDir(), "otherwt")
	s.taskDB.(*mockTaskDB).tasks[1].Worktree = other // def456, unsandboxed

	cr := callTool(t, s, "artifact_publish", map[string]any{"path": writeAt(t, filepath.Join(f.outside, "r.html"), publishPage), "account": "personal", "id": "def456"})
	testutil.True(t, cr.IsError)
	testutil.Contains(t, cr.Content[0].Text, "outside task def456")
	testutil.Equal(t, n.Load(), int32(0))
}

func TestArtifactRegister_ConfinesPath(t *testing.T) {
	for _, tc := range confineCases(t, "notes.txt") {
		t.Run(tc.name, func(t *testing.T) {
			s, store, _ := testPublishServer(t, true)
			f := confineTask(t, s, true)
			cr := callArtifactRegister(t, s, map[string]any{"path": tc.path(f), "id": "abc123"})
			if tc.want == "" {
				testutil.False(t, cr.IsError)
				testutil.Equal(t, len(store.saved), 1)
				return
			}
			testutil.True(t, cr.IsError)
			testutil.Contains(t, cr.Content[0].Text, tc.want)
			testutil.Equal(t, len(store.saved), 0)
		})
	}
}

func TestArtifactRegister_ConfinesFolder(t *testing.T) {
	t.Run("folder outside refused", func(t *testing.T) {
		s, store, _ := testPublishServer(t, true)
		f := confineTask(t, s, false)
		writeAt(t, filepath.Join(f.outside, "shots", "a.png"), "png")
		cr := callArtifactRegister(t, s, map[string]any{"path": filepath.Join(f.outside, "shots"), "id": "abc123"})
		testutil.True(t, cr.IsError)
		testutil.Contains(t, cr.Content[0].Text, "outside task abc123")
		testutil.Equal(t, len(store.saved), 0)
	})
	t.Run("folder under deny-read refused", func(t *testing.T) {
		s, store, _ := testPublishServer(t, true)
		f := confineTask(t, s, true)
		writeAt(t, filepath.Join(f.denied, "shots", "a.png"), "png")
		cr := callArtifactRegister(t, s, map[string]any{"path": filepath.Join(f.denied, "shots"), "id": "abc123"})
		testutil.True(t, cr.IsError)
		testutil.Contains(t, cr.Content[0].Text, "will not read it")
		testutil.Equal(t, len(store.saved), 0)
	})
	t.Run("hard-linked member skipped", func(t *testing.T) {
		s, store, _ := testPublishServer(t, true)
		f := confineTask(t, s, true)
		dir := filepath.Join(f.tmpRoot, "shots")
		writeAt(t, filepath.Join(dir, "a.png"), "png")
		secret := writeAt(t, filepath.Join(f.outside, "secret.png"), "secret")
		testutil.NoError(t, os.Link(secret, filepath.Join(dir, "b.png")))
		cr := callArtifactRegister(t, s, map[string]any{"path": dir, "id": "abc123"})
		testutil.False(t, cr.IsError)
		testutil.Equal(t, len(store.saved), 1)
		testutil.Equal(t, store.saved[0].Filename, "shots/a.png")
	})
}

func TestConfinedRead(t *testing.T) {
	s, _, _ := testPublishServer(t, true)
	f := confineTask(t, s, true)
	task := s.taskDB.(*mockTaskDB).tasks[0]
	p := writeAt(t, filepath.Join(f.worktree, "big.html"), "0123456789")
	t.Run("within cap", func(t *testing.T) {
		b, err := s.confinedRead(task, p, 10)
		testutil.NoError(t, err)
		testutil.Equal(t, string(b), "0123456789")
	})
	t.Run("over cap", func(t *testing.T) {
		_, err := s.confinedRead(task, p, 9)
		testutil.Error(t, err)
		testutil.Contains(t, err.Error(), "larger than")
	})
	t.Run("directory", func(t *testing.T) {
		_, err := s.confinedRead(task, f.worktree, 10)
		testutil.Error(t, err)
		testutil.Contains(t, err.Error(), "not a regular file")
	})
}

func TestArtifactPublish_NotConfigured(t *testing.T) {
	s, _ := testServerWithArtifacts(t)
	cr := callTool(t, s, "artifact_publish", map[string]any{"path": "/tmp/x.html", "account": "personal"})
	testutil.True(t, cr.IsError)
	testutil.Contains(t, cr.Content[0].Text, "not configured")
}

func TestArtifactTools_Listed(t *testing.T) {
	names := func(s *Server) map[string]bool {
		resp := doRequest(t, s, "tools/list", nil)
		b, _ := json.Marshal(resp.Result)
		var res ToolsListResult
		_ = json.Unmarshal(b, &res)
		out := map[string]bool{}
		for _, tool := range res.Tools {
			out[tool.Name] = true
		}
		return out
	}
	t.Run("publisher and store wired", func(t *testing.T) {
		s, _, _ := testPublishServer(t, true)
		got := names(s)
		testutil.True(t, got["artifact_publish"])
		testutil.True(t, got["artifact_open"])
		testutil.True(t, got["artifact_register"])
	})
	t.Run("publisher without task management", func(t *testing.T) {
		s, _, _ := testPublishServer(t, false)
		got := names(s)
		testutil.False(t, got["artifact_publish"])
		testutil.False(t, got["artifact_open"])
	})
	t.Run("store only", func(t *testing.T) {
		s, _ := testServerWithArtifacts(t)
		got := names(s)
		testutil.False(t, got["artifact_publish"])
		testutil.True(t, got["artifact_open"])
	})
}

// stubOpener replaces the server's opener and records each call.
type stubOpener struct {
	calls []openCall
	err   error
}

type openCall struct {
	path   string
	reveal bool
}

func (o *stubOpener) open(path string, reveal bool) error {
	o.calls = append(o.calls, openCall{path, reveal})
	return o.err
}

func registerForOpen(t *testing.T, s *Server, name string) {
	t.Helper()
	cr := callArtifactRegister(t, s, map[string]any{"path": writeTempFile(t, name, publishPage), "id": "abc123"})
	testutil.False(t, cr.IsError)
}

func TestArtifactOpen(t *testing.T) {
	prev := artifactOpenGOOS
	artifactOpenGOOS = "darwin"
	t.Cleanup(func() { artifactOpenGOOS = prev })

	for _, tc := range []struct {
		name   string
		reveal bool
	}{{"open in default app", false}, {"reveal in finder", true}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testServerWithArtifacts(t)
			op := &stubOpener{}
			s.openArtifact = op.open
			registerForOpen(t, s, "report.html")
			cr := callTool(t, s, "artifact_open", map[string]any{"filename": "report.html", "reveal": tc.reveal, "cwd": "/tmp/worktrees/myapp/fix-login"})
			testutil.False(t, cr.IsError)
			testutil.Equal(t, len(op.calls), 1)
			testutil.Equal(t, op.calls[0].reveal, tc.reveal)
			testutil.Equal(t, filepath.Base(op.calls[0].path), "report.html")
			real, err := filepath.EvalSymlinks(agent.ArtifactsDir("abc123"))
			testutil.NoError(t, err)
			testutil.True(t, strings.HasPrefix(op.calls[0].path, real+string(filepath.Separator)))
		})
	}

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"unregistered name", map[string]any{"filename": "other.html", "id": "abc123"}, "not a registered artifact"},
		{"traversal", map[string]any{"filename": "../../etc/passwd", "id": "abc123"}, "not a registered artifact"},
		{"missing filename", map[string]any{"id": "abc123"}, "filename is required"},
		{"no task", map[string]any{"filename": "report.html"}, "provide id or cwd"},
		{"other task's artifact", map[string]any{"filename": "report.html", "id": "def456"}, "not a registered artifact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testServerWithArtifacts(t)
			op := &stubOpener{}
			s.openArtifact = op.open
			registerForOpen(t, s, "report.html")
			// An unregistered file sitting in the artifact dir is still refused.
			testutil.NoError(t, os.WriteFile(filepath.Join(agent.ArtifactsDir("abc123"), "other.html"), []byte("x"), 0o600))
			cr := callTool(t, s, "artifact_open", tc.args)
			testutil.True(t, cr.IsError)
			testutil.Contains(t, cr.Content[0].Text, tc.want)
			testutil.Equal(t, len(op.calls), 0)
		})
	}

	t.Run("manifest row whose path escapes", func(t *testing.T) {
		s, store := testServerWithArtifacts(t)
		op := &stubOpener{}
		s.openArtifact = op.open
		store.saved = append(store.saved, &model.Artifact{TaskID: "abc123", Filename: "../escape.html", Type: model.ArtifactHTML})
		cr := callTool(t, s, "artifact_open", map[string]any{"filename": "../escape.html", "id": "abc123"})
		testutil.True(t, cr.IsError)
		testutil.Contains(t, cr.Content[0].Text, "invalid artifact path")
		testutil.Equal(t, len(op.calls), 0)
	})

	t.Run("opener failure surfaces", func(t *testing.T) {
		s, _ := testServerWithArtifacts(t)
		op := &stubOpener{err: io.ErrUnexpectedEOF}
		s.openArtifact = op.open
		registerForOpen(t, s, "report.html")
		cr := callTool(t, s, "artifact_open", map[string]any{"filename": "report.html", "id": "abc123"})
		testutil.True(t, cr.IsError)
		testutil.Contains(t, cr.Content[0].Text, "unexpected EOF")
	})
}

func TestArtifactOpen_NotSupportedOffMacOS(t *testing.T) {
	prev := artifactOpenGOOS
	artifactOpenGOOS = "linux"
	t.Cleanup(func() { artifactOpenGOOS = prev })
	s, _ := testServerWithArtifacts(t)
	op := &stubOpener{}
	s.openArtifact = op.open
	registerForOpen(t, s, "report.html")
	cr := callTool(t, s, "artifact_open", map[string]any{"filename": "report.html", "id": "abc123"})
	testutil.True(t, cr.IsError)
	testutil.Contains(t, cr.Content[0].Text, "not supported on linux")
	testutil.Equal(t, len(op.calls), 0)
}

func TestArtifactOpen_ExecutableAndTypeChecks(t *testing.T) {
	prev := artifactOpenGOOS
	artifactOpenGOOS = "darwin"
	t.Cleanup(func() { artifactOpenGOOS = prev })

	for _, tc := range []struct {
		name, file, typ  string
		openOK, revealOK bool
	}{
		{"html opens", "report.html", "", true, true},
		{"pdf opens", "doc.pdf", "", true, true},
		{"png opens", "shot.png", "", true, true},
		{"markdown opens", "notes.md", "", true, true},
		{"txt opens", "log.txt", "", true, true},
		{"mp3 opens", "clip.mp3", "", true, true},
		{"mp4 opens", "clip.mp4", "", true, true},
		{".command refused, reveal allowed", "run.command", "", false, true},
		{".sh refused, reveal allowed", "run.sh", "", false, true},
		{".scpt refused, reveal allowed", "x.scpt", "", false, true},
		{".webloc refused, reveal allowed", "x.webloc", "", false, true},
		{"declared text with exec extension refused", "run.sh", "text", false, true},
		{"type/extension mismatch refused", "report.html", "pdf", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reveal := range []bool{false, true} {
				s, _ := testServerWithArtifacts(t)
				op := &stubOpener{}
				s.openArtifact = op.open
				args := map[string]any{"path": writeTempFile(t, tc.file, publishPage), "id": "abc123"}
				if tc.typ != "" {
					args["type"] = tc.typ
				}
				testutil.False(t, callArtifactRegister(t, s, args).IsError)
				cr := callTool(t, s, "artifact_open", map[string]any{"filename": tc.file, "reveal": reveal, "id": "abc123"})
				want := tc.openOK
				if reveal {
					want = tc.revealOK
				}
				testutil.Equal(t, cr.IsError, !want)
				testutil.Equal(t, len(op.calls), map[bool]int{true: 1, false: 0}[want])
				if !want {
					testutil.Contains(t, cr.Content[0].Text, "reveal: true")
				}
			}
		})
	}
}

func TestArtifactOpen_DirectoriesAndBundlesRefused(t *testing.T) {
	prev := artifactOpenGOOS
	artifactOpenGOOS = "darwin"
	t.Cleanup(func() { artifactOpenGOOS = prev })
	for _, tc := range []struct {
		name, file string
		dir        bool
	}{
		{"app bundle directory", "Evil.app", true},
		{"plain directory", "folder.html", true},
		{"app-named file", "Evil.app", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reveal := range []bool{false, true} {
				s, store := testServerWithArtifacts(t)
				op := &stubOpener{}
				s.openArtifact = op.open
				full := filepath.Join(agent.ArtifactsDir("abc123"), tc.file)
				if tc.dir {
					testutil.NoError(t, os.MkdirAll(filepath.Join(full, "Contents"), 0o700))
				} else {
					writeAt(t, full, "x")
				}
				store.saved = append(store.saved, &model.Artifact{TaskID: "abc123", Filename: tc.file, Type: model.ArtifactHTML})
				cr := callTool(t, s, "artifact_open", map[string]any{"filename": tc.file, "reveal": reveal, "id": "abc123"})
				testutil.True(t, cr.IsError)
				testutil.Equal(t, len(op.calls), 0)
			}
		})
	}
}

func TestOpenArgs(t *testing.T) {
	testutil.DeepEqual(t, openArgs("/a/b.html", false), []string{"/a/b.html"})
	testutil.DeepEqual(t, openArgs("/a/b.html", true), []string{"-R", "/a/b.html"})
}

func TestArtifactPublish_UnavailableSaveFailureStillReports(t *testing.T) {
	s, store, _ := testPublishServer(t, true)
	store.err = io.ErrShortWrite
	cr := callTool(t, s, "artifact_publish", map[string]any{"path": writeTempFile(t, "report.html", publishPage), "account": "default", "id": "abc123"})
	testutil.False(t, cr.IsError)
	r := decodePublishResult(t, cr)
	testutil.Equal(t, r.Status, "unavailable")
	testutil.Equal(t, r.Artifact, "")
	testutil.Contains(t, r.Message, "failed")
	testutil.DeepEqual(t, r.AvailableAccounts, []string{"personal"})
}

func TestArtifactOpen_MissingBytes(t *testing.T) {
	prev := artifactOpenGOOS
	artifactOpenGOOS = "darwin"
	t.Cleanup(func() { artifactOpenGOOS = prev })
	s, store := testServerWithArtifacts(t)
	op := &stubOpener{}
	s.openArtifact = op.open
	store.saved = append(store.saved, &model.Artifact{TaskID: "abc123", Filename: "gone.html", Type: model.ArtifactHTML})
	cr := callTool(t, s, "artifact_open", map[string]any{"filename": "gone.html", "id": "abc123"})
	testutil.True(t, cr.IsError)
	testutil.Contains(t, cr.Content[0].Text, "no bytes on disk")
	testutil.Equal(t, len(op.calls), 0)
}

func TestSetPathPolicy(t *testing.T) {
	s := testServer()
	task := &model.Task{ID: "x", Worktree: "/wt"}
	w, d := s.taskPathPolicy(task)
	testutil.True(t, len(w) > 1) // worktree + temp roots by default
	testutil.True(t, len(d) > 0) // built-in deny-read paths
	s.SetPathPolicy(func(*model.Task) ([]string, []string) { return []string{"/only"}, nil })
	w, d = s.taskPathPolicy(task)
	testutil.DeepEqual(t, w, []string{"/only"})
	testutil.Equal(t, len(d), 0)
}

// zeros is an endless reader.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestCopyArtifact_StreamOverCap(t *testing.T) {
	s, _ := testServerWithArtifacts(t)
	_, err := s.copyArtifact("abc123", zeros{}, "big.txt", model.ArtifactText)
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "byte cap")
	_, statErr := os.Stat(filepath.Join(agent.ArtifactsDir("abc123"), "big.txt"))
	testutil.True(t, os.IsNotExist(statErr))
}
