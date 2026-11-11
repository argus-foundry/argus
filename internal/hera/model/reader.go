package model

import (
	"errors"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
)

var errNoDataVersion = errors.New("reader has no data version")

// HeraReader is the narrow, READ-ONLY data-access seam the rail builds from.
// It lists exactly the methods BuildModel calls on the M1 hera store.
//
// Local mode passes the real *db.DB, which satisfies this implicitly. Remote
// mode (*apistore.Store) does NOT implement the hera_* methods — the TUI
// degrades by passing a nil HeraReader to the page, which renders an
// "Hera unavailable in remote mode" banner instead of panicking or breaking
// the --remote build (see gotchas/remote-tui.md). The App resolves the reader
// once via a `a.db.(*db.DB)` type-assert, mirroring the other four local-only
// type-assert sites.
//
// ListMetaByNamespace is the one method also on store.Store (task_meta is
// task-addressed, not hera-addressed); the rail uses it to read the
// meta:hera.ready_to_close flag (M4) for a role's bound task.
type HeraReader interface {
	ListHeraOrchestrators(includeArchived bool) ([]*db.HeraOrchestrator, error)
	ListHeraRoles(orchID int64, includeArchived bool) ([]*db.HeraRole, error)
	ListHeraLiveBindings() ([]*db.HeraBinding, error)
	// ListHeraLatestBindings returns the most-recent binding per role regardless
	// of liveness — the structural rail bridge keys off it so an ended-but-not-
	// torn-down link still nests its child sub-orchestrator.
	ListHeraLatestBindings() ([]*db.HeraBinding, error)
	HeraRoleStatusFor(roleID int64) (*db.HeraRoleStatus, error)
	ListMetaByNamespace(namespace string) (map[string]map[string]string, error)
	// Tasks supplies the argus task snapshot so BuildModel can stamp each bound
	// role's TaskStatus/TaskResult (the orchestration-tree DAG colours nodes by
	// task progress). *db.DB satisfies this; remote mode passes a nil reader.
	Tasks() ([]*model.Task, error)
	// ListHeraBlocks returns the orchestrator's blocking edges (the plan DAG's
	// dependency edges). One bulk read per BuildModel; *db.DB satisfies it,
	// remote mode's nil reader degrades unchanged. See add-hera-plan-view D8.
	ListHeraBlocks(orchID int64) ([]db.HeraBlock, error)
	// SumHeraRoleCostAccruedByOrchestrator and SumNukedHeraRolesCostByOrchestrator
	// (add-coordinator-cost-estimate) are the bulk, one-query-per-orchestrator
	// reads feeding RoleView.CostUSDAccrued and OrchView.NukedRolesCostUSD —
	// see design.md Decision 4 for why the nuked sum is a separate, dedicated
	// query rather than an ListHeraRoles parameter.
	SumHeraRoleCostAccruedByOrchestrator(orchID int64) (map[int64]float64, error)
	SumNukedHeraRolesCostByOrchestrator(orchID int64) (float64, error)
}

// ResidentLister is the one extra read the resident view needs: the standing
// orchestrator (binding-identified), which the generic list hides. *db.DB
// satisfies it.
type ResidentLister interface {
	ListResidentOrchestrators(includeArchived bool) ([]*db.HeraOrchestrator, error)
}

// ResidentReader narrows a HeraReader to the standing orchestrator: BuildModel
// lists orchestrators through it, so the resident view is the ordinary Hera
// model scoped to exactly one team. Every other read passes through.
type ResidentReader struct {
	HeraReader
	lister ResidentLister
}

// NewResidentReader scopes inner to the standing orchestrator.
func NewResidentReader(inner HeraReader, lister ResidentLister) *ResidentReader {
	return &ResidentReader{HeraReader: inner, lister: lister}
}

// ListHeraOrchestrators returns only the standing orchestrator.
func (r *ResidentReader) ListHeraOrchestrators(includeArchived bool) ([]*db.HeraOrchestrator, error) {
	return r.lister.ListResidentOrchestrators(includeArchived)
}

// DataVersion forwards the inner store's change fingerprint (the page's
// rebuild gate type-asserts for it) so the scoped reader keeps the gate.
func (r *ResidentReader) DataVersion() (int64, error) {
	if dv, ok := r.HeraReader.(interface{ DataVersion() (int64, error) }); ok {
		return dv.DataVersion()
	}
	return 0, errNoDataVersion
}

// ListHeraBoardItems preserves the model builder's optional board read through
// the scoped wrapper; embedding HeraReader alone hides optional methods.
func (r *ResidentReader) ListHeraBoardItems(roleID int64, resolved bool) ([]*db.HeraBoardItem, error) {
	if br, ok := r.HeraReader.(interface {
		ListHeraBoardItems(int64, bool) ([]*db.HeraBoardItem, error)
	}); ok {
		return br.ListHeraBoardItems(roleID, resolved)
	}
	return nil, nil
}
