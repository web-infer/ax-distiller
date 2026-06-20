/** @typedef {string} AXID */

function newArrowDown() {
	const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");

	svg.classList.add("__ax_inspector_arrow-icon");

	svg.setAttribute("xmlns", "http://www.w3.org/2000/svg");
	svg.setAttribute("width", "24");
	svg.setAttribute("height", "24");
	svg.setAttribute("viewBox", "0 0 24 24");
	svg.setAttribute("fill", "none");
	svg.setAttribute("stroke", "currentColor");
	svg.setAttribute("stroke-width", "2");
	svg.setAttribute("stroke-linecap", "round");
	svg.setAttribute("stroke-linejoin", "round");

	const path1 = document.createElementNS(
		"http://www.w3.org/2000/svg",
		"path",
	);
	path1.setAttribute("d", "M12 5v14");

	const path2 = document.createElementNS(
		"http://www.w3.org/2000/svg",
		"path",
	);
	path2.setAttribute("d", "m19 12-7 7-7-7");

	svg.append(path1, path2);
	return svg;
}

class NodeNotFoundError extends Error {
	/**
	 * @param {string} id
	 */
	constructor(id) {
		super(`unknown node: ${id}`);
		this.name = "NodeNotFoundError";
		this.id = id;
	}
}

class TreeNode {
	/** @type {HTMLDivElement} */
	#root;
	/** @type {HTMLDivElement} */
	#container;
	/** @type {HTMLButtonElement} */
	#label;
	/** @type {HTMLDivElement} */
	#arrowDown;

	/** @type {AXID | null} */
	parent = null;
	/** @type {AXID} */
	id = "";
	/** @type {string} */
	name = "";
	/** @type {AXID[]} */
	children = [];

	/**
	 * @param {AXID | null} parent
	 * @param {AXID} id
	 * @param {string} name
	 * @param {AXID[]} children
	 */
	constructor(parent, id, name, children) {
		this.#root = document.createElement("div");

		this.#container = document.createElement("div");
		this.#container.style.paddingLeft = "1rem";

		this.#label = document.createElement("button");
		this.#label.className = "__ax_inspector_node-label";

		this.#arrowDown = document.createElement("div");
		this.#arrowDown.className = "__ax_inspector_arrow-icon-container";
		this.#arrowDown.append(newArrowDown());

		const text = document.createElement("span");
		text.innerText = name;
		this.#label.append(this.#arrowDown);
		this.#label.append(text);

		this.#root.append(this.#label);
		this.#root.append(this.#container);

		this.updateState(parent, id, name, children);
	}

	/**
	 * @param {InfoStruct} info
	 */
	static fromInfo(info) {
		return new TreeNode(
			info.Parent,
			info.ID,
			`${info.Role} (${info.StructureHash})`,
			info.Children,
		);
	}

	#updateArrow() {
		if (this.children.length === 0) {
			this.#arrowDown.style.display = "none";
			return;
		}

		this.#arrowDown.style.display = "flex";
		if (this.isExpanded()) {
			this.#arrowDown.style.transform = "";
		} else {
			this.#arrowDown.style.transform = "rotate(180deg)";
		}
	}

	/**
	 * @returns {boolean}
	 */
	isExpanded() {
		return this.#container.children.length >= this.children.length;
	}

	/**
	 * @param {AXID | null} parent
	 * @param {AXID} id
	 * @param {string} name
	 * @param {AXID[]} children
	 */
	updateState(parent, id, name, children) {
		this.parent = parent;
		this.id = id;
		this.name = name;
		this.children = children ?? [];
		this.#updateArrow();
	}

	/**
	 * @param {Tree} tree
	 */
	attach(tree) {
		if (this.parent === null) {
			tree.root.append(this.#root);
			return;
		}
		const parent = tree.mustResolve(this.parent);
		parent.#container.append(this.#root);
		parent.#updateArrow();
	}

	/**
	 * @param {Tree} tree
	 */
	detach(tree) {
		if (this.parent === null) {
			tree.root.removeChild(this.#root);
			return;
		}
		const parent = tree.mustResolve(this.parent);
		parent.#container.removeChild(this.#root);
	}

	detachChildren() {
		this.#container.replaceChildren();
		this.#updateArrow();
	}

	/**
	 * @param {() => void} callback
	 */
	onSelect(callback) {
		this.#label.onmouseenter = callback;
	}

	/**
	 * @param {() => void} callback
	 */
	onExpandToggle(callback) {
		this.#label.onclick = callback;
	}
}

class Tree {
	/** @typedef {(node: TreeNode) => void} OnSelect */
	/** @typedef {(node: TreeNode) => void} OnExpand */

	/** @type {HTMLDivElement} */
	root;
	/** @type {Map<string, TreeNode>} */
	nodes;

	/** @type {OnSelect | null} */
	#onSelect = null;

	/** @type {OnExpand | null} */
	#onExpandToggle = null;

	/** @param {HTMLDivElement} root */
	constructor(root) {
		this.root = root;
		this.nodes = new Map();
	}

	/**
	 * @param {AXID} id
	 * @returns {TreeNode}
	 * @throws {NodeNotFoundError}
	 */
	mustResolve(id) {
		const node = this.nodes.get(id);
		if (!node) {
			throw new NodeNotFoundError(`node not found: ${id}`);
		}
		return node;
	}

	/**
	 * @param {AXID} id
	 * @returns {TreeNode | undefined}
	 */
	resolve(id) {
		return this.nodes.get(id);
	}

	/**
	 * @param {TreeNode} node
	 */
	add(node) {
		if (!(node instanceof TreeNode)) {
			throw new Error("node must be an instance of TreeNode");
		}
		this.nodes.set(node.id, node);
		node.attach(this);
		node.onExpandToggle(() => {
			if (!this.#onExpandToggle) {
				return;
			}
			this.#onExpandToggle(node);
		});
		node.onSelect(() => {
			this.#selectNode(node);
		});
	}

	/**
	 * @param {TreeNode} node
	 */
	update(node) {
		const existing = this.nodes.get(node.id);
		if (!existing) {
			return;
		}
		existing.updateState(node.parent, node.id, node.name, node.children);
	}

	/**
	 * @param {AXID} id
	 */
	removeSubtree(id) {
		const node = this.resolve(id);
		if (!node) {
			return;
		}
		node.detach(this);
		this.nodes.delete(node.id);
	}

	/**
	 * @param {OnSelect} callback
	 */
	onSelect(callback) {
		this.#onSelect = callback;
	}

	/**
	 * @param {OnExpand} callback
	 */
	onExpandToggle(callback) {
		this.#onExpandToggle = callback;
	}

	/**
	 *  @param {TreeNode} node
	 */
	#selectNode(node) {
		if (!this.#onSelect) {
			return;
		}
		this.#onSelect(node);
	}
}
