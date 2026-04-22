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
	serializeNode(&sb, root, &count, opts)
	return sb.String()
}

var skipRoles = map[string]bool{
	"StaticText":    true,
	"InlineTextBox": true,
}

var transparentRoles = map[string]bool{
	"generic": true,
	"none":    true,
	"ignored": true,
}

type stackEntry struct {
	node  *structure.Structure
	depth int
}

// serializeNode iterates the left-child/right-sibling tree without recursion.
// Transparent nodes (generic with no name) are collapsed — children promoted to same depth.
func serializeNode(sb *strings.Builder, root *structure.Structure, count *int, opts SerializeOptions) {
	stack := []stackEntry{{root, 0}}

	for len(stack) > 0 && *count < opts.MaxLines {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		node, depth := e.node, e.depth
		if node == nil || depth > opts.MaxDepth {
			continue
		}

		role := node.Underlying.Role.Value
		name := node.Underlying.Name.Value
		if len(name) > opts.NameMaxLen {
			name = name[:opts.NameMaxLen] + "..."
		}

		isSynthetic := role == "SYNTHETIC_LIST" || role == "SYNTHETIC_OBJECT"

		// push sibling before processing children (stack is LIFO — sibling processed after subtree)
		if node.NextSibling != nil {
			stack = append(stack, stackEntry{node.NextSibling, depth})
		}

		if skipRoles[role] {
			continue
		}

		if transparentRoles[role] && name == "" && !isSynthetic {
			// collapse: promote children to same depth (not depth+1)
			if node.FirstChild != nil {
				stack = append(stack, stackEntry{node.FirstChild, depth})
			}
			continue
		}

		indent := strings.Repeat("  ", depth)
		if isSynthetic {
			fmt.Fprintf(sb, "%s%s:\n", indent, role)
		} else {
			id := node.Underlying.BackendDOMNodeID
			fmt.Fprintf(sb, "%s[%d] %s: %q\n", indent, id, role, name)
		}
		*count++

		if node.FirstChild != nil {
			stack = append(stack, stackEntry{node.FirstChild, depth + 1})
		}
	}

	if *count >= opts.MaxLines {
		fmt.Fprintf(sb, "... (truncated at %d lines)\n", opts.MaxLines)
	}
}
