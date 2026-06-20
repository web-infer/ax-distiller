package structure

import (
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/db"
	"context"
	"database/sql"
	"encoding/binary"
	"log/slog"
	"slices"
	"sync"

	"github.com/LQR471814/rod/lib/proto"
	"github.com/zeebo/xxh3"
)

/*

key problem: structure computation based on non-ignored nodes
sit.: updates come from potentially ignored nodes


naive solution: we store the entire AX tree (ignored and all), filter it, then
compute structure on the filtered result for every update


observations:
- each update is either:
	1) entire tree changed -> naive solution fastest
	2) some nodes added + some nodes updated (children may be added/deleted)
- in case 2:
	- we can assume that the path of nodes -> root which update are present in
	added/updated list
		- this implies that some nodes in the updated list are in each other's
		subtree
	- each updated node potentially contains multiple non-ignored structure
	node in the subtree


given a node AX ID:
1. check if already recomputed, if so return updated structure
2. get non-ignored direct descendents as a flat list
3. run fn recursively on all non-ignored direct descendents -> list[structure]
4. compute structure
5. collapse adjacent and slideWindow alternatively
6. save structure under AX ID
7. return structure

somewhere in here must compute dropped nodes and drop them
*/

var histogramPool = &sync.Pool{
	New: func() any {
		return make(map[uint64]uint32)
	},
}

type structureEntry struct {
	Value      *StructureInstance
	References int
}

type treeState = map[proto.AccessibilityAXNodeID]*StructureInstance

type Persistent struct {
	Root *StructureInstance

	logger *slog.Logger
	driver *sql.DB
	db     *db.Queries

	structIndex map[uint64][]*StructureInstance
	pathIndex   map[uint64][]*StructureInstance

	// we do not actually use state to cache any operations because at any
	// point an AX node ID can point to a different possible structure
	//
	// we use state simply to keep a reference of the latest-known
	// mapping of AX node to structure
	state treeState

	// recomputed stores newly fetched nodes after receiving updates from
	// axstream
	recomputed treeState
}

func NewPersistent(logger *slog.Logger, driver *sql.DB) *Persistent {
	return &Persistent{
		Root: nil,

		structIndex: make(map[uint64][]*StructureInstance),
		pathIndex:   make(map[uint64][]*StructureInstance),
		state:       make(treeState),
		recomputed:  make(treeState),

		logger: logger.WithGroup("persistent"),
		driver: driver,
		db:     db.New(driver),
	}
}

func upsertInstance(list []*StructureInstance, st *StructureInstance) []*StructureInstance {
	for i, e := range list {
		if e.Underlying.Underlying.NodeID == st.Underlying.Underlying.NodeID {
			list[i] = st
			return list
		}
	}
	return append(list, st)
}

func (p *Persistent) StructHashToInstances() map[uint64][]*StructureInstance {
	return p.structIndex
}

func (p *Persistent) PathHashToInstances() map[uint64][]*StructureInstance {
	return p.pathIndex
}

func (p *Persistent) InstancesByPathHash(pathHash uint64) []*StructureInstance {
	return p.pathIndex[pathHash]
}

func (p *Persistent) InstancesByStructHash(hash uint64) []*StructureInstance {
	return p.structIndex[hash]
}

func (p *Persistent) InstanceForAXID(ax proto.AccessibilityAXNodeID) *StructureInstance {
	return p.state[ax]
}

func isNotIgnored(node *cdp.AXNodeWithRelatives) bool {
	ignored := node.Underlying.Ignored ||
		// (node.FirstChild == nil && node.Underlying.Role.Value == "generic") ||
		(node.FirstChild == nil && node.Underlying.Role.Value == "InlineTextBox")
	return !ignored
}

func (p *Persistent) recomputeNodeStructure(node *cdp.AXNodeWithRelatives, state treeState) (out *StructureInstance) {
	out = &StructureInstance{Underlying: node}
	hashBuff := []byte(node.Underlying.Role.Value)

	var prev *StructureInstance
	for child := range cdp.FilterDescendentsShallow(isNotIgnored, node) {

		// single child may return multiple children in linked list (via NextSibling)
		firstStruct := p.recomputeNodeStructure(child, state)

		// may return NextSibling != nil, but only if hitting cache
		// should never hit cache in root

		if prev == nil {
			// set first child to the first childStruct
			out.FirstChild = firstStruct
		} else {
			// set final node of last child's NextSibling to first node of this child
			prev.NextSibling = firstStruct
		}

		for str := firstStruct; str != nil; str = str.NextSibling {
			// add all children hashes to structure
			hashBuff = binary.LittleEndian.AppendUint64(hashBuff, str.Hash)
			if str.NextSibling == nil {
				// prev points to the last node of the child list returned
				prev = str
			}
		}
	}

	out.Hash = xxh3.Hash(hashBuff)

	p.structIndex[out.Hash] = upsertInstance(p.structIndex[out.Hash], out)
	p.pathIndex[out.PathHash] = upsertInstance(p.pathIndex[out.PathHash], out)

	/*
		// we create synthetic structural wrappers for repeated nodes and patterns
		// in the children linked list
		for {
			// group repeated adjacent nodes into a wrapper
			out.FirstChild = deleteAdjacent(out.FirstChild)

			// identify most frequent (and among the most frequent the largest)
			// pattern and replace all instances of it with a wrapper
			var replaced bool
			out.FirstChild, replaced = slideWindow(out.FirstChild)

			// rinse and repeat until no patterns are found
			if !replaced {
				break
			}
		}
	*/

	if out.NextSibling != nil {
		panic("assert failed: out.NextSibling != nil")
	}

	state[node.Underlying.NodeID] = out
	return
}

func (p *Persistent) setParent(st *StructureInstance) {
	if st == nil {
		return
	}
	for child := st.FirstChild; child != nil; child = child.NextSibling {
		child.Parent = st
		p.setParent(child)
	}
}

func (p *Persistent) computeHashPathsInner(
	st *StructureInstance,
	parentHash uint64,
	roleHash uint64,
	index uint32,
) {
	if st == nil {
		return
	}

	var buf []byte
	buf = binary.BigEndian.AppendUint64(buf, parentHash)
	buf = binary.BigEndian.AppendUint64(buf, roleHash)
	buf = binary.BigEndian.AppendUint32(buf, index)
	st.PathHash = xxh3.Hash(buf)

	indices := histogramPool.Get().(map[uint64]uint32)
	for child := st.FirstChild; child != nil; child = child.NextSibling {
		childRoleHash := xxh3.Hash([]byte(st.Underlying.Underlying.Role.Value))
		p.computeHashPathsInner(child, st.PathHash, childRoleHash, indices[childRoleHash])
		indices[childRoleHash]++
	}
	clear(indices)
	histogramPool.Put(indices)
}

// addHashPaths computes and adds the PathHash attribute on the entire
// StructureInstance tree
func (p *Persistent) addHashPaths(st *StructureInstance) {
	p.computeHashPathsInner(
		st,
		0,
		xxh3.Hash([]byte(st.Underlying.Underlying.Role.Value)),
		0,
	)
}

func (p *Persistent) pushDB(ctx context.Context, state treeState) (err error) {
	tx, err := p.driver.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	txqry := p.db.WithTx(tx)
	for _, node := range state {
		var ns []byte
		var fc []byte

		if node.NextSibling != nil {
			ns = binary.BigEndian.AppendUint64(nil, node.NextSibling.Hash)
		}
		if node.FirstChild != nil {
			fc = binary.BigEndian.AppendUint64(nil, node.FirstChild.Hash)
		}

		nodeHash := binary.BigEndian.AppendUint64(nil, node.Hash)

		err = txqry.UpsertStructure(ctx, db.UpsertStructureParams{
			Hash:        nodeHash,
			NextSibling: ns,
			FirstChild:  fc,
		})
		if err != nil {
			return
		}

		var parent []byte
		if node.Parent != nil {
			parent = binary.BigEndian.AppendUint64(nil, node.Parent.PathHash)
		}

		err = txqry.UpsertPath(ctx, db.UpsertPathParams{
			Hash:      binary.BigEndian.AppendUint64(nil, node.PathHash),
			Structure: nodeHash,
			Parent:    parent,
		})
	}

	err = tx.Commit()
	return
}

func (p *Persistent) reconcileRecomputed() {
	for id, next := range p.recomputed {
		// we upsert all new recomputed nodes into db

		prev, ok := p.state[id]

		// if update
		if ok {
			// delete all previous children from state map which are not in
			// recomputed node's children
		cleanup:
			for prevChild := prev.FirstChild; prevChild != nil; prevChild = prevChild.NextSibling {
				for nextChild := next.FirstChild; nextChild != nil; nextChild = nextChild.NextSibling {
					if nextChild.Underlying.Underlying.BackendDOMNodeID == prevChild.Underlying.Underlying.BackendDOMNodeID {
						continue cleanup
					}
				}

				instanceList := p.structIndex[prevChild.Hash]
				idx := slices.Index(instanceList, prevChild)
				if idx >= 0 {
					p.structIndex[prevChild.Hash] = slices.Delete(instanceList, idx, idx+1)
				}

				pathList := p.pathIndex[prevChild.PathHash]
				idx = slices.Index(pathList, prevChild)
				if idx >= 0 {
					p.pathIndex[prevChild.PathHash] = slices.Delete(pathList, idx, idx+1)
				}

				delete(p.state, prevChild.Underlying.Underlying.NodeID)
			}
		}

		p.state[id] = next
	}
	clear(p.recomputed)
}

func (p *Persistent) HandleEvent(ctx context.Context, e axstream.Event) (err error) {
	switch e.Type {
	case axstream.EVENT_RESET:
		p.logger.Debug("start reset event")
		clear(p.state)
		p.Root = p.recomputeNodeStructure(e.Updated[0], p.state)
		p.setParent(p.Root)
		p.addHashPaths(p.Root)
		err = p.pushDB(ctx, p.state)
		p.logger.Debug("finish reset event")
	case axstream.EVENT_PATCH:
		// TODO: add functionality to update the parents of the updated
		// nodes? ? verify this behavior
		p.logger.Debug("start patch event")
		for _, updated := range e.Updated {
			st := p.recomputeNodeStructure(updated, p.recomputed)
			p.setParent(st)
			p.addHashPaths(st)
		}
		err = p.pushDB(ctx, p.recomputed)
		p.reconcileRecomputed()
		p.logger.Debug("finish patch event")
	}
	return
}
