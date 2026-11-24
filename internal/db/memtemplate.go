package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"

	"modernc.org/sqlite"
)

// memDSN is the in-memory DSN every OpenInMemory connection uses.
const memDSN = ":memory:?_pragma=foreign_keys(on)"

// memTemplate is a process-wide in-memory database holding the fully created
// (unseeded) schema. OpenInMemory clones it with SQLite's online backup API
// (a page copy) instead of replaying every CREATE/ALTER statement.
//
// The template connection is deliberately kept for the life of the process: it
// is one pure in-memory connection with no goroutine or file handle behind it,
// so there is nothing to leak, and closing it would force every later open to
// rebuild the schema. Only Open(path) runs real migrations; the template covers
// createTables alone, which is all OpenInMemory ever ran.
var memTemplate struct {
	mu    sync.Mutex // guards conn and serializes cloning from it
	conn  driver.Conn
	build func() (driver.Conn, error) // test seam; nil means newMemTemplate
}

// memTemplateConn returns the template, building it on first use. A build
// failure is returned as-is and not cached, so the next open retries. The
// caller must hold memTemplate.mu.
func memTemplateConn() (driver.Conn, error) {
	if memTemplate.conn != nil {
		return memTemplate.conn, nil
	}
	build := memTemplate.build
	if build == nil {
		build = newMemTemplate
	}
	c, err := build()
	if err != nil {
		return nil, fmt.Errorf("building in-memory template: %w", err)
	}
	memTemplate.conn = c
	return c, nil
}

func newMemTemplate() (driver.Conn, error) {
	pool, err := sql.Open("sqlite", memDSN)
	if err != nil {
		return nil, err
	}
	// The pool must not outlive this call: database/sql keeps an opener
	// goroutine per pool, which would deadlock a synctest bubble.
	defer pool.Close() //nolint:errcheck
	pool.SetMaxOpenConns(1)
	if err := (&DB{conn: pool}).createTables(); err != nil {
		return nil, err
	}
	c, err := pool.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	defer c.Close() //nolint:errcheck
	var out driver.Conn
	err = c.Raw(func(dc any) error {
		var cerr error
		out, cerr = cloneMem(dc)
		return cerr
	})
	return out, err
}

// cloneMem copies src's main database into a new private in-memory connection.
func cloneMem(src any) (driver.Conn, error) {
	b, ok := src.(interface {
		NewBackup(string) (*sqlite.Backup, error)
	})
	if !ok {
		return nil, fmt.Errorf("sqlite driver has no backup API")
	}
	bk, err := b.NewBackup(memDSN)
	if err != nil {
		return nil, err
	}
	if _, err := bk.Step(-1); err != nil {
		_ = bk.Finish()
		return nil, err
	}
	return bk.Commit()
}

type memConnector struct{}

func (memConnector) Driver() driver.Driver { return &sqlite.Driver{} }

// Connect returns a private in-memory clone of the template.
func (memConnector) Connect(context.Context) (driver.Conn, error) {
	memTemplate.mu.Lock()
	defer memTemplate.mu.Unlock()
	t, err := memTemplateConn()
	if err != nil {
		return nil, err
	}
	return cloneMem(t)
}
