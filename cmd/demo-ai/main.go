package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/chrome/fastclient"
	"ax-distiller/internal/slogx"
	"ax-distiller/internal/structure"
	"ax-distiller/internal/tree"
	"context"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"

	_ "embed"
)

func NewTestBrowser(chromeBin string) (browser *rod.Browser, err error) {
	dataTemp := "/tmp/ax-distiller/chrome-data"
	err = os.RemoveAll(dataTemp)
	if err != nil {
		return
	}
	err = os.MkdirAll(dataTemp, 0700)
	if err != nil {
		return
	}

	launch := launcher.New().Bin(chromeBin).
		UserDataDir(dataTemp).
		Headless(false).
		Set("display", os.Getenv("DISPLAY")).
		Set("disable-extensions", "false").
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-gpu", "true").
		Set("no-sandbox", "true").
		Set("no-default-browser-check", "true").
		Set("disable-remote-fonts", "true").
		Set("disable-background-networking", "true").
		Set("disable-dev-shm-usage", "true").
		Set("disable-sync", "true").
		Set("disable-translate", "true").
		Set("disable-default-apps", "true").
		Set("mute-audio", "true").
		Set("hide-scrollbars", "true")

	controlURL := launch.MustLaunch()
	browser = rod.New()
	client := fastclient.New()
	ws := &rodcdp.WebSocket{}
	err = ws.Connect(browser.GetContext(), controlURL, nil)
	if err != nil {
		panic(err)
	}
	client.Start(ws)
	browser.Client(client)
	browser.MustConnect()
	return
}

//go:embed label_prompt.txt
var label_prompt string

func main() {
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	logger := slogx.DemoLogger(slog.LevelInfo, func(group string, attrs iter.Seq[slog.Attr]) bool {
		switch group {
		case "main", "persistent":
			return true
		}
		return false
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	browser, err := NewTestBrowser("chromium")
	if err != nil {
		panic(err)
	}
	p := browser.MustPage("about:blank")
	chrome.DisableUnusedCDP(p)

	events, err := axstream.Listen(ctx, logger, p)
	if err != nil {
		panic(err)
	}

	timer := time.NewTimer(250 * time.Millisecond)
	persistLock := sync.Mutex{}
	persistent := structure.NewPersistent(logger)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				persistLock.Lock()
				persistent.HandleEvent(e)
				switch e.Type {
				case axstream.EVENT_RESET:
					logger.Info("page reset", "root", persistent.Root.Hash)
					timer.Reset(250 * time.Millisecond)
				case axstream.EVENT_PATCH:
					logger.Info("page updated")
					timer.Reset(250 * time.Millisecond)
				}
				persistLock.Unlock()
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				persistLock.Lock()
				type record struct {
					hash  uint64
					nodes []*cdp.AXNodeWithRelatives
				}
				entries := make([]record, len(persistent.Index))
				i := 0
				for k := range persistent.Index {
					entries[i] = record{
						hash:  k,
						nodes: persistent.Index[k],
					}
					i++
				}
				persistLock.Unlock()

				outputLock := sync.Mutex{}
				wg := sync.WaitGroup{}
				wg.Add(len(entries))
				for _, e := range entries {
					go func() {
						defer wg.Done()

						var examples strings.Builder
						for i := range 3 {
							if i >= len(e.nodes) {
								break
							}
							tree.PrintSExpr(e.nodes[i], &examples)
							fmt.Fprintln(&examples)
						}
						prompt := fmt.Sprintf(label_prompt, examples.String())
						result := ask(ctx, prompt)
						outputLock.Lock()
						fmt.Printf("--- SUMMARY - %v - %v\n", result, e.hash)
						fmt.Println(e.nodes[0])
						fmt.Println()
						outputLock.Unlock()
					}()
				}
				wg.Wait()
			}
		}
	}()

	p.MustNavigate("https://amazon.com")

	<-ctx.Done()
}
