package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/argus-foundry/argus/internal/config"
)

var ErrProjectNotFound = errors.New("project not found")

type ProjectSaveOptions struct {
	afterRead       func()
	PreserveProfile bool
	RequireExisting bool
}

// SaveProject preserves omitted profiles and detects no-ops against the committed row.
func (d *DB) SaveProject(name string, p config.Project, options ProjectSaveOptions) (config.Project, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ctx := context.Background()
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return config.Project{}, false, err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return config.Project{}, false, err
	}
	defer func() { _, _ = conn.ExecContext(ctx, `ROLLBACK`) }()
	var before [8]string
	err = conn.QueryRowContext(ctx, `SELECT path, branch, backend, sandbox_enabled, sandbox_deny_read, sandbox_extra_write, sandbox_allow_apple_events, profile FROM projects WHERE name=?`, name).Scan(&before[0], &before[1], &before[2], &before[3], &before[4], &before[5], &before[6], &before[7])
	if options.afterRead != nil {
		options.afterRead()
	}
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return config.Project{}, false, err
	}
	if !exists && options.RequireExisting {
		return config.Project{}, false, ErrProjectNotFound
	}
	if options.PreserveProfile && exists {
		p.Profile = before[7]
	}
	enabled := ""
	if p.Sandbox.Enabled != nil {
		if *p.Sandbox.Enabled {
			enabled = "true"
		} else {
			enabled = "false"
		}
	}
	after := [8]string{p.Path, p.Branch, p.Backend, enabled, strings.Join(p.Sandbox.DenyRead, ","), strings.Join(p.Sandbox.ExtraWrite, ","), strings.Join(p.Sandbox.AllowAppleEvents, ","), p.Profile}
	if exists && before == after {
		return p, false, nil
	}
	_, err = conn.ExecContext(ctx, `INSERT OR REPLACE INTO projects (name,path,branch,backend,sandbox_enabled,sandbox_deny_read,sandbox_extra_write,sandbox_allow_apple_events,profile) VALUES (?,?,?,?,?,?,?,?,?)`, name, after[0], after[1], after[2], after[3], after[4], after[5], after[6], after[7])
	if err != nil {
		return config.Project{}, false, err
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return config.Project{}, false, err
	}
	return p, true, nil
}
