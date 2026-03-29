package postprocess

import "ax-distiller/internal/chrome/cdp"

func FilterIgnored(node *cdp.AXNodeWithRelatives) (ret *cdp.AXNodeWithRelatives) {
	if node == nil {
		ret = nil
		return
	}

	fc := FilterIgnored(node.FirstChild)
	ns := FilterIgnored(node.NextSibling)

	if !node.Ignored {
		node.FirstChild = fc
		node.NextSibling = ns
		ret = node
		return
	}

	if fc != nil {
		ret = fc
		lastChild := fc
		for lastChild.NextSibling != nil {
			lastChild = lastChild.NextSibling
		}
		lastChild.NextSibling = ns
		return
	}

	if ns != nil {
		ret = ns
		return
	}

	ret = nil
	return
}
