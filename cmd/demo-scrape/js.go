package main

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/structure"
	"fmt"
	"log/slog"
	"sync"

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

	window.addEventListener('mousemove', (e) => {
		const el = document.elementFromPoint(e.clientX, e.clientY)
		const id = el.getAttribute("ax-id")
		if (id === null) {
			console.log(document.elementsFromPoint(e.clientX, e.clientY))
			setHash(null)
			return
		}
		if (prevEl === el) {
			return
		}
		if (prevEl) prevEl.style.outline = ""
		prevEl = el
		prevEl.style.outline = "red solid 1px"
		window.getStructureHash(id)
			.then((hash) => {
				if (hashState === hash) {
					return
				}
				if (hash === null) {
					setHash("NULL")
					return
				}
				hashState = hash
				setHash(hash)
			})
			.catch((err) => {
				setHash("ERR")
			})
	})

	window.addEventListener('keydown', (e) => {
		if (e.key === "c" && e.altKey) {
			navigator.clipboard.writeText(hashState)
			setHash("copied: " + hashState)
		}
	})
}
`

func initPageJS(page *rod.Page, logger *slog.Logger, persistent *structure.Persistent, persistLock *sync.Mutex) (err error) {
	_, err = page.Eval(jsController)
	if err != nil {
		err = fmt.Errorf("init js control: %w", err)
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

		logger.Info("lookup: hash", "id", axId, "res", structure.Hash)

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
