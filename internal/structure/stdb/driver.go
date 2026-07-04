package stdb

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"net/url"
)

//go:embed schema.sql
var schema string

func configureSQLite(driver *sql.DB) (err error) {
	// we configure driver to only open 1 connection and keep it open
	driver.SetMaxOpenConns(1)
	driver.SetMaxIdleConns(1)
	driver.SetConnMaxLifetime(0)
	return
}

var sqlite_options = url.Values{
	"_pragma": {
		// stores rollback journal in memory, since everything is in-memory
		"journal_mode(memory)",
		// no syncing with FS since everything is in-memory anyway
		"synchronous(off)",
		// determines where temp tables & indices are stored
		"temp_store(memory)",
		// everything is in-memory so we have 256 MB "in-memory" cache
		"cache_size(-262144)",
		// disable memory-mapped I/O since everything is in-memory anyway
		"mmap_size(0)",
		// only one connection will be using this DB so we avoid overhead
		// of locking
		"locking_mode(exclusive)",
		// check fkeys
		"foreign_keys(on)",
	},
}

func OpenDB(ctx context.Context, logger *slog.Logger) (driver *sql.DB, err error) {
	// stdb is an in-memory store
	driver, err = sql.Open("sqlite", fmt.Sprintf(
		":memory:?%s",
		sqlite_options.Encode(),
	))
	if err != nil {
		err = fmt.Errorf("open sqlite: %w", err)
		return
	}
	// we create the schema
	_, err = driver.ExecContext(ctx, schema)
	if err != nil {
		err = fmt.Errorf("create schema: %w", err)
		return
	}
	return
}
