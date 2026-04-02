package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/cdp"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"net/http"
	_ "net/http/pprof"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod/lib/proto"
	"github.com/lmittmann/tint"
)

func main() {
	go func() {
		err := http.ListenAndServe("localhost:6060", nil)
		if err != nil {
			panic(err)
		}
	}()

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

	fetchedMutex := sync.Mutex{}
	fetched := make(map[proto.AccessibilityAXNodeID]cdp.AXNode)

	subscriptionCount := uint64(0)

	timer := time.NewTimer(time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:

				slog.Info("subscriptions", "count", atomic.LoadUint64(&subscriptionCount))
			}
		}
	}()

	subctx, cancel := context.WithCancel(ctx)
	subscriptions := make(chan proto.AccessibilityAXNodeID)
	reset := func() {
		cancel()
	drain:
		for {
			select {
			case <-subscriptions:
			default:
				break drain
			}
		}
		subctx, cancel = context.WithCancel(ctx)
	}
	for range 4 {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case target := <-subscriptions:
					if subctx.Err() != nil {
						continue
					}

					// this is purely for subscribing, we do not use the result
					// of this command
					res, err := cdp.Command(subctx, page, cdp.GetChildAXNodes{
						ID: target,
					})
					if err != nil {
						// we ignore errors, they do not affect the result
						continue
					}
					atomic.AddUint64(&subscriptionCount, 1)

					for i, child := range res.Nodes {
						fetchedMutex.Lock()
						_, alreadyFetched := fetched[child.NodeID]
						fetched[child.NodeID] = res.Nodes[i]
						if !alreadyFetched {
							go func() {
								subscriptions <- child.NodeID
							}()
						}
						fetchedMutex.Unlock()
					}
					timer.Reset(time.Second)
				}
			}
		}()
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
				reset()
				var event struct {
					Root cdp.AXNode `json:"root"`
				}
				err = sonic.Unmarshal(buff, &event)
				if err != nil {
					panic(err)
				}

				// fetch root & reset state
				fetchedMutex.Lock()
				clear(fetched)
				fetched[event.Root.NodeID] = event.Root
				fetchedMutex.Unlock()

				go func() {
					subscriptions <- event.Root.NodeID
				}()

				// debugging
				slog.Info("Accessibility.loadComplete", "root", event.Root.BackendDOMNodeID)
			case "Accessibility.nodesUpdated":
				var nodes cdp.AXNodesResult
				err = sonic.Unmarshal(buff, &nodes)
				if err != nil {
					panic(err)
				}

				// fetch non-fetched nodes & update all nodes given
				for i, n := range nodes.Nodes {
					fetchedMutex.Lock()
					_, alreadyFetched := fetched[n.NodeID]
					if !alreadyFetched {
						go func() {
							subscriptions <- n.NodeID
						}()
					}
					fetched[n.NodeID] = nodes.Nodes[i]
					fetchedMutex.Unlock()
				}

				// debugging
				ids := make([]proto.DOMBackendNodeID, len(nodes.Nodes))
				for i, n := range nodes.Nodes {
					ids[i] = n.BackendDOMNodeID
				}
				slog.Info("Accessibility.nodesUpdated", "updated_nodes", ids)
			}
		}
	}

}
