package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"strings"

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

func isInvalidNodeCDPErr(err error) bool {
	return strings.Contains(err.Error(), "Could not find") || strings.Contains(err.Error(), "Invalid ID")
}

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
		frontBackMap := newDOMFrontendBackendLookup()

		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				switch e.method {
				case event_dom_documentUpdated:
					slog.Info("document updated!")
					switch state {
					case worker_init:
						slog.Warn("invalid state: documentUpdated happened twice!")
					case worker_hydrated:
						frontBackMap.Reset()
						state = worker_init
					}
				case event_ax_loadComplete:
					switch state {
					case worker_init, worker_hydrated:
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
						root, err := fetcher.Fetch(ctx, proto.AccessibilityAXNodeID(nodeID))
						if err != nil {
							if isInvalidNodeCDPErr(err) {
								// stale event, abort
								break
							}
							panic(err)
						}

						// touch whole DOM to subscribe to all DOM elements
						depth := -1
						doc, err := cdp.Command(ctx, page, cdp.DOMGetDocument{
							Depth:  &depth,
							Pierce: true,
						})
						if err != nil {
							if isInvalidNodeCDPErr(err) {
								// stale event, abort
								break
							}
							panic(err)
						}

						// map all frontend ids to backend dom node ids
						frontBackMap.Index(doc.Root)

						out <- Event{
							Type:    EVENT_REPLACE,
							Subtree: root,
						}
						state = worker_hydrated
					}
				case event_dom_childNodeInserted:
					switch state {
					case worker_init:
						slog.Warn("invalid state: childNodeInserted happened before hydration!")
					case worker_hydrated:
						var params proto.DOMChildNodeInserted
						err := sonic.ConfigFastest.Unmarshal(e.buff, &params)
						if err != nil {
							panic(err)
						}

						// we lookup the modified parent and the previous sibling
						// which the inserted node is after
						parent, err := cdp.Command(ctx, page, cdp.DOMGetBackendNodeID{
							NodeID: &params.ParentNodeID,
						})
						if err != nil {
							if isInvalidNodeCDPErr(err) {
								break
							}
							panic(err)
						}
						parentID := parent.Node.BackendNodeID

						var prevSiblingID *proto.DOMBackendNodeID
						// 0 indicates the node is the first child of the parent
						if params.PreviousNodeID != 0 {
							// we lookup the modified parent and the previous sibling
							// which the inserted node is after
							prevSibling, err := cdp.Command(ctx, page, cdp.DOMGetBackendNodeID{
								NodeID: &params.PreviousNodeID,
							})
							if err != nil {
								if isInvalidNodeCDPErr(err) {
									// if could not find node the event itself was
									// talking about, then it likely means this
									// event is now stale, we should drop any
									// additional processing
									break
								}
								panic(err)
							}
							prevSiblingID = &prevSibling.Node.BackendNodeID
						}

						// we fetch the subtree of the newly inserted node
						pFetchRelatives := true
						lookup, err := cdp.Command(ctx, page, cdp.GetPartialAXTree{
							BackendNodeID:  proto.DOMBackendNodeID(params.Node.BackendNodeID),
							FetchRelatives: &pFetchRelatives,
						})
						if err != nil {
							if isInvalidNodeCDPErr(err) {
								// stale event, abort
								break
							}
							panic(err)
						}
						if len(lookup.Nodes) < 1 {
							panic("assert failed: fetch partial ax tree of a single node should return at least 1 node")
						}
						subtree, err := fetcher.Fetch(ctx, lookup.Nodes[0].NodeID)
						if err != nil {
							if isInvalidNodeCDPErr(err) {
								// stale event, abort
								break
							}
							panic(err)
						}

						// we touch all the DOM nodes to subscribe to their events & index
						pDepth := -1
						desc, err := cdp.Command(ctx, page, cdp.DOMDescribeNode{
							NodeID: &params.Node.NodeID,
							Depth:  &pDepth,
							Pierce: true,
						})
						if err != nil {
							if isInvalidNodeCDPErr(err) {
								// stale event, abort
								break
							}
							panic(err)
						}
						frontBackMap.Index(desc.Node)

						out <- Event{
							Type:     EVENT_INSERT,
							ParentID: parentID,
							ID:       prevSiblingID,
							Subtree:  subtree,
						}
					}
				case event_dom_childNodeRemoved:
					switch state {
					case worker_init:
						slog.Warn("invalid state: childNodeInserted happened before hydration!")
					case worker_hydrated:
						var params proto.DOMChildNodeRemoved
						err := sonic.ConfigFastest.Unmarshal(e.buff, &params)
						if err != nil {
							panic(err)
						}
						backendNodeID, ok := frontBackMap.Lookup(params.NodeID)
						if !ok {
							// stale event, abort
							break
						}
						frontBackMap.DeIndex(params.NodeID)
						out <- Event{
							Type: EVENT_REMOVE,
							ID:   &backendNodeID,
						}
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

// domFrontendBackendLookup keeps track of a mapping from frontend DOM node IDs
// to backend DOM node IDs. This is because DOM events only provide frontend
// IDs and AX commands only provide backend IDs.
//
// Note: this is not thread-safe.
type domFrontendBackendLookup struct {
	records map[proto.DOMNodeID]domBackendRecord
}

type domBackendRecord struct {
	id proto.DOMBackendNodeID
	// we store children so that deindexing is faster later
	firstChild  proto.DOMNodeID
	nextSibling proto.DOMNodeID
}

func newDOMFrontendBackendLookup() domFrontendBackendLookup {
	return domFrontendBackendLookup{
		records: map[proto.DOMNodeID]domBackendRecord{},
	}
}

func (l domFrontendBackendLookup) Lookup(frontendID proto.DOMNodeID) (proto.DOMBackendNodeID, bool) {
	rec, ok := l.records[frontendID]
	if !ok {
		return 0, false
	}
	return rec.id, true
}

func (l domFrontendBackendLookup) indexInner(siblings []*cdp.DOMNode) proto.DOMNodeID {
	if len(siblings) == 0 {
		// the zero-value of proto.DOMNodeID indicates a nil-value (both for CDP and for us)
		return 0
	}
	node := siblings[0]

	var firstChildID proto.DOMNodeID
	if len(node.Children) > 0 {
		firstChildID = node.Children[0].NodeID
		l.indexInner(node.Children[1:])
	}

	nsID := l.indexInner(siblings[1:])
	l.records[node.NodeID] = domBackendRecord{
		id:          node.BackendNodeID,
		firstChild:  firstChildID,
		nextSibling: nsID,
	}
	return node.NodeID
}

func (l domFrontendBackendLookup) Index(root *cdp.DOMNode) proto.DOMNodeID {
	return l.indexInner([]*cdp.DOMNode{root})
}

func (l domFrontendBackendLookup) DeIndex(root proto.DOMNodeID) {
	if root <= 0 {
		return
	}
	rec := l.records[root]
	delete(l.records, root)
	l.DeIndex(rec.firstChild)
	l.DeIndex(rec.nextSibling)
}

func (l domFrontendBackendLookup) Reset() {
	clear(l.records)
}
