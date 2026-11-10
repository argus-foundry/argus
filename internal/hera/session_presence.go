package hera

import (
	"context"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
)

// sessionProbeTimeout bounds one presence probe; the supervisor client's own
// RPC timeout is 2s, so this only matters for a hung transport.
const sessionProbeTimeout = 3 * time.Second

// SessionPresent asks r whether taskID's session exists. The daemon's
// supervisor client answers through agent.SessionPresenceChecker, which
// surfaces an unreachable supervisor as an error (its HasSession folds every
// RPC failure, including a 2s timeout under load, into "no session", and
// counts a pending restart as present). The in-process agent.Runner has no
// such method; its HasSession is an authoritative map lookup, so the fallback
// never errors. A caller that destroys work on "absent" must treat a non-nil
// error as present.
func SessionPresent(r SessionChecker, taskID string) (bool, error) {
	if p, ok := r.(agent.SessionPresenceChecker); ok {
		ctx, cancel := context.WithTimeout(context.Background(), sessionProbeTimeout)
		defer cancel()
		return p.SessionPresent(ctx, taskID)
	}
	return r.HasSession(taskID), nil
}
