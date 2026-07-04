-- name: ListStructInstBySHash :many
select * from structure_instance
where shash = ?;

-- name: ListStructInstByPHash :many
select * from structure_instance
where phash = ?;

-- name: ListStructSHash :many
select shash from structure_instance
group by shash;

-- name: ListStructPHash :many
select phash from structure_instance
group by phash;

-- name: GetStruct :one
select * from structure_instance
where id = ?;

-- name: GetStructByAXID :one
select * from structure_instance
where ax_id = ?;

-- name: UpdateStruct :exec
update structure_instance
set ax_id = ?,
	role = ?,
	shash = ?,
	phash = ?,
	parent = ?
where id = ?;

-- TODO: remove later
-- -- name: PruneOrphanedInstances :exec
-- --
-- -- This removes structure instances that are not the root
-- delete from structure_instance
-- where id != 1 and parent is null;

-- TODO: remove later
-- -- name: GetAncestors :many
-- with recursive ancestor_cte(id, parent) as (
-- 	select id, parent from structure_instance
-- 	where id = ? limit 1
--
-- 	union all
--
-- 	-- 1. this query takes in the previous rows produced as the value of
-- 	--    `latest_ancestor`
-- 	-- 2. since the anchor query will produce only 1 row
-- 	-- 3. and for a single row as input for `latest_ancestor`, only 1 or 0
-- 	--    parents can be produced
-- 	-- 4. this recursive expression will return a list of ancestors from the
-- 	--    given anchor
-- 	select s.id, s.parent from structure_instance s
-- 	inner join latest_ancestor a
-- 		on s.id = a.parent
-- )
-- select id from ancestor_cte;

-- name: CreateStructureInstance :one
insert into structure_instance (ax_id, role, shash, phash, parent)
values (?, ?, ?, ?, ?)
returning id;

-- TODO: remove later
-- -- name: DeleteInstancesByParent :exec
-- delete from structure_instance
-- where parent = ?;

-- name: DeleteInstance :exec
delete from structure_instance
where id = ?;

-- name: UpdateStructureInstanceSHash :exec
update structure_instance
set shash = ?
where id = ?;

-- name: ListChildren :many
select * from structure_instance
where parent = ?;

