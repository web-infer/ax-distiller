package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
)

type WorkItem struct {
	Goal string
	URL  string
}

type intentResult struct {
	Goal      string   `json:"goal"`
	StartURLs []string `json:"start_urls"`
}

type Spawner struct {
	client *anthropic.Client
	engine *Engine
	logger *slog.Logger
	worker *Worker
	usage  TokenUsage
}

func NewSpawner(client *anthropic.Client, engine *Engine, logger *slog.Logger, maxTurns int) *Spawner {
	s := &Spawner{
		client: client,
		engine: engine,
		logger: logger,
	}
	s.worker = NewWorker(client, engine, logger, &s.usage, maxTurns)
	return s
}

// Usage returns the shared token counter for this spawner and its worker.
func (s *Spawner) Usage() *TokenUsage { return &s.usage }

func (s *Spawner) Run(ctx context.Context, task string) string {
	intent, err := s.parseIntent(ctx, task)
	if err != nil {
		return fmt.Sprintf("failed to parse intent: %v", err)
	}
	s.logger.Info("intent parsed", "goal", intent.Goal, "urls", intent.StartURLs)

	var findings []string
	for _, u := range intent.StartURLs {
		result := s.worker.Run(ctx, intent.Goal, u)
		if result.Findings != "" {
			findings = append(findings, result.Findings)
		}
	}

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
