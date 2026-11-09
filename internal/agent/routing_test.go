package agent

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

func TestCreateRoutesAccountWithExplicitBackendAndPins(t *testing.T) {
	for _, tc := range []struct {
		name, explicit, project, want string
		wantRouted                    bool
	}{
		{"route", "", "", "work", true},
		{"explicit default", "default", "", "default", false},
		{"explicit work", "work", "", "work", false},
		{"project pin", "", "default", "", false},
		{"project pin work", "", "work", "work", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initGitRepo(t)
			extra := "[backend_routing]\nstrategy=\"headroom\"\n[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"claude_usage\"\nthreshold_pct=90\n"
			if tc.project != "" {
				extra += "[project_accounts]\nproj=\"" + tc.project + "\"\n"
			}
			d := accountFileDB(t, repo, extra)
			restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
				return usagebudget.Reading{Percentage: 0}, account == "work"
			})
			defer restore()
			task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "routed", Project: "proj", Backend: "test", Account: tc.explicit})
			testutil.NoError(t, err)
			testutil.Equal(t, task.Account, tc.want)
			testutil.Equal(t, task.AccountRouted, tc.wantRouted)
			stored, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, stored.AccountRouted, tc.wantRouted)
		})
	}
}

func TestHeraBlankParentAllowsAccountRouting(t *testing.T) {
	repo := initGitRepo(t)
	d := accountFileDB(t, repo, "[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"none\"\n")
	parent := &model.Task{ID: "parent", Name: "parent", Project: "proj", Backend: "test"}
	testutil.NoError(t, d.Add(parent))
	testutil.Equal(t, parent.Account, "")
	orch, err := d.CreateHeraOrchestrator("route", "")
	testutil.NoError(t, err)
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "proj"}, parent.ID, repo)
	testutil.NoError(t, err)
	result, err := SpawnHeraWorker(d, &fakeRunner{sessionPID: 1}, HeraWorkerSpawnInput{OrchestratorID: orch.ID, BaseName: "worker", Project: "proj", Backend: "test", TaskPrompt: "p"})
	testutil.NoError(t, err)
	testutil.Equal(t, result.Task.Account, "work")
	// An explicit default is a nonblank stored identity and remains inherited.
	parent.Account = "default"
	testutil.NoError(t, d.Update(parent))
	account, err := resolveOrchestratorAccount(d, orch.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, account, "default")
}

func TestRoutedCoordinatorWorkerRoutes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		routed      bool
		wantAccount string
		wantRouted  bool
	}{
		{"routed coordinator", true, "", true},
		{"pinned coordinator", false, "personal", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initGitRepo(t)
			d := accountFileDB(t, repo, "[accounts.personal]\nclaude_config_dir=\""+filepath.Join(t.TempDir(), "personal")+"\"\n"+
				"[backend_routing]\nstrategy=\"headroom\"\n"+
				"[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"personal\"\nprobe=\"claude_usage\"\nthreshold_pct=90\n"+
				"[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"claude_usage\"\nthreshold_pct=90\n")
			// personal is over its threshold; work has headroom.
			restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
				switch account {
				case "personal":
					return usagebudget.Reading{Percentage: 95}, true
				case "work":
					return usagebudget.Reading{Percentage: 5}, true
				}
				return usagebudget.Reading{}, false
			})
			defer restore()
			parent := &model.Task{ID: "coord-" + tc.name, Name: "coord", Project: "proj", Backend: "test", Account: "personal", AccountRouted: tc.routed}
			testutil.NoError(t, d.Add(parent))
			orch, err := d.CreateHeraOrchestrator("routed", "")
			testutil.NoError(t, err)
			_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "proj"}, parent.ID, repo)
			testutil.NoError(t, err)

			account, err := resolveOrchestratorAccount(d, orch.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, account, tc.wantAccount)

			result, err := SpawnHeraWorker(d, &fakeRunner{sessionPID: 1}, HeraWorkerSpawnInput{OrchestratorID: orch.ID, BaseName: "worker", Project: "proj", Backend: "test", TaskPrompt: "p"})
			testutil.NoError(t, err)
			want := "personal"
			if tc.routed {
				want = "work"
			}
			testutil.Equal(t, result.Task.Account, want)
			testutil.Equal(t, result.Task.AccountRouted, tc.wantRouted)
		})
	}
}

func TestCoordinatorFiltersToClaudeAccountTiers(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Backends["other-claude"] = config.Backend{Command: "claude"}
	cfg.BackendRouting.Tiers = []config.BackendTier{{Backend: "codex", Probe: config.ProbeNone}, {Backend: "other-claude", Probe: config.ProbeNone}}
	testutil.Equal(t, resolveCoordinatorBackend("", "", cfg), "other-claude")
	cfg.Projects = map[string]config.Project{"proj": {Backend: "codex"}}
	testutil.Equal(t, resolveCoordinatorBackend("", "proj", cfg), "other-claude")
}

func TestCoordinatorSpawnRoutesAccount(t *testing.T) {
	for _, backend := range []string{"", "test"} {
		t.Run(backend, func(t *testing.T) {
			repo := initGitRepo(t)
			d := accountFileDB(t, repo, "[backend_routing]\nstrategy=\"headroom\"\n[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"claude_usage\"\nthreshold_pct=90\n")
			restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
				return usagebudget.Reading{Percentage: 0}, account == "work"
			})
			defer restore()
			result, err := SpawnHeraCoordinator(d, &fakeRunner{sessionPID: 1}, HeraCoordinatorSpawnInput{OrchestratorBaseName: "quota", TaskPrompt: "p", Project: "proj", Backend: backend})
			testutil.NoError(t, err)
			testutil.Equal(t, result.Task.Backend, "test")
			testutil.Equal(t, result.Task.Account, "work")
			testutil.True(t, result.Task.AccountRouted)
		})
	}
}

func TestSubCoordinatorRoutesAccountFromBlankParent(t *testing.T) {
	repo := initGitRepo(t)
	d := accountFileDB(t, repo, "[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"none\"\n")
	parent := &model.Task{ID: "blank-parent", Name: "parent", Project: "proj", Backend: "test"}
	testutil.NoError(t, d.Add(parent))
	orch, err := d.CreateHeraOrchestrator("parent", "")
	testutil.NoError(t, err)
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "proj"}, parent.ID, repo)
	testutil.NoError(t, err)
	role, err := d.CreateHeraPlannedRole(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "sub", ArgusProject: "proj", NodeKind: db.HeraNodeKindSubCoord, Prompt: "p"})
	testutil.NoError(t, err)
	result, err := MaterializeHeraSubCoordinator(d, &fakeRunner{sessionPID: 1}, HeraMaterializeInput{Role: role, TaskPrompt: "p", Project: "proj"})
	testutil.NoError(t, err)
	testutil.Equal(t, result.Task.Backend, "test")
	testutil.Equal(t, result.Task.Account, "work")
}

// TUI-process creation resolves tiers in-process, where the probe caches are
// empty. With the daemon's readings installed (the TUI does this at startup) a
// task must land on the account with room, and when every account is over its
// threshold on the least-used one — never silently on the default account.
func TestResolveTaskChoiceUsesInstalledDaemonReadings(t *testing.T) {
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	cfg := config.Config{
		Backends: map[string]config.Backend{"claude": {Command: "claude"}},
		Accounts: map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}},
		BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
			{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 90},
			{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 90},
		}},
	}
	for _, tc := range []struct {
		name              string
		def, personal     float64
		wantAccount, want string
	}{
		{"room on personal", 98, 70, "personal", "claude"},
		{"all over picks least-used", 98, 93, "personal", "claude"},
		{"all over, default least-used", 91, 99, "default", "claude"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restore := backendtier.SetUsageSource(func() (backendtier.Usage, error) {
				return backendtier.Usage{Claude: map[string]usagebudget.Reading{
					"default": {Percentage: tc.def}, "personal": {Percentage: tc.personal},
				}}, nil
			})
			defer restore()
			choice := resolveTaskChoice("", "", cfg, "", false)
			testutil.Equal(t, choice.Backend, tc.want)
			testutil.Equal(t, choice.Account, tc.wantAccount)
		})
	}
}

// An explicit task/project backend resolves by name without touching the
// usage source (hotfix: ResolveBackend runs per role per Hera refresh on the
// UI goroutine). A blank backend still routes on readings.
func TestResolveBackend_ExplicitBackendSkipsUsageReadings(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Backends = map[string]config.Backend{
		"claude": {Command: "claude"}, "codex": {Command: "codex"},
	}
	cfg.Defaults.Backend = "claude"
	cfg.Projects = map[string]config.Project{"p": {Backend: "codex"}}
	cfg.BackendRouting.Strategy = config.StrategyHeadroom
	cfg.BackendRouting.Tiers = []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 90},
		{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 90},
	}
	reads := 0
	t.Cleanup(backendtier.SetUsageSource(func() (backendtier.Usage, error) {
		reads++
		return backendtier.Usage{}, nil
	}))

	b, err := ResolveBackend(&model.Task{Backend: "claude", Project: "p"}, cfg)
	testutil.NoError(t, err)
	testutil.Equal(t, b.Command, "claude")
	b, err = ResolveBackend(&model.Task{Project: "p"}, cfg)
	testutil.NoError(t, err)
	testutil.Equal(t, b.Command, "codex")
	testutil.Equal(t, reads, 0)

	_, err = ResolveBackend(&model.Task{}, cfg)
	testutil.NoError(t, err)
	testutil.Equal(t, reads, 1) // blank backend still routes on readings
}

func TestUnpinnedLandingOnDefaultIsStoredBlankAndRouted(t *testing.T) {
	repo := initGitRepo(t)
	d := accountFileDB(t, repo) // no tiers: nothing to route to, default account
	task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "impl", Project: "proj", Backend: "test"})
	testutil.NoError(t, err)
	testutil.Equal(t, task.Account, "")
	testutil.True(t, task.AccountRouted)
	// Its child therefore routes rather than inheriting a default pin.
	testutil.Equal(t, InheritableAccount(task), "")
}

func TestStep3BestOverallAnyBackend(t *testing.T) {
	extra := "[backend_routing]\nstrategy=\"headroom\"\n[[backend_routing.tier]]\nbackend=\"pi\"\nprobe=\"none\"\n"
	t.Run("project backend without a tier adopts best overall", func(t *testing.T) {
		repo := initGitRepo(t)
		d := accountFileDB(t, repo, extra)
		testutil.NoError(t, d.SetProject("proj", config.Project{Path: repo, Branch: "HEAD", Backend: "codex"}))
		task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "s3", Project: "proj"})
		testutil.NoError(t, err)
		testutil.Equal(t, task.Backend, "pi")
	})
	t.Run("explicit backend is never changed", func(t *testing.T) {
		repo := initGitRepo(t)
		d := accountFileDB(t, repo, extra)
		task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "s3x", Project: "proj", Backend: "codex"})
		testutil.NoError(t, err)
		testutil.Equal(t, task.Backend, "codex")
	})
	t.Run("pinned account skips step 3", func(t *testing.T) {
		cfg := config.Config{Backends: map[string]config.Backend{"codex": {Command: "codex"}, "pi": {Command: "pi"}}, Projects: map[string]config.Project{"p": {Backend: "codex"}},
			BackendRouting: config.BackendRoutingConfig{Tiers: []config.BackendTier{{Backend: "pi", Probe: config.ProbeNone}}}}
		testutil.Equal(t, resolveTaskChoice("", "p", cfg, "", false).Backend, "pi")
		testutil.Equal(t, resolveTaskChoice("", "p", cfg, "default", false).Backend, "codex")
	})
}

func TestCreateAndStartReservesAndSpreadsBurst(t *testing.T) {
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	repo := initGitRepo(t)
	extra := "[backend_routing]\nstrategy=\"headroom\"\n[[backend_routing.tier]]\nbackend=\"test\"\nprobe=\"none\"\n[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"none\"\n"
	d := accountFileDB(t, repo, extra)
	counts := map[string]int{}
	for i := 0; i < 4; i++ {
		task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "burst" + string(rune('a'+i)), Project: "proj", Backend: "test"})
		testutil.NoError(t, err)
		counts[task.Account]++
	}
	testutil.Equal(t, counts[""], 2)
	testutil.Equal(t, counts["work"], 2)
}

func TestCreateAndStartFreshensUsageOnlyWhenTiersConfigured(t *testing.T) {
	calls := 0
	defer backendtier.SetFreshener(func() { calls++ })()
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)

	repo := initGitRepo(t)
	d := accountFileDB(t, repo) // no tiers
	_, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "nofresh", Project: "proj", Backend: "test"})
	testutil.NoError(t, err)
	testutil.Equal(t, calls, 0)

	repo2 := initGitRepo(t)
	d2 := accountFileDB(t, repo2, "[backend_routing]\nstrategy=\"headroom\"\n[[backend_routing.tier]]\nbackend=\"test\"\nprobe=\"none\"\n")
	_, _, err = CreateAndStart(d2, &fakeRunner{sessionPID: 1}, CreateInput{Name: "fresh", Project: "proj", Backend: "test"})
	testutil.NoError(t, err)
	testutil.Equal(t, calls, 1)
}

// Live burst shape: a project with no backend, claude (80) + codex (95) tiers.
// Step 2 scans every tier when no backend is set (the global default does not
// count as a project backend), so the roomiest backend wins and is labelled
// best-overall.
func TestNoProjectBackendRoutesAcrossBackends(t *testing.T) {
	cfg := config.Config{
		Backends: map[string]config.Backend{"claude": {Command: "claude"}, "codex": {Command: "codex"}},
		Defaults: config.Defaults{Backend: "claude"},
		Projects: map[string]config.Project{"p": {}},
		BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
			{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
			{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 95},
		}},
	}
	for _, tc := range []struct {
		name          string
		claude, codex float64
		want          string
	}{
		{"claude roomier", 9, 40, "claude"},
		{"codex roomier", 70, 10, "codex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backendtier.ResetSpawnLedger()
			t.Cleanup(backendtier.ResetSpawnLedger)
			defer backendtier.SetUsageSource(func() (backendtier.Usage, error) {
				return backendtier.Usage{
					Claude: map[string]usagebudget.Reading{"default": {Percentage: tc.claude}},
					Codex:  &usagebudget.Reading{Percentage: tc.codex},
				}, nil
			})()
			c := resolveTaskChoice("", "p", cfg, "", false)
			testutil.Equal(t, c.Backend, tc.want)
			testutil.Equal(t, c.Step, "best-overall")
		})
	}
}

const probedTiersTOML = "[backend_routing]\nstrategy=\"headroom\"\n[[backend_routing.tier]]\nbackend=\"test\"\nprobe=\"claude_usage\"\nthreshold_pct=80\n[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"claude_usage\"\nthreshold_pct=80\n"

func claudeUsage(def, work float64) func() (backendtier.Usage, error) {
	return func() (backendtier.Usage, error) {
		return backendtier.Usage{Claude: map[string]usagebudget.Reading{
			"default": {Percentage: def}, "work": {Percentage: work},
		}}, nil
	}
}

// The fresh read must land BEFORE resolution: the freshener publishes readings
// that reverse the stale snapshot, and the task follows the fresh ones.
func TestCreateAndStartRoutesOnFreshReadingNotStaleSnapshot(t *testing.T) {
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	t.Cleanup(backendtier.SetUsageSource(claudeUsage(5, 50))) // stale: default roomy
	defer backendtier.SetFreshener(func() {
		backendtier.SetUsageSource(claudeUsage(95, 10)) // fresh: default depleted
	})()
	repo := initGitRepo(t)
	d := accountFileDB(t, repo, probedTiersTOML)
	task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "freshfirst", Project: "proj", Backend: "test"})
	testutil.NoError(t, err)
	testutil.Equal(t, task.Account, "work")
	testutil.True(t, task.AccountRouted)
}

// Burst spreading through CreateAndStart on probed (known) tiers: without the
// per-spawn penalty all four would land on work (38 < 40).
func TestCreateAndStartSpreadsBurstOnKnownReadings(t *testing.T) {
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	t.Cleanup(backendtier.SetUsageSource(claudeUsage(40, 38)))
	repo := initGitRepo(t)
	d := accountFileDB(t, repo, probedTiersTOML)
	var got []string
	for i := 0; i < 4; i++ {
		task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "kb" + string(rune('a'+i)), Project: "proj", Backend: "test"})
		testutil.NoError(t, err)
		got = append(got, task.Account)
	}
	testutil.DeepEqual(t, got, []string{"work", "", "work", ""})
}

func TestCreateAndStartRollsBackReservationOnStartFailure(t *testing.T) {
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	repo := initGitRepo(t)
	extra := "[backend_routing]\nstrategy=\"headroom\"\n[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"none\"\n"
	d := accountFileDB(t, repo, extra)
	_, _, err := CreateAndStart(d, &fakeRunner{startErr: errors.New("boom")}, CreateInput{Name: "fail", Project: "proj", Backend: "test"})
	testutil.Error(t, err)
	testutil.Equal(t, backendtier.PendingSpawns("test", "work"), 0)

	_, _, err = CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "ok", Project: "proj", Backend: "test"})
	testutil.NoError(t, err)
	testutil.Equal(t, backendtier.PendingSpawns("test", "work"), 1)
}

func TestCreateAndStartRollsBackReservationOnAccountError(t *testing.T) {
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	repo := initGitRepo(t)
	d := accountFileDB(t, repo, "[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"work\"\nprobe=\"none\"\n")
	_, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "bad", Project: "proj", Backend: "test", Account: "cx"})
	testutil.Error(t, err)
	testutil.Equal(t, backendtier.PendingSpawns("test", "work"), 0)
	testutil.Equal(t, backendtier.PendingSpawns("test", "cx"), 0)
}

func TestCreateAndStartChargesAccountTheTaskRunsOn(t *testing.T) {
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	repo := initGitRepo(t)
	// The only tier names the codex-only account "cx", which cannot run the
	// claude-command "test" backend, so the task falls back to default.
	d := accountFileDB(t, repo, "[[backend_routing.tier]]\nbackend=\"test\"\naccount=\"cx\"\nprobe=\"none\"\n")
	task, _, err := CreateAndStart(d, &fakeRunner{sessionPID: 1}, CreateInput{Name: "fb", Project: "proj", Backend: "test"})
	testutil.NoError(t, err)
	testutil.Equal(t, task.Account, "")
	testutil.Equal(t, backendtier.PendingSpawns("test", "cx"), 0)
	testutil.Equal(t, backendtier.PendingSpawns("test", "default"), 1)
}
