package structure

import (
	"ax-distiller/internal/chrome/axstream"
	"fmt"
	"log/slog"

	"github.com/go-rod/rod/lib/proto"
)

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
	case axstream.EVENT_REPLACE:
		clear(p.mapping)
		p.Root = Construct(e.Subtree)
		fmt.Println(p.Root)
		p.indexStructureBackendIDs(p.Root)
	case axstream.EVENT_INSERT:
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
				panic(fmt.Errorf("assert failed: parent corresponding to ParentID (%v) must exist", e.ParentID))
			}
			ns := parent.FirstChild
			parent.FirstChild = subtreeStruct
			subtreeStruct.NextSibling = ns
		} else {
			ps, ok := p.mapping[*prevSiblingID]
			if !ok {
				slog.Info("info", "id", p.mapping[*prevSiblingID], "parent", e.ParentID)
				panic(fmt.Errorf("assert failed: previous sibling corresponding to ID (%v) must exist", *prevSiblingID))
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
