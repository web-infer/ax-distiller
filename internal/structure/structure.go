package structure

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/tree"
	"encoding/binary"
	"fmt"

	"github.com/elliotchance/orderedmap/v3"
	"github.com/zeebo/xxh3"
)

var synthetic_list = &cdp.AXNodeWithRelatives{
	Underlying: cdp.AXNode{
		Role: cdp.Value[string]{Value: "SYNTHETIC_LIST"},
	},
}

var synthetic_object = &cdp.AXNodeWithRelatives{
	Underlying: cdp.AXNode{
		Role: cdp.Value[string]{Value: "SYNTHETIC_OBJECT"},
	},
}

// StructureInstance is a concrete instance of a structure that is
// expected to be present in the browser state.
type StructureInstance struct {
	Hash     uint64
	PathHash uint64
	// ParentPathHash will == 0 if there is no parent
	ParentPathHash uint64
	Underlying     *cdp.AXNodeWithRelatives
	FirstChild     *StructureInstance
	NextSibling    *StructureInstance
}

// this removes adjacent letters and replaces it with a reference:
// AABCCC -> A'B'C'
//
// if not adjacent repeating letters are found, it will just return
// the original start cleanNode passed in without modification
func deleteAdjacent(start *StructureInstance) (ret *StructureInstance) {
	if start == nil {
		return
	}
	if start.NextSibling == nil {
		ret = start
		return
	}

	// points to the current wrapper structure, nil if no wrapper structure is
	// currently being created.
	var pending *StructureInstance

	// always points to the node whose next sibling must be updated to point to
	// a newly created wrapper structure.
	var prev *StructureInstance

	var next *StructureInstance
	for cur := start; cur != nil; cur = next {
		next = cur.NextSibling

		equal := next != nil && cur.Hash == next.Hash
		if equal {
			// if cur and next are equal

			if pending == nil {
				// if wrapper does not already exist
				pending = &StructureInstance{ // create pending structure in case where a structure is guaranteed to exist
					Hash:       cur.Hash,
					Underlying: synthetic_list,
					FirstChild: cur,
				}
				if prev != nil {
					prev.NextSibling = pending // make sure to update prev ptr with new next sibling (newly created wrapper)
				}
			}

			if ret == nil {
				ret = pending
			}
		} else {
			// if cur and next are not equal (or next is nil)

			if pending != nil {
				// if end of current pending wrapper
				pending.NextSibling = next // connect wrapper structure to next different letter
				cur.NextSibling = nil      // cut off next sibling of last child of wrapper
				prev = pending             // make sure prev always points to the structure whose next sibling may need to be modified
				pending = nil              // reset pending to indicate no current wrapper
			} else {
				prev = cur // make sure prev always points to the structure whose next sibling may need to be modified
			}

			if ret == nil {
				ret = cur
			}
		}
	}

	return
}

// this replaces the most frequent pattern with a reference
// ABABC -> R_1 R_1 C (R_1 = AB)
//
// algorithm will choose most frequent pattern, favoring larger patterns
// when the frequencies are the same
func slideWindow(start *StructureInstance) (ret *StructureInstance, replaced bool) {
	if start == nil {
		return
	}

	type pattern struct {
		start     *StructureInstance // start stores the first node in the pattern
		length    int
		instances int
		comboHash uint64
	}
	dict := orderedmap.NewOrderedMap[uint64, pattern]()

	// identify patterns

outer:
	for length := 2; ; length++ {

	inner:
		for cursor := start; ; cursor = cursor.NextSibling {
			// test if reached the end
			patternCursor := cursor
			for range length {
				if patternCursor == nil {
					// test if pattern length has exceeded # of nodes in layer
					if cursor == start {
						break outer
					}
					break inner
				}
				patternCursor = patternCursor.NextSibling
			}

			var buff []byte
			patternCursor = cursor
			for range length {
				buff = binary.LittleEndian.AppendUint64(buff, patternCursor.Hash)
				patternCursor = patternCursor.NextSibling
			}
			comboHash := xxh3.Hash(buff)

			existing, _ := dict.Get(comboHash)

			// check if this pattern's start is part of the last pattern
			// instance
			ref := existing.start
			if ref != nil {
				for range length {
					if cursor == ref {
						// if it is, then this pattern instance should not be recorded
						continue outer
					}
					ref = ref.NextSibling
				}
			}

			dict.Set(comboHash, pattern{
				start:     cursor,
				length:    length,
				instances: 1 + existing.instances,
				comboHash: comboHash,
			})
		}
	}

	// choose most optimal pattern

	maxInstances := -1
	maxLength := -1
	var maxPattern pattern
	for p := range dict.Values() {
		if (p.instances == maxInstances && p.length > maxLength) || p.instances > maxInstances {
			maxPattern = p
			maxLength = p.length
			maxInstances = p.instances
		}
	}
	if maxInstances < 2 {
		ret = start
		return
	}

	replaced = true

	// replace instances of pattern with wrappers

	// stores the last letter not a part of the current pattern being scanned
	var prev *StructureInstance

	// stores the next sibling reference in case it is overriden
	var next *StructureInstance

curStart:
	for curStart := start; curStart != nil; curStart = next {
		next = curStart.NextSibling

		// fmt.Println("starting from", curStart.Role.Value())

		var last *StructureInstance
		cur := curStart
		ref := maxPattern.start
		for refidx := 0; refidx < maxPattern.length; refidx++ {
			// fmt.Println(cur.Hash, cur.Role.Value(), ref.Hash, ref.Role.Value())

			// if reached end, no more patterns will be possible (since a full
			// pattern can never be matched, there are not enough nodes
			// available)
			if cur == nil {
				break curStart
			}

			if cur.Hash != ref.Hash {
				// pattern does not match
				if prev != nil {
					prev.NextSibling = curStart
				}
				prev = curStart

				// the case where the first node is not the start of a pattern
				// instance
				if ret == nil {
					ret = prev
				}

				continue curStart
			}
			ref = ref.NextSibling
			last = cur
			cur = cur.NextSibling
		}

		// matched pattern
		next = cur

		last.NextSibling = nil
		newNode := &StructureInstance{
			Hash:       maxPattern.comboHash,
			Underlying: synthetic_object,
			FirstChild: curStart,
		}
		if prev != nil {
			prev.NextSibling = newNode
		}
		prev = newNode

		// the case where the first node is the start of a pattern instance
		if ret == nil {
			ret = newNode
		}
	}

	return
}

func convertToStructure(start *cdp.AXNodeWithRelatives) (ret *StructureInstance) {
	if start == nil {
		return nil
	}

	/*
		cur iterates from start to end

		prev is structure

		newNode is created for each cur

		this is because Construct may return multiple nodes, they must be conjoined properly
	*/

	var prev *StructureInstance
	for cur := start; cur != nil; cur = cur.NextSibling {
		fc := Construct(cur.FirstChild)
		newNode := &StructureInstance{
			Underlying: cur,
			FirstChild: fc,
		}

		hashBuff := []byte(cur.Underlying.Role.Value)
		for child := fc; child != nil; child = child.NextSibling {
			hashBuff = binary.LittleEndian.AppendUint64(hashBuff, child.Hash)
		}
		newNode.Hash = xxh3.Hash(hashBuff)

		if ret == nil {
			ret = newNode
		}
		if prev != nil {
			prev.NextSibling = newNode
		}
		prev = newNode
		for prev.NextSibling != nil {
			prev = prev.NextSibling
		}
	}

	return
}

// this creates synthetic structural wrappers for repeated nodes and patterns
//
// the general process is:
//   - group repeated adjacent nodes into a wrapper
//   - identify most frequent (and among the most frequent the largest) pattern
//     and replace all instances of it with a wrapper
//   - rinse and repeat until no patterns are found
func Construct(current *cdp.AXNodeWithRelatives) (ret *StructureInstance) {
	if current == nil {
		ret = nil
		return
	}

	ret = convertToStructure(current)
	if ret == nil {
		return
	}

	// fmt.Println("============================")
	//
	// fmt.Println(ret)

	for {
		ret = deleteAdjacent(ret)
		// fmt.Println("deleteAdjacent")
		// fmt.Println(ret.String())

		var replaced bool
		ret, replaced = slideWindow(ret)
		// fmt.Println("slideWindow")
		// fmt.Println(ret.String())

		if !replaced {
			break
		}
	}

	return
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
