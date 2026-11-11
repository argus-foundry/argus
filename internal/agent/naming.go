package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/llm"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

var namingGenerateFn = llm.GenerateNameWithBackend

// namingBackend snapshots routing at creation; it must never route a second time.
func namingBackend(task model.Task, cfg config.Config) (llm.NamingBackend, error) {
	backend, ok := cfg.Backends[task.Backend]
	if !ok {
		return llm.NamingBackend{}, fmt.Errorf("unknown backend %q", task.Backend)
	}
	family := FamilyOf(backend.Command)
	if family.Naming == nil {
		return llm.NamingBackend{}, llm.ErrUnavailable
	}
	utility := *family.Naming
	accountEnv, authStrip, err := family.NamingAccount(&task, cfg)
	if err != nil {
		return utility, err
	}
	strip := append(append([]string(nil), authStrip...), "CODEX_HOME", "CODEX_SQLITE_HOME", "CODEX_THREAD_ID", "ARGUS_TASK_ID", "ARGUS_MODEL", "ARGUS_PROFILE", "ARGUS_ARCHETYPE")
	utility.Env = spawnBaseEnv(os.Environ(), strip, task.ID)
	if accountEnv != "" {
		utility.Env = append(utility.Env, accountEnv)
	}
	for target, source := range backend.EnvVars {
		if target == "" || source == "" {
			continue
		}
		filtered := utility.Env[:0]
		for _, entry := range utility.Env {
			key, _, _ := strings.Cut(entry, "=")
			if key != target {
				filtered = append(filtered, entry)
			}
		}
		utility.Env = filtered
		value, ok := resolveBackendEnvSource(cfg, source)
		if !ok {
			return utility, fmt.Errorf("naming credential %q unavailable", target)
		}
		utility.Env = append(utility.Env, target+"="+value)
	}
	return utility, nil
}

func runAutoRenameForTask(database *db.DB, task model.Task, prompt string, cfg config.Config) {
	utility, err := namingBackend(task, cfg)
	if err != nil {
		uxlog.Log("[autoname] skipped task=%s backend=%q account=%q err=%v", task.ID, task.Backend, task.Account, err)
		return
	}
	uxlog.Log("[autoname] request task=%s backend=%q account=%q", task.ID, task.Backend, task.Account)
	runAutoRenameWith(database, task.ID, task.Name, prompt, func(ctx context.Context, prompt string) (string, error) {
		name, err := namingGenerateFn(ctx, prompt, utility)
		uxlog.Log("[autoname] result task=%s backend=%q success=%t err=%v", task.ID, task.Backend, err == nil, err)
		return name, err
	})
}
