package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/db"
	"ax-distiller/internal/slogx"
	"ax-distiller/internal/structure"
	"context"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"os/signal"

	"github.com/LQR471814/rod"
	rodcdp "github.com/LQR471814/rod/lib/cdp"
	"github.com/LQR471814/rod/lib/launcher"

	"net/http"
	_ "net/http/pprof"
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
	client := rodcdp.New()
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

	driver, err := db.OpenDB(ctx, logger, ":memory:")
	if err != nil {
		panic(err)
	}
	defer db.CloseDB(driver)

	persistent := structure.NewPersistent(logger, driver)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-events:
				if !ok {
					break
				}

				err = persistent.HandleEvent(ctx, e)
				if err != nil {
					err = fmt.Errorf("handle event: %w", err)
					logger.Error("persistent error", "err", err)
				}
				switch e.Type {
				case axstream.EVENT_RESET:
					logger.Info("page reset", "root", persistent.Root.Hash)

					fmt.Println(persistent.Root)
				case axstream.EVENT_PATCH:
					logger.Info("page updated")
				}
			}
		}
	}()

	p.MustNavigate("https://ocw.mit.edu/search/?d=Mathematics")

	<-ctx.Done()
}
