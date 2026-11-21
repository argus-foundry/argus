package doctor

import (
	"errors"
	"fmt"
	"strings"
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

func TestRenderBrowserOrphansPartial(t *testing.T) {
	t.Parallel()
	err := &browserproc.PartialInspectionError{PIDs: []int{21}}
	for _, found := range []bool{false, true} {
		t.Run(fmt.Sprint(found), func(t *testing.T) {
			var procs []browserproc.Process
			if found {
				procs = []browserproc.Process{{PID: 20, PPID: 1, Root: true, Worktree: "/test/task"}}
			}
			text := RenderBrowserOrphans(procs, err)
			testutil.Contains(t, text, "PARTIAL")
			testutil.Equal(t, strings.Contains(text, "NONE FOUND"), false)
			if found {
				testutil.Contains(t, text, "pid 20")
			}
		})
	}
}
