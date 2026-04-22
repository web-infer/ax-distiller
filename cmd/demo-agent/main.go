package main

import (
	"ax-distiller/internal/agent"
	"ax-distiller/internal/chrome"
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/fastclient"
	"ax-distiller/internal/chrome/interact"
	"ax-distiller/internal/slogx"
	"ax-distiller/internal/structure"
	"context"
	"flag"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
)

func detectChrome() string {
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"google-chrome",
		"chromium",
		"chromium-browser",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[0]
}

func newBrowser(chromeBin string, headless bool) (*rod.Browser, error) {
	dataTemp := "/tmp/ax-distiller/chrome-data"
	_ = os.RemoveAll(dataTemp)
	if err := os.MkdirAll(dataTemp, 0700); err != nil {
		return nil, err
	}

	controlURL := launcher.New().Bin(chromeBin).
		UserDataDir(dataTemp).
		Headless(headless).
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
		Set("hide-scrollbars", "true").
		MustLaunch()

	browser := rod.New()
	client := fastclient.New()
	ws := &rodcdp.WebSocket{}
	if err := ws.Connect(browser.GetContext(), controlURL, nil); err != nil {
		return nil, err
	}
	client.Start(ws)
	browser.Client(client)
	browser.MustConnect()
	return browser, nil
}

func main() {
	var (
		headless  = flag.Bool("headless", false, "run Chrome headless (no window)")
		maxPages  = flag.Int("max-pages", agent.MaxPages, "max pages to visit (1-10)")
		chromeBin = flag.String("chrome", "", "path to Chrome/Chromium binary (auto-detected if empty)")
		verbose   = flag.Bool("verbose", false, "enable debug logging")
	)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: demo-agent [flags] <task>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "environment:")
		fmt.Fprintln(os.Stderr, "  ANTHROPIC_API_KEY   required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "examples:")
		fmt.Fprintln(os.Stderr, `  demo-agent "Find the price of the first product on amazon.com"`)
		fmt.Fprintln(os.Stderr, `  demo-agent -headless -max-pages 5 "What is the top headline on bbc.com"`)
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}
	task := strings.Join(flag.Args(), " ")

	if *maxPages < 1 || *maxPages > 10 {
		fmt.Fprintln(os.Stderr, "error: --max-pages must be between 1 and 10")
		os.Exit(1)
	}

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	logger := slogx.DemoLogger(logLevel, func(group string, attrs iter.Seq[slog.Attr]) bool {
		if *verbose {
			return true
		}
		switch group {
		case "main", "agent", "engine":
			return true
		}
		return false
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	bin := *chromeBin
	if bin == "" {
		bin = detectChrome()
	}
	logger.Info("using chrome", "bin", bin, "headless", *headless)

	browser, err := newBrowser(bin, *headless)
	if err != nil {
		fmt.Fprintf(os.Stderr, "browser launch failed: %v\n", err)
		os.Exit(1)
	}
	p := browser.MustPage("about:blank")
	chrome.DisableUnusedCDP(p)

	events, err := axstream.Listen(ctx, logger, p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "axstream failed: %v\n", err)
		os.Exit(1)
	}

	persistent := structure.NewPersistent(logger)
	inter := interact.New(p)
	eng := agent.NewEngine(inter, persistent, events, *maxPages, logger)

	client := anthropic.NewClient()
	spawner := agent.NewSpawner(&client, eng, logger)

	// periodic token usage reporter
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				in, out, total := spawner.Usage().Total()
				fmt.Printf("[tokens] input=%d output=%d total=%d\n", in, out, total)
			case <-ctx.Done():
				return
			}
		}
	}()

	fmt.Printf("Task: %s\n\n", task)
	result := spawner.Run(ctx, task)
	fmt.Printf("Result:\n%s\n", result)
}
