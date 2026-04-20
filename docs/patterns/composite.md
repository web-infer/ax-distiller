# Composite Pattern

## Intent

Compose objects into tree structures and treat individual objects and compositions uniformly. Clients traverse the tree through a single interface without knowing whether a node is a leaf or a container.

## Where Used

`internal/tree` defines the component interface. `internal/structure` and `internal/chrome/cdp` provide concrete implementations.

## Structure

```
      «interface»
       tree.Node
      /          \
cdp.AXNodeWithRelatives   structure.Structure
   (raw AX tree)             (compressed hash-tree)
```

### Component Interface (`internal/tree/tree.go`)

```go
type Node interface {
    Debug() DebugInfo      // name + metadata for printing
    Relatives() Relatives  // FirstChild + NextSibling
}

type Relatives struct {
    FirstChild  Node
    NextSibling Node
}
```

`tree.Print(Node)` and `tree.DetectCycles(Node)` operate on any `Node` — they do not know whether the concrete type is an AX node or a Structure.

### Composite Node (`internal/structure/structure.go`)

```go
type Structure struct {
    Hash        uint64
    Underlying  *cdp.AXNode
    FirstChild  *Structure   // child subtree (composite link)
    NextSibling *Structure   // sibling (composite link)
}
```

`Structure` is both leaf and composite: a node with no `FirstChild` is a leaf; one with children is a branch. The caller never distinguishes.

## Navigation

The tree uses **left-child / right-sibling** representation. A node's children are a linked list starting at `FirstChild`, chained by `NextSibling`.

```
        root
       /
    child1 → child2 → child3
    /
  grandchild1 → grandchild2
```

## Key Operations

| Operation | Location | Description |
|-----------|----------|-------------|
| `tree.Print` | `internal/tree/tree.go:109` | Recursive DFS print via `Node` interface |
| `tree.DetectCycles` | `internal/tree/tree.go:80` | Cycle check via `Node` interface |
| `structure.Construct` | `internal/structure/structure.go:306` | Build composite Structure tree from AX nodes |
| `Persistent.recomputeNodeStructure` | `internal/structure/persistent.go:92` | Incrementally rebuild composite subtree |

## Synthetic Wrappers

The compression passes (`deleteAdjacent`, `slideWindow`) introduce synthetic internal nodes — `SYNTHETIC_LIST` and `SYNTHETIC_OBJECT` — that act as composites over repeated children. These have no backing DOM node but participate in the tree uniformly as `Structure` values.
