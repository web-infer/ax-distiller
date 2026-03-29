package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"

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

type Event struct {
	Type EventType
	// ID refers to different nodes based on the value of Type:
	//
	//  - EVENT_REPLACE: The ID should be an empty string as REPLACE
	//    would indicate that the root is replaced.
	//  - EVENT_INSERT: The ID of the previous sibling the
	//    newly inserted node is after.
	//  - EVENT_REMOVE: The ID of the node + subtree to remove.
	ID      proto.DOMBackendNodeID
	Subtree *cdp.AXNodeWithRelatives
}

const (
	event_dom_documentUpdated   = "DOM.documentUpdated"
	event_dom_childNodeRemoved  = "DOM.childNodeRemoved"
	event_dom_childNodeInserted = "DOM.childNodeInserted"
	event_ax_loadComplete       = "Accessibility.loadComplete"
)

type workerState uint8

const (
	worker_init workerState = iota
	worker_hydrated
)

func Listen(ctx context.Context, out chan<- Event, page *rod.Page) (err error) {
	page = page.Context(ctx)

	err = sonic.Pretouch(reflect.TypeFor[proto.AccessibilityAXNode]())
	if err != nil {
		panic(err)
	}
	err = sonic.Pretouch(reflect.TypeFor[proto.DOMNode]())
	if err != nil {
		panic(err)
	}

	type pageEvent struct {
		method string
		buff   []byte
	}
	events := make(chan pageEvent, runtime.NumCPU())
	fetcher := cdp.NewAXSubtreeFetcher(page, 4)
	fetcher.Start(ctx)

	// we have one worker that processes event asynchronously to ensure the
	// order which events are processed is correct
	go func() {
		var state workerState
		frontendBackendDOMIDMap := make(map[proto.DOMNodeID]proto.DOMBackendNodeID)

		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				switch e.method {
				case event_dom_documentUpdated:
					switch state {
					case worker_init:
					case worker_hydrated:
					}
					slog.Info("document updated!")
				case event_ax_loadComplete:
					/*
						// retrieve the new root's backendDOMNodeId
						nodeIDAST, err := sonic.Get(e.buff, "root", "backendDOMNodeId")
						if err != nil {
							panic(err)
						}
						nodeID, err := nodeIDAST.Int64()
						if err != nil {
							panic(err)
						}

						// get the new root's AXNodeID
						fetchRelatives := true
						rootLookup, err := cdp.Command(ctx, page, cdp.GetPartialAXTree{
							BackendNodeID:  proto.DOMBackendNodeID(nodeID),
							FetchRelatives: &fetchRelatives,
						})
						if err != nil {
							panic(err)
						}
						if len(rootLookup.Nodes) != 1 {
							panic("assert failed: root partial result len should = 1")
						}
						root := fetcher.Fetch(ctx, rootLookup.Nodes[0].NodeID)
					*/

					// this should work fine, I am not sure why the above code
					// was there before
					rootIDAST, err := sonic.Get(e.buff, "root", "nodeId")
					if err != nil {
						panic(err)
					}
					nodeID, err := rootIDAST.String()
					if err != nil {
						panic(err)
					}

					// refetch
					root := fetcher.Fetch(ctx, proto.AccessibilityAXNodeID(nodeID))

					// touch whole DOM to subscribe to all DOM elements
					depth := -1
					doc, err := cdp.Command(ctx, page, cdp.DOMGetDocument{
						Depth:  &depth,
						Pierce: true,
					})
					if err != nil {
						panic(err)
					}

					// map all frontend ids to backend dom node ids
					frontendBackendDOMIDMap = map[proto.DOMNodeID]proto.DOMBackendNodeID{} // clear map
					queue := []cdp.DOMNode{doc.Root}
					for len(queue) > 0 {
						popped := queue[0]
						frontendBackendDOMIDMap[popped.NodeID] = popped.BackendNodeID
						queue = queue[1:]
						queue = append(queue, popped.Children...)
					}

					out <- Event{
						Type:    EVENT_REPLACE,
						Subtree: root,
					}
				case event_dom_childNodeInserted:
					// get the
					var params proto.DOMChildNodeInserted
					err := sonic.ConfigFastest.Unmarshal(e.buff, &params)
					if err != nil {
						panic(err)
					}

					// we lookup the modified parent and the previous sibling
					// which the inserted node is after
					prevSibling, err := cdp.Command(ctx, page, cdp.DOMGetBackendNodeID{
						NodeID: params.PreviousNodeID,
					})
					if err != nil {
						panic(err)
					}

					// we fetch the subtree of the newly inserted node
					fetchRelatives := true
					lookup, err := cdp.Command(ctx, page, cdp.GetPartialAXTree{
						BackendNodeID:  proto.DOMBackendNodeID(params.Node.BackendNodeID),
						FetchRelatives: &fetchRelatives,
					})
					if err != nil {
						panic(err)
					}
					if len(lookup.Nodes) < 1 {
						panic("assert failed: fetch partial ax tree of a single node should return at least 1 node")
					}
					subtree := fetcher.Fetch(ctx, lookup.Nodes[0].NodeID)

					out <- Event{
						Type:    EVENT_INSERT,
						ID:      prevSibling.Node.BackendNodeID,
						Subtree: subtree,
					}
				case event_dom_childNodeRemoved:
					var params proto.DOMChildNodeRemoved
					err := sonic.ConfigFastest.Unmarshal(e.buff, &params)
					if err != nil {
						panic(err)
					}
					removedLookup, err := cdp.Command(ctx, page, cdp.DOMGetBackendNodeID{
						NodeID: params.NodeID,
					})
					if err != nil {
						panic(err)
					}
					out <- Event{
						Type: EVENT_REMOVE,
						ID:   removedLookup.Node.BackendNodeID,
					}
				default:
					panic(fmt.Sprintf("unknown method: %v", e.method))
				}
			}
		}
	}()

	err = cdp.CommandUnary(ctx, page, proto.DOMEnable{})
	if err != nil {
		return
	}
	err = cdp.CommandUnary(ctx, page, proto.AccessibilityEnable{})
	if err != nil {
		return
	}

	pageEvents := page.Event()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-pageEvents:
				method := msg.Method
				buff := reflect.ValueOf(msg).Elem().FieldByName("data").Bytes()

				switch method {
				case event_ax_loadComplete, event_dom_childNodeInserted, event_dom_childNodeRemoved:
					// non-blocking but ordered
					go func() {
						events <- pageEvent{
							method: method,
							buff:   buff,
						}
					}()
				}
			}
		}
	}()
	return
}
