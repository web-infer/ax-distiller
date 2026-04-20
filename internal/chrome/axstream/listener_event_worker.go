package axstream

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/syncx"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod/lib/proto"
)

var eventLoopNoStale = fmt.Errorf("page should never become stale within the single event loop goroutine")

func (l listener) handleAXLoadComplete(eventBuff []byte) {
	slog.Debug("Accessibility.loadComplete")

	var event struct {
		Root cdp.AXNode `json:"root"`
	}
	err := sonic.Unmarshal(eventBuff, &event)
	if err != nil {
		panic(err)
	}

	l.treeState.PageReset(event.Root)

	targets := []proto.AccessibilityAXNodeID{event.Root.NodeID}
	bundle := syncx.NewBundledRequests(l.treeState.PageContext(), targets)
	defer bundle.Cleanup()
	l.subs <- subReq{
		pageID: l.treeState.PageID(),
		id:     targets[0],
		bundle: bundle,
	}

	ok := <-bundle.Done()
	if !ok {
		// if bundle context has canceled (the page has reset)
		slog.Debug("Accessibility.loadComplete: bundle context canceled")
		return
	}

	// all requests and their children have finished
	root, ok := l.treeState.GetTree(l.treeState.PageID(), targets[0])
	if !ok {
		panic(eventLoopNoStale)
	}

	slog.Debug("Accessibility.loadComplete: root complete!")

	l.events <- Event{
		Type:  EVENT_RESET,
		Added: []*cdp.AXNodeWithRelatives{root},
	}
}

func (l listener) handleNodesUpdated(eventBuff []byte) {
	slog.Debug("Accessibility.nodesUpdated")

	var nodes cdp.AXNodesResult
	err := sonic.Unmarshal(eventBuff, &nodes)
	if err != nil {
		panic(err)
	}

	pageID := l.treeState.PageID()

	// fetch non-fetched nodes & update all nodes given
	var subTargets []proto.AccessibilityAXNodeID
	var added []proto.AccessibilityAXNodeID
	var updated []proto.AccessibilityAXNodeID
	for _, n := range nodes.Nodes {
		alreadyFetched, ok := l.treeState.UpdateNode(pageID, n)
		if !ok {
			panic(eventLoopNoStale)
		}
		if alreadyFetched {
			updated = append(updated, n.NodeID)
			continue
		}
		subTargets = append(subTargets, n.NodeID)
		added = append(added, n.NodeID)
	}

	if len(subTargets) > 0 {
		slog.Debug("Accessibility.nodesUpdated: bundle start for nodesUpdated")
		bundle := syncx.NewBundledRequests(l.treeState.PageContext(), subTargets)
		defer bundle.Cleanup()

		pageID := l.treeState.PageID()
		for _, id := range subTargets {
			l.subs <- subReq{
				id:     id,
				bundle: bundle,
				pageID: pageID,
			}
		}
		ok := <-bundle.Done()
		slog.Debug("Accessibility.nodesUpdated: bundle stop for nodesUpdated")
		if ok {
			// bundle context has canceled (the page has reset)
			slog.Debug("Accessibility.nodesUpdated: bundle context canceled!")
			return
		}
	}

	// all requests and their children have completed
	ev := Event{
		Type:    EVENT_PATCH,
		Added:   make([]*cdp.AXNodeWithRelatives, len(added)),
		Updated: make([]*cdp.AXNodeWithRelatives, len(updated)),
	}
	var ok bool
	for i, id := range added {
		ev.Added[i], ok = l.treeState.GetTree(pageID, id)
		if !ok {
			panic(eventLoopNoStale)
		}
	}
	for i, id := range updated {
		ev.Updated[i], ok = l.treeState.GetTree(pageID, id)
		if !ok {
			panic(eventLoopNoStale)
		}
	}
	slog.Debug("Accessibility.nodesUpdated: notify events")
	l.events <- ev
}

func (l listener) eventSourceWorker() {
	events := l.page.Event()
	for {
		select {
		case <-l.ctx.Done():
			return
		case msg := <-events:
			if msg == nil {
				continue
			}
			method := msg.Method
			buff := reflect.ValueOf(msg).Elem().FieldByName("data").Bytes()

			switch method {
			case "Accessibility.loadComplete":
				l.handleAXLoadComplete(buff)
			case "Accessibility.nodesUpdated":
				l.handleNodesUpdated(buff)
			}
		}
	}
}
