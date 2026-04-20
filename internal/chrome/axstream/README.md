The `axstream` package is the layer that:

1. Takes in CDP accessibility events.
    1. `Accessibility.loadComplete` (when the page has finished
       loading, also indicates page navigation)
    2. `Accessibility.nodesUpdated` (when subscribed nodes have
       updated attributes or children)
2. Subscribes to all new nodes discovered recursively by calling
   CDP API.
3. Transforms raw `cdp.AXNode` into `cdp.AXNodeWithRelatives` and
   outputs it to consumer as a stream.

# Node subscription

CDP API will not inform you of any AX node updates on the page
unless you have "touched" it with any API that fetches information
about the node. (ex. `cdp.GetChildAXNodes`)

> [!NOTE]
> Calling `cdp.GetFullAXTree` only counts as "touching" the root
> node. Updates to any nodes in the subtree (which often includes
> the root, but not always) will not be propagated.

Therefore, we recursively fetch the entire tree by calling
`cdp.GetChildAXNodes` to ensure all nodes will eventually be
"touched", and thus "subscribed".

When a node has been subscribed, it will propagate updates via
`Accessibility.nodesUpdated` events.

# Parallelism

We utilize some parallelism to reduce the latency: browser AX tree
loads <-> exposed to the consumer (downstream algos).

The main bottleneck is calling `cdp.GetChildAXNodes` on *every
single AX node*.

Thus in the `listener.go` file, we have:

```
   1 event worker   <->   bundled sub request   ->   N subscriber workers <--
         |                          ^                         |  |          |
         |                          |-------------------------|  |----------|
         |
         v
output axstream.Event
```

- "event worker" processes CDP events (`loadComplete` and
  `nodesUpdated`)
- "subscriber workers" sends CDP API requests
    - subscriber workers also spawn subscription jobs themselves
- "bundled sub(scriber) requests" (`syncx/bundled_requests.go`)
  allows the "event worker" to wait until all the subtree requests
  it sends to the subscriber workers finishes, then it transforms
  the tree and sends to output.

