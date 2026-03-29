package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/fastclient"
	"ax-distiller/internal/postprocess"
	"ax-distiller/internal/structure"
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/lmittmann/tint"
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	browser, err := NewTestBrowser("chromium")
	if err != nil {
		panic(err)
	}

	events := make(chan axstream.Event)
	go func() {
		for e := range events {
			switch e.Type {
			case axstream.EVENT_REPLACE:
				filtered := postprocess.FilterIgnored(e.Subtree)
				constructed := structure.Construct(filtered)
				slog.Info("event replace root", "hash", constructed.Hash)
			case axstream.EVENT_INSERT:
				filtered := postprocess.FilterIgnored(e.Subtree)
				constructed := structure.Construct(filtered)
				slog.Info("event insert", "prev_sibling", e.ID, "hash", constructed.Hash)
			case axstream.EVENT_REMOVE:
				slog.Info("event remove", "id", e.ID)
			}
		}
	}()

	p := browser.MustPage("about:blank")
	chrome.DisableUnusedCDP(p)
	// chrome.BlockGraphics(p)
	err = axstream.Listen(ctx, events, p)
	if err != nil {
		panic(err)
	}

	p.MustNavigate("https://amazon.com")

	<-ctx.Done()
}
