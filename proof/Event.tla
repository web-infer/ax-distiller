---- MODULE Event ----
EXTENDS Integers, Sequences, TLC

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
CONSTANT NEW_INSERT_MAX_SIZE
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

RECURSIVE NewStructure(_, _)

NewStructure(existing_nodes, depth) == LET
	possible_nodes == (POSSIBLE_IDS \ removed_nodes) \ DOMAIN existing_nodes
IN
	IF depth < NEW_INSERT_MAX_SIZE /\ possible_nodes /= {} THEN
		LET
			new_id == CHOOSE id \in possible_nodes : TRUE
			parent == CHOOSE id \in DOMAIN existing_nodes : TRUE
			new_nodes == FuncSetKey(
				[existing_nodes EXCEPT
					![parent] = [existing_nodes[parent] EXCEPT
						!.children = Append(existing_nodes[parent].children, new_id)
					]
				],
				new_id, [id |-> new_id, children |-> << >>]
			)
			rec == NewStructure(new_nodes, depth + 1)
		IN
			[nodes |-> rec.nodes, parent |-> parent]
	ELSE
		[nodes |-> existing_nodes, parent |-> -1]

\* node(s) inserted somewhere in the tree (excluding root)
NodeInsert == LET
	res == NewStructure(nodes, 0)
IN
	/\ res.parent \in POSSIBLE_IDS
	/\ nodes' = res.nodes
	/\ IF res.parent \in fetched THEN
			queued_events' = Append(queued_events, [
				parent |-> res.parent,
				children |-> res.nodes[res.parent].children
			])
		ELSE
			UNCHANGED queued_events
	/\ UNCHANGED removed_nodes
	/\ UNCHANGED queued_fetches
	/\ UNCHANGED fetched
	/\ UNCHANGED delete_count

\* node removed somewhere in the tree (excluding root)
NodeRemove ==
	/\ delete_count < MAX_DELETIONS
	/\ \E parent \in DOMAIN nodes : Len(nodes[parent].children) > 0 /\ LET
			target == CHOOSE n \in TupleSet(nodes[parent].children) : TRUE
			without_target == [k \in (DOMAIN nodes \ {target}) |-> nodes[k]]
		IN
			/\ nodes' = [without_target EXCEPT
				![parent] = [without_target[parent] EXCEPT
					!.children = SelectSeq(without_target[parent].children, LAMBDA x : x /= target)]]
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
	/\ queued_fetches' = queued_fetches \o SelectSeq(event.children, LAMBDA x : x \in fetched)
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

AllInsertedFetched ==
	DOMAIN nodes = fetched

NoMissingNodes ==
	DOMAIN nodes \cup removed_nodes = POSSIBLE_IDS

Spec ==
	/\ Init
	/\ [][Next]_vars
	/\ []NoMissingNodes
	/\ <>AllInsertedFetched

====
