/**
 * @param {string} id
 * @returns {Promise<InfoStruct>}
 */
function getStructInfo(id) {
	//@ts-expect-error
	return window.__ax_inspect_getStructInfo(id);
}

/**
 * @param {string} id
 * @returns {Promise<InfoStruct[]>}
 */
function expandTo(id) {
	//@ts-expect-error
	return window.__ax_inspect_expandTo(id);
}

/**
 * @param {string | null} id
 * @param {number} levels
 * @returns {Promise<InfoStruct[]>}
 */
function expandLevels(id, levels) {
	//@ts-expect-error
	return window.__ax_inspect_expandLevels([id ?? "", levels]);
}
