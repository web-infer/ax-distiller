package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"path/filepath"

	_ "embed"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var sqlite_options = url.Values{
	"_pragma": {
		"journal_mode(WAL)",
		"synchronous(normal)",
		"temp_store(memory)",
		"mmap_size(30000000000)",
		"journal_size_limit(6144000)",
		"busy_timeout(10000)",
	},
}

func configureSQLite(driver *sql.DB) (err error) {
	driver.SetMaxOpenConns(1)
	driver.SetMaxIdleConns(1)
	driver.SetConnMaxLifetime(0)
	return
}

type gooseLogger struct {
	logger *slog.Logger
}

func (l gooseLogger) fmt(msg string) string {
	return fmt.Sprintf("[goose] %s", msg)
}

func (l gooseLogger) Printf(format string, v ...any) {
	l.logger.Info(l.fmt(fmt.Sprintf(format, v...)))
}

func (l gooseLogger) Fatalf(format string, v ...any) {
	l.logger.Error(l.fmt(fmt.Sprintf(format, v...)))
}

func migrateSQLite(ctx context.Context, logger *slog.Logger, driver *sql.DB) (err error) {
	subfs, err := fs.Sub(migrations, "migrations")
	if err != nil {
		err = fmt.Errorf("fs sub (./migrations): %w", err)
		return
	}
	gooseProv, err := goose.NewProvider(
		goose.DialectSQLite3,
		driver,
		subfs,
		goose.WithLogger(gooseLogger{logger: logger}),
	)
	if err != nil {
		err = fmt.Errorf("init goose: %w", err)
		return
	}
	_, err = gooseProv.Up(ctx)
	if err != nil {
		err = fmt.Errorf("migrate: %w", err)
		return
	}
	return
}

func OpenDB(ctx context.Context, logger *slog.Logger, file string) (driver *sql.DB, err error) {
	abspath, err := filepath.Abs(file)
	if err != nil {
		err = fmt.Errorf("filepath abs (%s): %w", file, err)
		return
	}
	var openUrl string
	if file == ":memory:" {
		openUrl = file
	} else {
		link := &url.URL{
			Scheme:   "file",
			Path:     abspath,
			RawQuery: sqlite_options.Encode(),
		}
		openUrl = link.String()
	}
	driver, err = sql.Open("sqlite", openUrl)
	if err != nil {
		err = fmt.Errorf("sql open (%s): %w", openUrl, err)
		return
	}

	err = driver.PingContext(ctx)
	if err != nil {
		err = fmt.Errorf("sql driver ping: %w", err)
		return
	}

	err = configureSQLite(driver)
	if err != nil {
		err = fmt.Errorf("config sqlite: %w", err)
		return
	}
	err = migrateSQLite(ctx, logger, driver)
	if err != nil {
		err = fmt.Errorf("migrate sqlite: %w", err)

		return
	}

	return
}

func CloseDB(driver *sql.DB) error {
	defer driver.Close()
	_, err := driver.Exec("pragma optimize")
	return err
}
