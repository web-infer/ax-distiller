# Structures

A `Structure` is a compressed, hash-identified subtree derived from the accessibility tree.

## What a Structure Is

A structure captures the *shape* of a DOM subtree — the roles of nodes and how they nest — independent of text content. Two subtrees with identical shapes produce the same `Hash`.

```go
type Structure struct {
    Hash        uint64      // xxh3 of role + children hashes
    Underlying  *cdp.AXNode // the original AX node (or a synthetic one)
    FirstChild  *Structure  // left-child in linked tree
    NextSibling *Structure  // right-sibling in linked tree
}
```

## Hash Computation

```
Hash(node) = xxh3( role_bytes || Hash(child_1) || Hash(child_2) || ... )
```

Hashes are structural fingerprints. Same hash = same shape, regardless of position or text.

## Compression Passes

Raw children lists are compressed before hashing via two passes run in a loop until stable:

### Pass 1 — `deleteAdjacent`

Collapses runs of identical siblings into a `SYNTHETIC_LIST` wrapper.

```
[A, A, A, B] → [LIST(A), B]
```

### Pass 2 — `slideWindow`

Finds the most frequent (largest, then most-repeated) multi-node pattern and wraps all instances.

```
[A, B, A, B, C] → [OBJ(A,B), OBJ(A,B), C]
```

Both passes use synthetic wrapper nodes:
- `SYNTHETIC_LIST` — repeated identical adjacent siblings
- `SYNTHETIC_OBJECT` — repeated multi-node pattern

## Ignored Nodes

Nodes with `Ignored: true` are transparent — structure computation skips them and treats their non-ignored descendants as direct children of the nearest non-ignored ancestor. See `Persistent.shallowIterNonIgnoredDescendents`.

## Guarantees

Given a structure hash:
- Every instance with that hash has the same role tree shape.
- Child count, child roles, and child hashes are identical across all instances.
- Text content and DOM position are NOT encoded — they vary freely.

This makes structure hashes stable across re-renders that don't change shape.
