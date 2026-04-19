package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type EventType uint8

const (
	EVENT_REPLACE EventType = iota
	EVENT_INSERT
	EVENT_REMOVE
)

// Event represents an AX tree mutation event.
type Event struct {
	// Type indicates the Event type.
	Type EventType
	// ParentID is only set when Type = EVENT_INSERT, it indicates the parent
	// the newly inserted node is under.
	ParentID proto.DOMBackendNodeID
	// ID refers to different nodes based on the value of Type:
	//
	//  - EVENT_REPLACE: The ID should be an empty string as REPLACE
	//    would indicate that the root is replaced.
	//  - EVENT_INSERT: The ID of the previous sibling the newly inserted node
	//    is after, it is nil if it is the first child of the parent.
	//  - EVENT_REMOVE: The ID of the node + subtree to remove.
	ID *proto.DOMBackendNodeID
	// Subtree contains the new subtree to be replaced or inserted, it is nil
	// when Type = EVENT_REMOVE.
	Subtree *cdp.AXNodeWithRelatives
}

// performance tuning
const (
	sub_worker_count        = 8
	sub_channel_buffer_size = 8
	out_channel_buffer_size = 8
)

const (
	event_dom_childNodeRemoved  = "DOM.childNodeRemoved"
	event_dom_childNodeInserted = "DOM.childNodeInserted"
	event_ax_loadComplete       = "Accessibility.loadComplete"
)

func isInvalidNodeCDPErr(err error) bool {
	return strings.Contains(err.Error(), "Could not find") ||
		strings.Contains(err.Error(), "Invalid ID") ||
		strings.Contains(err.Error(), "No node found")
}

type listenContext struct {
	out           chan<- Event
	page          *rod.Page
	ctx           context.Context
	subctx        context.Context
	cancel        func()
	nodesMutex    sync.Mutex
	nodes         map[proto.AccessibilityAXNodeID]cdp.AXNode
	pool          sync.Pool
	subscriptions chan proto.AccessibilityAXNodeID
}

func newListenContext(ctx context.Context, out chan<- Event, page *rod.Page) *listenContext {
	subctx, cancel := context.WithCancel(ctx)
	return &listenContext{
		out:        out,
		page:       page,
		ctx:        ctx,
		nodesMutex: sync.Mutex{},
		nodes:      make(map[proto.AccessibilityAXNodeID]cdp.AXNode),
		subctx:     subctx,
		cancel:     cancel,
		pool: sync.Pool{
			New: func() any {
				return &cdp.AXNodesResult{
					Nodes: make([]cdp.AXNode, 0),
				}
			},
		},
		subscriptions: make(chan proto.AccessibilityAXNodeID, sub_channel_buffer_size),
	}
}

func (c *listenContext) ResetPage() {
	c.cancel()
drain:
	for {
		select {
		case <-c.subscriptions:
		default:
			break drain
		}
	}
	c.subctx, c.cancel = context.WithCancel(c.ctx)
}

func (c *listenContext) subscribeWorker() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case target := <-c.subscriptions:
			if c.subctx.Err() != nil {
				continue
			}

			// this is purely for subscribing, we do not use the result
			// of this command
			res := c.pool.Get().(*cdp.AXNodesResult)
			err := cdp.CommandOutputPtr(c.subctx, c.page, cdp.GetChildAXNodes{
				ID: target,
			}, res)
			if err != nil {
				// we ignore errors, they do not affect the result
				continue
			}

			for i, child := range res.Nodes {
				c.nodesMutex.Lock()
				_, alreadyFetched := c.nodes[child.NodeID]
				c.nodes[child.NodeID] = res.Nodes[i]
				if !alreadyFetched {
					// this must be done in a goroutine because otherwise we
					// could have a deadlock:
					//
					// 1. total workers: A and B, both awaiting work
					// 2. A receives work from channel, has 2 children (job jA.1, jA.2)
					// 3. B receives job jA.1
					// 4. A is waiting on channel <- jA.2
					// 5. B finishes job jA.1, has 2 children (jB.1, jB.2)
					// 6. B is waiting on channel <- jB.1
					// 7. A and B are both waiting on channel send, no
					//    goroutines are available to receive those channel
					//    sends
					// 8. we have a deadlock
					//
					// we cannot use select { case ...: default: } because if
					// it takes the default branch, it effectively drops the
					// job, which cannot happen
					//
					// technically we haven't solved the deadlock here, just
					// moved it so that it happens if we run out of memory from
					// creating goroutines, but such a case is quite unlikely
					//
					// the subscription ordering here also doesn't matter
					// (since the only ordering that matters is the actual
					// events the browser sends to us, not subscriptions which
					// is only dependent on the node's id) so it is okay to
					// create goroutines like this
					go func() {
						c.subscriptions <- child.NodeID
					}()
				}
				c.nodesMutex.Unlock()
			}

			c.pool.Put(res)
		}
	}
}

func (c *listenContext) eventWorker() {
	events := c.page.Event()
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg := <-events:
			method := msg.Method
			buff := reflect.ValueOf(msg).Elem().FieldByName("data").Bytes()

			switch method {
			case "Accessibility.loadComplete":
				c.ResetPage()
				var event struct {
					Root cdp.AXNode `json:"root"`
				}
				err := sonic.Unmarshal(buff, &event)
				if err != nil {
					panic(err)
				}

				// fetch root & reset state
				c.nodesMutex.Lock()
				clear(c.nodes)
				c.nodes[event.Root.NodeID] = event.Root
				c.nodesMutex.Unlock()

				c.subscriptions <- event.Root.NodeID

				// TODO: remove debugging
				slog.Info("Accessibility.loadComplete", "root", event.Root.BackendDOMNodeID)
			case "Accessibility.nodesUpdated":
				var nodes cdp.AXNodesResult
				err := sonic.Unmarshal(buff, &nodes)
				if err != nil {
					panic(err)
				}

				// fetch non-fetched nodes & update all nodes given
				for i, n := range nodes.Nodes {
					c.nodesMutex.Lock()
					_, alreadyFetched := c.nodes[n.NodeID]
					c.nodes[n.NodeID] = nodes.Nodes[i]
					c.nodesMutex.Unlock()

					if !alreadyFetched {
						c.subscriptions <- n.NodeID
					}
				}

				// TODO: remove debugging
				slog.Info("Accessibility.nodesUpdated", "nodes", len(nodes.Nodes))
			}
		}
	}
}

func Listen(ctx context.Context, page *rod.Page) (out <-chan Event, err error) {
	page = page.Context(ctx)

	err = cdp.CommandUnary(ctx, page, proto.AccessibilityEnable{})
	if err != nil {
		return
	}

	events := make(chan Event, out_channel_buffer_size)
	out = events

	listenCtx := newListenContext(ctx, events, page)
	for range sub_worker_count {
		// this is a worker that is in charge of subscribing to newly found
		// discovered nodes from events
		go listenCtx.subscribeWorker()
	}
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
	go listenCtx.eventWorker()

	return
}
