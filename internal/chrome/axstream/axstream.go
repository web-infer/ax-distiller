package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"log/slog"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type EventType uint8

const (
	EVENT_RESET EventType = iota
	EVENT_PATCH
)

// Event represents an AX tree mutation event.
type Event struct {
	Type EventType
	// Updated is a list of AX nodes that have changed in some way, either 1)
	// metadata updating 2) children updating
	Updated []*cdp.AXNodeWithRelatives
}

func Listen(ctx context.Context, logger *slog.Logger, page *rod.Page) (out <-chan Event, err error) {
	logger = logger.WithGroup("axstream")
	page = page.Context(ctx)

	err = cdp.CommandUnary(ctx, page, proto.AccessibilityEnable{})
	if err != nil {
		return
	}

	events := make(chan Event, out_channel_buffer_size)
	out = events
	newListener(ctx, logger, events, page)

	return
}
