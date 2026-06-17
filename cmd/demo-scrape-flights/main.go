package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/db"
	"ax-distiller/internal/slogx"
	"ax-distiller/internal/stealth"
	"ax-distiller/internal/structure"
	"context"
	"iter"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"
)

func main() {
	logger := slogx.DemoLogger(slog.LevelInfo, func(group string, attrs iter.Seq[slog.Attr]) bool {
		return true
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	browser, err := chrome.NewBrowser("chromium")
	if err != nil {
		panic(err)
	}
	defer browser.Close()

	page := browser.MustPage("about:blank")
	chrome.DisableUnusedCDP(page)

	_, err = page.EvalOnNewDocument(stealth.JSExpr)
	if err != nil {
		panic(err)
	}

	events, err := axstream.Listen(ctx, logger, page)
	if err != nil {
		panic(err)
	}

	driver, err := db.OpenDB(ctx, logger, "state.db")
	if err != nil {
		panic(err)
	}
	defer db.CloseDB(driver)

	persistLock := sync.Mutex{}
	persistent := structure.NewPersistent(logger, driver)
	updatePersist := func(e axstream.Event) {
		defer persistLock.Unlock()
		persistLock.Lock()
		err := persistent.HandleEvent(ctx, e)
		if err != nil {
			logger.Error("persist", "err", err)
		}
	}

	onDone := make(chan error)
	control := NewController(logger, page, persistent, []Search{
		{
			From:   Location("SFO"),
			To:     Location("PVG"),
			Depart: time.Now(),
			Return: new(time.Now().Add(7 * 24 * time.Hour)),
		},
	}, onDone)
	control.StartWorkers(ctx)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-events:
				if !ok {
					return
				}
				updatePersist(e)

				switch e.Type {
				case axstream.EVENT_RESET:
					logger.Info("page reset", "root", persistent.Root.Hash)
				case axstream.EVENT_PATCH:
					logger.Info("page updated", "updated", len(e.Updated))
				}

				control.DispatchEvent(ControllerTreeUpdate{})
			}
		}
	}()

	// page.MustNavigate("http://localhost:8080")
	page.MustNavigate("https://www.google.com/travel/flights")
	// page.MustNavigate("https://amazon.com")

	select {
	case <-ctx.Done():
		logger.Info("canceled.")
	case err = <-onDone:
		if err != nil {
			logger.Error("scraping failed!", "err", err)
		} else {
			logger.Info("scraping done!")
		}
	}
}
