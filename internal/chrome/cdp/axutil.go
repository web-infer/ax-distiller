package cdp

import (
	"ax-distiller/internal/tree"
	"context"
	"fmt"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
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

func ellipsis(t string, length int) string {
	if len(t) > length {
		return t[:length] + "..."
	}
	return t
}

func (r AXNodeWithRelatives) Debug() tree.DebugInfo {
	return tree.DebugInfo{
		Name: fmt.Sprintf(
			"%v \"%v\" %v",
			r.Underlying.Role.Value,
			ellipsis(r.Underlying.Name.Value, 30),
			r.Underlying.Ignored,
		),
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

func ElementFromAX(ctx context.Context, page *rod.Page, backendNodeID proto.DOMBackendNodeID) (out *rod.Element, err error) {
	// taken from Page.ElementFromNode

	req := DOMResolveNode{
		BackendNodeID: backendNodeID,
	}
	res, err := Command(ctx, page, req)
	if err != nil {
		return
	}

	el, err := page.ElementFromObject(res.Object)
	if err != nil {
		return
	}

	desc, err := el.Describe(0, false)
	if err != nil {
		return
	}
	if desc.NodeName == "#text" {
		el, err = el.Parent()
		if err != nil {
			return
		}
	}
	return
}
