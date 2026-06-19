/** @typedef {string} AXID */

const arrowDown = document.createElement("template");
arrowDown.innerHTML = `
<svg class="arrow-icon" xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="lucide lucide-arrow-down-icon lucide-arrow-down">
	<path d="M12 5v14"/>
	<path d="m19 12-7 7-7-7"/>
</svg>
`;

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
	/** @type {boolean} */
	#expanded = true;

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
		this.#label.className = "node-label";

		this.#arrowDown = document.createElement("div");
		this.#arrowDown.className = "arrow-icon-container";
		this.#arrowDown.append(arrowDown.content.cloneNode(true));

		const text = document.createElement("span");
		text.innerText = name;
		this.#label.append(this.#arrowDown);
		this.#label.append(text);

		this.#root.append(this.#label);
		this.#root.append(this.#container);

		this.updateState(parent, id, name, children);
	}

	#updateArrow() {
		if (this.children.length === 0) {
			this.#arrowDown.style.display = "none";
			return;
		}
		this.#arrowDown.style.display = "flex";

		if (this.#expanded) {
			this.#arrowDown.style.transform = "";
		} else {
			this.#arrowDown.style.transform = "rotate(180deg)";
		}
	}

	/**
	 * @returns {boolean}
	 */
	isExpanded() {
		return this.#expanded;
	}

	/**
	 * @param {boolean} expanded
	 */
	setExpanded(expanded) {
		this.#expanded = expanded;
		this.#updateArrow();
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
		this.children = children;
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
	}

	/**
	 * @param {() => void} callback
	 */
	onSelect(callback) {
		this.#label.onclick = callback;
	}

	/**
	 * @param {() => void} callback
	 */
	onExpandToggle(callback) {
		this.#arrowDown.onclick = callback;
		this.#label.ondblclick = callback;
	}
}

class Tree {
	/** @typedef {(id: AXID) => void} OnSelect */
	/** @typedef {(id: AXID) => void} OnExpand */

	/** @type {HTMLDivElement} */
	root;
	/** @type {Map<string, TreeNode>} */
	nodes;

	/** @type {OnSelect | null} */
	#onSelect = null;

	/** @type {OnExpand | null} */
	#onExpand = null;

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
			if (node.isExpanded()) {
				node.setExpanded(false);
				this.#collapseNode(node);
				return;
			}
			node.setExpanded(true);
			this.#expandNode(node);
		});
		node.onSelect(() => {
			this.#selectNode(node);
		});
	}

	/**
	 * @param {AXID} id
	 */
	removeSubtree(id) {
		const node = this.resolve(id);
		if (!node) {
			return;
		}
		this.nodes.delete(node.id);
		node.detach(this);
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
	onExpand(callback) {
		this.#onExpand = callback;
	}

	/**
	 *  @param {TreeNode} node
	 */
	#selectNode(node) {
		if (!this.#onSelect) {
			return;
		}
		this.#onSelect(node.id);
	}

	/**
	 * @param {TreeNode} node
	 */
	#expandNode(node) {
		if (!this.#onExpand) {
			return;
		}
		this.#onExpand(node.id);
	}

	/**
	 * @param {TreeNode} node
	 */
	#collapseNode(node) {
		node.detachChildren();
	}
}
