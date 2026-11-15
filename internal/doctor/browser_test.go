package doctor

import (
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/browserproc"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestRenderBrowserOrphans(t *testing.T) {
	for _, tc := range []struct {
		name  string
		procs []browserproc.Process
		err   error
		want  string
	}{
		{"none", nil, nil, "NONE FOUND"},
		{"unknown", nil, errors.New("denied"), "UNKNOWN"},
		{"orphan", []browserproc.Process{{PID: 20, PPID: 1, Root: true, Worktree: "/test/task"}}, nil, "pid 20  /test/task"},
		{"live parent", []browserproc.Process{{PID: 20, PPID: 9, Root: true}}, nil, "NONE FOUND"},
		{"helper", []browserproc.Process{{PID: 20, PPID: 1}}, nil, "NONE FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.Contains(t, RenderBrowserOrphans(tc.procs, tc.err), tc.want) })
	}
}
