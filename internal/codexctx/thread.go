package codexctx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

// Thread identifies one Codex conversation in the thread index.
type Thread struct {
	ID          string
	RolloutPath string
}

// FindThread resolves the conversation to measure for a worktree from the
// thread index at stateDB. With a recorded sessionID only that thread, and
// only if it ran in cwd, matches. A replaced or foreign session yields
// ok=false. Without one the newest main CLI thread in cwd is used.
// A missing index is ok=false, not an error.
func FindThread(stateDB, cwd, sessionID string) (Thread, bool, error) {
	if cwd == "" {
		return Thread{}, false, errors.New("codexctx: empty worktree")
	}
	conn, err := OpenThreadIndex(stateDB)
	if err != nil {
		if os.IsNotExist(err) {
			return Thread{}, false, nil
		}
		return Thread{}, false, err
	}
	defer conn.Close() //nolint:errcheck // read-only handle
	var t Thread
	if sessionID != "" {
		err = conn.QueryRowContext(context.Background(), `SELECT id, rollout_path FROM threads WHERE id = ? AND cwd = ? AND rollout_path != '' AND source = ?`,
			sessionID, cwd, MainThreadSource).Scan(&t.ID, &t.RolloutPath)
	} else {
		err = conn.QueryRowContext(context.Background(), `SELECT id, rollout_path FROM threads WHERE cwd = ? AND rollout_path != '' AND source = ? ORDER BY updated_at DESC, id LIMIT 1`,
			cwd, MainThreadSource).Scan(&t.ID, &t.RolloutPath)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Thread{}, false, nil
	}
	if err != nil {
		return Thread{}, false, fmt.Errorf("query codex thread index: %w", err)
	}
	return t, true, nil
}
