package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"

	_ "embed"

	_ "modernc.org/sqlite"
)

const state_file = "state.db"

const schema = `CREATE TABLE label (
	hash blob not null primary key,
	label text not null
);`

const sqlite_options = `
pragma journal_mode = WAL;
pragma synchronous = normal;
pragma temp_store = memory;
pragma mmap_size = 30000000000;
pragma journal_size_limit = 6144000;
pragma busy_timeout = 10000;
`

type label struct {
	hash  uint64
	title string
}

var recordWrites chan label

func recordWrite(ctx context.Context, driver *sql.DB, logger *slog.Logger, l label) {
	hashBuff := binary.BigEndian.AppendUint64([]byte{}, l.hash)
	_, err := driver.ExecContext(
		ctx,
		`INSERT INTO label (hash, label) VALUES (?, ?)
ON CONFLICT DO UPDATE SET
	label = excluded.label`,
		hashBuff,
		l.title,
	)
	if err != nil {
		logger.Error("insert db", "err", err)
	}
}

func recordWriteWorker(ctx context.Context, driver *sql.DB, logger *slog.Logger) {
	recordWrites = make(chan label)
	for {
		select {
		case <-ctx.Done():
			close(recordWrites)
			return
		case l := <-recordWrites:
			recordWrite(ctx, driver, logger, l)
		}
	}
}

func configureSQLite(ctx context.Context, driver *sql.DB, logger *slog.Logger) (err error) {
	_, err = driver.ExecContext(ctx, sqlite_options)

	driver.SetMaxOpenConns(1)
	driver.SetMaxIdleConns(1)
	driver.SetConnMaxLifetime(0)

	go recordWriteWorker(ctx, driver, logger)

	return
}

func migrateSQLite(ctx context.Context, logger *slog.Logger, driver *sql.DB) (err error) {
	tx, err := driver.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	res, err := tx.QueryContext(ctx, `SELECT name 
FROM sqlite_master
WHERE type='table' AND name='label'`)
	if err != nil {
		return
	}
	defer res.Close()

	if !res.Next() {
		_, err = tx.ExecContext(ctx, schema)
		if err != nil {
			return
		}
		logger.Info("schema migrated")
		err = tx.Commit()
	}

	return
}

func OpenDB(ctx context.Context, logger *slog.Logger) (driver *sql.DB, err error) {
	driver, err = sql.Open("sqlite", fmt.Sprintf("file:%s", state_file))
	if err != nil {
		return
	}

	err = driver.PingContext(ctx)
	if err != nil {
		return
	}

	err = configureSQLite(ctx, driver, logger)
	if err != nil {
		return
	}
	err = migrateSQLite(ctx, logger, driver)
	if err != nil {
		return
	}

	return
}

func CloseDB(driver *sql.DB) error {
	defer driver.Close()
	_, err := driver.Exec("pragma optimize")
	return err
}

func LookupLabel(ctx context.Context, driver *sql.DB, hash uint64) (label string, ok bool, err error) {
	hashBuff := binary.BigEndian.AppendUint64([]byte{}, hash)
	res, err := driver.QueryContext(
		ctx,
		"SELECT (label) FROM label WHERE hash = ?",
		hashBuff,
	)
	if err != nil {
		ok = false
		return
	}
	defer res.Close()

	if !res.Next() {
		err = nil
		ok = false
		return
	}

	var title string
	err = res.Scan(&title)
	if err != nil {
		ok = false
		return
	}
	ok = true
	return
}

func RecordLabel(ctx context.Context, hash uint64, title string) (err error) {
	select {
	case <-ctx.Done():
		err = context.Canceled
	case recordWrites <- label{
		hash:  hash,
		title: title,
	}:
	}
	return
}
