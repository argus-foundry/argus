package gitutil

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

const tip = "0123456789abcdef0123456789abcdef01234567"

func handoffPRJSON(state, rollup string) string {
	return `{"state":"` + state + `","isDraft":false,"url":"https://x/pr/1","headRefOid":"` + tip + `","statusCheckRollup":` + rollup + `}`
}

func TestParsePRHandoff(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, raw, want string // want = reason substring; "" means ok
	}{
		{"open no checks", handoffPRJSON("OPEN", "[]"), ""},
		{"open null rollup", handoffPRJSON("OPEN", "null"), ""},
		{"open passing + pending + skipped", handoffPRJSON("OPEN", `[{"name":"a","conclusion":"SUCCESS","status":"COMPLETED"},{"name":"b","conclusion":"","status":"IN_PROGRESS"},{"name":"c","conclusion":"SKIPPED"},{"context":"ci/x","state":"PENDING"}]`), ""},
		{"check run failure", handoffPRJSON("OPEN", `[{"name":"unit","conclusion":"FAILURE"}]`), "unit"},
		{"status context error", handoffPRJSON("OPEN", `[{"context":"ci/x","state":"ERROR"}]`), "ci/x"},
		{"timed out", handoffPRJSON("OPEN", `[{"name":"t","conclusion":"TIMED_OUT"}]`), "t"},
		{"cancelled", handoffPRJSON("OPEN", `[{"name":"k","conclusion":"CANCELLED"}]`), "k"},
		{"merged", handoffPRJSON("MERGED", "[]"), "not open"},
		{"closed", handoffPRJSON("CLOSED", "[]"), "not open"},
		{"garbage", "not json", "unreadable"},
		{"empty", "", "unreadable"},
		{"no head", `{"state":"OPEN","url":"u"}`, "head commit unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, head, why := ParsePRHandoff(tc.raw)
			if tc.want == "" {
				testutil.Equal(t, why, "")
				testutil.Equal(t, url, "https://x/pr/1")
				testutil.Equal(t, head, tip)
				return
			}
			testutil.Contains(t, why, tc.want)
		})
	}
}

type fakeGit struct {
	out map[string]string
	err map[string]error
}

func (f fakeGit) run(_ context.Context, _ string, args ...string) (string, error) {
	k := args[0]
	if e := f.err[k]; e != nil {
		return "", e
	}
	return f.out[k], nil
}

func withFakes(t *testing.T, gh string, ghCode int, ghErr error, g fakeGit) {
	t.Helper()
	oldGh, oldGit := prRunner, handoffGit
	prRunner = func(context.Context, string, ...string) (string, int, error) { return gh, ghCode, ghErr }
	handoffGit = g.run
	t.Cleanup(func() { prRunner, handoffGit = oldGh, oldGit })
}

func goodGit() fakeGit {
	return fakeGit{out: map[string]string{"rev-parse": tip, "ls-remote": tip + "\trefs/heads/b", "status": ""}}
}

func TestCheckHandoff(t *testing.T) {
	ctx := context.Background()
	t.Run("all good", func(t *testing.T) {
		withFakes(t, handoffPRJSON("OPEN", "[]"), 0, nil, goodGit())
		st, ok, why := CheckHandoff(ctx, "/wt", "b")
		testutil.Equal(t, ok, true)
		testutil.Equal(t, why, "")
		testutil.Equal(t, st.TipSHA, tip)
		testutil.Equal(t, st.PRURL, "https://x/pr/1")
	})
	refusals := []struct {
		name  string
		gh    string
		code  int
		ghErr error
		mut   func(g *fakeGit)
		want  string
	}{
		{"no worktree handled elsewhere", handoffPRJSON("OPEN", "[]"), 0, nil, func(g *fakeGit) { g.out["ls-remote"] = "" }, "not pushed"},
		{"unpushed commits", handoffPRJSON("OPEN", "[]"), 0, nil, func(g *fakeGit) { g.out["rev-parse"] = strings.Repeat("a", 40) }, "differs from pushed"},
		{"pr head differs", `{"state":"OPEN","url":"u","headRefOid":"` + strings.Repeat("b", 40) + `"}`, 0, nil, func(g *fakeGit) {}, "PR head"},
		{"dirty", handoffPRJSON("OPEN", "[]"), 0, nil, func(g *fakeGit) { g.out["status"] = " M f.go" }, "uncommitted"},
		{"failing ci", handoffPRJSON("OPEN", `[{"name":"unit","conclusion":"FAILURE"}]`), 0, nil, func(g *fakeGit) {}, "unit"},
		{"no pr", "no pull requests found for branch", 1, errors.New("exit"), func(g *fakeGit) {}, "no PR"},
		{"gh error", "boom", 1, errors.New("exit"), func(g *fakeGit) {}, "PR lookup failed"},
		{"gh absent", "", 0, errGhAbsent, func(g *fakeGit) {}, "PR lookup failed"},
		{"rev-parse error", handoffPRJSON("OPEN", "[]"), 0, nil, func(g *fakeGit) { g.err = map[string]error{"rev-parse": errors.New("x")} }, "local tip unreadable"},
		{"ls-remote error", handoffPRJSON("OPEN", "[]"), 0, nil, func(g *fakeGit) { g.err = map[string]error{"ls-remote": errors.New("net")} }, "remote tip unreadable"},
		{"status error", handoffPRJSON("OPEN", "[]"), 0, nil, func(g *fakeGit) { g.err = map[string]error{"status": errors.New("x")} }, "status unreadable"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			g := goodGit()
			tc.mut(&g)
			withFakes(t, tc.gh, tc.code, tc.ghErr, g)
			_, ok, why := CheckHandoff(ctx, "/wt", "b")
			testutil.Equal(t, ok, false)
			testutil.Contains(t, why, tc.want)
		})
	}
	t.Run("missing worktree/branch refuses", func(t *testing.T) {
		_, ok, why := CheckHandoff(ctx, "", "b")
		testutil.Equal(t, ok, false)
		testutil.Contains(t, why, "no worktree")
		_, ok, _ = CheckHandoff(ctx, "/wt", "")
		testutil.Equal(t, ok, false)
	})
}

func TestShort(t *testing.T) {
	t.Parallel()
	testutil.Equal(t, short("abcdefghijkl"), "abcdefgh")
	testutil.Equal(t, short("abc"), "abc")
}
