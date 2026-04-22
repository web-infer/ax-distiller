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
	node := &proto.DOMNode{BackendNodeID: proto.DOMBackendNodeID(backendNodeID)}
	el, err := i.Page.Context(ctx).ElementFromNode(node)
	if err != nil {
		return err
	}
	if err := el.ScrollIntoView(); err != nil {
		return err
	}
	return el.Click(proto.InputMouseButtonLeft, 1)
}

func (i *Interact) Type(ctx context.Context, backendNodeID int64, text string) error {
	node := &proto.DOMNode{BackendNodeID: proto.DOMBackendNodeID(backendNodeID)}
	el, err := i.Page.Context(ctx).ElementFromNode(node)
	if err != nil {
		return err
	}
	if err := el.Focus(); err != nil {
		return err
	}
	return el.Input(text)
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = i.Page.Context(ctx).WaitLoad()
	time.Sleep(300 * time.Millisecond)
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
