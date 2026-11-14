package api

import (
	"slices"
	"sort"

	"github.com/argus-foundry/argus/internal/config"
)

// accountReferencesJSON lists what still points at an account.
type accountReferencesJSON struct {
	ArtifactProjects []string `json:"artifact_projects,omitempty"`
	Resident         bool     `json:"resident,omitempty"`
	Tasks            []string `json:"tasks,omitempty"`
	Default          bool     `json:"default,omitempty"`
	Projects         []string `json:"projects,omitempty"`
	Tiers            []string `json:"tiers,omitempty"`
}

func (r accountReferencesJSON) empty() bool {
	return len(r.Tasks) == 0 && !r.Default && len(r.Projects) == 0 && len(r.Tiers) == 0 && len(r.ArtifactProjects) == 0 && !r.Resident
}

// Reference checks include task kinds hidden from the ordinary task list.
func (s *Server) accountReferences(cfg config.Config, name string) (accountReferencesJSON, error) {
	var refs accountReferencesJSON
	tasks, err := s.db.TaskAccountReferences(name)
	if err != nil {
		return refs, err
	}
	refs.Tasks = tasks
	refs.Resident = slices.Contains(cfg.Resident.Accounts, name)
	for project, account := range cfg.ProjectArtifactAccounts {
		if account == name {
			refs.ArtifactProjects = append(refs.ArtifactProjects, project)
		}
	}
	sort.Strings(refs.ArtifactProjects)
	refs.Default = cfg.DefaultAccount == name
	for p, a := range cfg.ProjectAccounts {
		if a == name {
			refs.Projects = append(refs.Projects, p)
		}
	}
	sort.Strings(refs.Projects)
	for _, t := range cfg.BackendRouting.Tiers {
		if t.Account == name {
			refs.Tiers = append(refs.Tiers, t.Backend)
		}
	}
	return refs, nil
}
