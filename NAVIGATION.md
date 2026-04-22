# Justification

Much like how it is useful to be able to match nodes with exactly
the same subtree structure, it is also useful to match different
website states with nearly the same structure.

In the real world, it is often extremely unlikely that what we
call "the same structure" would actually have the same exact hash
because some nodes will have been added, moved around, and
deleted.

# Tree-diffing

Therefore it is useful to quantify exactly *how different* two
websites are, and in particular, what those differences are. This
could lead the way to later decision making.

In our case, we do not *strictly need* optimal string distance and
can do with approximations, especially when they are much faster
($O(n)$ for PQ-gram over $O(n^{3})$ for Zhang-Shasha).

# Later decision making

We can later infer "likely interfaces" from edit distance. Code
can be written to consume such interfaces.

