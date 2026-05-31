package main

import (
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/cdp"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/LQR471814/rod"
	rodcdp "github.com/LQR471814/rod/lib/cdp"
	"github.com/LQR471814/rod/lib/proto"
	"github.com/lmittmann/tint"
)

func PrintErrorTree(err error) {
	printErrorTree(err, 0)
}

func printErrorTree(err error, depth int) {
	if err == nil {
		return
	}

	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s- %T: %v\n", indent, err, err)

	// Go 1.20+: errors.Join / multi unwrap
	if u, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range u.Unwrap() {
			printErrorTree(child, depth+1)
		}
		return
	}

	// normal single unwrap
	if u, ok := err.(interface{ Unwrap() error }); ok {
		printErrorTree(u.Unwrap(), depth+1)
	}
}

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
				fmt.Println("received poll eval")
				for {
					info := page.MustInfo()
					pages, err := browser.Pages()
					if err != nil {
						panic(err)
					}
					var targetPage *rod.Page
					for _, p := range pages {
						if p.TargetID == info.TargetID {
							targetPage = p
							break
						}
					}

					_, err = targetPage.Eval("() => console.log('hello')")
					if err == nil {
						fmt.Println("eval success")
						break
					}

					if errors.Is(err, rodcdp.ErrCtxNotFound) {
						fmt.Println("context not found")
					} else {
						PrintErrorTree(err)
					}

					fmt.Println("eval fail, retry...")
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
