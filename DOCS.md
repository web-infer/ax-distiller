Couple layers to this application:

1. CDP: Low-level CDP communication
2. Browser layer: Abstracts over CDP, provides:
    - Tab/navigation control
    - AX tree (as a stream of tree mutations)
    - TODO: Interaction control
3. Post-processing:
    - Filters ignored nodes via custom logic.
    - Does simple cleaning.
    - Operates on the stream of tree mutations.
    - Should not modify types.
4. Structural analyzer: (copying/cloning occurs here)
    - Should take in tree mutations
    - Runs algorithms over the AX tree incrementally
5. Structure collector:
    - Should take in the stream of mutations and convert it into a
      persistent data structure.
    - Should be able to query both current and past structures.
    - Use a database here!

# Tree mutation stream

Often only a part of the DOM tree changes over time. There is no
need to rescan the whole tree every time and incur expensive
memory allocations.

We view the browser as exposing a stream of tree mutations.
Particularly events of the type:

```go
type MutationType uint8

const (
    MUTATION_ADD MutationType = iota
    MUTATION_DELETE
    MUTATION_REPLACE
)

type TreeMutation struct {
    ParentID int64
    Type MutationType
    Nodes []AXNode
}
```

We really only have add/delete subtree events. A refresh of the
whole tree can simply be treated as a replace subtree with the
whole tree as the payload.

The ParentID defines the parent ID whose children was mutated (if
`-1` then it is the whole tree must be replaced), the whole parent
needs to be replaced if its children change.

Nodes would be the nodes of the parent's subtree after the change.

# Structure API

The final goal is to be able to automatically retrieve information
and interact with the browser according to derived structures.

[Structures](./STRUCTURES.md) are effectively struct definitions.

The program should expose the root struct definition of the
current website (or a particular tab). It should also expose the
possible known struct definitions.

There is a library of built-in "methods" that operate on
particular structures.

Ex.

1. Read text: reads the text under any structure.
    - May also include functionality to do so recursively,
      handling whitespace according to browser behavior.
2. Read image: obtains the image under img/imgset/picture
structures.
3. Click: simulates a click on either anchor tags with href or any
tag with an onclick/onmouse/ontouch handler
4. TextInput: sets the value of a \<input type="text"\>
    -. etc... other form inputs
5. Scroll: attempts to scroll on structure.
6. etc...

## Navigations API

Continuing with the programming analogy, we can think of "methods"
on structures as analogous to "methods" on objects, with
well-defined argument types and return types.

Methods also have "pre-conditions" and "post-conditions".
Likewise, we can use a similar paradigm for website methods.

> [!EXAMPLE]
> Starting at structure A, running 'click' on structure A.B
> results in structure B"

