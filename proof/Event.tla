---- MODULE Event ----
EXTENDS Integers, Sequences, TLC, FiniteSets

(*
algo:
- if given change event:
	- search changed nodes for new children
	- for each child:
		- subscribe
		- if failure: we should expect change event for this node
		- if success: we may see change event for this node
- if given root:
	- for each child:
		- subscribe
		- if failure: we should expect change event for root
			- return
		- if success: we may see change event for root
			- recurse with child

algo is considered correct if: eventually, all inserted (non-deleted) nodes
will have been fetched
*)

CONSTANT POSSIBLE_IDS
CONSTANT MAX_DELETIONS

\* fn[node_id, record[id: node_id, children: seq[node_id]]]
VARIABLE nodes
\* set[node_id]
VARIABLE removed_nodes
\* set[node_id]
VARIABLE fetched
\* seq[record[parent: node_id, children: seq[node_id]]]
VARIABLE queued_events
\* seq[node_id]
VARIABLE queued_fetches
\* int
VARIABLE delete_count

vars == <<
	nodes,
	removed_nodes,
	fetched,
	queued_events,
	queued_fetches,
	delete_count
>>

Init ==
	/\ nodes = LET
			\* CHOOSE is used here because it doesn't really matter what ID
			\* value we set the root to
			root == CHOOSE r \in POSSIBLE_IDS : TRUE
		IN [k \in {root} |-> [id |-> k, children |-> << >>]]
	/\ fetched = {}
	/\ removed_nodes = {}
	/\ queued_events = << >>
	/\ queued_fetches = << >>
	/\ delete_count = 0

TupleSet(tuple) ==
	{tuple[i] : i \in DOMAIN tuple}

FuncSetKey(fn, key, value) ==
	[k \in DOMAIN fn \cup {key} |-> IF k = key THEN value ELSE fn[k]]

\* node(s) inserted somewhere in the tree (excluding root)
NodeInsert ==
	/\ \E new_id \in ((POSSIBLE_IDS \ removed_nodes) \ DOMAIN nodes) :
		\E parent_id \in DOMAIN nodes : LET
			new_parent == [nodes[parent_id] EXCEPT
				!.children = Append(@, new_id)
			]
		IN
			nodes' = FuncSetKey(
				[nodes EXCEPT ![parent_id] = new_parent],
				new_id, [id |-> new_id, children |-> << >>]
			) /\
			queued_events' = Append(queued_events, [
				parent |-> parent_id,
				children |-> new_parent.children
			])
	/\ UNCHANGED removed_nodes
	/\ UNCHANGED queued_fetches
	/\ UNCHANGED fetched
	/\ UNCHANGED delete_count

(*
1. we remove the current target from the function list
2. we remove the current target from the parent's children
3. we recursively call nodeRemoveInner on each of the children of the target
4. we return the new
*)

RECURSIVE removeNodeAndChildren(_, _, _)

removeNodeAndChildren(nodelist, siblings, idx) == IF idx <= Len(siblings) THEN
	LET
		target == siblings[idx]
		withNextSiblingsRemoved == removeNodeAndChildren(nodelist, siblings, idx + 1)
		withChildrenRemoved == LET
			targetChildren == nodelist[target].children
		IN
			IF Len(targetChildren) > 0 THEN
				removeNodeAndChildren(withNextSiblingsRemoved, targetChildren, 1)
			ELSE
				nodelist
		withTargetRemoved == [
			k \in (DOMAIN withChildrenRemoved \ {target}) |-> withChildrenRemoved[k]
		]
	IN
		withTargetRemoved
ELSE
	nodelist

\* node removed somewhere in the tree (excluding root)
NodeRemove ==
	/\ delete_count < MAX_DELETIONS
	/\ \E parent \in DOMAIN nodes :
		/\ Len(nodes[parent].children) > 0
		/\ \E target \in TupleSet(nodes[parent].children) :
			/\ nodes' = [
					removeNodeAndChildren(nodes, << target >>, 1) EXCEPT
					![parent] = [@ EXCEPT !.children = SelectSeq(@, LAMBDA child : child /= target)]
				]
			/\ queued_events' = IF parent \in fetched THEN Append(queued_events, parent) ELSE queued_events
			/\ removed_nodes' = removed_nodes \cup {target}
			/\ delete_count' = delete_count + 1
			/\ UNCHANGED queued_fetches
			/\ UNCHANGED fetched

\* event being received and handled (ax-distiller side)
\*
\* handling logic: immediately fetch changed node's children that are not
\* already fetched
RecvEvent == Len(queued_events) > 0 /\ LET
	event == Head(queued_events)
IN
	/\ queued_events' = Tail(queued_events)
	/\ queued_fetches' = queued_fetches \o SelectSeq(event.children, LAMBDA x : ~(x \in fetched))
	/\ UNCHANGED nodes
	/\ UNCHANGED fetched
	/\ UNCHANGED removed_nodes
	/\ UNCHANGED delete_count

\* fetch request received and handled (browser-side)
BrowserHandleFetch == Len(queued_fetches) > 0 /\ LET
	node_id == Head(queued_fetches)
	exists == node_id \in DOMAIN nodes
IN
	/\ fetched' = IF exists THEN fetched \cup {node_id} ELSE fetched
	/\ queued_fetches' = Tail(queued_fetches)
	/\ UNCHANGED nodes
	/\ UNCHANGED removed_nodes
	/\ UNCHANGED queued_events
	/\ UNCHANGED delete_count

Next ==
	\/ NodeInsert
	\/ NodeRemove
	\/ RecvEvent
	\/ BrowserHandleFetch
	\/ UNCHANGED vars

PropNoOrphansOutsideRoot ==
	[](Cardinality({ child \in DOMAIN nodes :
		~\E parent \in DOMAIN nodes :
		child \in TupleSet(nodes[parent].children)
	}) = 1)

PropAllNodesUsed ==
	<>(DOMAIN nodes \cup removed_nodes = POSSIBLE_IDS)

PropAllInsertedFetched ==
	<>(DOMAIN nodes = fetched)

Spec ==
	/\ Init
	/\ [][Next]_vars
	/\ SF_vars(Next) \* strong-fairness, Next must occur infinitely if it is enabled infinitely

====
