import { Schema as S } from "effect";

export const StructInfo = S.Struct({
	id: S.BigInt,
	ax_id: S.Option(S.String),
	role: S.String,
	structure_hash: S.BigInt,
	path_hash: S.BigInt,
	instances: S.Number,
	highlight: S.Array(S.BigInt),
	children: S.Array(S.BigInt),
});
