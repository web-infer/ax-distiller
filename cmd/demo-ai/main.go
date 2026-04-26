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

// here we assume that: 1 token ~ 4 letters
const max_context_letters = 50000 * 4

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

	driver, err := OpenDB(ctx, logger)
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(1)
	}
	defer CloseDB(driver)

	browser, err := NewTestBrowser("chromium")
	if err != nil {
		logger.Error("create browser", "err", err)
		os.Exit(1)
	}
	p := browser.MustPage("about:blank")
	chrome.DisableUnusedCDP(p)

	events, err := axstream.Listen(ctx, logger, p)
	if err != nil {
		logger.Error("axstream listen", "err", err)
		os.Exit(1)
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

				wg := sync.WaitGroup{}
				wg.Add(len(entries))
				for _, e := range entries {
					go func() {
						defer wg.Done()

						_, ok, err := QueryLabel(ctx, driver, e.hash)
						if err != nil {
							logger.Error("query db", "err", err)
							return
						}
						if ok {
							return
						}

						var body strings.Builder
						for i := range 3 {
							if i >= len(e.nodes) {
								break
							}
							fmt.Fprintln(&body, "<ax_tree>")
							tree.PrintSExpr(e.nodes[i], &body)
							fmt.Fprintln(&body)
							fmt.Fprintln(&body, "</ax_tree>")
						}
						bodyStr := body.String()
						if len(bodyStr) > max_context_letters {
							// we truncate body if we are going to exceed max context
							bodyStr = bodyStr[:max_context_letters]
						}

						// break it up into two prompts to enable better prompt caching
						title, err := ask(ctx, label_prompt, bodyStr)
						if err != nil {
							logger.Error("llm ask", "err", err)
							return
						}

						err = RecordLabel(ctx, e.hash, title)
						if err != nil {
							logger.Error("insert db", "err", err)
							return
						}
						logger.Info("record label", "hash", e.hash, "title", title)
					}()
				}
				wg.Wait()
			}
		}
	}()

	p.MustNavigate("https://ocw.mit.edu/search/?d=Mathematics")

	<-ctx.Done()
}
