package tui

import (
	"path/filepath"
	"strings"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera/tiering"
	"github.com/argus-foundry/argus/internal/profiles"
	"github.com/argus-foundry/argus/internal/tui/hera"
)

// hera_tiering.go is the App side of the plan-view diligence-tiering readout
// (add-diligence-profiles, D-VIEW). HeraPage.SetTierResolver is wired (local mode
// only) to resolveHeraTier, which stamps each RoleView's AppliedModel/Effort and
// ProfileWarning during the debounced doRefresh — OFF the Draw path, because
// resolution reads disk (profile files + the task row) and the pure projection
// must not. The plan view then renders the archetype + applied model/effort per
// node and a ⚠ warning when a project explicitly points at a missing/invalid
// profile (the runtime fail-open surfacing).

// resolveHeraTier annotates ONE role in isolation. It is a thin wrapper over a
// single-use build closure, kept for callers/tests that resolve a lone role; the
// rail refresh uses heraTierResolverForBuild directly so the work below is shared
// across every role of a build (hera-tier-resolve-once).
func (a *App) resolveHeraTier(rv *hera.RoleView) {
	annotate, _ := a.heraTierResolverForBuild()
	annotate(rv)
}

// heraTierValidate is the profile validator the rail's resolver uses; a package
// var only so tests can count loads.
var heraTierValidate tiering.ValidateFunc = tiering.DefaultValidate

// heraTierResolverForBuild returns the per-role annotation closure for ONE model
// build. The shared tiering.Resolver snapshots Config() once and memoizes for
// the closure's lifetime only (hera-tier-resolve-once): nothing outlives the
// build, so settings, bindings and profile files are re-read next time.
func (a *App) heraTierResolverForBuild() (annotate func(*hera.RoleView), done func()) {
	opts := tiering.DefaultOptions(a.db)
	opts.Validate = heraTierValidate
	r := tiering.NewResolver(opts)
	return r.Annotate, r.Done
}

// validProfileNames returns the names of on-disk diligence profiles that pass
// validation, discovered across the per-user library (~/.argus/profiles) and the
// project's in-repo directory (<projectPath>/.argus/profiles). Only valid profiles
// are returned, so the Settings project select-list can never bind a project to a
// malformed profile (settings-view requirement). A profile that fails to load or
// validate is dropped (the currently-bound name is re-added by the form's
// SetProfileOptions so an explicitly-bound-but-now-invalid name stays visible).
func (a *App) validProfileNames(projectPath string) []string {
	cfg := a.db.Config()
	loader := &profiles.Loader{LibraryDir: filepath.Join(db.DataDir(), "profiles")}
	if strings.TrimSpace(projectPath) != "" {
		loader.RepoDir = filepath.Join(projectPath, ".argus", "profiles")
	}
	var valid []string
	for _, name := range loader.Discover() {
		if p, errs := loader.ValidateName(name, cfg, agent.KnownModels, nil); p != nil && len(errs) == 0 {
			valid = append(valid, name)
		}
	}
	return valid
}
