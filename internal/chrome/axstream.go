package chrome

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"fmt"
	"reflect"
	"runtime"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type AXStreamEventType uint8

const (
	AXSTREAM_EVENT_REPLACE AXStreamEventType = iota
	AXSTREAM_EVENT_INSERT
	AXSTREAM_EVENT_REMOVE
)

type AXStreamEvent struct {
	Type AXStreamEventType
	// ID refers to different nodes based on the value of Type:
	//
	//  - AXSTREAM_EVENT_REPLACE: The ID should be an empty string as REPLACE
	//    would indicate that the root is replaced.
	//  - AXSTREAM_EVENT_INSERT: The ID of the previous sibling the
	//    newly inserted node is after.
	//  - AXSTREAM_EVENT_REMOVE: The ID of the node + subtree to remove.
	ID      proto.DOMBackendNodeID
	Subtree *cdp.AXNodeWithRelatives
}

const (
	event_dom_childNodeRemoved  = "DOM.childNodeRemoved"
	event_dom_childNodeInserted = "DOM.childNodeInserted"
	event_ax_loadComplete       = "Accessibility.loadComplete"
)

// findNodeByBackendID finds an AXNodeWithRelatives with a particular
// DOMBackendNodeID returning nil otherwise. It uses BFS as we want a more
// balanced average runtime
func findNodeByBackendID(root *cdp.AXNodeWithRelatives, id proto.DOMBackendNodeID) *cdp.AXNodeWithRelatives {
	q := []*cdp.AXNodeWithRelatives{root}
	for len(q) > 0 {
		popped := q[0]
		q = q[1:]
		if popped.BackendDOMNodeID == id {
			return popped
		}
		for child := popped.FirstChild; child != nil; child = child.NextSibling {
			q = append(q, child)
		}
	}
	return nil
}

func ListenAXStream(ctx context.Context, out chan<- AXStreamEvent, page *rod.Page) (err error) {
	err = sonic.Pretouch(reflect.TypeFor[proto.AccessibilityAXNode]())
	if err != nil {
		panic(err)
	}
	err = sonic.Pretouch(reflect.TypeFor[proto.DOMNode]())
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
		var currentRoot *cdp.AXNodeWithRelatives

		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				switch e.method {
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

					// refetch & touch everything
					root := fetcher.Fetch(ctx, proto.AccessibilityAXNodeID(nodeID))
					currentRoot = root
					out <- AXStreamEvent{
						Type:    AXSTREAM_EVENT_REPLACE,
						Subtree: root,
					}
				case event_dom_childNodeInserted:
					// get the
					var params proto.DOMChildNodeInserted
					err := sonic.ConfigFastest.Unmarshal(e.buff, &params)
					if err != nil {
						panic(err)
					}
					// we BFS instead of maintaining an index because indexing
					// & de-indexing every modified subtree every time a
					// mutation comes in is not really much faster than just
					// straight searching

					if currentRoot == nil {
						panic("assert failed: currentRoot must never be nil when child nodes are being inserted")
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

					out <- AXStreamEvent{
						Type:    AXSTREAM_EVENT_INSERT,
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
					out <- AXStreamEvent{
						Type: AXSTREAM_EVENT_REMOVE,
						ID:   removedLookup.Node.BackendNodeID,
					}
				default:
					panic(fmt.Sprintf("unknown method: %v", e.method))
				}
			}
		}
	}()

	for msg := range page.Event() {
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
	return
}
