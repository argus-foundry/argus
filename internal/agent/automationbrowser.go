package agent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
)

// Automation-browser scan (the daemon's browser census).
//
// Playwright MCP launches Chrome with --remote-debugging-pipe in its own
// process group (`detached: true`), so it is invisible to argus's per-session
// process-group accounting. The census uses this scanner to log each such
// browser's lifecycle — which task opened it, whether its launcher outlived it
// or vice versa — so that "Chrome windows left behind" reports can be
// diagnosed from the daemon log instead of guessed at. Log-only: nothing here
// ever signals a process.

// BrowserProc is one row of a process-table snapshot, reduced to what the
// browser scan needs.
type BrowserProc struct {
	PID, PPID, PGID int32
	Comm            string // kinfo p_comm (truncated to 16 bytes by the kernel)
	Start           int64  // process start time, microseconds since the epoch
}

// AutomationBrowser is one live automation-driven Chrome main process.
// PID+Start is its identity.
type AutomationBrowser struct {
	PID, PPID int32
	Start     int64
	EnvTaskID string // ARGUS_TASK_ID from its environment, when present
	// Ancestors is the parent chain (nearest first) captured the first time the
	// browser was seen with a live parent; nil if it was already orphaned then.
	Ancestors []BrowserProc
}

// remoteDebuggingPipeFlag is how Playwright (and Puppeteer's pipe mode) drives
// Chrome. A Chrome the user launched never carries it.
const remoteDebuggingPipeFlag = "--remote-debugging-pipe"

// maxBrowserAncestry bounds the captured parent chain.
const maxBrowserAncestry = 16

// isChromeMainComm is the cheap pre-filter: Chrome-family MAIN process names.
// Helpers ("Google Chrome He…" after the 16-byte truncation) never match.
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
	automation bool          // launched with --remote-debugging-pipe
	envTaskID  string        // ARGUS_TASK_ID, when present
	ancestors  []BrowserProc // captured once, while the parent was alive
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
			c.envTaskID = v
			break
		}
	}
	return c
}

type procKey struct {
	pid   int32
	start int64
}

// ancestry walks p's parent chain through the snapshot, nearest first, up to
// (not including) launchd. Bounded so a corrupt (cyclic) chain cannot loop.
func ancestry(p BrowserProc, byPID map[int32]BrowserProc) []BrowserProc {
	var out []BrowserProc
	cur := p.PPID
	for len(out) < maxBrowserAncestry && cur > 1 {
		anc, ok := byPID[cur]
		if !ok {
			break
		}
		out = append(out, anc)
		cur = anc.PPID
	}
	return out
}

// BrowserScanner lists live automation browsers. It caches each Chrome main
// process's classification by (pid, start time), so argv is read once per
// process instance; a steady-state Scan is one process-table read. Not
// goroutine-safe: driven by one loop.
type BrowserScanner struct {
	snapshot func() ([]BrowserProc, error)
	procArgs func(pid int32) (argv, env []string, err error)
	cache    map[procKey]browserClass

	// ArgvReads counts procArgs calls (for the cost-bound test).
	ArgvReads int
}

// NewBrowserScanner returns a scanner over the real process table (empty on
// platforms without one, and always under `go test`).
func NewBrowserScanner() *BrowserScanner {
	return newBrowserScanner(browserSnapshot, browserProcArgs)
}

func newBrowserScanner(snapshot func() ([]BrowserProc, error), procArgs func(int32) ([]string, []string, error)) *BrowserScanner {
	return &BrowserScanner{snapshot: snapshot, procArgs: procArgs, cache: map[procKey]browserClass{}}
}

// Scan returns every live automation browser.
func (s *BrowserScanner) Scan() ([]AutomationBrowser, error) {
	procs, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	byPID := make(map[int32]BrowserProc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	live := make(map[procKey]bool)
	var out []AutomationBrowser
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
				continue // exited, or unreadable: retry next scan
			}
			c = classifyBrowser(argv, env)
			if c.automation && p.PPID > 1 {
				c.ancestors = ancestry(p, byPID)
			}
			s.cache[key] = c
		}
		if !c.automation {
			continue
		}
		out = append(out, AutomationBrowser{PID: p.PID, PPID: p.PPID, Start: p.Start, EnvTaskID: c.envTaskID, Ancestors: c.ancestors})
	}
	for k := range s.cache {
		if !live[k] {
			delete(s.cache, k)
		}
	}
	return out, nil
}
