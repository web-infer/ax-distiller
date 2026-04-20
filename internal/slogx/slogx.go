package slogx

import (
	"context"
	"iter"
	"log/slog"
)

type FilterFn = func(group string, attrs iter.Seq[slog.Attr]) bool

type FilterHandler struct {
	level  slog.Level
	next   slog.Handler
	filter FilterFn

	baseattrs []slog.Attr
	group     string
}

func NewFilterHandler(next slog.Handler, level slog.Level, filter FilterFn) *FilterHandler {
	if filter == nil {
		panic("filter function cannot be nil!")
	}
	return &FilterHandler{
		next:   next,
		level:  level,
		filter: filter,
	}
}

func (h *FilterHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	if lvl < h.level {
		return false
	}
	return h.next.Enabled(ctx, lvl)
}

func (h *FilterHandler) Handle(ctx context.Context, r slog.Record) error {
	allow := h.filter(h.group, func(yield func(slog.Attr) bool) {
		for _, a := range h.baseattrs {
			if !yield(a) {
				return
			}
		}
		r.Attrs(yield)
	})
	if !allow {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *FilterHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &FilterHandler{
		next:      h.next.WithAttrs(a),
		group:     h.group,
		filter:    h.filter,
		level:     h.level,
		baseattrs: a,
	}
}

func (h *FilterHandler) WithGroup(name string) slog.Handler {
	return &FilterHandler{
		next:      h.next.WithGroup(name),
		group:     name,
		filter:    h.filter,
		baseattrs: h.baseattrs,
		level:     h.level,
	}
}
