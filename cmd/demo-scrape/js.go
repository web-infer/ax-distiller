package main

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/stealth"
	"ax-distiller/internal/structure"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/LQR471814/rod"
	"github.com/LQR471814/rod/lib/proto"
	"github.com/ysmood/gson"
)

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

func evalJS(page *rod.Page, expr string) (err error) {
	_, err = cdp.Command(page.GetContext(), page, proto.RuntimeEvaluate{
		Expression:    expr,
		ReturnByValue: true,
		AwaitPromise:  true,
	})
	return
}

func initPageJS(page *rod.Page, persistent *structure.Persistent, persistLock *sync.Mutex) (err error) {
	err = evalJS(page, stealth.JSExpr)

	err = evalJS(page, jsController)
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
		err = fmt.Errorf("expose fn: %w", err)
		return
	}

	depth := 1
	_, err = cdp.Command(page.GetContext(), page, proto.DOMGetDocument{
		Depth: &depth,
	})
	if err != nil {
		err = fmt.Errorf("get doc: %w", err)
		return
	}

	return
}

func pageLifecycleWorker(page *rod.Page, logger *slog.Logger, navStart chan struct{}) {
	events := page.Event()
	ctx := page.GetContext()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			switch ev.Method {
			case "Page.frameStartedNavigating":
				logger.Info("page frame started navigating")
				go func() {
					navStart <- struct{}{}
				}()
			}
		}
	}
}

func jsInitWorker(page *rod.Page, navStart chan struct{}, jsInit func() bool) {
	ctx := page.GetContext()
	for {
		select {
		case <-ctx.Done():
			return
		case <-navStart:
			for {
				ok := jsInit()
				if ok {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
}
