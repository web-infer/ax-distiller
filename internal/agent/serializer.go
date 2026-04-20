package agent

import (
	"ax-distiller/internal/structure"
	"fmt"
	"strings"
)

type SerializeOptions struct {
	MaxLines   int
	MaxDepth   int
	NameMaxLen int
}

func DefaultSerializeOptions() SerializeOptions {
	return SerializeOptions{MaxLines: 500, MaxDepth: 12, NameMaxLen: 80}
}

func Serialize(root *structure.Structure, opts SerializeOptions) string {
	if root == nil {
		return "(page not loaded)"
	}
	var sb strings.Builder
	count := 0
	serializeNode(&sb, root, 0, &count, opts)
	return sb.String()
}

// skipRoles are noisy structural roles with no LLM value — skip unless they have a name.
var skipRoles = map[string]bool{
	"StaticText":    true,
	"InlineTextBox": true,
}

// transparentRoles pass through to children without emitting a line when they have no name.
var transparentRoles = map[string]bool{
	"generic":  true,
	"none":     true,
	"ignored":  true,
}

func serializeNode(sb *strings.Builder, node *structure.Structure, depth int, count *int, opts SerializeOptions) {
	if node == nil || *count >= opts.MaxLines || depth > opts.MaxDepth {
		return
	}

	role := node.Underlying.Role.Value
	name := node.Underlying.Name.Value
	if len(name) > opts.NameMaxLen {
		name = name[:opts.NameMaxLen] + "..."
	}

	isSynthetic := role == "SYNTHETIC_LIST" || role == "SYNTHETIC_OBJECT"

	// skip pure noise roles
	if skipRoles[role] {
		serializeNode(sb, node.NextSibling, depth, count, opts)
		return
	}

	// transparent roles with no name: don't emit a line, but still recurse children
	if transparentRoles[role] && name == "" && !isSynthetic {
		serializeNode(sb, node.FirstChild, depth, count, opts)
		serializeNode(sb, node.NextSibling, depth, count, opts)
		return
	}

	indent := strings.Repeat("  ", depth)
	if isSynthetic {
		fmt.Fprintf(sb, "%s%s:\n", indent, role)
	} else {
		id := node.Underlying.BackendDOMNodeID
		fmt.Fprintf(sb, "%s[%d] %s: %q\n", indent, id, role, name)
	}
	*count++

	if *count >= opts.MaxLines {
		fmt.Fprintf(sb, "%s... (truncated at %d lines)\n", indent, opts.MaxLines)
		return
	}

	serializeNode(sb, node.FirstChild, depth+1, count, opts)
	serializeNode(sb, node.NextSibling, depth, count, opts)
}
