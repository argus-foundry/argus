// Package browserproc cleans up Playwright Chrome processes that escape their
// agent's process group. It never treats a shared browser cache as ownership.
package browserproc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// Process is a captured browser identity. Root distinguishes browser leaders
// from helpers; only roots carry independent ownership evidence.
type Process struct {
	PID, PPID, PGID        int
	Command, CWD, Worktree string
	Root                   bool
}

var output = func(name string, args ...string) ([]byte, error) {
	// Lifecycle tests must never inspect or signal real user browsers.
	if strings.HasSuffix(os.Args[0], ".test") {
		return nil, errors.New("browser process inspection disabled in tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

var signal = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
var wait = func(d time.Duration, cancel <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-cancel:
		return false
	}
}

// Worktree returns the task-level path of cwd strictly inside root, or empty.
func Worktree(cwd, root string) string {
	if !filepath.IsAbs(cwd) || !filepath.IsAbs(root) {
		return ""
	}
	cwd, root = physical(cwd), physical(root)
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(cwd))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return ""
	}
	return filepath.Join(root, parts[0], parts[1])
}

func physical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func executable(cmd string) string {
	exe := strings.SplitN(cmd, " --", 2)[0]
	if !strings.ContainsAny(exe, " \t\n") {
		return exe
	}
	// ps flattens argv; accept spaces only inside a recognized app bundle
	// path, never a command that merely mentions a Chrome executable.
	for _, app := range []string{"/Google Chrome.app/", "/Chromium.app/"} {
		if i := strings.Index(exe, app); i >= 0 && filepath.IsAbs(exe) && !strings.ContainsAny(exe[:i], " \t\n") {
			return exe
		}
	}
	return ""
}
func browserExe(cmd string) bool {
	switch filepath.Base(executable(cmd)) {
	case "Google Chrome", "Chromium", "chrome", "chromium", "chromium-browser", "chrome-headless-shell":
		return true
	}
	return false
}
func helperExe(cmd string) bool {
	return browserExe(cmd) || strings.HasPrefix(filepath.Base(executable(cmd)), "Google Chrome Helper") || strings.HasPrefix(filepath.Base(executable(cmd)), "Chromium Helper")
}

func identify(p Process, root string) Process {
	p.Worktree = Worktree(p.CWD, root)
	if p.Worktree == "" || !browserExe(p.Command) || strings.Contains(p.Command, " --type=") {
		return p
	}
	var profile string
	pipe := false
	for _, arg := range strings.Fields(p.Command) {
		if arg == "--remote-debugging-pipe" {
			pipe = true
		}
		if strings.HasPrefix(arg, "--user-data-dir=") {
			profile = strings.TrimPrefix(arg, "--user-data-dir=")
		}
	}
	ownedProfile := strings.HasPrefix(filepath.Base(profile), "playwright_chromiumdev_profile-") || Worktree(profile, root) == p.Worktree
	p.Root = pipe && ownedProfile
	return p
}

func parse(line string) (Process, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return Process{}, false
	}
	pid, e1 := strconv.Atoi(fields[0])
	ppid, e2 := strconv.Atoi(fields[1])
	pgid, e3 := strconv.Atoi(fields[2])
	if e1 != nil || e2 != nil || e3 != nil || pid <= 1 {
		return Process{}, false
	}
	// Keep executable paths containing spaces intact (Google Chrome.app).
	cmd := strings.TrimSpace(line)
	for range 3 {
		i := strings.IndexAny(cmd, " \t")
		if i < 0 {
			return Process{}, false
		}
		cmd = strings.TrimSpace(cmd[i:])
	}
	return Process{PID: pid, PPID: ppid, PGID: pgid, Command: cmd}, true
}

func cwd(pid int) (string, error) {
	if runtime.GOOS == "linux" {
		b, err := output("readlink", fmt.Sprintf("/proc/%d/cwd", pid))
		path := strings.TrimSuffix(strings.TrimSpace(string(b)), " (deleted)")
		if err == nil && !filepath.IsAbs(path) {
			err = errors.New("cwd unavailable")
		}
		return path, err
	}
	b, err := output("lsof", "-n", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "n/") {
			return strings.TrimPrefix(strings.TrimSuffix(line, " (deleted)"), "n"), nil
		}
	}
	return "", errors.New("cwd unavailable")
}

// Scan returns positively attributed browser roots and helpers captured in
// their detached group. Its output is still reverified before every signal.
func Scan(root string) ([]Process, error) {
	b, err := output("ps", "-axww", "-o", "pid=,ppid=,pgid=,command=")
	if err != nil {
		return nil, err
	}
	var all []Process
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 64*1024), 16<<20)
	for s.Scan() {
		if p, ok := parse(s.Text()); ok && helperExe(p.Command) {
			all = append(all, p)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	groups := map[int]Process{}
	var found []Process
	for _, p := range all {
		if !browserExe(p.Command) || strings.Contains(p.Command, " --type=") || !strings.Contains(p.Command, " --remote-debugging-pipe") {
			continue
		}
		p.CWD, err = cwd(p.PID)
		if err != nil {
			log("cwd unavailable", p, err)
			return nil, fmt.Errorf("browser pid %d cwd: %w", p.PID, err)
		}
		p = identify(p, root)
		if p.Root {
			found = append(found, p)
			if p.PGID == p.PID {
				groups[p.PGID] = p
			}
		}
	}
	for _, p := range all {
		leader, ok := groups[p.PGID]
		if !ok || p.PID == leader.PID || !strings.Contains(p.Command, " --type=") {
			continue
		}
		p.CWD, err = cwd(p.PID)
		if err != nil || p.CWD != leader.CWD {
			continue
		}
		p.Worktree = leader.Worktree
		found = append(found, p)
	}
	return found, nil
}

// Capture snapshots a single task's browsers before stopping its agent.
func Capture(worktree, root string) []Process {
	procs, err := Scan(root)
	if err != nil {
		log("scan unavailable", Process{Worktree: worktree}, err)
		return nil
	}
	var found []Process
	for _, p := range procs {
		if p.Worktree == physical(worktree) {
			found = append(found, p)
		}
	}
	return found
}

// Options gates escalation when the daemon shuts down or a task resumes.
type Options struct {
	Cancel  <-chan struct{}
	Proceed func() bool
}

// Stop signals only captured identities, independently of the agent's group.
// New launches during the grace period are deliberately outside the snapshot.
func Stop(procs []Process, o Options) {
	if len(procs) == 0 {
		return
	}
	proceed := func() bool {
		select {
		case <-o.Cancel:
			return false
		default:
		}
		return o.Proceed == nil || o.Proceed()
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		if !proceed() {
			log("cleanup no longer eligible", Process{}, nil)
			return
		}
		for _, p := range procs {
			if !proceed() {
				return
			}
			b, err := output("ps", "-ww", "-p", strconv.Itoa(p.PID), "-o", "pid=,ppid=,pgid=,command=")
			live, ok := parse(strings.TrimSpace(string(b)))
			if err != nil || !ok || live.PID != p.PID || live.PGID != p.PGID || live.Command != p.Command {
				log("skip changed or unavailable argv", p, err)
				continue
			}
			live.CWD, err = cwd(p.PID)
			if err != nil || live.CWD != p.CWD {
				log("skip changed or unavailable cwd", p, err)
				continue
			}
			err = signal(p.PID, sig)
			log(fmt.Sprintf("signal %s", sig), p, err)
		}
		if sig == syscall.SIGTERM && !wait(5*time.Second, o.Cancel) {
			return
		}
	}
}

func log(action string, p Process, err error) {
	slog.Info("browser cleanup: "+action, "pid", p.PID, "worktree", p.Worktree, "err", err)
	uxlog.Log("[browsercleanup] %s pid=%d worktree=%s err=%v", action, p.PID, p.Worktree, err)
}
