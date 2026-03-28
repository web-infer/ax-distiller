Structures are a pattern/abstraction over the DOM.

A structure is a subtree that (same exact AX roles) with
potentially variable repeating elements that is found in one or
more places in the same DOM or in different DOM states.

Consider it as analogous to a struct definition.

Like structs, it may be useful to create abstractions over structs
like: interfaces, discriminated unions, etc... Though we will not
bother for now.

When considering the "path" for a structure, since structures are
nested hierarchically, the user can simply be given the "root
structure" and have their own logic for traversing / consuming the
structure.

The point is not to reinvent the DOM, but to guarantee certain
properties so your scraping code isn't brittle: If you expect a
particular structure hash and you are given an instance with that
structure hash, you have *hard guarantees* of the properties of
that subtree, that is:

- Certain nodes must exist, and must be in certain orders with
  certain structural hashes.
    - Ex. If I have a hash that guarantees I have a header, body,
      and footer, I'd better expect that given an instance of that
      hash, I would have a header, body and footer, and all their
      children.
- etc..

Then we can add in heuristics for creating "interfaces" of
structures to make development faster. That is, to find
"commonalities" between different structures (much like an
interface is to a struct) (This may be an "edit-distance" graph of
sorts? Structures will less differences (in terms of edit count)
should be closer together?)

It is much like using asserts in programming. By properly
"asserting" the structure of the DOM, we can ensure a high degree
of correctness in our behavior, and to clearly indicate where we
must add or correct logic.

