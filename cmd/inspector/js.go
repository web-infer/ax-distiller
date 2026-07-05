package main

import (
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/structure"
	"ax-distiller/internal/structure/stdb"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"log/slog"
	"sync"

	"github.com/LQR471814/rod"
	"github.com/LQR471814/rod/lib/proto"
	"github.com/ysmood/gson"

	_ "embed"
)

//go:embed tree.js
var treeComponents string

//go:embed client.js
var client string

//go:embed rpc.js
var rpc string

var jsController = fmt.Sprintf(`() => {
%s
%s
%s
}`,
	rpc,
	treeComponents,
	client,
)

type NodeInfo struct {
	Parent *int64
	ID     int64
	AxID   *string
	Role   string
	// we return hashes as string representation because JS
	// unfortunately cannot store a full uint64 with number type
	StructureHash string
	PathHash      string
	Instances     int
	Highlights    []string
	Children      []string
}

func GetNodeInfo(ctx context.Context, stdbqry *stdb.Queries, id int64) (info NodeInfo, err error) {
	st, err := stdbqry.GetStruct(ctx, id)
	if err != nil {
		err = fmt.Errorf("get struct: %w", err)
		return
	}

	info = NodeInfo{
		ID:            string(st.Underlying.Underlying.NodeID),
		Role:          st.Underlying.Underlying.Role.Value,
		StructureHash: fmt.Sprint(st.Hash),
		PathHash:      fmt.Sprint(st.PathHash),
		Highlights:    []string{},
		Children:      []string{},
	}

	instances, err := stdbqry.ListStructInstBySHash(ctx, binary.BigEndian.AppendUint64(nil, st.Hash))
	if err != nil {
		err = fmt.Errorf("list instances by shash: %w", err)
		return
	}

	info.Instances = len(instances)

	for child := st.FirstChild; child != nil; child = child.NextSibling {
		info.Children = append(info.Children, string(child.Underlying.Underlying.NodeID))
	}

	switch st.Underlying.Underlying.Role.Value {
	case structure.ROLE_SYNTHETIC_LIST, structure.ROLE_SYNTHETIC_OBJECT:
		log.Println("-----------------")
		for _, h := range instances {
			log.Println("highlight", h)
			for child := h.FirstChild; child != nil; child = child.NextSibling {
				info.Highlights = append(info.Highlights, string(child.Underlying.Underlying.NodeID))
			}
		}
	default:
		// if regular structure, we highlight all of its instances
		info.Highlights = make([]string, len(instances))
		for i, h := range instances {
			if !h.AxID.Valid {
				err = fmt.Errorf("axid should always be defined on concrete structure instance: %w", err)
				return
			}
			info.Highlights[i] = h.AxID.String
		}
	}

	if st.Parent != nil {
		info.Parent = new(string(st.Parent.Underlying.Underlying.NodeID))
	}

	return
}

func expandLevels(
	persistent *structure.Persistent,
	st *structure.StructureInstance,
	out *[]NodeInfo,
	level, maxLevel int,
) {
	if maxLevel >= 0 && level >= maxLevel {
		return
	}
	if st == nil {
		return
	}
	*out = append(*out, NewNodeInfo(persistent, st))
	for child := st.FirstChild; child != nil; child = child.NextSibling {
		expandLevels(persistent, child, out, level+1, maxLevel)
	}
}

func expandTo(
	persistent *structure.Persistent,
	st *structure.StructureInstance,
	out *[]NodeInfo,
) {
	if st.Parent == nil {
		*out = append(*out, NewNodeInfo(persistent, st))
		return
	} else {
		expandTo(persistent, st.Parent, out)
		for sibling := st; sibling != nil; sibling = sibling.NextSibling {
			*out = append(*out, NewNodeInfo(persistent, sibling))
		}
	}
}

func initPageJS(
	page *rod.Page,
	logger *slog.Logger,
	stdbDriver *sql.DB,
	persistent *structure.Persistent,
	persistLock *sync.Mutex,
) (err error) {
	stdbqry := stdb.New(stdbDriver)

	// (this is a tuple)
	// type Args = [axId: string, levels: number]
	// if axId == "", it will return root
	_, err = page.Expose("__ax_inspect_expandLevels", func(j gson.JSON) (any, error) {
		persistLock.Lock()
		defer persistLock.Unlock()

		axId := j.Get("0").Str()
		levels := j.Get("1").Int()

		var instance *structure.StructureInstance
		infos := []NodeInfo{}

		if axId != "" {
			instance = persistent.InstanceForAXID(proto.AccessibilityAXNodeID(axId))
		} else {
			instance = persistent.Root
		}
		expandLevels(persistent, instance, &infos, 0, levels)

		return infos, nil
	})
	if err != nil {
		return
	}

	// string is axId
	// type Args = string
	_, err = page.Expose("__ax_inspect_expandTo", func(j gson.JSON) (any, error) {
		persistLock.Lock()
		defer persistLock.Unlock()

		id := j.Int()
		infos := []NodeInfo{}

		instance := persistent.InstanceForAXID(proto.AccessibilityAXNodeID(axId))
		expandTo(persistent, instance, &infos)

		return infos, nil
	})
	if err != nil {
		return
	}

	// string is axId
	// type Args = string
	_, err = page.Expose("__ax_inspect_getStructInfo", func(j gson.JSON) (any, error) {
		persistLock.Lock()
		defer persistLock.Unlock()

		axId := j.Str()
		st := persistent.InstanceForAXID(proto.AccessibilityAXNodeID(axId))
		if st == nil {
			return nil, nil
		}
		logger.Info("lookup: hash", "id", axId, "res", st.Hash)
		out := NewNodeInfo(persistent, st)

		return out, nil
	})
	if err != nil {
		err = fmt.Errorf("expose fn: %w", err)
		return
	}

	_, err = page.Eval(jsController)
	if err != nil {
		err = fmt.Errorf("init js control: %w", err)
		return
	}

	depth := 1
	_, err = cdp.Command(page.GetContext(), page, proto.DOMGetDocument{
		Depth: &depth,
	})
	if err != nil {
		err = fmt.Errorf("get doc: %w", err)
		return
	}

	return
}

func updateInspectorTree(
	page *rod.Page,
	persistent *structure.Persistent,
	persistLock *sync.Mutex,
	event axstream.Event,
) (err error) {
	persistLock.Lock()
	defer persistLock.Unlock()

	// must init empty slice because otherwise will return null
	infos := []NodeInfo{}
	for _, updated := range event.Updated {
		st := persistent.InstanceForAXID(updated.Underlying.NodeID)
		expandLevels(persistent, st, &infos, 0, -1)
	}
	_, err = page.Eval("window.__ax_inspector_updateTree", infos)
	return
}
