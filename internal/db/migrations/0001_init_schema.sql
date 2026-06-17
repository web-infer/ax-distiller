-- +goose Up

create table structure (
	hash blob check (length(hash) = 8) primary key,
	first_child blob check (length(first_child) = 8),
	next_sibling blob check (length(next_sibling) = 8)
);

create table path (
	hash blob check (length(hash) = 8),
	structure blob check (length(structure) = 8) not null
		references structure (hash)
			on update cascade
			on delete cascade,
	parent blob check (length(parent) = 8)
		references path (hash)
			on update cascade
			on delete cascade,
	primary key (hash, structure)
);

