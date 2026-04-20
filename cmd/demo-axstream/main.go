package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/fastclient"
	"ax-distiller/internal/slogx"
	"context"
	"iter"
	"log/slog"
	"os"
	"os/signal"

	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
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

func main() {
	logger := slogx.DemoLogger(slog.LevelInfo, func(group string, attrs iter.Seq[slog.Attr]) bool {
		return group == "main"
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
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				switch e.Type {
				case axstream.EVENT_RESET:
					logger.Info("reset")
				case axstream.EVENT_PATCH:
					logger.Info("patch", "added", len(e.Added), "updated", len(e.Updated))
				}
			}
		}
	}()

	p.MustNavigate("https://amazon.com")

	<-ctx.Done()
}
