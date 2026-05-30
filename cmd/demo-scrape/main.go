package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/chrome/fastclient"
	"ax-distiller/internal/slogx"
	"ax-distiller/internal/structure"
	"context"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/ysmood/gson"
)

func NewTestBrowser(chromeBin string) (browser *rod.Browser, err error) {
	dataTemp := "/tmp/ax-distiller/chrome-data"
	err = os.RemoveAll(dataTemp)
	if err != nil {
		return
	}
	err = os.MkdirAll(dataTemp, 0700)
	if err != nil {
		return
	}

	launch := launcher.New().Bin(chromeBin).
		UserDataDir(dataTemp).
		Headless(false).
		Set("display", os.Getenv("DISPLAY")).
		Set("disable-extensions", "false").
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-gpu", "true").
		Set("no-sandbox", "true").
		Set("no-default-browser-check", "true").
		Set("disable-remote-fonts", "true").
		Set("disable-background-networking", "true").
		Set("disable-dev-shm-usage", "true").
		Set("disable-sync", "true").
		Set("disable-translate", "true").
		Set("disable-default-apps", "true").
		Set("mute-audio", "true").
		Set("hide-scrollbars", "true")

	controlURL := launch.MustLaunch()
	browser = rod.New()
	client := fastclient.New()
	ws := &rodcdp.WebSocket{}
	err = ws.Connect(browser.GetContext(), controlURL, nil)
	if err != nil {
		panic(err)
	}
	client.Start(ws)
	browser.Client(client)
	browser.MustConnect()
	return
}

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

const jsController = `
() => {
	const status = document.createElement("p")
	status.style.position = "fixed"
	status.style.bottom = "0px"
	status.style.right = "30vw"
	status.style.background = "white"
	status.style.color = "black"
	status.style.padding = "0.05rem"
	document.body.append(status)

	const setHash = (hash) => {
		status.innerText = hash
	}

	let prevEl = null
	let hashState = ""
	window.onmousemove = (e) => {
		const el = document.elementFromPoint(e.clientX, e.clientY)
		const id = el.getAttribute("ax-id")
		if (id === null) {
			setHash(null)
			prevId = null
			return
		}
		if (prevEl === el) {
			return
		}
		if (prevEl) prevEl.style.outline = ""
		prevEl = el
		prevEl.style.outline = "red solid 1px"
		window.getStructureHash(id).then((hash) => {
			if (hashState === hash) { return }
			hashState = hash
			setHash(hash)
		})
	}

	window.onkeydown = (e) => {
		if (e.key === "c" && e.altKey) {
			navigator.clipboard.writeText(hashState)
			setHash("copied: " + hashState)
		}
	}
}
`

func initPageJS(ctx context.Context, page *rod.Page, persistent *structure.Persistent, persistLock *sync.Mutex) (err error) {
	_, err = page.Eval(jsController)
	if err != nil {
		return
	}

	_, err = page.Expose("getStructureHash", func(j gson.JSON) (any, error) {
		axId := j.Str()

		persistLock.Lock()
		structure := persistent.LookupStructure(proto.AccessibilityAXNodeID(axId))
		persistLock.Unlock()
		if structure == nil {
			return nil, nil
		}

		return fmt.Sprint(structure.Hash), nil
	})
	if err != nil {
		return
	}

	depth := 1
	_, err = cdp.Command(ctx, page, proto.DOMGetDocument{
		Depth: &depth,
	})
	if err != nil {
		return
	}

	return
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

	browser, err := NewTestBrowser("chromium")
	if err != nil {
		panic(err)
	}
	defer browser.Close()

	page := stealth.MustPage(browser)
	chrome.DisableUnusedCDP(page)

	events, err := axstream.Listen(ctx, logger, page)
	if err != nil {
		panic(err)
	}

	setAttrReqs := make(chan *cdp.AXNodeWithRelatives, 4)
	for range 4 {
		go setAttrWorker(ctx, page, logger, setAttrReqs)
	}

	persistLock := sync.Mutex{}
	persistent := structure.NewPersistent(logger)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-events:
				if !ok {
					return
				}
				persistLock.Lock()
				persistent.HandleEvent(e)
				persistLock.Unlock()

				switch e.Type {
				case axstream.EVENT_RESET:
					logger.Info("page reset", "root", persistent.Root.Hash)

					err = initPageJS(ctx, page, persistent, &persistLock)
					if err != nil {
						return
					}
				case axstream.EVENT_PATCH:
					logger.Info("page updated", "updated", len(e.Updated))
				}

				for _, node := range e.Updated {
					go setAttr(setAttrReqs, node)
					logger.Info("updated", "role", node.Underlying.Role.Value, "id", node.Underlying.NodeID)
					visit(node, func(awr *cdp.AXNodeWithRelatives) {
						if awr.Underlying.Role.Value == "listbox" || awr.Underlying.Role.Value == "option" {
							fmt.Println(awr)
						}
					})
				}
			}
		}
	}()

	page.MustNavigate("http://localhost:8080")
	// page.MustNavigate("https://www.google.com/travel/flights")
	// page.MustNavigate("https://amazon.com")

	<-ctx.Done()
}
