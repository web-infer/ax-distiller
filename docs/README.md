# ax-distiller Documentation

Architecture and design pattern reference for ax-distiller.

## Layers

```
┌─────────────────────────────────────┐
│           cmd/ (entry points)        │
├─────────────────────────────────────┤
│    internal/structure (Persistent)   │  ← structural analysis
├─────────────────────────────────────┤
│    internal/chrome/axstream          │  ← AX event stream
├─────────────────────────────────────┤
│    internal/chrome/cdp               │  ← raw CDP protocol
├─────────────────────────────────────┤
│    Chrome DevTools Protocol (remote) │
└─────────────────────────────────────┘
```

Each layer exposes a narrower, higher-level interface to the layer above.

## Design Patterns

| Pattern | Location | Doc |
|---------|----------|-----|
| [Composite](patterns/composite.md) | `internal/tree`, `internal/structure` | Tree nodes that contain trees |
| [Observer](patterns/observer.md) | `internal/chrome/axstream`, `internal/structure` | Event channel + handler |
| [Iterator](patterns/iterator.md) | `internal/structure/persistent.go` | `iter.Seq` over non-ignored nodes |
| [Strategy](patterns/strategy.md) | `internal/structure/structure.go` | Interchangeable compression passes |
| [Facade](patterns/facade.md) | `internal/chrome/axstream/axstream.go` | Single-call CDP abstraction |

## Key Data Flow

```
Chrome (CDP events)
  → axstream.Listen()       [Facade]
  → chan axstream.Event      [Observer — producer side]
  → Persistent.HandleEvent() [Observer — consumer side]
  → structure.Construct()    [Composite + Strategy]
  → *Structure (tree)        [Composite]
```

## Further Reading

- [Architecture overview](architecture.md)
- [Structure and hashing](structures.md)
