# Strategy Pattern

## Intent

Define a family of algorithms, encapsulate each one, and make them interchangeable. Lets the algorithm vary independently from clients that use it.

## Where Used

`internal/structure/structure.go` — `deleteAdjacent` and `slideWindow` are two compression strategies applied in sequence by `Construct` and `Persistent.recomputeNodeStructure`.

## Strategies

Both strategies take a `*Structure` linked list (sibling chain) and return a compressed `*Structure` linked list. They share the same signature:

```go
func deleteAdjacent(start *Structure) (ret *Structure)
func slideWindow(start *Structure) (ret *Structure, replaced bool)
```

### Strategy 1 — `deleteAdjacent`

Collapses adjacent identical siblings into a `SYNTHETIC_LIST` wrapper.

```
Input:  [A, A, A, B, C, C]
Output: [LIST(A,A,A), B, LIST(C,C)]
```

Single-pass, O(n). Always produces output (no `replaced` flag needed — the result may be identical to input only if no adjacencies exist, which is harmless).

### Strategy 2 — `slideWindow`

Finds the most frequent multi-node repeating pattern (preferring larger patterns on tie) and wraps all non-overlapping instances in `SYNTHETIC_OBJECT`.

```
Input:  [A, B, A, B, C]
Output: [OBJ(A,B), OBJ(A,B), C]
```

Two-pass: first scans all windows to build a frequency dict, then rewrites. Returns `replaced=false` when no pattern with ≥2 instances exists, signalling the loop to stop.

## Context — `Construct`

The context that drives the strategies:

```go
for {
    ret = deleteAdjacent(ret)      // strategy 1

    var replaced bool
    ret, replaced = slideWindow(ret)  // strategy 2

    if !replaced {
        break   // fixed point reached
    }
}
```

The loop runs until `slideWindow` reports no further compression is possible. `deleteAdjacent` runs every iteration because a `slideWindow` pass can create new adjacent identicals.

## Extensibility

Additional compression strategies (e.g. interleaved-pattern detection) can be inserted into the loop with no changes to callers. Each strategy is self-contained and operates on the same `*Structure` linked-list type.
