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
	class ObjectPool {
		constructor(newobj) {
			this.newobj = newobj
			this.pool = []
		}

		get() {
			let item = this.pool.pop()
			if (item === undefined) {
				return this.newobj()
			}
			return item
		}

		put(obj) {
			this.pool.push(obj)
		}
	}

	const highlighters = new ObjectPool(() => {
		const div = document.createElement("div")
		div.style.position = "fixed"
		div.style.zIndex = 99
		div.style.left = "0px"
		div.style.top = "0px"
		div.style.border = "1px solid red"
		div.style.pointerEvents = "none"
		document.body.append(div)
		return div
	})

	class Display {
		constructor() {
			this.status = document.createElement("p")
			this.status.style.position = "fixed"
			this.status.style.zIndex = 99
			this.status.style.bottom = "0px"
			this.status.style.right = "30vw"
			this.status.style.background = "white"
			this.status.style.color = "black"
			this.status.style.padding = "0.05rem"
			document.body.append(this.status)

			this.prevElements = []
		}

		clearPrev() {
			for (const p of this.prevElements) {
				p.style.display = "none"
				highlighters.put(p)
			}
			this.prevElements.length = 0
		}

		highlight(el) {
			const rect = el.getBoundingClientRect()
			const highlight = highlighters.get()
			highlight.style.transform = "translate(" + rect.left + "px, " + rect.top + "px)"
			highlight.style.width = String(rect.width) + "px"
			highlight.style.height = String(rect.height) + "px"
			highlight.style.display = "block"
			this.prevElements.push(highlight)
		}

		show(el, info) {
			this.status.innerText = "structure: " +
				info.StructureHash +
				" | path: " +
				info.PathHash +
				" (instances: " +
				info.Instances.length + ")"

			if (info.Instances.length > 50) {
				this.clearPrev()
				this.highlight(el)
				return
			}

			this.clearPrev()
			for (const axId of info.Instances) {
				const el = document.querySelector("[ax-id='" + axId + "']")
				if (el === null) {
					console.warn(axId, "does not exist!")
					continue
				}
				this.highlight(el)
			}
		}

		showError(el, err) {
			this.status.innerText = err

			this.clearPrev()
			this.highlight(el)
		}
	}

	const display = new Display()

	let state = null
	let prev = null

	window.addEventListener('mousemove', (e) => {
		const els = document.elementsFromPoint(e.clientX, e.clientY)

		let el = null
		let id = null
		for (const e of els) {
			id = e.getAttribute("ax-id")
			if (id === null) {
				continue
			}
			el = e
			break
		}

		if (id === null) {
			display.showError(els[0], "no elements with non-null ax-id")
			return
		}
		if (el === prev) {
			return
		}
		prev = el

		window.getStructInfo(id)
			.then((info) => {
				display.show(el, info)
				state = info
				console.log(info)
			})
			.catch((err) => {
				display.showError(el, String(err))
			})
	})

	window.addEventListener('keydown', (e) => {
		if (e.key === "c" && e.altKey) {
			navigator.clipboard.writeText(JSON.stringify(state))
			alert("copied!")
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

	_, err = page.Expose("getStructInfo", func(j gson.JSON) (any, error) {
		axId := j.Str()

		persistLock.Lock()
		structure := persistent.InstanceForAXID(proto.AccessibilityAXNodeID(axId))
		persistLock.Unlock()

		if structure == nil {
			return nil, nil
		}

		logger.Info("lookup: hash", "id", axId, "res", structure.Hash)

		type Return struct {
			// we return hashes as string representation because JS
			// unfortunately cannot store a full uint64 with number type
			StructureHash string
			PathHash      string
			Instances     []proto.AccessibilityAXNodeID
		}

		retrievedInst := persistent.InstancesByStructHash(structure.Hash)
		instances := make([]proto.AccessibilityAXNodeID, len(retrievedInst))
		for i, r := range retrievedInst {
			instances[i] = r.Underlying.Underlying.NodeID
		}

		out := Return{
			StructureHash: fmt.Sprint(structure.Hash),
			PathHash:      fmt.Sprint(structure.PathHash),
			Instances:     instances,
		}
		return out, nil
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
