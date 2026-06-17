package main

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/structure"
	"iter"

	"github.com/LQR471814/rod"
	"github.com/LQR471814/rod/lib/proto"
)

func ObjectFromBackendID(page *rod.Page, backendID proto.DOMBackendNodeID) (obj *proto.RuntimeRemoteObject, err error) {
	req := cdp.DOMResolveNode{
		BackendNodeID: backendID,
	}
	res, err := cdp.Command(page.GetContext(), page, req)
	if err != nil {
		return
	}
	obj = res.Object
	return
}

func iterSubtreeHashInner(st *structure.StructureInstance, hash uint64, yield func(*structure.StructureInstance) bool) bool {
	if st == nil {
		return true
	}
	if st.Hash == hash {
		return yield(st)
	}
	if !iterSubtreeHashInner(st.FirstChild, hash, yield) {
		return false
	}
	if !iterSubtreeHashInner(st.NextSibling, hash, yield) {
		return false
	}
	return true
}

func iterSubtreeHash(st *structure.StructureInstance, hash uint64) iter.Seq[*structure.StructureInstance] {
	return func(yield func(*structure.StructureInstance) bool) {
		iterSubtreeHashInner(st, hash, yield)
	}
}
