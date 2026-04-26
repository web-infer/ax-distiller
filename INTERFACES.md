# Justification

Much like how it is useful to be able to match nodes with exactly
the same subtree structure, it is also useful to match different
website states with nearly the same structure.

In the real world, it is often unlikely that what we call "the
same structure" would actually have the same exact hash because
some nodes will have been added, moved around, and deleted.

# Tree-diffing

Therefore it may be useful to quantify exactly *how different* two
arbitrary structures are based on their subtree.

> [!EXAMPLE]
> Amazon product card 1, product card 2, etc...

In our case, we do not *strictly need* optimal string distance and
can do with approximations, especially when they are much faster
($O(n)$ for PQ-gram over $O(n^{3})$ for Zhang-Shasha).

# Delete-distance

We find the number of deletions necessary on both sides to
transform the sequences into the longest common subsequence.

This encodes:

1. Small # = very close together
2. Large # (max m + n) = completely different

The complexity of this would be: $O(nm)$.

We penalize "dual-deletion"? Deletion only on one side is less bad
than deletion on both sides?

The deletion of nodes could also be weighted by the number of
nodes in the subtree.

We could also exit early if the total "weighted edit count"
exceeds a certain amount.

## Segmentation

Even if we can find the "distance" between structures, how does
that lead into a binary choice of: part of the same "structure
union / interface" and not part of it?

# Weisfeiler-Lehman

A very popular [algorithm](https://ysig.github.io/GraKeL/0.1a7/kernels/weisfeiler_lehman.html) for tree embedding.

It can encode trees into a representation where any particular
structure and $h$ layers of children beneath it are factored into
the representation.

The total runtime is $O(hn)$ where $n$ is the number of nodes in
the tree.

The algorithm is essentially:

1. Replace each label with
   `hash(multiset(sorted(neighbor.label)))` (a multiset is
   effectively a finite set where they keep a count of the # of
   duplicates rather than throwing them away)
    - Essentially you are counting the number of times a
      particular label shows up in the children (neighbor, in the
      context of tree would just indicate children).
    - Then you are replacing the label with a hash of that
      histogram.
2. Do this again $h$ times.

You can see that on the second round, the neighbor's new labels
now encode information about their children histogram (that is,
different children's children will lead to a different label), and
the third round, the grandchildren's histograms, etc...

## Embedding

To turn this into a vector we should remember the goals of the
embedding:

- For similar structures, this vector should be close together.
- For different structures, this vector should be far apart.

We remember that each label in each iteration:

- Is a $\mathbb{N}$ which is a deterministic result of its
  dependencies.
- For an iteration $h$, encodes a unique value for the exact
  distribution of roles that shows up in the node's subtree $h$
  layers down.

For demonstration purposes, we will use a vector with "infinite
dimensions" (effectively a hash map with an int key).

We can take an example:

1. Two amazon product cards, one with a discount and one without
   it.
2. The only difference is a discount that is (let's say) 2 layers
   down from the root.
3. We will compute the labels initially (which are just the
   roles themselves hashed into a number).
    - In this round, the label hashes
4. We will then compute the labels based on their sorted children
   and previous labels.
5. We will do that again.

> [!NOTE]
> In practice, you can make this vector fixed size by modding the
> key by the fixed size. This will have the effect of making
> certain different structures appear to be the same.
>
> This is often not a big issue in practice you expect to only
> differentiate at most a fixed # of structures on a given page.
> (that is, if you have a good hash function that will distribute
> hashes approximately randomly)

