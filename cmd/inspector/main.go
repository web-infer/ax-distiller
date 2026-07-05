package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/db"
	"ax-distiller/internal/slogx"
	"ax-distiller/internal/stealth"
	"ax-distiller/internal/structure"
	"ax-distiller/internal/structure/stdb"
	"context"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/LQR471814/rod"
	"github.com/LQR471814/rod/lib/proto"
)

func setAttrWorker(ctx context.Context, page *rod.Page, logger *slog.Logger, reqs <-chan *cdp.AXNodeWithRelatives) {
	for {
		select {
		case <-ctx.Done():
			return
		case node, ok := <-reqs:
			if !ok {
				return
			}
			pushReq := proto.DOMPushNodesByBackendIDsToFrontend{
				BackendNodeIDs: []proto.DOMBackendNodeID{
					node.Underlying.BackendDOMNodeID,
				},
			}
			pushRes, err := cdp.Command(ctx, page, pushReq)
			if err != nil {
				logger.Warn("push failure", "err", err, "role", node.Underlying.Role.Value)
				continue
			}

			req := cdp.DOMSetAttributeValue{
				NodeID: pushRes.NodeIDs[0],
				Name:   "ax-id",
				Value:  string(node.Underlying.NodeID),
			}
			err = cdp.CommandUnary(ctx, page, req)
			if err != nil {
				if strings.Contains(err.Error(), "shadow trees") {
					/*
						req := proto.DOMGetOuterHTML{
							BackendNodeID: node.Underlying.BackendDOMNodeID,
						}
						var res proto.DOMGetOuterHTMLResult
						res, err = cdp.Command(ctx, page, req)
						if err != nil {
							return
						}

						logger.Warn(fmt.Sprintf(
							"shadow tree (%v %v): %v",
							node.Underlying.Role.Value,
							node.Underlying.BackendDOMNodeID,
							&cdp.AXNodeWithRelatives{
								Underlying: node.Underlying,
								FirstChild: node.FirstChild,
							},
						), "html", res.OuterHTML)
					*/
					continue
				}
				if strings.Contains(err.Error(), "edit pseudo elements") {
					/*
						req := proto.DOMGetOuterHTML{
							BackendNodeID: node.Underlying.BackendDOMNodeID,
						}
						var res proto.DOMGetOuterHTMLResult
						res, err = cdp.Command(ctx, page, req)
						if err != nil {
							return
						}

						logger.Warn(fmt.Sprintf(
							"pseudo el (%v %v): %v",
							node.Underlying.Role.Value,
							node.Underlying.BackendDOMNodeID,
							&cdp.AXNodeWithRelatives{
								Underlying: node.Underlying,
								FirstChild: node.FirstChild,
							},
						), "html", res.OuterHTML)
					*/
					continue
				}
				logger.Warn(fmt.Sprintf("set attr failure: %v", node), "err", err)
				continue
			}
		}
	}
}

func setAttr(reqs chan<- *cdp.AXNodeWithRelatives, node *cdp.AXNodeWithRelatives) {
	if node == nil {
		return
	}
	if node.Underlying.BackendDOMNodeID < 0 {
		return
	}
	switch node.Underlying.Role.Value {
	case "InlineTextBox", "StaticText":
		return
	case "RootWebArea":
	default:
		if !node.Underlying.Ignored {
			reqs <- node
		}
	}

	setAttr(reqs, node.FirstChild)
	setAttr(reqs, node.NextSibling)
}

func visit(node *cdp.AXNodeWithRelatives, visitor func(*cdp.AXNodeWithRelatives)) {
	if node == nil {
		return
	}
	visitor(node)
	visit(node.FirstChild, visitor)
	visit(node.NextSibling, visitor)
}

func main() {
	logger := slogx.DemoLogger(slog.LevelInfo, func(group string, attrs iter.Seq[slog.Attr]) bool {
		switch group {
		case "main", "persistent":
			return true
		}
		return false
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	browser, err := chrome.NewBrowser("chromium")
	if err != nil {
		panic(err)
	}
	defer browser.Close()

	page := browser.MustPage("about:blank")
	chrome.DisableUnusedCDP(page)

	_, err = page.EvalOnNewDocument(stealth.JSExpr)
	if err != nil {
		panic(err)
	}

	events, err := axstream.Listen(ctx, logger, page)
	if err != nil {
		panic(err)
	}

	setAttrReqs := make(chan *cdp.AXNodeWithRelatives, 4)
	for range 4 {
		go setAttrWorker(ctx, page, logger, setAttrReqs)
	}

	dbDriver, err := db.OpenDB(ctx, logger, "state.db")
	if err != nil {
		panic(err)
	}
	defer db.CloseDB(dbDriver)

	stdbDriver, err := stdb.OpenDB(ctx, logger)
	if err != nil {
		panic(err)
	}
	defer stdbDriver.Close()

	persistLock := sync.Mutex{}
	persistent := structure.NewPersistent(logger, dbDriver, stdbDriver)
	persistHandleEvent := func(e axstream.Event) {
		defer persistLock.Unlock()
		persistLock.Lock()

		err = persistent.HandleEvent(ctx, e)
		if err != nil {
			err = fmt.Errorf("handle event: %w", err)
			logger.Error("persist", "err", err)
		}
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-events:
				if !ok {
					return
				}
				persistHandleEvent(e)

				switch e.Type {
				case axstream.EVENT_RESET:
					logger.Info("page reset")

					err = initPageJS(page, logger, persistent, &persistLock)
					if err != nil {
						logger.Error("init js", "err", err)
					}
				case axstream.EVENT_PATCH:
					logger.Info("page updated", "updated", len(e.Updated))
					err = updateInspectorTree(page, persistent, &persistLock, e)
					if err != nil {
						logger.Error("update inspector", "err", err)
					}
				}

				for _, node := range e.Updated {
					go setAttr(setAttrReqs, node)
				}
			}
		}
	}()

	page.MustNavigate("http://localhost:8080")
	// page.MustNavigate("https://ocw.mit.edu/search/?d=Mathematics")
	// page.MustNavigate("https://www.google.com/travel/flights")
	// page.MustNavigate("https://amazon.com")

	<-ctx.Done()
}
