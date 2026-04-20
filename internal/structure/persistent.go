package structure

import (
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"encoding/binary"
	"iter"
	"log/slog"

	"github.com/go-rod/rod/lib/proto"
	"github.com/zeebo/xxh3"
)

/*

key problem: structure computation based on non-ignored nodes
sit.: updates come from potentially ignored nodes


naive solution: we store the entire AX tree (ignored and all), filter it, then
compute structure on the filtered result for every update


observations:
- each update is either:
	1) entire tree changed -> naive solution fastest
	2) some nodes added + some nodes updated (children may be added/deleted)
- in case 2:
	- we can assume that the path of nodes -> root which update are present in
	added/updated list
		- this implies that some nodes in the updated list are in each other's
		subtree
	- each updated node potentially contains multiple non-ignored structure
	node in the subtree


given a node AX ID:
1. check if already recomputed, if so return updated structure
2. get non-ignored direct descendents as a flat list
3. run fn recursively on all non-ignored direct descendents -> list[structure]
4. compute structure
5. collapse adjacent and slideWindow alternatively
6. save structure under AX ID
7. return structure

somewhere in here must compute dropped nodes and drop them
*/

type structureEntry struct {
	Value      *Structure
	References int
}

type Persistent struct {
	Root       *Structure
	state      map[proto.AccessibilityAXNodeID]*Structure
	recomputed map[proto.AccessibilityAXNodeID]*Structure
	logger     *slog.Logger
}

func NewPersistent(logger *slog.Logger) *Persistent {
	return &Persistent{
		Root:       nil,
		state:      make(map[proto.AccessibilityAXNodeID]*Structure),
		recomputed: make(map[proto.AccessibilityAXNodeID]*Structure),
		logger:     logger.WithGroup("persistent"),
	}
}

func (p *Persistent) shallowIterNonIgnoredDescendentsInner(node *cdp.AXNodeWithRelatives, yield func(*cdp.AXNodeWithRelatives) bool) {
	if node == nil {
		return
	}
	defer p.shallowIterNonIgnoredDescendentsInner(node.NextSibling, yield)
	if !node.Underlying.Ignored {
		// we always immediately return when finding non-ignored node,
		// therefore there is no case where a node with a non-ignored ancestor
		// is yielded
		yield(node)
		return
	}
	p.shallowIterNonIgnoredDescendentsInner(node.FirstChild, yield)
}

func (p *Persistent) shallowIterNonIgnoredDescendents(node *cdp.AXNodeWithRelatives) iter.Seq[*cdp.AXNodeWithRelatives] {
	return func(yield func(*cdp.AXNodeWithRelatives) bool) {
		// we do not yield() the node itself
		p.shallowIterNonIgnoredDescendentsInner(node.FirstChild, yield)
	}
}

func (p *Persistent) recomputeNodeStructure(node *cdp.AXNodeWithRelatives, state map[proto.AccessibilityAXNodeID]*Structure) (out *Structure) {
	existing, ok := state[node.Underlying.NodeID]
	if ok {
		out = existing
		return
	}

	out = &Structure{
		Underlying: &node.Underlying,
	}
	hashBuf := []byte(node.Underlying.Role.Value)

	var prev *Structure
	for child := range p.shallowIterNonIgnoredDescendents(node) {
		// single child may return multiple children in linked list (via NextSibling)
		childStructs := p.recomputeNodeStructure(child, state)

		if prev == nil {
			// set first child to the first childStruct
			out.FirstChild = childStructs
		} else {
			// set final node of last child's NextSibling to first node of this child
			prev.NextSibling = childStructs
		}

		for c := childStructs; c != nil; c = c.NextSibling {
			// add all children hashes to structure
			binary.LittleEndian.AppendUint64(hashBuf, c.Hash)
			// prev points to the last node of the child list returned
			prev = c
		}
	}

	out.Hash = xxh3.Hash(hashBuf)

	// we create synthetic structural wrappers for repeated nodes and patterns
	// in the children linked list
	for {
		// group repeated adjacent nodes into a wrapper
		out.FirstChild = deleteAdjacent(out.FirstChild)

		// identify most frequent (and among the most frequent the largest)
		// pattern and replace all instances of it with a wrapper
		var replaced bool
		out.FirstChild, replaced = slideWindow(out.FirstChild)

		// rinse and repeat until no patterns are found
		if !replaced {
			break
		}
	}

	state[node.Underlying.NodeID] = out
	return
}

func (p *Persistent) reconcileRecomputed() {
	for id, next := range p.recomputed {
		prev, ok := p.state[id]

		// if update
		if ok {
			// delete all previous children from map which are not in recomputed node's children
		cleanup:
			for prevChild := prev.FirstChild; prevChild != nil; prevChild = prevChild.NextSibling {
				for nextChild := next.FirstChild; nextChild != nil; nextChild = nextChild.NextSibling {
					if nextChild.Underlying.BackendDOMNodeID == prevChild.Underlying.BackendDOMNodeID {
						continue cleanup
					}
				}
				p.logger.Debug("delete dropped", "node", prevChild.Underlying.BackendDOMNodeID)
				delete(p.state, prevChild.Underlying.NodeID)
			}
		}

		p.logger.Debug("update node", "node", next.Underlying.BackendDOMNodeID)
		p.state[id] = next
	}
	clear(p.recomputed)
}

func (p *Persistent) HandleEvent(e axstream.Event) {
	switch e.Type {
	case axstream.EVENT_RESET:
		p.logger.Debug("start reset event")
		clear(p.state)
		p.Root = p.recomputeNodeStructure(e.Added[0], p.state)
		p.logger.Debug("finish reset event")
	case axstream.EVENT_PATCH:
		p.logger.Debug("start patch event")
		for _, added := range e.Added {
			p.recomputeNodeStructure(added, p.state)
		}
		for _, updated := range e.Updated {
			p.recomputeNodeStructure(updated, p.state)
		}
		p.reconcileRecomputed()
		p.logger.Debug("finish patch event")
	}
}
