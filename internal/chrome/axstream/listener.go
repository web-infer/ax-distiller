package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"log/slog"
	"sync"

	"github.com/go-rod/rod"
)

// performance tuning
const (
	sub_worker_count        = 8
	sub_channel_buffer_size = 8
	out_channel_buffer_size = 8
)

type listener struct {
	// external parameters, immutable
	logger *slog.Logger
	ctx    context.Context
	events chan<- Event
	page   *rod.Page

	// allocated on init, immutable
	cdpResPool *sync.Pool

	// mutable treeState, reset on page reset
	treeState *listenerTreeState
}

func newListener(ctx context.Context, logger *slog.Logger, out chan<- Event, page *rod.Page) listener {
	l := listener{
		ctx:    ctx,
		logger: logger.WithGroup("listener"),
		page:   page,
		events: out,
		cdpResPool: &sync.Pool{
			New: func() any {
				return &cdp.AXNodesResult{
					Nodes: make([]cdp.AXNode, 0),
				}
			},
		},
	}

	l.logger.Debug("init page state")
	l.treeState = newListenerPageState(l)

	// we do not spawn multiple event workers because events must be handled in
	// order (ex. some node is deleted and re-inserted with a different tree,
	// if this ordering is corrupted catastrophic results will follow)
	//
	// we avoid blockage by async/IO operations by sending them all into the
	// subscription worker goroutines and buffering the channel to avoid being
	// blocked by channel write, this also naturally functions as a
	// backpressure mechanism (to stop event loop from sending more work to the
	// subscription workers if they cannot keep up & prevent potential memory
	// usage skyrocketing)
	l.logger.Debug("init event source worker")
	go l.eventSourceWorker()
	return l
}

// TODO: unnecessary code for now
//
//	func isInvalidNodeCDPErr(err error) bool {
//		return strings.Contains(err.Error(), "Could not find") ||
//			strings.Contains(err.Error(), "Invalid ID") ||
//			strings.Contains(err.Error(), "No node found")
//	}
