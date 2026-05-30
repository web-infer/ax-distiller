package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"fmt"
	"reflect"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod/lib/proto"
)

var eventLoopNoStale = fmt.Errorf("page should never become stale within the single event loop goroutine")

func (l listener) handleAXLoadComplete(eventBuff []byte) {
	l.logger.Debug("start event handle", "event", "Accessibility.loadComplete")

	var event struct {
		Root cdp.AXNode `json:"root"`
	}
	err := sonic.Unmarshal(eventBuff, &event)
	if err != nil {
		panic(err)
	}

	l.treeState.PageReset(event.Root)

	l.subSubtree(event.Root)

	// all requests and their children have finished
	root, ok := l.treeState.GetTree(l.treeState.PageID(), event.Root.NodeID)
	if !ok {
		panic(eventLoopNoStale)
	}

	l.logger.Debug("finish event handle", "event", "Accessibility.loadComplete")

	l.events <- Event{
		Type:    EVENT_RESET,
		Updated: []*cdp.AXNodeWithRelatives{root},
	}
}

// reconciles updated node with current state
//
// 1. subscribes to subtree if node is of unknown id
// 2. updates metadata and children list and reconciles children
func (l listener) reconcile(
	wg *sync.WaitGroup,
	pageID uint32,
	newNode cdp.AXNode,
	updated *[]proto.AccessibilityAXNodeID,
) {
	// nodes that have been added are, by definition, nodes with ids that
	// we have not seen yet
	//
	// it is possible for an updated node's children to consist of no added
	// nodes, simply removed or moved nodes
	//
	// it is also possible for a node to have unchanged children, simply
	// changed metadata!

	var hasChildUpdates bool
	for _, childID := range newNode.ChildIDs {
		_, alreadyExist := l.treeState.GetNode(childID)
		if !alreadyExist {
			wg.Go(func() { l.subSubtree(newNode) })
			hasChildUpdates = true
		}
	}

	existing, alreadyExist := l.treeState.GetNode(newNode.NodeID)
	hasMetaUpdates := !alreadyExist || !newNode.MetaEqual(existing)

	if !hasMetaUpdates && !hasChildUpdates {
		return
	}

	overriden, success := l.treeState.UpdateNode(pageID, newNode)
	if !success {
		// must abort, page has been invalidated
		return
	}
	if !overriden {
		panic("assert failed: should never have created new node while updating")
	}
	*updated = append(*updated, newNode.NodeID)
}

func (l listener) handleNodesUpdated(eventBuff []byte) {
	// the list of `Nodes` does not necessarily enumerate every node that has
	// been added or updated.
	//
	// however, each node given is guaranteed to have changed *one of* its
	// properties, either its children or attribute list. this means unless
	// a node (through JS) causes updateEvent other than its direct parent to
	// change, nothing other than its direct parent should be included.
	//
	// by empirical observation, it turns out that both parent and child can be
	// updated simultaneously in a single update event. consider the structure:
	//
	// - A (div)
	//   - B (div)
	//
	// a <p> can be added to both A and B in the same closure and both A and B
	// will be included in the resulting nodesUpdated event
	//
	// - A (div)
	//   - B (div)
	//     - <p>
	//   - <p>
	//
	// nodesUpdated.nodes = [A, B]
	//
	// what this indicates is that nodesUpdated likely tracks the *mutated
	// existing DOM elements* within a closure.
	//
	// hence, we will assume that:
	//
	// - nodes updated list = every existing AX node object that has been
	// mutated (children, attributes or both)
	// - the roots of new subtrees will be present in the children of some of
	// the nodes in the updated list
	var updateEvent cdp.AXNodesResult
	err := sonic.Unmarshal(eventBuff, &updateEvent)
	if err != nil {
		panic(err)
	}

	l.logger.Debug("start event handle", "event", "Accessibility.nodesUpdated")

	wg := sync.WaitGroup{}
	pageID := l.treeState.PageID()
	var updatedIDs []proto.AccessibilityAXNodeID
	for _, newNode := range updateEvent.Nodes {
		l.reconcile(&wg, pageID, newNode, &updatedIDs)
	}
	wg.Wait()

	// all requests and their children have completed
	ev := Event{
		Type:    EVENT_PATCH,
		Updated: make([]*cdp.AXNodeWithRelatives, len(updatedIDs)),
	}
	for i, id := range updatedIDs {
		var ok bool
		ev.Updated[i], ok = l.treeState.GetTree(pageID, id)
		if !ok {
			panic(eventLoopNoStale)
		}
	}
	l.logger.Debug("finish event handler", "event", "Accessibility.nodesUpdated")
	l.events <- ev
}

func (l listener) eventSourceWorker() {
	events := l.page.Event()
	for {
		select {
		case <-l.ctx.Done():
			return
		case msg, ok := <-events:
			if !ok {
				return
			}
			if msg == nil {
				continue
			}

			method := msg.Method

			buff := reflect.ValueOf(msg).Elem().FieldByName("data").Bytes()

			l.logger.Debug("event", "method", method)

			switch method {
			case "Accessibility.loadComplete":
				l.handleAXLoadComplete(buff)
			case "Accessibility.nodesUpdated":
				l.handleNodesUpdated(buff)
			}
		}
	}
}
