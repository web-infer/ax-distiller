package structure

import (
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"encoding/binary"
	"log/slog"
	"slices"

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
	Root   *Structure
	Index  map[uint64][]*Structure
	logger *slog.Logger
	// we do not actually use state to cache any operations because at any
	// point an AX node ID can point to a different possible structure, we
	// simply keep it for reference of the current mapping of AX node to
	// structure
	state      map[proto.AccessibilityAXNodeID]*Structure
	recomputed map[proto.AccessibilityAXNodeID]*Structure
}

func NewPersistent(logger *slog.Logger) *Persistent {
	return &Persistent{
		Root:       nil,
		Index:      make(map[uint64][]*Structure),
		state:      make(map[proto.AccessibilityAXNodeID]*Structure),
		recomputed: make(map[proto.AccessibilityAXNodeID]*Structure),
		logger:     logger.WithGroup("persistent"),
	}
}

func (p *Persistent) LookupStructure(id proto.AccessibilityAXNodeID) *Structure {
	return p.state[id]
}

func isNotIgnored(node *cdp.AXNodeWithRelatives) bool {
	ignored := node.Underlying.Ignored ||
		// (node.FirstChild == nil && node.Underlying.Role.Value == "generic") ||
		(node.FirstChild == nil && node.Underlying.Role.Value == "InlineTextBox")
	return !ignored
}

func (p *Persistent) recomputeNodeStructure(node *cdp.AXNodeWithRelatives, state map[proto.AccessibilityAXNodeID]*Structure) (out *Structure) {
	out = &Structure{Underlying: node}
	hashBuff := []byte(node.Underlying.Role.Value)

	var prev *Structure
	for child := range cdp.FilterDescendentsShallow(isNotIgnored, node) {

		// single child may return multiple children in linked list (via NextSibling)
		firstStruct := p.recomputeNodeStructure(child, state)

		// may return NextSibling != nil, but only if hitting cache
		// should never hit cache in root

		if prev == nil {
			// set first child to the first childStruct
			out.FirstChild = firstStruct
		} else {
			// set final node of last child's NextSibling to first node of this child
			prev.NextSibling = firstStruct
		}

		for str := firstStruct; str != nil; str = str.NextSibling {
			// add all children hashes to structure
			hashBuff = binary.LittleEndian.AppendUint64(hashBuff, str.Hash)
			if str.NextSibling == nil {
				// prev points to the last node of the child list returned
				prev = str
			}
		}
	}

	out.Hash = xxh3.Hash(hashBuff)
	p.Index[out.Hash] = append(p.Index[out.Hash], out)

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

	if out.NextSibling != nil {
		panic("assert failed: out.NextSibling != nil")
	}

	state[node.Underlying.NodeID] = out
	return
}

func (p *Persistent) reconcileRecomputed() {
	for id, next := range p.recomputed {
		prev, ok := p.state[id]

		// if update
		if ok {
			// delete all previous children from state map which are not in
			// recomputed node's children
		cleanup:
			for prevChild := prev.FirstChild; prevChild != nil; prevChild = prevChild.NextSibling {
				for nextChild := next.FirstChild; nextChild != nil; nextChild = nextChild.NextSibling {
					if nextChild.Underlying.Underlying.BackendDOMNodeID == prevChild.Underlying.Underlying.BackendDOMNodeID {
						continue cleanup
					}
				}

				instanceList := p.Index[prevChild.Hash]
				idx := slices.Index(instanceList, prevChild)
				if idx >= 0 {
					p.Index[prevChild.Hash] = slices.Delete(instanceList, idx, idx+1)
				}
				delete(p.state, prevChild.Underlying.Underlying.NodeID)
			}
		}

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
			p.recomputeNodeStructure(added, p.recomputed)
		}
		for _, updated := range e.Updated {
			p.recomputeNodeStructure(updated, p.recomputed)
		}
		p.reconcileRecomputed()
		p.logger.Debug("finish patch event")
	}
}
