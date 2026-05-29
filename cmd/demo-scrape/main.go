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
	"sync"
	"time"

	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
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
		case r := <-reqs:
			pushReq := proto.DOMPushNodesByBackendIDsToFrontend{
				BackendNodeIDs: []proto.DOMBackendNodeID{
					r.Underlying.BackendDOMNodeID,
				},
			}
			pushRes, err := cdp.Command(ctx, page, pushReq)
			if err != nil {
				continue
			}
			req := cdp.DOMSetAttributeValue{
				NodeID: pushRes.NodeIDs[0],
				Name:   "ax-id",
				Value:  string(r.Underlying.NodeID),
			}
			err = cdp.CommandUnary(ctx, page, req)
			if err != nil {
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
	reqs <- node
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
	let prev = Date.now()
	window.onmousemove = (e) => {
		const now = Date.now()
		if (now - prev < 50) {
			return
		}
		prev = now
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

func initJSClient(ctx context.Context, page *rod.Page, persistent *structure.Persistent) (err error) {
	page.MustEval(jsController)

	page.MustExpose("getStructureHash", func(j gson.JSON) (interface{}, error) {
		axId := j.Str()
		structure := persistent.LookupStructure(proto.AccessibilityAXNodeID(axId))
		if structure == nil {
			return nil, nil
		}
		return fmt.Sprint(structure.Hash), nil
	})

	depth := 1
	_, err = cdp.Command(ctx, page, proto.DOMGetDocument{
		Depth: &depth,
	})
	if err != nil {
		return
	}

	return
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
	page := browser.MustPage("about:blank")
	chrome.DisableUnusedCDP(page)

	events, err := axstream.Listen(ctx, logger, page)
	if err != nil {
		panic(err)
	}

	setAttrReqs := make(chan *cdp.AXNodeWithRelatives, 4)
	for range 4 {
		go setAttrWorker(ctx, page, logger, setAttrReqs)
	}

	timer := time.NewTimer(250 * time.Millisecond)
	persistLock := sync.Mutex{}
	persistent := structure.NewPersistent(logger)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				persistLock.Lock()
				persistent.HandleEvent(e)
				switch e.Type {
				case axstream.EVENT_RESET:
					logger.Info("page reset", "root", persistent.Root.Hash)
					timer.Reset(250 * time.Millisecond)

					initJSClient(ctx, page, persistent)
				case axstream.EVENT_PATCH:
					logger.Info("page updated", "added", len(e.Added), "updated", len(e.Updated))
					timer.Reset(250 * time.Millisecond)
				}
				persistLock.Unlock()

				for _, node := range e.Added {
					go setAttr(setAttrReqs, node)
				}
				for _, node := range e.Updated {
					go setAttr(setAttrReqs, node)
				}
			}
		}
	}()

	// go func() {
	// 	for {
	// 		select {
	// 		case <-ctx.Done():
	// 			return
	// 		case <-timer.C:
	// 			persistLock.Lock()
	//
	// 			var keys []uint64
	// 			for k := range persistent.Index {
	// 				keys = append(keys, k)
	// 			}
	// 			slices.SortFunc(keys, func(a, b uint64) int {
	// 				return len(persistent.Index[b]) - len(persistent.Index[a])
	// 			})
	// 			for _, k := range keys {
	// 				nodes := persistent.Index[k]
	// 				fmt.Printf("\nHash -- %v (%v)\n", k, len(nodes))
	// 				for i := range 3 {
	// 					if i >= len(nodes) {
	// 						break
	// 					}
	// 					tree.PrintSExpr(nodes[i], os.Stdout)
	// 					fmt.Println()
	// 				}
	// 			}
	//
	// 			persistLock.Unlock()
	// 		}
	// 	}
	// }()

	page.MustNavigate("https://www.google.com/travel/flights")

	<-ctx.Done()
}
