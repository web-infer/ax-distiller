We may revisit the idea of tree "commonality" with new algorithms
to avoid intractable computational complexity.

The idea is to hash a node's "path" from the root. (along with its
duplicate index if there are more than one)

That way, to find the "common nodes" between two trees, all one
has to do is set intersection, this can scale linearly with $n$
trees.

But often times, we may not even need *all common nodes* for a
single tree, it may be enough to simply check which other nodes
also have the same "path hash".

"Path hash" is also resilient to mutations to siblings as the part
that encodes its index depends on its structure hash, which is
dependent on all its children. Therefore, the only case where
ambiguity may arise is if another identical (down to the entire
subtree) sibling comes before it.

What we can do is that while indexing a tree's structural hashes,
we can also index every structure's path hash.

# Anchoring

It would be ideal to use hash path and structural hash to cover
each of their flaws. However, I can only see this working with
nodes near the "middle" of the tree (even then, it may get
confused).

Often is the case, the hash of the root and similar "kitchen-sink"
level containers is not very useful.

And for nodes at the bottom of the tree, one can simply start with
intermediate nodes, narrowing the range, until we reach the bottom
nodes we are interesting in (much like a selector).

