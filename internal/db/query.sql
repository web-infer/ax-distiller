-- name: UpsertStructure :exec
insert into structure (hash, first_child, next_sibling)
values (?, ?, ?) on conflict do update set
	first_child = excluded.first_child,
	next_sibling = excluded.next_sibling;

-- name: UpsertPath :exec
insert into path (hash, structure, parent)
values (?, ?, ?) on conflict do update set
	parent = excluded.parent;

-- name: ListPathsByHash :many
select * from path
where hash = ?;

-- name: GetStructure :one
select * from structure
where hash = ?
limit 1;

