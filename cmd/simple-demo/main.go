package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/fastclient"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime"

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
	client := fastclient.NewClient()
	ws := &rodcdp.WebSocket{}
	err = ws.Connect(browser.GetContext(), controlURL, nil)
	if err != nil {
		panic(err)
	}

	client.Start(ws, runtime.NumCPU())
	browser.Client(client)
	browser.MustConnect()
	return
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	browser, err := NewTestBrowser("chromium")
	if err != nil {
		panic(err)
	}

	events := make(chan chrome.AXStreamEvent)
	go func() {
		for e := range events {
			switch e.Type {
			case chrome.AXSTREAM_EVENT_INSERT:
				slog.Info("event insert", "subtree_role", e.Subtree.Role.Value, "prev_sibling", e.ID)
			case chrome.AXSTREAM_EVENT_REMOVE:
				slog.Info("event remove", "id", e.ID)
			case chrome.AXSTREAM_EVENT_REPLACE:
				slog.Info("event replace root")
			}
		}
	}()
	p := browser.MustPage("https://amazon.com")
	chrome.DisableUnusedCDP(p)
	// chrome.BlockGraphics(p)

	err = chrome.ListenAXStream(ctx, events, p)
	if err != nil {
		panic(err)
	}
}
