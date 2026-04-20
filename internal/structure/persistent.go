package structure

import (
	"ax-distiller/internal/chrome/axstream"
	"fmt"
	"log/slog"

	"github.com/go-rod/rod/lib/proto"
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

*/

type Persistent struct {
	Root    *Structure
	mapping map[proto.DOMBackendNodeID]*Structure
}

func NewPersistent() Persistent {
	return Persistent{
		Root:    nil,
		mapping: make(map[proto.DOMBackendNodeID]*Structure),
	}
}

func (p Persistent) indexStructureBackendIDs(s *Structure) {
	if s == nil {
		return
	}
	p.mapping[s.Underlying.BackendDOMNodeID] = s
	p.indexStructureBackendIDs(s.FirstChild)
	p.indexStructureBackendIDs(s.NextSibling)
}

func (p Persistent) deindexStructureBackendIDs(s *Structure) {
	if s == nil {
		return
	}
	delete(p.mapping, s.Underlying.BackendDOMNodeID)
	p.deindexStructureBackendIDs(s.FirstChild)
	p.deindexStructureBackendIDs(s.NextSibling)
}

func (p Persistent) HandleEvent(e axstream.Event) {
	switch e.Type {
	case axstream.EVENT_RESET:
		clear(p.mapping)
		p.Root = Construct(e.Added[0])
		p.indexStructureBackendIDs(p.Root)
	case axstream.EVENT_PATCH:
		// problem: structure is based on non-ignored nodes, however ignored
		// nodes may be the ones getting updated

		// furthermore, the api simply doesn't return ignored nodes, so we need
		// a method to "find" non-ignored nodes from ignored nodes

		// for insert we can lookup the shallowest non-ignored ancestor and use
		// that as the parent, then we splat it, the closest node

		// maybe we can do an on-demand lookup of an inserted event's subtree
		// and find the shallowest non-ignored nodes (if it is not found
		// conventionally) for removal

		subtreeStruct := Construct(e.Subtree)
		prevSiblingID := e.ID
		if prevSiblingID == nil {
			parent, ok := p.mapping[e.ParentID]
			if !ok {
				slog.Info("info", "parent", e.ParentID)
				fmt.Println(fmt.Errorf("assert failed: parent corresponding to ParentID (%v) must exist (persistent)", e.ParentID))
				return
			}
			ns := parent.FirstChild
			parent.FirstChild = subtreeStruct
			subtreeStruct.NextSibling = ns
		} else {
			ps, ok := p.mapping[*prevSiblingID]
			if !ok {
				slog.Info("info", "id", p.mapping[*prevSiblingID], "parent", e.ParentID)
				fmt.Println(fmt.Errorf("assert failed: previous sibling corresponding to ID (%v) must exist (persistent)", *prevSiblingID))
				return
			}
			ns := ps.NextSibling
			ps.NextSibling = subtreeStruct
			subtreeStruct.NextSibling = ns
		}
		p.indexStructureBackendIDs(subtreeStruct)
	case axstream.EVENT_REMOVE:
		s := p.mapping[*e.ID]
		p.deindexStructureBackendIDs(s)
	}
}
