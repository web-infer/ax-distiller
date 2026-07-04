package structure

import (
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/db"
	"ax-distiller/internal/structure/stdb"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"

	"github.com/zeebo/xxh3"
)

/*

some goals:

- more "modular" in the sense that there is less coupling, much less context
required to understand each component in isolation and the whole
- fix bugs
- work out "persist's" promises to the external world

contracts:

- handles ax stream event by method
- key observability possible:
	- arbitrary querying over structures tree

directly use SQLite DB for querying??

in-memory relation between structure instance and structure?

*/

type PersistV2 struct {
	logger     *slog.Logger
	dbDriver   *sql.DB
	stdbDriver *sql.DB
}

func NewPersistV2(logger *slog.Logger, dbDriver, stdbDriver *sql.DB) *PersistV2 {
	return &PersistV2{
		logger:     logger,
		dbDriver:   dbDriver,
		stdbDriver: stdbDriver,
	}
}

func (p *PersistV2) HandleEvent(ctx context.Context, e axstream.Event) (err error) {
	switch e.Type {
	case axstream.EVENT_RESET:
		root := e.Updated[0]
		st := p.initStructTree(root)
		err = p.pushDB(ctx, st)
		if err != nil {
			err = fmt.Errorf("push db: %w", err)
			return
		}
	case axstream.EVENT_PATCH:
		// parents of updated nodes only need to update their structure hashes
		// (to the root)

		for _, updated := range e.Updated {
			st := p.initStructTree(updated)
			err = p.pushDB(ctx, st)
			if err != nil {
				err = fmt.Errorf("push db (%d): %w", updated.Underlying.NodeID, err)
				return
			}
		}
	}

	return
}

// initStructTree runs through the steps of bringing an AX Node -> fully
// annotated structure
func (p *PersistV2) initStructTree(axroot *cdp.AXNodeWithRelatives) (out *StructureInstance) {
	out = p.makeStructTree(axroot)

	// these are independent of each other
	out.UpdateSHash()
	out.UpdatePHash()
	out.UpdateParentRecursively(nil)

	// this depends on structure hashes being present
	p.makeSynthetics(out)

	return
}

// makeStructTree maps an AX tree (child list style) -> structure tree (linked
// list style). It computes no "enriched" properties, those are handled with
// other functions inside initStructTree.
func (p *PersistV2) makeStructTree(axnode *cdp.AXNodeWithRelatives) (out *StructureInstance) {
	out = &StructureInstance{
		Underlying: axnode,
	}

	var prev *StructureInstance
	for childAXNode := range cdp.FilterDescendentsShallow(isNotIgnored, axnode) {
		// single child may return multiple children in linked list (via NextSibling)
		firstChildSt := p.makeStructTree(childAXNode)

		if prev == nil {
			// set first child to the first childStruct
			out.FirstChild = firstChildSt
		} else {
			// set final node of last child's NextSibling to first node of this child
			prev.NextSibling = firstChildSt
		}

		for childSt := firstChildSt; childSt != nil; childSt = childSt.NextSibling {
			if childSt.NextSibling == nil {
				// prev points to the last node of the child list returned
				prev = childSt
			}
		}
	}

	if out.NextSibling != nil {
		panic("assert failed: out.NextSibling != nil")
	}

	return
}

// makeSynthetics creates synthetic wrappers
func (p *PersistV2) makeSynthetics(st *StructureInstance) {
	p.makeSyntheticsInner(st)
	// p.updateSyntheticIDs(st)
}

func (p *PersistV2) makeSyntheticsInner(st *StructureInstance) {
	for child := st.FirstChild; child != nil; child = child.NextSibling {
		p.makeSyntheticsInner(child)
	}

	// we create synthetic structural wrappers for repeated nodes and patterns
	// in the children linked list
	for {
		// group repeated adjacent nodes into a wrapper
		st.FirstChild = deleteAdjacent(st.FirstChild)

		// identify most frequent (and among the most frequent the largest)
		// pattern and replace all instances of it with a wrapper
		var replaced bool
		st.FirstChild, replaced = slideWindow(st.FirstChild)

		// rinse and repeat until no patterns are found
		if !replaced {
			break
		}
	}
}

/*
TODO: remove this if "synthetic AX IDs" are not required because internal IDs
for each structure instance is good enough
func (p *PersistV2) updateSyntheticIDs(st *StructureInstance) {
	for child := st.FirstChild; child != nil; child = child.NextSibling {
		p.updateSyntheticIDs(child)
	}

	// we make synthetic node NodeID = the hash of its childrens' node ids
	//
	// this way we can accurately give each instance of synthetic node a unique ID
	switch st.Underlying.Underlying.Role.Value {
	case ROLE_SYNTHETIC_OBJECT, ROLE_SYNTHETIC_LIST:
		buf := getByteslice()
		for child := st.FirstChild; child != nil; child = child.NextSibling {
			buf = append(buf, []byte(child.Underlying.Underlying.NodeID)...)
		}
		hash := xxh3.Hash(buf)
		putByteslice(buf)
		st.Underlying.Underlying.Role.Value = fmt.Sprint(hash)
	}
}
*/

// pushDBState replaces a given subtree inside both the persistent
// (structure) and ephemeral (structure instance) database states
//
// it also updates all the ancestors of the given subtree root
func (p *PersistV2) pushDB(ctx context.Context, subtree *StructureInstance) (err error) {
	stdbtx, err := p.stdbDriver.BeginTx(ctx, nil)
	if err != nil {
		err = fmt.Errorf("begin structure db tx: %w", err)
		return
	}
	defer stdbtx.Rollback()

	dbtx, err := p.dbDriver.BeginTx(ctx, nil)
	if err != nil {
		err = fmt.Errorf("begin db tx: %w", err)
		return
	}
	defer dbtx.Rollback()

	// we only check fkey at end of TX
	_, err = stdbtx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON")
	if err != nil {
		err = fmt.Errorf("enable pragma defer_foreign_keys: %w", err)
		return
	}
	_, err = dbtx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON")
	if err != nil {
		err = fmt.Errorf("enable pragma defer_foreign_keys: %w", err)
		return
	}

	stdbTxqry := stdb.New(stdbtx)
	dbTxqry := db.New(dbtx)

	op := newPushDBStateOp(p.logger, dbTxqry, stdbTxqry)
	err = op.Do(ctx, subtree)
	if err != nil {
		err = fmt.Errorf("replace: %w", err)
		return
	}

	err = stdbtx.Commit()
	if err != nil {
		err = fmt.Errorf("commit structure db tx: %w", err)
		return
	}

	err = dbtx.Commit()
	if err != nil {
		err = fmt.Errorf("commit db tx: %w", err)
		return
	}

	return
}

// pushDBStateOp provides context for sub-operations of the pushDBState method
type pushDBStateOp struct {
	logger *slog.Logger
	db     *db.Queries
	stdb   *stdb.Queries
}

func newPushDBStateOp(
	logger *slog.Logger,
	db *db.Queries,
	stdb *stdb.Queries,
) pushDBStateOp {
	return pushDBStateOp{
		logger: logger,
		db:     db,
		stdb:   stdb,
	}
}

func (op *pushDBStateOp) Do(ctx context.Context, root *StructureInstance) (err error) {
	err = op.pushInstances(ctx, root)
	if err != nil {
		err = fmt.Errorf("push instances: %w", err)
		return
	}
	err = op.pushStructures(ctx, root)
	if err != nil {
		err = fmt.Errorf("push persistent structures: %w", err)
		return
	}
	return
}

// pushInstances pushes the structure instance state into the ephemeral
// database (and updates existing state and prunes deleted state)
//
// we expect root to always be an instance with a corrresponding AX node in the
// DOM
func (op *pushDBStateOp) pushInstances(ctx context.Context, root *StructureInstance) (err error) {
	if root.Underlying.Underlying.NodeID == "" {
		// this is because we will never receive an update with a synthetic
		// root and that it is impossible to resolve a node uniquely otherwise
		err = fmt.Errorf("can only push structure instance with concrete underlying AX ID")
		return
	}

	// resolve root's internal ID from root's AX ID
	st, err := op.stdb.GetStructByAXID(ctx, sql.NullString{
		String: string(root.Underlying.Underlying.NodeID),
		Valid:  true,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		err = fmt.Errorf(
			"resolve structure instance by AX ID (%s): %w",
			root.Underlying.Underlying.NodeID,
			err,
		)
		return
	}

	// we delete the root
	err = op.stdb.DeleteInstance(ctx, st.ID)
	if err != nil {
		err = fmt.Errorf("delete root instance: %w", err)
		return
	}

	// we push the root
	err = op.pushInstancesInner(ctx, root, st.Parent)
	if err != nil {
		err = fmt.Errorf("push root (%d): %w")
		return
	}

	// we update its ancestors
	err = op.updateAncestors(ctx, st)
	if err != nil {
		err = fmt.Errorf("update ancestors (%d): %w", st.ID)
		return
	}

	return
}

func (op *pushDBStateOp) pushInstancesInner(
	ctx context.Context,
	node *StructureInstance,
	parent sql.NullInt64,
) (err error) {
	id, err := op.pushSingleInstance(ctx, node, parent)
	if err != nil {
		err = fmt.Errorf("push single instance: %w", err)
		return
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		err = op.pushInstancesInner(ctx, child, sql.NullInt64{
			Int64: id,
			Valid: true,
		})
		if err != nil {
			err = fmt.Errorf("push child (%d): %w", child.Hash, err)
			return
		}
	}
	return
}

func (op *pushDBStateOp) pushSingleInstance(ctx context.Context, node *StructureInstance, parent sql.NullInt64) (id int64, err error) {
	var axId sql.NullString
	if node.Underlying.Underlying.NodeID != "" {
		axId = sql.NullString{
			String: string(node.Underlying.Underlying.NodeID),
			Valid:  true,
		}
	}

	shash := getByteslice()
	defer putByteslice(shash)
	shash = binary.BigEndian.AppendUint64(shash, node.Hash)

	phash := getByteslice()
	defer putByteslice(phash)
	phash = binary.BigEndian.AppendUint64(phash, node.Hash)

	id, err = op.stdb.CreateStructureInstance(ctx, stdb.CreateStructureInstanceParams{
		AxID:   axId,
		Parent: parent,
		Shash:  shash,
		Phash:  phash,
	})
	if err != nil {
		err = fmt.Errorf("create structure instance: %w", err)
		return
	}
	return
}

// pushStructures pushes the (instance-independent) structures of the given
// structure instance tree to the persistent database
//
// it updates existing structures as well
func (op *pushDBStateOp) pushStructures(ctx context.Context, node *StructureInstance) (err error) {
	// upsert the node's structure
	err = op.pushSingleStructure(ctx, node)
	if err != nil {
		err = fmt.Errorf("push single structure: %w", err)
		return
	}

	// upsert the node's path
	err = op.pushSingleStructPath(ctx, node)
	if err != nil {
		err = fmt.Errorf("push single structure path: %w", err)
		return
	}

	// recursively upsert children
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		err = op.pushStructures(ctx, child)
		if err != nil {
			err = fmt.Errorf("push child (%s): %w", child.Underlying.Underlying.NodeID, err)
			return
		}
	}

	return
}

func (op *pushDBStateOp) pushSingleStructure(ctx context.Context, node *StructureInstance) (err error) {
	hash := getByteslice()
	defer putByteslice(hash)
	hash = binary.BigEndian.AppendUint64(hash, node.Hash)

	var ns []byte
	if node.NextSibling != nil {
		ns = getByteslice()
		defer putByteslice(ns)

		ns = binary.BigEndian.AppendUint64(ns, node.NextSibling.Hash)
	}

	var fc []byte
	if node.FirstChild != nil {
		fc = getByteslice()
		defer putByteslice(fc)

		fc = binary.BigEndian.AppendUint64(fc, node.FirstChild.Hash)
	}

	err = op.db.UpsertStructure(ctx, db.UpsertStructureParams{
		Hash:        hash,
		FirstChild:  fc,
		NextSibling: ns,
	})
	if err != nil {
		err = fmt.Errorf("upsert structure: %w", err)
		return
	}

	return
}

func (op *pushDBStateOp) pushSingleStructPath(ctx context.Context, node *StructureInstance) (err error) {
	hash := getByteslice()
	defer putByteslice(hash)
	hash = binary.BigEndian.AppendUint64(hash, node.Hash)

	pathHash := getByteslice()
	defer putByteslice(pathHash)

	var parentPathHash []byte
	if node.Parent != nil {
		parentPathHash = getByteslice()
		defer putByteslice(parentPathHash)
		parentPathHash = binary.BigEndian.AppendUint64(parentPathHash, node.Parent.PathHash)
	}

	err = op.db.UpsertPath(ctx, db.UpsertPathParams{
		Hash:      pathHash,
		Structure: hash,
		Parent:    parentPathHash,
	})
	if err != nil {
		err = fmt.Errorf("upsert path: %w", err)
		return
	}

	return
}

func (op *pushDBStateOp) updateSHashDB(
	ctx context.Context,
	node stdb.StructureInstance,
) (err error) {
	// list children
	children, err := op.stdb.ListChildren(ctx, sql.NullInt64{
		Int64: node.ID,
		Valid: true,
	})
	if err != nil {
		err = fmt.Errorf("list children: %w", err)
		return
	}

	// compute hash from children and self
	hashBuf := getByteslice()
	defer putByteslice(hashBuf)

	hashBuf = append(hashBuf, []byte(node.Role.String)...)
	for _, child := range children {
		hashBuf = append(hashBuf, child.Shash...)
	}
	hash := xxh3.Hash(hashBuf)

	shash := getByteslice()
	defer putByteslice(shash)
	shash = binary.BigEndian.AppendUint64(shash, hash)

	// update hash in db
	err = op.stdb.UpdateStructureInstanceSHash(ctx, stdb.UpdateStructureInstanceSHashParams{
		ID:    node.ID,
		Shash: shash,
	})
	if err != nil {
		err = fmt.Errorf("update structure instance shash: %w", err)
		return
	}
	return
}

func (op *pushDBStateOp) updateAncestors(ctx context.Context, root stdb.StructureInstance) (err error) {
	current := root
	for current.Parent.Valid {
		parentID := current.Parent.Int64

		var parentInst stdb.StructureInstance
		parentInst, err = op.stdb.GetStruct(ctx, parentID)
		if err != nil {
			err = fmt.Errorf("get struct: %w", err)
			return
		}

		err = op.updateSHashDB(ctx, parentInst)
		if err != nil {
			err = fmt.Errorf("update shash (%d): %w", parentID, err)
			return
		}

		current = parentInst
	}
	return
}
