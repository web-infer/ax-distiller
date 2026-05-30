package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/fastclient"
	"ax-distiller/internal/slogx"
	"ax-distiller/internal/structure"
	"context"
	"errors"
	"iter"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
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

func main() {
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	logger := slogx.DemoLogger(slog.LevelDebug, func(group string, attrs iter.Seq[slog.Attr]) bool {
		switch group {
		case "main":
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
			case e, ok := <-events:
				if !ok {
					break
				}
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

	labeler := newLabeler(
		ctx,
		logger,
		driver,
		persistent,
		&persistLock,
	)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				persistLock.Lock()
				hashes := make([]uint64, len(persistent.Index))
				i := 0
				for h := range persistent.Index {
					hashes[i] = h
					i++
				}
				persistLock.Unlock()

				for _, h := range hashes {
					_, err := labeler.Label(h)
					if errors.Is(err, context.Canceled) {
						return
					}
					if err != nil {
						logger.Warn("label failed", "err", err)
					}
				}
			}
		}
	}()

	p.MustNavigate("https://ocw.mit.edu/search/?d=Mathematics")

	<-ctx.Done()
}
