package interact

import (
	"ax-distiller/internal/chrome/cdp"
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

var keyByName = map[string]input.Key{
	"Enter":      input.Enter,
	"Escape":     input.Escape,
	"Tab":        input.Tab,
	"Backspace":  input.Backspace,
	"ArrowUp":    input.ArrowUp,
	"ArrowDown":  input.ArrowDown,
	"ArrowLeft":  input.ArrowLeft,
	"ArrowRight": input.ArrowRight,
	"Space":      input.Space,
}

type Interact struct {
	Page *rod.Page
}

func New(page *rod.Page) *Interact {
	return &Interact{Page: page}
}

func (i *Interact) Navigate(ctx context.Context, url string) error {
	// Navigate blocks until DOMContentLoaded — cap at 20s
	navCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return i.Page.Context(navCtx).Navigate(url)
}

func (i *Interact) Click(ctx context.Context, backendNodeID int64) error {
	nid := proto.DOMBackendNodeID(backendNodeID)
	page := i.Page.Context(ctx)

	// Use DOM.getBoxModel — no execution context required, only BackendNodeID.
	// Cap at 5s: off-screen or detached nodes can stall indefinitely.
	boxCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	box, err := cdp.Command[proto.DOMGetBoxModel, proto.DOMGetBoxModelResult](boxCtx, i.Page, proto.DOMGetBoxModel{BackendNodeID: nid})
	if err != nil {
		return fmt.Errorf("getBoxModel node %d: %w", backendNodeID, err)
	}

	// Content quad: [x0,y0, x1,y1, x2,y2, x3,y3] — compute centroid.
	q := box.Model.Content
	x := (q[0] + q[2] + q[4] + q[6]) / 4
	y := (q[1] + q[3] + q[5] + q[7]) / 4

	if err := page.Mouse.MoveTo(proto.Point{X: x, Y: y}); err != nil {
		return fmt.Errorf("mouse move node %d: %w", backendNodeID, err)
	}
	return page.Mouse.Click(proto.InputMouseButtonLeft, 1)
}

func (i *Interact) Type(ctx context.Context, backendNodeID int64, text string) error {
	node := &proto.DOMNode{BackendNodeID: proto.DOMBackendNodeID(backendNodeID)}
	el, err := i.elementFromNode(ctx, node)
	if err != nil {
		return err
	}
	if err := el.Focus(); err != nil {
		return err
	}
	return el.Input(text)
}

// elementFromNode resolves a BackendDOMNodeID to a rod Element, retrying up to
// 3 times with 600ms backoff. Used by Type — Click bypasses this via DOM.getBoxModel.
func (i *Interact) elementFromNode(ctx context.Context, node *proto.DOMNode) (*rod.Element, error) {
	var (
		el  *rod.Element
		err error
	)
	for attempt := range 3 {
		el, err = i.Page.Context(ctx).ElementFromNode(node)
		if err == nil {
			return el, nil
		}
		if attempt < 2 {
			time.Sleep(600 * time.Millisecond)
		}
	}
	return nil, err
}

func (i *Interact) PressKey(ctx context.Context, key string) error {
	k, ok := keyByName[key]
	if !ok {
		return fmt.Errorf("unknown key %q (supported: Enter, Escape, Tab, Backspace, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Space)", key)
	}
	return i.Page.Context(ctx).Keyboard.Press(k)
}

func (i *Interact) CurrentURL() string {
	info, err := i.Page.Info()
	if err != nil {
		return ""
	}
	return info.URL
}

func (i *Interact) WaitSettle() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = i.Page.Context(ctx).WaitLoad()
	time.Sleep(800 * time.Millisecond)
}

// FindNode queries the live AX tree for a node matching role and/or name.
// Returns the BackendDOMNodeID of the first match, or 0 if not found.
func (i *Interact) FindNode(ctx context.Context, role, name string) (int64, error) {
	q := cdp.QueryAXTree{Role: role, AccessibleName: name}
	res, err := cdp.Command(ctx, i.Page, q)
	if err != nil {
		return 0, err
	}
	for _, n := range res.Nodes {
		if int64(n.BackendDOMNodeID) > 0 {
			return int64(n.BackendDOMNodeID), nil
		}
	}
	return 0, nil
}
