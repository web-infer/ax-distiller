package tree

import (
	"fmt"
	"io"

	"github.com/xlab/treeprint"
)

type DebugInfo struct {
	Name     any
	Metadata any
	Ignored  bool
}

type Relatives struct {
	FirstChild  Node
	NextSibling Node
}

type Node interface {
	Debug() DebugInfo
	Relatives() Relatives
}

func printCycle(tree treeprint.Tree, cur, end Node, count int) {
	if cur == nil {
		return
	}

	if cur == end {
		if count == 1 {
			tree.AddBranch("<CYCLE>")
			return
		}
		count++
	}

	var children treeprint.Tree
	info := cur.Debug()
	if info.Metadata != nil {
		children = tree.AddMetaBranch(info.Metadata, info.Name)
	} else {
		children = tree.AddBranch(info.Name)
	}

	rel := cur.Relatives()
	printCycle(children, rel.FirstChild, end, count)
	printCycle(tree, rel.NextSibling, end, count)
}

func detectCyclesInner(seen []Node, node Node) (err error) {
	if node == nil {
		return
	}

	var existing Node
	for _, other := range seen {
		if other == node {
			existing = other
			break
		}
	}
	if existing != nil {
		tp := treeprint.New()
		printCycle(tp, existing, node, 0)
		err = fmt.Errorf("found cycle: %v", tp.String())
		return
	}

	seen = append(seen, node)
	rel := node.Relatives()
	err = detectCyclesInner(seen, rel.FirstChild)
	if err != nil {
		return
	}
	err = detectCyclesInner(seen, rel.NextSibling)
	return
}

func DetectCycles(root Node) (err error) {
	err = detectCyclesInner(nil, root)
	return
}

func printInner(node Node, tree treeprint.Tree) {
	if node == nil {
		return
	}

	debug := node.Debug()
	relatives := node.Relatives()

	if !debug.Ignored {
		var children treeprint.Tree
		if debug.Metadata != nil {
			children = tree.AddMetaBranch(debug.Metadata, debug.Name)
		} else {
			children = tree.AddBranch(debug.Name)
		}
		printInner(relatives.FirstChild, children)
		printInner(relatives.NextSibling, tree)
		return
	}

	printInner(relatives.FirstChild, tree)
	printInner(relatives.NextSibling, tree)
}

func Print(node Node) string {
	if node == nil {
		return "<nil>"
	}
	err := DetectCycles(node)
	if err != nil {
		panic(err)
	}
	tree := treeprint.New()
	printInner(node, tree)
	return tree.String()
}

func printSExprInner(node Node, out io.Writer) {
	if node == nil {
		return
	}
	out.Write([]byte("("))
	debug := node.Debug()
	fmt.Fprint(out, debug.Name)
	rel := node.Relatives()
	if rel.FirstChild != nil {
		out.Write([]byte(" "))
	}
	printSExprInner(rel.FirstChild, out)
	out.Write([]byte(")"))
	if rel.NextSibling != nil {
		out.Write([]byte(" "))
	}
	printSExprInner(rel.NextSibling, out)
}

func PrintSExpr(node Node, out io.Writer) {
	if node == nil {
		out.Write([]byte("<nil>"))
		return
	}
	err := DetectCycles(node)
	if err != nil {
		panic(err)
	}
	printSExprInner(node, out)
}
