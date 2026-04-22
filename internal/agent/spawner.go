package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
)

const maxWorkers = 3
const maxPages = 10

type WorkItem struct {
	Goal string
	URL  string
}

type intentResult struct {
	Goal      string   `json:"goal"`
	StartURLs []string `json:"start_urls"`
}

type Spawner struct {
	client  *anthropic.Client
	engine  *Engine
	logger  *slog.Logger
	workers [maxWorkers]*Worker
	usage   TokenUsage
}

func NewSpawner(client *anthropic.Client, engine *Engine, logger *slog.Logger) *Spawner {
	s := &Spawner{
		client: client,
		engine: engine,
		logger: logger,
	}
	for i := range maxWorkers {
		s.workers[i] = NewWorker(client, engine, logger, &s.usage)
	}
	return s
}

// Usage returns the shared token counter for this spawner and its workers.
func (s *Spawner) Usage() *TokenUsage { return &s.usage }

func (s *Spawner) Run(ctx context.Context, task string) string {
	// 1 LLM call: parse intent
	intent, err := s.parseIntent(ctx, task)
	if err != nil {
		return fmt.Sprintf("failed to parse intent: %v", err)
	}
	s.logger.Info("intent parsed", "goal", intent.Goal, "urls", intent.StartURLs)

	// work queue with active-item tracking for clean shutdown
	type queueMsg struct {
		item WorkItem
		done bool // sentinel to signal worker exit
	}
	workQueue := make(chan queueMsg, 32)

	var (
		findingsMu sync.Mutex
		findings   []string
		activeMu   sync.Mutex
		active     int
	)

	enqueue := func(item WorkItem) {
		activeMu.Lock()
		active++
		activeMu.Unlock()
		select {
		case workQueue <- queueMsg{item: item}:
		case <-ctx.Done():
		}
	}

	complete := func() {
		activeMu.Lock()
		active--
		zero := active == 0
		activeMu.Unlock()
		if zero {
			// signal all workers to exit
			for range maxWorkers {
				workQueue <- queueMsg{done: true}
			}
		}
	}

	for _, u := range intent.StartURLs {
		enqueue(WorkItem{Goal: intent.Goal, URL: u})
	}

	var wg sync.WaitGroup
	for i := range maxWorkers {
		wg.Add(1)
		w := s.workers[i]
		go func() {
			defer wg.Done()
			for {
				select {
				case msg := <-workQueue:
					if msg.done {
						return
					}
					result := w.Run(ctx, msg.item.Goal, msg.item.URL)
					if result.Findings != "" {
						findingsMu.Lock()
						findings = append(findings, result.Findings)
						findingsMu.Unlock()
					}
					complete()
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	wg.Wait()

	result := s.synthesize(ctx, intent.Goal, findings)

	in, out, total := s.usage.Total()
	s.logger.Info("token usage", "input", in, "output", out, "total", total)

	return result
}

func (s *Spawner) parseIntent(ctx context.Context, task string) (intentResult, error) {
	msg, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 256,
		System: []anthropic.TextBlockParam{
			{Text: spawnerIntentSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(spawnerIntentUserPrompt(task))),
		},
	})
	if err != nil {
		return intentResult{}, fmt.Errorf("intent parse: %w", err)
	}
	if len(msg.Content) == 0 {
		return intentResult{}, fmt.Errorf("empty response")
	}
	s.usage.Add(msg.Usage)

	var r intentResult
	raw := stripJSON(msg.Content[0].Text)
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return intentResult{}, fmt.Errorf("parse json: %w (raw: %s)", err, raw)
	}
	return r, nil
}

func (s *Spawner) synthesize(ctx context.Context, goal string, findings []string) string {
	msg, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: spawnerSynthesizeSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(spawnerSynthesizeUserPrompt(goal, findings))),
		},
	})
	if err != nil {
		return fmt.Sprintf("synthesis failed: %v", err)
	}
	if len(msg.Content) == 0 {
		return "(no answer)"
	}
	s.usage.Add(msg.Usage)
	return msg.Content[0].Text
}
