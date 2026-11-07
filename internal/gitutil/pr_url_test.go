package gitutil

import (
	"context"
	"errors"
	"testing"

	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func TestParsePRURL(t *testing.T) {
	good := map[string]string{
		"https://github.com/org/repo/pull/7":             "https://github.com/org/repo/pull/7",
		" https://GitHub.com/org/re.po-x/pull/7/ ":       "https://github.com/org/re.po-x/pull/7",
		"https://github.com/org/repo/pull/7?x=1#issue-9": "https://github.com/org/repo/pull/7",
	}
	for in, want := range good {
		got, err := ParsePRURL(in)
		testutil.NoError(t, err)
		testutil.Equal(t, got, want)
	}
	bad := []string{
		"", "not a url", "http://github.com/o/r/pull/7", "https://evil.com/o/r/pull/7",
		"https://github.com.evil.com/o/r/pull/7", "https://u:p@github.com/o/r/pull/7",
		"https://github.com:8443/o/r/pull/7", "https://github.com/o/r/issues/7",
		"https://github.com/o/r/pull/0", "https://github.com/o/r/pull/-1",
		"https://github.com/o/r/pull/x", "https://github.com/o/r/pull/7/files",
		"https://github.com/o/pull/7", "https://github.com/o/r%2Fx/pull/7",
		"file:///etc/passwd",
	}
	for _, in := range bad {
		_, err := ParsePRURL(in)
		testutil.ErrorIs(t, err, ErrInvalidPRURL)
	}
}

func TestFetchPRByURL(t *testing.T) {
	orig := prRunner
	t.Cleanup(func() { prRunner = orig })
	run := func(out string, code int, err error) {
		prRunner = func(_ context.Context, _ string, args ...string) (string, int, error) {
			testutil.Equal(t, args[2], "https://github.com/o/r/pull/7")
			return out, code, err
		}
	}
	run(`{"state":"MERGED","url":"https://github.com/o/r/pull/7"}`, 0, nil)
	res, err := FetchPRByURL(context.Background(), "https://github.com/o/r/pull/7")
	testutil.NoError(t, err)
	testutil.Equal(t, res.Merged, true)
	testutil.Equal(t, res.Lifecycle(), model.PRLifecycleMerged)

	run(`{"state":"CLOSED","url":"https://github.com/o/r/pull/7"}`, 0, nil)
	res, err = FetchPRByURL(context.Background(), "https://github.com/o/r/pull/7")
	testutil.NoError(t, err)
	testutil.Equal(t, res.Lifecycle(), model.PRLifecycleClosed)

	run("no pull requests found", 1, nil)
	_, err = FetchPRByURL(context.Background(), "https://github.com/o/r/pull/7")
	testutil.Equal(t, err != nil, true)

	run("", 0, errors.New("boom"))
	_, err = FetchPRByURL(context.Background(), "https://github.com/o/r/pull/7")
	testutil.Equal(t, err != nil, true)

	run(`not json`, 0, nil)
	_, err = FetchPRByURL(context.Background(), "https://github.com/o/r/pull/7")
	testutil.Equal(t, err != nil, true)
}
