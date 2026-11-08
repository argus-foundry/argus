package daemon

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/claudesession"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/lastexchange"
	"github.com/drn/argus/internal/model"
)

// recycleExchangeSection returns the "Last exchange with the user" seed-prompt
// section extracted from task's PRIOR Claude transcript, or "" when there is
// nothing to relay. It MUST be called before task.SessionID is cleared and
// never fails or delays the recycle: every problem is logged and omits the
// section (Claude-family backends only; others fail open). The extracted text
// stays local — it is written only into the session's own prompt.
func recycleExchangeSection(task *model.Task, cfg config.Config) string {
	if task == nil || task.SessionID == "" || task.Worktree == "" ||
		filepath.Base(task.SessionID) != task.SessionID {
		return ""
	}
	backend, err := agent.ResolveBackend(task, cfg)
	if err != nil || !agent.FamilyOf(backend.Command).SessionSwitcher {
		return "" // non-Claude backend: no transcript reader, omit
	}
	dir, err := agent.ClaudeConfigDirForTask(task, cfg)
	if err != nil {
		slog.Warn("[recycle] last exchange: config dir", "task", task.ID, "err", err)
		return ""
	}
	paths := []string{task.Worktree}
	if phys, perr := filepath.EvalSymlinks(task.Worktree); perr == nil && phys != task.Worktree {
		paths = append(paths, phys)
	}
	for _, p := range paths {
		proj, perr := claudesession.ProjectDirIn(dir, p)
		if perr != nil {
			continue
		}
		file := filepath.Join(proj, task.SessionID+".jsonl")
		if _, serr := os.Stat(file); serr != nil {
			continue
		}
		ex, rerr := lastexchange.FromFile(file)
		if rerr != nil {
			slog.Warn("[recycle] last exchange: read transcript", "task", task.ID, "err", rerr)
		}
		sec := ex.Section()
		slog.Info("[recycle] last exchange extracted", "task", task.ID, "human_bytes", len(ex.Human), "reply_bytes", len(ex.Reply))
		return sec
	}
	slog.Info("[recycle] last exchange: transcript not found", "task", task.ID, "session", task.SessionID)
	return ""
}
