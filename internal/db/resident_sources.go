package db

import "github.com/argus-foundry/argus/internal/config"

// ResidentSources reports file overrides for local Settings edits.
func (d *DB) ResidentSources() config.ResidentSources { return d.cfgLoader.ResidentSources() }
