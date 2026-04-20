# Iterator Pattern

## Intent

Provide a way to sequentially access elements of a collection without exposing its underlying representation.

## Where Used

`internal/structure/persistent.go` — `shallowIterNonIgnoredDescendents` exposes a filtered traversal of the AX node tree as a Go `iter.Seq`.

## Implementation

```go
func (p *Persistent) shallowIterNonIgnoredDescendents(
    node *cdp.AXNodeWithRelatives,
) iter.Seq[*cdp.AXNodeWithRelatives]
```

Returns an iterator over the *non-ignored direct structural descendants* of `node` — skipping ignored nodes transparently and descending into their children to find the next non-ignored node.

### Traversal Logic

The iterator performs a shallow DFS: it stops descending as soon as it finds a non-ignored node, yielding it as a direct child. Ignored nodes are transparent containers.

```
root (non-ignored)
├── ignored-A
│   ├── non-ignored-B   ← yielded as direct child of root
│   └── non-ignored-C   ← yielded as direct child of root
└── non-ignored-D       ← yielded as direct child of root
```

### Usage

```go
for child := range p.shallowIterNonIgnoredDescendents(node) {
    childStruct := p.recomputeNodeStructure(child, state)
    // ...
}
```

The consumer (`recomputeNodeStructure`) sees a flat sequence of structural children and never reasons about the ignored-node layer.

## Why Iterator Here

The AX tree stores ignored nodes inline — they appear in the tree but must not affect structure hashing. The iterator encapsulates the "skip ignored, descend into their children" traversal rule in one place, keeping `recomputeNodeStructure` free of that concern.
