package cdp

import (
	"ax-distiller/internal/tree"
	"fmt"
)

// AXNodeWithRelatives is an AXNode with a FirstChild and NextSibling pointer
// that enable much faster traversal than a hashmap lookup (if we were to just
// use the ID references).
//
// We copy AXNode into this representation so we avoid GC overhead by
// referencing with pointers. This is not zero-copy but is much faster than
// firing GC.
type AXNodeWithRelatives struct {
	Underlying  AXNode
	FirstChild  *AXNodeWithRelatives
	NextSibling *AXNodeWithRelatives
}

func (r AXNodeWithRelatives) Debug() tree.DebugInfo {
	return tree.DebugInfo{
		Name:     fmt.Sprintf("%v [%v]", r.Underlying.Name.Value, r.Underlying.Ignored),
		Metadata: r.Underlying.NodeID,
	}
}

func (r AXNodeWithRelatives) Relatives() tree.Relatives {
	rel := tree.Relatives{}
	if r.FirstChild != nil {
		rel.FirstChild = r.FirstChild
	}
	if r.NextSibling != nil {
		rel.NextSibling = r.NextSibling
	}
	return rel
}

func (r AXNodeWithRelatives) String() string {
	return tree.Print(r)
}
