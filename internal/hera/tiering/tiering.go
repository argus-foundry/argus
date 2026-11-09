// Package tiering is the tview-free half of the Hera rail's diligence-tiering
// and context-pressure readouts: resolving each role's applied model/effort and
// profile warning, and its context percentage. The TUI rail and GET /api/hera
// both call it so the two surfaces cannot drift.
package tiering

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/profiles"
	"github.com/argus-foundry/argus/internal/review"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const (
	SourceTaskOverride   = "task_override"
	SourceProfile        = "profile"
	SourceBackendDefault = "backend_default"
	SourceCLIDefault     = "cli_default"
	SourceUnavailable    = "unavailable"

	BasisCurrentConfig   = "current_config"
	BasisPlannedIntent   = "planned_intent"
	DenomWorkerWindow    = "worker_window"
	DenomCoordRecycle    = "coordinator_recycle_budget"
	ContextLevelNone     = 0
	ContextLevelWarm     = 1
	ContextLevelHot      = 2
	ContextLevelCritical = 3
)

// ValidateFunc loads and validates a named profile.
type ValidateFunc func(l *profiles.Loader, name string, cfg config.Config) (*profiles.Profile, []error)

// DefaultValidate uses the same profile validation as launch resolution.
func DefaultValidate(l *profiles.Loader, name string, cfg config.Config) (*profiles.Profile, []error) {
	return l.ValidateName(name, cfg, agent.KnownModels, review.NewValidator(cfg))
}

// Resolution is one role's resolved model choice. EffectiveModel is "" when the
// CLI picks its own default or the model is unavailable — never a fabricated ID.
type Resolution struct {
	RequestedModel string
	EffectiveModel string
	Effort         string
	Source         string
	Basis          string
	Profile        string
	Warning        string
}

// Options configures a Resolver. GetTask is required; Validate defaults to
// DefaultValidate and exists as a test seam to count profile loads.
type Options struct {
	Config     config.Config
	LibraryDir string
	GetTask    func(id string) (*model.Task, error)
	Validate   ValidateFunc
}

type profileKey struct{ name, repoDir string }

type profileEntry struct {
	prof *profiles.Profile
	ok   bool
}

type modelKey struct {
	model, profile, backend, account, project, archetype, repoDir string
	roleArchetype                                                 string
	prof                                                          profileKey
}

type appliedResult struct {
	model, source, warning string
}

// Resolver resolves roles for ONE snapshot/build. Everything is memoized for
// its lifetime only — nothing outlives the build, so settings, bindings and
// profile files are re-read next time. It is goroutine-confined to its build.
type Resolver struct {
	opts       Options
	profMemo   map[profileKey]profileEntry
	modelMemo  map[modelKey]appliedResult
	repoDirs   map[string]string
	calls      int
	profHits   int
	profMisses int
	modelHits  int
	modelMiss  int
}

// NewResolver snapshots the supplied config for the life of the resolver.
func NewResolver(o Options) *Resolver {
	if o.Validate == nil {
		o.Validate = DefaultValidate
	}
	return &Resolver{
		opts:      o,
		profMemo:  map[profileKey]profileEntry{},
		modelMemo: map[modelKey]appliedResult{},
		repoDirs:  map[string]string{},
	}
}

// Done logs one summary line per build (never one per role).
func (r *Resolver) Done() {
	uxlog.Log("[hera-tier] build: roles=%d profile hits=%d misses=%d model hits=%d misses=%d (1 config snapshot)",
		r.calls, r.profHits, r.profMisses, r.modelHits, r.modelMiss)
}

// RepoDir returns the in-repo profile directory for a worktree, or "" when the
// worktree has none (an absent directory behaves exactly like RepoDir=="").
func RepoDir(worktree string) string {
	if worktree == "" {
		return ""
	}
	dir := filepath.Join(worktree, ".argus", "profiles")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

func (r *Resolver) repoDirOf(wt string) string {
	if d, ok := r.repoDirs[wt]; ok {
		return d
	}
	d := RepoDir(wt)
	r.repoDirs[wt] = d
	return d
}

func (r *Resolver) loadProfile(pk profileKey) profileEntry {
	if e, ok := r.profMemo[pk]; ok {
		r.profHits++
		return e
	}
	r.profMisses++
	loader := &profiles.Loader{LibraryDir: r.opts.LibraryDir, RepoDir: pk.repoDir}
	uxlog.Log("[hera-tier] profile load name=%q repoDir=%q", pk.name, pk.repoDir)
	prof, errs := r.opts.Validate(loader, pk.name, r.opts.Config)
	e := profileEntry{prof: prof, ok: prof != nil && len(errs) == 0}
	r.profMemo[pk] = e
	return e
}

func (r *Resolver) taskFor(rv *heramodel.RoleView) *model.Task {
	if rv.TaskID != "" {
		t, err := r.opts.GetTask(rv.TaskID)
		if err == nil && t != nil {
			return t
		}
		uxlog.Log("[hera-tier] task fetch failed for role %d, using planned intent: %v", rv.RoleID, err)
	}
	return &model.Task{Archetype: rv.Archetype, Project: rv.ArgusProject, Worktree: rv.WorktreePath}
}

// explicitProfile is the profile the operator pointed at: the task's per-spawn
// override, else the project's binding. "" means the silent "default" fallback.
func (r *Resolver) explicitProfile(t *model.Task, project string, useTask bool) string {
	if useTask {
		if p := strings.TrimSpace(t.Profile); p != "" {
			return p
		}
	}
	if project != "" {
		if p, ok := r.opts.Config.Projects[project]; ok {
			return p.Profile
		}
	}
	return ""
}

// Annotate stamps AppliedModel/AppliedEffort/ProfileWarning and ContextPercent
// on one role (the TUI rail's per-role annotation).
func (r *Resolver) Annotate(rv *heramodel.RoleView) {
	if rv == nil {
		return
	}
	r.calls++
	rv.ContextPercent = ContextPercent(rv.ContextSize, ContextBudget(rv.Kind, r.opts.Config))
	prof, res, ok := r.resolve(rv, false)
	rv.ProfileWarning = res.profileWarning
	if !ok {
		return
	}
	rv.AppliedEffort = prof.effort
	rv.AppliedModel = prof.applied
}

// Resolve returns the full read-only model resolution for one role.
func (r *Resolver) Resolve(rv *heramodel.RoleView) Resolution {
	r.calls++
	_, res, _ := r.resolve(rv, true)
	return res.out
}

type annotated struct {
	effort, applied string
}

type resolved struct {
	profileWarning string
	out            Resolution
}

func (r *Resolver) resolve(rv *heramodel.RoleView, full bool) (annotated, resolved, bool) {
	var t *model.Task
	if full || rv.Archetype != "" {
		t = r.taskFor(rv)
	} else {
		t = &model.Task{Archetype: rv.Archetype, Project: rv.ArgusProject, Worktree: rv.WorktreePath}
	}
	explicit := r.explicitProfile(t, t.Project, true)
	name := explicit
	if name == "" {
		name = "default"
	}
	pk := profileKey{name: name, repoDir: r.repoDirOf(t.Worktree)}
	entry := r.loadProfile(pk)

	out := Resolution{Basis: BasisCurrentConfig, Profile: name, RequestedModel: strings.TrimSpace(t.Model)}
	if rv.TaskID == "" {
		out.Basis = BasisPlannedIntent
	}
	var res resolved
	if !entry.ok && explicit != "" {
		res.profileWarning = fmt.Sprintf("profile %q missing or invalid", name)
	}
	if !entry.ok {
		if full || rv.Archetype != "" {
			r.fillApplied(&out, t, rv.Archetype, nil, pk)
			out.Warning = joinWarn(res.profileWarning, out.Warning)
		}
		res.out = out
		return annotated{applied: out.EffectiveModel}, res, !full && rv.Archetype != "" && out.Source == SourceTaskOverride
	}
	// Empty role archetypes intentionally hide the TUI model and effort readout.
	if !full && rv.Archetype == "" {
		return annotated{}, res, false
	}
	prof := entry.prof
	var a annotated
	a.effort = strings.TrimSpace(prof.Archetype[strings.TrimSpace(t.Archetype)].Effort)
	out.Effort = a.effort
	ap := r.fillApplied(&out, t, rv.Archetype, prof, pk)
	a.applied = ap
	// The TUI can show profile intent when its configured backend is unavailable.
	if !full && out.Source == SourceUnavailable {
		a.applied = r.plannedModel(t, rv.Archetype, prof)
	}
	out.Warning = joinWarn(res.profileWarning, out.Warning)
	res.out = out
	return a, res, true
}

func joinWarn(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}

func (r *Resolver) fillApplied(out *Resolution, t *model.Task, archetype string, prof *profiles.Profile, pk profileKey) string {
	mk := modelKey{
		model: t.Model, profile: t.Profile, backend: t.Backend, account: t.Account,
		project: t.Project, archetype: t.Archetype, repoDir: r.repoDirOf(t.Worktree),
		roleArchetype: archetype, prof: pk,
	}
	ar, ok := r.modelMemo[mk]
	if ok {
		r.modelHits++
	} else {
		r.modelMiss++
		ar = r.applied(t, prof)
		r.modelMemo[mk] = ar
	}
	out.EffectiveModel = ar.model
	out.Source = ar.source
	out.Warning = ar.warning
	return ar.model
}

// plannedModel preserves the TUI's profile-intent fallback without exporting it as resolved REST telemetry.
func (r *Resolver) plannedModel(t *model.Task, archetype string, prof *profiles.Profile) string {
	backend := t.Backend
	if backend == "" {
		backend = r.opts.Config.Defaults.Backend
	}
	return strings.TrimSpace(prof.Archetype[archetype].Models[backend])
}

func (r *Resolver) applied(t *model.Task, prof *profiles.Profile) appliedResult {
	cfg := r.opts.Config
	backend, err := agent.ResolveBackend(t, cfg)
	if err != nil {
		uxlog.Log("[hera-tier] backend unresolved for task %q: %v", t.ID, err)
		return appliedResult{source: SourceUnavailable, warning: "backend could not be resolved"}
	}
	m, rp, overridden := agent.ResolveModelFromProfile(t, backend, cfg, prof)
	switch {
	case overridden:
		return appliedResult{model: m, source: SourceTaskOverride}
	case rp != nil:
		return appliedResult{model: m, source: SourceProfile}
	case m != "":
		return appliedResult{model: m, source: SourceBackendDefault}
	}
	return appliedResult{source: SourceCLIDefault}
}

// Source is the slice of the task store the resolver needs.
type Source interface {
	Config() config.Config
	Get(id string) (*model.Task, error)
}

// DefaultOptions builds Options over a store, with the library dir under the data dir.
func DefaultOptions(d Source) Options {
	return Options{
		Config:     d.Config(),
		LibraryDir: filepath.Join(db.DataDir(), "profiles"),
		GetTask:    d.Get,
	}
}

// ContextBudget is the kind-dependent denominator: coordinators use the
// recycle-nudge POLICY threshold (not a window size); everything else uses the
// worker context window, so the 40/65/90 tiers fire at the intended fractions.
func ContextBudget(kind db.HeraRoleKind, cfg config.Config) int {
	if kind == db.HeraKindCoordinator {
		return cfg.Hera.CoordinatorContextBudget
	}
	return cfg.Hera.WorkerContextWindow
}

// ContextPercent converts a raw context_size token count into a 0-100
// percentage of budget. budget <= 0 and size <= 0 resolve to 0; the result caps
// at 100 since a worker has no hard stop.
func ContextPercent(size, budget int) int {
	if budget <= 0 || size <= 0 {
		return 0
	}
	pct := size * 100 / budget
	if pct > 100 {
		pct = 100
	}
	return pct
}

// ContextLevel maps a percentage to the rail indicator tier: warm >=40,
// hot >=65, critical >=90.
func ContextLevel(percent int) int {
	switch {
	case percent >= 90:
		return ContextLevelCritical
	case percent >= 65:
		return ContextLevelHot
	case percent >= 40:
		return ContextLevelWarm
	}
	return ContextLevelNone
}

// Context is a role's measured context pressure. DenominatorTokens and Percent
// are nil when the configured budget is unusable but the token count is valid.
type Context struct {
	Tokens            int
	DenominatorKind   string
	DenominatorTokens *int
	Percent           *int
}

// ContextFor returns nil when the role has no valid observation (size <= 0 —
// the model carries 0 for absent or unparseable context_size).
func ContextFor(rv *heramodel.RoleView, cfg config.Config) *Context {
	if rv == nil || rv.ContextSize <= 0 {
		return nil
	}
	c := &Context{Tokens: rv.ContextSize, DenominatorKind: DenomWorkerWindow}
	if rv.Kind == db.HeraKindCoordinator {
		c.DenominatorKind = DenomCoordRecycle
	}
	budget := ContextBudget(rv.Kind, cfg)
	if budget > 0 {
		pct := ContextPercent(rv.ContextSize, budget)
		c.DenominatorTokens = &budget
		c.Percent = &pct
	}
	return c
}
