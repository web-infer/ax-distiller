package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/syncx"

	"github.com/go-rod/rod/lib/proto"
)

type subReq struct {
	pageID uint32
	id     proto.AccessibilityAXNodeID
	bundle *syncx.BundledRequests[proto.AccessibilityAXNodeID]
}

func (l listener) handleSubscribe(req subReq) {
	if req.pageID != l.treeState.PageID() {
		// abort if stale request
		return
	}

	defer req.bundle.Resolve(req.id)

	res := l.cdpResPool.Get().(*cdp.AXNodesResult)
	err := cdp.CommandOutputPtr(
		l.treeState.PageContext(),
		l.page,
		cdp.GetChildAXNodes{ID: req.id},
		res,
	)
	if err != nil {
		// we ignore errors, they do not affect the result
		return
	}

	var childTargets []proto.AccessibilityAXNodeID
	for _, child := range res.Nodes {
		alreadyFetched, ok := l.treeState.UpdateNode(req.pageID, child)
		if !ok {
			// abort if stale request
			return
		}
		if !alreadyFetched {
			childTargets = append(childTargets, child.NodeID)
		}
	}

	if len(childTargets) == 0 {
		return
	}
	// we are guaranteed len(childTargets) > 0

	childBundle := syncx.NewBundledRequests(l.treeState.PageContext(), l.logger, childTargets)
	go func() {
		defer childBundle.Cleanup()
		<-childBundle.Done()
	}()

	req.bundle.AddChild(childBundle)
	for _, id := range childTargets {
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
		go func() {
			l.subs <- subReq{
				pageID: req.pageID,
				id:     id,
				bundle: childBundle,
			}
		}()
	}

	l.cdpResPool.Put(res)
}

func (l listener) subscriberWorker() {
	for {
		select {
		case <-l.ctx.Done():
			return
		case req := <-l.subs:
			l.handleSubscribe(req)
		}
	}
}
