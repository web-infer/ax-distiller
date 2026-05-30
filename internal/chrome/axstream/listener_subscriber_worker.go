package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"sync"
	"sync/atomic"

	"github.com/go-rod/rod/lib/proto"
)

func (l listener) subscribe(pageID uint32, id proto.AccessibilityAXNodeID) (childTargets []proto.AccessibilityAXNodeID) {
	res := l.cdpResPool.Get().(*cdp.AXNodesResult)
	err := cdp.CommandOutputPtr(
		l.treeState.PageContext(),
		l.page,
		cdp.GetChildAXNodes{ID: id},
		res,
	)
	if err != nil {
		// we ignore errors, they do not affect the result
		return
	}

	for _, child := range res.Nodes {
		fetchedBefore, ok := l.treeState.UpdateNode(pageID, child)
		if !ok {
			// abort if stale request
			return
		}
		if !fetchedBefore {
			childTargets = append(childTargets, child.NodeID)
		}
	}
	return
}

func (l listener) subDispatcher(
	ctx context.Context,
	wg *sync.WaitGroup,
	count *uint64,
	queue chan proto.AccessibilityAXNodeID,
	pageID uint32,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case id, ok := <-queue:
			if !ok {
				return
			}
			childTargets := l.subscribe(pageID, id)
			atomic.AddUint64(count, 1)
			wg.Add(len(childTargets) - 1)
			go func(targets []proto.AccessibilityAXNodeID) {
				// this must be done in a goroutine because otherwise we
				// could have a deadlock:
				//
				// 1. total workers: A and B, both awaiting work
				// 2. A receives work from channel, has 2 children (job jA.1, jA.2)
				// 3. B receives job jA.1
				// 4. A is waiting on channel <- jA.2
				// 5. B finishes job jA.1, has 2 children (jB.1, jB.2)
				// 6. B is waiting on channel <- jB.1
				// 7. A and B are both waiting on channel send, no
				//    goroutines are available to receive those channel
				//    sends
				// 8. we have a deadlock
				//
				// we cannot use select { case ...: default: } because if
				// it takes the default branch, it effectively drops the
				// job, which cannot happen
				//
				// technically we haven't solved the deadlock here, just
				// moved it so that it happens if we run out of memory from
				// creating goroutines, but such a case is quite unlikely
				//
				// the subscription ordering here also doesn't matter
				// (since the only ordering that matters is the actual
				// events the browser sends to us, not subscriptions which
				// is only dependent on the node's id) so it is okay to
				// create goroutines like this
				for _, id := range targets {
					queue <- id
				}
			}(childTargets)
		}
	}
}

func (l listener) subSubtree(root cdp.AXNode) {
	count := uint64(0)
	wg := sync.WaitGroup{}

	pageID := l.treeState.PageID()
	l.treeState.UpdateNode(pageID, root)

	queue := make(chan proto.AccessibilityAXNodeID, sub_channel_buffer_size)
	defer close(queue)

	for range sub_worker_count {
		go l.subDispatcher(l.treeState.PageContext(), &wg, &count, queue, pageID)
	}

	wg.Add(1)
	queue <- root.NodeID
	wg.Wait()

	l.logger.Info("subscribed nodes", "count", count, "root", root.NodeID)
}
