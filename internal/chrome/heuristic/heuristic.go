package heuristic

import (
	"ax-distiller/internal/structure"
	"strings"
)

// noiseRoles are subtree roots that reliably contain no useful navigation or
// content for web traversal goals.
var noiseRoles = map[string]bool{
	"contentinfo": true, // page footer
}

// noiseNamePrefixes match accessible names of decorative / accessibility-only
// sections that add tokens without helping navigation or extraction.
var noiseNamePrefixes = []string{
	"Skip to",
	"Shortcuts menu",
	"Keyboard shortcuts",
}

// Simplify returns a pruned copy of root with known-noise subtrees removed.
// Deterministic — no LLM call. Original tree is not modified.
func Simplify(root *structure.Structure) *structure.Structure {
	return pruneList(root)
}

func pruneList(head *structure.Structure) *structure.Structure {
	var newHead, tail *structure.Structure
	for cur := head; cur != nil; cur = cur.NextSibling {
		if isNoise(cur) {
			continue
		}
		n := &structure.Structure{
			Hash:       cur.Hash,
			Underlying: cur.Underlying,
			FirstChild: pruneList(cur.FirstChild),
		}
		if newHead == nil {
			newHead = n
			tail = n
		} else {
			tail.NextSibling = n
			tail = n
		}
	}
	return newHead
}

func isNoise(n *structure.Structure) bool {
	if noiseRoles[n.Underlying.Role.Value] {
		return true
	}
	name := n.Underlying.Name.Value
	for _, prefix := range noiseNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
