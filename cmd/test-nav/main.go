package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/cdp"
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"time"

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

	page := browser.MustPage("about:blank").Context(ctx)

	err = cdp.CommandUnary(ctx, page, proto.AccessibilityEnable{})
	if err != nil {
		return
	}

	pollEval := make(chan struct{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pollEval:
				for {
					_, err := page.Eval("() => console.log('hello')")
					if err == nil {
						fmt.Println("eval success")
						break
					}
					time.Sleep(200 * time.Millisecond)
				}
			}
		}
	}()

	go func() {
		events := page.Event()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-events:
				if !ok {
					break
				}

				switch msg.Method {
				case "Page.frameStartedNavigating":
					go func() { pollEval <- struct{}{} }()
				case "Accessibility.loadComplete":
					fmt.Println("ax load complete")
				}
			}
		}
	}()

	err = cdp.CommandUnary(ctx, page, proto.PageEnable{})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("navigate google flight")
	page.Navigate("https://flights.google.com")
	page.MustWaitStable()

	fmt.Println("navigate amazon")
	page.Navigate("https://amazon.com")
	// TODO: simply ensure that MustEval happens after event proto.PageLifecycleEventNameNetworkAlmostIdle
	page.MustWaitStable()

}
