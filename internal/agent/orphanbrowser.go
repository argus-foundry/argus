package agent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"syscall"
)

// Orphaned automation browsers (reap-orphaned-playwright-browsers).
//
// Playwright launches Chrome with `detached: true`, so the browser leads its
// own process group and an argus StopTree (which signals the AGENT's group)
// never reaches it. Playwright closes it only from its own exit handlers, which
// never run when the Playwright server is SIGKILLed — after which the browser
// is reparented to launchd, its control pipe is closed, and nothing can ever
// drive or close it again. The daemon's browser sweeper uses this scanner to
// find such browsers and close them.

// BrowserProc is one row of a process-table snapshot, reduced to what the
// orphan-browser scan needs.
type BrowserProc struct {
	PID, PPID, PGID int32
	Comm            string // kinfo p_comm (truncated to 16 bytes by the kernel)
	Start           int64  // process start time, microseconds since the epoch
}

// OrphanBrowser is an argus-owned automation browser whose launcher has died.
// PID+Start is its identity: every signal re-checks it.
type OrphanBrowser struct {
	PID, PGID int32
	Start     int64
	TaskID    string // ARGUS_TASK_ID from its environment, when present
}

// remoteDebuggingPipeFlag is how Playwright (and Puppeteer's pipe mode) drives
// Chrome. A Chrome the user launched never carries it.
const remoteDebuggingPipeFlag = "--remote-debugging-pipe"

// argusProcessNames are the p_comm values of the processes that launch agent
// sessions: the session-supervisor and daemon (~/.argus/argusd reports
// "argus"), or the TUI's in-process runner fallback.
var argusProcessNames = map[string]bool{"argus": true, "argusd": true}

// isChromeMainComm is the cheap pre-filter: Chrome-family MAIN process names.
// Helpers ("Google Chrome He…" after the 16-byte truncation) never match.
// A missed name fails safe — the browser is simply not considered.
func isChromeMainComm(comm string) bool {
	switch comm {
	case "Google Chrome", "Google Chrome fo", "Chromium", "Google Chrome Ca", "Google Chrome Be", "Google Chrome De":
		return true
	}
	return strings.HasPrefix(comm, "chrome-headless-") || comm == "chrome" || comm == "chromium"
}

// parseProcArgs2 splits a darwin kern.procargs2 buffer into argv and env:
// a 4-byte argc, the exec path, NUL padding, argc argv strings, then env
// strings, all NUL-terminated.
func parseProcArgs2(buf []byte) (argv, env []string, err error) {
	if len(buf) < 4 {
		return nil, nil, errors.New("procargs2: short buffer")
	}
	argc := int(binary.LittleEndian.Uint32(buf[:4]))
	rest := buf[4:]
	// Skip the exec path and the NUL padding that follows it.
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil, nil, errors.New("procargs2: no exec path terminator")
	}
	rest = rest[i:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	for len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			j = len(rest)
		}
		s := string(rest[:j])
		if j < len(rest) {
			rest = rest[j+1:]
		} else {
			rest = nil
		}
		if len(argv) < argc {
			argv = append(argv, s)
			continue
		}
		if s == "" {
			break // end of the environment block
		}
		env = append(env, s)
	}
	if len(argv) < argc {
		return nil, nil, errors.New("procargs2: truncated argv")
	}
	return argv, env, nil
}

// browserClass is the cached classification of one process instance.
type browserClass struct {
	automation bool   // launched with --remote-debugging-pipe
	owned      bool   // observed under an argus ancestor, or env carries ARGUS_TASK_ID
	taskID     string // ARGUS_TASK_ID, when present
}

func classifyBrowser(argv, env []string) browserClass {
	var c browserClass
	for _, a := range argv[min(1, len(argv)):] {
		if a == remoteDebuggingPipeFlag {
			c.automation = true
			break
		}
	}
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "ARGUS_TASK_ID="); ok && v != "" {
			c.owned, c.taskID = true, v
			break
		}
	}
	return c
}

type procKey struct {
	pid   int32
	start int64
}

// hasArgusAncestor walks p's parent chain through the snapshot. Bounded by the
// snapshot size so a corrupt (cyclic) chain cannot loop forever.
func hasArgusAncestor(p BrowserProc, byPID map[int32]BrowserProc) bool {
	cur := p.PPID
	for range len(byPID) {
		if cur <= 1 {
			return false
		}
		anc, ok := byPID[cur]
		if !ok {
			return false
		}
		if argusProcessNames[anc.Comm] {
			return true
		}
		cur = anc.PPID
	}
	return false
}

// BrowserScanner finds orphaned argus-owned automation browsers. It caches
// each Chrome main process's classification by (pid, start time), so argv is
// read once per process instance; a steady-state Scan is one process-table
// read. Not goroutine-safe: driven by one loop.
type BrowserScanner struct {
	snapshot func() ([]BrowserProc, error)
	procArgs func(pid int32) (argv, env []string, err error)
	cache    map[procKey]browserClass

	// ArgvReads counts procArgs calls (for the cost-bound test and logging).
	ArgvReads int
}

// NewBrowserScanner returns a scanner over the real process table (a no-op
// on platforms without one, and always under `go test`).
func NewBrowserScanner() *BrowserScanner {
	return newBrowserScanner(browserSnapshot, browserProcArgs)
}

func newBrowserScanner(snapshot func() ([]BrowserProc, error), procArgs func(int32) ([]string, []string, error)) *BrowserScanner {
	return &BrowserScanner{snapshot: snapshot, procArgs: procArgs, cache: map[procKey]browserClass{}}
}

// Scan returns the orphaned owned browsers in the current process table, and
// records ownership of still-parented browsers observed under an argus process
// (the only ownership signal for Codex agents, whose MCP servers do not inherit
// ARGUS_TASK_ID). Ownership is recorded on every scan regardless of whether
// the caller then closes anything.
func (s *BrowserScanner) Scan() ([]OrphanBrowser, error) {
	procs, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	byPID := make(map[int32]BrowserProc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	live := make(map[procKey]bool)
	var orphans []OrphanBrowser
	for _, p := range procs {
		if !isChromeMainComm(p.Comm) {
			continue
		}
		key := procKey{p.PID, p.Start}
		live[key] = true
		c, ok := s.cache[key]
		if !ok {
			s.ArgvReads++
			argv, env, err := s.procArgs(p.PID)
			if err != nil {
				continue // exited, or unreadable: unknown ownership, retry next scan
			}
			c = classifyBrowser(argv, env)
			s.cache[key] = c
		}
		if !c.automation {
			continue
		}
		if !c.owned && p.PPID > 1 && hasArgusAncestor(p, byPID) {
			c.owned = true
			s.cache[key] = c
		}
		if c.owned && p.PPID == 1 {
			orphans = append(orphans, OrphanBrowser{PID: p.PID, PGID: p.PGID, Start: p.Start, TaskID: c.taskID})
		}
	}
	for k := range s.cache {
		if !live[k] {
			delete(s.cache, k)
		}
	}
	return orphans, nil
}

// browserKill is the signal seam (syscall.Kill by default, disabled under go
// test so a test can never signal a real PID).
var browserKill = func(pid int, sig syscall.Signal) error {
	if isTestBinary() {
		return errors.New("kill disabled in test binary")
	}
	return syscall.Kill(pid, sig)
}

// browserLookup re-reads one PID's process-table row (the identity seam).
var browserLookup = browserProcByPID

// SignalOrphanBrowser sends sig to b only if the same process instance is
// still alive and still orphaned (start time matches, parent is launchd).
// It signals the whole process group when b leads it — Playwright's detached
// spawn guarantees that — so the renderer/GPU/crashpad helpers go too.
// Returns whether a signal was sent; a vanished or reused PID is (false, nil).
func SignalOrphanBrowser(b OrphanBrowser, sig syscall.Signal) (bool, error) {
	cur, ok, err := browserLookup(b.PID)
	if err != nil {
		return false, err
	}
	if !ok || cur.Start != b.Start || cur.PPID != 1 {
		return false, nil
	}
	target := int(b.PID)
	if cur.PGID == b.PID {
		target = -target
	}
	if err := browserKill(target, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
