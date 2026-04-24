# Architecture

## Layer Responsibilities

### 1. CDP (`internal/chrome/cdp`)

Raw Chrome DevTools Protocol. Handles JSON marshalling, WebSocket framing, and type definitions for AX nodes (`AXNode`, `AXNodeWithRelatives`). No business logic.

### 2. axstream (`internal/chrome/axstream`)

Translates raw CDP events into a typed `Event` stream:

```go
type Event struct {
    Type    EventType               // EVENT_RESET | EVENT_PATCH
    Added   []*cdp.AXNodeWithRelatives
    Updated []*cdp.AXNodeWithRelatives
}
```

Hides listener lifecycle, reconnect logic, and CDP enable/disable calls behind `axstream.Listen(ctx, logger, page)`.

### 3. heuristic (`internal/chrome/heuristic`)

Goal-aware AX tree simplification. Pure functions, no LLM:

- `Summarize(root *structure.Structure) string` — 2-level section overview for the pruning agent prompt
- `PruneByIDs(root *structure.Structure, ids map[int64]bool) *structure.Structure` — returns a pruned copy of the tree with specified subtrees removed; original untouched

### 4. structure (`internal/structure`)

Two responsibilities:

- `Construct(*cdp.AXNodeWithRelatives) *Structure` — builds a compressed structural hash-tree from a raw AX subtree (stateless, pure).
- `Persistent` — maintains a live, incrementally updated map of `NodeID → *Structure`, consuming `axstream.Event`s.

### 5. cmd/ (entry points)

Demo and test binaries. Wire the layers together with concrete loggers and browser connections.

## Concurrency Model

`axstream` runs two goroutines internally (event worker + subscriber worker). The output `chan Event` is buffered. Consumers (`Persistent.HandleEvent`) are called synchronously from the reader goroutine — no internal locking needed in `Persistent`.

## Incremental Update Strategy

On `EVENT_RESET`: wipe state, recompute full tree.

On `EVENT_PATCH`:
1. Recompute structure for each added/updated node into a scratch `recomputed` map.
2. Call `reconcileRecomputed()` — diff previous children against new children, delete dropped nodes from `state`, then promote `recomputed` into `state`.

This avoids full-tree re-traversal for small DOM mutations.
