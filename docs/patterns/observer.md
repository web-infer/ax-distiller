# Observer Pattern

## Intent

Define a one-to-many dependency so that when a subject changes state, all dependents are notified and updated automatically.

## Where Used

`internal/chrome/axstream` is the subject (event producer). `internal/structure.Persistent` is the observer (event consumer).

## Structure

```
  Subject                        Observer
  ───────                        ────────
  axstream.Listen()  ──chan──→   Persistent.HandleEvent(e Event)
  (produces Events)              (reacts to Events)
```

## Subject — axstream

`axstream.Listen` returns a read-only `chan Event`. The subject fires two event types:

```go
const (
    EVENT_RESET EventType = iota  // full tree replacement
    EVENT_PATCH                   // incremental update
)

type Event struct {
    Type    EventType
    Added   []*cdp.AXNodeWithRelatives
    Updated []*cdp.AXNodeWithRelatives
}
```

Internally, `listener` buffers and coalesces raw CDP events before emitting onto the channel. This decouples Chrome's raw event rate from the observer's processing rate.

## Observer — Persistent

```go
func (p *Persistent) HandleEvent(e axstream.Event) {
    switch e.Type {
    case axstream.EVENT_RESET:
        // rebuild entire tree
    case axstream.EVENT_PATCH:
        // recompute only changed subtrees
    }
}
```

`Persistent` does not subscribe/unsubscribe dynamically — the channel is the subscription. Callers wire the two together in `cmd/`:

```go
events, _ := axstream.Listen(ctx, logger, page)
persistent := structure.NewPersistent(logger)
for e := range events {
    persistent.HandleEvent(e)
}
```

## Go Channel as Observer Bus

Go channels provide the observer contract without an explicit `Subscribe`/`Notify` API:

| Classic Observer | Go equivalent |
|-----------------|---------------|
| `Subject.Subscribe(observer)` | pass `chan Event` to consumer |
| `Subject.Notify()` | send on channel |
| `Observer.Update(event)` | receive from channel |

The buffer size (`out_channel_buffer_size`) decouples producer and consumer timing.
