package structure

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/tree"
	"encoding/binary"
	"fmt"

	"github.com/zeebo/xxh3"
)

// StructureInstance is a concrete instance of a structure that is
// expected to be present in the browser state.
type StructureInstance struct {
	Hash        uint64
	PathHash    uint64
	Parent      *StructureInstance
	Underlying  *cdp.AXNodeWithRelatives
	FirstChild  *StructureInstance
	NextSibling *StructureInstance
}

// UpdateParentRecursively sets the parent reference of nodes recursively in
// the event that they are not set or incorrect for some reason.
//
// This is usually used in initialization logic.
func (st *StructureInstance) UpdateParentRecursively(parent *StructureInstance) {
	st.Parent = parent
	for child := st.FirstChild; child != nil; child = child.NextSibling {
		child.UpdateParentRecursively(st)
	}
}

// UpdateSHash updates the structure hash of the structure using its child references
func (st *StructureInstance) UpdateSHash() {
	// correct handling for synthetics
	switch st.Underlying.Underlying.Role.Value {
	case ROLE_SYNTHETIC_LIST:
		if st.FirstChild == nil {
			panic("assert failed: synthetic list must have child != nil")
		}
		// shash of synthetic list is always = to the hash of its children
		st.Hash = st.FirstChild.Hash
		return
		// TODO: think more about how to handle synthetic objects later
	}

	hashBuff := getByteslice()
	hashBuff = append(hashBuff, []byte(st.Underlying.Underlying.Role.Value)...)

	for child := st.FirstChild; child != nil; child = child.NextSibling {
		child.UpdateSHash()
		// add all children hashes to structure
		hashBuff = binary.BigEndian.AppendUint64(hashBuff, child.Hash)
	}

	st.Hash = xxh3.Hash(hashBuff)
	putByteslice(hashBuff)
}

// UpdatePHash updates the path hash of the structure using its parent reference
func (st *StructureInstance) UpdatePHash() {
	roleHash := xxh3.Hash([]byte(st.Underlying.Underlying.Role.Value))
	st.updatePHashInner(0, roleHash, 0)
}

func (st *StructureInstance) updatePHashInner(
	parentHash uint64,
	roleHash uint64,
	index uint32,
) {
	if st == nil {
		return
	}
	switch st.Underlying.Underlying.Role.Value {
	case ROLE_SYNTHETIC_LIST, ROLE_SYNTHETIC_OBJECT:
		// synthetics are supposed to be transparent when it comes to
		// "structural identity"
		for child := st.FirstChild; child != nil; child = child.NextSibling {
			child.updatePHashInner(parentHash, roleHash, index)
		}
		return
	}

	// path hash =
	//   parent path hash +
	//   current role +
	//   the "nth duplicate" of this role in the siblings
	buf := getByteslice()
	buf = binary.BigEndian.AppendUint64(buf, parentHash)
	buf = binary.BigEndian.AppendUint64(buf, roleHash)
	buf = binary.BigEndian.AppendUint32(buf, index)
	st.PathHash = xxh3.Hash(buf)
	putByteslice(buf)

	indices := getHistogram()
	for child := st.FirstChild; child != nil; child = child.NextSibling {
		childRoleHash := xxh3.Hash([]byte(st.Underlying.Underlying.Role.Value))
		child.updatePHashInner(st.PathHash, childRoleHash, indices[childRoleHash])
		indices[childRoleHash]++
	}
	putHistogram(indices)
}

func (s *StructureInstance) Debug() tree.DebugInfo {
	meta := s.Underlying.Underlying.Role.Value
	return tree.DebugInfo{
		Name: fmt.Sprintf(
			"%v - %v (%v)",
			s.Hash,
			s.PathHash,
			s.Underlying.Underlying.BackendDOMNodeID,
		),
		Metadata: meta,
	}
}

func (s *StructureInstance) Relatives() (rel tree.Relatives) {
	if s.FirstChild != nil {
		rel.FirstChild = s.FirstChild
	}
	if s.NextSibling != nil {
		rel.NextSibling = s.NextSibling
	}
	return
}

func (s *StructureInstance) String() string {
	return tree.Print(s)
}
