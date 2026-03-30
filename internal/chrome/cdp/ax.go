package cdp

import "github.com/go-rod/rod/lib/proto"

type Value[T any] struct {
	Value T `json:"value,omitempty"`
}

// AXNode exists because *proto.AccessibilityAXNode contains a bunch of
// unnecessary fields that slow down parsing
type AXNode struct {
	NodeID           proto.AccessibilityAXNodeID   `json:"nodeId"`
	BackendDOMNodeID proto.DOMBackendNodeID        `json:"backendDOMNodeId"`
	ChildIDs         []proto.AccessibilityAXNodeID `json:"childIds,omitempty"`

	Ignored  bool          `json:"ignored"`
	Role     Value[string] `json:"role"`
	ProtoReq Value[string] `json:"name"`
	// Description CDPAXValue[string] `json:"description"`
	// Value       CDPAXValue[any]    `json:"value"`
}

type AXNodesResult struct {
	Nodes []*AXNode `json:"nodes"`
}

// GetFullAXTree (experimental) Fetches the entire accessibility tree for the root Document.
type GetFullAXTree struct {
	// Depth (optional) The maximum depth at which descendants of the root node should be retrieved.
	// If omitted, the full tree is returned.
	Depth *int `json:"depth,omitempty"`

	// FrameID (optional) The frame for whose document the AX tree should be retrieved.
	// If omitted, the root frame is used.
	FrameID proto.PageFrameID `json:"frameId,omitempty"`
}

func (GetFullAXTree) ProtoReq() string { return "Accessibility.getFullAXTree" }

// this function does not need to be implemented since it is just used to infer
// the type of the return param
func (GetFullAXTree) Call(c proto.Client) (out *AXNodesResult, err error) { return }

// GetPartialAXTree (experimental) Fetches the accessibility node and partial accessibility tree for this DOM node, if it exists.
type GetPartialAXTree struct {
	// NodeID (optional) Identifier of the node to get the partial accessibility tree for.
	NodeID *proto.DOMNodeID `json:"nodeId,omitempty"`

	// BackendNodeID (optional) Identifier of the backend node to get the partial accessibility tree for.
	BackendNodeID *proto.DOMBackendNodeID `json:"backendNodeId,omitempty"`

	// ObjectID (optional) JavaScript object id of the node wrapper to get the partial accessibility tree for.
	ObjectID proto.RuntimeRemoteObjectID `json:"objectId,omitempty"`

	// FetchRelatives (optional) Whether to fetch this node's ancestors, siblings and children. Defaults to true.
	FetchRelatives *bool `json:"fetchRelatives,omitempty"`
}

func (GetPartialAXTree) ProtoReq() string { return "Accessibility.getPartialAXTree" }

func (GetPartialAXTree) Call(c proto.Client) (out *AXNodesResult, err error) { return }

// GetChildAXNodes (experimental) Fetches a particular accessibility node by AXNodeId.
// Requires `enable()` to have been called previously.
type GetChildAXNodes struct {
	// ID ...
	ID proto.AccessibilityAXNodeID `json:"id"`

	// FrameID (optional) The frame in whose document the node resides.
	// If omitted, the root frame is used.
	FrameID proto.PageFrameID `json:"frameId,omitempty"`
}

func (GetChildAXNodes) ProtoReq() string { return "Accessibility.getChildAXNodes" }

func (GetChildAXNodes) Call(c proto.Client) (out *AXNodesResult, err error) { return }

type QueryAXTree struct {
	NodeID         proto.DOMNodeID             `json:"nodeId,omitempty"`
	BackendNodeID  *proto.DOMBackendNodeID     `json:"backendNodeId,omitempty"`
	ObjectId       proto.RuntimeRemoteObjectID `json:"objectId,omitempty"`
	AccessibleName string                      `json:"accessibleName,omitempty"`
	Role           string                      `json:"role,omitempty"`
}

func (QueryAXTree) ProtoReq() string { return "Accessibility.queryAXTree" }

func (QueryAXTree) Call(c proto.Client) (out *QueryAXTreeResult, err error) { return }

type QueryAXTreeResult struct {
	Nodes []*AXNode `json:"nodes"`
}
