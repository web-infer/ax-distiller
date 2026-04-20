package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"

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
	Type    EventType
	Added   []*cdp.AXNodeWithRelatives
	Updated []*cdp.AXNodeWithRelatives
}

func Listen(ctx context.Context, page *rod.Page) (out <-chan Event, err error) {
	page = page.Context(ctx)

	err = cdp.CommandUnary(ctx, page, proto.AccessibilityEnable{})
	if err != nil {
		return
	}

	events := make(chan Event, out_channel_buffer_size)
	out = events
	newListener(ctx, events, page)

	return
}
