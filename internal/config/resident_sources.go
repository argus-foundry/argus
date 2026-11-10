package config

// ResidentSources is Settings-only provenance, outside the account spawn manifest.
type ResidentSources struct{ WorkerAccounts map[string]bool }

func (l *FileLoader) ResidentSources() ResidentSources {
	if l == nil {
		return ResidentSources{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return ResidentSources{WorkerAccounts: cloneWorkerAccountSources(l.residentSources.WorkerAccounts)}
}

func cloneWorkerAccountSources(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
