package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/go-rod/rod/lib/proto"
)

type listenerPageState struct {
	listener listener

	mutex sync.Mutex

	pageID        uint32
	pageCtx       context.Context
	pageCtxCancel func()
	nodes         map[proto.AccessibilityAXNodeID]cdp.AXNode
}

func newListenerPageState(l listener) *listenerPageState {
	pageCtx, pageCancel := context.WithCancel(l.ctx)
	return &listenerPageState{
		listener:      l,
		pageCtx:       pageCtx,
		pageCtxCancel: pageCancel,
		nodes:         make(map[proto.AccessibilityAXNodeID]cdp.AXNode),
	}
}

func (s *listenerPageState) PageID() uint32 {
	return atomic.LoadUint32(&s.pageID)
}

func (s *listenerPageState) PageContext() context.Context {
	return s.pageCtx
}

func (s *listenerPageState) PageReset(root cdp.AXNode) {
	defer s.mutex.Unlock()
	s.mutex.Lock()

	s.pageCtxCancel()
	s.pageID++
	s.pageCtx, s.pageCtxCancel = context.WithCancel(s.listener.ctx)
	clear(s.nodes)

	// we clone ChildIDs because:
	// 1. after call UpdateNode on []nodes in subscriber worker or event worker
	// 2. CDP res object of []nodes will be returned to object pool
	// 3. each nodes in []nodes contains the ChildID as a slice
	// 4. slice is a ptr
	// 5. after returning []nodes to object pool, slices are also returned to
	// object pool to be reused in diff nodes
	// 6. using n.ChildIds after this point without copying will result in UB
	root.ChildIDs = slices.Clone(root.ChildIDs)
	s.nodes[root.NodeID] = root
}

func (s *listenerPageState) UpdateNode(pageID uint32, n cdp.AXNode) (existing, ok bool) {
	defer s.mutex.Unlock()
	s.mutex.Lock()

	if pageID != s.pageID {
		return
	}
	_, existing = s.nodes[n.NodeID]

	// same cloning logic here as the one in PageReset()
	n.ChildIDs = slices.Clone(n.ChildIDs)
	s.nodes[n.NodeID] = n

	ok = true
	return
}

func (s *listenerPageState) GetTree(pageID uint32, id proto.AccessibilityAXNodeID) (tree *cdp.AXNodeWithRelatives, ok bool) {
	// we lock nodes while running getTree because:
	// 1. getTree is only ever called from one goroutine
	// 2. nodes, which getTree reads is also written concurrently from subscribers
	// 3. therefore getTree must lock nodes
	defer s.mutex.Unlock()
	s.mutex.Lock()

	if pageID != s.pageID {
		return
	}
	// pageID is ensured to match
	tree = s.getTreeInner(id)
	ok = true
	return
}

// we assume no cycles exist in the tree, cycle detection is too expensive
func (s *listenerPageState) getTreeInner(id proto.AccessibilityAXNodeID) *cdp.AXNodeWithRelatives {
	// here, a child not existing is the same semantically as a child that is
	// not yet loaded, thus we treat them same
	node, exists := s.nodes[id]
	if !exists {
		return nil
	}
	out := &cdp.AXNodeWithRelatives{Underlying: node}
	var prev *cdp.AXNodeWithRelatives
	for _, childID := range node.ChildIDs {
		child := s.getTreeInner(childID)
		if child == nil {
			break
		}
		if prev == nil {
			out.FirstChild = child
			prev = child
			continue
		}
		prev.NextSibling = child
		prev = child
	}
	return out
}
