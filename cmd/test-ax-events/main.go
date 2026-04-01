package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/cdp"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"reflect"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod/lib/proto"
	"github.com/lmittmann/tint"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	browser, err := chrome.NewBrowser("chromium")
	if err != nil {
		panic(err)
	}

	page := browser.MustPage("https://amazon.com")

	err = cdp.CommandUnary(ctx, page, proto.AccessibilityEnable{})
	if err != nil {
		return
	}

	events := page.Event()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-events:
			method := msg.Method
			buff := reflect.ValueOf(msg).Elem().FieldByName("data").Bytes()
			switch method {
			case "Accessibility.loadComplete":
				slog.Info("event: load complete", "payload", string(buff))
				depth := -1
				res, err := cdp.Command(ctx, page, cdp.GetFullAXTree{Depth: &depth})
				if err != nil {
					slog.Warn("get full ax tree", "err", err)
				}
				queue := []proto.AccessibilityAXNodeID{res.Nodes[0].NodeID}
				for len(queue) > 0 {
					popped := queue[0]
					queue = queue[1:]
					children, err := cdp.Command(ctx, page, cdp.GetChildAXNodes{
						ID: popped,
					})
					if err != nil {
						slog.Warn("get children", "err", err)
						continue
					}
					for _, child := range children.Nodes {
						queue = append(queue, child.NodeID)
					}
				}
				slog.Info("event: subscribed to all")
			case "Accessibility.nodesUpdated":
				var nodes cdp.AXNodesResult
				err = sonic.Unmarshal(buff, &nodes)
				if err != nil {
					panic(err)
				}
				roles := make([]string, len(nodes.Nodes))
				for i, n := range nodes.Nodes {
					roles[i] = n.Role.Value
				}
				slog.Info("event: nodes updated", "roles", roles)
			}
		}
	}

}
