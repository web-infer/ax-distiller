package slogx

import (
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

func DemoLogger(level slog.Level, filter FilterFn) *slog.Logger {
	tintHandle := tint.NewHandler(os.Stderr, &tint.Options{Level: level})
	filterHandle := NewFilterHandler(tintHandle, level, filter)
	logger := slog.New(filterHandle)
	logger = logger.WithGroup("main")
	return logger
}
