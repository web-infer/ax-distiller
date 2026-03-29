package cdp

import (
	"ax-distiller/internal/syncx"
	"context"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type axSubtreeWork struct {
	fetcher *AXSubtreeFetcher
	node    *AXNodeWithRelatives
}

func (j axSubtreeWork) Exec() {
	// if NodeID is a negative number, we know it is a leaf
	if j.node.NodeID[0] == '-' {
		return
	}
	// this always causes "invalid ID for some reason"
	if j.node.NodeID == "0" {
		return
	}

	res, err := Command(
		j.fetcher.ctx,
		j.fetcher.page,
		GetChildAXNodes{ID: j.node.NodeID},
	)
	if err == context.Canceled {
		return
	}
	if err != nil {
		return
	}

	var prev *AXNodeWithRelatives
	for i, child := range res.Nodes {
		// the CDP get child nodes command returns BOTH the "ignored" nodes that
		// are the actual direct descendents of the current AX node and the AX
		// nodes that would be the direct descendents with the ignored nodes
		// removed. therefore we should ignore "none" nodes which are nodes that
		// are removed.
		if child.Role.Value == "none" {
			continue
		}

		childOutput := &AXNodeWithRelatives{
			AXNode: &res.Nodes[i],
		}

		if prev != nil {
			prev.NextSibling = childOutput
		} else {
			j.node.FirstChild = childOutput
		}

		j.fetcher.pool.Add(axSubtreeWork{
			fetcher: j.fetcher,
			node:    childOutput,
		})
		prev = childOutput
	}
}

type AXNodeWithRelatives struct {
	*AXNode
	FirstChild  *AXNodeWithRelatives
	NextSibling *AXNodeWithRelatives
}

type AXSubtreeFetcher struct {
	ctx  context.Context
	page *rod.Page
	root *AXNodeWithRelatives
	pool *syncx.WorkerPool[axSubtreeWork]

	err    error
	once   sync.Once
	cancel func()
}

func NewAXSubtreeFetcher(page *rod.Page, workers int) *AXSubtreeFetcher {
	return &AXSubtreeFetcher{
		page: page,
		pool: syncx.NewWorkerPool[axSubtreeWork](workers),
	}
}

func (f *AXSubtreeFetcher) exit(err error) {
	f.once.Do(func() {
		f.err = err
		f.cancel()
	})
}

func (f *AXSubtreeFetcher) Start(ctx context.Context) {
	f.pool.Start(ctx)
}

func (f *AXSubtreeFetcher) Fetch(ctx context.Context, nodeID proto.AccessibilityAXNodeID) (out *AXNodeWithRelatives, err error) {
	f.ctx, f.cancel = context.WithCancel(ctx)
	f.root = &AXNodeWithRelatives{
		// technically only the NodeID is required
		AXNode: &AXNode{NodeID: nodeID},
	}
	f.pool.Add(axSubtreeWork{
		fetcher: f,
		node:    f.root,
	})
	select {
	case <-f.ctx.Done():
	case <-f.pool.Wait():
	}
	if f.err != nil {
		err = f.err
		return
	}
	out = f.root
	return
}
