package agent

import (
	"ax-distiller/internal/chrome/axstream"
	"ax-distiller/internal/chrome/interact"
	"ax-distiller/internal/structure"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

const MaxPages = 10

var ErrPageLimitReached = errors.New("page limit reached (max 10)")
var ErrAlreadyVisited = errors.New("url already visited")

type EngineResult struct {
	URL       string
	Structure string
	PageNum   int
}

type Engine struct {
	mu         sync.Mutex
	inter      *interact.Interact
	persistent *structure.Persistent
	events     <-chan axstream.Event
	visited    map[string]bool
	pageCount  int
	maxPages   int
	logger     *slog.Logger
}

func NewEngine(inter *interact.Interact, persistent *structure.Persistent, events <-chan axstream.Event, maxPages int, logger *slog.Logger) *Engine {
	return &Engine{
		inter:      inter,
		persistent: persistent,
		events:     events,
		visited:    make(map[string]bool),
		logger:   logger.WithGroup("engine"),
		maxPages: maxPages,
	}
}

// Load is the only entry point for agents to interact with the browser.
// Sequential and mutex-protected — blocks concurrent callers.
func (e *Engine) Load(ctx context.Context, url string) (EngineResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pageCount >= e.maxPages {
		return EngineResult{}, ErrPageLimitReached
	}
	if e.visited[url] {
		return EngineResult{}, ErrAlreadyVisited
	}

	e.logger.Info("engine: navigating", "url", url, "page", e.pageCount+1, "max", e.maxPages)
	if err := e.inter.Navigate(ctx, url); err != nil {
		return EngineResult{}, err
	}
	e.logger.Info("engine: waiting for page reset")

	// drain until EVENT_RESET or timeout
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev := <-e.events:
			e.persistent.HandleEvent(ev)
			if ev.Type == axstream.EVENT_RESET {
				e.logger.Info("engine: got EVENT_RESET, draining patches")
				goto settled
			}
		case <-timeout:
			e.logger.Warn("engine: timed out waiting for EVENT_RESET, proceeding")
			goto settled
		case <-ctx.Done():
			return EngineResult{}, ctx.Err()
		}
	}

settled:
	// drain patches for 2s — EVENT_RESET gives a sparse tree; patches fill it in
	patchTimeout := time.After(2 * time.Second)
patchDrain:
	for {
		select {
		case ev := <-e.events:
			e.persistent.HandleEvent(ev)
		case <-patchTimeout:
			break patchDrain
		case <-ctx.Done():
			break patchDrain
		}
	}

	e.visited[url] = true
	e.pageCount++

	text := Serialize(e.persistent.Root, DefaultSerializeOptions())
	e.dumpDebug(url, text, e.pageCount)
	return EngineResult{
		URL:       url,
		Structure: text,
		PageNum:   e.pageCount,
	}, nil
}

// reread re-serializes the current page state without navigating.
// Used by Executor after an interaction changes DOM without a full page load.
func (e *Engine) reread(ctx context.Context) (EngineResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// drain any pending patch events
	drain:
	for {
		select {
		case ev := <-e.events:
			e.persistent.HandleEvent(ev)
		default:
			break drain
		}
	}

	text := Serialize(e.persistent.Root, DefaultSerializeOptions())
	return EngineResult{
		Structure: text,
		PageNum:   e.pageCount,
	}, nil
}

func (e *Engine) dumpDebug(url, text string, pageNum int) {
	dir := "tmp/agent-debug"
	_ = os.MkdirAll(dir, 0700)
	path := fmt.Sprintf("%s/page-%d.txt", dir, pageNum)
	content := fmt.Sprintf("URL: %s\nPage: %d/%d\n\n%s", url, pageNum, e.maxPages, text)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		e.logger.Warn("debug dump failed", "err", err)
		return
	}
	e.logger.Info("engine: debug dump written", "path", path)
}
