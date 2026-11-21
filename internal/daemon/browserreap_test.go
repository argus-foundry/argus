package daemon

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/browserproc"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBrowserReaperEligibility(t *testing.T) {
	wt := dsRoot + "/Sketch/task"
	for _, tc := range []struct {
		name    string
		rows    []*model.Task
		running []string
		want    int
	}{
		{"finished", []*model.Task{dsTask("a", wt, model.StatusComplete)}, nil, 1},
		{"missing", nil, nil, 1},
		{"live orphan", []*model.Task{dsTask("a", wt, model.StatusInProgress)}, []string{"a"}, 0},
		{"resumed complete", []*model.Task{dsTask("a", wt, model.StatusComplete)}, []string{"a"}, 0},
		{"unfinished", []*model.Task{dsTask("a", wt, model.StatusInReview)}, nil, 0},
		{"shared unfinished", []*model.Task{dsTask("a", wt, model.StatusComplete), dsTask("b", wt, model.StatusInReview)}, nil, 0},
		{"unknown live row", nil, []string{"unknown"}, 0},
		{"archived", []*model.Task{{ID: "a", Worktree: wt, Status: model.StatusInReview, Archived: true}}, nil, 1},
		{"resident", []*model.Task{{ID: "a", Worktree: wt, Status: model.StatusComplete, Kind: model.KindResident}}, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := &fakeDSTasks{rows: tc.rows}
			r := newBrowserReaper(func() ([]string, error) { return tc.running, nil }, tasks, dsRoot)
			r.scan = func(string) ([]browserproc.Process, error) {
				return []browserproc.Process{{PID: 20, PPID: 1, Worktree: wt, Root: true}}, nil
			}
			stops := 0
			r.stop = func(_ []browserproc.Process, o browserproc.Options) { testutil.Equal(t, o.Proceed(), true); stops++ }
			for range 5 {
				r.tick()
			}
			testutil.Equal(t, stops, 0)
			r.tick()
			testutil.Equal(t, stops, tc.want)
		})
	}
}

func TestBrowserReaperGraceResets(t *testing.T) {
	r := newBrowserReaper(func() ([]string, error) { return nil, nil }, &fakeDSTasks{}, dsRoot)
	wt := dsRoot + "/p/t"
	scanErr := false
	r.scan = func(string) ([]browserproc.Process, error) {
		if scanErr {
			return nil, errors.New("unknown")
		}
		return []browserproc.Process{{PID: 20, Root: true, Worktree: wt}}, nil
	}
	var stopped int
	r.stop = func([]browserproc.Process, browserproc.Options) { stopped++ }
	for range 5 {
		r.tick()
	}
	scanErr = true
	r.tick()
	scanErr = false
	for range 5 {
		r.tick()
	}
	testutil.Equal(t, stopped, 0)
	r.tick()
	testutil.Equal(t, stopped, 1)
}

func TestBrowserReaperPartialGrace(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			r := newBrowserReaper(func() ([]string, error) { return nil, nil }, &fakeDSTasks{}, dsRoot)
			wt := dsRoot + "/p/t"
			other := wt
			if !shared {
				other += "-other"
			}
			failed := false
			r.scan = func(string) ([]browserproc.Process, error) {
				p := []browserproc.Process{{PID: 20, Root: true, Worktree: wt}}
				if failed {
					return p, &browserproc.PartialInspectionError{PIDs: []int{30}}
				}
				return append(p, browserproc.Process{PID: 30, Root: true, Worktree: other}), nil
			}
			var stopped []int
			r.stop = func(ps []browserproc.Process, _ browserproc.Options) {
				for _, p := range ps {
					stopped = append(stopped, p.PID)
				}
			}
			for range 5 {
				r.tick()
			}
			failed = true
			r.tick()
			testutil.DeepEqual(t, stopped, []int{20})
			failed = false
			for range 5 {
				r.tick()
			}
			testutil.DeepEqual(t, stopped, []int{20})
			r.tick()
			sort.Ints(stopped)
			testutil.DeepEqual(t, stopped, []int{20, 20, 30})
		})
	}
}

func TestBrowserReaperChangedIdentityResetsGrace(t *testing.T) {
	wt := dsRoot + "/p/t"
	for _, tc := range []struct {
		name   string
		change func(*browserproc.Process)
	}{
		{"PID", func(p *browserproc.Process) { p.PID = 21 }},
		{"command", func(p *browserproc.Process) { p.Command += " --new-profile" }},
		{"cwd", func(p *browserproc.Process) { p.CWD += "/nested" }},
		{"PGID", func(p *browserproc.Process) { p.PGID = 21 }},
		{"worktree", func(p *browserproc.Process) { p.Worktree += "-other" }},
		{"root", func(p *browserproc.Process) { p.Root = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newBrowserReaper(func() ([]string, error) { return nil, nil }, &fakeDSTasks{}, dsRoot)
			candidate := browserproc.Process{PID: 20, PGID: 20, Root: true, Command: "/cache/chrome --remote-debugging-pipe", CWD: wt, Worktree: wt}
			healthy := candidate
			healthy.PID, healthy.PGID = 30, 30
			r.scan = func(string) ([]browserproc.Process, error) {
				return []browserproc.Process{candidate, healthy}, nil
			}
			var stopped []browserproc.Process
			r.stop = func(ps []browserproc.Process, _ browserproc.Options) {
				stopped = append(stopped, ps...)
			}
			for range 5 {
				r.tick()
			}
			testutil.Equal(t, len(stopped), 0)
			tc.change(&candidate)
			r.tick()
			// The unchanged browser reaches its sixth tick, even when the
			// changed candidate still claims the same worktree.
			testutil.DeepEqual(t, stopped, []browserproc.Process{healthy})
			for range 4 {
				r.tick()
			}
			testutil.DeepEqual(t, stopped, []browserproc.Process{healthy})
			r.tick()
			// The changed identity needs six observations of its own.
			testutil.DeepEqual(t, stopped, []browserproc.Process{healthy, candidate})
		})
	}
}

func TestBrowserReaperLoop(t *testing.T) {
	r := newBrowserReaper(func() ([]string, error) { return nil, nil }, &fakeDSTasks{}, dsRoot)
	done := make(chan struct{})
	r.cancel = done
	scanned := make(chan struct{}, 1)
	r.scan = func(string) ([]browserproc.Process, error) {
		select {
		case scanned <- struct{}{}:
		default:
		}
		return nil, nil
	}
	exited := make(chan struct{})
	go func() { r.run(time.Millisecond); close(exited) }()
	select {
	case <-scanned:
	case <-time.After(3 * time.Second):
		t.Fatal("sweep never ticked")
	}
	close(done)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("sweep did not stop")
	}
}

func TestBrowserReaperFailsClosed(t *testing.T) {
	wt := dsRoot + "/Sketch/task"
	for _, tc := range []string{"task error", "running error", "scan error", "outside root", "resume during stop", "cancelled"} {
		t.Run(tc, func(t *testing.T) {
			tasks := &fakeDSTasks{}
			var running []string
			var runErr error
			r := newBrowserReaper(func() ([]string, error) { return running, runErr }, tasks, dsRoot)
			path := wt
			if tc == "outside root" {
				path = "/personal/project/task"
			}
			r.scan = func(string) ([]browserproc.Process, error) {
				if tc == "scan error" {
					return nil, errors.New("denied")
				}
				return []browserproc.Process{{PID: 20, Worktree: path, Root: true}}, nil
			}
			if tc == "task error" {
				tasks.err = errors.New("denied")
			}
			if tc == "running error" {
				runErr = errors.New("denied")
			}
			if tc == "cancelled" {
				c := make(chan struct{})
				close(c)
				r.cancel = c
			}
			stops := 0
			r.stop = func(_ []browserproc.Process, o browserproc.Options) {
				stops++
				if tc == "resume during stop" {
					tasks.rows = []*model.Task{dsTask("a", wt, model.StatusComplete)}
					running = []string{"a"}
					testutil.Equal(t, o.Proceed(), false)
				}
			}
			for range 6 {
				r.tick()
			}
			want := 0
			if tc == "resume during stop" {
				want = 1
			}
			testutil.Equal(t, stops, want)
		})
	}
}
