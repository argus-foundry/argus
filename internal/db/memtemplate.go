package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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

// memConnector hands out exactly one connection per OpenInMemory call. A real
// :memory: pool that opened a second connection used to get a blank database
// and fail with "no such table"; cloning the template for it would instead
// hand back a migrated-but-empty DB that silently loses reads and writes. A
// second Connect therefore fails loudly. (SetMaxOpenConns(1) is avoided: it
// turns the same mistake into a deadlock.)
type memConnector struct {
	mu     sync.Mutex
	handed bool
}

func (*memConnector) Driver() driver.Driver { return &sqlite.Driver{} }

// errMemSecondConn is returned when the pool asks for a second connection.
var errMemSecondConn = errors.New("in-memory DB supports a single connection; a second was requested")

// Connect returns a private in-memory clone of the template, once.
func (c *memConnector) Connect(context.Context) (driver.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handed {
		return nil, errMemSecondConn
	}
	memTemplate.mu.Lock()
	defer memTemplate.mu.Unlock()
	t, err := memTemplateConn()
	if err != nil {
		return nil, err
	}
	conn, err := cloneMem(t)
	if err != nil {
		return nil, err
	}
	c.handed = true
	return conn, nil
}
