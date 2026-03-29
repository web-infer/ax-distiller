package cdp

import "github.com/go-rod/rod/lib/proto"

type DOMNode struct {
	NodeID        proto.DOMNodeID        `json:"nodeId"`
	BackendNodeID proto.DOMBackendNodeID `json:"backendNodeId"`
	Children      []DOMNode              `json:"children,omitempty"`
}

// DOMGetBackendNodeID gets the DOMBackendNodeID for a given DOMNodeID, it is
// effectively a stripped-down version of DOMDescribeNode.
type DOMGetBackendNodeID struct {
	// NodeID (optional) Identifier of the node.
	NodeID proto.DOMNodeID `json:"nodeId,omitempty"`
}

func (DOMGetBackendNodeID) ProtoReq() string { return "DOM.describeNode" }

func (DOMGetBackendNodeID) Call(c proto.Client) (out *DOMGetBackendNodeIDResult, err error) { return }

type DOMGetBackendNodeIDResult struct {
	Node struct {
		BackendNodeID proto.DOMBackendNodeID `json:"backendNodeId"`
	} `json:"node"`
}

// DOMDescribeNode Describes node given its id, does not require domain to be
// enabled. Does not start tracking any objects, can be used for automation.
type DOMDescribeNode struct {
	// NodeID (optional) Identifier of the node.
	NodeID proto.DOMNodeID `json:"nodeId,omitempty"`

	// BackendNodeID (optional) Identifier of the backend node.
	BackendNodeID proto.DOMBackendNodeID `json:"backendNodeId,omitempty"`

	// ObjectID (optional) JavaScript object id of the node wrapper.
	ObjectID proto.RuntimeRemoteObjectID `json:"objectId,omitempty"`

	// Depth (optional) The maximum depth at which children should be retrieved, defaults to 1. Use -1 for the
	// entire subtree or provide an integer larger than 0.
	Depth *int `json:"depth,omitempty"`

	// Pierce (optional) Whether or not iframes and shadow roots should be traversed when returning the subtree
	// (default is false).
	Pierce bool `json:"pierce,omitempty"`
}

func (DOMDescribeNode) ProtoReq() string { return "DOM.describeNode" }

func (DOMDescribeNode) Call(c proto.Client) (out *DOMDescribeNodeResult, err error) { return }

type DOMDescribeNodeResult struct {
	Node DOMNode `json:"node"`
}

// DOMGetDocument Returns the root DOM node (and optionally the subtree) to
// the caller. Implicitly enables the DOM domain events for the current target.
type DOMGetDocument struct {
	// Depth (optional) The maximum depth at which children should be retrieved, defaults to 1. Use -1 for the
	// entire subtree or provide an integer larger than 0.
	Depth *int `json:"depth,omitempty"`

	// Pierce (optional) Whether or not iframes and shadow roots should be traversed when returning the subtree
	// (default is false).
	Pierce bool `json:"pierce,omitempty"`
}

func (DOMGetDocument) ProtoReq() string { return "DOM.getDocument" }

func (DOMGetDocument) Call(c proto.Client) (out *DOMGetDocumentResult, err error) { return }

type DOMGetDocumentResult struct {
	Root DOMNode `json:"root"`
}
