# Facade Pattern

## Intent

Provide a simplified interface to a complex subsystem. The facade hides subsystem complexity and reduces coupling between clients and internals.

## Where Used

`internal/chrome/axstream/axstream.go` — `axstream.Listen` is a facade over the CDP accessibility event subsystem.

## What It Hides

Without the facade, a caller would need to:

1. Enable the Accessibility domain via CDP (`AccessibilityEnable`)
2. Create and manage a `listener` goroutine
3. Wire up the event worker (`listener_event_worker.go`)
4. Wire up the subscriber worker (`listener_subscriber_worker.go`)
5. Handle channel buffering and lifecycle
6. Manage context cancellation across goroutines

### The Facade

```go
func Listen(ctx context.Context, logger *slog.Logger, page *rod.Page) (out <-chan Event, err error) {
    err = cdp.CommandUnary(ctx, page, proto.AccessibilityEnable{})
    if err != nil {
        return
    }
    events := make(chan Event, out_channel_buffer_size)
    out = events
    newListener(ctx, logger, events, page)
    return
}
```

Caller receives one channel. All internal complexity is invisible.

## Client Usage

```go
events, err := axstream.Listen(ctx, logger, page)
if err != nil { ... }
for e := range events {
    persistent.HandleEvent(e)
}
```

The client knows nothing about CDP domains, goroutine workers, or channel wiring.

## Subsystem Components Hidden

| File | Responsibility |
|------|---------------|
| `listener.go` | Goroutine lifecycle and state |
| `listener_event_worker.go` | Consume raw CDP events, coalesce into `Event` |
| `listener_subscriber_worker.go` | Manage downstream subscribers |
| `listener_state.go` | Shared mutable state between workers |
| `cdp/ax.go`, `cdp/dom.go` | Low-level CDP node types and commands |
