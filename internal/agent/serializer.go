package agent

import (
	"ax-distiller/internal/structure"
	"fmt"
	"strings"
)

type SerializeOptions struct {
	MaxLines     int
	MaxDepth     int
	NameMaxLen   int
	MaxListItems int // max direct children rendered per SYNTHETIC_LIST (0 = unlimited)
}

func DefaultSerializeOptions() SerializeOptions {
	return SerializeOptions{MaxLines: 500, MaxDepth: 12, NameMaxLen: 80, MaxListItems: 8}
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
	node             *structure.Structure
	depth            int
	suppressSiblings bool
	truncMsg         string // if non-empty, print this line and skip node rendering
}

// serializeNode iterates the left-child/right-sibling tree without recursion.
// Transparent nodes (generic with no name) are collapsed — children promoted to same depth.
func serializeNode(sb *strings.Builder, root *structure.Structure, count *int, opts SerializeOptions) {
	stack := []stackEntry{{node: root}}

	for len(stack) > 0 && *count < opts.MaxLines {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// truncation message sentinel
		if e.truncMsg != "" {
			fmt.Fprintf(sb, "%s%s\n", strings.Repeat("  ", e.depth), e.truncMsg)
			*count++
			continue
		}

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
		if !e.suppressSiblings && node.NextSibling != nil {
			stack = append(stack, stackEntry{node: node.NextSibling, depth: depth})
		}

		if skipRoles[role] {
			continue
		}

		if transparentRoles[role] && name == "" && !isSynthetic {
			// collapse: promote children to same depth (not depth+1)
			if node.FirstChild != nil {
				stack = append(stack, stackEntry{node: node.FirstChild, depth: depth, suppressSiblings: e.suppressSiblings})
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

		if node.FirstChild == nil {
			continue
		}

		// cap SYNTHETIC_LIST direct children
		if role == "SYNTHETIC_LIST" && opts.MaxListItems > 0 {
			total := 0
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				total++
			}
			if total > opts.MaxListItems {
				remaining := total - opts.MaxListItems
				// push truncation sentinel first (LIFO → printed after all kept children)
				stack = append(stack, stackEntry{depth: depth + 1, truncMsg: fmt.Sprintf("... (%d more items)", remaining)})
				// push first MaxListItems children in reverse order with suppressSiblings=true
				children := make([]*structure.Structure, 0, opts.MaxListItems)
				c := node.FirstChild
				for i := 0; i < opts.MaxListItems; i++ {
					children = append(children, c)
					c = c.NextSibling
				}
				for i := len(children) - 1; i >= 0; i-- {
					stack = append(stack, stackEntry{node: children[i], depth: depth + 1, suppressSiblings: true})
				}
				continue
			}
		}

		stack = append(stack, stackEntry{node: node.FirstChild, depth: depth + 1})
	}

	if *count >= opts.MaxLines {
		fmt.Fprintf(sb, "... (truncated at %d lines)\n", opts.MaxLines)
	}
}
