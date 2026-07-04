create table structure_instance (
	-- id == 1 is treated as the root structure instance
	id integer primary key autoincrement,
	ax_id text unique,
	role text,
	shash blob not null check (length(shash) = 8),
	phash blob not null check (length(phash) = 8),
	parent integer references structure_instance (id)
		on update cascade
		on delete cascade
);

-- no index needs to be created for structure_instance.ax_id since
-- it is created by default with the `unique` constraint

create index idx_structure_instance_shash
on structure_instance (shash);

create index idx_structure_instance_phash
on structure_instance (phash);

create index idx_structure_instance_parent
on structure_instance (parent);
