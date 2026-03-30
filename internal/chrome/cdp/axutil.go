package cdp

import (
	"context"
	"log/slog"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type AXNodeWithRelatives struct {
	*AXNode
	FirstChild  *AXNodeWithRelatives
	NextSibling *AXNodeWithRelatives
}

type GetAXTreeOptions struct {
	WithName string
	WithRole string
}

func GetSubtree(
	ctx context.Context,
	page *rod.Page,
	rootId proto.DOMBackendNodeID,
	opts GetAXTreeOptions,
) (root *AXNodeWithRelatives, err error) {
	fetchRelatives := false
	nodeInfo, err := Command(ctx, page, GetPartialAXTree{
		BackendNodeID:  &rootId,
		FetchRelatives: &fetchRelatives,
	})
	if err != nil {
		return
	}
	if len(nodeInfo.Nodes) == 0 {
		panic("assert failed: expected exactly one node to be returned from GetPartialAXTree")
	}
	rootInfo := nodeInfo.Nodes[0]
	subtree, err := Command(ctx, page, QueryAXTree{
		BackendNodeID:  &rootId,
		AccessibleName: opts.WithName,
		Role:           opts.WithRole,
	})
	if err != nil {
		return
	}
	if len(subtree.Nodes) == 0 {
		var children AXNodesResult
		children, err = Command(ctx, page, GetChildAXNodes{
			ID: rootInfo.NodeID,
		})
		if err != nil {
			return
		}
		slog.Info("found children", "result", children)
	}
	nodes := make(map[proto.AccessibilityAXNodeID]*AXNode)
	nodes[rootInfo.NodeID] = rootInfo
	for _, n := range subtree.Nodes {
		nodes[n.NodeID] = n
	}
	root = getSubtreeInner(nodes, []proto.AccessibilityAXNodeID{rootInfo.NodeID})
	return
}

func getSubtreeInner(
	m map[proto.AccessibilityAXNodeID]*AXNode,
	siblings []proto.AccessibilityAXNodeID,
) *AXNodeWithRelatives {
	if len(siblings) == 0 {
		return nil
	}
	node, ok := m[siblings[0]]
	if !ok {
		panic("assert failed: expect node to exist in accessibility map")
	}
	fc := getSubtreeInner(m, node.ChildIDs)
	ns := getSubtreeInner(m, siblings[1:])
	return &AXNodeWithRelatives{
		AXNode:      node,
		FirstChild:  fc,
		NextSibling: ns,
	}
}
