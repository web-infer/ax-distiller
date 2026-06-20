/**
 * @template T
 */
class ObjectPool {
	/** @type {() => T} */
	newobj;
	/** @type {T[]} */
	pool;

	/**
	 * @param {() => T} newobj
	 */
	constructor(newobj) {
		this.newobj = newobj;
		this.pool = [];
	}

	/**
	 * @returns {T}
	 */
	get() {
		let item = this.pool.pop();
		if (item === undefined) {
			return this.newobj();
		}
		return item;
	}

	/**
	 * @param {T} obj
	 */
	put(obj) {
		this.pool.push(obj);
	}
}

/**
 * @typedef {{Parent: string | null, ID: string, Role: string, StructureHash: string, PathHash: string, Instances: string[], Children: string[]}} InfoStruct
 */

class Highlighter {
	/** @type {ObjectPool<HTMLDivElement>} */
	#pool;
	/** @type {HTMLDivElement[]} */
	#active;

	constructor() {
		this.#active = [];
		this.#pool = new ObjectPool(() => {
			const div = document.createElement("div");
			div.style.position = "fixed";
			div.style.zIndex = "99";
			div.style.left = "0px";
			div.style.top = "0px";
			div.style.border = "1px solid red";
			div.style.pointerEvents = "none";
			document.body.append(div);
			return div;
		});
	}

	clear() {
		for (const h of this.#active) {
			h.style.display = "none";
			this.#pool.put(h);
		}
		this.#active.length = 0;
	}

	/**
	 * @param {Element} el
	 */
	highlight(el) {
		const rect = el.getBoundingClientRect();
		const highlight = this.#pool.get();
		highlight.style.transform =
			"translate(" + rect.left + "px, " + rect.top + "px)";
		highlight.style.width = String(rect.width) + "px";
		highlight.style.height = String(rect.height) + "px";
		highlight.style.display = "block";
		this.#active.push(highlight);
	}
}

/** @typedef {"role" | "struct_hash" | "path_hash" | "instance_count"} Fields */

class Display {
	/** @type {Record<string, HTMLSpanElement>} */
	#fields;

	/**
	 * @param {HTMLDivElement} root
	 */
	constructor(root) {
		this.root = root;
		this.root.style.display = "grid";
		this.root.style.gridTemplateColumns = "1fr 2fr";

		this.#fields = {};

		this.#makePair("<Alt+C>", "Show element in tree.");

		const hr = document.createElement("hr");
		hr.style.gridColumn = "span 2";
		hr.style.width = "100%";
		this.root.append(hr);

		this.#makeField("role", "AX Role");
		this.#makeField("struct_hash", "Structure Hash");
		this.#makeField("path_hash", "Path Hash");
		this.#makeField("instance_count", "Instances");
	}

	/**
	 * @param {string} label
	 * @param {string} value
	 */
	#makePair(label, value) {
		const labelNode = document.createElement("span");
		labelNode.innerText = label + ": ";
		const valueNode = document.createElement("span");
		valueNode.innerText = value;
		this.root.append(labelNode, valueNode);
		return [labelNode, valueNode];
	}

	/**
	 * @param {Fields} field
	 * @param {string} name
	 */
	#makeField(field, name) {
		const [, value] = this.#makePair(name, "null");
		this.#fields[field] = value;
	}

	/**
	 * @param {Fields} field
	 * @param {string} value
	 */
	#setField(field, value) {
		this.#fields[field].innerText = value;
	}

	/**
	 * @param {InfoStruct} info
	 */
	setInfo(info) {
		this.#setField("role", info.Role);
		this.#setField("struct_hash", info.StructureHash);
		this.#setField("path_hash", info.PathHash);
		this.#setField("instance_count", String(info.Instances.length));
	}

	/**
	 * @param {string} err
	 */
	setError(err) {
		this.#setField("role", err);
		this.#setField("struct_hash", "");
		this.#setField("path_hash", "");
		this.#setField("instance_count", "");
	}
}

class SyncedTree {
	/** @type {Tree} */
	#tree;

	/**
	 * @param {HTMLDivElement} root
	 */
	constructor(root) {
		this.#tree = new Tree(root);

		this.#tree.onExpandToggle((node) => {
			if (node.isExpanded()) {
				node.detachChildren();
				return;
			}
			console.log(node);
			expandLevels(node.id, 2)
				.then((infos) => {
					infos.splice(0, 1);
					for (const info of infos) {
						this.#tree.add(TreeNode.fromInfo(info));
					}
				})
				.catch((err) => {
					console.error("expand levels (1)", err);
				});
		});

		expandLevels(null, 10)
			.then((infos) => {
				for (const info of infos) {
					this.#tree.add(TreeNode.fromInfo(info));
				}
			})
			.catch((err) => {
				console.error("expand levels", err);
			});
	}

	/**
	 * @param {OnSelect} callback
	 */
	onSelect(callback) {
		this.#tree.onSelect(callback);
	}

	/**
	 * @param {AXID} id
	 */
	show(id) {
		expandTo(id)
			.then((infos) => {
				for (const info of infos.reverse()) {
					const existing = this.#tree.nodes.get(info.ID);
					if (existing) {
						existing.detach(this.#tree);
					}
				}
				// we reverse again because reverse() mutates the array
				for (const info of infos.reverse()) {
					this.#tree.add(TreeNode.fromInfo(info));
				}
			})
			.catch((err) => {
				console.error("expand to", err);
			});
	}

	/**
	 * @param {InfoStruct[]} infos
	 */
	update(infos) {
		for (const update of infos) {
			this.#tree.update(TreeNode.fromInfo(update));
		}
	}
}

class Inspector {
	/** @type {Highlighter} */
	#highlight;

	/** @type {SyncedTree} */
	#tree;

	/** @type {Display} */
	#display;

	constructor() {
		const style = document.createElement("style");
		style.textContent = `
.__ax_inspector_node-label {
  display: flex;
  border: 0;
  text-align: left;
  width: 100%;
  margin: 0;
}
.__ax_inspector_node-label:hover {
  color: white;
  background-color: gray;
}
.__ax_inspector_arrow-icon-container {
  display: flex;
  align-items: center;
}
.__ax_inspector_arrow-icon {
  width: 1rem;
  height: 1rem;
}
	`;
		document.head.append(style);

		this.#highlight = new Highlighter();

		this.container = document.createElement("div");

		this.container.style.display = "grid";
		this.container.style.gridTemplateRows = "1fr min-content";
		this.container.style.position = "fixed";
		this.container.style.zIndex = "99";
		this.container.style.background = "white";
		this.container.style.color = "black";
		this.container.style.padding = "0.05rem";

		this.container.style.height = "600px";
		this.container.style.width = "400px";
		this.container.style.bottom = "0px";
		this.container.style.right = "0px";

		const tree = document.createElement("div");
		tree.style.overflow = "scroll";
		const display = document.createElement("div");

		this.#tree = new SyncedTree(tree);

		this.#tree.onSelect((node) => {
			getStructInfo(node.id)
				.then((info) => {
					const el = this.#getNodeByAXID(info.ID);
					if (!el) {
						this.#display.setInfo(info);
						return;
					}
					this.show(el, info);
				})
				.catch((err) => {
					console.error("get struct info:", err);
				});
		});

		this.#display = new Display(display);
		this.container.append(tree, display);

		document.body.append(this.container);
	}

	/**
	 * @param {AXID} id
	 */
	treeShow(id) {
		this.#tree.show(id);
	}

	/**
	 * @param {InfoStruct[]} infos
	 */
	updateTree(infos) {
		this.#tree.update(infos);
	}

	/**
	 * @param {AXID} id
	 * @returns {Element | null}
	 */
	#getNodeByAXID(id) {
		return document.querySelector(`[ax-id='${id}']`);
	}

	/**
	 * @param {Element} el
	 * @param {InfoStruct} info
	 */
	show(el, info) {
		this.#display.setInfo(info);

		if (info.Instances.length > 50) {
			this.#highlight.clear();
			this.#highlight.highlight(el);
			return;
		}

		this.#highlight.clear();
		for (const axId of info.Instances) {
			const el = this.#getNodeByAXID(axId);
			if (el === null) {
				console.warn(axId, "does not exist!");
				continue;
			}
			this.#highlight.highlight(el);
		}
	}

	/**
	 * @param {Element} el
	 * @param {string} err
	 */
	showError(el, err) {
		this.#display.setError(err);
		this.#highlight.clear();
		this.#highlight.highlight(el);
	}
}

const inspect = new Inspector();

/** @type {InfoStruct | null} */
let state = null;

/** @type {Element | null} */
let prev = null;

window.addEventListener("mousemove", (e) => {
	const els = document.elementsFromPoint(e.clientX, e.clientY);

	let el = null;
	let id = null;
	for (const e of els) {
		id = e.getAttribute("ax-id");
		if (id === null) {
			continue;
		}
		el = e;
		break;
	}

	if (el === null || id === null) {
		inspect.showError(els[0], "no elements with non-null ax-id");
		return;
	}
	if (el === prev) {
		return;
	}
	prev = el;

	let parent = el.parentNode;
	while (parent !== null) {
		if (parent === inspect.container) {
			return;
		}
		parent = parent.parentNode;
	}

	getStructInfo(id)
		.then((info) => {
			inspect.show(el, info);
			state = info;
			console.log(info);
		})
		.catch((err) => {
			inspect.showError(el, String(err));
		});
});

window.addEventListener("keydown", (e) => {
	if (e.key === "c" && e.altKey) {
		if (state) {
			inspect.treeShow(state.ID);
		}
	}
});

// TODO: complete
Object.defineProperty(window, "__ax_inspector_updateTree", {
	value:
		/**
		 * @param {InfoStruct[]} infos
		 */
		(infos) => {
			inspect.updateTree(infos ?? []);
		},
	enumerable: false,
	writable: false,
	configurable: false,
});
