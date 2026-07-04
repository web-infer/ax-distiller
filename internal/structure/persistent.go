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

	"github.com/LQR471814/rod/lib/proto"
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

// replaceInstance replaces or adds a structure instance to a list of them
func replaceInstance(list []*StructureInstance, st *StructureInstance) []*StructureInstance {
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

func (p *Persistent) deleteInstance(state treeState, inst *StructureInstance) {
	instanceList := p.structIndex[inst.Hash]
	idx := slices.Index(instanceList, inst)
	if idx >= 0 {
		p.structIndex[inst.Hash] = slices.Delete(instanceList, idx, idx+1)
	}

	pathList := p.pathIndex[inst.PathHash]
	idx = slices.Index(pathList, inst)
	if idx >= 0 {
		p.pathIndex[inst.PathHash] = slices.Delete(pathList, idx, idx+1)
	}

	delete(state, inst.Underlying.Underlying.NodeID)
}

func (p *Persistent) saveStructures(st *StructureInstance, state treeState) {
	state[st.Underlying.Underlying.NodeID] = st
	p.structIndex[st.Hash] = replaceInstance(p.structIndex[st.Hash], st)
	p.pathIndex[st.PathHash] = replaceInstance(p.pathIndex[st.PathHash], st)

	for child := st.FirstChild; child != nil; child = child.NextSibling {
		p.saveStructures(child, state)
	}
}

func (p *Persistent) saveSynthStructures(st *StructureInstance, state treeState) {
	switch st.Underlying.Underlying.Role.Value {
	case ROLE_SYNTHETIC_OBJECT, ROLE_SYNTHETIC_LIST:
		state[st.Underlying.Underlying.NodeID] = &StructureInstance{
			Hash:     st.Hash,
			PathHash: st.PathHash,
		}
	}
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
		nodeHash := getByteslice()

		if node.NextSibling != nil {
			ns = getByteslice()
			ns = binary.BigEndian.AppendUint64(ns, node.NextSibling.Hash)
		}
		if node.FirstChild != nil {
			fc = getByteslice()
			fc = binary.BigEndian.AppendUint64(fc, node.FirstChild.Hash)
		}
		nodeHash = binary.BigEndian.AppendUint64(nodeHash, node.Hash)

		err = txqry.UpsertStructure(ctx, db.UpsertStructureParams{
			Hash:        nodeHash,
			NextSibling: ns,
			FirstChild:  fc,
		})
		if err != nil {
			return
		}

		if ns != nil {
			putByteslice(ns)
		}
		if fc != nil {
			putByteslice(fc)
		}
		putByteslice(nodeHash)

		parent := getByteslice()
		pathHash := getByteslice()

		if node.Parent != nil {
			parent = binary.BigEndian.AppendUint64(parent, node.Parent.PathHash)
		}
		pathHash = binary.BigEndian.AppendUint64(pathHash, node.PathHash)

		err = txqry.UpsertPath(ctx, db.UpsertPathParams{
			Hash:      pathHash,
			Structure: nodeHash,
			Parent:    parent,
		})

		putByteslice(parent)
		putByteslice(pathHash)
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
				p.deleteInstance(p.state, prevChild)
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

		p.Root = p.makeNodeTree(e.Updated[0])
		p.makePathHashes(p.Root)
		p.makeParent(p.Root)
		p.makeSynthetics(p.Root)
		p.saveStructures(p.Root, p.state)

		err = p.pushDB(ctx, p.state)

		p.logger.Debug("finish reset event")
	case axstream.EVENT_PATCH:
		// TODO: add functionality to update the parents of the updated
		// nodes? ? verify this behavior
		p.logger.Debug("start patch event")

		for _, updated := range e.Updated {
			st := p.makeNodeTree(updated)
			p.makePathHashes(st)
			p.makeParent(st)
			p.makeSynthetics(st)
			p.saveStructures(p.Root, p.recomputed)
		}

		err = p.pushDB(ctx, p.recomputed)

		p.reconcileRecomputed()

		p.logger.Debug("finish patch event")
	}
	return
}
